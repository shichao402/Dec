mod frontend_guard;
mod grpc;
mod runtime_bundle;

use grpc::{read_local_metadata, spawn_local_server, AuthResult, InvokeResult, PingInfo, Session};
use serde::{Deserialize, Serialize};
use std::cmp::Ordering;
use std::collections::VecDeque;
use std::fs;
use std::path::PathBuf;
use std::process::Stdio;
use std::sync::Mutex as StdMutex;
use std::time::Duration;
use tauri::{AppHandle, Emitter, Manager, State};
use tokio::sync::Mutex;
use url::Url;
use uuid::Uuid;

#[derive(Default)]
struct AppState {
    session: Mutex<Option<Session>>,
    pending_intents: StdMutex<VecDeque<OpenIntent>>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize)]
#[serde(rename_all = "kebab-case")]
enum OpenIntent {
    UnlockLocal,
}

const OPEN_INTENT_EVENT: &str = "open-intent";

fn parse_open_intent(value: &str) -> Option<OpenIntent> {
    let url = Url::parse(value).ok()?;
    (url.scheme() == "dec"
        && url.host_str() == Some("unlock")
        && url.path() == "/local"
        && url.query().is_none()
        && url.fragment().is_none()
        && url.username().is_empty()
        && url.password().is_none()
        && url.port().is_none())
    .then_some(OpenIntent::UnlockLocal)
}

fn focus_main_window(app: &AppHandle) {
    let Some(window) = app.get_webview_window("main") else {
        return;
    };
    let _ = window.show();
    if window.is_minimized().unwrap_or(false) {
        let _ = window.unminimize();
    }
    let _ = window.set_focus();
}

fn enqueue_open_intents<'a>(app: &AppHandle, values: impl IntoIterator<Item = &'a str>) {
    let intents: Vec<_> = values.into_iter().filter_map(parse_open_intent).collect();
    if intents.is_empty() {
        return;
    }
    let state = app.state::<AppState>();
    let mut pending = state
        .pending_intents
        .lock()
        .unwrap_or_else(|poisoned| poisoned.into_inner());
    for intent in intents {
        if pending.back() != Some(&intent) {
            pending.push_back(intent);
        }
    }
    drop(pending);
    let _ = app.emit(OPEN_INTENT_EVENT, ());
}

#[tauri::command]
fn take_open_intent(state: State<'_, AppState>) -> Option<OpenIntent> {
    state
        .pending_intents
        .lock()
        .unwrap_or_else(|poisoned| poisoned.into_inner())
        .pop_front()
}

#[derive(Debug, Clone, Serialize, Deserialize)]
struct SavedConnection {
    id: String,
    label: String,
    kind: String,
    host: String,
    port: u16,
    ssh_host: String,
    ssh_user: String,
    #[serde(default)]
    tls: bool,
    #[serde(default)]
    tls_server_name: String,
    #[serde(default)]
    auth_email: String,
    #[serde(default)]
    password_saved: bool,
}

#[derive(Debug, Clone, Deserialize)]
struct ManagedDevice {
    #[serde(rename = "Alias")]
    alias: String,
    #[serde(rename = "SSHTarget")]
    ssh_target: String,
}

#[derive(Debug, Deserialize)]
struct ConnectProbe {
    #[serde(rename = "DecInstalled")]
    dec_installed: bool,
    #[serde(rename = "DecVersion")]
    dec_version: String,
    #[serde(rename = "ListenReady")]
    listen_ready: bool,
}

const CREDENTIAL_SERVICE: &str = "dev.dec.console";
const REMOTE_PROVISION_PORT: u16 = 47_653;
const CONSOLE_VERSION: &str = env!("CARGO_PKG_VERSION");

fn dec_home() -> PathBuf {
    std::env::var_os("DEC_HOME")
        .map(PathBuf::from)
        .or_else(|| dirs::home_dir().map(|home| home.join(".dec")))
        .unwrap_or_else(|| PathBuf::from(".dec"))
}

fn parse_release_version(value: &str) -> Option<(u64, u64, u64)> {
    let value = value.trim().trim_start_matches('v');
    let core = value.split(['-', '+']).next()?;
    let mut parts = core.split('.');
    let parsed = (
        parts.next()?.parse().ok()?,
        parts.next()?.parse().ok()?,
        parts.next()?.parse().ok()?,
    );
    if parts.next().is_some() {
        return None;
    }
    Some(parsed)
}

fn compare_versions(left: &str, right: &str) -> Result<Ordering, String> {
    match (parse_release_version(left), parse_release_version(right)) {
        (Some(left), Some(right)) => Ok(left.cmp(&right)),
        _ if left.trim() == right.trim() => Ok(Ordering::Equal),
        _ => Err(format!("无法比较版本：Console {left} / dec-server {right}")),
    }
}

fn reject_newer_server(server_version: &str) -> Result<(), String> {
    if compare_versions(CONSOLE_VERSION, server_version)? == Ordering::Less {
        return Err(format!(
            "Dec Console {CONSOLE_VERSION} 低于目标服务 {server_version}，拒绝连接。请先更新 Console"
        ));
    }
    Ok(())
}

fn suite_binary(name: &str) -> PathBuf {
    let suffix = if cfg!(windows) { ".exe" } else { "" };
    dec_home().join("bin").join(format!("{name}{suffix}"))
}

fn installed_suite_version() -> Option<String> {
    let output = std::process::Command::new(suite_binary("dec"))
        .arg("--version")
        .stdin(Stdio::null())
        .output()
        .ok()?;
    let text = String::from_utf8_lossy(&output.stdout);
    text.split_whitespace()
        .find(|word| parse_release_version(word).is_some())
        .map(str::to_owned)
}

fn suite_complete() -> bool {
    ["dec", "dec-server", "dec-mcp", "dec-exec"]
        .iter()
        .all(|name| suite_binary(name).is_file())
}

fn stop_legacy_local_server() -> Result<(), String> {
    let path = dec_home().join("run").join("server.json");
    let data = fs::read_to_string(&path).map_err(|e| format!("读取 {path:?} 失败: {e}"))?;
    let value: serde_json::Value =
        serde_json::from_str(&data).map_err(|e| format!("解析 {path:?} 失败: {e}"))?;
    let pid = value
        .get("pid")
        .and_then(serde_json::Value::as_u64)
        .filter(|pid| *pid > 0)
        .ok_or("旧服务发现文件缺少有效 pid")?;
    let status = if cfg!(windows) {
        std::process::Command::new("taskkill")
            .args(["/PID", &pid.to_string(), "/T"])
            .status()
    } else {
        std::process::Command::new("kill")
            .arg(pid.to_string())
            .status()
    }
    .map_err(|e| format!("停止旧 dec-server 失败: {e}"))?;
    if !status.success() {
        return Err(format!("停止旧 dec-server 失败（pid {pid}）"));
    }
    let _ = fs::remove_file(path);
    Ok(())
}

async fn install_local_suite(app: &AppHandle) -> Result<(), String> {
    if let Some(installed) = installed_suite_version() {
        match compare_versions(CONSOLE_VERSION, &installed)? {
            Ordering::Less => {
                return Err(format!(
                    "Dec Console {CONSOLE_VERSION} 低于本机运行时 {installed}，拒绝降级。请先更新 Console"
                ));
            }
            Ordering::Equal if suite_complete() => return Ok(()),
            _ => {}
        }
    }

    runtime_bundle::install(app, &dec_home(), CONSOLE_VERSION)?;
    let installed = installed_suite_version().ok_or("安装后无法读取 Dec 运行时版本")?;
    if compare_versions(CONSOLE_VERSION, &installed)? != Ordering::Equal {
        return Err(format!(
            "Console 与运行时版本未对齐：Console {CONSOLE_VERSION} / 运行时 {installed}"
        ));
    }
    if !suite_complete() {
        return Err("安装后 Dec 四件套仍不完整".into());
    }
    Ok(())
}

fn credential_entry(id: &str) -> Result<keyring::Entry, String> {
    keyring::Entry::new(CREDENTIAL_SERVICE, id).map_err(|e| format!("打开系统凭据库失败: {e}"))
}

fn data_file() -> Result<PathBuf, String> {
    let dir = dirs::data_dir()
        .unwrap_or_else(|| PathBuf::from("."))
        .join("dec-console");
    fs::create_dir_all(&dir).map_err(|e| e.to_string())?;
    Ok(dir.join("connections.json"))
}

fn read_saved_connections() -> Result<Vec<SavedConnection>, String> {
    let path = data_file()?;
    if !path.exists() {
        return Ok(Vec::new());
    }
    let data = fs::read_to_string(path).map_err(|e| e.to_string())?;
    let mut list: Vec<SavedConnection> = serde_json::from_str(&data).map_err(|e| e.to_string())?;
    for conn in &mut list {
        if conn.kind == "ssh" {
            conn.ssh_host = ssh_destination(&conn.ssh_user, &conn.ssh_host);
            conn.ssh_user.clear();
            conn.host = "127.0.0.1".into();
            conn.port = REMOTE_PROVISION_PORT;
        }
    }
    Ok(list)
}

#[tauri::command]
async fn list_connections(app: AppHandle) -> Result<Vec<SavedConnection>, String> {
    let mut list = read_saved_connections()?;
    let mut control = connect_local(&app).await?;
    let result = control
        .invoke(
            "list_managed_devices".into(),
            "".into(),
            "global".into(),
            b"{}".to_vec(),
        )
        .await?;
    if !result.error.is_empty() {
        return Err(result.error);
    }
    let devices: Vec<ManagedDevice> = serde_json::from_str(&result.result_json)
        .map_err(|e| format!("解析受管设备清单失败: {e}"))?;
    for device in devices {
        if let Some(conn) = list.iter_mut().find(|conn| {
            conn.kind == "ssh"
                && (conn.label.eq_ignore_ascii_case(&device.alias)
                    || conn.ssh_host.eq_ignore_ascii_case(&device.ssh_target))
        }) {
            conn.label = device.alias;
            conn.ssh_host = device.ssh_target;
            conn.ssh_user.clear();
            conn.host = "127.0.0.1".into();
            conn.port = REMOTE_PROVISION_PORT;
            continue;
        }
        list.push(SavedConnection {
            id: format!("managed:{}", device.alias.to_lowercase()),
            label: device.alias,
            kind: "ssh".into(),
            host: "127.0.0.1".into(),
            port: REMOTE_PROVISION_PORT,
            ssh_host: device.ssh_target,
            ssh_user: String::new(),
            tls: false,
            tls_server_name: String::new(),
            auth_email: String::new(),
            password_saved: false,
        });
    }
    Ok(list)
}

#[tauri::command]
async fn save_connection(
    app: AppHandle,
    mut conn: SavedConnection,
    password: Option<String>,
) -> Result<SavedConnection, String> {
    if conn.kind == "ssh" {
        conn.host = "127.0.0.1".into();
        conn.port = REMOTE_PROVISION_PORT;
        conn.ssh_host = ssh_destination(&conn.ssh_user, &conn.ssh_host);
        conn.ssh_user.clear();
    }
    if conn.id.is_empty() || conn.id.starts_with("managed:") {
        // MCP 置备派生的 managed:<alias> 条目第一次保存时转成本地连接 ID，
        // 避免把后端派生 ID 当作凭据库的长期主键。
        conn.id = Uuid::new_v4().to_string();
    }
    let entry = credential_entry(&conn.id)?;
    if conn.password_saved {
        if let Some(password) = password.filter(|value| !value.is_empty()) {
            entry
                .set_password(&password)
                .map_err(|e| format!("保存密码到系统凭据库失败: {e}"))?;
        }
    } else if let Err(err) = entry.delete_credential() {
        if !matches!(err, keyring::Error::NoEntry) {
            return Err(format!("从系统凭据库删除密码失败: {err}"));
        }
    }
    let mut list = read_saved_connections()?;
    if let Some(existing) = list.iter_mut().find(|c| c.id == conn.id) {
        *existing = conn.clone();
    } else {
        list.push(conn.clone());
    }
    let path = data_file()?;
    fs::write(
        path,
        serde_json::to_string_pretty(&list).map_err(|e| e.to_string())?,
    )
    .map_err(|e| e.to_string())?;
    if conn.kind == "ssh" {
        register_managed_connection(&app, &conn).await?;
    }
    Ok(conn)
}

async fn register_managed_connection(
    app: &AppHandle,
    conn: &SavedConnection,
) -> Result<(), String> {
    let mut control = connect_local(app).await?;
    let payload = serde_json::json!({
        "Alias": conn.label.trim(),
        "Target": { "Alias": conn.ssh_host.trim(), "Host": "", "User": "", "Port": 0 },
        "Tags": null,
        "ProvisionedVersion": "",
    });
    let result = control
        .invoke(
            "register_managed_device".into(),
            "".into(),
            "global".into(),
            serde_json::to_vec(&payload).map_err(|e| e.to_string())?,
        )
        .await?;
    if !result.error.is_empty() {
        return Err(result.error);
    }
    Ok(())
}

#[tauri::command]
async fn delete_connection(app: AppHandle, id: String) -> Result<(), String> {
    let local = read_saved_connections()?;
    let alias = if let Some(conn) = local.iter().find(|c| c.id == id) {
        (conn.kind == "ssh").then(|| conn.label.clone())
    } else {
        id.strip_prefix("managed:").map(str::to_string)
    };
    let list: Vec<_> = local.into_iter().filter(|c| c.id != id).collect();
    let path = data_file()?;
    fs::write(
        path,
        serde_json::to_string_pretty(&list).map_err(|e| e.to_string())?,
    )
    .map_err(|e| e.to_string())?;
    if let Err(err) = credential_entry(&id)?.delete_credential() {
        if !matches!(err, keyring::Error::NoEntry) {
            return Err(format!("从系统凭据库删除密码失败: {err}"));
        }
    }
    if let Some(alias) = alias {
        let mut control = connect_local(&app).await?;
        let payload = serde_json::json!({ "Alias": alias });
        let result = control
            .invoke(
                "remove_managed_device".into(),
                "".into(),
                "global".into(),
                serde_json::to_vec(&payload).map_err(|e| e.to_string())?,
            )
            .await?;
        if !result.error.is_empty() {
            return Err(result.error);
        }
    }
    Ok(())
}

#[tauri::command]
fn load_saved_password(id: String) -> Result<String, String> {
    match credential_entry(&id)?.get_password() {
        Ok(password) => Ok(password),
        Err(keyring::Error::NoEntry) => Ok(String::new()),
        Err(err) => Err(format!("读取系统凭据库失败: {err}")),
    }
}

#[tauri::command]
async fn probe_remote_host(app: AppHandle, ssh_target: String) -> Result<InvokeResult, String> {
    let target = ssh_target.trim();
    if target.is_empty() {
        return Err("请填写 SSH 主机".into());
    }
    let mut control = connect_local(&app).await?;
    let payload = serde_json::json!({
        "Alias": target,
        "Host": "",
        "User": "",
        "Port": 0,
    });
    control
        .invoke(
            "probe_remote_host".into(),
            format!("device:{}", target.to_lowercase()),
            "global".into(),
            serde_json::to_vec(&payload).map_err(|e| e.to_string())?,
        )
        .await
}

#[tauri::command]
async fn provision_remote_host(
    app: AppHandle,
    alias: String,
    ssh_target: String,
    confirm: String,
    action_key: String,
) -> Result<InvokeResult, String> {
    let target = ssh_target.trim();
    if target.is_empty() {
        return Err("请填写 SSH 主机".into());
    }
    let control = connect_local(&app).await?;
    let project_root = format!("device:{}", target.to_lowercase());
    let payload = serde_json::json!({
        "Alias": alias.trim(),
        "Target": { "Alias": target, "Host": "", "User": "", "Port": 0 },
        "Confirm": confirm,
        "Confirmed": false,
        "Version": format!("v{CONSOLE_VERSION}"),
    });
    let event_root = project_root.clone();
    grpc::run_operation(
        control.client_clone(),
        "provision_remote_host".into(),
        project_root,
        "global".into(),
        serde_json::to_vec(&payload).map_err(|e| e.to_string())?,
        |event| {
            emit_action_event(
                &app,
                &action_key,
                &event_root,
                "provision_remote_host",
                event,
            )
        },
    )
    .await
}

#[tauri::command]
async fn connect_target(
    app: AppHandle,
    kind: String,
    host: String,
    port: u16,
    ssh_host: String,
    ssh_user: String,
    tls: bool,
    tls_server_name: String,
    state: State<'_, AppState>,
) -> Result<PingInfo, String> {
    disconnect_inner(&state).await;
    validate_remote_transport(&kind, &host, tls)?;
    let mut session = match kind.as_str() {
        "local" => connect_local(&app).await?,
        "ssh" => {
            let target = ssh_destination(&ssh_user, &ssh_host);
            ensure_remote_service_running(&app, &target).await?;
            connect_ssh("", &target, REMOTE_PROVISION_PORT).await?
        }
        _ => {
            let endpoint = format!("{host}:{port}");
            grpc::connect_channel(&endpoint, "", tls, &tls_server_name).await?
        }
    };
    let mut ping = session.ping().await?;
    reject_newer_server(&ping.version)?;
    if compare_versions(CONSOLE_VERSION, &ping.version)? != Ordering::Equal {
        let hint = if kind == "remote" {
            "远程直连没有安装通道，请改用 SSH 连接完成运行时升级"
        } else {
            "目标运行时初始化后仍未与 Console 对齐"
        };
        return Err(format!(
            "{hint}：Console {CONSOLE_VERSION} / 服务 {}",
            ping.version
        ));
    }
    // 远程/SSH 客户端拿不到 server.json 的 listen token；即使实例已被别的门面
    // 解锁，也必须 Authenticate 取得属于本会话的 control token。
    if kind != "local" {
        ping.unlocked = false;
    }
    if ping.unlocked {
        session.start_keep_alive();
    }
    *state.session.lock().await = Some(session);
    Ok(ping)
}

fn validate_remote_transport(kind: &str, host: &str, tls: bool) -> Result<(), String> {
    if kind == "remote" && !tls && !matches!(host.trim(), "127.0.0.1" | "localhost" | "::1") {
        return Err("远程直连必须启用 TLS；也可以改用 SSH 隧道".into());
    }
    Ok(())
}

async fn connect_local(app: &AppHandle) -> Result<Session, String> {
    runtime_bundle::prewarm(app, &dec_home(), CONSOLE_VERSION)?;
    if let Ok((endpoint, token)) = read_local_metadata() {
        if let Ok(mut session) = grpc::connect_channel(&endpoint, &token, false, "").await {
            if let Ok(ping) = session.ping().await {
                reject_newer_server(&ping.version)?;
                if compare_versions(CONSOLE_VERSION, &ping.version)? == Ordering::Equal {
                    return Ok(session);
                }
                let graceful = session
                    .shutdown(format!(
                        "Console {CONSOLE_VERSION} 将本机运行时由 {} 升级并对齐",
                        ping.version
                    ))
                    .await
                    .is_ok();
                let metadata = dec_home().join("run").join("server.json");
                if graceful {
                    for _ in 0..50 {
                        if !metadata.exists() {
                            break;
                        }
                        tokio::time::sleep(Duration::from_millis(100)).await;
                    }
                }
                if !graceful || metadata.exists() {
                    // 兼容尚未支持「锁定态本机 Shutdown」的历史服务。
                    stop_legacy_local_server()?;
                }
            }
        }
    }
    install_local_suite(app).await?;
    spawn_local_server()?;
    for _ in 0..50 {
        tokio::time::sleep(Duration::from_millis(100)).await;
        if let Ok((endpoint, token)) = read_local_metadata() {
            if let Ok(mut session) = grpc::connect_channel(&endpoint, &token, false, "").await {
                let ping = session.ping().await?;
                reject_newer_server(&ping.version)?;
                if compare_versions(CONSOLE_VERSION, &ping.version)? != Ordering::Equal {
                    return Err(format!(
                        "本机运行时初始化后版本仍未对齐：Console {CONSOLE_VERSION} / 服务 {}",
                        ping.version
                    ));
                }
                return Ok(session);
            }
        }
    }
    Err("本机 dec-server 未就绪".into())
}

fn ssh_destination(user: &str, host: &str) -> String {
    let host = host.trim();
    let user = user.trim();
    if user.is_empty() || host.contains('@') {
        host.to_string()
    } else {
        format!("{user}@{host}")
    }
}

/// 把 `host:36000` / `user@host:36000` 拆成 ssh 目标与 `-p` 端口。
fn ssh_dial_target(raw: &str) -> (String, Option<u16>) {
    let raw = raw.trim();
    if let Some((host, port_str)) = raw.rsplit_once(':') {
        if !host.is_empty() {
            if let Ok(port) = port_str.parse::<u16>() {
                if port > 0 {
                    return (host.to_string(), Some(port));
                }
            }
        }
    }
    (raw.to_string(), None)
}

async fn ensure_remote_service_running(app: &AppHandle, target: &str) -> Result<(), String> {
    if target.trim().is_empty() {
        return Err("请填写 SSH 主机".into());
    }
    let mut control = connect_local(app).await?;
    let target_payload = serde_json::json!({
        "Alias": target.trim(),
        "Host": "",
        "User": "",
        "Port": 0,
    });
    let operation_key = format!("device:{}", target.trim().to_lowercase());
    let probe_result = control
        .invoke(
            "probe_remote_host".into(),
            operation_key.clone(),
            "global".into(),
            serde_json::to_vec(&target_payload).map_err(|e| e.to_string())?,
        )
        .await?;
    if !probe_result.error.is_empty() {
        return Err(probe_result.error);
    }
    let probe: ConnectProbe = serde_json::from_str(&probe_result.result_json)
        .map_err(|e| format!("解析远端探测结果失败: {e}"))?;
    if !probe.dec_installed || !probe.listen_ready {
        return Err("目标端尚未初始化，请先在连接页完成一次“一键部署”".into());
    }
    reject_newer_server(&probe.dec_version)?;
    if compare_versions(CONSOLE_VERSION, &probe.dec_version)? == Ordering::Greater {
        let provision_payload = serde_json::json!({
            "Alias": target.trim(),
            "Target": target_payload.clone(),
            "Confirm": "",
            "Confirmed": false,
            "Version": format!("v{CONSOLE_VERSION}"),
        });
        let upgraded = grpc::run_operation(
            control.client_clone(),
            "provision_remote_host".into(),
            operation_key.clone(),
            "global".into(),
            serde_json::to_vec(&provision_payload).map_err(|e| e.to_string())?,
            |_| {},
        )
        .await?;
        if !upgraded.error.is_empty() {
            return Err(format!("远端运行时自动升级失败: {}", upgraded.error));
        }
    }

    let result = control
        .invoke(
            "ensure_remote_service".into(),
            operation_key,
            "global".into(),
            serde_json::to_vec(&target_payload).map_err(|e| e.to_string())?,
        )
        .await?;
    if !result.error.is_empty() {
        return Err(result.error);
    }
    Ok(())
}

async fn connect_ssh(user: &str, host: &str, remote_port: u16) -> Result<Session, String> {
    let local_port = 37_000 + (std::process::id() % 1000) as u16;
    let combined = if user.is_empty() {
        host.to_string()
    } else {
        format!("{user}@{host}")
    };
    let (dial, ssh_port) = ssh_dial_target(&combined);
    let spec = format!("{local_port}:127.0.0.1:{remote_port}");
    let mut cmd = std::process::Command::new("ssh");
    cmd.args(["-N", "-L", &spec]);
    if let Some(port) = ssh_port {
        cmd.args(["-p", &port.to_string()]);
    }
    cmd.arg(&dial)
        .stdin(std::process::Stdio::null())
        .stdout(std::process::Stdio::null())
        .stderr(std::process::Stdio::null());
    let child = cmd.spawn().map_err(|e| format!("ssh 失败: {e}"))?;
    tokio::time::sleep(Duration::from_millis(400)).await;
    let mut session =
        grpc::connect_channel(&format!("127.0.0.1:{local_port}"), "", false, "").await?;
    session.ssh = Some(child);
    Ok(session)
}

async fn disconnect_inner(state: &AppState) {
    let mut guard = state.session.lock().await;
    if let Some(mut session) = guard.take() {
        if let Some(task) = session.keep_alive.take() {
            task.abort();
        }
        if let Some(mut child) = session.ssh.take() {
            let _ = child.kill();
        }
    }
}

#[tauri::command]
async fn disconnect(state: State<'_, AppState>) -> Result<(), String> {
    disconnect_inner(&state).await;
    Ok(())
}

// 停止目标设备上的 dec-server 并断开会话；前端随后用已保存的连接重连。
// 本机由门面拉起，远端由连接方经 SSH 按需拉起（ADR 0019）。
#[tauri::command]
async fn stop_service(state: State<'_, AppState>) -> Result<(), String> {
    {
        let mut guard = state.session.lock().await;
        let session = guard.as_mut().ok_or("尚未连接")?;
        session.shutdown("console 请求重启".into()).await?;
    }
    disconnect_inner(&state).await;
    Ok(())
}

#[tauri::command]
async fn ping_server(state: State<'_, AppState>) -> Result<PingInfo, String> {
    let mut guard = state.session.lock().await;
    let session = guard.as_mut().ok_or("尚未连接")?;
    session.ping().await
}

#[tauri::command]
async fn authenticate(
    email: String,
    password: String,
    totp: String,
    remember_device: bool,
    state: State<'_, AppState>,
) -> Result<AuthResult, String> {
    let mut guard = state.session.lock().await;
    let session = guard.as_mut().ok_or("尚未连接")?;
    let result = session
        .authenticate(email, password, totp, remember_device)
        .await?;
    if result.unlocked {
        session.start_keep_alive();
    }
    Ok(result)
}

#[tauri::command]
async fn invoke_method(
    app: AppHandle,
    method: String,
    project_root: String,
    workspace_plane: String,
    payload_json: String,
    action_key: String,
    state: State<'_, AppState>,
) -> Result<InvokeResult, String> {
    let mut guard = state.session.lock().await;
    let session = guard.as_mut().ok_or("尚未连接")?;
    let mut result = session
        .invoke(
            method.clone(),
            project_root.clone(),
            workspace_plane,
            payload_json.into_bytes(),
        )
        .await?;
    for event in result.events.drain(..) {
        emit_action_event(&app, &action_key, &project_root, &method, event);
    }
    Ok(result)
}

#[tauri::command]
async fn get_active_operation(
    project_root: String,
    state: State<'_, AppState>,
) -> Result<serde_json::Value, String> {
    let mut guard = state.session.lock().await;
    let session = guard.as_mut().ok_or("尚未连接")?;
    session.active_operation(project_root).await
}

#[tauri::command]
async fn watch_operation(
    app: AppHandle,
    project_root: String,
    operation_id: String,
    action_key: String,
    operation: String,
    state: State<'_, AppState>,
) -> Result<InvokeResult, String> {
    let client = {
        let guard = state.session.lock().await;
        guard.as_ref().ok_or("尚未连接")?.client_clone()
    };
    let event_root = project_root.clone();
    grpc::watch_operation(client, project_root, operation_id, |event| {
        emit_action_event(&app, &action_key, &event_root, &operation, event);
    })
    .await
}

#[tauri::command]
async fn run_operation(
    app: AppHandle,
    operation: String,
    project_root: String,
    workspace_plane: String,
    payload_json: String,
    action_key: String,
    state: State<'_, AppState>,
) -> Result<InvokeResult, String> {
    let client = {
        let guard = state.session.lock().await;
        guard.as_ref().ok_or("尚未连接")?.client_clone()
    };
    let event_root = project_root.clone();
    let event_operation = operation.clone();
    grpc::run_operation(
        client,
        operation,
        project_root,
        workspace_plane,
        payload_json.into_bytes(),
        |event| {
            emit_action_event(&app, &action_key, &event_root, &event_operation, event);
        },
    )
    .await
}

fn emit_action_event(
    app: &AppHandle,
    action_key: &str,
    project_root: &str,
    operation: &str,
    mut event: serde_json::Value,
) {
    if let Some(object) = event.as_object_mut() {
        object.insert("actionKey".into(), action_key.into());
        object.insert("projectRoot".into(), project_root.into());
        object.insert("operation".into(), operation.into());
    }
    let _ = app.emit("operation-event", event);
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        // 必须最先注册：Windows/Linux 会把 URI 交给第二个进程，再由这里转发给主实例。
        .plugin(tauri_plugin_single_instance::init(|app, _argv, _cwd| {
            // deep-link feature 已先触发 on_open_url；这里仅负责恢复主窗口，
            // 避免同一个 URI 从 argv 和插件事件重复入队。
            focus_main_window(app);
        }))
        .plugin(tauri_plugin_deep_link::init())
        .manage(AppState::default())
        .setup(|app| {
            if cfg!(debug_assertions) {
                if let Err(err) = frontend_guard::probe_dev_frontend() {
                    eprintln!("[dec-console] {err}");
                    std::process::exit(1);
                }
                app.handle().plugin(
                    tauri_plugin_log::Builder::default()
                        .level(log::LevelFilter::Info)
                        .build(),
                )?;
            }

            #[cfg(any(target_os = "linux", all(debug_assertions, windows)))]
            {
                use tauri_plugin_deep_link::DeepLinkExt;
                app.deep_link().register_all()?;
            }

            use tauri_plugin_deep_link::DeepLinkExt;
            if let Some(urls) = app.deep_link().get_current()? {
                let values: Vec<_> = urls.iter().map(Url::as_str).collect();
                enqueue_open_intents(app.handle(), values);
            }
            let deep_link_app = app.handle().clone();
            app.deep_link().on_open_url(move |event| {
                let urls = event.urls();
                let values: Vec<_> = urls.iter().map(Url::as_str).collect();
                enqueue_open_intents(&deep_link_app, values);
                focus_main_window(&deep_link_app);
            });

            let args: Vec<_> = std::env::args().collect();
            enqueue_open_intents(app.handle(), args.iter().map(String::as_str));
            focus_main_window(app.handle());
            Ok(())
        })
        .invoke_handler(tauri::generate_handler![
            take_open_intent,
            list_connections,
            save_connection,
            delete_connection,
            load_saved_password,
            probe_remote_host,
            provision_remote_host,
            connect_target,
            disconnect,
            stop_service,
            ping_server,
            authenticate,
            invoke_method,
            run_operation,
            get_active_operation,
            watch_operation,
        ])
        .run(tauri::generate_context!())
        .expect("error while running Dec console");
}

#[cfg(test)]
mod tests {
    use super::{
        compare_versions, parse_open_intent, parse_release_version, ssh_dial_target,
        validate_remote_transport, OpenIntent, SavedConnection,
    };
    use std::cmp::Ordering;

    #[test]
    fn compares_console_and_runtime_versions() {
        assert_eq!(parse_release_version("v1.13.48"), Some((1, 13, 48)));
        assert_eq!(
            compare_versions("1.14.0", "v1.13.48").unwrap(),
            Ordering::Greater
        );
        assert_eq!(
            compare_versions("1.13.48", "v1.13.48").unwrap(),
            Ordering::Equal
        );
        assert!(compare_versions("dev", "v1.13.48").is_err());
    }

    #[test]
    fn ssh_dial_target_splits_devcloud_port() {
        assert_eq!(
            ssh_dial_target("update.devcloud.woa.com:36000"),
            ("update.devcloud.woa.com".into(), Some(36000))
        );
        assert_eq!(
            ssh_dial_target("root@update.devcloud.woa.com:36000"),
            ("root@update.devcloud.woa.com".into(), Some(36000))
        );
        assert_eq!(ssh_dial_target("build-box"), ("build-box".into(), None));
    }

    #[test]
    fn remote_non_loopback_requires_tls() {
        assert!(validate_remote_transport("remote", "server.example", false).is_err());
        assert!(validate_remote_transport("remote", "server.example", true).is_ok());
        assert!(validate_remote_transport("ssh", "server.example", false).is_ok());
        assert!(validate_remote_transport("remote", "127.0.0.1", false).is_ok());
    }

    #[test]
    fn only_accepts_unlock_local_deep_link() {
        assert_eq!(
            parse_open_intent("dec://unlock/local"),
            Some(OpenIntent::UnlockLocal)
        );
        assert_eq!(
            parse_open_intent("DEC://unlock/local"),
            Some(OpenIntent::UnlockLocal)
        );
        assert_eq!(parse_open_intent("dec://unlock/remote"), None);
        assert_eq!(parse_open_intent("dec://unlock/local?next=remote"), None);
        assert_eq!(parse_open_intent("https://unlock/local"), None);
    }

    #[test]
    fn old_saved_connection_defaults_new_fields() {
        let value = r#"{
          "id":"1","label":"old","kind":"ssh","host":"127.0.0.1","port":1234,
          "ssh_host":"host","ssh_user":"user"
        }"#;
        let conn: SavedConnection = serde_json::from_str(value).expect("legacy connection");
        assert!(!conn.tls);
        assert!(conn.tls_server_name.is_empty());
        assert!(conn.auth_email.is_empty());
        assert!(!conn.password_saved);
        let encoded = serde_json::to_string(&conn).expect("serialize connection");
        assert!(!encoded.contains("password\":"));
    }
}
