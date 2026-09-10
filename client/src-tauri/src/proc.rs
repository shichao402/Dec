use std::ffi::OsStr;
use std::process::Command;

// Console 是 GUI 进程，自己没有控制台。Windows 上每起一个控制台程序
//（dec-server --version、tasklist、ssh）都会新建一个控制台窗口闪到前台并抢
// 走键盘焦点。Go 侧由 internal/sysproc 兜住同一条线。
#[cfg(windows)]
const CREATE_NO_WINDOW: u32 = 0x0800_0000;

/// 起子进程的唯一入口。`no_direct_command` 守着这条线。
pub fn command(program: impl AsRef<OsStr>) -> Command {
    let mut cmd = Command::new(program);
    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        cmd.creation_flags(CREATE_NO_WINDOW);
    }
    cmd
}

#[cfg(test)]
mod tests {
    use std::fs;
    use std::path::Path;

    // 允许直接用 std::process::Command 的文件：包装器自身（正文含被检测的字面
    // 量），以及 grpc.rs —— 它用 DETACHED_PROCESS 让 dec-server 脱离 Console 生命
    // 周期，该标志本身就不建控制台，且与 CREATE_NO_WINDOW 互斥。
    const ALLOWED: [&str; 2] = ["proc.rs", "grpc.rs"];

    #[test]
    fn no_direct_command() {
        let src = Path::new(env!("CARGO_MANIFEST_DIR")).join("src");
        let mut offenders = Vec::new();
        for entry in fs::read_dir(&src).expect("读取 src 失败") {
            let path = entry.expect("读取目录项失败").path();
            let Some(name) = path.file_name().and_then(|name| name.to_str()) else {
                continue;
            };
            if !name.ends_with(".rs") || ALLOWED.contains(&name) {
                continue;
            }
            let text = fs::read_to_string(&path).expect("读取源码失败");
            for (index, line) in text.lines().enumerate() {
                if line.trim_start().starts_with("//") {
                    continue;
                }
                if line.contains("Command::new(") {
                    offenders.push(format!("{name}:{}", index + 1));
                }
            }
        }
        assert!(
            offenders.is_empty(),
            "以下位置直接起子进程，Windows 上会弹控制台窗口，请改用 proc::command：\n  {}",
            offenders.join("\n  "),
        );
    }
}
