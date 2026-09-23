#requires -Version 5.1
[CmdletBinding()]
param(
    [ValidateSet('2', '4', 'Both')][string]$Scale = 'Both',
    [switch]$Force
)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$root = $PSScriptRoot
$data = Join-Path $root 'NoxData'
$builder = Join-Path $root 'OpenNox-SpriteBuilder.exe'
if (-not (Test-Path -LiteralPath $builder -PathType Leaf)) {
    throw 'OpenNox-SpriteBuilder.exe is missing. Re-extract the complete release or run UPDATE.cmd.'
}
foreach ($name in @('video.bag', 'video.idx')) {
    if (-not (Test-Path -LiteralPath (Join-Path $data $name) -PathType Leaf)) {
        throw "Your installed NoxData is missing $name."
    }
}
if (Get-Process -Name 'opennox', 'opennox-hd', 'opennox-hd-texture2x' -ErrorAction SilentlyContinue) {
    throw 'Close OpenNox before building sprites.'
}

$scales = if ($Scale -eq 'Both') { @(2, 4) } else { @([int]$Scale) }
foreach ($factor in $scales) {
    $output = if ($factor -eq 2) { Join-Path $data 'video.bag.zip' }
              else { Join-Path $root 'generated-sprites\4x\video.bag.zip' }
    if ((Test-Path -LiteralPath $output -PathType Leaf) -and -not $Force) {
        Write-Host "Existing ${factor}x archive kept: $output (use -Force to rebuild)"
        continue
    }
    Write-Host "Building ${factor}x sprites from your own installed Nox files. This may take a while..."
    & $builder -data $data -output $output -scale $factor
    if ($LASTEXITCODE -ne 0) { throw "${factor}x sprite generation failed with exit code $LASTEXITCODE." }
    if (-not (Test-Path -LiteralPath $output -PathType Leaf)) { throw "${factor}x archive is missing after generation." }
}
Write-Host '2x sprites are playable in OpenNox HD. The 4x archive is an export for future builds; the current launcher runs at 2x.'
