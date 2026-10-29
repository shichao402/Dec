# Dec agent one-line installer (Windows)
#
# Usage:
#   iwr -useb https://dec.firoyang.com/install.ps1 | iex
#
# Overridable environment variables:
#   DEC_URL             service URL, default https://dec.firoyang.com
#   DEC_AGENT_DIR       install dir, default %LOCALAPPDATA%\Programs\dec
#   DEC_AGENT_NAME      device name, default %COMPUTERNAME%
#   DEC_NONINTERACTIVE  set to 1 to skip enrollment (no TTY)
#   DEC_INITIALIZE      set to 1 to use initialize instead of enroll (first device only)
#
# The script does five things: download and verify the agent and tray, stop old
# instances, replace the binaries, configure the service URL and enroll, and
# register the scheduled task and tray auto-start.
#
# WHY THIS FILE MUST STAY PURE ASCII - do not add non-ASCII characters, not even
# in comments:
#
# It is delivered by `iwr ... | iex`. Invoke-WebRequest decodes the response
# into a .NET string before Invoke-Expression ever sees it, and on Windows
# PowerShell 5.1 that decoding is not reliably UTF-8 - depending on version and
# headers it falls back to ISO-8859-1 or the console ANSI codepage. When that
# happens the UTF-8 BOM turns into three junk characters in front of the leading
# '#', the comment stops being a comment, and PowerShell reports the whole first
# line as an unknown cmdlet; every CJK byte then renders as '?'.
#
# Pure ASCII decodes identically under every codepage, so the output cannot be
# corrupted no matter how the host decodes the response. This is why there is no
# BOM either: it would survive as a character and break line 1 all over again.
# scripts/install.sh keeps its Chinese text - Unix terminals are UTF-8 by
# default and have no equivalent problem.

$ErrorActionPreference = "Stop"
# Helps the child dec-agent's own output on consoles that can do UTF-8. Our own
# messages are ASCII, so they render correctly regardless.
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8

# Windows PowerShell 5.1 may still default to TLS 1.0, which the server refuses.
try {
    [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
} catch {
    # Old systems lack the Tls12 enum; let the request fail on its own.
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

# Get-Platform returns windows-amd64 or windows-arm64.
#
# [Environment]::Is64BitOperatingSystem only answers "is it 64-bit" and cannot
# tell ARM64 apart; on 32-bit PowerShell running on a 64-bit OS,
# PROCESSOR_ARCHITECTURE is x86 too - the real architecture sits in
# PROCESSOR_ARCHITEW6432.
function Get-Platform {
    $arch = $env:PROCESSOR_ARCHITECTURE
    if ($env:PROCESSOR_ARCHITEW6432) {
        $arch = $env:PROCESSOR_ARCHITEW6432
    }
    if ([string]::IsNullOrWhiteSpace($arch)) {
        # Rare trimmed-down environments leave this empty; fall back to .NET.
        $arch = if ([Environment]::Is64BitOperatingSystem) { "AMD64" } else { "" }
    }
    switch ($arch.ToUpperInvariant()) {
        "AMD64" { return "windows-amd64" }
        "ARM64" { return "windows-arm64" }
        default {
            Write-Fail "Unsupported architecture: '$arch' (64-bit Windows required)"
            exit 1
        }
    }
}

# Get-RequiredVersion scrapes the agent version the server currently requires.
#
# The download page is the cert-free public endpoint and states required_client
# directly in its HTML. Failing to read it is not an error - it means the server
# has no release manifest yet, in which case we just install what is there.
function Get-RequiredVersion {
    param([string]$BaseUrl)
    try {
        $page = Invoke-WebRequest -Uri "$BaseUrl/download" -UseBasicParsing -TimeoutSec 20
        $m = [regex]::Match($page.Content, '<code>\s*(v[0-9]+\.[0-9]+\.[0-9]+)\s*</code>')
        if ($m.Success) { return $m.Groups[1].Value }
    } catch {
        Write-Warn "Could not read the download page (install continues): $($_.Exception.Message)"
    }
    return ""
}

# Get-InstalledVersion reads the version of the locally installed agent, or ""
# when it is missing or will not run.
#
# The `version` subcommand prints the version and exits, so it never blocks.
# `status` would not do: it errors out in an unconfigured state and exits non-0.
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

# Save-Verified downloads one artifact and checks it against the server's digest.
#
# The response carries an X-Checksum-SHA256 header computed from the release
# manifest; silently accepting an unverified binary in a remote-exec installer
# is not an acceptable risk. A missing digest warns but does not block: dev
# environments have no manifest in the data directory.
function Save-Verified {
    param(
        [string]$Url,
        [string]$Dest,
        [switch]$Optional
    )

    $head = Invoke-WebRequest -Uri $Url -Method Head -UseBasicParsing -TimeoutSec 30
    $expected = ""
    # PS5.1 exposes Headers as Dictionary[string,string], PS7 as
    # Dictionary[string,string[]], so the value is read differently.
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
            Write-Fail "Checksum mismatch: $Url"
            Write-Fail "  expected sha256: $expected"
            Write-Fail "  actual sha256:   $actual"
            Write-Fail "The file was deleted. Do not run binaries from unknown sources; contact the maintainer if the release is broken."
            exit 1
        }
        Write-Ok "Verified: $(Split-Path -Leaf $Dest)"
    } else {
        Write-Warn "Server sent no checksum; skipping integrity check for $(Split-Path -Leaf $Dest)"
    }

    Move-Item -Force $tmp $Dest
    return $true
}

# Stop-Existing stops the running agent and tray.
#
# Windows cannot replace a running exe (file lock), so it must be stopped before
# upgrading. `schtasks /End` is the graceful path; leftovers are then killed,
# otherwise Move-Item fails.
function Stop-Existing {
    foreach ($task in @("DecTray", "DecAgent")) {
        & schtasks.exe /Query /TN $task *> $null
        if ($LASTEXITCODE -eq 0) {
            & schtasks.exe /End /TN $task *> $null
            Write-Ok "Stopped scheduled task $task"
        }
    }

    Start-Sleep -Milliseconds 1200

    foreach ($name in @("dec-tray", "dec-agent")) {
        $procs = Get-Process -Name $name -ErrorAction SilentlyContinue
        if ($procs) {
            $procs | Stop-Process -Force -ErrorAction SilentlyContinue
            Write-Ok "Stopped leftover process $name"
        }
    }

    Start-Sleep -Milliseconds 500
}

# Add-ToUserPath writes the install dir into the user-level PATH (once).
function Add-ToUserPath {
    param([string]$Dir)
    $current = [Environment]::GetEnvironmentVariable("Path", "User")
    $parts = @()
    if (-not [string]::IsNullOrWhiteSpace($current)) {
        $parts = $current.Split(";") | Where-Object { $_ -ne "" }
    }
    if ($parts -contains $Dir) {
        Write-Ok "$Dir is already in PATH"
        return
    }
    $newPath = if ([string]::IsNullOrWhiteSpace($current)) { $Dir } else { "$current;$Dir" }
    [Environment]::SetEnvironmentVariable("Path", $newPath, "User")
    Write-Ok "Added $Dir to the user PATH"
}

# Test-Enrolled reports whether this machine already has a device identity.
#
# It inspects the identity file rather than parsing `dec-agent status` output:
# that text is written for humans, so any wording change would mislead us here.
function Test-Enrolled {
    param([string]$StateDir)
    return (Test-Path (Join-Path $StateDir "identity\identity.json"))
}

# Invoke-Install is the main flow.
function Invoke-Install {
    Write-Host ""
    Write-Host "  Dec agent installer" -ForegroundColor White
    Write-Host ""

    if ($PSVersionTable.PSVersion.Major -lt 5) {
        Write-Fail "PowerShell 5.0 or newer is required, found $($PSVersionTable.PSVersion)"
        exit 1
    }

    $platform = Get-Platform
    $baseUrl = if ($env:DEC_URL) { $env:DEC_URL } else { "https://dec.firoyang.com" }
    $baseUrl = $baseUrl.TrimEnd("/")
    $installDir = if ($env:DEC_AGENT_DIR) { $env:DEC_AGENT_DIR } else { Join-Path $env:LOCALAPPDATA "Programs\dec" }
    $deviceName = if ($env:DEC_AGENT_NAME) { $env:DEC_AGENT_NAME } else { $env:COMPUTERNAME }
    $agentExe = Join-Path $installDir "dec-agent.exe"
    $trayExe = Join-Path $installDir "dec-tray.exe"

    # The tray is amd64-only; Windows on ARM runs it under x64 emulation, which
    # is not worth a second toolchain.
    $trayPlatform = "windows-amd64"

    Write-Host "  Platform   $platform"
    Write-Host "  Install to $installDir"
    Write-Host "  Server     $baseUrl"
    Write-Host "  Device     $deviceName"

    # ---- 1. Versions ----------------------------------------------------
    Write-Step "Checking versions"
    $required = Get-RequiredVersion -BaseUrl $baseUrl
    $current = Get-InstalledVersion -Exe $agentExe
    if ($required) { Write-Ok "Server requires version $required" } else { Write-Warn "Could not read the server's required version" }
    if ($current) { Write-Ok "Installed version $current" } else { Write-Ok "Not installed yet" }

    $uptodate = $false
    if ($current -and $required -and ($current -eq $required)) {
        # Same version does not mean a complete set: the tray may have been deleted.
        if ((Test-Path $agentExe) -and (Test-Path $trayExe)) {
            $uptodate = $true
        } else {
            Write-Warn "Already up to date, but the tray is missing; installing it"
        }
    }

    # ---- 2. Download ----------------------------------------------------
    if (-not $uptodate) {
        Write-Step "Downloading agent and tray"
        New-Item -ItemType Directory -Force -Path $installDir | Out-Null
        $staging = Join-Path $installDir ".staging"
        if (Test-Path $staging) { Remove-Item -Recurse -Force $staging }
        New-Item -ItemType Directory -Force -Path $staging | Out-Null

        try {
            Save-Verified -Url "$baseUrl/download/dec-agent-$platform.exe" -Dest (Join-Path $staging "dec-agent.exe") | Out-Null
        } catch {
            Write-Fail "Failed to download the agent: $($_.Exception.Message)"
            Write-Fail "If this machine uses a proxy, set `$env:HTTPS_PROXY and retry"
            Remove-Item -Recurse -Force $staging -ErrorAction SilentlyContinue
            exit 1
        }

        # The tray is a visibility-only component: a release without a tray
        # artifact must not sink the whole install - the agent still works, it
        # just has no taskbar icon.
        $trayOk = $false
        try {
            $trayOk = Save-Verified -Url "$baseUrl/download/dec-tray-$trayPlatform.exe" `
                -Dest (Join-Path $staging "dec-tray.exe") -Optional
        } catch {
            Write-Warn "Tray download failed (the agent is unaffected): $($_.Exception.Message)"
        }
        if (-not $trayOk) {
            Write-Warn "Tray component unavailable; no taskbar icon after install"
        }

        # ---- 3. Replace -------------------------------------------------
        Write-Step "Replacing binaries"
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
                Write-Fail "Failed to replace $name (may still be in use): $($_.Exception.Message)"
                Write-Fail "Sign out and back in, then run this script again"
                exit 1
            }
        }
        Remove-Item -Recurse -Force $staging -ErrorAction SilentlyContinue

        $installed = Get-InstalledVersion -Exe $agentExe
        if (-not $installed) {
            Write-Fail "dec-agent.exe cannot be executed after install; antivirus may be blocking it"
            exit 1
        }
        Write-Ok "Installed $installed ($($replaced -join ', '))"
    }

    Add-ToUserPath -Dir $installDir
    if (-not ($env:Path -split ";" -contains $installDir)) {
        $env:Path = "$env:Path;$installDir"
    }

    # State dir: on Windows the agent defaults to %LOCALAPPDATA%\dec-agent.
    $stateDir = if ($env:DEC_AGENT_STATE) { $env:DEC_AGENT_STATE } else { Join-Path $env:LOCALAPPDATA "dec-agent" }

    # ---- 4. Configure and enroll --------------------------------------
    Write-Step "Configuring the service URL"
    # configure leaves the local identity alone when the URL is unchanged; it
    # only clears it and demands re-enrollment when the URL actually differs.
    & $agentExe configure --url $baseUrl | Out-Null
    if ($LASTEXITCODE -ne 0) {
        Write-Fail "Failed to configure the service URL"
        exit 1
    }
    Write-Ok "Service URL $baseUrl"

    $enrolled = Test-Enrolled -StateDir $stateDir
    if ($enrolled) {
        Write-Ok "This device is already enrolled; skipping enrollment"
    } elseif ($env:DEC_NONINTERACTIVE -eq "1") {
        Write-Warn "Non-interactive mode, skipping enrollment. Run later: dec-agent enroll --name $deviceName"
    } else {
        Write-Step "Enrolling this device"
        Write-Host "  You will need your Bitwarden email, master password and 2FA code." -ForegroundColor DarkGray

        # Credentials are collected here and passed down through the environment:
        # under `iex` the script itself owns stdin, so dec-agent reading stdin
        # would hit EOF immediately. The master password travels as an env var
        # rather than a command-line argument to keep it out of process listings.
        #
        # The console itself is interactive, so Read-Host gets real input; the
        # password is held as a SecureString and converted to plain text only to
        # hand it to the child process, then cleared at once.
        $bwEmail = Read-Host "  Bitwarden email"
        $bwPassword = Read-Host "  Bitwarden master password" -AsSecureString
        $bwTotp = Read-Host "  2FA code (leave blank if not configured)"

        $plain = [Runtime.InteropServices.Marshal]::PtrToStringAuto(
            [Runtime.InteropServices.Marshal]::SecureStringToBSTR($bwPassword))

        # The first device uses initialize: a fresh service has no root key yet,
        # so enroll could only fail.
        $sub = if ($env:DEC_INITIALIZE -eq "1") { "initialize" } else { "enroll" }
        try {
            $env:DEC_BW_PASSWORD = $plain
            & $agentExe $sub --email $bwEmail --totp $bwTotp --name $deviceName
            if ($LASTEXITCODE -ne 0) {
                Write-Warn "Enrollment did not finish. Rerun later: dec-agent $sub --name $deviceName"
            } else {
                $enrolled = $true
            }
        } catch {
            Write-Warn "Enrollment did not finish: $($_.Exception.Message)"
        } finally {
            Remove-Item Env:\DEC_BW_PASSWORD -ErrorAction SilentlyContinue
            $plain = $null
        }
    }

    # ---- 5. Register the service --------------------------------------
    Write-Step "Registering the system service"
    # --binary and --tray are passed explicitly: install defaults to "the
    # current process", which from inside this script may not be the final
    # install location.
    #
    # The tray is optional, so when it is missing we say --no-tray outright:
    # otherwise install would fail to find dec-tray and merely warn, which reads
    # as an unexpected failure rather than a declared choice.
    if (Test-Path $trayExe) {
        & $agentExe install --binary $agentExe --tray $trayExe
    } else {
        & $agentExe install --binary $agentExe --no-tray
    }
    if ($LASTEXITCODE -ne 0) {
        Write-Fail "Failed to register the system service"
        exit 1
    }

    # ---- 6. Verify -----------------------------------------------------
    Write-Step "Verifying"
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
        Write-Warn "The local page is not responding yet; the service may still be starting. Logs: $(Join-Path $env:LOCALAPPDATA 'dec-agent\logs')"
        exit 1
    }

    Write-Ok "Agent is online, service URL $($status.server_url)"
    if ($status.device_id) {
        Write-Ok "Device $($status.device_id)"
    } else {
        Write-Warn "Device not enrolled yet; run dec-agent enroll --name $deviceName to finish"
    }
    if ($status.aligned -eq $false) {
        Write-Warn "Agent version is out of sync with the server; rerun this script to upgrade"
    }

    Write-Host ""
    Write-Host "  Admin      http://127.0.0.1:43679/admin" -ForegroundColor White
    Write-Host "  Uninstall  dec-agent uninstall" -ForegroundColor White
    Write-Host ""
    Write-Host "  Note: the binaries are unsigned. If SmartScreen blocks them, choose More info, then Run anyway." -ForegroundColor DarkGray
    Write-Host ""
}

try {
    Invoke-Install
} catch {
    Write-Fail "Install failed: $($_.Exception.Message)"
    Write-Fail "At: $($_.InvocationInfo.PositionMessage)"
    exit 1
}
