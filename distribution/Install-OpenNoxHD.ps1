#requires -Version 5.1
[CmdletBinding()]
param(
    [string]$GamePath = '',
    [string]$Destination = '',
    [switch]$NoShortcut,
    [switch]$Upgrade,
    [switch]$GenerateSprites
)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

function FullPath([string]$Path) {
    $full = [IO.Path]::GetFullPath($Path)
    if ($full -eq [IO.Path]::GetPathRoot($full)) { return $full }
    $full.TrimEnd([IO.Path]::DirectorySeparatorChar)
}
function IsWithin([string]$Child, [string]$Parent) {
    $Child.Equals($Parent, [StringComparison]::OrdinalIgnoreCase) -or
        $Child.StartsWith($Parent.TrimEnd([IO.Path]::DirectorySeparatorChar) + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)
}
function Assert-NoLinks([string]$Path) {
    $current = $Path
    while ($current) {
        if (Test-Path -LiteralPath $current) {
            $item = Get-Item -LiteralPath $current -Force
            if ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) {
                throw "Linked folders/files are not supported during installation: $current"
            }
        }
        $parent = Split-Path -Parent $current
        if ($parent -eq $current) { break }
        $current = $parent
    }
}
function Safe-PayloadPath([string]$Root, [string]$Relative) {
    if ([string]::IsNullOrWhiteSpace($Relative) -or $Relative -match '(^[\\/]|:|(^|[\\/])\.\.?([\\/]|$))') {
        throw "Unsafe package path: $Relative"
    }
    $path = FullPath (Join-Path $Root $Relative)
    if (-not (IsWithin $path $Root) -or $path -eq $Root) { throw "Unsafe package path: $Relative" }
    Assert-NoLinks $path
    $path
}
function File-Sha256([string]$Path) {
    $stream = [IO.File]::OpenRead($Path)
    $algorithm = [Security.Cryptography.SHA256]::Create()
    try { [BitConverter]::ToString($algorithm.ComputeHash($stream)).Replace('-', '').ToLowerInvariant() }
    finally { $algorithm.Dispose(); $stream.Dispose() }
}
function Copy-Verified([string]$Source, [string]$Target, [string]$Sha256) {
    [void][IO.Directory]::CreateDirectory((Split-Path -Parent $Target))
    Copy-Item -LiteralPath $Source -Destination $Target
    if ((File-Sha256 $Target) -ne $Sha256) {
        throw "Copy verification failed: $Target"
    }
}
function Select-Folder([string]$Description, [string]$Initial = '') {
    Add-Type -AssemblyName System.Windows.Forms
    $dialog = New-Object System.Windows.Forms.FolderBrowserDialog
    $dialog.Description = $Description
    $dialog.SelectedPath = $Initial
    try {
        if ($dialog.ShowDialog() -ne [Windows.Forms.DialogResult]::OK) { throw 'Installation cancelled.' }
        $dialog.SelectedPath
    } finally { $dialog.Dispose() }
}
function Find-NoxInstall {
    $candidates = [Collections.Generic.List[string]]::new()
    foreach ($base in @($env:ProgramFiles, ${env:ProgramFiles(x86)}, 'C:\GOG Games', 'C:\Games')) {
        if ($base) { $candidates.Add((Join-Path $base 'Nox')); $candidates.Add((Join-Path $base 'GOG Galaxy\Games\Nox')) }
    }
    foreach ($registry in @('HKLM:\SOFTWARE\GOG.com\Games', 'HKLM:\SOFTWARE\WOW6432Node\GOG.com\Games', 'HKCU:\SOFTWARE\GOG.com\Games')) {
        foreach ($key in @(Get-ChildItem -LiteralPath $registry -ErrorAction SilentlyContinue)) {
            $values = Get-ItemProperty -LiteralPath $key.PSPath
            if ($values.PSObject.Properties['path'] -and $values.path) { $candidates.Add([string]$values.path) }
        }
    }
    foreach ($path in $candidates) {
        if ((Test-Path -LiteralPath (Join-Path $path 'video.bag')) -or
            (Test-Path -LiteralPath (Join-Path $path 'NoxData\video.bag'))) { return $path }
    }
    return ''
}

$packageRoot = FullPath $PSScriptRoot
$payload = Join-Path $packageRoot 'payload'
$manifest = Get-Content -LiteralPath (Join-Path $packageRoot 'package-manifest.json') -Raw | ConvertFrom-Json
if ($manifest.format -ne 'opennox-hd-package-v1' -or $manifest.scale -ne 2 -or @($manifest.files).Count -eq 0) {
    throw 'This is not a supported OpenNox HD 2x package. Download and extract the complete release ZIP.'
}
$seen = @{}
foreach ($file in $manifest.files) {
    $relative = [string]$file.path
    $path = Safe-PayloadPath $payload $relative
    if ($seen.ContainsKey($path)) { throw "Duplicate package path: $relative" }
    $seen[$path] = $true
    if ($file.sha256 -notmatch '^[0-9a-fA-F]{64}$' -or -not (Test-Path -LiteralPath $path -PathType Leaf)) {
        throw "Missing or invalid package file: $relative. Extract the entire ZIP again."
    }
    if ((File-Sha256 $path) -ne $file.sha256) {
        throw "Package checksum failed: $relative. Download the release again."
    }
}
foreach ($required in @('opennox-hd-texture2x.exe', 'OpenNox-SpriteBuilder.exe', 'SDL2.dll', 'OpenAL32.dll', 'OpenNox-Launcher.ps1', 'START-OPENNOX.cmd', 'Build-HD-Sprites.ps1', 'BUILD-HD-SPRITES.cmd', 'opennox.yml')) {
    if (-not $seen.ContainsKey((Safe-PayloadPath $payload $required))) { throw "Package is incomplete: $required" }
}

if ($Upgrade) {
    if (Get-Process -Name 'opennox', 'opennox-hd', 'opennox-hd-texture2x' -ErrorAction SilentlyContinue) {
        throw 'Close OpenNox before updating so saves and settings can be preserved.'
    }
    if ([string]::IsNullOrWhiteSpace($Destination)) {
        $Destination = Select-Folder 'Select the OpenNox HD folder to update' (Join-Path ([Environment]::GetFolderPath('LocalApplicationData')) 'OpenNox-HD')
    }
    $Destination = FullPath $Destination
    Assert-NoLinks $Destination
    $installedMarker = Join-Path $Destination 'package-manifest.json'
    if (-not (Test-Path -LiteralPath $installedMarker -PathType Leaf)) { throw 'Update requires an existing OpenNox HD installation with package-manifest.json.' }
    $installed = Get-Content -LiteralPath $installedMarker -Raw | ConvertFrom-Json
    if ($installed.format -ne 'opennox-hd-package-v1' -or $installed.scale -ne 2) { throw 'Unrecognized installation; choose an OpenNox HD 2x folder.' }
    $comparisonStage = Join-Path $Destination 'CodexBackups\original-sprite-comparison\staged'
    if ((Test-Path -LiteralPath $comparisonStage) -and @(Get-ChildItem -LiteralPath $comparisonStage -Recurse -File).Count) {
        throw 'An interrupted sprite comparison needs recovery. Start and close the installed game before updating.'
    }
    $GamePath = Join-Path $Destination 'NoxData'
}
if ([string]::IsNullOrWhiteSpace($GamePath)) {
    Write-Host 'Select your installed copy of Nox (the folder containing video.bag).'
    $detected = Find-NoxInstall
    if ($detected) { Write-Host "Found Nox: $detected" }
    $GamePath = Select-Folder 'Select your Nox installation or its NoxData folder' $detected
}
$GamePath = FullPath $GamePath
if (-not (Test-Path -LiteralPath (Join-Path $GamePath 'video.bag') -PathType Leaf) -and
    (Test-Path -LiteralPath (Join-Path $GamePath 'NoxData\video.bag') -PathType Leaf)) {
    $GamePath = Join-Path $GamePath 'NoxData'
}
Assert-NoLinks $GamePath
$requiredData = @('video.bag', 'video.idx', 'audio.bag', 'audio.idx', 'thing.bin', 'gamedata.bin',
    'modifier.bin', 'monster.bin', 'soundset.bin', 'nox.csf', 'default.cfg', 'default.pal',
    'default.fnt', 'large.fnt', 'small.fnt', 'number.fnt')
foreach ($name in $requiredData) {
    $path = Join-Path $GamePath $name
    if (-not (Test-Path -LiteralPath $path -PathType Leaf) -or (Get-Item -LiteralPath $path).Length -eq 0) {
        throw "Nox installation is incomplete: $name is missing or empty in $GamePath"
    }
}
if (-not (Test-Path -LiteralPath (Join-Path $GamePath 'maps') -PathType Container) -or
    @(Get-ChildItem -LiteralPath (Join-Path $GamePath 'maps') -Filter '*.map' -Recurse -File).Count -eq 0) {
    throw "No Nox maps found in $GamePath\maps. Select the complete game installation."
}
if ([string]::IsNullOrWhiteSpace($Destination)) {
    $defaultDestination = Join-Path ([Environment]::GetFolderPath('LocalApplicationData')) 'OpenNox-HD'
    $answer = Read-Host "Install folder [Enter for $defaultDestination]"
    $Destination = if ([string]::IsNullOrWhiteSpace($answer)) { $defaultDestination } else { $answer.Trim('"') }
}
$Destination = FullPath $Destination
Assert-NoLinks $Destination
foreach ($protected in @($GamePath, $packageRoot)) {
    if ($Upgrade -and $protected -eq $GamePath) { continue }
    if ((IsWithin $Destination $protected) -or (IsWithin $protected $Destination)) {
        throw 'Choose a separate install folder outside both the original game and the extracted download.'
    }
}
if (-not $Upgrade -and (Test-Path -LiteralPath $Destination)) {
    throw "Install folder already exists: $Destination. Choose a new folder; existing games and saves are never overwritten."
}

# Import only game content; exclude saves, configs, keys, logs, launchers and DLLs.
$imports = [Collections.Generic.List[object]]::new()
if ($Upgrade) {
    # Clone the complete existing installation, including saves and custom files.
    # Keep the old directory intact until the replacement is fully verified.
    foreach ($item in @(Get-ChildItem -LiteralPath $Destination -Recurse -Force)) {
        if ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) { throw "Linked installation content is not supported: $($item.FullName)" }
        if (-not $item.PSIsContainer) { $imports.Add($item) }
    }
} else {
Get-ChildItem -LiteralPath $GamePath -File | Where-Object {
    $_.Extension.ToLowerInvariant() -in @('.bag', '.idx', '.bin', '.csf', '.fnt', '.pal', '.rul') -and
    $_.Name -notlike '*.hd2.fnt'
} | ForEach-Object { $imports.Add($_) }
$imports.Add((Get-Item -LiteralPath (Join-Path $GamePath 'default.cfg')))
foreach ($folder in @('maps', 'Dialog', 'MOVIES', 'MUSIC', 'window', 'data', 'images')) {
    $path = Join-Path $GamePath $folder
    if (-not (Test-Path -LiteralPath $path -PathType Container)) { continue }
    Assert-NoLinks $path
    $entries = @(Get-ChildItem -LiteralPath $path -Recurse -Force)
    foreach ($item in $entries) {
        if ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) { throw "Linked game content is not supported: $($item.FullName)" }
        if (-not $item.PSIsContainer -and $item.Extension.ToLowerInvariant() -notin @('.exe', '.dll', '.ps1', '.cmd', '.bat', '.pem', '.key')) {
            $imports.Add($item)
        }
    }
}
}
foreach ($item in $imports) { Assert-NoLinks $item.FullName }
$bytes = ($imports | Measure-Object -Property Length -Sum).Sum + ($manifest.files | Measure-Object -Property bytes -Sum).Sum
$drive = New-Object IO.DriveInfo ([IO.Path]::GetPathRoot($Destination))
$spriteReserve = if ($GenerateSprites -and -not $Upgrade -and -not $manifest.includes_overlay) { 2GB } else { 0 }
if ($drive.AvailableFreeSpace -lt ($bytes + 256MB + $spriteReserve)) { throw 'Not enough free space on the installation drive.' }

$stage = $Destination + '.install-' + [Guid]::NewGuid().ToString('N')
$backup = $Destination + '.previous-' + (Get-Date).ToString('yyyyMMdd-HHmmss') + '-' + [Guid]::NewGuid().ToString('N').Substring(0, 8)
$oldMoved = $false
try {
    [void][IO.Directory]::CreateDirectory($stage)
    Write-Host 'Copying and verifying Nox data. This can take several minutes...'
    $completed = 0
    $total = $imports.Count + @($manifest.files).Count
    foreach ($item in $imports) {
        $importRoot = if ($Upgrade) { $Destination } else { $GamePath }
        $targetRoot = if ($Upgrade) { $stage } else { Join-Path $stage 'NoxData' }
        $relative = $item.FullName.Substring($importRoot.Length).TrimStart('\', '/')
        $target = Safe-PayloadPath $targetRoot $relative
        Copy-Verified $item.FullName $target (File-Sha256 $item.FullName)
        $completed++
        Write-Progress -Activity 'Installing OpenNox HD' -Status "$completed of $total files: $relative" -PercentComplete (100 * $completed / $total)
    }
    Write-Host 'Installing OpenNox HD...'
    foreach ($file in $manifest.files) {
        Copy-Verified (Safe-PayloadPath $payload $file.path) (Safe-PayloadPath $stage $file.path) $file.sha256
        $completed++
        Write-Progress -Activity 'Installing OpenNox HD' -Status "$completed of $total files: $($file.path)" -PercentComplete (100 * $completed / $total)
    }
    Copy-Item -LiteralPath (Join-Path $packageRoot 'package-manifest.json') -Destination (Join-Path $stage 'package-manifest.json')
    if ($GenerateSprites -and -not $Upgrade -and -not $manifest.includes_overlay) {
        Write-Host 'Generating 2x artwork from your copied Nox files. This may take a while...'
        & (Join-Path $stage 'OpenNox-SpriteBuilder.exe') -data (Join-Path $stage 'NoxData') -output (Join-Path $stage 'NoxData\video.bag.zip') -scale 2
        if ($LASTEXITCODE -ne 0 -or -not (Test-Path -LiteralPath (Join-Path $stage 'NoxData\video.bag.zip') -PathType Leaf)) {
            throw "2x sprite generation failed with exit code $LASTEXITCODE."
        }
    }
    # Validate real first-run settings before making the install visible.
    & (Join-Path $stage 'OpenNox-Launcher.ps1') -NoLaunch | Out-Null
    if ($Upgrade) {
        if (Get-Process -Name 'opennox', 'opennox-hd', 'opennox-hd-texture2x' -ErrorAction SilentlyContinue) { throw 'OpenNox was started during the update. Close it and retry.' }
        # Both rename targets are validated siblings of the selected install.
        if ((Split-Path -Parent $backup) -ne (Split-Path -Parent $Destination) -or (Test-Path -LiteralPath $backup)) { throw 'Invalid rollback location.' }
        [IO.Directory]::Move($Destination, $backup)
        $oldMoved = $true
    }
    [IO.Directory]::Move($stage, $Destination)
} catch {
    if ($oldMoved -and -not (Test-Path -LiteralPath $Destination)) { [IO.Directory]::Move($backup, $Destination) }
    # Preserve a failed stage for diagnosis; never recursively delete a computed path.
    throw "Installation failed: $($_.Exception.Message) No existing installation was changed. Incomplete staging folder: $stage"
}
Write-Progress -Activity 'Installing OpenNox HD' -Completed
if ($Upgrade) { Write-Host "Update complete. Saves and settings preserved. Rollback copy: $backup" }
if (-not $NoShortcut -and -not $Upgrade) {
    try {
        $desktop = [Environment]::GetFolderPath('DesktopDirectory')
        $shortcutPath = Join-Path $desktop 'OpenNox HD.lnk'
        if (Test-Path -LiteralPath $shortcutPath) { $shortcutPath = Join-Path $desktop ('OpenNox HD-' + [Guid]::NewGuid().ToString('N').Substring(0, 8) + '.lnk') }
        $shell = New-Object -ComObject WScript.Shell
        $shortcut = $shell.CreateShortcut($shortcutPath)
        $shortcut.TargetPath = Join-Path $Destination 'START-OPENNOX.cmd'
        $shortcut.WorkingDirectory = $Destination
        $shortcut.IconLocation = (Join-Path $Destination 'opennox-hd-texture2x.exe') + ',0'
        $shortcut.Save()
    } catch { Write-Warning "Installed successfully, but the desktop shortcut could not be created: $($_.Exception.Message)" }
}
Write-Host "Installed successfully: $Destination"
Write-Host 'Start with START-OPENNOX.cmd or the desktop shortcut.'
Write-Host 'Change resolution and sprite choices with SETTINGS.cmd.'
Write-Host 'Run BUILD-HD-SPRITES.cmd to generate a separate 4x export (the current launcher plays 2x).'
Write-Host 'To uninstall, close the game and delete this separate folder and its shortcut. Back up NoxData\Save first.'
