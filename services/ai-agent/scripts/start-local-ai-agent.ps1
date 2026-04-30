param(
    [string]$HostAddress = "127.0.0.1",
    [int]$Port = 8010,
    [ValidateSet("local_fallback", "ollama", "openai_compatible", "deepseek")]
    [string]$Provider = "local_fallback",
    [string]$Model = "local-fallback",
    [string]$OllamaBaseUrl = "http://127.0.0.1:11434"
)

$ErrorActionPreference = "Stop"

$repoRoot = Resolve-Path (Join-Path $PSScriptRoot "..\..\..")
$runtimeDir = Join-Path $repoRoot "data\runtime"
New-Item -ItemType Directory -Force -Path $runtimeDir | Out-Null

$env:PYTHONPATH = Join-Path $repoRoot "services\ai-agent\src"
$env:ROSIE_AI_HOST = $HostAddress
$env:ROSIE_AI_PORT = "$Port"
$env:ROSIE_LLM_PROVIDER = $Provider
$env:ROSIE_LLM_MODEL = $Model
$env:ROSIE_OLLAMA_BASE_URL = $OllamaBaseUrl
$env:ROSIE_LLM_TIMEOUT_SECONDS = "30"

$out = Join-Path $runtimeDir "ai-agent.out.log"
$err = Join-Path $runtimeDir "ai-agent.err.log"
$python = Join-Path $repoRoot ".venv\Scripts\python.exe"

$process = Start-Process `
    -FilePath $python `
    -ArgumentList @("-m", "uvicorn", "rosie_ai_agent.app:app", "--host", $HostAddress, "--port", "$Port") `
    -WorkingDirectory $repoRoot `
    -RedirectStandardOutput $out `
    -RedirectStandardError $err `
    -PassThru `
    -WindowStyle Hidden

Set-Content -Path (Join-Path $runtimeDir "ai-agent.pid") -Value $process.Id

[PSCustomObject]@{
    pid = $process.Id
    url = "http://${HostAddress}:$Port"
    provider = $Provider
    model = $Model
    stdout = $out
    stderr = $err
}
