# Dec 代理一键安装（Windows）
#
# 用法：
#   iwr -useb https://dec.firoyang.com/install.ps1 | iex
#
# 可覆盖的环境变量：
#   DEC_URL               服务地址，默认 https://dec.firoyang.com
#   DEC_AGENT_DIR         安装目录，默认 %LOCALAPPDATA%\Programs\dec
#   DEC_AGENT_NAME        设备名，默认本机计算机名
#   DEC_NONINTERACTIVE    设为 1 时跳过登记（无 TTY 的场合）
#   DEC_INITIALIZE        设为 1 时走 initialize 而不是 enroll（仅首台设备）
#
# 脚本做五件事：下载并校验代理与托盘、停掉旧实例、替换二进制、
# 配置服务地址并登记、注册计划任务与托盘登录自启动。

$ErrorActionPreference = "Stop"
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8

# Windows PowerShell 5.1 默认可能还是 TLS 1.0，连不上服务端。
try {
    [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
} catch {
    # 旧系统上没有 Tls12 枚举就算了，让后续请求自己报错。
}

function Write-Step {
    param([string]$Message)
    Write-Host ""
    Write-Host "==> $Message" -ForegroundColor Cyan
}

function Write-Ok {
    param([string]$Message)
    Write-Host "  OK  $Message" -ForegroundColor Green
}

function Write-Warn {
    param([string]$Message)
    Write-Host "  !!  $Message" -ForegroundColor Yellow
}

function Write-Fail {
    param([string]$Message)
    Write-Host "  XX  $Message" -ForegroundColor Red
}

# Get-Platform 返回 windows-amd64 或 windows-arm64。
#
# [Environment]::Is64BitOperatingSystem 只回答"是不是 64 位"，分不出 ARM64；
# 32 位 PowerShell 跑在 64 位系统上时 PROCESSOR_ARCHITECTURE 也是 x86，
# 真实架构在 PROCESSOR_ARCHITEW6432 里。
function Get-Platform {
    $arch = $env:PROCESSOR_ARCHITECTURE
    if ($env:PROCESSOR_ARCHITEW6432) {
        $arch = $env:PROCESSOR_ARCHITEW6432
    }
    if ([string]::IsNullOrWhiteSpace($arch)) {
        # 极少数精简环境里这个变量是空的，退回到 .NET 的能力探测。
        $arch = if ([Environment]::Is64BitOperatingSystem) { "AMD64" } else { "" }
    }
    switch ($arch.ToUpperInvariant()) {
        "AMD64" { return "windows-amd64" }
        "ARM64" { return "windows-arm64" }
        default {
            Write-Fail "不支持的架构: '$arch'（需要 64 位 Windows）"
            exit 1
        }
    }
}

# Get-RequiredVersion 从下载页抓服务端当前要求的代理版本。
#
# 下载页是无证书公开端点，HTML 里直接写着 required_client。抓不到不算错，
# 说明服务端还没上传发布清单，此时直接装目录里的产物即可。
#
# 正则只锚 ASCII 结构（<code>vX.Y.Z</code>），不匹配中文标签：Windows PowerShell
# 5.1 按 ANSI 解码无 BOM 脚本，中文字面量会变成乱码，一匹配就永远失败。
function Get-RequiredVersion {
    param([string]$BaseUrl)
    try {
        $page = Invoke-WebRequest -Uri "$BaseUrl/download" -UseBasicParsing -TimeoutSec 20
        $m = [regex]::Match($page.Content, '<code>\s*(v[0-9]+\.[0-9]+\.[0-9]+)\s*</code>')
        if ($m.Success) { return $m.Groups[1].Value }
    } catch {
        Write-Warn "读取下载页失败（不影响安装）: $($_.Exception.Message)"
    }
    return ""
}

# Get-InstalledVersion 读本机已装代理的版本，没有或跑不起来时返回空。
#
# `version` 子命令只打印版本号就退出，不会阻塞，所以直接调用即可。
# 用 status 则不行：它在未配置状态下会报错，退出码非 0。
function Get-InstalledVersion {
    param([string]$Exe)
    if (-not (Test-Path $Exe)) { return "" }
    try {
        $out = & $Exe version 2>&1 | Out-String
        $m = [regex]::Match($out, 'v[0-9]+\.[0-9]+\.[0-9]+')
        if ($m.Success) { return $m.Value }
    } catch {
        return ""
    }
    return ""
}

# Save-Verified 下载一个产物并按服务端给出的摘要校验。
#
# 下载响应带 X-Checksum-SHA256 头（服务端从发布清单里算好的），
# 注入式远程安装下静默接受未校验的二进制是不可接受的风险。
# 摘要缺失时告警但不阻断：开发环境的数据目录里没有发布清单。
function Save-Verified {
    param(
        [string]$Url,
        [string]$Dest,
        [switch]$Optional
    )

    $head = Invoke-WebRequest -Uri $Url -Method Head -UseBasicParsing -TimeoutSec 30
    $expected = ""
    # PS5.1 的 Headers 是 Dictionary[string,string]，PS7 是
    # Dictionary[string,string[]]，取值方式不同。
    if ($head.Headers) {
        $raw = $head.Headers["X-Checksum-SHA256"]
        if ($null -ne $raw) {
            if ($raw -is [array]) { $raw = $raw[0] }
            $expected = ([string]$raw).Trim().ToLowerInvariant()
        }
    }

    $tmp = "$Dest.download"
    if (Test-Path $tmp) { Remove-Item -Force $tmp }
    try {
        Invoke-WebRequest -Uri $Url -OutFile $tmp -UseBasicParsing -TimeoutSec 300
    } catch {
        if ($Optional) { return $false }
        throw
    }

    if ($expected) {
        $actual = (Get-FileHash -Path $tmp -Algorithm SHA256).Hash.ToLowerInvariant()
        if ($actual -ne $expected) {
            Remove-Item -Force $tmp -ErrorAction SilentlyContinue
            Write-Fail "产物校验失败: $Url"
            Write-Fail "  期望 sha256: $expected"
            Write-Fail "  实际 sha256: $actual"
            Write-Fail "已删除该产物。请勿使用来源不明的二进制；如确认发布端异常请联系维护者。"
            exit 1
        }
        Write-Ok "校验通过: $(Split-Path -Leaf $Dest)"
    } else {
        Write-Warn "服务端未提供摘要，跳过 $(Split-Path -Leaf $Dest) 的完整性校验"
    }

    Move-Item -Force $tmp $Dest
    return $true
}

# Stop-Existing 停掉正在跑的代理与托盘。
#
# Windows 不能替换正在执行的 exe（文件锁），升级前必须先停。计划任务的
# /End 是优雅结束；残留下来的进程再强制清掉，否则 Move-Item 会失败。
function Stop-Existing {
    foreach ($task in @("DecTray", "DecAgent")) {
        & schtasks.exe /Query /TN $task *> $null
        if ($LASTEXITCODE -eq 0) {
            & schtasks.exe /End /TN $task *> $null
            Write-Ok "已结束计划任务 $task"
        }
    }

    Start-Sleep -Milliseconds 1200

    foreach ($name in @("dec-tray", "dec-agent")) {
        $procs = Get-Process -Name $name -ErrorAction SilentlyContinue
        if ($procs) {
            $procs | Stop-Process -Force -ErrorAction SilentlyContinue
            Write-Ok "已停止残留进程 $name"
        }
    }

    Start-Sleep -Milliseconds 500
}

# Add-ToUserPath 把安装目录写进用户级 PATH（只写一次）。
function Add-ToUserPath {
    param([string]$Dir)
    $current = [Environment]::GetEnvironmentVariable("Path", "User")
    $parts = @()
    if (-not [string]::IsNullOrWhiteSpace($current)) {
        $parts = $current.Split(";") | Where-Object { $_ -ne "" }
    }
    if ($parts -contains $Dir) {
        Write-Ok "PATH 中已存在 $Dir"
        return
    }
    $newPath = if ([string]::IsNullOrWhiteSpace($current)) { $Dir } else { "$current;$Dir" }
    [Environment]::SetEnvironmentVariable("Path", $newPath, "User")
    Write-Ok "已把 $Dir 加入用户 PATH"
}

# Test-Enrolled 判断本机是否已经登记过设备身份。
#
# 直接看身份文件，不解析 `dec-agent status` 的中文输出：那是给人看的文案，
# 措辞一变就误判，而且无 BOM 脚本里的中文本身就是乱码。
function Test-Enrolled {
    param([string]$StateDir)
    return (Test-Path (Join-Path $StateDir "identity\identity.json"))
}

# Invoke-Install 是主流程。
function Invoke-Install {
    Write-Host ""
    Write-Host "  Dec 代理一键安装" -ForegroundColor White
    Write-Host ""

    if ($PSVersionTable.PSVersion.Major -lt 5) {
        Write-Fail "需要 PowerShell 5.0 或更高版本，当前 $($PSVersionTable.PSVersion)"
        exit 1
    }

    $platform = Get-Platform
    $baseUrl = if ($env:DEC_URL) { $env:DEC_URL } else { "https://dec.firoyang.com" }
    $baseUrl = $baseUrl.TrimEnd("/")
    $installDir = if ($env:DEC_AGENT_DIR) { $env:DEC_AGENT_DIR } else { Join-Path $env:LOCALAPPDATA "Programs\dec" }
    $deviceName = if ($env:DEC_AGENT_NAME) { $env:DEC_AGENT_NAME } else { $env:COMPUTERNAME }
    $agentExe = Join-Path $installDir "dec-agent.exe"
    $trayExe = Join-Path $installDir "dec-tray.exe"

    # 托盘只发布 amd64；Windows on ARM 用 x64 仿真跑，不值得多一条工具链。
    $trayPlatform = "windows-amd64"

    Write-Host "  平台      $platform"
    Write-Host "  安装目录  $installDir"
    Write-Host "  服务地址  $baseUrl"
    Write-Host "  设备名    $deviceName"

    # ---- 1. 版本判定 ----------------------------------------------------
    Write-Step "检查版本"
    $required = Get-RequiredVersion -BaseUrl $baseUrl
    $current = Get-InstalledVersion -Exe $agentExe
    if ($required) { Write-Ok "服务端要求版本 $required" } else { Write-Warn "无法获取服务端要求版本" }
    if ($current) { Write-Ok "本机已装版本 $current" } else { Write-Ok "本机未安装" }

    $uptodate = $false
    if ($current -and $required -and ($current -eq $required)) {
        # 版本相同不代表套件完整：托盘可能被手动删掉。
        if ((Test-Path $agentExe) -and (Test-Path $trayExe)) {
            $uptodate = $true
        } else {
            Write-Warn "版本已是最新，但托盘缺失，将补齐"
        }
    }

    # ---- 2. 下载 --------------------------------------------------------
    if (-not $uptodate) {
        Write-Step "下载代理与托盘"
        New-Item -ItemType Directory -Force -Path $installDir | Out-Null
        $staging = Join-Path $installDir ".staging"
        if (Test-Path $staging) { Remove-Item -Recurse -Force $staging }
        New-Item -ItemType Directory -Force -Path $staging | Out-Null

        try {
            Save-Verified -Url "$baseUrl/download/dec-agent-$platform.exe" -Dest (Join-Path $staging "dec-agent.exe") | Out-Null
        } catch {
            Write-Fail "下载代理失败: $($_.Exception.Message)"
            Write-Fail "若本机走代理，先设置 `$env:HTTPS_PROXY 后重试"
            Remove-Item -Recurse -Force $staging -ErrorAction SilentlyContinue
            exit 1
        }

        # 托盘只是可见性组件（需求 7.4）：某次发布没出托盘产物时不能拖垮安装，
        # 代理本身照常工作，只是任务栏没有图标。
        $trayOk = $false
        try {
            $trayOk = Save-Verified -Url "$baseUrl/download/dec-tray-$trayPlatform.exe" `
                -Dest (Join-Path $staging "dec-tray.exe") -Optional
        } catch {
            Write-Warn "托盘下载失败（代理不受影响）: $($_.Exception.Message)"
        }
        if (-not $trayOk) {
            Write-Warn "本次未取得托盘组件，安装后没有任务栏图标"
        }

        # ---- 3. 替换 ----------------------------------------------------
        Write-Step "替换二进制"
        Stop-Existing
        $replaced = @()
        foreach ($name in @("dec-agent.exe", "dec-tray.exe")) {
            $src = Join-Path $staging $name
            if (-not (Test-Path $src)) { continue }
            $dst = Join-Path $installDir $name
            try {
                Move-Item -Force $src $dst
                $replaced += $name
            } catch {
                Write-Fail "替换 $name 失败（可能仍被进程占用）: $($_.Exception.Message)"
                Write-Fail "请注销后重新登录，再重跑本脚本"
                exit 1
            }
        }
        Remove-Item -Recurse -Force $staging -ErrorAction SilentlyContinue

        $installed = Get-InstalledVersion -Exe $agentExe
        if (-not $installed) {
            Write-Fail "安装后无法执行 dec-agent.exe，可能被杀软拦截"
            exit 1
        }
        Write-Ok "已安装 $installed（$($replaced -join '、')）"
    }

    Add-ToUserPath -Dir $installDir
    if (-not ($env:Path -split ";" -contains $installDir)) {
        $env:Path = "$env:Path;$installDir"
    }

    # 状态目录：代理在 Windows 上默认用 %LOCALAPPDATA%\dec-agent。
    $stateDir = if ($env:DEC_AGENT_STATE) { $env:DEC_AGENT_STATE } else { Join-Path $env:LOCALAPPDATA "dec-agent" }

    # ---- 4. 配置与登记 ------------------------------------------------
    Write-Step "配置服务地址"
    # configure 在地址未变时不动本机身份；地址变了才会清掉并要求重新登记。
    & $agentExe configure --url $baseUrl | Out-Null
    if ($LASTEXITCODE -ne 0) {
        Write-Fail "配置服务地址失败"
        exit 1
    }
    Write-Ok "服务地址 $baseUrl"

    $enrolled = Test-Enrolled -StateDir $stateDir
    if ($enrolled) {
        Write-Ok "本机已登记，跳过登记步骤"
    } elseif ($env:DEC_NONINTERACTIVE -eq "1") {
        Write-Warn "非交互模式，跳过登记。稍后手动执行：dec-agent enroll --name $deviceName"
    } else {
        Write-Step "登记这台设备"
        Write-Host "  需要 Bitwarden 邮箱、主密码和两步验证码。" -ForegroundColor DarkGray

        # 凭据在这里收集再经环境变量传下去：iex 场景下脚本自身占着 stdin，
        # dec-agent 从标准输入读会直接读到 EOF。主密码走环境变量而不是命令行
        # 参数，避免留在进程参数里。
        #
        # 控制台本身是可交互的，所以 Read-Host 拿得到输入；密码用
        # SecureString 承接，转明文只为了传给子进程，用完立即清掉。
        $bwEmail = Read-Host "  Bitwarden 邮箱"
        $bwPassword = Read-Host "  Bitwarden 主密码" -AsSecureString
        $bwTotp = Read-Host "  两步验证码（未配置可留空）"

        $plain = [Runtime.InteropServices.Marshal]::PtrToStringAuto(
            [Runtime.InteropServices.Marshal]::SecureStringToBSTR($bwPassword))

        # 首台设备走 initialize：全新服务还没有根密钥，enroll 必然失败。
        $sub = if ($env:DEC_INITIALIZE -eq "1") { "initialize" } else { "enroll" }
        try {
            $env:DEC_BW_PASSWORD = $plain
            & $agentExe $sub --email $bwEmail --totp $bwTotp --name $deviceName
            if ($LASTEXITCODE -ne 0) {
                Write-Warn "登记未完成。可稍后重跑：dec-agent $sub --name $deviceName"
            } else {
                $enrolled = $true
            }
        } catch {
            Write-Warn "登记未完成: $($_.Exception.Message)"
        } finally {
            Remove-Item Env:\DEC_BW_PASSWORD -ErrorAction SilentlyContinue
            $plain = $null
        }
    }

    # ---- 5. 注册服务 --------------------------------------------------
    Write-Step "注册系统服务"
    # 显式传 --binary 与 --tray：install 默认取"当前进程"，从脚本里调用时
    # 那条路径可能不是最终安装位置。
    #
    # 托盘是可选组件，缺失时明确传 --no-tray：否则 install 会在旁边找不到
    # dec-tray 而只打印警告，但那属于"预期失败"，不如直接说清楚。
    if (Test-Path $trayExe) {
        & $agentExe install --binary $agentExe --tray $trayExe
    } else {
        & $agentExe install --binary $agentExe --no-tray
    }
    if ($LASTEXITCODE -ne 0) {
        Write-Fail "注册系统服务失败"
        exit 1
    }

    # ---- 6. 验证 ------------------------------------------------------
    Write-Step "验证"
    $deadline = (Get-Date).AddSeconds(40)
    $status = $null
    while ((Get-Date) -lt $deadline) {
        try {
            $status = Invoke-RestMethod -Uri "http://127.0.0.1:43679/api/status" -TimeoutSec 3
            break
        } catch {
            Start-Sleep -Seconds 2
        }
    }

    if (-not $status) {
        Write-Warn "本机页面尚未响应，服务可能还在启动。查看日志：$(Join-Path $env:LOCALAPPDATA 'dec-agent\logs')"
        exit 1
    }

    Write-Ok "代理在线，服务地址 $($status.server_url)"
    if ($status.device_id) {
        Write-Ok "设备 $($status.device_id)"
    } else {
        Write-Warn "尚未登记设备，执行 dec-agent enroll --name $deviceName 完成接入"
    }
    if ($status.aligned -eq $false) {
        Write-Warn "代理版本与服务端不对齐，请重跑本脚本升级"
    }

    Write-Host ""
    Write-Host "  管理面   http://127.0.0.1:43679/admin" -ForegroundColor White
    Write-Host "  卸载     dec-agent uninstall" -ForegroundColor White
    Write-Host ""
    Write-Host "  提示：二进制未做代码签名，SmartScreen 拦截时点「更多信息 → 仍要运行」。" -ForegroundColor DarkGray
    Write-Host ""
}

try {
    Invoke-Install
} catch {
    Write-Fail "安装过程中发生错误: $($_.Exception.Message)"
    Write-Fail "位置: $($_.InvocationInfo.PositionMessage)"
    exit 1
}
