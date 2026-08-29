# Turns the flat 1024x1024 brand icon into an RGBA source for `tauri icon`.
# The artwork already has its rounded-square corners painted black; platform
# icon pipelines expect those corners transparent instead.
[CmdletBinding()]
param(
    [string]$Source,
    [string]$Output,
    [int]$CornerRadius = 208
)

$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Drawing

$repoRoot = Split-Path -Parent $PSScriptRoot
if (-not $Source) { $Source = Join-Path $repoRoot 'assets\branding\dec-icon-1024.png' }
if (-not $Output) { $Output = Join-Path $repoRoot 'assets\branding\dec-icon-source.png' }

$Source = [System.IO.Path]::GetFullPath($Source)
$Output = [System.IO.Path]::GetFullPath($Output)

$src = [System.Drawing.Bitmap]::FromFile($Source)
try {
    $size = $src.Width
    if ($src.Width -ne $src.Height) {
        throw "source icon must be square, got $($src.Width)x$($src.Height)"
    }

    $dst = New-Object System.Drawing.Bitmap($size, $size, [System.Drawing.Imaging.PixelFormat]::Format32bppArgb)
    try {
        $g = [System.Drawing.Graphics]::FromImage($dst)
        try {
            $g.Clear([System.Drawing.Color]::Transparent)
            $g.SmoothingMode = [System.Drawing.Drawing2D.SmoothingMode]::AntiAlias
            $g.InterpolationMode = [System.Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic
            $g.PixelOffsetMode = [System.Drawing.Drawing2D.PixelOffsetMode]::HighQuality

            $d = $CornerRadius * 2
            $path = New-Object System.Drawing.Drawing2D.GraphicsPath
            try {
                $path.AddArc(0, 0, $d, $d, 180, 90)
                $path.AddArc($size - $d, 0, $d, $d, 270, 90)
                $path.AddArc($size - $d, $size - $d, $d, $d, 0, 90)
                $path.AddArc(0, $size - $d, $d, $d, 90, 90)
                $path.CloseFigure()
                $g.SetClip($path)
            } finally {
                $path.Dispose()
            }

            $g.DrawImage($src, (New-Object System.Drawing.Rectangle(0, 0, $size, $size)))
        } finally {
            $g.Dispose()
        }
        $dst.Save($Output, [System.Drawing.Imaging.ImageFormat]::Png)
    } finally {
        $dst.Dispose()
    }
} finally {
    $src.Dispose()
}

Write-Host "wrote $Output"
