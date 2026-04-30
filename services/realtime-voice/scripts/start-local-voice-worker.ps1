param(
    [string]$HostAddress = "127.0.0.1",
    [int]$Port = 8020,
    [string]$ModelDir = "$env:USERPROFILE\.cache\modelscope\hub\models\iic\SenseVoiceSmall",
    [ValidateSet("edge", "sapi", "sherpa_onnx")]
    [string]$TtsProvider = "edge",
    [string]$Voice = "zh-CN-XiaoxiaoNeural",
    [string]$SapiVoice = "Microsoft Huihui Desktop",
    [int]$SapiRate = 1,
    [string]$SherpaModelDir = "",
    [int]$SherpaSpeakerId = 0,
    [double]$SherpaSpeed = 1.0,
    [string]$AiAgentUrl = ""
)

$ErrorActionPreference = "Stop"

$repoRoot = Resolve-Path (Join-Path $PSScriptRoot "..\..\..")
$runtimeDir = Join-Path $repoRoot "data\runtime"
New-Item -ItemType Directory -Force -Path $runtimeDir | Out-Null

$env:PYTHONPATH = Join-Path $repoRoot "services\realtime-voice\src"
$env:ROSIE_REALTIME_PIPELINE_PROVIDER = "native"
$env:ROSIE_REALTIME_AGENT_ENABLED = "true"
if ($AiAgentUrl) {
    $env:ROSIE_AI_AGENT_URL = $AiAgentUrl
} else {
    Remove-Item Env:ROSIE_AI_AGENT_URL -ErrorAction SilentlyContinue
}
$env:ROSIE_STT_PROVIDER = "funasr"
$env:ROSIE_STT_MODEL = $ModelDir
$env:ROSIE_STT_LANGUAGE = "zh"
$env:ROSIE_STT_TIMEOUT_SECONDS = "30"
$env:ROSIE_STT_MIN_AUDIO_BYTES = "16000"
$env:ROSIE_TTS_PROVIDER = $TtsProvider
$env:ROSIE_TTS_VOICE = $Voice
$env:ROSIE_TTS_SAPI_VOICE = $SapiVoice
$env:ROSIE_TTS_SAPI_RATE = "$SapiRate"
$env:ROSIE_TTS_SHERPA_MODEL_TYPE = "vits"
if ($SherpaModelDir) {
    $env:ROSIE_TTS_SHERPA_MODEL = Join-Path $SherpaModelDir "model.onnx"
    $env:ROSIE_TTS_SHERPA_TOKENS = Join-Path $SherpaModelDir "tokens.txt"
    $env:ROSIE_TTS_SHERPA_LEXICON = Join-Path $SherpaModelDir "lexicon.txt"
    Remove-Item Env:ROSIE_TTS_SHERPA_DATA_DIR -ErrorAction SilentlyContinue
}
$env:ROSIE_TTS_SHERPA_SID = "$SherpaSpeakerId"
$env:ROSIE_TTS_SHERPA_SPEED = "$SherpaSpeed"
$env:ROSIE_TTS_FFMPEG_PATH = "ffmpeg"
$env:ROSIE_TTS_TIMEOUT_SECONDS = "30"

$out = Join-Path $runtimeDir "realtime-voice-worker.out.log"
$err = Join-Path $runtimeDir "realtime-voice-worker.err.log"
$python = Join-Path $repoRoot ".venv\Scripts\python.exe"

$process = Start-Process `
    -FilePath $python `
    -ArgumentList @("-m", "uvicorn", "rosie_realtime_voice.app:app", "--host", $HostAddress, "--port", "$Port") `
    -WorkingDirectory $repoRoot `
    -RedirectStandardOutput $out `
    -RedirectStandardError $err `
    -PassThru `
    -WindowStyle Hidden

Set-Content -Path (Join-Path $runtimeDir "realtime-voice-worker.pid") -Value $process.Id

[PSCustomObject]@{
    pid = $process.Id
    url = "http://${HostAddress}:$Port"
    tts_provider = $TtsProvider
    ai_agent_url = $AiAgentUrl
    stdout = $out
    stderr = $err
}
