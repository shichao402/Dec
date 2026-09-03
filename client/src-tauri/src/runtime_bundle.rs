use serde::Deserialize;
use sha2::{Digest, Sha256};
use std::collections::BTreeMap;
use std::fs;
use std::io::{Read, Write};
use std::path::{Path, PathBuf};
use tauri::{AppHandle, Manager};
use uuid::Uuid;

const COMPONENTS: [&str; 4] = ["dec", "dec-server", "dec-mcp", "dec-exec"];

#[derive(Debug, Deserialize)]
struct RuntimeManifest {
    version: String,
    os: String,
    arch: String,
    files: BTreeMap<String, String>,
}

struct SuiteFile {
    name: String,
    source: PathBuf,
    expected: String,
}

fn platform() -> (&'static str, &'static str) {
    let os = if cfg!(target_os = "windows") {
        "windows"
    } else if cfg!(target_os = "macos") {
        "darwin"
    } else {
        "linux"
    };
    let arch = if cfg!(target_arch = "aarch64") {
        "arm64"
    } else {
        "amd64"
    };
    (os, arch)
}

fn binary_name(component: &str) -> String {
    format!("{component}{}", if cfg!(windows) { ".exe" } else { "" })
}

fn sha256_file(path: &Path) -> Result<String, String> {
    let mut file = fs::File::open(path).map_err(|e| format!("读取 {path:?} 失败: {e}"))?;
    let mut digest = Sha256::new();
    let mut buffer = [0_u8; 64 * 1024];
    loop {
        let count = file
            .read(&mut buffer)
            .map_err(|e| format!("读取 {path:?} 失败: {e}"))?;
        if count == 0 {
            break;
        }
        digest.update(&buffer[..count]);
    }
    Ok(format!("{:x}", digest.finalize()))
}

fn verify_file(path: &Path, expected: &str) -> Result<(), String> {
    let actual = sha256_file(path)?;
    if actual != expected {
        return Err(format!(
            "内置运行时校验失败：{} sha256 期望 {expected}，实际 {actual}",
            path.display()
        ));
    }
    Ok(())
}

fn stage_file(file: &SuiteFile, stage: &Path) -> Result<(), String> {
    let target = stage.join(&file.name);
    let mut input =
        fs::File::open(&file.source).map_err(|e| format!("读取 {:?} 失败: {e}", file.source))?;
    let mut output = fs::File::create(&target).map_err(|e| format!("创建 {target:?} 失败: {e}"))?;
    std::io::copy(&mut input, &mut output)
        .map_err(|e| format!("复制 {} 失败: {e}", file.source.display()))?;
    output
        .flush()
        .map_err(|e| format!("刷新 {target:?} 失败: {e}"))?;
    output
        .sync_all()
        .map_err(|e| format!("同步 {target:?} 失败: {e}"))?;
    verify_file(&target, &file.expected)?;
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let mode = if file.name == "runtime-manifest.json" {
            0o644
        } else {
            0o755
        };
        fs::set_permissions(&target, fs::Permissions::from_mode(mode))
            .map_err(|e| format!("设置 {target:?} 权限失败: {e}"))?;
    }
    Ok(())
}

fn rollback_suite(target_dir: &Path, backup: &Path, installed: &[String], backed: &[String]) {
    for name in installed.iter().rev() {
        let _ = fs::remove_file(target_dir.join(name));
    }
    for name in backed.iter().rev() {
        let _ = fs::rename(backup.join(name), target_dir.join(name));
    }
}

fn replace_suite(files: &[SuiteFile], target_dir: &Path) -> Result<(), String> {
    let parent = target_dir
        .parent()
        .ok_or_else(|| format!("目标路径缺少父目录: {}", target_dir.display()))?;
    fs::create_dir_all(parent).map_err(|e| format!("创建 {parent:?} 失败: {e}"))?;
    let id = Uuid::new_v4();
    let stage = parent.join(format!(".runtime-stage-{id}"));
    let backup = parent.join(format!(".runtime-backup-{id}"));
    fs::create_dir(&stage).map_err(|e| format!("创建 {stage:?} 失败: {e}"))?;
    fs::create_dir(&backup).map_err(|e| format!("创建 {backup:?} 失败: {e}"))?;

    let mut backed = Vec::new();
    let mut installed = Vec::new();
    let result = (|| {
        // 所有资源先完成复制与摘要校验，之后才触碰现有套件。
        for file in files {
            stage_file(file, &stage)?;
        }
        fs::create_dir_all(target_dir)
            .map_err(|e| format!("创建运行时目录 {target_dir:?} 失败: {e}"))?;
        for file in files {
            let target = target_dir.join(&file.name);
            if target.exists() {
                fs::rename(&target, backup.join(&file.name))
                    .map_err(|e| format!("备份旧运行时 {} 失败: {e}", target.display()))?;
                backed.push(file.name.clone());
            }
            fs::rename(stage.join(&file.name), &target)
                .map_err(|e| format!("激活运行时 {} 失败: {e}", target.display()))?;
            installed.push(file.name.clone());
        }
        Ok(())
    })();
    if result.is_err() {
        rollback_suite(target_dir, &backup, &installed, &backed);
    }
    let _ = fs::remove_dir_all(&stage);
    let _ = fs::remove_dir_all(&backup);
    result
}

fn read_manifest(resource_platform: &Path) -> Result<RuntimeManifest, String> {
    let path = resource_platform.join("runtime-manifest.json");
    let data = fs::read(&path).map_err(|e| {
        format!(
            "读取内置运行时清单 {path:?} 失败: {e}。源码开发请先在仓库根运行 \
             `python scripts/build-console.py --prepare-runtime-only`；release 安装包可能已损坏"
        )
    })?;
    serde_json::from_slice(&data).map_err(|e| format!("解析内置运行时清单 {path:?} 失败: {e}"))
}

fn resource_platform(app: &AppHandle) -> Result<(PathBuf, String), String> {
    let (os, arch) = platform();
    let platform_id = format!("{os}-{arch}");
    let path = app
        .path()
        .resource_dir()
        .map_err(|e| format!("定位 Console resources 失败: {e}"))?
        .join("resources")
        .join("runtime")
        .join(&platform_id);
    Ok((path, platform_id))
}

fn bundle(
    app: &AppHandle,
    console_version: &str,
) -> Result<(PathBuf, RuntimeManifest, String), String> {
    let (os, arch) = platform();
    let (resource_platform, platform_id) = resource_platform(app)?;
    let manifest = read_manifest(&resource_platform)?;
    let expected_version = format!("v{}", console_version.trim_start_matches('v'));
    if manifest.version != expected_version || manifest.os != os || manifest.arch != arch {
        return Err(format!(
            "内置运行时身份不匹配：期望 {expected_version} {os}/{arch}，实际 {} {}/{}",
            manifest.version, manifest.os, manifest.arch
        ));
    }
    // 在写 cache/bin 前一次性验证全部源文件，避免后几项损坏时留下部分更新。
    for component in COMPONENTS {
        let name = binary_name(component);
        let expected = manifest
            .files
            .get(&name)
            .ok_or_else(|| format!("内置运行时清单缺少 {name}"))?;
        verify_file(&resource_platform.join(name), expected)?;
    }
    Ok((resource_platform, manifest, platform_id))
}

fn component_files(resource_platform: &Path, manifest: &RuntimeManifest) -> Vec<SuiteFile> {
    COMPONENTS
        .iter()
        .map(|component| {
            let name = binary_name(component);
            SuiteFile {
                source: resource_platform.join(&name),
                expected: manifest.files[&name].clone(),
                name,
            }
        })
        .collect()
}

pub fn prewarm(app: &AppHandle, dec_home: &Path, console_version: &str) -> Result<(), String> {
    let (resource_platform, platform_id) = resource_platform(app)?;
    if cfg!(debug_assertions) && !resource_platform.join("runtime-manifest.json").is_file() {
        eprintln!(
            "[dec-console] warning: debug runtime bundle {platform_id} 尚未准备，跳过缓存预热；\
             如需安装/升级运行时，请先在仓库根运行 \
             `python scripts/build-console.py --prepare-runtime-only`"
        );
        return Ok(());
    }
    cache(app, dec_home, console_version)
}

pub fn cache(app: &AppHandle, dec_home: &Path, console_version: &str) -> Result<(), String> {
    let (resource_platform, manifest, platform_id) = bundle(app, console_version)?;
    let cache_dir = dec_home
        .join("runtime-cache")
        .join(console_version.trim_start_matches('v'))
        .join(&platform_id);
    let mut files = component_files(&resource_platform, &manifest);
    let manifest_source = resource_platform.join("runtime-manifest.json");
    let manifest_hash = sha256_file(&manifest_source)?;
    files.push(SuiteFile {
        name: "runtime-manifest.json".into(),
        source: manifest_source,
        expected: manifest_hash,
    });
    replace_suite(&files, &cache_dir)
}

pub fn install(app: &AppHandle, dec_home: &Path, console_version: &str) -> Result<(), String> {
    cache(app, dec_home, console_version)?;
    let (resource_platform, manifest, _) = bundle(app, console_version)?;
    let bin_dir = dec_home.join("bin");
    replace_suite(&component_files(&resource_platform, &manifest), &bin_dir)
}

#[cfg(test)]
mod tests {
    use super::{binary_name, platform, read_manifest, replace_suite, sha256_file, SuiteFile};
    use std::fs;
    use uuid::Uuid;

    #[test]
    fn native_platform_and_names_are_stable() {
        let (os, arch) = platform();
        assert!(matches!(os, "windows" | "darwin" | "linux"));
        assert!(matches!(arch, "amd64" | "arm64"));
        assert_eq!(binary_name("dec").ends_with(".exe"), cfg!(windows));
    }

    #[test]
    fn invalid_late_source_keeps_existing_suite_unchanged() {
        let root = std::env::temp_dir().join(format!("dec-runtime-test-{}", Uuid::new_v4()));
        let source = root.join("source");
        let target = root.join("bin");
        fs::create_dir_all(&source).unwrap();
        fs::create_dir_all(&target).unwrap();
        let mut files = Vec::new();
        for (index, name) in ["dec", "dec-server", "dec-mcp", "dec-exec"]
            .iter()
            .enumerate()
        {
            let source_path = source.join(name);
            fs::write(&source_path, format!("new-{name}")).unwrap();
            fs::write(target.join(name), format!("old-{name}")).unwrap();
            files.push(SuiteFile {
                name: (*name).into(),
                expected: if index == 3 {
                    "0".repeat(64)
                } else {
                    sha256_file(&source_path).unwrap()
                },
                source: source_path,
            });
        }
        assert!(replace_suite(&files, &target).is_err());
        for name in ["dec", "dec-server", "dec-mcp", "dec-exec"] {
            assert_eq!(
                fs::read_to_string(target.join(name)).unwrap(),
                format!("old-{name}")
            );
        }
        let _ = fs::remove_dir_all(root);
    }

    #[test]
    fn missing_debug_bundle_error_has_prepare_command() {
        let root = std::env::temp_dir().join(format!("dec-runtime-test-{}", Uuid::new_v4()));
        fs::create_dir_all(&root).unwrap();
        let err = read_manifest(&root).unwrap_err();
        assert!(err.contains("--prepare-runtime-only"));
        let _ = fs::remove_dir_all(root);
    }
}
