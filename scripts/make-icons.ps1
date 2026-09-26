# Renders the full platform icon set from the 1024px brand master.
#
# Usage:
#   powershell -File scripts/make-icons.ps1 [-Source <1024 png>] [-IconDir <icons dir>]
#
# The master is an opaque rounded-square artwork; every target is a plain
# high-quality bicubic resize, plus multi-resolution .ico and .icns outputs.

[CmdletBinding()]
param(
    [string]$Source,
    [string]$IconDir
)

$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Drawing

$repoRoot = Split-Path -Parent $PSScriptRoot
if (-not $Source) { $Source = Join-Path $repoRoot 'assets\branding\dec-icon-source.png' }
if (-not $IconDir) { $IconDir = Join-Path $repoRoot 'client\src-tauri\icons' }
$Source = [System.IO.Path]::GetFullPath($Source)
$IconDir = [System.IO.Path]::GetFullPath($IconDir)

if (-not (Test-Path $Source)) { throw "source not found: $Source" }
if (-not (Test-Path $IconDir)) { throw "icon dir not found: $IconDir" }

function New-Resized([System.Drawing.Bitmap]$bmp, [int]$size) {
    $out = New-Object System.Drawing.Bitmap($size, $size, [System.Drawing.Imaging.PixelFormat]::Format32bppArgb)
    $g = [System.Drawing.Graphics]::FromImage($out)
    try {
        $g.InterpolationMode = [System.Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic
        $g.PixelOffsetMode = [System.Drawing.Drawing2D.PixelOffsetMode]::HighQuality
        $g.SmoothingMode = [System.Drawing.Drawing2D.SmoothingMode]::HighQuality
        $g.DrawImage($bmp, (New-Object System.Drawing.Rectangle(0, 0, $size, $size)))
    } finally {
        $g.Dispose()
    }
    return $out
}

function Save-Png([System.Drawing.Bitmap]$bmp, [string]$path) {
    $dir = Split-Path -Parent $path
    if (-not (Test-Path $dir)) { New-Item -ItemType Directory -Path $dir | Out-Null }
    $bmp.Save($path, [System.Drawing.Imaging.ImageFormat]::Png)
}

$pngTargets = [ordered]@{
    '32x32.png'             = 32
    '64x64.png'             = 64
    '128x128.png'           = 128
    '128x128@2x.png'        = 256
    'icon.png'              = 1024
    'Square30x30Logo.png'   = 30
    'Square44x44Logo.png'   = 44
    'StoreLogo.png'         = 50
    'Square71x71Logo.png'   = 71
    'Square89x89Logo.png'   = 89
    'Square107x107Logo.png' = 107
    'Square142x142Logo.png' = 142
    'Square150x150Logo.png' = 150
    'Square284x284Logo.png' = 284
    'Square310x310Logo.png' = 310
}

$icoSizes = @(16, 24, 32, 48, 64, 128, 256)

# icns chunk type -> pixel size.
$icnsChunks = [ordered]@{
    'icp4' = 16; 'icp5' = 32; 'icp6' = 64; 'ic07' = 128; 'ic08' = 256
    'ic09' = 512; 'ic10' = 1024; 'ic11' = 32; 'ic12' = 64; 'ic13' = 256; 'ic14' = 1024
}

# iOS AppIcon slots: name -> points, scale.
$iosTargets = [ordered]@{
    'AppIcon-20x20@1x.png'        = 20;  'AppIcon-20x20@2x.png'  = 40;  'AppIcon-20x20@3x.png' = 60
    'AppIcon-29x29@1x.png'        = 29;  'AppIcon-29x29@2x.png'  = 58;  'AppIcon-29x29@3x.png' = 87
    'AppIcon-40x40@1x.png'        = 40;  'AppIcon-40x40@2x.png'  = 80;  'AppIcon-40x40@3x.png' = 120
    'AppIcon-60x60@2x.png'        = 120; 'AppIcon-60x60@3x.png'  = 180
    'AppIcon-76x76@1x.png'        = 76;  'AppIcon-76x76@2x.png'  = 152
    'AppIcon-83.5x83.5@2x.png'    = 167
    'AppIcon-512@2x.png'          = 1024
}

# Android launcher densities: dpi name -> px.
$androidDensities = [ordered]@{
    'mdpi' = 48; 'hdpi' = 72; 'xhdpi' = 96; 'xxhdpi' = 144; 'xxxhdpi' = 192
}

$master = New-Object System.Drawing.Bitmap($Source)
$rendered = @{}
try {
    $allSizes = @($pngTargets.Values) + $icoSizes + @($icnsChunks.Values) + @($iosTargets.Values) + @($androidDensities.Values) |
        Sort-Object -Unique
    foreach ($size in $allSizes) {
        $rendered[$size] = New-Resized $master $size
        Write-Host ("rendered {0}px" -f $size)
    }

    foreach ($name in $pngTargets.Keys) {
        Save-Png $rendered[$pngTargets[$name]] (Join-Path $IconDir $name)
    }
    Write-Host "wrote $($pngTargets.Count) png files"

    foreach ($name in $iosTargets.Keys) {
        Save-Png $rendered[$iosTargets[$name]] (Join-Path (Join-Path $IconDir 'ios') $name)
    }
    Write-Host "wrote $($iosTargets.Count) ios png files"

    foreach ($dpi in $androidDensities.Keys) {
        $px = $androidDensities[$dpi]
        $dir = Join-Path (Join-Path $IconDir 'android') ("mipmap-" + $dpi)
        Save-Png $rendered[$px] (Join-Path $dir 'ic_launcher.png')
        Save-Png $rendered[$px] (Join-Path $dir 'ic_launcher_round.png')
        Save-Png $rendered[$px] (Join-Path $dir 'ic_launcher_foreground.png')
    }
    Write-Host "wrote android mipmaps"

    # Multi-resolution .ico, PNG-compressed entries (supported since Vista).
    $blobs = @()
    foreach ($size in $icoSizes) {
        $ms = New-Object System.IO.MemoryStream
        $rendered[$size].Save($ms, [System.Drawing.Imaging.ImageFormat]::Png)
        $blobs += , $ms.ToArray()
        $ms.Dispose()
    }
    $icoPath = Join-Path $IconDir 'icon.ico'
    $fs = [System.IO.File]::Create($icoPath)
    $bw = New-Object System.IO.BinaryWriter($fs)
    try {
        $bw.Write([UInt16]0)
        $bw.Write([UInt16]1)
        $bw.Write([UInt16]$icoSizes.Count)
        $offset = 6 + 16 * $icoSizes.Count
        for ($i = 0; $i -lt $icoSizes.Count; $i++) {
            $dim = if ($icoSizes[$i] -ge 256) { 0 } else { $icoSizes[$i] }
            $bw.Write([Byte]$dim)
            $bw.Write([Byte]$dim)
            $bw.Write([Byte]0)      # palette count
            $bw.Write([Byte]0)      # reserved
            $bw.Write([UInt16]1)    # color planes
            $bw.Write([UInt16]32)   # bits per pixel
            $bw.Write([UInt32]$blobs[$i].Length)
            $bw.Write([UInt32]$offset)
            $offset += $blobs[$i].Length
        }
        foreach ($blob in $blobs) { $bw.Write($blob) }
    } finally {
        $bw.Dispose(); $fs.Dispose()
    }
    Write-Host "wrote $icoPath"

    # .icns, PNG payload per chunk, big-endian lengths.
    $icnsPath = Join-Path $IconDir 'icon.icns'
    $body = New-Object System.IO.MemoryStream
    foreach ($type in $icnsChunks.Keys) {
        $ms = New-Object System.IO.MemoryStream
        $rendered[$icnsChunks[$type]].Save($ms, [System.Drawing.Imaging.ImageFormat]::Png)
        $png = $ms.ToArray()
        $ms.Dispose()
        $body.Write([System.Text.Encoding]::ASCII.GetBytes($type), 0, 4)
        $len = [BitConverter]::GetBytes([UInt32]($png.Length + 8))
        [Array]::Reverse($len)
        $body.Write($len, 0, 4)
        $body.Write($png, 0, $png.Length)
    }
    $bodyBytes = $body.ToArray()
    $body.Dispose()
    $fs = [System.IO.File]::Create($icnsPath)
    try {
        $fs.Write([System.Text.Encoding]::ASCII.GetBytes('icns'), 0, 4)
        $total = [BitConverter]::GetBytes([UInt32]($bodyBytes.Length + 8))
        [Array]::Reverse($total)
        $fs.Write($total, 0, 4)
        $fs.Write($bodyBytes, 0, $bodyBytes.Length)
    } finally {
        $fs.Dispose()
    }
    Write-Host "wrote $icnsPath"
} finally {
    foreach ($bmp in $rendered.Values) { $bmp.Dispose() }
    $master.Dispose()
}
