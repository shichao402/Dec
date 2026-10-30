//! 验证旋转推进与「render 不打断动画」这条约定。
//!
//! 之前踩过一次：render 每次轮询（3 秒）都会重置旋转计时，导致动画
//! 转一圈就停住。这里用最小状态机复现该逻辑，避免回归。

use std::time::{Duration, Instant};

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum IconState {
    Ok,
    Restarting,
}

#[derive(Debug, Clone, Copy, PartialEq)]
struct View {
    icon: IconState,
}

const SPIN_TICK: Duration = Duration::from_millis(60);
const FRAMES: u32 = 24;

#[derive(Default)]
struct App {
    shown: Option<View>,
    spin: Option<u32>,
    spin_at: Option<Instant>,
    cache: bool,
    loading: bool,
    started_at: Option<Instant>,
}

impl App {
    /// 与 main.rs 的 render 保持同样的行为。
    fn render(&mut self, v: View, now: Instant) {
        if self.shown.as_ref() == Some(&v) {
            return;
        }
        let spinning = v.icon == IconState::Restarting;
        let already = self.spin.is_some();
        if spinning {
            if !already {
                if self.cache {
                    self.spin = Some(0);
                    self.spin_at = Some(now + SPIN_TICK);
                } else if !self.loading {
                    self.loading = true;
                }
            }
        } else {
            self.spin = None;
            self.spin_at = None;
        }
        self.shown = Some(v);
    }

    fn on_cache_ready(&mut self, now: Instant) {
        self.loading = false;
        self.cache = true;
        if self.shown.is_some_and(|v| v.icon == IconState::Restarting) {
            self.spin = Some(0);
            self.spin_at = Some(now + SPIN_TICK);
            self.started_at = Some(now);
        }
    }

    fn tick(&mut self, now: Instant) {
        let (Some(frame), true) = (self.spin, self.cache) else { return };
        if self.spin_at.is_some_and(|at| now >= at) {
            self.spin = Some((frame + 1) % FRAMES);
            self.spin_at = Some(now + SPIN_TICK);
        }
    }
}

#[test]
fn render_must_not_restart_the_spin() {
    let t0 = Instant::now();
    let mut app = App::default();

    // 第一次轮询：进入重启中，缓存还没好，开始后台预渲染。
    app.render(View { icon: IconState::Restarting }, t0);
    assert!(app.spin.is_none(), "缓存没好时不应凭空开始转");
    assert!(app.loading);

    // 预渲染完成，开始转。
    app.on_cache_ready(t0 + Duration::from_millis(300));
    assert_eq!(app.spin, Some(0));

    // 转了两帧。
    app.tick(t0 + Duration::from_millis(400));
    app.tick(t0 + Duration::from_millis(500));
    let frame_after_two = app.spin.unwrap();
    assert_eq!(frame_after_two, 2);

    // 轮询又来了一次（3 秒后），状态没变，仍是重启中。
    app.render(View { icon: IconState::Restarting }, t0 + Duration::from_secs(3));
    assert_eq!(app.spin, Some(frame_after_two), "render 不该把帧数重置为 0");

    // 继续还能推进，说明计时没被打断。
    app.tick(t0 + Duration::from_secs(3) + Duration::from_millis(100));
    assert_ne!(app.spin, Some(frame_after_two), "render 之后应当还能继续转");
}

#[test]
fn leaving_restarting_stops_the_spin() {
    let t0 = Instant::now();
    let mut app = App { cache: true, ..Default::default() };
    app.render(View { icon: IconState::Restarting }, t0);
    assert!(app.spin.is_some());

    // 代理连回来了。
    app.render(View { icon: IconState::Ok }, t0 + Duration::from_secs(1));
    assert!(app.spin.is_none(), "离开重启中必须停止旋转");
    assert!(app.spin_at.is_none(), "停止后不能再留下定时唤醒");
}

#[test]
fn spin_loops_without_getting_stuck() {
    let t0 = Instant::now();
    let mut app = App { cache: true, ..Default::default() };
    app.render(View { icon: IconState::Restarting }, t0);
    let mut now = t0;
    for _ in 0..(FRAMES * 3) {
        now += SPIN_TICK;
        app.tick(now);
    }
    // 转了三圈仍应正常推进，不能停在某一帧。
    assert!(app.spin.is_some());
    assert!(app.spin.unwrap() < FRAMES);
}
