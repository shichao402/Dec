//! 托盘的平台无关逻辑：把 /api/status 映射成图标、文案与通知。
//!
//! 这里不碰任何 GUI 或系统调用，所以能在任意平台跑单元测试。macOS 与
//! Windows 的托盘只消费 [`View`] 和 [`Notice`]，保证两端口径一致（需求 6.1）。

use chrono::{DateTime, Duration, Utc};
use serde::Deserialize;

/// 托盘图标的五种状态（需求 2.1）。
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum IconState {
    /// 代理在跑且与云端对齐。
    Ok,
    /// 代理在跑但有错误（同步失败、版本未对齐），或重启后超时未恢复。
    Error,
    /// 云端维护中。
    Maintenance,
    /// 托盘刚发起重启，还在等代理连回来。
    Restarting,
    /// 连不上代理。
    Down,
}

/// /api/status 里托盘关心的字段；其余字段忽略。
#[derive(Debug, Clone, Default, Deserialize)]
#[serde(default)]
pub struct Status {
    pub version: String,
    pub device_id: String,
    pub aligned: bool,
    pub server_version: String,
    pub maintenance: bool,
    pub last_sync: Option<DateTime<Utc>>,
    pub last_error: String,
    pub admin_url: String,
    pub http_addr: String,
    pub log_path: String,
    pub urls: Vec<AccessUrl>,
}

/// 对应 /api/status 的 urls 项。
#[derive(Debug, Clone, Default, Deserialize)]
#[serde(default)]
pub struct AccessUrl {
    pub url: String,
    pub available: bool,
}

/// 托盘渲染所需的一切。
#[derive(Debug, Clone, PartialEq)]
pub struct View {
    pub icon: IconState,
    pub tooltip: String,
    /// 菜单首行的状态文本。
    pub status_line: String,
    /// 版本未对齐时的「本机 x，云端 y」，否则为空（需求 1.6）。
    pub version_note: String,
    /// 双击与「打开管理面」窗口加载的地址（需求 3.1）。
    pub admin_url: String,
    /// 「查看日志」打开的文件。
    pub log_path: String,
    /// 连得上代理。
    pub reachable: bool,
}

/// 需要弹系统通知的事件。只在状态跳变的那一刻产生一次，避免持续骚扰。
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Notice {
    /// 从「连得上」变成「连不上」。
    Lost,
    /// 发起重启后超过宽限期仍连不上（需求 1.10）。
    RestartTimeout,
}

/// 发起重启后等代理连回来的宽限期（需求 1.10：30 秒）。
pub const RESTART_GRACE_SECS: i64 = 30;

/// 状态机：吃轮询结果，吐视图与通知。
#[derive(Debug, Default)]
pub struct Tracker {
    restart_until: Option<DateTime<Utc>>,
    restart_timed_out: bool,
    was_reachable: bool,
    /// 最近一次连上时拿到的日志路径；连不上时「查看日志」仍要能用。
    last_log_path: String,
    default_log_path: String,
    last: Option<View>,
}

impl Tracker {
    pub fn new(default_log_path: String) -> Self {
        Self { default_log_path, ..Default::default() }
    }

    /// 托盘发起重启或启动时调用，进入宽限期。
    pub fn begin_restart(&mut self, now: DateTime<Utc>) {
        self.restart_until = Some(now + Duration::seconds(RESTART_GRACE_SECS));
        self.restart_timed_out = false;
    }

    /// 最近一次视图；还没轮询过时按「连不上」处理。
    pub fn last(&self) -> View {
        self.last.clone().unwrap_or_else(|| self.down_view())
    }

    /// 处理一次轮询。`st` 为 `None` 表示没连上代理。
    pub fn observe(&mut self, st: Option<&Status>, now: DateTime<Utc>) -> (View, Vec<Notice>) {
        let mut notices = Vec::new();
        let view = match st {
            Some(st) => {
                self.restart_until = None;
                self.restart_timed_out = false;
                if !st.log_path.is_empty() {
                    self.last_log_path = st.log_path.clone();
                }
                self.was_reachable = true;
                classify(st, now)
            }
            None => {
                let in_grace = self.restart_until.is_some_and(|u| now < u);
                if in_grace {
                    self.restarting_view()
                } else if self.restart_until.is_some() || self.restart_timed_out {
                    // 宽限期过了还没回来：置异常并提示看日志，只提示一次。
                    if !self.restart_timed_out {
                        notices.push(Notice::RestartTimeout);
                    }
                    self.restart_until = None;
                    self.restart_timed_out = true;
                    self.was_reachable = false;
                    self.timeout_view()
                } else {
                    if self.was_reachable {
                        notices.push(Notice::Lost);
                    }
                    self.was_reachable = false;
                    self.down_view()
                }
            }
        };
        self.last = Some(view.clone());
        (view, notices)
    }

    fn log_path(&self) -> String {
        if self.last_log_path.is_empty() {
            self.default_log_path.clone()
        } else {
            self.last_log_path.clone()
        }
    }

    fn offline(&self, icon: IconState, line: &str, tip: &str) -> View {
        View {
            icon,
            tooltip: tip.to_string(),
            status_line: line.to_string(),
            version_note: String::new(),
            admin_url: String::new(),
            log_path: self.log_path(),
            reachable: false,
        }
    }

    fn down_view(&self) -> View {
        self.offline(IconState::Down, "未运行", "Dec 代理未运行")
    }

    fn restarting_view(&self) -> View {
        self.offline(IconState::Restarting, "重启中…", "Dec 代理重启中…")
    }

    fn timeout_view(&self) -> View {
        self.offline(IconState::Error, "重启后未恢复", "Dec 代理重启后未恢复，请查看日志")
    }
}

/// 把一次成功的轮询映射成视图。
///
/// 优先级：维护 > 同步错误 > 版本未对齐 > 正常。
pub fn classify(st: &Status, now: DateTime<Utc>) -> View {
    let (icon, line) = if st.maintenance {
        (IconState::Maintenance, "云端维护中")
    } else if !st.last_error.is_empty() {
        (IconState::Error, "同步异常")
    } else if !st.aligned {
        (IconState::Error, "版本待更新")
    } else {
        (IconState::Ok, "运行正常")
    };
    let version_note = if !st.aligned && !st.server_version.is_empty() {
        format!("本机 {}，云端 {}", or(&st.version, "未知"), or(&st.server_version, "未知"))
    } else {
        String::new()
    };
    let admin_url = pick_admin_url(st);
    View {
        icon,
        tooltip: tooltip(st, line, &admin_url, now),
        status_line: line.to_string(),
        version_note,
        admin_url,
        log_path: st.log_path.clone(),
        reachable: true,
    }
}

/// 「打开管理面」的地址：优先代理算好的 admin_url，其次第一个可用 url，
/// 最后用实际回环地址兜底（需求 3.1）。
pub fn pick_admin_url(st: &Status) -> String {
    if !st.admin_url.is_empty() {
        return st.admin_url.clone();
    }
    if let Some(u) = st.urls.iter().find(|u| u.available && !u.url.is_empty()) {
        return format!("{}/admin", u.url.trim_end_matches('/'));
    }
    if !st.http_addr.is_empty() {
        return format!("http://{}/admin", st.http_addr);
    }
    String::new()
}

fn tooltip(st: &Status, line: &str, admin_url: &str, now: DateTime<Utc>) -> String {
    let mut s = format!("Dec 代理 · {line}");
    // 写明实际地址：端口可能被参数改掉，想在浏览器里打开时要靠这里知道。
    if !admin_url.is_empty() {
        s.push_str("\n访问 ");
        s.push_str(admin_url.trim_end_matches("/admin"));
    }
    if !st.device_id.is_empty() {
        s.push_str("\n设备 ");
        s.push_str(&st.device_id.chars().take(12).collect::<String>());
    }
    if !st.version.is_empty() {
        s.push_str("\n版本 ");
        s.push_str(&st.version);
    }
    s.push_str("\n最近同步 ");
    s.push_str(&human_since(st.last_sync, now));
    if !st.last_error.is_empty() {
        s.push_str("\n错误：");
        s.push_str(st.last_error.lines().next().unwrap_or(""));
    }
    s
}

/// 把时间点说成「刚刚 / N 分钟前 / N 小时前 / N 天前 / 从未」。
/// Go 的零值时间（0001-01-01）也算「从未」。
pub fn human_since(t: Option<DateTime<Utc>>, now: DateTime<Utc>) -> String {
    let Some(t) = t.filter(|t| t.timestamp() > 0) else {
        return "从未".into();
    };
    let secs = (now - t).num_seconds();
    match secs {
        s if s < 60 => "刚刚".into(),
        s if s < 3600 => format!("{} 分钟前", s / 60),
        s if s < 86400 => format!("{} 小时前", s / 3600),
        s => format!("{} 天前", s / 86400),
    }
}

fn or<'a>(s: &'a str, def: &'a str) -> &'a str {
    if s.trim().is_empty() {
        def
    } else {
        s
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn t0() -> DateTime<Utc> {
        DateTime::parse_from_rfc3339("2026-10-23T12:00:00Z").unwrap().with_timezone(&Utc)
    }

    fn ok_status() -> Status {
        Status {
            version: "v2.1.6".into(),
            device_id: "dev-abcdefghijklmnop".into(),
            aligned: true,
            server_version: "v2.1.6".into(),
            last_sync: Some(t0() - Duration::seconds(5)),
            admin_url: "http://127.0.0.1:43679/admin".into(),
            http_addr: "127.0.0.1:43679".into(),
            log_path: "/tmp/agent.log".into(),
            ..Default::default()
        }
    }

    #[test]
    fn parses_real_status_json_with_go_zero_time() {
        let raw = r#"{"server_url":"https://dec.firoyang.com","version":"v2.1.6","aligned":true,
            "last_sync":"0001-01-01T00:00:00Z","urls":[{"url":"http://dec.localhost:43679","kind":"localhost","available":true}],
            "unknown_field":1}"#;
        let st: Status = serde_json::from_str(raw).unwrap();
        assert_eq!(human_since(st.last_sync, t0()), "从未");
        assert_eq!(pick_admin_url(&st), "http://dec.localhost:43679/admin");
    }

    #[test]
    fn icon_priority_maintenance_error_unaligned_ok() {
        let mut st = ok_status();
        assert_eq!(classify(&st, t0()).icon, IconState::Ok);
        st.aligned = false;
        let v = classify(&st, t0());
        assert_eq!(v.icon, IconState::Error);
        assert_eq!(v.version_note, "本机 v2.1.6，云端 v2.1.6");
        st.last_error = "boom\nstack".into();
        let v = classify(&st, t0());
        assert_eq!(v.status_line, "同步异常");
        assert!(v.tooltip.contains("错误：boom") && !v.tooltip.contains("stack"));
        st.maintenance = true;
        assert_eq!(classify(&st, t0()).icon, IconState::Maintenance);
    }

    #[test]
    fn admin_url_fallback_chain() {
        let mut st = ok_status();
        assert_eq!(pick_admin_url(&st), "http://127.0.0.1:43679/admin");
        st.admin_url.clear();
        st.urls = vec![
            AccessUrl { url: "https://x".into(), available: false },
            AccessUrl { url: "http://dec.localhost:43679/".into(), available: true },
        ];
        assert_eq!(pick_admin_url(&st), "http://dec.localhost:43679/admin");
        st.urls.clear();
        assert_eq!(pick_admin_url(&st), "http://127.0.0.1:43679/admin");
    }

    #[test]
    fn lost_notice_only_on_transition() {
        let mut tr = Tracker::new("/default.log".into());
        let (_, n) = tr.observe(None, t0());
        assert!(n.is_empty(), "启动时就连不上不应弹「失去连接」");
        let (_, n) = tr.observe(Some(&ok_status()), t0());
        assert!(n.is_empty());
        let (v, n) = tr.observe(None, t0());
        assert_eq!(n, vec![Notice::Lost]);
        assert_eq!(v.icon, IconState::Down);
        assert_eq!(v.log_path, "/tmp/agent.log", "连不上时仍能打开最近的日志");
        let (_, n) = tr.observe(None, t0());
        assert!(n.is_empty(), "持续连不上只提示一次");
    }

    #[test]
    fn restart_grace_then_timeout_marks_error_once() {
        let mut tr = Tracker::new("/default.log".into());
        tr.observe(Some(&ok_status()), t0());
        tr.begin_restart(t0());
        let (v, n) = tr.observe(None, t0() + Duration::seconds(10));
        assert_eq!(v.icon, IconState::Restarting);
        assert!(n.is_empty(), "宽限期内不弹「失去连接」");
        let (v, n) = tr.observe(None, t0() + Duration::seconds(31));
        assert_eq!(v.icon, IconState::Error);
        assert_eq!(n, vec![Notice::RestartTimeout]);
        let (v, n) = tr.observe(None, t0() + Duration::seconds(40));
        assert_eq!(v.icon, IconState::Error, "超时后保持异常态，不退回「未运行」");
        assert!(n.is_empty());
        let (v, _) = tr.observe(Some(&ok_status()), t0() + Duration::seconds(50));
        assert_eq!(v.icon, IconState::Ok);
    }

    #[test]
    fn restart_recovers_within_grace() {
        let mut tr = Tracker::new(String::new());
        tr.begin_restart(t0());
        tr.observe(None, t0() + Duration::seconds(2));
        let (v, n) = tr.observe(Some(&ok_status()), t0() + Duration::seconds(4));
        assert_eq!(v.icon, IconState::Ok);
        assert!(n.is_empty());
        let (v, n) = tr.observe(None, t0() + Duration::seconds(60));
        assert_eq!(v.icon, IconState::Down);
        assert_eq!(n, vec![Notice::Lost], "恢复后再掉线按普通掉线处理");
    }

    #[test]
    fn tooltip_shows_actual_address() {
        let mut st = ok_status();
        st.admin_url = "http://127.0.0.1:50001/admin".into();
        let v = classify(&st, t0());
        assert!(v.tooltip.contains("访问 http://127.0.0.1:50001\n"), "{}", v.tooltip);
    }

    #[test]
    fn human_since_buckets() {
        assert_eq!(human_since(Some(t0() - Duration::seconds(30)), t0()), "刚刚");
        assert_eq!(human_since(Some(t0() - Duration::minutes(5)), t0()), "5 分钟前");
        assert_eq!(human_since(Some(t0() - Duration::hours(3)), t0()), "3 小时前");
        assert_eq!(human_since(Some(t0() - Duration::days(2)), t0()), "2 天前");
        assert_eq!(human_since(None, t0()), "从未");
    }
}
