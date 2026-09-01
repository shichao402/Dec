use std::io::{Read, Write};
use std::net::TcpStream;
use std::time::Duration;

/// 开发前端端口。刻意避开 Vite 默认 5173：本机多项目时那个端口几乎一定被别的软件占着。
/// 与 `client/vite.config.ts`、`tauri.conf.json` 的 `devUrl` 必须同步。
pub const DEV_FRONTEND_PORT: u16 = 59124;

/// 写入 `client/index.html` 的身份标记。debug WebView 只加载带这个标记的页面。
pub const CONSOLE_IDENTITY: &str = "com.shichao402.dec.console";

pub fn identity_meta_marker() -> String {
    format!(r#"name="dec-console" content="{CONSOLE_IDENTITY}""#)
}

pub fn frontend_identity_ok(html: &str) -> bool {
    html.contains(&identity_meta_marker())
}

/// debug 构建在显示窗口前核对 `devUrl`。对不上就退出，避免把 Authenticate IPC
/// 交给占用该端口的其它前端（Vite 默认端口碰撞时会发生）。
pub fn probe_dev_frontend() -> Result<(), String> {
    let html = fetch_dev_index().map_err(|err| {
        format!(
            "Dec Console 开发前端未在 127.0.0.1:{DEV_FRONTEND_PORT} 提供页面（{err}）。请用 `npm run tauri dev` 启动，不要直接运行 debug 的 app.exe。"
        )
    })?;
    if !frontend_identity_ok(&html) {
        return Err(format!(
            "127.0.0.1:{DEV_FRONTEND_PORT} 上跑的不是 Dec Console（缺少 meta name=dec-console）。拒绝打开窗口，以免把主密码框交给其它项目的页面。"
        ));
    }
    Ok(())
}

fn fetch_dev_index() -> Result<String, String> {
    let mut stream = TcpStream::connect(("127.0.0.1", DEV_FRONTEND_PORT))
        .map_err(|err| format!("连不上: {err}"))?;
    let timeout = Some(Duration::from_secs(2));
    stream
        .set_read_timeout(timeout)
        .map_err(|err| err.to_string())?;
    stream
        .set_write_timeout(timeout)
        .map_err(|err| err.to_string())?;
    let request = format!(
        "GET / HTTP/1.1\r\nHost: 127.0.0.1:{DEV_FRONTEND_PORT}\r\nConnection: close\r\n\r\n"
    );
    stream
        .write_all(request.as_bytes())
        .map_err(|err| format!("写入失败: {err}"))?;
    let mut body = Vec::new();
    stream
        .read_to_end(&mut body)
        .map_err(|err| format!("读取失败: {err}"))?;
    Ok(String::from_utf8_lossy(&body).into_owned())
}

#[cfg(test)]
mod tests {
    use super::{frontend_identity_ok, identity_meta_marker, CONSOLE_IDENTITY};

    #[test]
    fn accepts_console_index_html() {
        let html = format!(
            r#"<!doctype html><html><head><meta {marker} /></head></html>"#,
            marker = identity_meta_marker()
        );
        assert!(frontend_identity_ok(&html));
        assert!(identity_meta_marker().contains(CONSOLE_IDENTITY));
    }

    #[test]
    fn rejects_foreign_frontend() {
        let loom = "<!doctype html><html><head><title>Loom</title></head></html>";
        assert!(!frontend_identity_ok(loom));
        assert!(!frontend_identity_ok(""));
    }
}
