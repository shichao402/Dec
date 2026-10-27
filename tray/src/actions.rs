//! 托盘对外部世界的操作：读状态、开浏览器、控制代理服务、弹通知。
//!
//! 服务控制与 `internal/agent/service.go` 用同一套原语与同一组名字
//! （launchd 标签 com.dec.agent、计划任务 DecAgent），两边必须一起改。

use std::path::PathBuf;
use std::process::Command;
use std::time::Duration;

use crate::state::Status;

/// 代理回环地址。优先读代理落盘的 `agent.addr`（实际监听地址，需求 3.3），
/// 其次 DEC_AGENT_PORT，最后 127.0.0.1:43679。每次轮询都重读，代理换端口重启后自动跟上。
/// 地址文件只认回环主机，防止被改写成外部地址把托盘引到别处。
pub fn agent_addr() -> String {
    if let Some(addr) = state_dir()
        .and_then(|d| std::fs::read_to_string(d.join("agent.addr")).ok())
        .map(|s| s.trim().to_string())
        .filter(|a| is_loopback_addr(a))
    {
        return addr;
    }
    let port = std::env::var("DEC_AGENT_PORT")
        .ok()
        .and_then(|v| v.parse::<u16>().ok())
        .filter(|p| *p > 0)
        .unwrap_or(43679);
    format!("127.0.0.1:{port}")
}

fn is_loopback_addr(a: &str) -> bool {
    let Some((host, port)) = a.rsplit_once(':') else {
        return false;
    };
    matches!(host, "127.0.0.1" | "localhost" | "[::1]") && port.parse::<u16>().is_ok_and(|p| p > 0)
}

/// 读一次 /api/status。连不上、响应异常、解析失败都返回 None：
/// 对托盘来说都意味着「拿不到可信状态」。
pub fn fetch_status(addr: &str) -> Option<Status> {
    let agent = ureq::AgentBuilder::new()
        .timeout(Duration::from_secs(3))
        .build();
    let resp = agent
        .get(&format!("http://{addr}/api/status"))
        .call()
        .ok()?;
    serde_json::from_reader(resp.into_reader()).ok()
}

/// 代理状态目录，规则同 `agent.DefaultStateDir`。
fn state_dir() -> Option<PathBuf> {
    if let Some(d) = std::env::var_os("DEC_AGENT_STATE").filter(|d| !d.is_empty()) {
        return Some(PathBuf::from(d));
    }
    if cfg!(windows) {
        return std::env::var_os("LOCALAPPDATA").map(|d| PathBuf::from(d).join("dec-agent"));
    }
    std::env::var_os("HOME").map(|h| PathBuf::from(h).join(".dec-agent"))
}

/// 代理从未连上过时「查看日志」用的默认路径。
pub fn default_log_path() -> String {
    state_dir()
        .map(|d| d.join("logs").join("agent.log").to_string_lossy().into_owned())
        .unwrap_or_default()
}

/// 用系统默认程序打开 URL 或文件。
pub fn open_target(target: &str) -> Result<(), String> {
    if target.is_empty() {
        return Err("没有可打开的目标".into());
    }
    open::that_detached(target).map_err(|e| e.to_string())
}

fn run(cmd: &mut Command) -> Result<(), String> {
    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        // CREATE_NO_WINDOW：托盘是无控制台程序，调 schtasks 不能闪黑框。
        cmd.creation_flags(0x0800_0000);
    }
    let out = cmd.output().map_err(|e| e.to_string())?;
    if out.status.success() {
        Ok(())
    } else {
        let msg = String::from_utf8_lossy(&out.stderr).trim().to_string();
        Err(if msg.is_empty() { format!("退出码 {}", out.status) } else { msg })
    }
}

#[cfg(target_os = "macos")]
mod svc {
    use super::run;
    use std::process::Command;

    const LABEL: &str = "com.dec.agent";

    fn target() -> String {
        // SAFETY: getuid 无副作用、不会失败。
        format!("gui/{}/{}", unsafe { libc_getuid() }, LABEL)
    }

    extern "C" {
        #[link_name = "getuid"]
        fn libc_getuid() -> u32;
    }

    fn plist() -> String {
        let home = std::env::var("HOME").unwrap_or_default();
        format!("{home}/Library/LaunchAgents/{LABEL}.plist")
    }

    /// 重启：kickstart -k；服务已被「退出」卸载时先 bootstrap 再拉起。
    pub fn restart() -> Result<(), String> {
        if run(Command::new("/bin/launchctl").args(["kickstart", "-k", &target()])).is_ok() {
            return Ok(());
        }
        let domain = target().rsplit_once('/').map(|(d, _)| d.to_string()).unwrap_or_default();
        run(Command::new("/bin/launchctl").args(["bootstrap", &domain, &plist()]))
            .map_err(|e| format!("代理服务未安装或无法加载：{e}"))?;
        run(Command::new("/bin/launchctl").args(["kickstart", &target()]))
    }

    /// 停止：bootout 卸载，KeepAlive 不会再拉起；下次登录 launchd 重新加载（需求 1.5）。
    pub fn stop() -> Result<(), String> {
        run(Command::new("/bin/launchctl").args(["bootout", &target()]))
    }

    pub fn notify(title: &str, body: &str) {
        // 文本经 argv 传入，避免拼 AppleScript 字符串带来的转义问题。
        let _ = Command::new("/usr/bin/osascript")
            .args([
                "-e",
                "on run argv",
                "-e",
                "display notification (item 2 of argv) with title (item 1 of argv)",
                "-e",
                "end run",
                title,
                body,
            ])
            .spawn();
    }
}

#[cfg(target_os = "windows")]
mod svc {
    use super::run;
    use std::process::Command;

    const TASK: &str = "DecAgent";

    /// 重启：结束再运行计划任务（与 agent.RestartService 一致）。
    pub fn restart() -> Result<(), String> {
        let _ = run(Command::new("schtasks").args(["/End", "/TN", TASK]));
        run(Command::new("schtasks").args(["/Run", "/TN", TASK]))
            .map_err(|e| format!("代理计划任务未安装或无法运行：{e}"))
    }

    /// 停止：结束计划任务实例；任务是 ONLOGON 触发，下次登录才会再起（需求 1.5）。
    pub fn stop() -> Result<(), String> {
        run(Command::new("schtasks").args(["/End", "/TN", TASK]))
    }

    pub fn notify(title: &str, body: &str) {
        let _ = notify_rust::Notification::new()
            .summary(title)
            .body(body)
            .appname("Dec")
            .show();
    }
}

pub use svc::{notify, restart as restart_agent, stop as stop_agent};
