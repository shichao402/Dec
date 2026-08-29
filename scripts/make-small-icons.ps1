# Rebuilds the small icon sizes with thicker outlines.
#
# The brand mark's champagne outlines are ~5px wide on the 1024px master, which
# collapses below one device pixel once downscaled to 16-48px and washes the fan
# out to a single blob. For each small size the outlines are dilated on the
# master first, so the artwork keeps its exact composition while the strokes
# survive the downscale.
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

Add-Type -ReferencedAssemblies 'System.Drawing' -TypeDefinition @'
using System;
using System.Drawing;
using System.Drawing.Imaging;
using System.Runtime.InteropServices;

public static class StrokeThickener
{
    // Champagne outline color of the brand mark, sampled from the master.
    const int StrokeR = 246, StrokeG = 210, StrokeB = 155;

    static bool IsStroke(byte r, byte g, byte b, byte a)
    {
        return a > 128 && r > 160 && g > 130 && r > b + 25;
    }

    /// <summary>
    /// Returns a copy of the master with the outline strokes dilated by
    /// <paramref name="radius"/> pixels, leaving fully transparent pixels alone
    /// so the rounded corners stay intact.
    /// </summary>
    public static Bitmap Thicken(Bitmap src, int radius)
    {
        int w = src.Width, h = src.Height;
        var clone = new Bitmap(src);
        if (radius <= 0) return clone;

        var data = clone.LockBits(new Rectangle(0, 0, w, h), ImageLockMode.ReadWrite, PixelFormat.Format32bppArgb);
        int stride = data.Stride;
        var buf = new byte[stride * h];
        Marshal.Copy(data.Scan0, buf, 0, buf.Length);

        var mask = new bool[w * h];
        for (int y = 0; y < h; y++)
        {
            int row = y * stride;
            for (int x = 0; x < w; x++)
            {
                int i = row + x * 4;
                if (IsStroke(buf[i + 2], buf[i + 1], buf[i], buf[i + 3])) mask[y * w + x] = true;
            }
        }

        // Separable max filter: horizontal pass, then vertical pass.
        var tmp = new bool[w * h];
        for (int y = 0; y < h; y++)
        {
            for (int x = 0; x < w; x++)
            {
                bool hit = false;
                int lo = x - radius < 0 ? 0 : x - radius;
                int hi = x + radius >= w ? w - 1 : x + radius;
                for (int k = lo; k <= hi && !hit; k++) if (mask[y * w + k]) hit = true;
                tmp[y * w + x] = hit;
            }
        }
        var dil = new bool[w * h];
        for (int x = 0; x < w; x++)
        {
            for (int y = 0; y < h; y++)
            {
                bool hit = false;
                int lo = y - radius < 0 ? 0 : y - radius;
                int hi = y + radius >= h ? h - 1 : y + radius;
                for (int k = lo; k <= hi && !hit; k++) if (tmp[k * w + x]) hit = true;
                dil[y * w + x] = hit;
            }
        }

        for (int y = 0; y < h; y++)
        {
            int row = y * stride;
            for (int x = 0; x < w; x++)
            {
                if (!dil[y * w + x]) continue;
                int i = row + x * 4;
                if (buf[i + 3] == 0) continue; // outside the rounded corners
                buf[i] = (byte)StrokeB;
                buf[i + 1] = (byte)StrokeG;
                buf[i + 2] = (byte)StrokeR;
                buf[i + 3] = 255;
            }
        }

        Marshal.Copy(buf, 0, data.Scan0, buf.Length);
        clone.UnlockBits(data);
        return clone;
    }
}
'@

function New-Resized([System.Drawing.Bitmap]$bmp, [int]$size) {
    $out = New-Object System.Drawing.Bitmap($size, $size, [System.Drawing.Imaging.PixelFormat]::Format32bppArgb)
    $g = [System.Drawing.Graphics]::FromImage($out)
    try {
        $g.Clear([System.Drawing.Color]::Transparent)
        $g.InterpolationMode = [System.Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic
        $g.PixelOffsetMode = [System.Drawing.Drawing2D.PixelOffsetMode]::HighQuality
        $g.DrawImage($bmp, (New-Object System.Drawing.Rectangle(0, 0, $size, $size)))
    } finally {
        $g.Dispose()
    }
    return $out
}

# Target outline weight in device pixels, so every size carries the same visual
# stroke weight instead of jumping between sizes. Going heavier than this closes
# the gaps between adjacent cards and the fan turns into a solid blob.
$targetStrokePx = 0.85
$masterStrokePx = 5

# 16px is an exception: the cards are only ~3px wide there, so the outlines merge
# no matter what. A slightly heavier value keeps that filled fan silhouette crisp
# rather than muddy.
$radiusOverride = @{ 16 = 22 }

function Get-DilateRadius([int]$size) {
    if ($radiusOverride.ContainsKey($size)) { return $radiusOverride[$size] }
    $wanted = $targetStrokePx * 1024.0 / $size
    $r = [Math]::Round(($wanted - $masterStrokePx) / 2.0)
    if ($r -lt 0) { return 0 }
    return [int]$r
}

# Standalone PNGs that platform surfaces read directly.
$pngTargets = [ordered]@{
    '32x32.png'             = 32
    '64x64.png'             = 64
    '128x128.png'           = 128
    'StoreLogo.png'         = 50
    'Square30x30Logo.png'   = 30
    'Square44x44Logo.png'   = 44
    'Square71x71Logo.png'   = 71
    'Square89x89Logo.png'   = 89
    'Square107x107Logo.png' = 107
    'Square142x142Logo.png' = 142
    'Square150x150Logo.png' = 150
}
$icoSizes = @(16, 24, 32, 48, 64, 128, 256)

$master = [System.Drawing.Bitmap]::FromFile($Source)
$rendered = @{}
try {
    $allSizes = @($pngTargets.Values) + $icoSizes | Sort-Object -Unique
    foreach ($size in $allSizes) {
        $radius = Get-DilateRadius $size
        $thick = [StrokeThickener]::Thicken($master, $radius)
        try {
            $rendered[$size] = New-Resized $thick $size
        } finally {
            $thick.Dispose()
        }
        Write-Host ("rendered {0}px (dilate {1})" -f $size, $radius)
    }

    foreach ($name in $pngTargets.Keys) {
        $path = Join-Path $IconDir $name
        $rendered[$pngTargets[$name]].Save($path, [System.Drawing.Imaging.ImageFormat]::Png)
        Write-Host "wrote $path"
    }

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
            $s = $icoSizes[$i]
            $dim = if ($s -ge 256) { 0 } else { $s }
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
        $bw.Dispose()
        $fs.Dispose()
    }
    Write-Host "wrote $icoPath ($($icoSizes -join ', ')px)"
} finally {
    foreach ($bmp in $rendered.Values) { $bmp.Dispose() }
    $master.Dispose()
}
