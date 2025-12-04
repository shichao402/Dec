# CursorToolset 一键安装脚本 (Windows PowerShell)
# 使用方法: iwr -useb https://raw.githubusercontent.com/firoyang/CursorToolset/main/install.ps1 | iex

$ErrorActionPreference = "Stop"

# 颜色输出函数
function Write-ColorOutput {
    param(
        [string]$Message,
        [string]$Type = "Info"
    )
    
    switch ($Type) {
        "Success" { Write-Host "✓ $Message" -ForegroundColor Green }
        "Error" { Write-Host "✗ $Message" -ForegroundColor Red }
        "Warning" { Write-Host "⚠ $Message" -ForegroundColor Yellow }
        "Info" { Write-Host "ℹ $Message" -ForegroundColor Blue }
        default { Write-Host $Message }
    }
}

# 检测平台
function Get-Platform {
    $arch = if ([Environment]::Is64BitOperatingSystem) { "amd64" } else { "386" }
    return "windows-$arch"
}

# 检查依赖
function Test-Dependencies {
    Write-ColorOutput "检查依赖..." -Type "Info"
    
    if (-not (Get-Command git -ErrorAction SilentlyContinue)) {
        Write-ColorOutput "Git 未安装。请先安装 Git: https://git-scm.com/" -Type "Error"
        exit 1
    }
    
    Write-ColorOutput "依赖检查通过" -Type "Success"
}

# 添加到 PATH
function Add-ToPath {
    param([string]$PathToAdd)
    
    $currentPath = [Environment]::GetEnvironmentVariable("Path", "User")
    
    if ($currentPath -notlike "*$PathToAdd*") {
        Write-ColorOutput "添加到 PATH: $PathToAdd" -Type "Info"
        $newPath = "$currentPath;$PathToAdd"
        [Environment]::SetEnvironmentVariable("Path", $newPath, "User")
        
        # 更新当前会话的 PATH
        $env:Path = "$env:Path;$PathToAdd"
        
        Write-ColorOutput "已添加到用户 PATH" -Type "Success"
    } else {
        Write-ColorOutput "PATH 中已存在，跳过" -Type "Info"
    }
}

# 主安装函数
function Install-CursorToolset {
    Write-Host ""
    Write-Host "╔═══════════════════════════════════════╗"
    Write-Host "║   CursorToolset 一键安装脚本          ║"
    Write-Host "╚═══════════════════════════════════════╝"
    Write-Host ""
    
    # 检查依赖
    Test-Dependencies
    
    # 检测平台
    $platform = Get-Platform
    Write-ColorOutput "检测到平台: $platform" -Type "Info"
    
    # 定义安装路径
    $installDir = Join-Path $env:USERPROFILE ".cursor\toolsets\CursorToolset"
    $binDir = Join-Path $installDir "bin"
    $binaryPath = Join-Path $binDir "cursortoolset.exe"
    
    Write-ColorOutput "安装目录: $installDir" -Type "Info"
    
    # 创建安装目录
    Write-ColorOutput "创建安装目录..." -Type "Info"
    New-Item -ItemType Directory -Force -Path $binDir | Out-Null
    
    # 克隆仓库到临时目录
    $tempDir = Join-Path $env:TEMP "cursortoolset-install-$(Get-Random)"
    Write-ColorOutput "克隆仓库到临时目录: $tempDir" -Type "Info"
    
    try {
        git clone --depth 1 https://github.com/firoyang/CursorToolset.git $tempDir
    } catch {
        Write-ColorOutput "克隆仓库失败: $_" -Type "Error"
        exit 1
    }
    
    # 检查是否安装了 Go
    if (Get-Command go -ErrorAction SilentlyContinue) {
        Write-ColorOutput "使用 Go 构建..." -Type "Info"
        
        Push-Location $tempDir
        try {
            go build -o $binaryPath .
            Write-ColorOutput "构建成功" -Type "Success"
        } catch {
            Write-ColorOutput "构建失败: $_" -Type "Error"
            Pop-Location
            Remove-Item -Recurse -Force $tempDir
            exit 1
        }
        Pop-Location
    } else {
        Write-ColorOutput "Go 未安装，尝试下载预编译版本..." -Type "Warning"
        
        # 获取最新版本号
        try {
            $apiResponse = Invoke-RestMethod -Uri "https://api.github.com/repos/firoyang/CursorToolset/releases/latest"
            $latestVersion = $apiResponse.tag_name
        } catch {
            Write-ColorOutput "无法获取最新版本" -Type "Warning"
            Write-ColorOutput "请先安装 Go: https://go.dev/dl/" -Type "Error"
            Remove-Item -Recurse -Force $tempDir
            exit 1
        }
        
        # 下载预编译版本
        $binaryName = "cursortoolset-$PLATFORM.exe"
        $downloadUrl = "https://github.com/firoyang/CursorToolset/releases/download/$latestVersion/$binaryName"
        
        Write-ColorOutput "下载 $latestVersion 版本..." -Type "Info"
        try {
            Invoke-WebRequest -Uri $downloadUrl -OutFile $binaryPath
            Write-ColorOutput "下载成功" -Type "Success"
        } catch {
            Write-ColorOutput "下载失败: $_" -Type "Error"
            Write-ColorOutput "请先安装 Go: https://go.dev/dl/" -Type "Error"
            Remove-Item -Recurse -Force $tempDir
            exit 1
        }
    }
    
    # 复制配置文件
    Write-ColorOutput "复制配置文件..." -Type "Info"
    Copy-Item (Join-Path $tempDir "available-toolsets.json") $installDir
    
    # 清理临时目录
    Remove-Item -Recurse -Force $tempDir
    
    # 添加到 PATH
    Write-ColorOutput "配置环境变量..." -Type "Info"
    Add-ToPath $binDir
    
    # 验证安装
    Write-ColorOutput "验证安装..." -Type "Info"
    if (Test-Path $binaryPath) {
        try {
            $version = & $binaryPath --version 2>&1
            Write-ColorOutput "安装成功！版本: $version" -Type "Success"
        } catch {
            $version = "unknown"
            Write-ColorOutput "安装成功！" -Type "Success"
        }
    } else {
        Write-ColorOutput "安装失败：可执行文件不存在" -Type "Error"
        exit 1
    }
    
    Write-Host ""
    Write-Host "╔═══════════════════════════════════════╗"
    Write-Host "║         安装完成！                    ║"
    Write-Host "╚═══════════════════════════════════════╝"
    Write-Host ""
    Write-ColorOutput "安装位置: $binaryPath" -Type "Info"
    Write-Host ""
    Write-ColorOutput "环境变量已更新！请重新打开 PowerShell 窗口，或运行：" -Type "Info"
    Write-Host "  `$env:Path = [System.Environment]::GetEnvironmentVariable('Path','User')"
    Write-Host ""
    Write-ColorOutput "之后可以在任何位置运行：" -Type "Info"
    Write-Host "  cursortoolset install"
    Write-Host "  cursortoolset list"
    Write-Host "  cursortoolset update"
    Write-Host ""
}

# 运行主函数
try {
    Install-CursorToolset
} catch {
    Write-ColorOutput "安装过程中发生错误: $_" -Type "Error"
    exit 1
}

