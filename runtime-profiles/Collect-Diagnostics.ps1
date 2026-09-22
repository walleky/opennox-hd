#requires -Version 5.1
$ErrorActionPreference = 'Stop'
$root = $PSScriptRoot
$report = [ordered]@{ created_utc = [DateTime]::UtcNow.ToString('o'); windows = [Environment]::OSVersion.VersionString; processor_count = [Environment]::ProcessorCount }
$binaries = @{}
foreach ($name in @('opennox-hd-texture2x.exe', 'SDL2.dll', 'OpenAL32.dll')) {
    $path = Join-Path $root $name
    if (Test-Path -LiteralPath $path -PathType Leaf) {
        $stream = [IO.File]::OpenRead($path)
        $hash = [Security.Cryptography.SHA256]::Create()
        try { $binaries[$name] = [BitConverter]::ToString($hash.ComputeHash($stream)).Replace('-', '').ToLowerInvariant() }
        finally { $hash.Dispose(); $stream.Dispose() }
    }
}
$report.binaries = $binaries
foreach ($name in @('package-manifest.json', 'launcher-settings.json')) {
    $path = Join-Path $root $name
    if (Test-Path -LiteralPath $path) {
        try {
            $value = Get-Content -LiteralPath $path -Raw | ConvertFrom-Json
            if ($name -eq 'package-manifest.json') { $report.version = $value.version }
            else { $report.resolution = $value.resolution; $report.sprites = $value.sprites }
        } catch { $report[$name] = 'unreadable' }
    }
}
$log = Join-Path $root 'logs\opennox.log'
$report.performance = @()
if (Test-Path -LiteralPath $log) {
    # Export only numeric renderer counters, never raw logs, player names or paths.
    $report.performance = @(Get-Content -LiteralPath $log -Tail 10000 | ForEach-Object {
        if ($_ -match '(\[hdperf\] frames=.*)$') { $Matches[1] }
    } | Select-Object -Last 120)
}
$output = Join-Path $root ('diagnostics-' + (Get-Date).ToString('yyyyMMdd-HHmmssfff') + '.json')
[IO.File]::WriteAllText($output, ($report | ConvertTo-Json -Depth 5), [Text.UTF8Encoding]::new($false))
Write-Host "Diagnostics saved: $output"
Write-Host 'Review this file before sharing it in a GitHub issue. Nothing was uploaded.'
