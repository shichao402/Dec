use base64::engine::general_purpose::STANDARD as BASE64;
use base64::Engine;
use relkit_updater::proto::check_result;
use relkit_updater::proto::download_result;
use relkit_updater::proto::{
    CheckPolicy, ClientProfile, InstallSpec, Layout, RecoveryHelp, RecoveryLink, Runtime,
    TrustedKey,
};
use relkit_updater::{check_result_to_json, OpenResult, Updater};
use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::path::{Path, PathBuf};
use std::process::Stdio;
use tauri::{AppHandle, Manager};

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ConsoleUpdateEnvelope {
    pub current_version: String,
    pub can_auto_install: bool,
    pub result: serde_json::Value,
    pub status: serde_json::Value,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
struct RelkitConfig {
    product: String,
    default_channel: String,
    channels: Vec<String>,
    signing: SigningConfig,
    directory: DirectoryConfig,
    recovery: RecoveryConfig,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
struct SigningConfig {
    public_keys: Vec<PublicKeyConfig>,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
struct PublicKeyConfig {
    key_id: String,
    public_key_base64: String,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
struct DirectoryConfig {
    entry_urls: Vec<String>,
}

#[derive(Debug, Deserialize)]
struct RecoveryConfig {
    message: String,
    links: Vec<RecoveryLinkConfig>,
}

#[derive(Debug, Deserialize)]
struct RecoveryLinkConfig {
    label: String,
    url: String,
}

fn platform_id() -> &'static str {
    if cfg!(all(target_os = "windows", target_arch = "x86_64")) {
        "windows-amd64"
    } else if cfg!(all(target_os = "macos", target_arch = "x86_64")) {
        "darwin-amd64"
    } else if cfg!(all(target_os = "macos", target_arch = "aarch64")) {
        "darwin-arm64"
    } else if cfg!(target_arch = "aarch64") {
        "linux-arm64"
    } else {
        "linux-amd64"
    }
}

fn sidecar_path(app: &AppHandle) -> Result<PathBuf, String> {
    let name = if cfg!(windows) {
        "relkit-updater.exe"
    } else {
        "relkit-updater"
    };
    app.path()
        .resource_dir()
        .map_err(|err| format!("定位 Console resources 失败: {err}"))
        .map(|root| {
            root.join("resources")
                .join("updater")
                .join(platform_id())
                .join(name)
        })
}

fn updater_data_dir(app: &AppHandle) -> Result<PathBuf, String> {
    app.path()
        .app_data_dir()
        .map(|path| path.join("updater"))
        .map_err(|err| format!("定位 Console 数据目录失败: {err}"))
}

fn version_code(version: &str) -> Result<i64, String> {
    let core = version
        .trim()
        .trim_start_matches('v')
        .split(['-', '+'])
        .next()
        .unwrap_or_default();
    let parts = core
        .split('.')
        .map(|part| part.parse::<i64>())
        .collect::<Result<Vec<_>, _>>()
        .map_err(|_| format!("无效 Console 版本 {version:?}"))?;
    if parts.len() != 3 || parts.iter().any(|part| *part < 0 || *part > 999) {
        return Err(format!("无效 Console 版本 {version:?}"));
    }
    Ok(parts[0] * 1_000_000 + parts[1] * 1_000 + parts[2])
}

fn updater(app: &AppHandle, current: &str) -> Result<Updater, String> {
    let config: RelkitConfig = serde_json::from_str(include_str!("../../../relkit.json"))
        .map_err(|err| format!("解析内置更新配置失败: {err}"))?;
    let trusted_keys = config
        .signing
        .public_keys
        .into_iter()
        .map(|key| {
            BASE64
                .decode(&key.public_key_base64)
                .map(|public_key| TrustedKey {
                    key_id: key.key_id,
                    public_key,
                })
                .map_err(|err| format!("解析更新公钥失败: {err}"))
        })
        .collect::<Result<Vec<_>, _>>()?;
    let recovery = RecoveryHelp {
        message: config.recovery.message,
        links: config
            .recovery
            .links
            .into_iter()
            .map(|link| RecoveryLink {
                label: link.label,
                url: link.url,
            })
            .collect(),
    };
    let sidecar = sidecar_path(app)?;
    if !sidecar.is_file() {
        return Err(format!("Console 更新组件不存在：{}", sidecar.display()));
    }
    let data_dir = updater_data_dir(app)?;
    let executable =
        std::env::current_exe().map_err(|err| format!("定位 Console 可执行文件失败: {err}"))?;
    let install_root = executable
        .parent()
        .unwrap_or_else(|| Path::new("."))
        .to_string_lossy()
        .into_owned();
    let profile = ClientProfile {
        product: config.product,
        allowed_channels: config.channels,
        entry_urls: config.directory.entry_urls,
        index_urls: Vec::new(),
        fallback_urls: Vec::new(),
        trusted_keys,
        recovery: Some(recovery),
    };
    let runtime = Runtime {
        channel: config.default_channel,
        current_code: version_code(current)?,
        client_selectors: HashMap::from([
            ("os".into(), std::env::consts::OS.into()),
            ("arch".into(), normalized_arch().into()),
            ("component".into(), "console".into()),
            ("audience".into(), "user".into()),
        ]),
        data_dir: data_dir.to_string_lossy().into_owned(),
        install: Some(InstallSpec {
            layout: Layout::WholeRoot as i32,
            install_root,
            executable_relpath: executable
                .file_name()
                .unwrap_or_default()
                .to_string_lossy()
                .into_owned(),
            sidecar_relpath: sidecar
                .file_name()
                .unwrap_or_default()
                .to_string_lossy()
                .into_owned(),
            preserve: Vec::new(),
            retain: 0,
            relaunch: true,
            file_set: Vec::new(),
        }),
        sidecar_path: sidecar.to_string_lossy().into_owned(),
    };
    match Updater::open(profile, runtime) {
        OpenResult::Opened { updater, .. } => Ok(*updater),
        OpenResult::Failed(error) => Err(error_message(&error)),
    }
}

fn normalized_arch() -> &'static str {
    match std::env::consts::ARCH {
        "x86_64" => "amd64",
        "aarch64" => "arm64",
        value => value,
    }
}

fn error_message(error: &relkit_updater::proto::Error) -> String {
    if error.message.trim().is_empty() {
        "更新引擎返回未知错误".into()
    } else {
        error.message.clone()
    }
}

struct Checked {
    envelope: ConsoleUpdateEnvelope,
    plan_id: Option<String>,
    artifact_name: Option<String>,
}

fn check_sync(app: &AppHandle, current: &str, force: bool) -> Result<Checked, String> {
    let updater = updater(app, current)?;
    let policy = CheckPolicy {
        after_success: Some(pbjson_types::Duration {
            seconds: 24 * 60 * 60,
            nanos: 0,
        }),
        after_failure: Some(pbjson_types::Duration {
            seconds: 60 * 60,
            nanos: 0,
        }),
    };
    let result = updater.check(force, 0, Some(policy));
    let status = updater.status();
    let result_json = check_result_to_json(&result)
        .map_err(|err| format!("序列化更新检查结果失败: {err}"))
        .and_then(|json| {
            serde_json::from_str(&json).map_err(|err| format!("解析更新检查结果失败: {err}"))
        })?;
    let status_json =
        serde_json::to_value(status).map_err(|err| format!("序列化更新状态失败: {err}"))?;
    let envelope = ConsoleUpdateEnvelope {
        current_version: current.to_string(),
        can_auto_install: cfg!(windows),
        result: result_json,
        status: status_json,
    };
    match &result.kind {
        Some(check_result::Kind::UpdateAvailable(available)) => {
            let artifact = available
                .artifacts
                .first()
                .map(|value| value.name.clone())
                .filter(|value| !value.is_empty());
            Ok(Checked {
                envelope,
                plan_id: Some(available.plan_id.clone()),
                artifact_name: artifact,
            })
        }
        _ => Ok(Checked {
            envelope,
            plan_id: None,
            artifact_name: None,
        }),
    }
}

pub async fn check(
    app: &AppHandle,
    current: &str,
    force: bool,
) -> Result<ConsoleUpdateEnvelope, String> {
    check_sync(app, current, force).map(|checked| checked.envelope)
}

pub async fn install(app: &AppHandle, current: &str) -> Result<ConsoleUpdateEnvelope, String> {
    let checked = check_sync(app, current, true)?;
    let plan_id = checked
        .plan_id
        .ok_or_else(|| result_error(&checked.envelope.result, current))?;
    let artifact_name = checked.artifact_name.ok_or("更新包不可用")?;
    let updater = updater(app, current)?;
    let download = updater.download(plan_id.clone(), |_| {});
    if let Some(download_result::Kind::Failed(failed)) = download.kind {
        return Err(failed
            .error
            .as_ref()
            .map(error_message)
            .unwrap_or_else(|| "下载 Console 更新失败".into()));
    }
    let package = updater_data_dir(app)?
        .join("staging")
        .join(plan_id)
        .join(artifact_name);
    if !package.is_file() {
        return Err(format!("下载完成但更新包不存在：{}", package.display()));
    }
    spawn_installer(app, &package)?;
    if checked.envelope.can_auto_install {
        app.exit(0);
    }
    Ok(checked.envelope)
}

fn result_error(result: &serde_json::Value, current: &str) -> String {
    result
        .get("failed")
        .and_then(|failed| failed.get("error"))
        .and_then(|error| error.get("message"))
        .and_then(serde_json::Value::as_str)
        .filter(|message| !message.is_empty())
        .map(ToOwned::to_owned)
        .or_else(|| {
            result
                .get("fallbackRequired")
                .and_then(|fallback| fallback.get("message"))
                .and_then(serde_json::Value::as_str)
                .filter(|message| !message.is_empty())
                .map(ToOwned::to_owned)
        })
        .unwrap_or_else(|| format!("当前已是最新版本 {current}"))
}

fn detached(command: &mut std::process::Command) {
    command
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null());
    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        const CREATE_NEW_PROCESS_GROUP: u32 = 0x00000200;
        const DETACHED_PROCESS: u32 = 0x00000008;
        command.creation_flags(CREATE_NEW_PROCESS_GROUP | DETACHED_PROCESS);
    }
}

fn spawn_installer(app: &AppHandle, package: &Path) -> Result<(), String> {
    #[cfg(windows)]
    {
        let apply_dir = updater_data_dir(app)?.join("apply");
        std::fs::create_dir_all(&apply_dir).map_err(|err| format!("创建更新目录失败: {err}"))?;
        let script = apply_dir.join("install-console.ps1");
        let quote = |value: &Path| value.to_string_lossy().replace('\'', "''");
        let relaunch =
            std::env::current_exe().map_err(|err| format!("定位 Console 可执行文件失败: {err}"))?;
        std::fs::write(
            &script,
            format!(
                "Start-Sleep -Milliseconds 1500\n\
                 $p = Start-Process -FilePath '{}' -ArgumentList '/S' -Wait -PassThru\n\
                 if ($p.ExitCode -eq 0) {{ Start-Process -FilePath '{}' }}\n",
                quote(package),
                quote(&relaunch),
            ),
        )
        .map_err(|err| format!("准备安装脚本失败: {err}"))?;
        let mut command = crate::proc::command("powershell");
        command.args([
            "-NoProfile",
            "-ExecutionPolicy",
            "Bypass",
            "-WindowStyle",
            "Hidden",
            "-File",
            &script.to_string_lossy(),
        ]);
        detached(&mut command);
        command
            .spawn()
            .map_err(|err| format!("启动安装程序失败: {err}"))?;
        return Ok(());
    }
    #[cfg(target_os = "macos")]
    {
        let mut command = crate::proc::command("open");
        command.arg(package);
        detached(&mut command);
        command
            .spawn()
            .map_err(|err| format!("打开安装包失败: {err}"))?;
        return Ok(());
    }
    #[cfg(all(not(windows), not(target_os = "macos")))]
    {
        let mut command = crate::proc::command(package);
        detached(&mut command);
        command
            .spawn()
            .map_err(|err| format!("打开安装包失败: {err}"))?;
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::version_code;

    #[test]
    fn semver_code_matches_relkit_strategy() {
        assert_eq!(version_code("v1.13.72").unwrap(), 1_013_072);
        assert_eq!(version_code("1.13.72+build").unwrap(), 1_013_072);
    }

    #[test]
    fn semver_code_rejects_invalid_versions() {
        assert!(version_code("1.13").is_err());
        assert!(version_code("1.1000.0").is_err());
        assert!(version_code("latest").is_err());
    }
}
