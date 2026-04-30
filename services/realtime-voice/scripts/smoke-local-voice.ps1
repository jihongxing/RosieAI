param(
    [string]$WebSocketUrl = "ws://127.0.0.1:8020/ws/jambonz/audio",
    [string]$ModelDir = "$env:USERPROFILE\.cache\modelscope\hub\models\iic\SenseVoiceSmall",
    [string]$CallSid = "local-voice-smoke",
    [int]$Turns = 2
)

$ErrorActionPreference = "Stop"

$repoRoot = Resolve-Path (Join-Path $PSScriptRoot "..\..\..")
$runtimeDir = Join-Path $repoRoot "data\runtime"
New-Item -ItemType Directory -Force -Path $runtimeDir | Out-Null

$mp3 = Join-Path $ModelDir "example\zh.mp3"
$pcm = Join-Path $runtimeDir "sensevoice-zh-16k.s16le"
& ffmpeg -hide_banner -loglevel error -y -i $mp3 -f s16le -acodec pcm_s16le -ac 1 -ar 16000 $pcm

$env:ROSIE_SMOKE_WS_URL = $WebSocketUrl
$env:ROSIE_SMOKE_PCM = $pcm
$env:ROSIE_SMOKE_CALL_SID = $CallSid
$env:ROSIE_SMOKE_TURNS = "$Turns"

$python = Join-Path $repoRoot ".venv\Scripts\python.exe"
$code = @'
import asyncio
import json
import os
import time
from pathlib import Path

import websockets


async def run_turn(index: int):
    audio = Path(os.environ["ROSIE_SMOKE_PCM"]).read_bytes()
    async with websockets.connect(os.environ["ROSIE_SMOKE_WS_URL"], max_size=16 * 1024 * 1024) as ws:
        await ws.send(json.dumps({
            "callSid": f"{os.environ['ROSIE_SMOKE_CALL_SID']}-{index}",
            "sampleRate": 16000,
            "merchant_id": "demo-merchant",
            "merchant_name": "test merchant",
            "system_prompt": "You are Rosie's test phone agent. Reply briefly.",
        }))
        started = time.perf_counter()
        await ws.send(audio)
        reply = json.loads(await asyncio.wait_for(ws.recv(), timeout=120))
        audio_out = await asyncio.wait_for(ws.recv(), timeout=120)
        return {
            "turn": index,
            "elapsed_ms": round((time.perf_counter() - started) * 1000),
            "input_pcm_bytes": len(audio),
            "reply_source": reply.get("source"),
            "reply": reply.get("reply"),
            "output_audio_bytes": len(audio_out),
        }


async def main():
    turns = int(os.environ.get("ROSIE_SMOKE_TURNS", "2"))
    results = []
    for index in range(1, turns + 1):
        results.append(await run_turn(index))
    print(json.dumps(results, ensure_ascii=False, indent=2))


asyncio.run(main())
'@

$scriptPath = Join-Path $runtimeDir "smoke-local-voice.py"
Set-Content -Path $scriptPath -Value $code -Encoding ascii
& $python $scriptPath
