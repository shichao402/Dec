param(
    [switch]$Yes
)

# Dec 卸载脚本 (Windows PowerShell)
# 使用方法: iwr -useb https://raw.githubusercontent.com/shichao402/Dec/ReleaseLatest/scripts/uninstall.ps1 | iex

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

function Uninstall-Dec {
    Write-Host ""
    Write-Host "╔═══════════════════════════════════════╗"
    Write-Host "║           Dec 卸载脚本               ║"
    Write-Host "╚═══════════════════════════════════════╝"
    Write-Host ""

    $installDir = if ($env:DEC_HOME) { $env:DEC_HOME } else { Join-Path $env:USERPROFILE ".dec" }
    $binDir = Join-Path $installDir "bin"
    $binaryPath = Join-Path $binDir "dec.exe"
    $systemConfigPath = Join-Path $installDir "config\system.json"
    $globalConfigPath = Join-Path $installDir "config.yaml"
    $vaultDir = Join-Path $installDir "vault"

    if (-not (Test-Path $installDir) -and -not (Test-Path $binaryPath)) {
        Write-ColorOutput "未找到安装目录: $installDir" -Type "Warning"
        return
    }

    Write-ColorOutput "这将删除整个 Dec 根目录，包括：" -Type "Warning"
    Write-Host "  - 可执行文件: $binaryPath"
    Write-Host "  - 系统配置: $systemConfigPath"
    Write-Host "  - 全局配置: $globalConfigPath"
    Write-Host "  - 本地 Vault: $vaultDir"
    Write-Host ""

    if (-not $Yes) {
        $confirm = Read-Host "确定要卸载 Dec 吗？(y/N)"
        if ($confirm -ne "y" -and $confirm -ne "Y") {
            Write-ColorOutput "已取消卸载" -Type "Info"
            return
        }
    } else {
        Write-ColorOutput "--Yes 模式，跳过确认" -Type "Info"
    }

    try {
        $currentPath = [Environment]::GetEnvironmentVariable("Path", "User")
        if ($currentPath -like "*$binDir*") {
            $newPath = (($currentPath -split ';') | Where-Object { $_ -and $_ -ne $binDir -and $_ -ne "$binDir\" }) -join ';'
            [Environment]::SetEnvironmentVariable("Path", $newPath, "User")
            $env:Path = (($env:Path -split ';') | Where-Object { $_ -and $_ -ne $binDir -and $_ -ne "$binDir\" }) -join ';'
            Write-ColorOutput "已从用户 PATH 移除 $binDir" -Type "Success"
        }
    } catch {
        Write-ColorOutput "清理 PATH 时发生警告: $_" -Type "Warning"
    }

    if (Test-Path $installDir) {
        Remove-Item -Path $installDir -Recurse -Force
        Write-ColorOutput "已删除: $installDir" -Type "Success"
    }

    Write-Host ""
    Write-ColorOutput "请重新打开 PowerShell 窗口以使环境变量更改生效" -Type "Info"
}

try {
    Uninstall-Dec
} catch {
    Write-ColorOutput "卸载过程中发生错误: $_" -Type "Error"
    exit 1
}
