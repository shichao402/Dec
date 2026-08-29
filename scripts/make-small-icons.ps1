# Renders the whole platform icon set from the 1024px brand master.
#
# The mark is five outlined cards fanned from a shared pivot. Downscaled below
# ~48px the outlines fall under one device pixel and the five cards collapse into
# an unreadable blob, so two optical variants are produced from the same artwork:
#
#   <= 48px  three solid cards (centre plus the two outermost), keeping the
#            original fan angle and silhouette. Filling them solid is what makes
#            them survive; the gaps between five cards are narrower than the
#            cards themselves, so a five-card solid version merges into one blob.
#   >= 64px  the original five outlined cards, with the outlines dilated just
#            enough to hold a consistent ~0.85 device px stroke weight.
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
using System.Collections.Generic;
using System.Drawing;
using System.Drawing.Imaging;
using System.Runtime.InteropServices;

public static class CardOps
{
    // Champagne outline color of the brand mark, sampled from the master.
    const int StrokeR = 246, StrokeG = 210, StrokeB = 155;

    // Kind codes per pixel.
    const byte KTransparent = 0, KStroke = 1, KDark = 2, KEmerald = 3;

    public class Img
    {
        public int W, H, Stride;
        public byte[] Buf;
        public byte[] Kind;
        public int[] Comp;      // card interior id, -1 elsewhere
        public int CompCount;
    }

    static bool IsStroke(byte r, byte g, byte b, byte a)
    {
        return a > 128 && r > 160 && g > 130 && r > b + 25;
    }

    /// <summary>
    /// Classifies every pixel and labels the five card interiors left to right,
    /// by flood filling the plate from the border and treating outlines as walls.
    /// </summary>
    public static Img Load(Bitmap src)
    {
        int w = src.Width, h = src.Height;
        var data = src.LockBits(new Rectangle(0, 0, w, h), ImageLockMode.ReadOnly, PixelFormat.Format32bppArgb);
        var img = new Img { W = w, H = h, Stride = data.Stride };
        img.Buf = new byte[data.Stride * h];
        Marshal.Copy(data.Scan0, img.Buf, 0, img.Buf.Length);
        src.UnlockBits(data);

        img.Kind = new byte[w * h];
        for (int y = 0; y < h; y++)
        {
            int row = y * img.Stride;
            for (int x = 0; x < w; x++)
            {
                int i = row + x * 4;
                byte b = img.Buf[i], g = img.Buf[i + 1], r = img.Buf[i + 2], a = img.Buf[i + 3];
                byte k;
                if (a == 0) k = KTransparent;
                else if (IsStroke(r, g, b, a)) k = KStroke;
                else if (g > r + 15 && g > 60 && g < 160) k = KEmerald;
                else k = KDark;
                img.Kind[y * w + x] = k;
            }
        }

        var outside = new bool[w * h];
        var stack = new Stack<int>();
        for (int x = 0; x < w; x++) { Seed(img, outside, stack, x, 0); Seed(img, outside, stack, x, h - 1); }
        for (int y = 0; y < h; y++) { Seed(img, outside, stack, 0, y); Seed(img, outside, stack, w - 1, y); }
        while (stack.Count > 0)
        {
            int p = stack.Pop();
            int px = p % w, py = p / w;
            if (px > 0) Seed(img, outside, stack, px - 1, py);
            if (px < w - 1) Seed(img, outside, stack, px + 1, py);
            if (py > 0) Seed(img, outside, stack, px, py - 1);
            if (py < h - 1) Seed(img, outside, stack, px, py + 1);
        }

        img.Comp = new int[w * h];
        for (int i = 0; i < img.Comp.Length; i++) img.Comp[i] = -1;
        var tmpId = new int[w * h];
        for (int i = 0; i < tmpId.Length; i++) tmpId[i] = -1;
        var raw = new List<int[]>();
        int next = 0;
        for (int y = 0; y < h; y++)
        {
            for (int x = 0; x < w; x++)
            {
                int idx = y * w + x;
                if (outside[idx] || tmpId[idx] != -1) continue;
                byte k = img.Kind[idx];
                if (k == KTransparent || k == KStroke) continue;

                int id = next++;
                long sumX = 0; int count = 0;
                var st = new Stack<int>();
                st.Push(idx); tmpId[idx] = id;
                while (st.Count > 0)
                {
                    int p = st.Pop();
                    int px = p % w, py = p / w;
                    sumX += px; count++;
                    PushComp(img, outside, tmpId, st, px - 1, py, id);
                    PushComp(img, outside, tmpId, st, px + 1, py, id);
                    PushComp(img, outside, tmpId, st, px, py - 1, id);
                    PushComp(img, outside, tmpId, st, px, py + 1, id);
                }
                raw.Add(new int[] { id, (int)(sumX / Math.Max(1, count)), count });
            }
        }

        // Drop antialias specks, then order the real cards by centre x.
        var keep = new List<int[]>();
        foreach (var r in raw) if (r[2] > 2000) keep.Add(r);
        keep.Sort(delegate (int[] a, int[] b) { return a[1].CompareTo(b[1]); });
        var remap = new Dictionary<int, int>();
        for (int i = 0; i < keep.Count; i++) remap[keep[i][0]] = i;
        for (int i = 0; i < tmpId.Length; i++)
        {
            int t = tmpId[i];
            if (t >= 0 && remap.ContainsKey(t)) img.Comp[i] = remap[t];
        }
        img.CompCount = keep.Count;
        return img;
    }

    static void Seed(Img img, bool[] outside, Stack<int> stack, int x, int y)
    {
        int idx = y * img.W + x;
        if (outside[idx]) return;
        byte k = img.Kind[idx];
        if (k == KStroke) return;
        outside[idx] = true;
        if (k == KTransparent) return;
        stack.Push(idx);
    }

    static void PushComp(Img img, bool[] outside, int[] tmpId, Stack<int> st, int x, int y, int id)
    {
        int idx = y * img.W + x;
        if (outside[idx] || tmpId[idx] != -1) return;
        byte k = img.Kind[idx];
        if (k == KTransparent || k == KStroke) return;
        tmpId[idx] = id;
        st.Push(idx);
    }

    /// <summary>
    /// Renders a variant of the master. Cards outside <paramref name="keepCards"/>
    /// are erased back to the plate color; <paramref name="solid"/> fills the kept
    /// cards with the outline color except the accent card, and
    /// <paramref name="dilate"/> thickens whatever outline remains.
    /// </summary>
    public static Bitmap Render(Img img, int[] keepCards, bool solid, int accentCard, int dilate)
    {
        int w = img.W, h = img.H, stride = img.Stride;
        var buf = (byte[])img.Buf.Clone();
        var keep = new HashSet<int>(keepCards);

        int pi = 8 * stride + (w / 2) * 4;
        byte pb = img.Buf[pi], pg = img.Buf[pi + 1], pr = img.Buf[pi + 2];

        var paint = new bool[w * h];
        for (int y = 0; y < h; y++)
        {
            int row = y * stride;
            for (int x = 0; x < w; x++)
            {
                int idx = y * w + x;
                int i = row + x * 4;
                int comp = img.Comp[idx];

                if (comp >= 0)
                {
                    if (!keep.Contains(comp)) { Set(buf, i, pr, pg, pb); continue; }
                    if (solid && comp != accentCard) paint[idx] = true;
                    continue;
                }
                if (img.Kind[idx] == KStroke)
                {
                    if (NearKeptCard(img, keep, x, y, 14)) paint[idx] = true;
                    else Set(buf, i, pr, pg, pb);
                }
            }
        }

        if (dilate > 0) paint = Dilate(paint, w, h, dilate);

        for (int y = 0; y < h; y++)
        {
            int row = y * stride;
            for (int x = 0; x < w; x++)
            {
                int idx = y * w + x;
                if (!paint[idx] || img.Kind[idx] == KTransparent) continue;
                Set(buf, row + x * 4, (byte)StrokeR, (byte)StrokeG, (byte)StrokeB);
            }
        }

        var outBmp = new Bitmap(w, h, PixelFormat.Format32bppArgb);
        var data = outBmp.LockBits(new Rectangle(0, 0, w, h), ImageLockMode.WriteOnly, PixelFormat.Format32bppArgb);
        Marshal.Copy(buf, 0, data.Scan0, buf.Length);
        outBmp.UnlockBits(data);
        return outBmp;
    }

    static bool NearKeptCard(Img img, HashSet<int> keep, int x, int y, int r)
    {
        int w = img.W, h = img.H;
        for (int dy = -r; dy <= r; dy++)
        {
            int yy = y + dy; if (yy < 0 || yy >= h) continue;
            for (int dx = -r; dx <= r; dx++)
            {
                int xx = x + dx; if (xx < 0 || xx >= w) continue;
                int c = img.Comp[yy * w + xx];
                if (c >= 0 && keep.Contains(c)) return true;
            }
        }
        return false;
    }

    static void Set(byte[] buf, int i, byte r, byte g, byte b)
    {
        buf[i] = b; buf[i + 1] = g; buf[i + 2] = r; buf[i + 3] = 255;
    }

    static bool[] Dilate(bool[] mask, int w, int h, int radius)
    {
        var tmp = new bool[w * h];
        for (int y = 0; y < h; y++)
            for (int x = 0; x < w; x++)
            {
                bool hit = false;
                int lo = Math.Max(0, x - radius), hi = Math.Min(w - 1, x + radius);
                for (int k = lo; k <= hi && !hit; k++) if (mask[y * w + k]) hit = true;
                tmp[y * w + x] = hit;
            }
        var dil = new bool[w * h];
        for (int x = 0; x < w; x++)
            for (int y = 0; y < h; y++)
            {
                bool hit = false;
                int lo = Math.Max(0, y - radius), hi = Math.Min(h - 1, y + radius);
                for (int k = lo; k <= hi && !hit; k++) if (tmp[k * w + x]) hit = true;
                dil[y * w + x] = hit;
            }
        return dil;
    }
}
'@

# Below this size the simplified three-card variant is used.
$SimplifyAtOrBelow = 48

# Target outline weight in device px for the five-card variant, so no size shows
# a visibly different stroke weight than its neighbours.
$targetStrokePx = 0.85
$masterStrokePx = 5

function Get-DilateRadius([int]$size) {
    $wanted = $targetStrokePx * 1024.0 / $size
    $r = [Math]::Round(($wanted - $masterStrokePx) / 2.0)
    if ($r -lt 0) { return 0 }
    return [int]$r
}

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

$master = [System.Drawing.Bitmap]::FromFile($Source)
$rendered = @{}
$variantCache = @{}
try {
    $img = [CardOps]::Load($master)
    if ($img.CompCount -ne 5) {
        throw "expected 5 cards in the master, found $($img.CompCount)"
    }
    $allCards = 0..($img.CompCount - 1)
    $accent = [int](($img.CompCount - 1) / 2)
    $threeCards = @(0, $accent, ($img.CompCount - 1))

    $allSizes = @($pngTargets.Values) + $icoSizes + @($icnsChunks.Values) | Sort-Object -Unique
    foreach ($size in $allSizes) {
        if ($size -le $SimplifyAtOrBelow) {
            $key = 'simple'
            if (-not $variantCache.ContainsKey($key)) {
                $variantCache[$key] = [CardOps]::Render($img, $threeCards, $true, $accent, 0)
            }
            $note = 'three solid cards'
        } else {
            $radius = Get-DilateRadius $size
            $key = "full-$radius"
            if (-not $variantCache.ContainsKey($key)) {
                $variantCache[$key] = [CardOps]::Render($img, $allCards, $false, $accent, $radius)
            }
            $note = "five cards, dilate $radius"
        }
        $rendered[$size] = New-Resized $variantCache[$key] $size
        Write-Host ("rendered {0}px ({1})" -f $size, $note)
    }

    foreach ($name in $pngTargets.Keys) {
        $path = Join-Path $IconDir $name
        $rendered[$pngTargets[$name]].Save($path, [System.Drawing.Imaging.ImageFormat]::Png)
    }
    Write-Host "wrote $($pngTargets.Count) png files"

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
    Write-Host "wrote $icoPath ($($icoSizes -join ', ')px)"

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
    Write-Host "wrote $icnsPath ($($icnsChunks.Count) chunks)"
} finally {
    foreach ($bmp in $rendered.Values) { $bmp.Dispose() }
    foreach ($bmp in $variantCache.Values) { $bmp.Dispose() }
    $master.Dispose()
}
