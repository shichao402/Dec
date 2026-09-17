use serde::Deserialize;
use std::collections::HashMap;
use std::fs;
use std::io::Write;
use std::path::PathBuf;
use std::sync::Mutex as StdMutex;
use tauri::{AppHandle, Manager};

#[derive(Debug, Clone, Default)]
pub(crate) struct AgentToolsCache {
    pub version: String,
    pub owners: HashMap<String, String>,
}

#[derive(Debug, Deserialize)]
struct ManifestFile {
    protocol: u32,
    version: String,
    tools: Vec<ManifestTool>,
}

#[derive(Debug, Deserialize)]
struct ManifestTool {
    name: String,
    owner: String,
}

const AGENT_TOOLS_PROTOCOL: u32 = 1;

pub(crate) fn agent_tools_path() -> PathBuf {
    crate::dec_home().join("run").join("agent-tools.json")
}

pub(crate) fn refresh_agent_tools_manifest(
    app: &AppHandle,
) -> Result<AgentToolsCache, String> {
    let server = crate::suite_binary("dec-server");
    if !server.is_file() {
        return Err(format!("找不到 {}", server.display()));
    }
    let output = crate::proc::command(&server)
        .arg("--dump-agent-tools")
        .output()
        .map_err(|e| format!("执行 dec-server --dump-agent-tools 失败: {e}"))?;
    if !output.status.success() {
        let stderr = String::from_utf8_lossy(&output.stderr);
        return Err(format!(
            "dec-server --dump-agent-tools 失败: {}",
            stderr.trim()
        ));
    }
    let raw = output.stdout;
    let manifest: ManifestFile =
        serde_json::from_slice(&raw).map_err(|e| format!("解析 agent-tools 清单失败: {e}"))?;
    if manifest.protocol != AGENT_TOOLS_PROTOCOL {
        return Err(format!(
            "agent-tools protocol {} 不受支持（Console 支持 {AGENT_TOOLS_PROTOCOL}）",
            manifest.protocol
        ));
    }
    let dir = crate::dec_home().join("run");
    fs::create_dir_all(&dir).map_err(|e| e.to_string())?;
    let tmp = dir.join(format!("agent-tools-{}.json", std::process::id()));
    {
        let mut file = fs::File::create(&tmp).map_err(|e| e.to_string())?;
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            let mut perms = file.metadata().map_err(|e| e.to_string())?.permissions();
            perms.set_mode(0o600);
            file.set_permissions(perms).map_err(|e| e.to_string())?;
        }
        file.write_all(&raw).map_err(|e| e.to_string())?;
    }
    let path = agent_tools_path();
    let _ = fs::remove_file(&path);
    fs::rename(&tmp, &path).map_err(|e| e.to_string())?;

    let mut owners = HashMap::new();
    for tool in &manifest.tools {
        owners.insert(tool.name.clone(), tool.owner.clone());
    }
    let cache = AgentToolsCache {
        version: manifest.version,
        owners,
    };
    let state = app.state::<crate::AppState>();
    *state.agent_tools.lock().unwrap_or_else(|e| e.into_inner()) = Some(cache.clone());
    Ok(cache)
}

pub(crate) fn ensure_agent_tools_cache(app: &AppHandle) -> Result<AgentToolsCache, String> {
    let cached = {
        let state = app.state::<crate::AppState>();
        let guard = state.agent_tools.lock().unwrap_or_else(|e| e.into_inner());
        guard.clone()
    };
    if let Some(cache) = cached {
        if !cache.version.is_empty() {
            return Ok(cache);
        }
    }
    if let Ok(raw) = fs::read(agent_tools_path()) {
        if let Ok(manifest) = serde_json::from_slice::<ManifestFile>(&raw) {
            if manifest.protocol == AGENT_TOOLS_PROTOCOL {
                let mut owners = HashMap::new();
                for tool in &manifest.tools {
                    owners.insert(tool.name.clone(), tool.owner.clone());
                }
                let cache = AgentToolsCache {
                    version: manifest.version,
                    owners,
                };
                let state = app.state::<crate::AppState>();
                *state.agent_tools.lock().unwrap_or_else(|e| e.into_inner()) = Some(cache.clone());
                return Ok(cache);
            }
        }
    }
    refresh_agent_tools_manifest(app)
}

pub(crate) fn manifest_version_from_disk() -> Option<String> {
    let raw = fs::read(agent_tools_path()).ok()?;
    let manifest: ManifestFile = serde_json::from_slice(&raw).ok()?;
    if manifest.protocol != AGENT_TOOLS_PROTOCOL {
        return None;
    }
    Some(manifest.version)
}

pub(crate) fn agent_tools_version(app: &AppHandle) -> String {
    if let Some(version) = manifest_version_from_disk() {
        return version;
    }
    ensure_agent_tools_cache(app)
        .map(|c| c.version)
        .unwrap_or_default()
}

pub(crate) type AgentToolsState = StdMutex<Option<AgentToolsCache>>;
