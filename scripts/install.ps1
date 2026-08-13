# Dec 一键安装脚本 (Windows PowerShell)
# 使用方法: iwr -useb https://raw.githubusercontent.com/shichao402/Dec/ReleaseLatest/scripts/install.ps1 | iex

$ErrorActionPreference = "Stop"
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8

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

function Get-Platform {
    $arch = if ([Environment]::Is64BitOperatingSystem) { "amd64" } else { "386" }
    return "windows-$arch"
}

function Compare-Versions {
    param(
        [string]$Version1,
        [string]$Version2
    )

    $v1 = $Version1 -replace '^v', ''
    $v2 = $Version2 -replace '^v', ''
    $v1Parts = $v1 -split '\.'
    $v2Parts = $v2 -split '\.'

    for ($i = 0; $i -lt 3; $i++) {
        $v1Part = if ($v1Parts[$i]) { [int]$v1Parts[$i] } else { 0 }
        $v2Part = if ($v2Parts[$i]) { [int]$v2Parts[$i] } else { 0 }

        if ($v1Part -gt $v2Part) { return 1 }
        if ($v1Part -lt $v2Part) { return -1 }
    }

    return 0
}

function Install-Dec {
    Write-Host ""
    Write-Host "╔═══════════════════════════════════════╗"
    Write-Host "║        Dec 一键安装脚本              ║"
    Write-Host "╚═══════════════════════════════════════╝"
    Write-Host ""

    $platform = Get-Platform
    $installDir = if ($env:DEC_HOME) { $env:DEC_HOME } else { Join-Path $env:USERPROFILE ".dec" }
    $binDir = Join-Path $installDir "bin"
    $binaryPath = Join-Path $binDir "dec.exe"
    $binaries = @("dec", "dec-server", "dec-mcp", "dec-exec")
    $updateBranch = if ($env:DEC_BRANCH) { $env:DEC_BRANCH } else { "ReleaseLatest" }

    Write-ColorOutput "检测到平台: $platform" -Type "Info"
    Write-ColorOutput "安装目录: $installDir" -Type "Info"
    Write-ColorOutput "更新分支: $updateBranch" -Type "Info"

    # raw.githubusercontent.com 在部分网络下会间歇性超时，失败时回退到 CDN 镜像
    $versionSources = @(
        "https://raw.githubusercontent.com/shichao402/Dec/$updateBranch/version.json",
        "https://cdn.jsdelivr.net/gh/shichao402/Dec@$updateBranch/version.json"
    )
    $latestVersion = $null
    for ($i = 0; $i -lt $versionSources.Count; $i++) {
        try {
            $versionJson = Invoke-RestMethod -Uri $versionSources[$i] -TimeoutSec 20 -ErrorAction Stop
            $latestVersion = $versionJson.version
            if ($latestVersion) { break }
        } catch {
            if ($i -lt $versionSources.Count - 1) {
                Write-ColorOutput "从 $($versionSources[$i]) 获取版本信息失败，尝试下一个来源" -Type "Warning"
            }
        }
    }
    if (-not $latestVersion) {
        Write-ColorOutput "无法从 $updateBranch 获取版本信息" -Type "Error"
        Write-ColorOutput "若使用代理客户端，请先设置 `$env:HTTPS_PROXY 后重试" -Type "Error"
        exit 1
    }

    Write-ColorOutput "最新版本: $latestVersion" -Type "Info"

    if (Test-Path $binaryPath) {
        try {
            $currentVersion = & $binaryPath --version 2>&1 | Select-String -Pattern 'v(\d+\.\d+\.\d+)' | ForEach-Object { $_.Matches[0].Value } | Select-Object -First 1
            if ($currentVersion) {
                Write-ColorOutput "当前已安装版本: $currentVersion" -Type "Info"
                if ((Compare-Versions -Version1 $currentVersion -Version2 $latestVersion) -ge 0) {
                    $suiteComplete = $true
                    foreach ($binary in $binaries) {
                        if (-not (Test-Path (Join-Path $binDir "$binary.exe"))) { $suiteComplete = $false }
                    }
                    if ($suiteComplete) {
                        Write-ColorOutput "已是最新版本，且四个程序完整" -Type "Success"
                        exit 0
                    }
                    Write-ColorOutput "主程序已是最新版本，但服务/门面程序不完整，将修复安装" -Type "Warning"
                }
                # 版本较旧，提示用户选择
                $answer = Read-Host "检测到旧版本 $currentVersion，最新版本为 $latestVersion，是否覆盖安装？[Y/n]"
                if ($answer -eq 'n' -or $answer -eq 'N') {
                    Write-ColorOutput "已跳过安装" -Type "Info"
                    exit 0
                }
            } else {
                # 版本解析失败，提示用户选择
                Write-ColorOutput "检测到已安装的 Dec，但无法获取版本号" -Type "Warning"
                $answer = Read-Host "是否覆盖安装？[Y/n]"
                if ($answer -eq 'n' -or $answer -eq 'N') {
                    Write-ColorOutput "已跳过安装" -Type "Info"
                    exit 0
                }
            }
        } catch {
            # 执行失败，提示用户选择
            Write-ColorOutput "检测到已安装的 Dec，但无法获取版本号" -Type "Warning"
            $answer = Read-Host "是否覆盖安装？[Y/n]"
            if ($answer -eq 'n' -or $answer -eq 'N') {
                Write-ColorOutput "已跳过安装" -Type "Info"
                exit 0
            }
        }
    }

    New-Item -ItemType Directory -Force -Path $binDir | Out-Null

    $downloadTag = if ($updateBranch -eq "ReleaseTest") { "test-$latestVersion" } else { $latestVersion }
    Write-ColorOutput "下载 Dec 程序组..." -Type "Info"
    foreach ($binary in $binaries) {
        $binaryName = "$binary-$platform.exe"
        $downloadUrl = "https://github.com/shichao402/Dec/releases/download/$downloadTag/$binaryName"
        $targetPath = Join-Path $binDir "$binary.exe"
        try {
            Invoke-WebRequest -Uri $downloadUrl -OutFile $targetPath -ErrorAction Stop
        } catch {
            Write-ColorOutput "下载失败: $downloadUrl" -Type "Error"
            exit 1
        }
    }
    Write-ColorOutput "四个程序下载完成" -Type "Success"

    Write-ColorOutput "配置环境变量..." -Type "Info"
    $currentPath = [Environment]::GetEnvironmentVariable("Path", "User")
    if ($currentPath -notlike "*$binDir*") {
        $newPath = if ([string]::IsNullOrWhiteSpace($currentPath)) { $binDir } else { "$currentPath;$binDir" }
        [Environment]::SetEnvironmentVariable("Path", $newPath, "User")
        $env:Path = if ([string]::IsNullOrWhiteSpace($env:Path)) { $binDir } else { "$env:Path;$binDir" }
        Write-ColorOutput "已添加到用户 PATH" -Type "Success"
    } else {
        Write-ColorOutput "PATH 中已存在该目录，跳过" -Type "Info"
    }

    try {
        $installedVersion = & $binaryPath --version 2>&1
        if ($installedVersion -notmatch 'v\d+\.\d+\.\d+') {
            Write-ColorOutput "安装失败：无法验证已安装的二进制文件" -Type "Error"
            exit 1
        }
        Write-ColorOutput "安装成功，版本: $installedVersion" -Type "Success"
    } catch {
        Write-ColorOutput "安装失败：无法执行已下载的二进制文件" -Type "Error"
        exit 1
    }

    Write-Host ""
    Write-ColorOutput "之后可以运行：" -Type "Info"
    Write-Host "  dec --help"
    Write-Host "  dec            # 打开 TUI；Settings 中连接仓库"
    Write-Host ""
}

try {
    Install-Dec
} catch {
    Write-ColorOutput "安装过程中发生错误: $_" -Type "Error"
    exit 1
}
