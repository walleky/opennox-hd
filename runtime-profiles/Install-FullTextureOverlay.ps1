[CmdletBinding()]
param(
    [ValidateSet('Install', 'Restore')]
    [string]$Action = 'Install',
    [string]$RuntimeRoot = (Join-Path $env:USERPROFILE 'Documents\OpenNox-Upscale'),
    [ValidateSet(2)]
    [int]$Scale = 2,
    [string]$Archive = '',
    [string]$PatchedExecutable = '',
    [string]$LauncherProfile = '',
    [string]$LauncherCommand = '',
    [string]$RuntimeExecutableName = '',
    [string]$FontSpriteDirectory = ''
)

$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
if ([string]::IsNullOrWhiteSpace($LauncherProfile)) { $LauncherProfile = Join-Path $PSScriptRoot 'OpenNox-Launcher.ps1' }
if ([string]::IsNullOrWhiteSpace($LauncherCommand)) { $LauncherCommand = Join-Path $PSScriptRoot 'START-OPENNOX.cmd' }
if ([string]::IsNullOrWhiteSpace($Archive)) {
    # The active supported release is the codec-aware Hybrid 2x archive.
    # The older bulk-upscale archive is retained only for offline comparison.
    $Archive = Join-Path $projectRoot 'builds\codec-aware-2x\nox-hybrid\full-overlay\video.bag.zip'
}
if ([string]::IsNullOrWhiteSpace($PatchedExecutable)) {
    $PatchedExecutable = Join-Path $projectRoot 'runtime\build-menu-multicore-2tiles\opennox-hd-menu-multicore-2tiles.exe'
}
if ([string]::IsNullOrWhiteSpace($RuntimeExecutableName)) {
    $RuntimeExecutableName = 'opennox-hd-texture2x.exe'
}
if ([string]::IsNullOrWhiteSpace($FontSpriteDirectory) -and $Scale -eq 2) {
    $FontSpriteDirectory = Join-Path $projectRoot 'builds\font-overlay\alegreya-sans-medium'
}
$stateRoot = Join-Path $RuntimeRoot 'CodexBackups\full-texture-overlay'
$activeManifest = Join-Path $stateRoot 'active-manifest.json'
$pilotManifest = Join-Path $RuntimeRoot 'CodexBackups\texture-density-pilot\active-manifest.json'

function Get-Sha256([string]$Path) {
    $stream = [IO.File]::OpenRead($Path)
    $algorithm = [Security.Cryptography.SHA256]::Create()
    try { [BitConverter]::ToString($algorithm.ComputeHash($stream)).Replace('-', '').ToLowerInvariant() }
    finally { $algorithm.Dispose(); $stream.Dispose() }
}

function Safe-OverlayPath([string]$Root, [string]$RelativePath) {
    if ([string]::IsNullOrWhiteSpace($RelativePath) -or $RelativePath -match '(^[\\/]|:|(^|[\\/])\.\.?([\\/]|$))') {
        throw "Unsafe overlay path: $RelativePath"
    }
    $rootFull = [IO.Path]::GetFullPath($Root).TrimEnd('\')
    $target = [IO.Path]::GetFullPath((Join-Path $rootFull $RelativePath))
    if (-not $target.StartsWith($rootFull + '\', [StringComparison]::OrdinalIgnoreCase)) {
        throw "Unsafe overlay path: $RelativePath"
    }
    $current = $target
    while ($current) {
        if ((Test-Path -LiteralPath $current) -and ((Get-Item -LiteralPath $current -Force).Attributes -band [IO.FileAttributes]::ReparsePoint)) {
            throw "Linked overlay path is not supported: $current"
        }
        $current = Split-Path -Parent $current
    }
    $target
}

function Assert-GameStopped {
    if (Get-Process -Name 'opennox', 'opennox-hd', 'opennox-hd-texture2x' -ErrorAction SilentlyContinue) {
        throw 'OpenNox is running. Exit the game before installing or restoring the full overlay.'
    }
}

Assert-GameStopped
if (-not (Test-Path -LiteralPath $RuntimeRoot -PathType Container)) {
    throw "Runtime does not exist: $RuntimeRoot"
}

if ($Action -eq 'Restore') {
    if (-not (Test-Path -LiteralPath $activeManifest -PathType Leaf)) {
        throw "No active full-overlay manifest exists: $activeManifest"
    }
    $manifest = Get-Content -Raw -LiteralPath $activeManifest | ConvertFrom-Json
    $restoreFiles = @($manifest.files)
    [array]::Reverse($restoreFiles)
    # Check the entire restore before deleting or overwriting any installed file.
    $seen = @{}
    foreach ($file in $restoreFiles) {
        $target = Safe-OverlayPath $RuntimeRoot $file.relative_path
        if ($seen.ContainsKey($target)) { throw "Duplicate restore path: $target" }
        $seen[$target] = $true
        if ((Test-Path -LiteralPath $target -PathType Leaf) -and (Get-Sha256 $target) -ne $file.installed_sha256) {
            throw "Refusing to overwrite a changed full-overlay file: $target"
        }
        if ($file.had_backup) {
            $backup = Safe-OverlayPath ([string]$manifest.baseline_root) $file.relative_path
            if (-not (Test-Path -LiteralPath $backup -PathType Leaf)) { throw "Recorded backup is missing: $backup" }
            if ($file.backup_sha256 -and (Get-Sha256 $backup) -ne $file.backup_sha256) { throw "Backup checksum mismatch: $backup" }
        }
    }
    foreach ($file in $restoreFiles) {
        $target = Safe-OverlayPath $RuntimeRoot $file.relative_path
        if (Test-Path -LiteralPath $target -PathType Leaf) {
            $actual = Get-Sha256 $target
            if ($actual -ne $file.installed_sha256) {
                throw "Refusing to overwrite a changed full-overlay file: $target"
            }
            Remove-Item -LiteralPath $target -Force
        }
        if ($file.had_backup) {
            $backup = Safe-OverlayPath ([string]$manifest.baseline_root) $file.relative_path
            if (-not (Test-Path -LiteralPath $backup -PathType Leaf)) {
                throw "Recorded backup is missing: $backup"
            }
            [System.IO.Directory]::CreateDirectory((Split-Path -Parent $target)) | Out-Null
            Copy-Item -LiteralPath $backup -Destination $target -Force
        }
    }
    $restoredManifest = Join-Path $stateRoot ("restored-{0}.json" -f (Get-Date -Format 'yyyyMMdd-HHmmss'))
    Move-Item -LiteralPath $activeManifest -Destination $restoredManifest
    Write-Host "Full texture overlay restored. Record: $restoredManifest"
    return
}

if (Test-Path -LiteralPath $activeManifest -PathType Leaf) {
    throw "The full texture overlay is already active: $activeManifest"
}
if (Test-Path -LiteralPath $pilotManifest -PathType Leaf) {
    throw "The eight-frame pilot is still active. Restore it before installing the full overlay: $pilotManifest"
}
foreach ($required in @($Archive, $PatchedExecutable, $LauncherProfile, $LauncherCommand)) {
    if (-not (Test-Path -LiteralPath $required -PathType Leaf)) {
        throw "Required full-overlay input is missing: $required"
    }
}

$fontSpriteFiles = @()
if (-not [string]::IsNullOrWhiteSpace($FontSpriteDirectory)) {
    $fontSpriteFiles = @('default.hd2.fnt', 'large.hd2.fnt', 'small.hd2.fnt', 'number.hd2.fnt', 'OFL.txt', 'manifest.json')
    foreach ($name in $fontSpriteFiles) {
        $source = Join-Path $FontSpriteDirectory $name
        if (-not (Test-Path -LiteralPath $source -PathType Leaf)) {
            throw "Required HD font sprite input is missing: $source"
        }
    }
    $fontManifest = Get-Content -Raw -LiteralPath (Join-Path $FontSpriteDirectory 'manifest.json') | ConvertFrom-Json
    if ([string]$fontManifest.format -ne 'opennox-hd-font-sprites-v1' -or $fontManifest.fonts.Count -ne 4) {
        throw "HD font sprite manifest is invalid: $FontSpriteDirectory"
    }
}

$archiveManifestPath = "$Archive.manifest.json"
if (-not (Test-Path -LiteralPath $archiveManifestPath -PathType Leaf)) {
    throw "Verified archive manifest is missing: $archiveManifestPath"
}
$archiveManifest = Get-Content -Raw -LiteralPath $archiveManifestPath | ConvertFrom-Json
if ($archiveManifest.status -ne 'verified' -or -not $archiveManifest.full_crc_verified) {
    throw "Archive manifest does not record a full CRC verification: $archiveManifestPath"
}
if ([int]$archiveManifest.assets -ne 124244 -or [int]$archiveManifest.counts.entries -ne 326248) {
    throw "Archive manifest has unexpected coverage: $archiveManifestPath"
}
$archiveGeometryPolicy = [string]$archiveManifest.geometry_policy
if ([int]$archiveManifest.scale -ne $Scale -or $archiveGeometryPolicy -notmatch '^logical-point-native-2x(?:-[A-Za-z0-9]+)*-v[0-9]+$') {
    throw "Archive manifest does not identify a supported native 2x geometry policy: $archiveManifestPath"
}
$archiveSha = Get-Sha256 $Archive
if ($archiveSha -ne [string]$archiveManifest.archive_sha256) {
    throw "Archive hash does not match its verified manifest: $Archive"
}

$installId = Get-Date -Format 'yyyyMMdd-HHmmssfff'
$baselineRoot = Join-Path $stateRoot "baseline-$installId"
[System.IO.Directory]::CreateDirectory($baselineRoot) | Out-Null
$installed = [System.Collections.Generic.List[object]]::new()

function Install-OverlayFile([string]$Source, [string]$RelativePath, [string]$KnownSourceSha = '') {
    $target = Safe-OverlayPath $RuntimeRoot $RelativePath
    $backup = Safe-OverlayPath $baselineRoot $RelativePath
    $hadBackup = Test-Path -LiteralPath $target -PathType Leaf
    if ($hadBackup) {
        [System.IO.Directory]::CreateDirectory((Split-Path -Parent $backup)) | Out-Null
        Copy-Item -LiteralPath $target -Destination $backup -Force
    }
    [System.IO.Directory]::CreateDirectory((Split-Path -Parent $target)) | Out-Null
    $stage = "$target.full-overlay-stage"
    Copy-Item -LiteralPath $Source -Destination $stage -Force
    $sourceSha = if ($KnownSourceSha) { $KnownSourceSha } else { Get-Sha256 $Source }
    $stageSha = Get-Sha256 $stage
    if ($stageSha -ne $sourceSha) {
        Remove-Item -LiteralPath $stage -Force
        throw "Staged file hash mismatch: $target"
    }
    # All files are staged and verified before the first runtime mutation.
    $installed.Add([ordered]@{
        relative_path = $RelativePath
        source_sha256 = $sourceSha
        installed_sha256 = $stageSha
        had_backup = $hadBackup
        backup_sha256 = if ($hadBackup) { Get-Sha256 $backup } else { $null }
    }) | Out-Null
}

Install-OverlayFile $PatchedExecutable $RuntimeExecutableName
Install-OverlayFile $LauncherProfile 'OpenNox-Launcher.ps1'
Install-OverlayFile $LauncherCommand 'START-OPENNOX.cmd'
Install-OverlayFile $Archive 'NoxData\video.bag.zip' $archiveSha
if ($fontSpriteFiles.Count -ne 0) {
    foreach ($name in @('default.hd2.fnt', 'large.hd2.fnt', 'small.hd2.fnt', 'number.hd2.fnt')) {
        Install-OverlayFile (Join-Path $FontSpriteDirectory $name) (Join-Path 'NoxData' $name)
    }
    Install-OverlayFile (Join-Path $FontSpriteDirectory 'OFL.txt') 'NoxData\font-licenses\AlegreyaSans-OFL.txt'
    Install-OverlayFile (Join-Path $FontSpriteDirectory 'manifest.json') 'NoxData\font-licenses\AlegreyaSans-manifest.json'
}

$installRecord = [ordered]@{
    status = 'active'
    installed_at = (Get-Date).ToString('o')
    runtime_root = $RuntimeRoot
    baseline_root = $baselineRoot
    renderer_sha256 = Get-Sha256 $PatchedExecutable
    archive_source = [System.IO.Path]::GetFullPath($Archive)
    archive_manifest = $archiveManifestPath
    archive_sha256 = $archiveSha
    assets = 124244
    entries = 326248
    scale = $Scale
    geometry_policy = $archiveGeometryPolicy
    runtime_executable = $RuntimeExecutableName
    texture_cache_mb = 512
    font_sprites = if ($fontSpriteFiles.Count -ne 0) {
        [ordered]@{
            family = 'Alegreya Sans Medium'
            scale = 2
            source_manifest = (Join-Path $FontSpriteDirectory 'manifest.json')
        }
    } else { $null }
    files = @($installed)
}
[System.IO.Directory]::CreateDirectory($stateRoot) | Out-Null
[System.IO.File]::WriteAllText(
    "$activeManifest.stage",
    (($installRecord | ConvertTo-Json -Depth 7) + [Environment]::NewLine),
    [System.Text.UTF8Encoding]::new($false)
)
$replaced = [Collections.Generic.List[object]]::new()
try {
    foreach ($file in $installed) {
        $target = Safe-OverlayPath $RuntimeRoot $file.relative_path
        if ($file.had_backup) {
            [IO.File]::Replace("$target.full-overlay-stage", $target, $null)
        } else {
            [IO.File]::Move("$target.full-overlay-stage", $target)
        }
        $replaced.Add($file)
    }
    [IO.File]::Move("$activeManifest.stage", $activeManifest)
} catch {
    foreach ($file in $replaced) {
        $target = Safe-OverlayPath $RuntimeRoot $file.relative_path
        if ($file.had_backup) {
            Copy-Item -LiteralPath (Safe-OverlayPath $baselineRoot $file.relative_path) -Destination "$target.rollback-stage"
            [IO.File]::Replace("$target.rollback-stage", $target, $null)
        } else { Remove-Item -LiteralPath $target }
    }
    throw
}
Write-Host "Installed all 124,244 accepted ${Scale}x sprites as one verified ZIP overlay."
Write-Host "Active manifest: $activeManifest"
Write-Host 'Run START-OPENNOX.cmd and choose a readable display preset.'
