use serde::{Deserialize, Serialize};
use std::path::PathBuf;
use std::process::Stdio;
use tauri::{AppHandle, Manager};
use tokio::process::Command;

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ConsoleUpdateStatus {
    pub current_version: String,
    pub latest_version: String,
    pub need_update: bool,
    pub mandatory: bool,
    pub release_notes_markdown: String,
    pub release_notes_url: String,
    pub checked_at: String,
    pub from_cache: bool,
    pub auto_check_interval: String,
    pub can_auto_install: bool,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
struct DownloadResult {
    package_path: String,
    status: ConsoleUpdateStatus,
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

fn helper_path(app: &AppHandle) -> Result<PathBuf, String> {
    let name = if cfg!(windows) {
        "dec-console-updater.exe"
    } else {
        "dec-console-updater"
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

fn data_dir(app: &AppHandle) -> Result<PathBuf, String> {
    app.path()
        .app_data_dir()
        .map(|path| path.join("updater"))
        .map_err(|err| format!("定位 Console 数据目录失败: {err}"))
}

fn helper_command(path: &PathBuf) -> Command {
    let mut command = Command::from(crate::proc::command(path));
    command.stdin(Stdio::null()).stderr(Stdio::piped());
    command
}

async fn run_helper(app: &AppHandle, args: &[String]) -> Result<Vec<u8>, String> {
    let path = helper_path(app)?;
    if !path.is_file() {
        return Err(format!("Console 更新组件不存在：{}", path.display()));
    }
    let output = helper_command(&path)
        .args(args)
        .output()
        .await
        .map_err(|err| format!("启动 Console 更新组件失败: {err}"))?;
    if !output.status.success() {
        let message = String::from_utf8_lossy(&output.stderr).trim().to_string();
        return Err(if message.is_empty() {
            format!("Console 更新组件退出：{}", output.status)
        } else {
            message
        });
    }
    Ok(output.stdout)
}

pub async fn check(
    app: &AppHandle,
    current: &str,
    force: bool,
) -> Result<ConsoleUpdateStatus, String> {
    let mut args = vec![
        "check".into(),
        "--current".into(),
        current.into(),
        "--data-dir".into(),
        data_dir(app)?.to_string_lossy().into_owned(),
    ];
    if force {
        args.push("--force".into());
    }
    let output = run_helper(app, &args).await?;
    serde_json::from_slice(&output).map_err(|err| format!("解析更新检查结果失败: {err}"))
}

pub async fn install(app: &AppHandle, current: &str) -> Result<ConsoleUpdateStatus, String> {
    let updater_data = data_dir(app)?;
    let output = run_helper(
        app,
        &[
            "download".into(),
            "--current".into(),
            current.into(),
            "--data-dir".into(),
            updater_data.to_string_lossy().into_owned(),
        ],
    )
    .await?;
    let download: DownloadResult =
        serde_json::from_slice(&output).map_err(|err| format!("解析更新下载结果失败: {err}"))?;

    let source = helper_path(app)?;
    let apply_dir = updater_data.join("apply");
    std::fs::create_dir_all(&apply_dir).map_err(|err| format!("创建更新目录失败: {err}"))?;
    let helper_name = source.file_name().ok_or("Console 更新组件路径无效")?;
    let detached_helper = apply_dir.join(helper_name);
    std::fs::copy(&source, &detached_helper)
        .map_err(|err| format!("准备独立更新组件失败: {err}"))?;

    let mut command = crate::proc::command(detached_helper);
    command
        .args([
            "apply",
            "--package",
            &download.package_path,
            "--parent-pid",
            &std::process::id().to_string(),
            "--relaunch",
            &std::env::current_exe()
                .map_err(|err| format!("定位 Console 可执行文件失败: {err}"))?
                .to_string_lossy(),
        ])
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
    command
        .spawn()
        .map_err(|err| format!("启动安装程序失败: {err}"))?;
    if download.status.can_auto_install {
        app.exit(0);
    }
    Ok(download.status)
}
