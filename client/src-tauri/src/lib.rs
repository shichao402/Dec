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
fn save_connection(mut conn: SavedConnection) -> Result<SavedConnection, String> {
    if conn.id.is_empty() {
        conn.id = Uuid::new_v4().to_string();
    }
    let mut list = list_connections()?;
    if let Some(existing) = list.iter_mut().find(|c| c.id == conn.id) {
        *existing = conn.clone();
    } else {
        list.push(conn.clone());
    }
    let path = data_file()?;
    fs::write(path, serde_json::to_string_pretty(&list).map_err(|e| e.to_string())?)
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
    fs::write(path, serde_json::to_string_pretty(&list).map_err(|e| e.to_string())?)
        .map_err(|e| e.to_string())?;
    Ok(())
}

#[tauri::command]
async fn connect_target(
    kind: String,
    host: String,
    port: u16,
    ssh_host: String,
    ssh_user: String,
    state: State<'_, AppState>,
) -> Result<PingInfo, String> {
    disconnect_inner(&state).await;
    let mut session = match kind.as_str() {
        "local" => connect_local().await?,
        "ssh" => connect_ssh(&ssh_user, &ssh_host, port).await?,
        _ => {
            let endpoint = format!("{host}:{port}");
            grpc::connect_channel(&endpoint, "").await?
        }
    };
    let ping = session.ping().await?;
    *state.session.lock().await = Some(session);
    Ok(ping)
}

async fn connect_local() -> Result<Session, String> {
    if let Ok((endpoint, token)) = read_local_metadata() {
        if let Ok(session) = grpc::connect_channel(&endpoint, &token).await {
            return Ok(session);
        }
    }
    spawn_local_server()?;
    for _ in 0..50 {
        tokio::time::sleep(Duration::from_millis(100)).await;
        if let Ok((endpoint, token)) = read_local_metadata() {
            if let Ok(session) = grpc::connect_channel(&endpoint, &token).await {
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
    let mut session = grpc::connect_channel(&format!("127.0.0.1:{local_port}"), "").await?;
    session.ssh = Some(child);
    Ok(session)
}

async fn disconnect_inner(state: &AppState) {
    let mut guard = state.session.lock().await;
    if let Some(mut session) = guard.take() {
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
    session
        .authenticate(email, password, totp, remember_device)
        .await
}

#[tauri::command]
async fn invoke_method(
    method: String,
    project_root: String,
    workspace_plane: String,
    payload_json: String,
    state: State<'_, AppState>,
) -> Result<InvokeResult, String> {
    let mut guard = state.session.lock().await;
    let session = guard.as_mut().ok_or("尚未连接")?;
    session
        .invoke(
            method,
            project_root,
            workspace_plane,
            payload_json.into_bytes(),
        )
        .await
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
async fn run_operation(
    app: AppHandle,
    operation: String,
    project_root: String,
    workspace_plane: String,
    payload_json: String,
    state: State<'_, AppState>,
) -> Result<InvokeResult, String> {
    let mut guard = state.session.lock().await;
    let session = guard.as_mut().ok_or("尚未连接")?;
    session
        .run_operation(
            operation,
            project_root,
            workspace_plane,
            payload_json.into_bytes(),
            |event| {
                let _ = app.emit("operation-event", event);
            },
        )
        .await
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
            connect_target,
            disconnect,
            ping_server,
            authenticate,
            invoke_method,
            run_operation,
            get_active_operation,
        ])
        .run(tauri::generate_context!())
        .expect("error while running Dec console");
}
