//! dec-tray：Dec 代理的托盘组件（agent-tray-access 需求 1–3、6、7）。
//!
//! 独立小进程，与代理只通过回环 /api/status 和系统服务命令松耦合：
//! 托盘挂了代理照跑，代理没跑托盘显示「未运行」。
//!
//! 交互：双击打开管理面窗口；Windows 右键、macOS 单击/右键/Control+单击弹菜单。
//! 管理面窗口全局唯一，加载代理回环地址上的 /admin，关闭即销毁。
//! macOS 菜单栏没有原生双击，单击要等一个系统双击间隔确认不是双击后才弹菜单。
#![cfg_attr(windows, windows_subsystem = "windows")]

#[cfg_attr(not(any(target_os = "macos", target_os = "windows")), allow(dead_code))]
mod state;

#[cfg(any(target_os = "macos", target_os = "windows"))]
mod actions;
#[cfg(any(target_os = "macos", target_os = "windows"))]
mod icons;
#[cfg(any(target_os = "macos", target_os = "windows"))]
mod logo_polygons;
#[cfg(any(target_os = "macos", target_os = "windows"))]
mod window;

fn version() -> &'static str {
    option_env!("DEC_TRAY_VERSION").unwrap_or("dev")
}

fn main() {
    if std::env::args().any(|a| a == "--version" || a == "-v") {
        println!("dec-tray {}", version());
        return;
    }
    #[cfg(any(target_os = "macos", target_os = "windows"))]
    app::run();
    #[cfg(not(any(target_os = "macos", target_os = "windows")))]
    {
        // 需求 6.3：不支持托盘的平台直接跳过，不报错退出，代理不受影响。
        eprintln!("dec-tray: 当前平台未提供托盘，已跳过");
    }
}

#[cfg(any(target_os = "macos", target_os = "windows"))]
mod app {
    use std::sync::mpsc;
    use std::thread;
    use std::time::{Duration, Instant};

    use chrono::Utc;
    use tao::event::{Event, StartCause, WindowEvent};
    use tao::event_loop::{ControlFlow, EventLoopBuilder, EventLoopProxy, EventLoopWindowTarget};
    use tray_icon::menu::{Menu, MenuEvent, MenuId, MenuItem, PredefinedMenuItem};
    use tray_icon::{Icon, MouseButton, MouseButtonState, TrayIcon, TrayIconBuilder, TrayIconEvent};

    use crate::actions;
    use crate::icons;
    use crate::state::{IconState, Notice, Status, Tracker, View};
    use crate::window::{self, AdminWindow};

    const POLL: Duration = Duration::from_secs(3);

    /// 「重启中」图标的旋转节拍。24 帧一圈，每帧 60ms 约 1.44 秒转一圈：
    /// 慢于这个速度显得卡顿，快于它又像卡死重连的转圈提示。
    const SPIN_TICK: Duration = Duration::from_millis(60);

    enum UserEvent {
        Tray(TrayIconEvent),
        Menu(MenuEvent),
        Polled(Option<Status>),
        RestartFailed(String),
        /// 后台线程预渲染好的一整圈旋转帧（按帧序）。
        Spun(Vec<Icon>),
    }

    struct Items {
        header: MenuItem,
        open: MenuItem,
        status: MenuItem,
        log: MenuItem,
        restart: MenuItem,
        quit: MenuItem,
    }

    impl Items {
        fn new() -> (Menu, Self) {
            let items = Items {
                header: MenuItem::new("Dec 代理 · 未运行", false, None),
                open: MenuItem::new("打开管理面", false, None),
                status: MenuItem::new("查看状态", true, None),
                log: MenuItem::new("查看日志", true, None),
                restart: MenuItem::new("重启代理", true, None),
                quit: MenuItem::new("退出", true, None),
            };
            let menu = Menu::new();
            let sep = || PredefinedMenuItem::separator();
            let _ = menu.append_items(&[
                &items.header,
                &sep(),
                &items.open,
                &items.status,
                &items.log,
                &sep(),
                &items.restart,
                &sep(),
                &items.quit,
            ]);
            (menu, items)
        }

        fn apply(&self, v: &View) {
            let mut head = format!("Dec 代理 · {}", v.status_line);
            if !v.version_note.is_empty() {
                head.push_str(&format!("（{}）", v.version_note));
            }
            self.header.set_text(head);
            self.open.set_enabled(v.reachable && !v.admin_url.is_empty());
            self.log.set_enabled(!v.log_path.is_empty());
            // 需求 1.6：未运行时提供「启动代理」。两者底层同一动作：
            // 已卸载的服务先加载再拉起，在跑的服务先结束再拉起。
            let down = !v.reachable && v.icon != IconState::Restarting;
            self.restart.set_text(if down { "启动代理" } else { "重启代理" });
        }
    }

    struct App {
        tray: Option<TrayIcon>,
        items: Items,
        menu: Option<Menu>,
        tracker: Tracker,
        shown: Option<View>,
        poke: mpsc::Sender<()>,
        proxy: EventLoopProxy<UserEvent>,
        /// macOS：第一次左键抬起后等第二击的截止时间（需求 1.4）。
        pending_click: Option<Instant>,
        /// 管理面窗口，同一时间最多一个。
        admin: Option<AdminWindow>,
        /// 托盘只有一个菜单栏图标、没有主窗口；macOS 上窗口打开期间需要
        /// 应用菜单承载「编辑」快捷键，否则 WebView 里的复制粘贴失效。
        #[cfg(target_os = "macos")]
        app_menu: Option<Menu>,
        /// 「重启中」的当前旋转帧；其它状态为 None，图标静止。
        spin: Option<u32>,
        /// 预渲染好的整圈旋转帧，按帧序。
        spin_cache: Option<Vec<tray_icon::Icon>>,
        /// 下一次旋转帧的唤醒时刻。
        spin_at: Option<Instant>,
        /// 是否正在后台算旋转帧，避免重复起线程。
        spin_loading: bool,
    }

    impl App {
        fn render(&mut self, v: View) {
            if self.shown.as_ref() == Some(&v) {
                return;
            }
            let spinning = v.icon == IconState::Restarting;
            // 已经在转就不要重来：轮询每 3 秒会 render 一次，若每次都重置
            // 计时，动画会周期性被打断，看起来一卡一卡。
            let already_spinning = self.spin.is_some();
            if spinning {
                if !already_spinning {
                    // 有缓存就立刻转起来；没有就先让后台算，算好再开始。
                    if self.spin_cache.is_some() {
                        self.spin = Some(0);
                        self.spin_at = Some(Instant::now() + SPIN_TICK);
                    } else if !self.spin_loading {
                        self.spin_loading = true;
                        let proxy = self.proxy.clone();
                        // 整圈 24 帧、每帧 20 毫秒以上，放主线程会卡住托盘。
                        thread::spawn(move || {
                            let frames = icons::spin_frames_ready();
                            let _ = proxy.send_event(UserEvent::Spun(frames));
                        });
                    }
                }
            } else {
                self.spin = None;
                self.spin_at = None;
            }
            if let Some(tray) = &self.tray {
                if self.shown.as_ref().map(|s| s.icon) != Some(v.icon) {
                    let _ = tray.set_icon(Some(icons::icon(v.icon)));
                }
                let _ = tray.set_tooltip(Some(&v.tooltip));
            }
            self.items.apply(&v);
            self.shown = Some(v);
        }

        /// 收到预渲染的旋转帧：缓存起来，若当前正需要旋转就立刻开始。
        fn on_spun(&mut self, frames: Vec<tray_icon::Icon>) {
            self.spin_loading = false;
            if frames.is_empty() || self.spin_cache.is_some() {
                return;
            }
            self.spin_cache = Some(frames);
            if self.shown.as_ref().is_some_and(|v| v.icon == IconState::Restarting) {
                self.spin = Some(0);
                self.spin_at = Some(Instant::now() + SPIN_TICK);
            }
        }

        /// 推进一帧旋转；只在「重启中」生效，否则什么都不做。
        fn advance_spin(&mut self) {
            let (Some(frame), Some(cache)) = (self.spin, self.spin_cache.as_ref()) else {
                return;
            };
            let next = ((frame as usize) + 1) % cache.len();
            self.spin = Some(next as u32);
            if let Some(tray) = &self.tray {
                let _ = tray.set_icon(Some(cache[next].clone()));
            }
            self.spin_at = Some(Instant::now() + SPIN_TICK);
        }

        /// 到点就推进一帧，返回事件循环下次该在何时醒来。
        fn tick_spin(&mut self) -> ControlFlow {
            if let Some(at) = self.spin_at {
                if Instant::now() >= at {
                    self.advance_spin();
                }
            }
            self.next_control_flow()
        }

        /// 事件循环的等待策略：有待办时间点时精确定时唤醒，否则纯等待。
        fn next_control_flow(&self) -> ControlFlow {
            let mut deadline = self.pending_click;
            if let Some(d) = self.spin_at {
                deadline = Some(deadline.map_or(d, |p| p.min(d)));
            }
            match deadline {
                Some(d) => ControlFlow::WaitUntil(d),
                None => ControlFlow::Wait,
            }
        }

        fn on_polled(&mut self, st: Option<Status>) {
            let (v, notices) = self.tracker.observe(st.as_ref(), Utc::now());
            self.render(v);
            for n in notices {
                match n {
                    Notice::Lost => actions::notify(
                        "Dec 代理未运行",
                        "托盘已失去与代理的连接，可从菜单「重启代理」。",
                    ),
                    Notice::RestartTimeout => actions::notify(
                        "Dec 代理重启后未恢复",
                        "已等待 30 秒仍连不上代理，请从菜单「查看日志」排查。",
                    ),
                }
            }
        }

        /// 打开管理面窗口；已经开着就拉到前面，不会出现第二个。
        fn open_admin(&mut self, target: &EventLoopWindowTarget<UserEvent>) {
            let v = self.tracker.last();
            if !v.reachable || v.admin_url.is_empty() {
                actions::notify("无法打开管理面", "代理未就绪，请先确认代理在运行。");
                return;
            }
            if let Some(w) = self.admin.as_mut() {
                w.focus(&v.admin_url);
                mac::activate(target);
                return;
            }
            // 先把进程激活成前台应用，再建窗口。
            // 顺序反过来的话，窗口虽然建在可见状态、也调了 set_focus，
            // 但进程本身还不是活跃应用，窗口会落在别的应用窗口后面，
            // 用户以为没打开。Accessory 模式（不进 Dock）尤其明显。
            mac::stay_in_menu_bar(target);
            mac::activate(target);
            match window::open(target, &v.admin_url) {
                Ok(w) => {
                    #[cfg(target_os = "macos")]
                    {
                        if self.app_menu.is_none() {
                            self.app_menu = Some(mac::app_menu());
                        }
                    }
                    self.admin = Some(w);
                    // 窗口建好后应用激活才真正落到这个窗口上，再拉一次。
                    mac::activate(target);
                }
                Err(e) => {
                    // 窗口起不来（如 Windows 缺 WebView2 运行时）时退回浏览器，入口不能断。
                    eprintln!("dec-tray: {e}");
                    if actions::open_target(&v.admin_url).is_err() {
                        actions::notify("打开管理面失败", &format!("请手动访问：{}", v.admin_url));
                    }
                }
            }
        }

        fn close_admin(&mut self, _target: &EventLoopWindowTarget<UserEvent>) {
            // 窗口只是销毁；进程始终留在菜单栏，激活策略无需切换。
            self.admin = None;
        }

        fn show_status(&self) {
            let v = self.tracker.last();
            let mut body = v.status_line.clone();
            for extra in [&v.version_note, &v.admin_url] {
                if !extra.is_empty() {
                    body.push('\n');
                    body.push_str(extra);
                }
            }
            actions::notify("Dec 代理状态", &body);
        }

        fn restart(&mut self) {
            self.tracker.begin_restart(Utc::now());
            let (v, _) = self.tracker.observe(None, Utc::now());
            self.render(v);
            let proxy = self.proxy.clone();
            let poke = self.poke.clone();
            // launchctl / schtasks 可能要几百毫秒，不在主线程等。
            thread::spawn(move || {
                if let Err(e) = actions::restart_agent() {
                    let _ = proxy.send_event(UserEvent::RestartFailed(e));
                }
                thread::sleep(Duration::from_millis(500));
                let _ = poke.send(());
            });
        }

        fn on_menu(&mut self, id: &MenuId, target: &EventLoopWindowTarget<UserEvent>) -> bool {
            let it = &self.items;
            if id == it.open.id() {
                self.open_admin(target);
            } else if id == it.status.id() {
                self.show_status();
            } else if id == it.log.id() {
                let v = self.tracker.last();
                if actions::open_target(&v.log_path).is_err() {
                    actions::notify("打开日志失败", &format!("日志文件：{}", v.log_path));
                }
            } else if id == it.restart.id() {
                self.restart();
            } else if id == it.quit.id() {
                // 需求 1.5：退出 = 托盘与代理一起停，且不被保活立即拉起。
                if let Err(e) = actions::stop_agent() {
                    actions::notify("停止代理失败", &e);
                }
                return true;
            }
            false
        }

        fn show_menu(&self) {
            if let Some(tray) = &self.tray {
                tray.show_menu();
            }
        }

        fn on_tray(&mut self, ev: TrayIconEvent, target: &EventLoopWindowTarget<UserEvent>) {
            match ev {
                #[cfg(target_os = "windows")]
                TrayIconEvent::DoubleClick { button: MouseButton::Left, .. } => self.open_admin(target),
                TrayIconEvent::Click {
                    button: MouseButton::Left,
                    button_state: MouseButtonState::Up,
                    ..
                } if cfg!(target_os = "macos") => {
                    if mac::control_pressed() {
                        self.pending_click = None;
                        self.show_menu();
                    } else if self.pending_click.take().is_some() {
                        self.open_admin(target);
                    } else {
                        self.pending_click = Some(Instant::now() + mac::double_click_interval());
                    }
                }
                _ => {}
            }
        }

        fn flush_pending_click(&mut self, _target: &EventLoopWindowTarget<UserEvent>) {
            if self.pending_click.is_some_and(|d| Instant::now() >= d) {
                self.pending_click = None;
                self.show_menu();
            }
        }
    }

    #[cfg(target_os = "macos")]
    mod mac {
        use objc2_app_kit::{NSApplication, NSEvent, NSEventModifierFlags};
        use objc2_foundation::MainThreadMarker;
        use std::time::Duration;
        use tao::event_loop::EventLoopWindowTarget;
        use tao::platform::macos::{ActivationPolicy, EventLoopWindowTargetExtMacOS};
        use tray_icon::menu::{Menu, PredefinedMenuItem, Submenu};

        /// 全程留在菜单栏，绝不进 Dock。
        ///
        /// 托盘进程是裸二进制、没有 .app bundle，macOS 找不到任何图标资源，
        /// 进 Dock 只能显示通用的可执行文件占位图。运行期用
        /// `setApplicationIconImage` 虽然能设置成功（读回来确实是品牌图），
        /// 但 Dock 渲染时不认，仍然显示占位图——这条路走不通。
        ///
        /// 所以干脆不进 Dock：窗口照样能开、能交互、能拉前，只是不出现在
        /// Dock 和 Cmd+Tab 里。入口全在菜单栏图标上。
        pub fn stay_in_menu_bar<T>(target: &EventLoopWindowTarget<T>) {
            target.set_activation_policy_at_runtime(ActivationPolicy::Accessory);
        }

        /// 把窗口提到最前。
        ///
        /// Accessory 应用不是「活跃应用」，光 unhide 不足以让窗口拿到焦点，
        /// 必须显式 activate 一次，否则窗口会开在别的应用后面。
        pub fn activate<T>(_target: &EventLoopWindowTarget<T>) {
            let Some(mtm) = MainThreadMarker::new() else {
                return;
            };
            NSApplication::sharedApplication(mtm).activate();
        }

        /// 应用菜单：Cmd+C/V/X/A/Z 与 Cmd+W/Q 都靠它生效。
        pub fn app_menu() -> Menu {
            let menu = Menu::new();
            let app = Submenu::with_items(
                "Dec",
                true,
                &[
                    &PredefinedMenuItem::hide(None),
                    &PredefinedMenuItem::separator(),
                    &PredefinedMenuItem::close_window(Some("关闭窗口")),
                ],
            )
            .expect("应用菜单");
            let edit = Submenu::with_items(
                "编辑",
                true,
                &[
                    &PredefinedMenuItem::undo(None),
                    &PredefinedMenuItem::redo(None),
                    &PredefinedMenuItem::separator(),
                    &PredefinedMenuItem::cut(None),
                    &PredefinedMenuItem::copy(None),
                    &PredefinedMenuItem::paste(None),
                    &PredefinedMenuItem::select_all(None),
                ],
            )
            .expect("编辑菜单");
            let _ = menu.append_items(&[&app, &edit]);
            menu.init_for_nsapp();
            menu
        }

        pub fn double_click_interval() -> Duration {
            let secs = NSEvent::doubleClickInterval();
            Duration::from_secs_f64(if secs > 0.0 { secs } else { 0.25 })
        }

        pub fn control_pressed() -> bool {
            NSEvent::modifierFlags_class().contains(NSEventModifierFlags::Control)
        }
    }

    #[cfg(not(target_os = "macos"))]
    mod mac {
        use std::time::Duration;
        use tao::event_loop::EventLoopWindowTarget;
        pub fn stay_in_menu_bar<T>(_: &EventLoopWindowTarget<T>) {}
        pub fn activate<T>(_: &EventLoopWindowTarget<T>) {}
        pub fn double_click_interval() -> Duration {
            Duration::from_millis(250)
        }
        pub fn control_pressed() -> bool {
            false
        }
    }

    fn spawn_poller(proxy: EventLoopProxy<UserEvent>) -> mpsc::Sender<()> {
        let (tx, rx) = mpsc::channel::<()>();
        thread::spawn(move || loop {
            let st = actions::fetch_status(&actions::agent_addr());
            if proxy.send_event(UserEvent::Polled(st)).is_err() {
                return;
            }
            match rx.recv_timeout(POLL) {
                Ok(()) | Err(mpsc::RecvTimeoutError::Timeout) => {}
                Err(mpsc::RecvTimeoutError::Disconnected) => return,
            }
        });
        tx
    }

    pub fn run() {
        #[allow(unused_mut)]
        let mut event_loop = EventLoopBuilder::<UserEvent>::with_user_event().build();
        #[cfg(target_os = "macos")]
        {
            use tao::platform::macos::{ActivationPolicy, EventLoopExtMacOS};
            // 只在菜单栏出现，不占 Dock。
            event_loop.set_activation_policy(ActivationPolicy::Accessory);
        }

        let proxy = event_loop.create_proxy();
        {
            let p = proxy.clone();
            TrayIconEvent::set_event_handler(Some(move |e| {
                let _ = p.send_event(UserEvent::Tray(e));
            }));
            let p = proxy.clone();
            MenuEvent::set_event_handler(Some(move |e| {
                let _ = p.send_event(UserEvent::Menu(e));
            }));
        }

        let (menu, items) = Items::new();
        let poke = spawn_poller(proxy.clone());
        let mut app = App {
            tray: None,
            items,
            menu: Some(menu),
            tracker: Tracker::new(actions::default_log_path()),
            shown: None,
            poke,
            proxy,
            pending_click: None,
            admin: None,
            #[cfg(target_os = "macos")]
            app_menu: None,
            spin: None,
            spin_cache: None,
            spin_at: None,
            spin_loading: false,
        };

        event_loop.run(move |event, target, control_flow| {
            match event {
                // macOS 要求托盘在事件循环启动后再创建。
                Event::NewEvents(StartCause::Init) => {
                    let first = app.tracker.last();
                    let menu = app.menu.take().expect("菜单只创建一次");
                    app.tray = TrayIconBuilder::new()
                        .with_menu(Box::new(menu))
                        .with_menu_on_left_click(false)
                        .with_icon(icons::icon(IconState::Down))
                        .with_tooltip(&first.tooltip)
                        .build()
                        .map_err(|e| eprintln!("dec-tray: 创建托盘失败：{e}"))
                        .ok();
                    if app.tray.is_none() {
                        // 需求 7.4：托盘起不来不影响代理，直接退出。
                        *control_flow = ControlFlow::Exit;
                        return;
                    }
                }
                Event::UserEvent(UserEvent::Polled(st)) => app.on_polled(st),
                Event::UserEvent(UserEvent::Tray(ev)) => app.on_tray(ev, target),
                Event::WindowEvent { window_id, event: WindowEvent::CloseRequested, .. }
                    if app.admin.as_ref().is_some_and(|w| w.id() == window_id) =>
                {
                    app.close_admin(target);
                }
                Event::UserEvent(UserEvent::Menu(ev)) => {
                    if app.on_menu(&ev.id, target) {
                        app.admin = None;
                        app.tray = None;
                        *control_flow = ControlFlow::Exit;
                        return;
                    }
                }
                Event::UserEvent(UserEvent::RestartFailed(e)) => {
                    actions::notify("重启失败", &e);
                }
                Event::UserEvent(UserEvent::Spun(frames)) => app.on_spun(frames),
                _ => {}
            }
            app.flush_pending_click(target);
            app.tick_spin();
            *control_flow = app.next_control_flow();
        });
    }
}
