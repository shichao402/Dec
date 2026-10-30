//! 托盘自带的管理面窗口。
//!
//! 窗口全局唯一：已经打开时只把它拉到前面；关闭即销毁窗口和 WebView，
//! 下次打开重新创建，常驻的仍然只有托盘本身。页面就是代理回环地址上的
//! /admin，与浏览器打开的是同一份，所以不需要自有域名，也不需要证书。
//!
//! 窗口只承载本机回环页面：管理面里指向外部的链接（下载地址、文档等）
//! 交给系统浏览器打开，不在这个窗口里跳走。

use tao::dpi::LogicalSize;
use tao::event_loop::EventLoopWindowTarget;
use tao::window::{Window, WindowBuilder, WindowId};
use wry::{NewWindowResponse, WebView, WebViewBuilder};

use crate::actions;

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
        self.window.set_focus();
    }
}

/// 创建窗口并加载 `url`。
pub fn open<T: 'static>(target: &EventLoopWindowTarget<T>, url: &str) -> Result<AdminWindow, String> {
    let window = WindowBuilder::new()
        .with_title(TITLE)
        .with_inner_size(LogicalSize::new(1120.0, 780.0))
        .with_min_inner_size(LogicalSize::new(720.0, 480.0))
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
    Ok(AdminWindow { window, webview, url: url.to_string() })
}

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
