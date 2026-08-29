pub mod service {
    pub mod v1 {
        tonic::include_proto!("service.v1");
    }
}

use self::service::v1::dec_service_client::DecServiceClient;
use self::service::v1::{
    AuthenticateRequest, GetActiveOperationRequest, InvokeRequest, PingRequest,
    RunOperationRequest,
};
use serde::{Deserialize, Serialize};
use std::process::Child;
use std::sync::{Arc, Mutex};
use std::time::Duration;
use tonic::service::Interceptor;
use tonic::transport::{Channel, Endpoint};
use tonic::{Request, Status};

#[derive(Clone)]
pub struct TokenInterceptor {
    pub token: Arc<Mutex<String>>,
}

impl Interceptor for TokenInterceptor {
    fn call(&mut self, mut request: Request<()>) -> Result<Request<()>, Status> {
        let token = self.token.lock().map_err(|_| Status::internal("token lock"))?;
        if !token.is_empty() {
            let value = token
                .parse()
                .map_err(|_| Status::internal("invalid token metadata"))?;
            request.metadata_mut().insert("x-dec-token", value);
        }
        request
            .metadata_mut()
            .insert("x-dec-facade", "web".parse().unwrap());
        request
            .metadata_mut()
            .insert("x-dec-client-id", "dec-console".parse().unwrap());
        Ok(request)
    }
}

pub type Svc = DecServiceClient<tonic::service::interceptor::InterceptedService<Channel, TokenInterceptor>>;

pub struct Session {
    pub interceptor: TokenInterceptor,
    pub client: Svc,
    #[allow(dead_code)]
    pub endpoint: String,
    pub ssh: Option<Child>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct PingInfo {
    pub version: String,
    pub instance_id: String,
    pub unlocked: bool,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct AuthResult {
    pub unlocked: bool,
    pub need_2fa: bool,
    pub control_token: String,
    pub expires_in_ms: i64,
    pub error: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct InvokeResult {
    pub result_json: String,
    pub error: String,
}

pub async fn connect_channel(endpoint: &str, token: &str) -> Result<Session, String> {
    let url = if endpoint.starts_with("http://") || endpoint.starts_with("https://") {
        endpoint.to_string()
    } else {
        format!("http://{endpoint}")
    };
    let channel = Endpoint::from_shared(url.clone())
        .map_err(|e| e.to_string())?
        .connect_timeout(Duration::from_secs(5))
        .connect()
        .await
        .map_err(|e| e.to_string())?;
    let interceptor = TokenInterceptor {
        token: Arc::new(Mutex::new(token.to_string())),
    };
    let client = DecServiceClient::with_interceptor(channel, interceptor.clone());
    Ok(Session {
        interceptor,
        client,
        endpoint: endpoint.to_string(),
        ssh: None,
    })
}

impl Session {
    pub async fn ping(&mut self) -> Result<PingInfo, String> {
        let resp = self
            .client
            .ping(Request::new(PingRequest {}))
            .await
            .map_err(|e| e.to_string())?
            .into_inner();
        Ok(PingInfo {
            version: resp.version,
            instance_id: resp.instance_id,
            unlocked: resp.unlocked,
        })
    }

    pub async fn authenticate(
        &mut self,
        email: String,
        password: String,
        totp: String,
        remember_device: bool,
    ) -> Result<AuthResult, String> {
        let resp = self
            .client
            .authenticate(Request::new(AuthenticateRequest {
                password,
                totp,
                remember_device,
                email,
            }))
            .await
            .map_err(|e| e.to_string())?
            .into_inner();
        if resp.unlocked && !resp.control_token.is_empty() {
            if let Ok(mut token) = self.interceptor.token.lock() {
                *token = resp.control_token.clone();
            }
        }
        Ok(AuthResult {
            unlocked: resp.unlocked,
            need_2fa: resp.need_2fa,
            control_token: resp.control_token,
            expires_in_ms: resp.expires_in_ms,
            error: resp.error,
        })
    }

    pub async fn invoke(
        &mut self,
        method: String,
        project_root: String,
        workspace_plane: String,
        payload_json: Vec<u8>,
    ) -> Result<InvokeResult, String> {
        let resp = self
            .client
            .invoke(Request::new(InvokeRequest {
                method,
                project_root,
                payload_json,
                unlock_timeout_ms: 0,
                workspace_plane,
            }))
            .await
            .map_err(|e| e.to_string())?
            .into_inner();
        Ok(InvokeResult {
            result_json: String::from_utf8_lossy(&resp.result_json).into_owned(),
            error: String::new(),
        })
    }

    pub async fn run_operation(
        &mut self,
        operation: String,
        project_root: String,
        workspace_plane: String,
        payload_json: Vec<u8>,
        mut on_event: impl FnMut(serde_json::Value),
    ) -> Result<InvokeResult, String> {
        let mut stream = self
            .client
            .run_operation(Request::new(RunOperationRequest {
                operation,
                project_root,
                client_id: "dec-console".into(),
                facade: "web".into(),
                payload_json,
                unlock_timeout_ms: 0,
                workspace_plane,
            }))
            .await
            .map_err(|e| e.to_string())?
            .into_inner();
        let mut last = InvokeResult {
            result_json: String::new(),
            error: String::new(),
        };
        while let Some(msg) = stream.message().await.map_err(|e| e.to_string())? {
            if let Some(event) = msg.event {
                on_event(serde_json::json!({
                    "level": event.level,
                    "scope": event.scope,
                    "message": event.message,
                    "timeUnixMs": event.time_unix_ms,
                }));
            }
            if msg.done {
                last.result_json = String::from_utf8_lossy(&msg.result_json).into_owned();
                last.error = msg.error;
            }
        }
        Ok(last)
    }

    pub async fn active_operation(&mut self, project_root: String) -> Result<serde_json::Value, String> {
        let resp = self
            .client
            .get_active_operation(Request::new(GetActiveOperationRequest { project_root }))
            .await
            .map_err(|e| e.to_string())?
            .into_inner();
        match resp.operation {
            None => Ok(serde_json::json!({ "active": false })),
            Some(op) => Ok(serde_json::json!({
                "active": op.active,
                "operationId": op.operation_id,
                "operation": op.operation,
                "facade": op.facade,
            })),
        }
    }
}

pub fn read_local_metadata() -> Result<(String, String), String> {
    let home = std::env::var("DEC_HOME").unwrap_or_else(|_| {
        dirs::home_dir()
            .map(|p| p.join(".dec").to_string_lossy().into_owned())
            .unwrap_or_else(|| ".dec".into())
    });
    let path = std::path::Path::new(&home).join("run").join("server.json");
    let data = std::fs::read_to_string(&path).map_err(|e| format!("读取 {path:?} 失败: {e}"))?;
    let value: serde_json::Value =
        serde_json::from_str(&data).map_err(|e| format!("解析 server.json 失败: {e}"))?;
    let endpoint = value
        .get("endpoint")
        .and_then(|v| v.as_str())
        .ok_or("server.json 缺少 endpoint")?
        .to_string();
    let token = value
        .get("token")
        .and_then(|v| v.as_str())
        .unwrap_or("")
        .to_string();
    Ok((endpoint, token))
}

#[cfg(test)]
mod tests {
    use super::read_local_metadata;
    use std::fs;

    #[test]
    fn reads_server_json() {
        let dir = std::env::temp_dir().join(format!("dec-console-test-{}", std::process::id()));
        let run = dir.join("run");
        fs::create_dir_all(&run).unwrap();
        fs::write(
            run.join("server.json"),
            r#"{"version":1,"endpoint":"127.0.0.1:9","token":"abc","pid":1}"#,
        )
        .unwrap();
        let old = std::env::var("DEC_HOME").ok();
        unsafe { std::env::set_var("DEC_HOME", &dir) };
        let (endpoint, token) = read_local_metadata().expect("metadata");
        assert_eq!(endpoint, "127.0.0.1:9");
        assert_eq!(token, "abc");
        match old {
            Some(v) => unsafe { std::env::set_var("DEC_HOME", v) },
            None => unsafe { std::env::remove_var("DEC_HOME") },
        }
        let _ = fs::remove_dir_all(dir);
    }
}

pub fn spawn_local_server() -> Result<(), String> {
    let exe = if cfg!(windows) {
        "dec-server.exe"
    } else {
        "dec-server"
    };
    let mut cmd = std::process::Command::new(exe);
    cmd.stdin(std::process::Stdio::null())
        .stdout(std::process::Stdio::null())
        .stderr(std::process::Stdio::null());
    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        const CREATE_NEW_PROCESS_GROUP: u32 = 0x00000200;
        const DETACHED_PROCESS: u32 = 0x00000008;
        cmd.creation_flags(CREATE_NEW_PROCESS_GROUP | DETACHED_PROCESS);
    }
    cmd.spawn().map_err(|e| format!("拉起 {exe} 失败: {e}"))?;
    Ok(())
}
