mod grpc;

use grpc::{read_local_metadata, spawn_local_server, AuthResult, InvokeResult, PingInfo, Session};
use serde::{Deserialize, Serialize};
use std::fs;
use std::path::PathBuf;
use std::time::Duration;
use tauri::{AppHandle, Emitter, State};
use tokio::sync::Mutex;
use uuid::Uuid;

#[derive(Default)]
struct AppState {
    session: Mutex<Option<Session>>,
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

const CREDENTIAL_SERVICE: &str = "dev.dec.console";

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

#[tauri::command]
fn list_connections() -> Result<Vec<SavedConnection>, String> {
    let path = data_file()?;
    if !path.exists() {
        return Ok(Vec::new());
    }
    let data = fs::read_to_string(path).map_err(|e| e.to_string())?;
    serde_json::from_str(&data).map_err(|e| e.to_string())
}

#[tauri::command]
fn save_connection(
    mut conn: SavedConnection,
    password: Option<String>,
) -> Result<SavedConnection, String> {
    if conn.id.is_empty() {
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
    let mut list = list_connections()?;
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
    Ok(conn)
}

#[tauri::command]
fn delete_connection(id: String) -> Result<(), String> {
    let list: Vec<_> = list_connections()?
        .into_iter()
        .filter(|c| c.id != id)
        .collect();
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
async fn connect_target(
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
        "local" => connect_local().await?,
        "ssh" => connect_ssh(&ssh_user, &ssh_host, port).await?,
        _ => {
            let endpoint = format!("{host}:{port}");
            grpc::connect_channel(&endpoint, "", tls, &tls_server_name).await?
        }
    };
    let mut ping = session.ping().await?;
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

async fn connect_local() -> Result<Session, String> {
    if let Ok((endpoint, token)) = read_local_metadata() {
        if let Ok(session) = grpc::connect_channel(&endpoint, &token, false, "").await {
            return Ok(session);
        }
    }
    spawn_local_server()?;
    for _ in 0..50 {
        tokio::time::sleep(Duration::from_millis(100)).await;
        if let Ok((endpoint, token)) = read_local_metadata() {
            if let Ok(session) = grpc::connect_channel(&endpoint, &token, false, "").await {
                return Ok(session);
            }
        }
    }
    Err("本机 dec-server 未就绪".into())
}

async fn connect_ssh(user: &str, host: &str, remote_port: u16) -> Result<Session, String> {
    let local_port = 37_000 + (std::process::id() % 1000) as u16;
    let target = if user.is_empty() {
        host.to_string()
    } else {
        format!("{user}@{host}")
    };
    let spec = format!("{local_port}:127.0.0.1:{remote_port}");
    let child = std::process::Command::new("ssh")
        .args(["-N", "-L", &spec, &target])
        .stdin(std::process::Stdio::null())
        .stdout(std::process::Stdio::null())
        .stderr(std::process::Stdio::null())
        .spawn()
        .map_err(|e| format!("ssh 失败: {e}"))?;
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

// 停止目标设备上的 dec-server 并断开会话；前端随后用已保存的连接重连，
// 本机连接会拉起新版本二进制，远端由该设备的服务管理器负责重启。
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
        .manage(AppState::default())
        .setup(|app| {
            if cfg!(debug_assertions) {
                app.handle().plugin(
                    tauri_plugin_log::Builder::default()
                        .level(log::LevelFilter::Info)
                        .build(),
                )?;
            }
            Ok(())
        })
        .invoke_handler(tauri::generate_handler![
            list_connections,
            save_connection,
            delete_connection,
            load_saved_password,
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
    use super::{validate_remote_transport, SavedConnection};

    #[test]
    fn remote_non_loopback_requires_tls() {
        assert!(validate_remote_transport("remote", "server.example", false).is_err());
        assert!(validate_remote_transport("remote", "server.example", true).is_ok());
        assert!(validate_remote_transport("ssh", "server.example", false).is_ok());
        assert!(validate_remote_transport("remote", "127.0.0.1", false).is_ok());
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
