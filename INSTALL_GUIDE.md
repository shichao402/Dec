# CursorToolset 安装指南

## 一键安装

### Linux / macOS

```bash
curl -fsSL https://raw.githubusercontent.com/shichao402/CursorToolset/main/install.sh | bash
```

安装完成后，运行以下命令使环境变量生效：
```bash
source ~/.zshrc    # 如果使用 zsh
source ~/.bashrc   # 如果使用 bash
source ~/.bash_profile   # macOS bash
```

### Windows (PowerShell)

以管理员身份运行 PowerShell，然后执行：

```powershell
iwr -useb https://raw.githubusercontent.com/shichao402/CursorToolset/main/install.ps1 | iex
```

安装完成后，重新打开 PowerShell 窗口。

## 安装位置

### Linux / macOS
- **安装目录**: `~/.cursortoolsets/CursorToolset/`
- **可执行文件**: `~/.cursortoolsets/CursorToolset/bin/cursortoolset`
- **配置文件**: `~/.cursortoolsets/CursorToolset/available-toolsets.json`

### Windows
- **安装目录**: `%USERPROFILE%\.cursortoolsets\CursorToolset\`
- **可执行文件**: `%USERPROFILE%\.cursortoolsets\CursorToolset\bin\cursortoolset.exe`
- **配置文件**: `%USERPROFILE%\.cursortoolsets\CursorToolset\available-toolsets.json`

## 手动安装

如果一键安装脚本不可用，可以手动安装：

### 前提条件
- Git
- Go 1.21 或更高版本（用于构建）

### 步骤

#### Linux / macOS

```bash
# 1. 创建安装目录
mkdir -p ~/.cursortoolsets/CursorToolset/bin

# 2. 克隆仓库
git clone https://github.com/shichao402/CursorToolset.git /tmp/cursortoolset-build

# 3. 构建
cd /tmp/cursortoolset-build
go build -o ~/.cursortoolsets/CursorToolset/bin/cursortoolset

# 4. 复制配置文件
cp available-toolsets.json ~/.cursortoolsets/CursorToolset/

# 5. 清理临时目录
cd ~
rm -rf /tmp/cursortoolset-build

# 6. 添加到 PATH
echo 'export PATH="$HOME/.cursortoolsets/CursorToolset/bin:$PATH"' >> ~/.zshrc
source ~/.zshrc
```

#### Windows (PowerShell)

```powershell
# 1. 创建安装目录
New-Item -ItemType Directory -Force -Path "$env:USERPROFILE\.cursor\toolsets\CursorToolset\bin"

# 2. 克隆仓库
git clone https://github.com/shichao402/CursorToolset.git "$env:TEMP\cursortoolset-build"

# 3. 构建
cd "$env:TEMP\cursortoolset-build"
go build -o "$env:USERPROFILE\.cursor\toolsets\CursorToolset\bin\cursortoolset.exe"

# 4. 复制配置文件
Copy-Item available-toolsets.json "$env:USERPROFILE\.cursor\toolsets\CursorToolset\"

# 5. 清理临时目录
cd $env:USERPROFILE
Remove-Item -Recurse -Force "$env:TEMP\cursortoolset-build"

# 6. 添加到 PATH（需要管理员权限或用户权限）
$path = [Environment]::GetEnvironmentVariable("Path", "User")
$newPath = "$env:USERPROFILE\.cursor\toolsets\CursorToolset\bin"
if ($path -notlike "*$newPath*") {
    [Environment]::SetEnvironmentVariable("Path", "$path;$newPath", "User")
}

# 7. 更新当前会话的 PATH
$env:Path = "$env:Path;$newPath"
```

## 验证安装

安装完成后，验证是否成功：

```bash
# 检查版本
cursortoolset --version

# 列出可用工具集
cursortoolset list

# 查看帮助
cursortoolset --help
```

## 更新 CursorToolset

安装后可以使用内置的更新功能：

```bash
# 更新所有（CursorToolset + 配置 + 工具集）
cursortoolset update

# 只更新 CursorToolset 自身
cursortoolset update --self

# 只更新配置文件
cursortoolset update --available

# 只更新已安装的工具集
cursortoolset update --toolsets
```

## 卸载

### Linux / macOS

```bash
# 1. 删除安装目录
rm -rf ~/.cursortoolsets/CursorToolset

# 2. 从 PATH 中移除（手动编辑配置文件）
# 编辑 ~/.zshrc 或 ~/.bashrc，删除包含 cursortoolset 的行
```

### Windows

```powershell
# 1. 删除安装目录
Remove-Item -Recurse -Force "$env:USERPROFILE\.cursor\toolsets\CursorToolset"

# 2. 从 PATH 中移除
$path = [Environment]::GetEnvironmentVariable("Path", "User")
$newPath = $path -replace [regex]::Escape("$env:USERPROFILE\.cursor\toolsets\CursorToolset\bin;?"), ""
[Environment]::SetEnvironmentVariable("Path", $newPath, "User")
```

## 故障排除

### 问题 1: 命令未找到

**错误**: `command not found: cursortoolset`

**解决方案**:
- Linux/macOS: 运行 `source ~/.zshrc` 或重新打开终端
- Windows: 重新打开 PowerShell 窗口

### 问题 2: 权限错误

**错误**: `Permission denied`

**解决方案**:
```bash
# 确保可执行文件有执行权限
chmod +x ~/.cursortoolsets/CursorToolset/bin/cursortoolset
```

### 问题 3: Go 未安装

**解决方案**:
- macOS: `brew install go`
- Linux: 参考 https://go.dev/doc/install
- Windows: 下载安装程序 https://go.dev/dl/

### 问题 4: Git 未安装

**解决方案**:
- macOS: `brew install git` 或使用 Xcode Command Line Tools
- Linux: `sudo apt install git` (Ubuntu/Debian) 或 `sudo yum install git` (CentOS/RHEL)
- Windows: https://git-scm.com/download/win

## 高级配置

### 自定义安装位置

如果想安装到其他位置，修改安装脚本中的 `INSTALL_DIR` 变量：

```bash
# Linux/macOS
INSTALL_DIR="/usr/local/cursortoolset"

# Windows
$installDir = "C:\Tools\CursorToolset"
```

### 使用代理

如果网络需要代理，设置环境变量：

```bash
# Linux/macOS
export HTTP_PROXY=http://proxy.example.com:8080
export HTTPS_PROXY=http://proxy.example.com:8080

# Windows
$env:HTTP_PROXY = "http://proxy.example.com:8080"
$env:HTTPS_PROXY = "http://proxy.example.com:8080"
```

## 开发者安装

如果你想参与开发或从源码构建：

```bash
# 克隆仓库
git clone https://github.com/shichao402/CursorToolset.git
cd CursorToolset

# 构建
go build -o cursortoolset

# 本地测试
./cursortoolset --help

# 安装到系统
go install
```

## 下一步

安装完成后，请查看：
- [README.md](./README.md) - 使用文档
- [ARCHITECTURE.md](./ARCHITECTURE.md) - 架构设计
- [FEATURES.md](./FEATURES.md) - 功能演示

开始使用：
```bash
# 列出可用工具集
cursortoolset list

# 安装工具集
cursortoolset install

# 查看帮助
cursortoolset --help
```

