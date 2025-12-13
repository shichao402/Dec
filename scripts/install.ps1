# Dec 一键安装脚本 (Windows PowerShell)
# 使用方法: iwr -useb https://raw.githubusercontent.com/shichao402/Dec/ReleaseLatest/install.ps1 | iex

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

# 比较版本号
function Compare-Versions {
    param(
        [string]$Version1,
        [string]$Version2
    )
    
    # 移除 v 前缀
    $v1 = $Version1 -replace '^v', ''
    $v2 = $Version2 -replace '^v', ''
    
    # 分割版本号
    $v1Parts = $v1 -split '\.'
    $v2Parts = $v2 -split '\.'
    
    # 比较每个部分
    for ($i = 0; $i -lt 3; $i++) {
        $v1Part = if ($v1Parts[$i]) { [int]$v1Parts[$i] } else { 0 }
        $v2Part = if ($v2Parts[$i]) { [int]$v2Parts[$i] } else { 0 }
        
        if ($v1Part -gt $v2Part) { return 1 }
        if ($v1Part -lt $v2Part) { return -1 }
    }
    
    return 0
}

# 主安装函数
function Install-Dec {
    Write-Host ""
    Write-Host "╔═══════════════════════════════════════╗"
    Write-Host "║   Dec 一键安装脚本          ║"
    Write-Host "╚═══════════════════════════════════════╝"
    Write-Host ""
    
    # 检测平台
    $platform = Get-Platform
    Write-ColorOutput "检测到平台: $platform" -Type "Info"
    
    # 定义安装路径（新设计：统一使用环境目录）
    # 优先使用环境变量 DEC_HOME，如果未设置则使用默认路径
    if ($env:DEC_HOME) {
        $installDir = $env:DEC_HOME
        Write-ColorOutput "使用环境变量 DEC_HOME: $installDir" -Type "Info"
    } else {
        $installDir = Join-Path $env:USERPROFILE ".decs"
    }
    $binDir = Join-Path $installDir "bin"
    $configDir = Join-Path $installDir "config"
    $reposDir = Join-Path $installDir "repos"
    $binaryPath = Join-Path $binDir "dec.exe"
    $configPath = Join-Path $configDir "available-toolsets.json"
    
    Write-ColorOutput "安装目录: $installDir" -Type "Info"
    
    # 从 ReleaseLatest 分支获取版本号（唯一来源）
    Write-ColorOutput "获取最新版本号..." -Type "Info"
    try {
        $versionJson = Invoke-RestMethod -Uri "https://raw.githubusercontent.com/shichao402/Dec/ReleaseLatest/version.json" -ErrorAction Stop
        $latestVersion = $versionJson.version
        
        if (-not $latestVersion) {
            Write-ColorOutput "无法解析版本号" -Type "Error"
            exit 1
        }
        
        Write-ColorOutput "最新版本: $latestVersion" -Type "Info"
    } catch {
        Write-ColorOutput "无法从 ReleaseLatest 分支获取版本信息" -Type "Error"
        Write-ColorOutput "请检查网络连接或稍后重试" -Type "Error"
        exit 1
    }
    
    # 检查是否已安装
    $currentVersion = $null
    if (Test-Path $binaryPath) {
        try {
            $versionOutput = & $binaryPath --version 2>&1
            if ($versionOutput -match 'v(\d+\.\d+\.\d+)') {
                $currentVersion = $matches[0]
                Write-ColorOutput "当前已安装版本: $currentVersion" -Type "Info"
                
                # 比较版本
                $compareResult = Compare-Versions -Version1 $currentVersion -Version2 $latestVersion
                if ($compareResult -ge 0) {
                    Write-ColorOutput "已是最新版本，无需更新" -Type "Success"
                    exit 0
                } else {
                    Write-ColorOutput "发现新版本，准备更新..." -Type "Info"
                }
            }
        } catch {
            # 忽略错误，继续安装
        }
    }
    
    # 创建安装目录
    Write-ColorOutput "创建安装目录..." -Type "Info"
    New-Item -ItemType Directory -Force -Path $binDir | Out-Null
    New-Item -ItemType Directory -Force -Path $configDir | Out-Null
    New-Item -ItemType Directory -Force -Path $reposDir | Out-Null
    
    # 构建下载 URL（使用版本号）
    $binaryName = "dec-$platform.exe"
    $downloadUrl = "https://github.com/shichao402/Dec/releases/download/$latestVersion/$binaryName"
    
    # 下载预编译版本
    Write-ColorOutput "下载预编译版本..." -Type "Info"
    try {
        Invoke-WebRequest -Uri $downloadUrl -OutFile $binaryPath -ErrorAction Stop
        Write-ColorOutput "预编译版本下载成功" -Type "Success"
    } catch {
        Write-ColorOutput "下载预编译版本失败" -Type "Error"
        Write-ColorOutput "下载 URL: $downloadUrl" -Type "Error"
        Write-ColorOutput "请确保该版本已发布到 GitHub Releases" -Type "Error"
        exit 1
    }
    
    # 添加到 PATH
    Write-ColorOutput "配置环境变量..." -Type "Info"
    $currentPath = [Environment]::GetEnvironmentVariable("Path", "User")
    
    if ($currentPath -notlike "*$binDir*") {
        Write-ColorOutput "添加到 PATH: $binDir" -Type "Info"
        $newPath = "$currentPath;$binDir"
        [Environment]::SetEnvironmentVariable("Path", $newPath, "User")
        
        # 更新当前会话的 PATH
        $env:Path = "$env:Path;$binDir"
        
        Write-ColorOutput "已添加到用户 PATH" -Type "Success"
    } else {
        Write-ColorOutput "PATH 中已存在，跳过" -Type "Info"
    }
    
    # 验证安装
    Write-ColorOutput "验证安装..." -Type "Info"
    if (Test-Path $binaryPath) {
        try {
            $installedVersion = & $binaryPath --version 2>&1
            Write-ColorOutput "安装成功！版本: $installedVersion" -Type "Success"
        } catch {
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
    Write-Host "  dec install"
    Write-Host "  dec list"
    Write-Host "  dec update"
    Write-Host ""
}

# 运行主函数
try {
    Install-Dec
} catch {
    Write-ColorOutput "安装过程中发生错误: $_" -Type "Error"
    exit 1
}
