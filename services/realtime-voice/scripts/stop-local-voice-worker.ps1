$ErrorActionPreference = "Stop"

$repoRoot = Resolve-Path (Join-Path $PSScriptRoot "..\..\..")
$pidPath = Join-Path $repoRoot "data\runtime\realtime-voice-worker.pid"

if (Test-Path $pidPath) {
    $pidValue = Get-Content $pidPath
    Stop-Process -Id $pidValue -Force -ErrorAction SilentlyContinue
}

Get-CimInstance Win32_Process |
    Where-Object { $_.CommandLine -match "rosie_realtime_voice.app:app" } |
    ForEach-Object { Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue }
