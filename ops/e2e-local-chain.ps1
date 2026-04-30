param(
    [string]$HostAddress = "127.0.0.1",
    [int]$ApiPort = 8030,
    [int]$AiPort = 8010,
    [int]$VoicePort = 8020,
    [string]$DatabaseUrl = $env:ROSIE_DATABASE_URL,
    [string]$CallSid = ("e2e-" + (Get-Date -Format "yyyyMMddHHmmss")),
    [switch]$SkipMigrations,
    [switch]$KeepRunning
)

$ErrorActionPreference = "Stop"

$repoRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
$runtimeDir = Join-Path $repoRoot "data\runtime"
New-Item -ItemType Directory -Force -Path $runtimeDir | Out-Null

$python = Join-Path $repoRoot ".venv\Scripts\python.exe"
if (-not (Test-Path $python)) {
    throw "Missing Python virtualenv at $python. Create it and install service requirements first."
}

$apiUrl = "http://${HostAddress}:$ApiPort"
$aiUrl = "http://${HostAddress}:$AiPort"
$voiceUrl = "http://${HostAddress}:$VoicePort"
$wsUrl = "ws://${HostAddress}:$VoicePort/ws/jambonz/audio"

function Stop-ExistingProcess {
    param(
        [string]$PidFile,
        [string]$CommandLinePattern
    )
    if (Test-Path $PidFile) {
        $pidValue = Get-Content $PidFile -ErrorAction SilentlyContinue
        if ($pidValue) {
            Stop-Process -Id $pidValue -Force -ErrorAction SilentlyContinue
        }
        Remove-Item $PidFile -Force -ErrorAction SilentlyContinue
    }
    Get-CimInstance Win32_Process |
        Where-Object { $_.CommandLine -and $_.CommandLine -match $CommandLinePattern } |
        ForEach-Object { Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue }
}

function Wait-HttpOk {
    param(
        [string]$Url,
        [int]$TimeoutSeconds = 30
    )
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    $lastError = $null
    while ((Get-Date) -lt $deadline) {
        try {
            $response = Invoke-RestMethod -Uri $Url -Method Get -TimeoutSec 3
            if ($response.status -eq "ok") {
                return $response
            }
        } catch {
            $lastError = $_.Exception.Message
        }
        Start-Sleep -Milliseconds 500
    }
    throw "Timed out waiting for $Url. Last error: $lastError"
}

function Start-AiAgent {
    $env:PYTHONPATH = Join-Path $repoRoot "services\ai-agent\src"
    $env:ROSIE_AI_HOST = $HostAddress
    $env:ROSIE_AI_PORT = "$AiPort"
    $env:ROSIE_LLM_PROVIDER = "local_fallback"
    $env:ROSIE_LLM_MODEL = "local-fallback"
    $env:ROSIE_LLM_TIMEOUT_SECONDS = "30"

    $out = Join-Path $runtimeDir "e2e-ai-agent.out.log"
    $err = Join-Path $runtimeDir "e2e-ai-agent.err.log"
    $process = Start-Process `
        -FilePath $python `
        -ArgumentList @("-m", "uvicorn", "rosie_ai_agent.app:app", "--host", $HostAddress, "--port", "$AiPort") `
        -WorkingDirectory $repoRoot `
        -RedirectStandardOutput $out `
        -RedirectStandardError $err `
        -PassThru `
        -WindowStyle Hidden
    Set-Content -Path (Join-Path $runtimeDir "e2e-ai-agent.pid") -Value $process.Id
    return [PSCustomObject]@{ name = "ai-agent"; pid = $process.Id; url = $aiUrl; stdout = $out; stderr = $err }
}

function Start-GoApi {
    $env:ROSIE_API_ADDR = "${HostAddress}:$ApiPort"
    $env:ROSIE_DATABASE_URL = $DatabaseUrl
    $env:ROSIE_DEFAULT_ACCESS_NUMBER = "8613736849910"
    $env:ROSIE_DEFAULT_MERCHANT_ID = "demo-merchant"
    $env:ROSIE_DEFAULT_MERCHANT_NAME = "测试理发店"
    $env:ROSIE_AI_AGENT_URL = $aiUrl
    $env:ROSIE_AI_SUMMARY_ENABLED = "true"
    $env:ROSIE_AI_SUMMARY_TIMEOUT_SECONDS = "5"

    $out = Join-Path $runtimeDir "e2e-api-go.out.log"
    $err = Join-Path $runtimeDir "e2e-api-go.err.log"
    $apiDir = Join-Path $repoRoot "services\api-go"
    $process = Start-Process `
        -FilePath "go" `
        -ArgumentList @("run", ".\cmd\rosie-api") `
        -WorkingDirectory $apiDir `
        -RedirectStandardOutput $out `
        -RedirectStandardError $err `
        -PassThru `
        -WindowStyle Hidden
    Set-Content -Path (Join-Path $runtimeDir "e2e-api-go.pid") -Value $process.Id
    return [PSCustomObject]@{ name = "api-go"; pid = $process.Id; url = $apiUrl; stdout = $out; stderr = $err }
}

function Invoke-GoMigrations {
    if (-not $DatabaseUrl -or $SkipMigrations) {
        return $null
    }
    $apiDir = Join-Path $repoRoot "services\api-go"
    $out = Join-Path $runtimeDir "e2e-migrate.out.log"
    $err = Join-Path $runtimeDir "e2e-migrate.err.log"
    $process = Start-Process `
        -FilePath "go" `
        -ArgumentList @("run", ".\cmd\rosie-migrate", "-database-url", $DatabaseUrl, "-migrations-dir", "migrations") `
        -WorkingDirectory $apiDir `
        -RedirectStandardOutput $out `
        -RedirectStandardError $err `
        -PassThru `
        -Wait `
        -WindowStyle Hidden
    if ($process.ExitCode -ne 0) {
        $message = ""
        if (Test-Path $err) {
            $message = Get-Content -Raw $err
        }
        throw "Database migrations failed with exit code $($process.ExitCode). $message"
    }
    return [PSCustomObject]@{ name = "migrations"; stdout = $out; stderr = $err }
}

function Start-RealtimeVoice {
    $env:PYTHONPATH = Join-Path $repoRoot "services\realtime-voice\src"
    $env:ROSIE_REALTIME_HOST = $HostAddress
    $env:ROSIE_REALTIME_PORT = "$VoicePort"
    $env:ROSIE_REALTIME_PREWARM_ENABLED = "false"
    $env:ROSIE_REALTIME_PIPELINE_PROVIDER = "native"
    $env:ROSIE_REALTIME_AGENT_ENABLED = "true"
    $env:ROSIE_AI_AGENT_URL = $aiUrl
    $env:ROSIE_STT_PROVIDER = "none"
    $env:ROSIE_TTS_PROVIDER = "none"
    $env:ROSIE_BUSINESS_API_URL = $apiUrl
    $env:ROSIE_REALTIME_RESULT_ENABLED = "true"
    $env:ROSIE_BUSINESS_AUTO_DISPATCH_ENABLED = "false"

    $out = Join-Path $runtimeDir "e2e-realtime-voice.out.log"
    $err = Join-Path $runtimeDir "e2e-realtime-voice.err.log"
    $process = Start-Process `
        -FilePath $python `
        -ArgumentList @("-m", "uvicorn", "rosie_realtime_voice.app:app", "--host", $HostAddress, "--port", "$VoicePort") `
        -WorkingDirectory $repoRoot `
        -RedirectStandardOutput $out `
        -RedirectStandardError $err `
        -PassThru `
        -WindowStyle Hidden
    Set-Content -Path (Join-Path $runtimeDir "e2e-realtime-voice.pid") -Value $process.Id
    return [PSCustomObject]@{ name = "realtime-voice"; pid = $process.Id; url = $voiceUrl; stdout = $out; stderr = $err }
}

function Invoke-SimulatedCall {
    $env:ROSIE_E2E_WS_URL = $wsUrl
    $env:ROSIE_E2E_CALL_SID = $CallSid
    $code = @'
import asyncio
import json
import os

import websockets


async def main():
    async with websockets.connect(os.environ["ROSIE_E2E_WS_URL"], max_size=16 * 1024 * 1024) as ws:
        call_sid = os.environ["ROSIE_E2E_CALL_SID"]
        await ws.send(json.dumps({
            "callSid": call_sid,
            "sampleRate": 16000,
            "merchant_id": "demo-merchant",
            "merchant_name": "测试理发店",
            "system_prompt": "当前商家：测试理发店\n服务项目：剪发、烫染、护理\n预约需留下称呼、电话和到店时间。",
            "from": "+8613811112222",
            "to": "8613736849910",
        }, ensure_ascii=False))
        await ws.send(json.dumps({
            "transcript": "你好，我姓王，电话是13811112222，想预约明天下午三点剪头发。"
        }, ensure_ascii=False))
        reply = json.loads(await asyncio.wait_for(ws.recv(), timeout=20))
        print(json.dumps({"call_sid": call_sid, "reply": reply}, ensure_ascii=False))


asyncio.run(main())
'@
    $scriptPath = Join-Path $runtimeDir "e2e-simulated-call.py"
    Set-Content -Path $scriptPath -Value $code -Encoding utf8
    $output = & $python $scriptPath
    return $output | ConvertFrom-Json
}

function Assert-Condition {
    param(
        [bool]$Condition,
        [string]$Message
    )
    if (-not $Condition) {
        throw $Message
    }
}

$started = @()
try {
    Stop-ExistingProcess -PidFile (Join-Path $runtimeDir "e2e-ai-agent.pid") -CommandLinePattern "rosie_ai_agent.app:app.*--port $AiPort"
    Stop-ExistingProcess -PidFile (Join-Path $runtimeDir "e2e-api-go.pid") -CommandLinePattern "rosie-api|cmd\\rosie-api"
    Stop-ExistingProcess -PidFile (Join-Path $runtimeDir "e2e-realtime-voice.pid") -CommandLinePattern "rosie_realtime_voice.app:app.*--port $VoicePort"

    $started += Start-AiAgent
    Wait-HttpOk -Url "$aiUrl/health" | Out-Null

    $migrationResult = Invoke-GoMigrations
    $started += Start-GoApi
    Wait-HttpOk -Url "$apiUrl/health" | Out-Null

    $started += Start-RealtimeVoice
    Wait-HttpOk -Url "$voiceUrl/health" | Out-Null

    $call = Invoke-SimulatedCall
    Start-Sleep -Seconds 1

    $deps = Invoke-RestMethod -Uri "$apiUrl/health/deps" -Method Get -TimeoutSec 10
    $detail = Invoke-RestMethod -Uri "$apiUrl/calls/$CallSid" -Method Get -TimeoutSec 10
    $logs = Invoke-RestMethod -Uri "$apiUrl/notification-logs?merchant_id=demo-merchant&limit=20" -Method Get -TimeoutSec 10
    $retries = Invoke-RestMethod -Uri "$voiceUrl/business-result-retries" -Method Get -TimeoutSec 10

    Assert-Condition -Condition ($deps.status -eq "ok") -Message "Expected healthy dependencies, got $($deps.status)."
    Assert-Condition -Condition ($deps.dependencies.database.status -eq "ok") -Message "Expected database dependency to be ok."
    Assert-Condition -Condition ($deps.dependencies.ai_summary.status -eq "ok") -Message "Expected ai_summary dependency to be ok."
    Assert-Condition -Condition ($detail.call.call_sid -eq $CallSid) -Message "Call detail missing expected call_sid."
    Assert-Condition -Condition ($detail.transcript.source -eq "realtime_voice") -Message "Transcript was not written by realtime_voice."
    Assert-Condition -Condition ($detail.summary.intent -eq "appointment") -Message "Expected appointment summary, got $($detail.summary.intent)."
    Assert-Condition -Condition ($detail.summary.customer_phone -eq "13811112222") -Message "Expected structured customer phone from summary."
    Assert-Condition -Condition ($detail.inbox.status -eq "needs_review") -Message "Expected inbox item to need review."

    $matchingLogs = @($logs.items | Where-Object { $_.idempotency_key -eq "realtime_call:demo-merchant:$CallSid" })
    Assert-Condition -Condition ($matchingLogs.Count -eq 1) -Message "Expected one realtime notification log."
    Assert-Condition -Condition ($matchingLogs[0].status -eq "queued") -Message "Expected queued notification log, got $($matchingLogs[0].status)."
    Assert-Condition -Condition (@($retries.items).Count -eq 0) -Message "Expected empty business result retry queue."

    [PSCustomObject]@{
        status = "ok"
        mode = if ($DatabaseUrl) { "postgres" } else { "memory" }
        migrations = $migrationResult
        health_deps = $deps
        services = $started
        call = [PSCustomObject]@{
            call_sid = $CallSid
            reply_source = $call.reply.source
            reply = $call.reply.reply
            summary = $detail.summary.summary
            intent = $detail.summary.intent
            customer_phone = $detail.summary.customer_phone
            inbox_status = $detail.inbox.status
        }
        notification_log = $matchingLogs[0]
        retry_queue_count = @($retries.items).Count
    } | ConvertTo-Json -Depth 8
} finally {
    if (-not $KeepRunning) {
        Stop-ExistingProcess -PidFile (Join-Path $runtimeDir "e2e-realtime-voice.pid") -CommandLinePattern "rosie_realtime_voice.app:app.*--port $VoicePort"
        Stop-ExistingProcess -PidFile (Join-Path $runtimeDir "e2e-api-go.pid") -CommandLinePattern "rosie-api|cmd\\rosie-api"
        Stop-ExistingProcess -PidFile (Join-Path $runtimeDir "e2e-ai-agent.pid") -CommandLinePattern "rosie_ai_agent.app:app.*--port $AiPort"
    }
}
