//! 托盘自带的管理面窗口。
//!
//! 窗口全局唯一：已经打开时只把它拉到前面；关闭即销毁窗口和 WebView，
//! 下次打开重新创建，常驻的仍然只有托盘本身。页面就是代理回环地址上的
//! /admin，与浏览器打开的是同一份，所以不需要自有域名，也不需要证书。
//!
//! 窗口只承载本机回环页面：管理面里指向外部的链接（下载地址、文档等）
//! 交给系统浏览器打开，不在这个窗口里跳走。
//!
//! Windows 上这个窗口刻意不占任务栏位置（ADR 0060）：入口只有托盘一个，
//! 与 macOS 的 Accessory 形态对齐。窗口仍出现在 Alt+Tab，最小化后双击
//! 托盘图标即可拉回。

use tao::dpi::LogicalSize;
use tao::event_loop::EventLoopWindowTarget;
#[cfg(target_os = "windows")]
use tao::platform::windows::WindowBuilderExtWindows;
#[cfg(target_os = "windows")]
use tao::platform::windows::WindowExtWindows;
#[cfg(target_os = "windows")]
use tao::window::Icon;
use tao::window::{Window, WindowBuilder, WindowId};
use wry::{NewWindowResponse, WebView, WebViewBuilder};

use crate::actions;
#[cfg(target_os = "windows")]
use crate::icons;
#[cfg(target_os = "windows")]
use crate::state::IconState;

const TITLE: &str = "Dec 管理面";

pub struct AdminWindow {
    window: Window,
    webview: WebView,
    url: String,
}

impl AdminWindow {
    pub fn id(&self) -> WindowId {
        self.window.id()
    }

    /// 拉到前面；代理地址变了（换端口重启）就顺带切过去。
    pub fn focus(&mut self, url: &str) {
        if self.url != url && self.webview.load_url(url).is_ok() {
            self.url = url.to_string();
        }
        // set_focus 在窗口不可见时是空操作，所以顺序必须是先露出再聚焦。
        self.window.set_minimized(false);
        self.window.set_visible(true);
        // 窗口重新显示时资源管理器会把任务栏按钮补回来（最小化再还原也会），
        // 所以每次拉到前面都重新删一遍，不能只在创建时做一次。
        #[cfg(target_os = "windows")]
        let _ = self.window.set_skip_taskbar(true);
        self.window.set_focus();
    }
}

/// 创建窗口并加载 `url`。
pub fn open<T: 'static>(target: &EventLoopWindowTarget<T>, url: &str) -> Result<AdminWindow, String> {
    let builder = WindowBuilder::new()
        .with_title(TITLE)
        .with_inner_size(LogicalSize::new(1120.0, 780.0))
        .with_min_inner_size(LogicalSize::new(720.0, 480.0));

    // Windows 上这个窗口不占任务栏位置（ADR 0060）。macOS 靠 Accessory
    // 激活策略做到不进 Dock，Windows 没有对应概念，得显式删掉任务栏按钮。
    // 图标必须两个通道都设：大小图标分属 WM_SETICON 的不同槽位，
    // 只设一个会让标题栏或任务栏退回 exe 的通用占位图。
    #[cfg(target_os = "windows")]
    let builder = {
        let icon = window_icon();
        builder
            .with_skip_taskbar(true)
            .with_window_icon(icon.clone())
            .with_taskbar_icon(icon)
    };

    let window = builder
        .build(target)
        .map_err(|e| format!("创建窗口失败：{e}"))?;

    let webview = WebViewBuilder::new()
        .with_url(url)
        .with_navigation_handler(|u| {
            if is_local(&u) {
                return true;
            }
            let _ = actions::open_target(&u);
            false
        })
        .with_new_window_req_handler(|u, _| {
            // target=_blank 之类的新窗口请求：本机页面留在当前窗口，外部交给浏览器。
            let _ = actions::open_target(&u);
            NewWindowResponse::Deny
        })
        .build(&window)
        .map_err(|e| format!("创建页面失败：{e}"))?;

    // 先确保窗口可见再聚焦：set_focus 遇到不可见窗口会直接返回。
    window.set_visible(true);
    window.set_focus();

    // tao 在 build 阶段就把 skip_taskbar 应用了，但那时窗口还没显示；
    // 显示之后资源管理器会重新建任务栏条目，把上一步的效果盖掉。
    // 所以在窗口真正露出来之后再补一次。
    #[cfg(target_os = "windows")]
    let _ = window.set_skip_taskbar(true);

    Ok(AdminWindow { window, webview, url: url.to_string() })
}

/// 窗口图标：复用托盘那枚品牌图形。
///
/// 不用更大尺寸是刻意的：光栅化耗时随边长平方增长，实测同一枚图形
/// 64px 约 0.39 秒、128px 就要 20 秒、256px 要 83 秒（release 构建）。
/// 窗口图标在任务栏与 Alt+Tab 里的显示尺寸不超过 32px，64px 已够系统取用。
///
/// 缓存的是 RGBA 字节而不是成品 [`Icon`]：`tao::window::Icon` 内含
/// `HICON`，既不 `Sync` 也不适合跨线程持有，而字节是可以安全共享的。
/// 缓存命中后每次只需一次 `CreateIcon`，可以忽略。
///
/// 用 [`IconState::Ok`] 是因为这是「这是一枚 Dec 窗口」的身份标识，
/// 不该随代理运行状态变色——状态在托盘图标上已经表达过了。
#[cfg(target_os = "windows")]
fn window_icon_rgba() -> &'static [u8] {
    static CACHED: std::sync::OnceLock<Vec<u8>> = std::sync::OnceLock::new();
    CACHED.get_or_init(|| icons::rgba(IconState::Ok))
}

#[cfg(target_os = "windows")]
fn window_icon() -> Option<Icon> {
    Icon::from_rgba(window_icon_rgba().to_vec(), icons::SIZE, icons::SIZE).ok()
}

/// 提前在后台把窗口图标的像素算好。
///
/// 光栅化要几百毫秒，若等到双击托盘才做，窗口会晚半拍才出现。托盘启动时
/// 就让一个后台线程把它算完，用户第一次双击时直接命中缓存。与「重启中」
/// 旋转帧的预渲染是同一个思路。
#[cfg(target_os = "windows")]
pub fn warm_up() {
    std::thread::spawn(|| {
        let _ = window_icon_rgba();
    });
}

#[cfg(not(target_os = "windows"))]
pub fn warm_up() {}

/// 只认代理回环页面：127.0.0.1、localhost、*.localhost 的 http 地址，以及
/// WebView 内部使用的 about:blank。
pub fn is_local(url: &str) -> bool {
    if url == "about:blank" {
        return true;
    }
    let Some(rest) = url.strip_prefix("http://") else {
        return false;
    };
    let authority = rest.split(['/', '?', '#']).next().unwrap_or("");
    let Some((host, port)) = authority.rsplit_once(':') else {
        return false;
    };
    if port.parse::<u16>().map_or(true, |p| p == 0) {
        return false;
    }
    let host = host.to_ascii_lowercase();
    host == "127.0.0.1" || host == "localhost" || host.ends_with(".localhost")
}

#[cfg(test)]
mod tests {
    use super::is_local;

    #[test]
    fn only_loopback_pages_stay_in_window() {
        for u in [
            "http://127.0.0.1:43679/admin",
            "http://dec.localhost:43679/admin#x",
            "http://localhost:1/",
            "about:blank",
        ] {
            assert!(is_local(u), "{u}");
        }
        for u in [
            "https://127.0.0.1:43679/admin",
            "http://127.0.0.1/admin",
            "http://127.0.0.1:0/",
            "http://evil.com:43679/",
            "http://127.0.0.1.evil.com:43679/",
            "https://dec.firoyang.com/download",
            "file:///etc/passwd",
        ] {
            assert!(!is_local(u), "{u}");
        }
    }
}
