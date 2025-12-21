# Dec 卸载脚本 (Windows PowerShell)
# 使用方法: iwr -useb https://raw.githubusercontent.com/shichao402/Dec/ReleaseLatest/uninstall.ps1 | iex

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

# 主卸载函数
function Uninstall-Dec {
    Write-Host ""
    Write-Host "╔═══════════════════════════════════════╗"
    Write-Host "║   Dec 卸载脚本              ║"
    Write-Host "╚═══════════════════════════════════════╝"
    Write-Host ""
    
    # 定义安装路径（使用新的目录结构）
    if ($env:DEC_HOME) {
        $installDir = $env:DEC_HOME
    } else {
        $installDir = Join-Path $env:USERPROFILE ".decs"
    }
    $binDir = Join-Path $installDir "bin"
    $configDir = Join-Path $installDir "config"
    $reposDir = Join-Path $installDir "repos"
    $binaryPath = Join-Path $binDir "dec.exe"
    
    # 检查是否已安装
    if (-not (Test-Path $installDir) -and -not (Test-Path $binaryPath)) {
        Write-ColorOutput "未找到安装目录: $installDir" -Type "Warning"
        Write-ColorOutput "Dec 可能未安装或已卸载" -Type "Info"
        return
    }
    
    Write-ColorOutput "找到安装目录: $installDir" -Type "Info"
    
    # 确认卸载
    Write-Host ""
    Write-ColorOutput "这将删除以下内容：" -Type "Warning"
    Write-Host "  - 安装目录: $installDir"
    Write-Host "  - 可执行文件: $binaryPath"
    Write-Host "  - 配置文件: $(Join-Path $configDir 'available-toolsets.json')"
    Write-Host "  - 工具集仓库: $reposDir"
    Write-Host ""
    $confirm = Read-Host "确定要卸载 Dec 吗？(y/N)"
    
    if ($confirm -ne "y" -and $confirm -ne "Y") {
        Write-ColorOutput "取消卸载" -Type "Info"
        return
    }
    
    # 从 PATH 中移除
    Write-ColorOutput "清理环境变量配置..." -Type "Info"
    
    try {
        $currentPath = [Environment]::GetEnvironmentVariable("Path", "User")
        
        if ($currentPath -like "*$binDir*") {
            # 移除 binDir 路径
            $pathParts = $currentPath -split ';'
            $newPathParts = $pathParts | Where-Object { $_ -ne $binDir -and $_ -ne "$binDir\" }
            $newPath = $newPathParts -join ';'
            
            [Environment]::SetEnvironmentVariable("Path", $newPath, "User")
            
            # 更新当前会话的 PATH
            $env:Path = ($env:Path -split ';' | Where-Object { $_ -ne $binDir -and $_ -ne "$binDir\" }) -join ';'
            
            Write-ColorOutput "已从用户 PATH 移除" -Type "Success"
        } else {
            Write-ColorOutput "PATH 中未找到相关配置" -Type "Info"
        }
    } catch {
        Write-ColorOutput "清理 PATH 时出错: $_" -Type "Warning"
    }
    
    # 删除安装目录
    Write-ColorOutput "删除安装目录..." -Type "Info"
    if (Test-Path $installDir) {
        try {
            Remove-Item -Path $installDir -Recurse -Force -ErrorAction Stop
            Write-ColorOutput "已删除: $installDir" -Type "Success"
        } catch {
            Write-ColorOutput "删除目录时出错: $_" -Type "Error"
            Write-ColorOutput "请手动删除: $installDir" -Type "Warning"
            return
        }
    }
    
    # 检查 .cursor\toolsets 目录是否为空，如果为空则删除
    $cursorToolsetsDir = Join-Path $env:USERPROFILE ".cursor\toolsets"
    if (Test-Path $cursorToolsetsDir) {
        $items = Get-ChildItem -Path $cursorToolsetsDir -ErrorAction SilentlyContinue
        if ($null -eq $items -or $items.Count -eq 0) {
            Write-ColorOutput "清理空目录..." -Type "Info"
            try {
                Remove-Item -Path $cursorToolsetsDir -Force -ErrorAction SilentlyContinue
                $cursorDir = Join-Path $env:USERPROFILE ".cursor"
                $cursorItems = Get-ChildItem -Path $cursorDir -ErrorAction SilentlyContinue
                if ($null -eq $cursorItems -or $cursorItems.Count -eq 0) {
                    Remove-Item -Path $cursorDir -Force -ErrorAction SilentlyContinue
                }
            } catch {
                # 忽略错误
            }
        }
    }
    
    Write-Host ""
    Write-Host "╔═══════════════════════════════════════╗"
    Write-Host "║         卸载完成！                    ║"
    Write-Host "╚═══════════════════════════════════════╝"
    Write-Host ""
    Write-ColorOutput "Dec 已成功卸载" -Type "Success"
    Write-Host ""
    Write-ColorOutput "请重新打开 PowerShell 窗口以使环境变量更改生效" -Type "Info"
    Write-Host ""
}

# 运行主函数
try {
    Uninstall-Dec
} catch {
    Write-ColorOutput "卸载过程中发生错误: $_" -Type "Error"
    exit 1
}



