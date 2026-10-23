<#
.SYNOPSIS
  purge-legacy-dec.ps1 - 断代清理旧一代 Dec 在 Windows 上留下的全部产物。

.DESCRIPTION
  只清理旧一代：Console / dec-server / dec-exec / dec-mcp / %USERPROFILE%\.dec /
  项目内 .dec 与落点 / IDE 里的 dec-* 条目。
  新架构（dec-agent + %USERPROFILE%\.dec-agent）被硬编码保护，一步不碰。

  判定依据：旧架构落点带 dec- 前缀，新架构不带（如 relkit-ops），互不冲突。

  本脚本刻意不使用 param() 块。
  原因：通过 irm <url> | iex 执行时无法传参数，且 param() 会让脚本被当作
  带参函数解析，导致管道执行失败。改用环境变量传参，本机也可用 -Apply。

.EXAMPLE
  # 本机预演
  powershell -ExecutionPolicy Bypass -File .\purge-legacy-dec.ps1

.EXAMPLE
  # 本机执行
  powershell -ExecutionPolicy Bypass -File .\purge-legacy-dec.ps1 -Apply

.EXAMPLE
  # 通过网络即时执行（预演）
  irm http://192.168.1.5:8765/purge-legacy-dec.ps1 | iex

.EXAMPLE
  # 通过网络即时执行（真正删除）
  $env:PURGE_APPLY='1'; irm http://192.168.1.5:8765/purge-legacy-dec.ps1 | iex

.NOTES
  安全设计：
    1. 默认 dry-run。不加 -Apply 或 PURGE_APPLY=1 绝不删除任何东西。
    2. 硬编码保护 .dec-agent，任何情况下都跳过。
    3. 删除前打印待删清单，可核对。
    4. 每个删除动作独立、幂等，中途失败不影响已完成步骤。
    5. 不触碰任何 git 仓库历史。
    6. 不使用 exit，避免管道执行时关掉用户的终端窗口。
#>

$ErrorActionPreference = "Continue"

# ---------------------------------------------------------------------------
# 参数解析：同时支持 -File 传参和环境变量传参
#   - 通过 -File 执行时，参数进入 $args
#   - 通过 irm | iex 执行时无法传参，改用环境变量
# 刻意不使用 param() 块：param() 会让脚本被当作带参函数解析，
# 在 irm <url> | iex 场景下表现不可靠。
# ---------------------------------------------------------------------------
$Apply = $false
$PurgeSecrets = $false
$Workspace = ""

foreach ($a in $args) {
    if ($a -eq '-Apply') { $Apply = $true }
    elseif ($a -eq '-PurgeSecrets') { $PurgeSecrets = $true }
}

# -Workspace 需要一个值，单独处理
for ($i = 0; $i -lt $args.Count; $i++) {
    if ($args[$i] -eq '-Workspace' -and ($i + 1) -lt $args.Count) {
        $Workspace = $args[$i + 1]
    }
}

# 环境变量优先级更高，因为管道执行只能靠它
if ($env:PURGE_APPLY -and $env:PURGE_APPLY -ne "0") { $Apply = $true }
if ($env:PURGE_SECRETS -and $env:PURGE_SECRETS -ne "0") { $PurgeSecrets = $true }
if ($env:DEC_WORKSPACE) { $Workspace = $env:DEC_WORKSPACE }

if (-not $Workspace) {
    $Workspace = Join-Path $env:USERPROFILE "workspace"
}

$Mode = if ($Apply) { "执行" } else { "预演" }
$script:Removed = 0
$script:Skipped = 0
$script:Failed = 0

$ProtectedRoots = @(
    (Join-Path $env:USERPROFILE ".dec-agent")
)

function Test-ProtectedPath {
    param([string]$Path)
    if (-not $Path) { return $false }
    if ($Path -like "*dec-agent*") { return $true }
    $full = [System.IO.Path]::GetFullPath($Path)
    foreach ($p in $ProtectedRoots) {
        $pf = [System.IO.Path]::GetFullPath($p)
        if ($full -eq $pf) { return $true }
        if ($full.StartsWith($pf + [System.IO.Path]::DirectorySeparatorChar)) { return $true }
    }
    return $false
}

function Remove-LegacyPath {
    param([string]$Label, [string]$Path)

    if (-not (Test-Path -LiteralPath $Path)) { return }

    if (Test-ProtectedPath -Path $Path) {
        Write-Host "  [保护] $Label -> $Path"
        $script:Skipped++
        return
    }

    if (-not $Apply) {
        Write-Host "  [待删] $Label -> $Path"
        $script:Removed++
        return
    }

    try {
        Remove-Item -LiteralPath $Path -Recurse -Force -ErrorAction Stop
        Write-Host "  [已删] $Label -> $Path"
        $script:Removed++
    }
    catch {
        Write-Host "  [失败] $Label -> $Path : $($_.Exception.Message)" -ForegroundColor Red
        $script:Failed++
    }
}

Write-Host "=== 旧一代 Dec 清理（$Mode）==="
Write-Host "工作区: $Workspace"
Write-Host ""

# ---------------------------------------------------------------------------
# 1. 旧进程
# ---------------------------------------------------------------------------
Write-Host "--- 1. 旧进程 ---"

$newAgentIds = @()
$newAgent = Get-Process -Name "dec-agent" -ErrorAction SilentlyContinue
if ($newAgent) { $newAgentIds = @($newAgent | ForEach-Object { $_.Id }) }

$legacyNames = @("dec-console", "dec-server", "dec-exec", "dec-host-setup", "dec-mcp")
$legacyProcs = @()
foreach ($n in $legacyNames) {
    $procs = Get-Process -Name $n -ErrorAction SilentlyContinue
    if ($procs) { $legacyProcs += $procs }
}
# 按可执行文件路径兜底匹配 ~\.dec\ 下的进程
Get-Process -ErrorAction SilentlyContinue | Where-Object {
    $_.Path -and ($_.Path -like "*\.dec\bin\*")
} | ForEach-Object {
    if ($legacyProcs.Id -notcontains $_.Id) { $legacyProcs += $_ }
}
$legacyProcs = $legacyProcs | Sort-Object Id -Unique

if (-not $legacyProcs) {
    Write-Host "  没有发现旧进程"
}
else {
    foreach ($p in $legacyProcs) {
        if ($newAgentIds -contains $p.Id) {
            Write-Host "  [保护] 跳过新代理 PID $($p.Id)"
            $script:Skipped++
            continue
        }
        if (-not $Apply) {
            Write-Host "  [待停] PID $($p.Id) $($p.ProcessName)"
        }
        else {
            try {
                Stop-Process -Id $p.Id -Force -ErrorAction Stop
                Write-Host "  [已停] PID $($p.Id) $($p.ProcessName)"
            }
            catch {
                Write-Host "  [失败] 无法停止 PID $($p.Id): $($_.Exception.Message)" -ForegroundColor Red
                $script:Failed++
            }
        }
    }
}
Write-Host ""

# ---------------------------------------------------------------------------
# 2. 旧全局状态
# ---------------------------------------------------------------------------
Write-Host "--- 2. 旧全局状态 ---"
Remove-LegacyPath "旧全局状态 ~\.dec" (Join-Path $env:USERPROFILE ".dec")
Write-Host ""

# ---------------------------------------------------------------------------
# 3. 旧 Console 应用与数据
# ---------------------------------------------------------------------------
Write-Host "--- 3. 旧 Console ---"
$consoleCandidates = @(
    (Join-Path $env:LOCALAPPDATA "Programs\dec-console"),
    (Join-Path $env:APPDATA "com.shichao402.dec.console"),
    (Join-Path $env:LOCALAPPDATA "com.shichao402.dec.console"),
    (Join-Path $env:APPDATA "dec-console"),
    (Join-Path $env:LOCALAPPDATA "dec-console"),
    (Join-Path $env:APPDATA "dec-server"),
    (Join-Path $env:LOCALAPPDATA "dec-server")
)
foreach ($c in $consoleCandidates) {
    Remove-LegacyPath "Console 数据" $c
}

# 开始菜单快捷方式（只删 Dec 相关的，不碰其他）
$startMenu = Join-Path $env:APPDATA "Microsoft\Windows\Start Menu\Programs"
if (Test-Path -LiteralPath $startMenu) {
    Get-ChildItem -LiteralPath $startMenu -Filter "*Dec*" -ErrorAction SilentlyContinue |
        ForEach-Object { Remove-LegacyPath "开始菜单快捷方式" $_.FullName }
}
Write-Host ""

# ---------------------------------------------------------------------------
# 4. 项目内 .dec
# ---------------------------------------------------------------------------
Write-Host "--- 4. 项目内 .dec 目录 ---"
if (Test-Path -LiteralPath $Workspace) {
    $count = 0
    Get-ChildItem -LiteralPath $Workspace -Directory -ErrorAction SilentlyContinue |
        ForEach-Object {
            $dotdec = Join-Path $_.FullName ".dec"
            if (Test-Path -LiteralPath $dotdec) {
                $count++
                Remove-LegacyPath "项目 .dec" $dotdec
            }
        }
    if ($count -eq 0) { Write-Host "  没有发现 .dec 目录" }
    else { Write-Host "  （共 $count 个）" }
}
else {
    Write-Host "  工作区不存在: $Workspace"
}
Write-Host ""

# ---------------------------------------------------------------------------
# 5. 项目内 .secrets
# ---------------------------------------------------------------------------
Write-Host "--- 5. 项目内 .secrets 目录 ---"
$secretDirs = @()
if (Test-Path -LiteralPath $Workspace) {
    Get-ChildItem -LiteralPath $Workspace -Directory -ErrorAction SilentlyContinue |
        ForEach-Object {
            $s = Join-Path $_.FullName ".secrets"
            if (Test-Path -LiteralPath $s) { $secretDirs += $s }
        }
}

if ($secretDirs.Count -eq 0) {
    Write-Host "  没有发现 .secrets\"
}
elseif (-not $PurgeSecrets) {
    Write-Host "  [保留] 发现 $($secretDirs.Count) 个 .secrets\，默认不删（里面是凭据）。"
    Write-Host "         确认要删请加 -PurgeSecrets 或设置 `$env:PURGE_SECRETS='1'。"
    $script:Skipped += $secretDirs.Count
}
else {
    foreach ($s in $secretDirs) {
        Remove-LegacyPath "项目 .secrets" $s
    }
}
Write-Host ""

# ---------------------------------------------------------------------------
# 6. IDE 落点（dec- 前缀）
# ---------------------------------------------------------------------------
Write-Host "--- 6. IDE 落点（dec- 前缀）---"
if (Test-Path -LiteralPath $Workspace) {
    $ideDirs = @(
        ".cursor\skills", ".claude\skills", ".codex\skills",
        ".cursor\rules",  ".claude\rules",  ".codex\rules"
    )
    $found = 0
    Get-ChildItem -LiteralPath $Workspace -Directory -ErrorAction SilentlyContinue |
        ForEach-Object {
            $proj = $_
            foreach ($rel in $ideDirs) {
                $dir = Join-Path $proj.FullName $rel
                if (-not (Test-Path -LiteralPath $dir)) { continue }
                Get-ChildItem -LiteralPath $dir -ErrorAction SilentlyContinue |
                    Where-Object { $_.Name -like "dec-*" } |
                    ForEach-Object {
                        $found++
                        Remove-LegacyPath "旧落点" $_.FullName
                    }
            }
        }
    if ($found -eq 0) { Write-Host "  没有发现 dec- 前缀落点" }
}
else {
    Write-Host "  工作区不存在: $Workspace"
}
Write-Host ""

# ---------------------------------------------------------------------------
# 7. MCP 配置中的 dec-* 条目
# ---------------------------------------------------------------------------
Write-Host "--- 7. MCP 配置中的 dec-* 条目 ---"
$mcpRels = @(".cursor\mcp.json", ".claude\mcp.json", ".mcp.json")

if (Test-Path -LiteralPath $Workspace) {
    Get-ChildItem -LiteralPath $Workspace -Directory -ErrorAction SilentlyContinue |
        ForEach-Object {
            $proj = $_

            foreach ($rel in $mcpRels) {
                $file = Join-Path $proj.FullName $rel
                if (-not (Test-Path -LiteralPath $file)) { continue }

                try {
                    $json = Get-Content -LiteralPath $file -Raw -Encoding UTF8 | ConvertFrom-Json
                }
                catch {
                    Write-Host "  [跳过] $file : 无法解析"
                    return
                }

                $containerKey = $null
                foreach ($k in @("mcpServers", "mcp_servers")) {
                    if ($json.PSObject.Properties.Name -contains $k) { $containerKey = $k; break }
                }
                if (-not $containerKey) { Write-Host "  [无] $file"; return }

                $container = $json.$containerKey
                $keys = @($container.PSObject.Properties.Name | Where-Object { $_ -like "dec-*" })
                if ($keys.Count -eq 0) { Write-Host "  [无] $file"; return }

                if (-not $Apply) {
                    Write-Host "  [待删] $file : $($keys -join ', ')"
                    $script:Removed++
                    return
                }

                foreach ($k in $keys) {
                    $container.PSObject.Properties.Remove($k)
                }
                try {
                    $out = $json | ConvertTo-Json -Depth 20
                    # 用 WriteAllText 而非 Set-Content：PS 5.1 的 -Encoding UTF8 会写 BOM，
                    # 带 BOM 的 JSON 会让部分解析器失败。
                    $utf8NoBom = New-Object System.Text.UTF8Encoding($false)
                    [System.IO.File]::WriteAllText($file, $out, $utf8NoBom)
                    Write-Host "  [已删] $file : $($keys -join ', ')"
                    $script:Removed++
                }
                catch {
                    Write-Host "  [失败] $file : $($_.Exception.Message)" -ForegroundColor Red
                    $script:Failed++
                }
            }

            # Codex 用 TOML，不自动改写以免破坏格式，只提示。
            $toml = Join-Path $proj.FullName ".codex\config.toml"
            if (Test-Path -LiteralPath $toml) {
                $content = Get-Content -LiteralPath $toml -Raw -ErrorAction SilentlyContinue
                if ($content -match "(?m)^\s*\[mcp_servers\.dec-") {
                    Write-Host "  [需手工] $toml 含 [mcp_servers.dec-*] 段"
                }
            }
        }
}
else {
    Write-Host "  工作区不存在: $Workspace"
}
Write-Host ""

# ---------------------------------------------------------------------------
# 8. 旧计划任务
# ---------------------------------------------------------------------------
Write-Host "--- 8. 旧计划任务 ---"
if (-not (Get-Command Get-ScheduledTask -ErrorAction SilentlyContinue)) {
    Write-Host "  当前系统没有计划任务功能，跳过"
    Write-Host ""
}
else {
    $tasks = Get-ScheduledTask -ErrorAction SilentlyContinue |
        Where-Object { $_.TaskName -like "*dec*" -or $_.TaskName -like "*Dec*" }

if (-not $tasks) {
    Write-Host "  没有发现旧计划任务"
}
else {
    foreach ($t in $tasks) {
        if ($t.TaskName -like "*dec-agent*") {
            Write-Host "  [保护] 跳过新代理任务 $($t.TaskName)"
            $script:Skipped++
            continue
        }
        if (-not $Apply) {
            Write-Host "  [待删] 计划任务 $($t.TaskName)"
            $script:Removed++
        }
        else {
            try {
                Unregister-ScheduledTask -TaskName $t.TaskName -Confirm:$false -ErrorAction Stop
                Write-Host "  [已删] 计划任务 $($t.TaskName)"
                $script:Removed++
            }
            catch {
                Write-Host "  [失败] 计划任务 $($t.TaskName): $($_.Exception.Message)" -ForegroundColor Red
                $script:Failed++
            }
        }
    }
    }
}
Write-Host ""

# ---------------------------------------------------------------------------
# 汇总
# ---------------------------------------------------------------------------
Write-Host "=== 汇总 ==="
Write-Host "  ${Mode}条目: $($script:Removed)"
Write-Host "  保护/跳过: $($script:Skipped)"
if ($script:Failed -gt 0) {
    Write-Host "  失败: $($script:Failed)" -ForegroundColor Red
}

if (-not $Apply) {
    Write-Host ""
    Write-Host "以上为预演，未做任何修改。确认无误后加 -Apply 执行："
    Write-Host '  $env:PURGE_APPLY=''1''; irm <url> | iex'
}
else {
    Write-Host ""
    Write-Host "清理完成。保留："
    Write-Host "  $env:USERPROFILE\.dec-agent\"
}

# 不使用 exit，避免管道执行时关闭用户终端。
