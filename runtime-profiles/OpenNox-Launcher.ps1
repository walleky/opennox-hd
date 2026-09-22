[CmdletBinding()]
param(
    [ValidateSet('saved', 'prompt', 'auto', 'last', '1080p', '1440p', '4k', 'original')]
    [string]$Resolution = 'saved',
    [ValidateSet('saved', 'upscaled', 'original', 'prompt')]
    [string]$SpriteMode = 'saved',
    [string]$ConfigPath = '',
    [switch]$NoLaunch,
    [switch]$Settings
)

$ErrorActionPreference = 'Stop'
$runtimeRoot = $PSScriptRoot
if ($Settings) { $Resolution = 'prompt'; $SpriteMode = 'prompt'; $NoLaunch = $true }
if ([string]::IsNullOrWhiteSpace($ConfigPath)) {
    $ConfigPath = Join-Path $runtimeRoot 'opennox-user.yml'
}
$ConfigPath = [System.IO.Path]::GetFullPath($ConfigPath)

function Get-ActiveTextureScale {
    $manifestPath = Join-Path $runtimeRoot 'CodexBackups\full-texture-overlay\active-manifest.json'
    if (Test-Path -LiteralPath $manifestPath -PathType Leaf) {
        try {
            $manifest = Get-Content -Raw -LiteralPath $manifestPath | ConvertFrom-Json
            $scale = [int]$manifest.scale
            if ($scale -ne 2) { throw "Unsupported texture scale: $scale. Install the Hybrid 2x package." }
        } catch { throw "Invalid active overlay manifest: $($_.Exception.Message)" }
    }
    return 2
}

function Get-PrimaryDisplaySize {
    if (-not ('OpenNoxDisplayMetrics' -as [type])) {
        Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;

public static class OpenNoxDisplayMetrics {
    [DllImport("user32.dll")]
    public static extern IntPtr SetThreadDpiAwarenessContext(IntPtr dpiContext);

    [DllImport("user32.dll")]
    public static extern int GetSystemMetrics(int index);
}
'@
    }

    # Avoid DPI virtualization (for example, 1920x1200 reported as 1280x800).
    $previous = [OpenNoxDisplayMetrics]::SetThreadDpiAwarenessContext([IntPtr](-4))
    try {
        $width = [OpenNoxDisplayMetrics]::GetSystemMetrics(0)
        $height = [OpenNoxDisplayMetrics]::GetSystemMetrics(1)
    } finally {
        if ($previous -ne [IntPtr]::Zero) {
            [void][OpenNoxDisplayMetrics]::SetThreadDpiAwarenessContext($previous)
        }
    }
    if ($width -lt 640 -or $height -lt 480) {
        throw "Could not detect a usable primary display size (reported ${width}x${height})."
    }
    [pscustomobject]@{ Width = $width; Height = $height }
}

function Get-ScaledInternalSize {
    param(
        [Parameter(Mandatory)][int]$Width,
        [Parameter(Mandatory)][int]$Height,
        [Parameter(Mandatory)][int]$Scale
    )
    $minWidth = [Math]::Max(320, [int][Math]::Ceiling(640 / $Scale))
    $minHeight = [Math]::Max(240, [int][Math]::Ceiling(480 / $Scale))
    [pscustomobject]@{
        Width = [Math]::Max($minWidth, [int][Math]::Floor($Width / $Scale))
        Height = [Math]::Max($minHeight, [int][Math]::Floor($Height / $Scale))
    }
}

function Get-VideoSize {
    param([Parameter(Mandatory)][string]$Path)
    $lines = [System.IO.File]::ReadAllLines($Path)
    $inVideo = $false
    $inSize = $false
    $videoIndent = -1
    $sizeIndent = -1
    $width = $null
    $height = $null
    foreach ($line in $lines) {
        $indent = [regex]::Match($line, '^\s*').Value.Length
        if ($line -match '^(\s*)video:\s*$') {
            $inVideo = $true
            $inSize = $false
            $videoIndent = $Matches[1].Length
            continue
        }
        if ($inVideo -and $line.Trim() -and $indent -le $videoIndent) { break }
        if ($inVideo -and $line -match '^(\s*)size:\s*$' -and $Matches[1].Length -gt $videoIndent) {
            $inSize = $true
            $sizeIndent = $Matches[1].Length
            continue
        }
        if ($inSize -and $line.Trim() -and $indent -le $sizeIndent) { $inSize = $false }
        if ($inSize -and $line -match '^\s*width:\s*(\d+)\s*$') { $width = [int]$Matches[1] }
        if ($inSize -and $line -match '^\s*height:\s*(\d+)\s*$') { $height = [int]$Matches[1] }
    }
    if (-not $width -or -not $height) {
        throw "Config has no video.size width/height: $Path"
    }
    [pscustomobject]@{ Width = $width; Height = $height }
}

function Set-VideoSize {
    param(
        [Parameter(Mandatory)][string]$Path,
        [Parameter(Mandatory)][int]$Width,
        [Parameter(Mandatory)][int]$Height
    )
    $currentSize = Get-VideoSize -Path $Path
    if ($currentSize.Width -eq $Width -and $currentSize.Height -eq $Height) { return }
    $lines = [System.IO.File]::ReadAllLines($Path)
    $inVideo = $false
    $inSize = $false
    $videoIndent = -1
    $sizeIndent = -1
    $changedWidth = $false
    $changedHeight = $false
    for ($i = 0; $i -lt $lines.Length; $i++) {
        $line = $lines[$i]
        $indent = [regex]::Match($line, '^\s*').Value.Length
        if ($line -match '^(\s*)video:\s*$') {
            $inVideo = $true
            $inSize = $false
            $videoIndent = $Matches[1].Length
            continue
        }
        if ($inVideo -and $line.Trim() -and $indent -le $videoIndent) { break }
        if ($inVideo -and $line -match '^(\s*)size:\s*$' -and $Matches[1].Length -gt $videoIndent) {
            $inSize = $true
            $sizeIndent = $Matches[1].Length
            continue
        }
        if ($inSize -and $line.Trim() -and $indent -le $sizeIndent) { $inSize = $false }
        if ($inSize -and $line -match '^(\s+)width:\s*\d+\s*$') {
            $lines[$i] = "$($Matches[1])width: $Width"
            $changedWidth = $true
        }
        if ($inSize -and $line -match '^(\s+)height:\s*\d+\s*$') {
            $lines[$i] = "$($Matches[1])height: $Height"
            $changedHeight = $true
        }
    }
    if (-not $changedWidth -or -not $changedHeight) {
        throw "Could not update video.size in $Path"
    }
    $stage = "$Path.launcher-stage"
    $backup = "$Path.before-resolution-$((Get-Date).ToString('yyyyMMdd-HHmmssfff'))"
    [System.IO.File]::WriteAllLines($stage, $lines, [System.Text.UTF8Encoding]::new($false))
    [System.IO.File]::Replace($stage, $Path, $backup)
}

function Set-LegacyIntegerOption {
    param(
        [Parameter(Mandatory)][string]$Path,
        [Parameter(Mandatory)][string]$Name,
        [Parameter(Mandatory)][int]$Value
    )
    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        throw "OpenNox legacy config is missing: $Path"
    }
    $lines = [System.IO.File]::ReadAllLines($Path)
    $pattern = '^(\s*' + [regex]::Escape($Name) + '\s*=\s*)-?\d+(\s*)$'
    $found = $false
    $changed = $false
    for ($i = 0; $i -lt $lines.Length; $i++) {
        if ($lines[$i] -match $pattern) {
            $found = $true
            $updated = "$($Matches[1])$Value$($Matches[2])"
            if ($lines[$i] -ne $updated) {
                $lines[$i] = $updated
                $changed = $true
            }
            break
        }
    }
    if (-not $found) {
        $lines += "$Name = $Value"
        $changed = $true
    }
    if (-not $changed) { return }

    $stage = "$Path.launcher-stage"
    $backup = "$Path.before-$($Name.ToLowerInvariant())-$((Get-Date).ToString('yyyyMMdd-HHmmssfff'))"
    [System.IO.File]::WriteAllLines($stage, $lines, [System.Text.UTF8Encoding]::new($false))
    [System.IO.File]::Replace($stage, $Path, $backup)
}

function Set-LegacyVideoMode {
    param(
        [Parameter(Mandatory)][string]$Path,
        [Parameter(Mandatory)][int]$Width,
        [Parameter(Mandatory)][int]$Height
    )
    if ($Width -le 0 -or $Height -le 0) {
        throw "Invalid legacy video mode: ${Width}x${Height}"
    }
    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        throw "OpenNox legacy config is missing: $Path"
    }
    $lines = [System.IO.File]::ReadAllLines($Path)
    $found = $false
    $changed = $false
    for ($i = 0; $i -lt $lines.Length; $i++) {
        if ($lines[$i] -match '^(\s*VideoMode\s*=\s*)\d+\s+\d+\s+(\d+)(\s*)$') {
            $found = $true
            $updated = "$($Matches[1])$Width $Height $($Matches[2])$($Matches[3])"
            if ($lines[$i] -ne $updated) {
                $lines[$i] = $updated
                $changed = $true
            }
            break
        }
    }
    if (-not $found) {
        $lines += "VideoMode = $Width $Height 16"
        $changed = $true
    }
    if (-not $changed) { return }

    $stage = "$Path.launcher-stage"
    $backup = "$Path.before-videomode-$((Get-Date).ToString('yyyyMMdd-HHmmssfff'))"
    [System.IO.File]::WriteAllLines($stage, $lines, [System.Text.UTF8Encoding]::new($false))
    [System.IO.File]::Replace($stage, $Path, $backup)
}

function Get-OriginalSpriteComparisonFiles {
    @(
        'NoxData\video.bag.zip',
        'NoxData\default.hd2.fnt',
        'NoxData\large.hd2.fnt',
        'NoxData\small.hd2.fnt',
        'NoxData\number.hd2.fnt'
    )
}

function Restore-OriginalSpriteComparisonAssets {
    param([Parameter(Mandatory)][string]$RuntimeRoot)
    $stageRoot = Join-Path $RuntimeRoot 'CodexBackups\original-sprite-comparison\staged'
    if (-not (Test-Path -LiteralPath $stageRoot -PathType Container)) {
        return $false
    }
    $restored = $false
    foreach ($relativePath in @(Get-OriginalSpriteComparisonFiles)) {
        $staged = Join-Path $stageRoot $relativePath
        if (-not (Test-Path -LiteralPath $staged -PathType Leaf)) {
            continue
        }
        $target = Join-Path $RuntimeRoot $relativePath
        if (Test-Path -LiteralPath $target -PathType Leaf) {
            throw "Cannot recover original-sprite comparison asset because its target already exists: $target"
        }
        [System.IO.Directory]::CreateDirectory((Split-Path -Parent $target)) | Out-Null
        Move-Item -LiteralPath $staged -Destination $target
        $restored = $true
    }
    return $restored
}

function Stage-OriginalSpriteComparisonAssets {
    param([Parameter(Mandatory)][string]$RuntimeRoot)
    $stageRoot = Join-Path $RuntimeRoot 'CodexBackups\original-sprite-comparison\staged'
    try {
        foreach ($relativePath in @(Get-OriginalSpriteComparisonFiles)) {
            $target = Join-Path $RuntimeRoot $relativePath
            if (-not (Test-Path -LiteralPath $target -PathType Leaf)) {
                continue
            }
            $staged = Join-Path $stageRoot $relativePath
            if (Test-Path -LiteralPath $staged -PathType Leaf) {
                throw "Original-sprite comparison staging target already exists: $staged"
            }
            [System.IO.Directory]::CreateDirectory((Split-Path -Parent $staged)) | Out-Null
            Move-Item -LiteralPath $target -Destination $staged
        }
    } catch {
        Restore-OriginalSpriteComparisonAssets -RuntimeRoot $RuntimeRoot | Out-Null
        throw
    }
}

if (Get-Process -Name 'opennox', 'opennox-hd', 'opennox-hd-texture2x' -ErrorAction SilentlyContinue) {
    throw 'OpenNox is already running. Close it before changing settings or starting another session.'
}
$preferencesPath = Join-Path $runtimeRoot 'launcher-settings.json'
$savedResolution = 'auto'
$savedSprites = 'upscaled'
if (Test-Path -LiteralPath $preferencesPath -PathType Leaf) {
    try {
        $preferences = Get-Content -LiteralPath $preferencesPath -Raw | ConvertFrom-Json
        if ($preferences.resolution -notin @('auto', 'last', '1080p', '1440p', '4k', 'original') -or
            $preferences.sprites -notin @('upscaled', 'original')) { throw 'Unknown setting' }
        $savedResolution = $preferences.resolution
        $savedSprites = $preferences.sprites
    } catch {
        Write-Warning 'Saved launcher settings could not be read. Using automatic resolution and upgraded sprites.'
    }
}
if ($Resolution -eq 'saved') { $Resolution = $savedResolution }
if ($SpriteMode -eq 'saved') { $SpriteMode = $savedSprites }
if (-not (Test-Path -LiteralPath $ConfigPath -PathType Leaf)) {
    $baseConfig = Join-Path $runtimeRoot 'opennox.yml'
    if (-not (Test-Path -LiteralPath $baseConfig -PathType Leaf)) {
        throw "Persistent config is missing and no base config exists: $ConfigPath"
    }
    Copy-Item -LiteralPath $baseConfig -Destination $ConfigPath
}

$recoveredComparisonAssets = $false
if (-not $NoLaunch) {
    if (Get-Process -Name 'opennox', 'opennox-hd', 'opennox-hd-texture2x' -ErrorAction SilentlyContinue) {
        throw 'OpenNox is already running. Close it before starting another session.'
    }
    $recoveredComparisonAssets = Restore-OriginalSpriteComparisonAssets -RuntimeRoot $runtimeRoot
}
$textureScale = Get-ActiveTextureScale
$current = Get-VideoSize -Path $ConfigPath
$display = Get-PrimaryDisplaySize
$recommended = Get-ScaledInternalSize -Width $display.Width -Height $display.Height -Scale $textureScale
$overlayArchive = Join-Path $runtimeRoot 'NoxData\video.bag.zip'
$overlayInstalled = Test-Path -LiteralPath $overlayArchive -PathType Leaf
if ($SpriteMode -eq 'prompt') {
    Write-Host ''
    Write-Host 'Sprite comparison mode'
    Write-Host '  1. Upgraded 2x sprites and HD font sprites [recommended]'
    Write-Host '  2. Original sprites and original fonts (same layout and display size)'
    $answer = Read-Host 'Choose 1-2'
    $SpriteMode = switch ($answer) {
        '' { 'upscaled' }
        '1' { 'upscaled' }
        '2' { 'original' }
        default { throw "Invalid choice: $answer" }
    }
}
$usingUpscaledSprites = $overlayInstalled -and $SpriteMode -eq 'upscaled'
$size1080p = Get-ScaledInternalSize -Width 1920 -Height 1080 -Scale $textureScale
$size1440p = Get-ScaledInternalSize -Width 2560 -Height 1440 -Scale $textureScale
$size4k = Get-ScaledInternalSize -Width 3840 -Height 2160 -Scale $textureScale
$sizeOriginal = Get-ScaledInternalSize -Width (640 * $textureScale) -Height (480 * $textureScale) -Scale $textureScale
$presets = @{
    '1080p' = @($size1080p.Width, $size1080p.Height)
    '1440p' = @($size1440p.Width, $size1440p.Height)
    '4k' = @($size4k.Width, $size4k.Height)
    'original' = @($sizeOriginal.Width, $sizeOriginal.Height)
}
if ($Resolution -eq 'prompt') {
    Write-Host ''
    Write-Host 'OpenNox readable display preset'
    Write-Host "Detected primary display: $($display.Width)x$($display.Height)"
    Write-Host "OpenNox does not scale its HUD. These presets render at ${textureScale}x density"
    Write-Host "and present fullscreen at ${textureScale}x so menus, HUD, and the world stay readable."
    Write-Host ''
    Write-Host "  1. Auto for this display ($($recommended.Width)x$($recommended.Height) internal) [recommended]"
    Write-Host "  2. 1920x1080 display ($($presets['1080p'][0])x$($presets['1080p'][1]) internal)"
    Write-Host "  3. 2560x1440 display ($($presets['1440p'][0])x$($presets['1440p'][1]) internal)"
    Write-Host "  4. 3840x2160 display ($($presets['4k'][0])x$($presets['4k'][1]) internal)"
    Write-Host "  5. Use last saved internal mode ($($current.Width)x$($current.Height))"
    Write-Host '  6. Original view (640x480 internal)'
    Write-Host '  7. Cancel'
    $answer = Read-Host 'Choose 1-7'
    $Resolution = switch ($answer) {
        '' { 'auto' }
        '1' { 'auto' }
        '2' { '1080p' }
        '3' { '1440p' }
        '4' { '4k' }
        '5' { 'last' }
        '6' { 'original' }
        '7' { 'cancel' }
        default { throw "Invalid choice: $answer" }
    }
}
if ($Resolution -eq 'cancel') { return }

if ($Resolution -eq 'auto') {
    Set-VideoSize -Path $ConfigPath -Width $recommended.Width -Height $recommended.Height
} elseif ($Resolution -ne 'last') {
    $size = $presets[$Resolution]
    Set-VideoSize -Path $ConfigPath -Width $size[0] -Height $size[1]
}
$requested = Get-VideoSize -Path $ConfigPath
$legacyConfig = Join-Path $runtimeRoot 'NoxData\nox.cfg'
if (-not (Test-Path -LiteralPath $legacyConfig -PathType Leaf)) {
    $defaultLegacyConfig = Join-Path $runtimeRoot 'NoxData\default.cfg'
    if (-not (Test-Path -LiteralPath $defaultLegacyConfig -PathType Leaf)) {
        throw 'Nox game data is incomplete: NoxData\default.cfg is missing. Run INSTALL.cmd again into a new folder.'
    }
    Copy-Item -LiteralPath $defaultLegacyConfig -Destination $legacyConfig
}
# OpenNox still consults this legacy mode on some startup paths. Keep it in
# lockstep with the launcher-selected YAML size so a preset cannot fall back
# to a stale 4:3 game buffer.
Set-LegacyVideoMode -Path $legacyConfig -Width $requested.Width -Height $requested.Height
Set-LegacyIntegerOption -Path $legacyConfig -Name 'Stretched' -Value 1
Set-LegacyIntegerOption -Path $legacyConfig -Name 'TexturedFloors' -Value 1
$settingsJson = @{ resolution = $Resolution; sprites = $SpriteMode } | ConvertTo-Json
$settingsStage = $preferencesPath + '.stage'
[IO.File]::WriteAllText($settingsStage, $settingsJson, [Text.UTF8Encoding]::new($false))
if (Test-Path -LiteralPath $preferencesPath) {
    [IO.File]::Replace($settingsStage, $preferencesPath, [NullString]::Value)
} else { [IO.File]::Move($settingsStage, $preferencesPath) }
$targetDisplay = switch ($Resolution) {
    '1080p' { [pscustomobject]@{ Width = 1920; Height = 1080 } }
    '1440p' { [pscustomobject]@{ Width = 2560; Height = 1440 } }
    '4k' { [pscustomobject]@{ Width = 3840; Height = 2160 } }
    default { $display }
}

if ($Resolution -eq 'last' -and
    ($requested.Width -gt $recommended.Width -or $requested.Height -gt $recommended.Height)) {
    Write-Warning "The saved internal mode $($requested.Width)x$($requested.Height) is larger than the readable recommendation $($recommended.Width)x$($recommended.Height)."
}

if ($Settings) {
    Write-Host "Saved display choice: $Resolution. Sprite mode: $SpriteMode."
    return
}
if ($NoLaunch) {
    [pscustomobject]@{
        config = $ConfigPath
        selection = $Resolution
        display_width = $display.Width
        display_height = $display.Height
        target_display_width = $targetDisplay.Width
        target_display_height = $targetDisplay.Height
        texture_scale = $textureScale
        sprite_mode = $SpriteMode
        internal_width = $requested.Width
        internal_height = $requested.Height
        recovered_comparison_assets = $recoveredComparisonAssets
        full_texture_overlay_installed = $overlayInstalled
        upscaled_sprite_overlay_used = $usingUpscaledSprites
        launched = $false
    } | ConvertTo-Json -Compress
    return
}

if (Get-Process -Name 'opennox', 'opennox-hd', 'opennox-hd-texture2x' -ErrorAction SilentlyContinue) {
    throw 'OpenNox is already running. Close it before starting another session.'
}
Set-LegacyIntegerOption -Path $legacyConfig -Name 'TexturedFloors' -Value 1
$patchedExecutableName = 'opennox-hd-texture2x.exe'
$patchedExecutable = Join-Path $runtimeRoot $patchedExecutableName
$executable = $patchedExecutable
if (-not (Test-Path -LiteralPath $executable -PathType Leaf)) {
    throw "OpenNox HD executable is missing: $executable"
}
$env:NOX_DATA = Join-Path $runtimeRoot 'NoxData'
$env:NOX_TEXTURE_CACHE_MB = '512'
$env:NOX_TEXTURE_DENSITY_SCALE = [string]$textureScale
$startedAt = Get-Date
$logDir = Join-Path $runtimeRoot 'logs'
[System.IO.Directory]::CreateDirectory($logDir) | Out-Null
$logPath = Join-Path $logDir 'opennox.log'
$previousLog = ''
if (Test-Path -LiteralPath $logPath -PathType Leaf) {
    $previousLog = Join-Path $logDir "opennox.before-launch-$($startedAt.ToString('yyyyMMdd-HHmmssfff')).log"
    Move-Item -LiteralPath $logPath -Destination $previousLog
}
Write-Host "Starting OpenNox for a $($targetDisplay.Width)x$($targetDisplay.Height) display."
Write-Host "Readable internal gameplay mode: $($requested.Width)x$($requested.Height)."
Write-Host "Client: $([System.IO.Path]::GetFileName($executable))."
Write-Host 'Textured floors: enabled.'
if ($usingUpscaledSprites) {
    Write-Host "${textureScale}x sprite overlay: all 124,244 accepted assets (lazy ZIP loading)."
    Write-Host "Decoded ${textureScale}x texture cache: $($env:NOX_TEXTURE_CACHE_MB) MiB with frame-safe LRU eviction."
} elseif ($overlayInstalled) {
    Write-Host 'Original sprite comparison: the installed 2x ZIP and HD font sprites are temporarily hidden for this session.'
    Write-Host "The ${textureScale}x presentation path stays active so layout and display size match the upgraded view."
} else {
    Write-Host "${textureScale}x sprite overlay: no full archive installed."
}
Write-Host 'Fullscreen/windowed and other options will use their last saved values.'
$process = $null
$peakWorkingSet = 0L
$peakPrivateBytes = 0L
$comparisonAssetsStaged = $false
try {
    if ($SpriteMode -eq 'original' -and $overlayInstalled) {
        Stage-OriginalSpriteComparisonAssets -RuntimeRoot $runtimeRoot
        $comparisonAssetsStaged = $true
    }
    $process = Start-Process -FilePath $executable -WorkingDirectory $runtimeRoot -ArgumentList @('-config', ('"{0}"' -f $ConfigPath)) -PassThru
    while (-not $process.WaitForExit(1000)) {
        try {
            $process.Refresh()
            $peakWorkingSet = [Math]::Max($peakWorkingSet, $process.WorkingSet64)
            $peakPrivateBytes = [Math]::Max($peakPrivateBytes, $process.PrivateMemorySize64)
        } catch {
            # The process may exit between WaitForExit and Refresh.
        }
    }
    $process.WaitForExit()
} finally {
    if ($comparisonAssetsStaged) {
        Restore-OriginalSpriteComparisonAssets -RuntimeRoot $runtimeRoot | Out-Null
    }
}

$persisted = Get-VideoSize -Path $ConfigPath
$evidence = @()
if (Test-Path -LiteralPath $logPath -PathType Leaf) {
    $evidence = [System.IO.File]::ReadAllLines($logPath) | Where-Object {
        $_ -match '\[sdl\]\s*:?\s*window size:' -or
        $_ -match '\[video\]\s*:?\s*mode switch:' -or
        ($_ -match '\[render\]\s*:?' -and $_ -match 'surface')
    }
}
$gameplayModePattern = "\[video\]\s*:?\s*mode switch:\s*\($($requested.Width),$($requested.Height)\) \(menu: false\)"
$confirmed = [bool]($evidence | Where-Object { $_ -match $gameplayModePattern } | Select-Object -First 1)
$status = if ($confirmed) {
    'confirmed: runtime log contains the requested internal gameplay mode'
} else {
    'not proven: runtime log did not show the requested internal gameplay mode; enter gameplay before exiting'
}
$reportPath = Join-Path $runtimeRoot 'logs\last-resolution-check.txt'
$report = @(
    "Started: $($startedAt.ToString('o'))"
    "Exited: $((Get-Date).ToString('o'))"
    "Binary: $executable"
    "Config: $ConfigPath"
    "Previous runtime log: $previousLog"
    "Selected: $Resolution"
    "Detected physical display: $($display.Width)x$($display.Height)"
    "Preset target display: $($targetDisplay.Width)x$($targetDisplay.Height)"
    "Requested internal gameplay mode: $($requested.Width)x$($requested.Height)"
    "Persisted internal gameplay mode after clean exit: $($persisted.Width)x$($persisted.Height)"
    "Exit code: $($process.ExitCode)"
    "Texture density scale: ${textureScale}x"
    "Sprite mode: $SpriteMode"
    "Full texture overlay installed: $overlayInstalled"
    "Upscaled sprite overlay used: $usingUpscaledSprites"
    "Original comparison assets restored: $comparisonAssetsStaged"
    "Decoded texture cache limit: $($env:NOX_TEXTURE_CACHE_MB) MiB"
    "Observed peak working set: $([Math]::Round($peakWorkingSet / 1MB, 1)) MiB"
    "Observed peak private bytes: $([Math]::Round($peakPrivateBytes / 1MB, 1)) MiB"
    "Verification: $status"
    ''
    'Observed runtime evidence:'
) + $evidence
[System.IO.Directory]::CreateDirectory((Split-Path -Parent $reportPath)) | Out-Null
[System.IO.File]::WriteAllLines($reportPath, $report, [System.Text.UTF8Encoding]::new($false))
Write-Host "Saved internal gameplay mode: $($persisted.Width)x$($persisted.Height)"
Write-Host "Resolution verification: $status"
Write-Host "Report: $reportPath"
if ($process.ExitCode -ne 0) { exit $process.ExitCode }
