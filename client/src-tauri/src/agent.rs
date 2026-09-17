use crate::connect_target_inner;
use serde::{Deserialize, Serialize};
use std::fs;
use std::io::Write;
use std::path::PathBuf;
use std::sync::atomic::{AtomicBool, Ordering};
use std::time::Duration;
use tauri::{AppHandle, Emitter, Manager};
use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::net::{TcpListener, TcpStream};
use uuid::Uuid;

const METADATA_VERSION: u32 = 1;
const MCP_UNLOCK_TIMEOUT_MS: i64 = 180_000;
static METADATA_WRITTEN: AtomicBool = AtomicBool::new(false);

#[derive(Debug, Serialize, Deserialize)]
struct ConsoleMetadata {
    version: u32,
    endpoint: String,
    token: String,
    pid: u32,
}

#[derive(Debug, Deserialize)]
struct InvokeBody {
    #[serde(default)]
    method: String,
    #[serde(default)]
    operation: String,
    #[serde(default)]
    project_root: String,
    #[serde(default)]
    workspace_plane: String,
    #[serde(default)]
    payload: serde_json::Value,
}

#[derive(Debug, Deserialize)]
struct ToolCallBody {
    name: String,
    #[serde(default)]
    arguments: serde_json::Value,
    #[serde(default)]
    client_version: String,
}

#[derive(Debug, Deserialize)]
struct ConnectBody {
    #[serde(default)]
    id: String,
    #[serde(default)]
    kind: String,
    #[serde(default)]
    host: String,
    #[serde(default)]
    port: u16,
    #[serde(default)]
    ssh_host: String,
    #[serde(default)]
    ssh_user: String,
    #[serde(default)]
    tls: bool,
    #[serde(default)]
    tls_server_name: String,
}

#[derive(Debug, Deserialize)]
struct PlanResult {
    #[serde(default)]
    name: String,
    #[serde(default)]
    owner: String,
    #[serde(default)]
    shape: String,
    #[serde(default)]
    steps: Vec<PlanStep>,
    #[serde(default)]
    envelope: serde_json::Map<String, serde_json::Value>,
    #[serde(default)]
    error: String,
}

#[derive(Debug, Deserialize)]
struct PlanStep {
    #[serde(default)]
    kind: String,
    #[serde(default)]
    method: String,
    #[serde(default)]
    project_root: String,
    #[serde(default)]
    plane: String,
    #[serde(default)]
    payload: serde_json::Value,
    #[serde(default)]
    key: String,
    #[serde(default)]
    optional: bool,
}

pub async fn serve(app: AppHandle) {
    let listener = match TcpListener::bind("127.0.0.1:0").await {
        Ok(listener) => listener,
        Err(err) => {
            eprintln!("[dec-console] Agent 网关绑定失败: {err}");
            return;
        }
    };
    let addr = match listener.local_addr() {
        Ok(addr) => addr,
        Err(err) => {
            eprintln!("[dec-console] Agent 网关地址失败: {err}");
            return;
        }
    };
    let token = Uuid::new_v4().simple().to_string() + &Uuid::new_v4().simple().to_string();
    if let Err(err) = write_metadata(&format!("127.0.0.1:{}", addr.port()), &token) {
        eprintln!("[dec-console] 写入 console.json 失败: {err}");
        return;
    }
    loop {
        match listener.accept().await {
            Ok((stream, _)) => {
                let app = app.clone();
                let token = token.clone();
                tokio::spawn(async move {
                    if let Err(err) = handle_connection(stream, app, &token).await {
                        log::warn!("agent gateway: {err}");
                    }
                });
            }
            Err(err) => {
                log::warn!("agent gateway accept: {err}");
                tokio::time::sleep(Duration::from_millis(50)).await;
            }
        }
    }
}

pub fn remove_metadata() {
    if !METADATA_WRITTEN.swap(false, Ordering::SeqCst) {
        return;
    }
    let path = metadata_path();
    let _ = fs::remove_file(path);
}

fn metadata_path() -> PathBuf {
    crate::dec_home().join("run").join("console.json")
}

fn write_metadata(endpoint: &str, token: &str) -> Result<(), String> {
    let dir = crate::dec_home().join("run");
    fs::create_dir_all(&dir).map_err(|e| e.to_string())?;
    let body = serde_json::to_vec(&ConsoleMetadata {
        version: METADATA_VERSION,
        endpoint: endpoint.to_string(),
        token: token.to_string(),
        pid: std::process::id(),
    })
    .map_err(|e| e.to_string())?;
    let tmp = dir.join(format!("console-{}.json", std::process::id()));
    {
        let mut file = fs::File::create(&tmp).map_err(|e| e.to_string())?;
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            let mut perms = file.metadata().map_err(|e| e.to_string())?.permissions();
            perms.set_mode(0o600);
            file.set_permissions(perms).map_err(|e| e.to_string())?;
        }
        file.write_all(&body).map_err(|e| e.to_string())?;
    }
    let path = metadata_path();
    let _ = fs::remove_file(&path);
    fs::rename(&tmp, &path).map_err(|e| e.to_string())?;
    METADATA_WRITTEN.store(true, Ordering::SeqCst);
    Ok(())
}

fn is_blocked_path(path: &str) -> bool {
    matches!(
        path.trim_end_matches('/'),
        "/agent/authenticate" | "/authenticate"
    )
}

fn is_blocked_method(method: &str) -> bool {
    method.eq_ignore_ascii_case("authenticate")
}

fn token_from_headers(headers: &[(String, String)]) -> Option<String> {
    for (name, value) in headers {
        if name.eq_ignore_ascii_case("authorization") {
            let value = value.trim();
            let prefix = "Bearer ";
            if value.len() > prefix.len() && value[..prefix.len()].eq_ignore_ascii_case(prefix) {
                return Some(value[prefix.len()..].trim().to_string());
            }
        }
        if name.eq_ignore_ascii_case("x-dec-agent-token") {
            return Some(value.trim().to_string());
        }
    }
    None
}

fn client_id_from_headers(headers: &[(String, String)]) -> String {
    headers
        .iter()
        .find(|(name, _)| name.eq_ignore_ascii_case("x-dec-client-id"))
        .map(|(_, value)| value.trim().to_string())
        .filter(|value| !value.is_empty())
        .unwrap_or_else(|| format!("mcp-{}", std::process::id()))
}

struct HttpRequest {
    method: String,
    path: String,
    headers: Vec<(String, String)>,
    body: Vec<u8>,
}

async fn handle_connection(
    mut stream: TcpStream,
    app: AppHandle,
    token: &str,
) -> Result<(), String> {
    let req = read_request(&mut stream).await?;
    let path = req.path.split('?').next().unwrap_or(&req.path).to_string();
    let query = req.path.split_once('?').map(|(_, q)| q.to_string());
    let status;
    let body;
    if token_from_headers(&req.headers).as_deref() != Some(token) {
        status = 401;
        body = serde_json::json!({"error":"未授权","code":"CONSOLE_AGENT_UNAUTHORIZED"});
    } else if is_blocked_path(&path) {
        status = 403;
        body = serde_json::json!({"error":"禁止经 Agent 网关认证","code":"CONSOLE_AUTH_FORBIDDEN"});
    } else {
        match dispatch(
            &app,
            &req.method,
            &path,
            query.as_deref(),
            &req.headers,
            &req.body,
        )
        .await
        {
            Ok(value) => {
                status = 200;
                body = value;
            }
            Err((code, err, extra)) => {
                status = code;
                let mut value = serde_json::json!({"error": err, "code": extra});
                if let Some(object) = value.as_object_mut() {
                    if extra.is_empty() {
                        object.remove("code");
                    }
                }
                body = value;
            }
        }
    }
    write_response(&mut stream, status, &body).await
}

async fn dispatch(
    app: &AppHandle,
    method: &str,
    path: &str,
    query: Option<&str>,
    headers: &[(String, String)],
    body: &[u8],
) -> Result<serde_json::Value, (u16, String, String)> {
    let client_id = client_id_from_headers(headers);
    match (method, path) {
        ("GET", "/agent/hello") => hello(app)
            .await
            .map_err(|e| (409, e, "CONSOLE_NOT_CONNECTED".into())),
        ("GET", "/agent/connections") => connections(app).map_err(|e| (500, e, String::new())),
        ("GET", "/agent/active_operation") => {
            let root = query_param(query, "project_root").unwrap_or_default();
            active_operation(app, root, &client_id)
                .await
                .map_err(|e| status_for_rpc(e))
        }
        ("POST", "/agent/invoke") => {
            let req: InvokeBody =
                serde_json::from_slice(body).map_err(|e| (400, e.to_string(), String::new()))?;
            if is_blocked_method(&req.method) {
                return Err((
                    403,
                    "禁止经 Agent 网关认证".into(),
                    "CONSOLE_AUTH_FORBIDDEN".into(),
                ));
            }
            invoke(app, req, &client_id).await.map_err(status_for_rpc)
        }
        ("POST", "/agent/run") => {
            let req: InvokeBody =
                serde_json::from_slice(body).map_err(|e| (400, e.to_string(), String::new()))?;
            if is_blocked_method(&req.operation) {
                return Err((
                    403,
                    "禁止经 Agent 网关认证".into(),
                    "CONSOLE_AUTH_FORBIDDEN".into(),
                ));
            }
            run(app, req, &client_id).await.map_err(status_for_rpc)
        }
        ("POST", "/agent/connect") => {
            let req: ConnectBody =
                serde_json::from_slice(body).map_err(|e| (400, e.to_string(), String::new()))?;
            connect(app, req).await.map_err(|e| (400, e, String::new()))
        }
        ("POST", "/agent/tool_call") => {
            let req: ToolCallBody =
                serde_json::from_slice(body).map_err(|e| (400, e.to_string(), String::new()))?;
            if is_blocked_method(&req.name) {
                return Err((
                    403,
                    "禁止经 Agent 网关认证".into(),
                    "CONSOLE_AUTH_FORBIDDEN".into(),
                ));
            }
            tool_call(app, req, &client_id)
                .await
                .map_err(status_for_rpc)
        }
        _ => Err((404, format!("未知路径 {path}"), String::new())),
    }
}

fn status_for_rpc(err: String) -> (u16, String, String) {
    if err.contains("尚未连接") {
        (409, err, "CONSOLE_NOT_CONNECTED".into())
    } else {
        (500, err, String::new())
    }
}

fn query_param(query: Option<&str>, key: &str) -> Option<String> {
    query?
        .split('&')
        .filter_map(|pair| pair.split_once('='))
        .find(|(k, _)| *k == key)
        .map(|(_, v)| urlencoding_decode(v))
}

fn urlencoding_decode(value: &str) -> String {
    let mut out = String::new();
    let bytes = value.as_bytes();
    let mut i = 0;
    while i < bytes.len() {
        match bytes[i] {
            b'+' => {
                out.push(' ');
                i += 1;
            }
            b'%' if i + 2 < bytes.len() => {
                let hex = &value[i + 1..i + 3];
                if let Ok(byte) = u8::from_str_radix(hex, 16) {
                    out.push(byte as char);
                    i += 3;
                } else {
                    out.push('%');
                    i += 1;
                }
            }
            c => {
                out.push(c as char);
                i += 1;
            }
        }
    }
    out
}

async fn hello(app: &AppHandle) -> Result<serde_json::Value, String> {
    let state = app.state::<crate::AppState>();
    let current = state.current.lock().await.clone();
    let connected = current.is_some() && state.session.lock().await.is_some();
    let mut unlocked = current.as_ref().map(|c| c.unlocked).unwrap_or(false);
    let mut version = current
        .as_ref()
        .map(|c| c.version.clone())
        .unwrap_or_default();
    if connected {
        let client = {
            let guard = state.session.lock().await;
            guard.as_ref().ok_or("尚未连接")?.client_clone()
        };
        if let Ok(ping) = crate::grpc::ping(client).await {
            unlocked = ping.unlocked;
            version = ping.version.clone();
            if let Some(current) = state.current.lock().await.as_mut() {
                current.unlocked = ping.unlocked;
                current.version = ping.version;
            }
        }
    }
    Ok(serde_json::json!({
        "version": env!("CARGO_PKG_VERSION"),
        "connected": connected,
        "unlocked": unlocked,
        "server_version": version,
        "connection": current,
    }))
}

fn connections(app: &AppHandle) -> Result<serde_json::Value, String> {
    let list = crate::read_saved_connections()?;
    let _ = app;
    Ok(serde_json::json!({ "connections": list }))
}

async fn invoke(
    app: &AppHandle,
    req: InvokeBody,
    client_id: &str,
) -> Result<serde_json::Value, String> {
    let client = rpc_client(app, client_id).await?;
    let plane = if req.workspace_plane.trim().is_empty() {
        "local".to_string()
    } else {
        req.workspace_plane
    };
    let payload = serde_json::to_vec(&req.payload).map_err(|e| e.to_string())?;
    let result = crate::grpc::invoke(
        client,
        req.method.clone(),
        req.project_root.clone(),
        plane,
        payload,
        MCP_UNLOCK_TIMEOUT_MS,
    )
    .await?;
    emit_events(app, &req.project_root, &req.method, &result.events);
    Ok(rpc_json(result))
}

async fn run(
    app: &AppHandle,
    req: InvokeBody,
    client_id: &str,
) -> Result<serde_json::Value, String> {
    let client = rpc_client(app, client_id).await?;
    let plane = if req.workspace_plane.trim().is_empty() {
        "local".to_string()
    } else {
        req.workspace_plane
    };
    let payload = serde_json::to_vec(&req.payload).map_err(|e| e.to_string())?;
    let operation = if req.operation.trim().is_empty() {
        req.method.clone()
    } else {
        req.operation.clone()
    };
    let project_root = req.project_root.clone();
    let app_clone = app.clone();
    let op_clone = operation.clone();
    let result = crate::grpc::run_operation(
        client,
        operation,
        project_root.clone(),
        plane,
        payload,
        client_id.to_string(),
        "mcp".into(),
        MCP_UNLOCK_TIMEOUT_MS,
        |event| emit_action(&app_clone, &project_root, &op_clone, event),
    )
    .await?;
    Ok(rpc_json(result))
}

async fn active_operation(
    app: &AppHandle,
    project_root: String,
    client_id: &str,
) -> Result<serde_json::Value, String> {
    let client = rpc_client(app, client_id).await?;
    crate::grpc::active_operation(client, project_root).await
}

async fn connect(app: &AppHandle, req: ConnectBody) -> Result<serde_json::Value, String> {
    let mut kind = req.kind;
    let mut host = req.host;
    let mut port = req.port;
    let mut ssh_host = req.ssh_host;
    let mut ssh_user = req.ssh_user;
    let mut tls = req.tls;
    let mut tls_server_name = req.tls_server_name;
    let mut saved_id = req.id.clone();
    if !req.id.trim().is_empty() {
        let list = crate::read_saved_connections()?;
        let conn = list
            .into_iter()
            .find(|c| c.id == req.id)
            .ok_or_else(|| format!("未找到连接 {}", req.id))?;
        kind = conn.kind;
        host = conn.host;
        port = conn.port;
        ssh_host = conn.ssh_host;
        ssh_user = conn.ssh_user;
        tls = conn.tls;
        tls_server_name = conn.tls_server_name;
        saved_id = conn.id;
    }
    if kind.trim().is_empty() {
        kind = "local".into();
    }
    let ping = connect_target_inner(
        app,
        kind,
        host,
        port,
        ssh_host,
        ssh_user,
        tls,
        tls_server_name,
        saved_id,
    )
    .await?;
    // 连接本机时会对齐运行时；顺带保证清单已写出，供壳 bootstrap 后重载。
    let _ = crate::agent_tools::refresh_agent_tools_manifest(app);
    hello(app).await.map(|mut value| {
        value["ping"] = serde_json::to_value(ping).unwrap_or(serde_json::Value::Null);
        value
    })
}

async fn tool_call(
    app: &AppHandle,
    req: ToolCallBody,
    client_id: &str,
) -> Result<serde_json::Value, String> {
    let _ = req.client_version;
    let cache = crate::agent_tools::ensure_agent_tools_cache(app)?;
    let owner = cache
        .owners
        .get(req.name.as_str())
        .cloned()
        .unwrap_or_else(|| {
            // bootstrap 单工具或清单尚未缓存时，按名字兜底。
            if matches!(
                req.name.as_str(),
                "dec_console_status" | "dec_list_connections" | "dec_connect"
            ) {
                "console".into()
            } else {
                "server".into()
            }
        });

    let (ok, result, error, events) = if owner == "console" {
        execute_console_tool(app, &req).await?
    } else {
        execute_server_tool(app, &req, client_id).await?
    };

    let manifest_version = crate::agent_tools::manifest_version_from_disk()
        .unwrap_or(cache.version);

    Ok(serde_json::json!({
        "ok": ok,
        "result": result,
        "error": error,
        "events": events,
        "manifest_version": manifest_version,
    }))
}

async fn execute_console_tool(
    app: &AppHandle,
    req: &ToolCallBody,
) -> Result<(bool, serde_json::Value, String, Vec<serde_json::Value>), String> {
    match req.name.as_str() {
        "dec_console_status" => {
            // 清单缺失（bootstrap）时才 dump；已有文件不覆盖，便于同版本改 schema 后壳重载。
            if crate::agent_tools::manifest_version_from_disk().is_none() {
                let _ = crate::agent_tools::refresh_agent_tools_manifest(app);
            }
            let value = hello(app).await?;
            Ok((true, value, String::new(), Vec::new()))
        }
        "dec_list_connections" => {
            let value = connections(app)?;
            Ok((true, value, String::new(), Vec::new()))
        }
        "dec_connect" => {
            let body: ConnectBody = serde_json::from_value(req.arguments.clone())
                .map_err(|e| format!("dec_connect 参数无效: {e}"))?;
            let value = connect(app, body).await?;
            Ok((true, value, String::new(), Vec::new()))
        }
        other => Err(format!("未知 Console 工具 {other}")),
    }
}

async fn execute_server_tool(
    app: &AppHandle,
    req: &ToolCallBody,
    client_id: &str,
) -> Result<(bool, serde_json::Value, String, Vec<serde_json::Value>), String> {
    let client = rpc_client(app, client_id).await?;
    let plan_payload = serde_json::json!({
        "Name": req.name,
        "Arguments": req.arguments,
    });
    let plan_raw = serde_json::to_vec(&plan_payload).map_err(|e| e.to_string())?;
    let plan_invoke = crate::grpc::invoke(
        client.clone(),
        "plan_agent_tool".into(),
        String::new(),
        "global".into(),
        plan_raw,
        MCP_UNLOCK_TIMEOUT_MS,
    )
    .await?;
    if !plan_invoke.error.is_empty() {
        return Ok((
            false,
            serde_json::Value::Null,
            plan_invoke.error,
            plan_invoke.events,
        ));
    }
    let plan: PlanResult = serde_json::from_str(&plan_invoke.result_json)
        .map_err(|e| format!("解析 plan_agent_tool 失败: {e}"))?;
    if !plan.error.is_empty() {
        return Ok((false, serde_json::Value::Null, plan.error, Vec::new()));
    }
    execute_plan(app, client_id, plan).await
}

async fn execute_plan(
    app: &AppHandle,
    client_id: &str,
    plan: PlanResult,
) -> Result<(bool, serde_json::Value, String, Vec<serde_json::Value>), String> {
    let mut all_events = Vec::new();
    match plan.shape.as_str() {
        "planes" => {
            let mut outcomes = Vec::new();
            let mut any_ok = false;
            for step in plan.steps {
                let plane = if step.plane.is_empty() {
                    "local".to_string()
                } else {
                    step.plane.clone()
                };
                if step.kind == "error" {
                    outcomes.push(serde_json::json!({
                        "plane": plane,
                        "ok": false,
                        "error": step.method,
                    }));
                    continue;
                }
                match run_step(app, client_id, &step, &mut all_events).await {
                    Ok(value) => {
                        any_ok = true;
                        outcomes.push(serde_json::json!({
                            "plane": plane,
                            "ok": true,
                            "result": value,
                        }));
                    }
                    Err(err) => {
                        outcomes.push(serde_json::json!({
                            "plane": plane,
                            "ok": false,
                            "error": err,
                        }));
                    }
                }
            }
            if !any_ok {
                return Ok((
                    false,
                    serde_json::json!({ "planes": outcomes }),
                    "两平面均失败，详见 planes[].error".into(),
                    all_events,
                ));
            }
            Ok((
                true,
                serde_json::json!({ "planes": outcomes }),
                String::new(),
                all_events,
            ))
        }
        "keyed" => {
            let mut out = serde_json::Map::new();
            for (k, v) in plan.envelope {
                out.insert(k, v);
            }
            for step in plan.steps {
                match run_step(app, client_id, &step, &mut all_events).await {
                    Ok(value) => {
                        let key = if step.key.is_empty() {
                            "result".into()
                        } else {
                            step.key
                        };
                        out.insert(key, value);
                    }
                    Err(err) => {
                        if step.optional {
                            continue;
                        }
                        return Ok((false, serde_json::Value::Null, err, all_events));
                    }
                }
            }
            Ok((true, serde_json::Value::Object(out), String::new(), all_events))
        }
        _ => {
            // single
            let Some(step) = plan.steps.into_iter().next() else {
                return Ok((true, serde_json::Value::Null, String::new(), all_events));
            };
            match run_step(app, client_id, &step, &mut all_events).await {
                Ok(value) => Ok((true, value, String::new(), all_events)),
                Err(err) => Ok((false, serde_json::Value::Null, err, all_events)),
            }
        }
    }
}

async fn run_step(
    app: &AppHandle,
    client_id: &str,
    step: &PlanStep,
    events: &mut Vec<serde_json::Value>,
) -> Result<serde_json::Value, String> {
    match step.kind.as_str() {
        "error" => Err(step.method.clone()),
        "console_active_operation" => {
            match active_operation(app, step.project_root.clone(), client_id).await {
                Ok(value) => Ok(value),
                Err(_err) if step.optional => Ok(serde_json::Value::Null),
                Err(err) => Err(err),
            }
        }
        "invoke" => {
            let client = rpc_client(app, client_id).await?;
            let plane = if step.plane.trim().is_empty() {
                "local".to_string()
            } else {
                step.plane.clone()
            };
            let payload = serde_json::to_vec(&step.payload).map_err(|e| e.to_string())?;
            let result = crate::grpc::invoke(
                client,
                step.method.clone(),
                step.project_root.clone(),
                plane,
                payload,
                MCP_UNLOCK_TIMEOUT_MS,
            )
            .await?;
            emit_events(app, &step.project_root, &step.method, &result.events);
            events.extend(result.events);
            if !result.error.is_empty() {
                return Err(result.error);
            }
            Ok(parse_result_json(&result.result_json))
        }
        "run" => {
            let client = rpc_client(app, client_id).await?;
            let plane = if step.plane.trim().is_empty() {
                "local".to_string()
            } else {
                step.plane.clone()
            };
            let payload = serde_json::to_vec(&step.payload).map_err(|e| e.to_string())?;
            let project_root = step.project_root.clone();
            let app_clone = app.clone();
            let op_clone = step.method.clone();
            let result = crate::grpc::run_operation(
                client,
                step.method.clone(),
                project_root.clone(),
                plane,
                payload,
                client_id.to_string(),
                "mcp".into(),
                MCP_UNLOCK_TIMEOUT_MS,
                |event| emit_action(&app_clone, &project_root, &op_clone, event),
            )
            .await?;
            events.extend(result.events.clone());
            if !result.error.is_empty() {
                return Err(result.error);
            }
            Ok(parse_result_json(&result.result_json))
        }
        other => Err(format!("未知计划步骤 kind {other}")),
    }
}

fn parse_result_json(raw: &str) -> serde_json::Value {
    if raw.trim().is_empty() {
        serde_json::Value::Null
    } else {
        serde_json::from_str(raw).unwrap_or(serde_json::Value::String(raw.to_string()))
    }
}

async fn rpc_client(app: &AppHandle, client_id: &str) -> Result<crate::grpc::Svc, String> {
    let state = app.state::<crate::AppState>();
    let guard = state.session.lock().await;
    let session = guard.as_ref().ok_or("尚未连接")?;
    Ok(session.client_for("mcp", client_id))
}

fn rpc_json(result: crate::grpc::InvokeResult) -> serde_json::Value {
    let parsed = if result.result_json.trim().is_empty() {
        serde_json::Value::Null
    } else {
        serde_json::from_str(&result.result_json)
            .unwrap_or(serde_json::Value::String(result.result_json.clone()))
    };
    serde_json::json!({
        "ok": result.error.is_empty(),
        "result": parsed,
        "error": result.error,
        "events": result.events,
    })
}

fn emit_events(app: &AppHandle, project_root: &str, operation: &str, events: &[serde_json::Value]) {
    for event in events {
        emit_action(app, project_root, operation, event.clone());
    }
}

fn emit_action(app: &AppHandle, project_root: &str, operation: &str, mut event: serde_json::Value) {
    if let Some(object) = event.as_object_mut() {
        object.insert("actionKey".into(), format!("mcp:{operation}").into());
        object.insert("projectRoot".into(), project_root.into());
        object.insert("operation".into(), operation.into());
    }
    let _ = app.emit("operation-event", event);
}

async fn read_request(stream: &mut TcpStream) -> Result<HttpRequest, String> {
    let mut buf = Vec::new();
    let mut tmp = [0u8; 2048];
    loop {
        let n = stream.read(&mut tmp).await.map_err(|e| e.to_string())?;
        if n == 0 {
            break;
        }
        buf.extend_from_slice(&tmp[..n]);
        if buf.len() > 8 * 1024 * 1024 {
            return Err("请求过大".into());
        }
        if let Some(pos) = find_header_end(&buf) {
            let (method, path, headers) = parse_headers(&buf[..pos])?;
            let content_length = headers
                .iter()
                .find(|(name, _)| name.eq_ignore_ascii_case("content-length"))
                .and_then(|(_, v)| v.trim().parse::<usize>().ok())
                .unwrap_or(0);
            let mut body = buf[pos + 4..].to_vec();
            while body.len() < content_length {
                let n = stream.read(&mut tmp).await.map_err(|e| e.to_string())?;
                if n == 0 {
                    break;
                }
                body.extend_from_slice(&tmp[..n]);
            }
            body.truncate(content_length);
            return Ok(HttpRequest {
                method,
                path,
                headers,
                body,
            });
        }
    }
    Err("不完整的 HTTP 请求".into())
}

fn find_header_end(buf: &[u8]) -> Option<usize> {
    buf.windows(4).position(|w| w == b"\r\n\r\n")
}

fn parse_headers(raw: &[u8]) -> Result<(String, String, Vec<(String, String)>), String> {
    let text = String::from_utf8_lossy(raw);
    let mut lines = text.split("\r\n");
    let request_line = lines.next().ok_or("空请求")?;
    let mut parts = request_line.split_whitespace();
    let method = parts.next().ok_or("缺少方法")?.to_string();
    let path = parts.next().ok_or("缺少路径")?.to_string();
    let mut headers = Vec::new();
    for line in lines {
        if line.is_empty() {
            continue;
        }
        if let Some((name, value)) = line.split_once(':') {
            headers.push((name.trim().to_string(), value.trim().to_string()));
        }
    }
    Ok((method, path, headers))
}

async fn write_response(
    stream: &mut TcpStream,
    status: u16,
    body: &serde_json::Value,
) -> Result<(), String> {
    let payload = serde_json::to_vec(body).map_err(|e| e.to_string())?;
    let reason = match status {
        200 => "OK",
        400 => "Bad Request",
        401 => "Unauthorized",
        403 => "Forbidden",
        404 => "Not Found",
        409 => "Conflict",
        _ => "Error",
    };
    let header = format!(
        "HTTP/1.1 {status} {reason}\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n",
        payload.len()
    );
    stream
        .write_all(header.as_bytes())
        .await
        .map_err(|e| e.to_string())?;
    stream
        .write_all(&payload)
        .await
        .map_err(|e| e.to_string())?;
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::{
        is_blocked_method, is_blocked_path, token_from_headers, write_metadata, ConsoleMetadata,
        PlanResult,
    };
    use std::fs;

    #[test]
    fn blocks_authenticate_surface() {
        assert!(is_blocked_path("/agent/authenticate"));
        assert!(is_blocked_method("authenticate"));
        assert!(is_blocked_method("Authenticate"));
        assert!(!is_blocked_method("invoke"));
        assert!(!is_blocked_path("/agent/hello"));
        assert!(!is_blocked_method("dec_set_requires"));
    }

    #[test]
    fn assemble_keyed_merges_envelope() {
        let mut envelope = serde_json::Map::new();
        envelope.insert("plane".into(), serde_json::json!("local"));
        let plan = PlanResult {
            name: "dec_status".into(),
            owner: "server".into(),
            shape: "keyed".into(),
            steps: vec![],
            envelope,
            error: String::new(),
        };
        assert_eq!(plan.shape, "keyed");
        assert_eq!(plan.envelope.get("plane").and_then(|v| v.as_str()), Some("local"));
    }

    #[test]
    fn reads_bearer_token() {
        let headers = vec![
            ("Host".into(), "127.0.0.1".into()),
            ("Authorization".into(), "Bearer secret-token".into()),
        ];
        assert_eq!(
            token_from_headers(&headers).as_deref(),
            Some("secret-token")
        );
    }

    #[test]
    fn writes_console_json() {
        let dir = std::env::temp_dir().join(format!("dec-console-agent-{}", std::process::id()));
        let _ = fs::remove_dir_all(&dir);
        fs::create_dir_all(&dir).unwrap();
        let old = std::env::var("DEC_HOME").ok();
        unsafe { std::env::set_var("DEC_HOME", &dir) };
        write_metadata("127.0.0.1:9", "tok").expect("write");
        let data = fs::read_to_string(dir.join("run").join("console.json")).expect("read");
        let meta: ConsoleMetadata = serde_json::from_str(&data).expect("json");
        assert_eq!(meta.endpoint, "127.0.0.1:9");
        assert_eq!(meta.token, "tok");
        match old {
            Some(v) => unsafe { std::env::set_var("DEC_HOME", v) },
            None => unsafe { std::env::remove_var("DEC_HOME") },
        }
        let _ = fs::remove_dir_all(dir);
    }
}
