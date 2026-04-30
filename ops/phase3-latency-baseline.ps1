param(
    [ValidateSet("edge", "sapi", "sherpa_onnx")]
    [string]$TtsProvider = "sherpa_onnx",
    [ValidateSet("local_fallback", "ollama", "openai_compatible", "deepseek")]
    [string]$AiProvider = "local_fallback",
    [string]$AiModel = "local-fallback",
    [string]$OllamaBaseUrl = "http://127.0.0.1:11434",
    [string]$SenseVoiceModelDir = "$env:USERPROFILE\.cache\modelscope\hub\models\iic\SenseVoiceSmall",
    [string]$SherpaModelDir = "data\models\tts\vits-icefall-zh-aishell3",
    [int]$Turns = 3,
    [int]$MaxTotalMs = 1500,
    [int]$AiPort = 8010,
    [int]$VoicePort = 8020,
    [int]$ReadyTimeoutSeconds = 240,
    [switch]$SkipStart,
    [switch]$KeepRunning
)

$ErrorActionPreference = "Stop"

$repoRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
$runtimeDir = Join-Path $repoRoot "data\runtime"
New-Item -ItemType Directory -Force -Path $runtimeDir | Out-Null

$aiUrl = "http://127.0.0.1:$AiPort"
$voiceUrl = "http://127.0.0.1:$VoicePort"
$wsUrl = "ws://127.0.0.1:$VoicePort/ws/jambonz/audio"
$reportPath = Join-Path $runtimeDir ("phase3-latency-{0}.json" -f (Get-Date -Format "yyyyMMdd-HHmmss"))

function Assert-PathExists {
    param(
        [string]$Path,
        [string]$Message
    )
    if (-not (Test-Path $Path)) {
        throw "$Message Path: $Path"
    }
}

function Wait-HttpReady {
    param(
        [string]$Url,
        [int]$TimeoutSeconds,
        [switch]$RequireReady
    )
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    $lastError = ""
    while ((Get-Date) -lt $deadline) {
        try {
            $response = Invoke-RestMethod -Uri $Url -Method Get -TimeoutSec 5
            if (-not $RequireReady) {
                return $response
            }
            if ([string]$response.ready -eq "true" -or $response.ready -eq $true) {
                return $response
            }
            $lastError = "service responded but ready=$($response.ready), prewarm_error=$($response.prewarm_error)"
        } catch {
            $lastError = $_.Exception.Message
        }
        Start-Sleep -Seconds 2
    }
    throw "Timed out waiting for $Url. Last error: $lastError"
}

$python = Join-Path $repoRoot ".venv\Scripts\python.exe"
Assert-PathExists $python "Missing workspace Python runtime. Create .venv and install service requirements first."
Assert-PathExists (Join-Path $SenseVoiceModelDir "example\zh.mp3") "Missing SenseVoice sample audio. Install/download SenseVoiceSmall before running the baseline."

$resolvedSherpaModelDir = ""
if ($TtsProvider -eq "sherpa_onnx") {
    $resolvedSherpaModelDir = Resolve-Path (Join-Path $repoRoot $SherpaModelDir) -ErrorAction SilentlyContinue
    if (-not $resolvedSherpaModelDir) {
        throw "Missing sherpa-onnx TTS model directory. Run .\services\realtime-voice\scripts\download-sherpa-onnx-zh-tts.ps1 first, or pass -SherpaModelDir."
    }
    Assert-PathExists (Join-Path $resolvedSherpaModelDir "model.onnx") "Missing sherpa-onnx model file."
    Assert-PathExists (Join-Path $resolvedSherpaModelDir "tokens.txt") "Missing sherpa-onnx tokens file."
}

$started = -not $SkipStart
try {
    if (-not $SkipStart) {
        & (Join-Path $repoRoot "services\ai-agent\scripts\stop-local-ai-agent.ps1")
        & (Join-Path $repoRoot "services\realtime-voice\scripts\stop-local-voice-worker.ps1")

        $ai = & (Join-Path $repoRoot "services\ai-agent\scripts\start-local-ai-agent.ps1") `
            -Port $AiPort `
            -Provider $AiProvider `
            -Model $AiModel `
            -OllamaBaseUrl $OllamaBaseUrl

        $voiceArgs = @{
            Port = $VoicePort
            ModelDir = $SenseVoiceModelDir
            TtsProvider = $TtsProvider
            AiAgentUrl = $aiUrl
        }
        if ($TtsProvider -eq "sherpa_onnx") {
            $voiceArgs["SherpaModelDir"] = [string]$resolvedSherpaModelDir
        }
        $voice = & (Join-Path $repoRoot "services\realtime-voice\scripts\start-local-voice-worker.ps1") @voiceArgs
    }

    $aiHealth = Wait-HttpReady -Url "$aiUrl/health" -TimeoutSeconds 60
    $voiceHealth = Wait-HttpReady -Url "$voiceUrl/health" -TimeoutSeconds $ReadyTimeoutSeconds -RequireReady

    & (Join-Path $repoRoot "services\realtime-voice\scripts\smoke-local-voice.ps1") `
        -WebSocketUrl $wsUrl `
        -ApiBaseUrl $voiceUrl `
        -ModelDir $SenseVoiceModelDir `
        -CallSid "phase3-baseline" `
        -Turns $Turns `
        -MaxTotalMs $MaxTotalMs `
        -ReportPath $reportPath

    $latency = Invoke-RestMethod -Uri "$voiceUrl/latency-report?max_total_ms=$MaxTotalMs&limit=200" -Method Get -TimeoutSec 10
    $summary = [PSCustomObject]@{
        status = $latency.status
        report_path = $reportPath
        target_total_ms = $latency.target_total_ms
        turn_count = $latency.turn_count
        slow_turn_count = $latency.slow_turn_count
        total_ms = $latency.total_ms
        stt_ms = $latency.stt_ms
        agent_ms = $latency.agent_ms
        tts_ms = $latency.tts_ms
        ai_health = $aiHealth
        voice_health = $voiceHealth
    }
    $summary
    if ($latency.status -eq "degraded") {
        throw "Phase 3 latency baseline degraded. See report: $reportPath"
    }
} finally {
    if ($started -and -not $KeepRunning) {
        & (Join-Path $repoRoot "services\realtime-voice\scripts\stop-local-voice-worker.ps1")
        & (Join-Path $repoRoot "services\ai-agent\scripts\stop-local-ai-agent.ps1")
    }
}
