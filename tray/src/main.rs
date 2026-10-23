//! dec-tray：Dec 代理的托盘组件（agent-tray-access 需求 1–3、6、7）。
//!
//! 独立小进程，与代理只通过回环 /api/status 和系统服务命令松耦合：
//! 托盘挂了代理照跑，代理没跑托盘显示「未运行」。
//!
//! 交互：双击打开管理面；Windows 右键、macOS 单击/右键/Control+单击弹菜单。
//! macOS 菜单栏没有原生双击，单击要等一个系统双击间隔确认不是双击后才弹菜单。
#![cfg_attr(windows, windows_subsystem = "windows")]

#[cfg_attr(not(any(target_os = "macos", target_os = "windows")), allow(dead_code))]
mod state;

#[cfg(any(target_os = "macos", target_os = "windows"))]
mod actions;
#[cfg(any(target_os = "macos", target_os = "windows"))]
mod icons;

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
    use tao::event::{Event, StartCause};
    use tao::event_loop::{ControlFlow, EventLoopBuilder, EventLoopProxy};
    use tray_icon::menu::{Menu, MenuEvent, MenuId, MenuItem, PredefinedMenuItem};
    use tray_icon::{MouseButton, MouseButtonState, TrayIcon, TrayIconBuilder, TrayIconEvent};

    use crate::actions;
    use crate::icons;
    use crate::state::{IconState, Notice, Status, Tracker, View};

    const POLL: Duration = Duration::from_secs(3);

    enum UserEvent {
        Tray(TrayIconEvent),
        Menu(MenuEvent),
        Polled(Option<Status>),
        RestartFailed(String),
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
    }

    impl App {
        fn render(&mut self, v: View) {
            if self.shown.as_ref() == Some(&v) {
                return;
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
                    Notice::Cert(msg) => actions::notify("Dec 本机证书需关注", &msg),
                }
            }
        }

        fn open_admin(&self) {
            let v = self.tracker.last();
            if !v.reachable || v.admin_url.is_empty() {
                actions::notify("无法打开管理面", "代理未就绪，请先确认代理在运行。");
                return;
            }
            if actions::open_target(&v.admin_url).is_err() {
                actions::notify("打开浏览器失败", &format!("请手动访问：{}", v.admin_url));
            }
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

        fn on_menu(&mut self, id: &MenuId) -> bool {
            let it = &self.items;
            if id == it.open.id() {
                self.open_admin();
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

        fn on_tray(&mut self, ev: TrayIconEvent) {
            match ev {
                #[cfg(target_os = "windows")]
                TrayIconEvent::DoubleClick { button: MouseButton::Left, .. } => self.open_admin(),
                TrayIconEvent::Click {
                    button: MouseButton::Left,
                    button_state: MouseButtonState::Up,
                    ..
                } if cfg!(target_os = "macos") => {
                    if mac::control_pressed() {
                        self.pending_click = None;
                        self.show_menu();
                    } else if self.pending_click.take().is_some() {
                        self.open_admin();
                    } else {
                        self.pending_click = Some(Instant::now() + mac::double_click_interval());
                    }
                }
                _ => {}
            }
        }

        fn flush_pending_click(&mut self) {
            if self.pending_click.is_some_and(|d| Instant::now() >= d) {
                self.pending_click = None;
                self.show_menu();
            }
        }
    }

    #[cfg(target_os = "macos")]
    mod mac {
        use objc2_app_kit::{NSEvent, NSEventModifierFlags};
        use std::time::Duration;

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
        };

        event_loop.run(move |event, _, control_flow| {
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
                Event::UserEvent(UserEvent::Tray(ev)) => app.on_tray(ev),
                Event::UserEvent(UserEvent::Menu(ev)) => {
                    if app.on_menu(&ev.id) {
                        app.tray = None;
                        *control_flow = ControlFlow::Exit;
                        return;
                    }
                }
                Event::UserEvent(UserEvent::RestartFailed(e)) => {
                    actions::notify("重启失败", &e);
                }
                _ => {}
            }
            app.flush_pending_click();
            *control_flow = match app.pending_click {
                Some(deadline) => ControlFlow::WaitUntil(deadline),
                None => ControlFlow::Wait,
            };
        });
    }
}
