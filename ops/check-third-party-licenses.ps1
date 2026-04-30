param(
    [string]$OutputPath = "",
    [switch]$FailOnUnknown
)

$ErrorActionPreference = "Stop"

$repoRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
if (-not $OutputPath) {
    $OutputPath = Join-Path $repoRoot "data\runtime\third-party-license-report.json"
}
$outputDir = Split-Path $OutputPath -Parent
New-Item -ItemType Directory -Force -Path $outputDir | Out-Null

function Read-Requirements {
    param([string]$Path)
    $items = @()
    if (-not (Test-Path $Path)) {
        return $items
    }
    foreach ($line in Get-Content $Path) {
        $text = $line.Trim()
        if (-not $text -or $text.StartsWith("#") -or $text.StartsWith("-")) {
            continue
        }
        $name = ($text -split "[<>=~!;\[]", 2)[0].Trim()
        if ($name) {
            $items += [PSCustomObject]@{
                name = $name
                spec = $text
                source_file = (Resolve-Path $Path).Path.Replace("$repoRoot\", "")
            }
        }
    }
    return $items
}

$requirementFiles = @(
    "services\ai-agent\requirements.txt",
    "services\call-webhook\requirements.txt",
    "services\realtime-voice\requirements.txt",
    "services\realtime-voice\requirements-voice-models.txt"
)

$pythonDeps = @()
foreach ($file in $requirementFiles) {
    $pythonDeps += Read-Requirements (Join-Path $repoRoot $file)
}
$pythonNames = $pythonDeps | ForEach-Object { $_.name } | Sort-Object -Unique

$pythonExe = Join-Path $repoRoot ".venv\Scripts\python.exe"
if (-not (Test-Path $pythonExe)) {
    $pythonExe = "python"
}

$pythonMetadata = @{}
if ($pythonNames.Count -gt 0) {
    $env:ROSIE_LICENSE_PKGS = ($pythonNames | ConvertTo-Json -Compress)
    $code = @'
import importlib.metadata as md
import json
import os

packages = json.loads(os.environ.get("ROSIE_LICENSE_PKGS", "[]"))
results = {}
for name in packages:
    item = {"installed": False, "version": "", "license": "", "classifiers": []}
    try:
        dist = md.distribution(name)
    except md.PackageNotFoundError:
        results[name] = item
        continue
    meta = dist.metadata
    classifiers = [
        value for value in meta.get_all("Classifier", [])
        if value.startswith("License ::")
    ]
    item.update({
        "installed": True,
        "version": dist.version,
        "license": (meta.get("License") or "").strip(),
        "classifiers": classifiers,
    })
    results[name] = item
print(json.dumps(results, ensure_ascii=False))
'@
    $helperPath = Join-Path $outputDir "license-metadata-helper.py"
    Set-Content -Path $helperPath -Value $code -Encoding utf8
    $raw = & $pythonExe $helperPath
    if ($LASTEXITCODE -eq 0 -and $raw) {
        $parsed = $raw | ConvertFrom-Json
        foreach ($property in $parsed.PSObject.Properties) {
            $pythonMetadata[$property.Name] = $property.Value
        }
    }
}

$pythonReport = @()
foreach ($dep in $pythonDeps) {
    $meta = $pythonMetadata[$dep.name]
    $license = ""
    $version = ""
    $installed = $false
    $classifiers = @()
    if ($meta) {
        $license = [string]$meta.license
        $version = [string]$meta.version
        $installed = [bool]$meta.installed
        $classifiers = @($meta.classifiers)
    }
    $pythonReport += [PSCustomObject]@{
        ecosystem = "python"
        name = $dep.name
        spec = $dep.spec
        installed = $installed
        version = $version
        license = $license
        license_classifiers = $classifiers
        source_file = $dep.source_file
        needs_review = (-not $installed) -or (-not $license -and $classifiers.Count -eq 0)
    }
}

$goReport = @()
$goMod = Join-Path $repoRoot "services\api-go\go.mod"
if (Test-Path $goMod) {
    Push-Location (Join-Path $repoRoot "services\api-go")
    try {
        $goList = & go list -m all 2>$null
        if ($LASTEXITCODE -eq 0) {
            foreach ($line in $goList) {
                $parts = $line.Trim() -split "\s+"
                if ($parts.Count -eq 0 -or -not $parts[0]) {
                    continue
                }
                $goReport += [PSCustomObject]@{
                    ecosystem = "go"
                    name = $parts[0]
                    version = if ($parts.Count -gt 1) { $parts[1] } else { "" }
                    license = ""
                    source_file = "services\api-go\go.mod"
                    needs_review = $true
                }
            }
        }
    } finally {
        Pop-Location
    }
}

$criticalComponents = @(
    [PSCustomObject]@{
        name = "jambonz"
        use = "SIP/call control/listen WebSocket"
        license = "core repos commonly MIT; self-hosted deployment requires jambonz license key"
        status = "review_before_trial"
        action = "Save self-hosting license key and confirm deployment terms"
    },
    [PSCustomObject]@{
        name = "Pipecat"
        use = "Realtime voice agent pipeline"
        license = "BSD-2-Clause"
        status = "trial_ok_with_notice"
        action = "Keep LICENSE/NOTICE and scan provider extras"
    },
    [PSCustomObject]@{
        name = "Qwen/Qwen3"
        use = "Local LLM route"
        license = "Apache-2.0 for upstream repo; verify exact model card"
        status = "model_card_required"
        action = "Record exact model ID/version and license"
    },
    [PSCustomObject]@{
        name = "FunASR/SenseVoice"
        use = "Local STT"
        license = "MIT for project; pretrained models may have separate model license"
        status = "model_card_required"
        action = "Record STT model ID/version and model license"
    },
    [PSCustomObject]@{
        name = "CosyVoice"
        use = "Future TTS/voice clone route"
        license = "Apache-2.0 metadata; voice clone data rights still required"
        status = "voice_authorization_required"
        action = "Use only merchant-authorized voice material"
    },
    [PSCustomObject]@{
        name = "sherpa-onnx"
        use = "Current local dynamic TTS option"
        license = "Apache-2.0 metadata"
        status = "trial_ok_with_notice"
        action = "Keep notices and record model license"
    }
)

$unknownCount = @($pythonReport + $goReport | Where-Object { $_.needs_review }).Count
$report = [PSCustomObject]@{
    generated_at = (Get-Date).ToUniversalTime().ToString("o")
    critical_components = $criticalComponents
    dependencies = @{
        python = $pythonReport
        go = $goReport
    }
    summary = @{
        python_dependency_count = $pythonReport.Count
        go_module_count = $goReport.Count
        needs_review_count = $unknownCount
        output_path = (Resolve-Path (Split-Path $OutputPath -Parent)).Path + "\" + (Split-Path $OutputPath -Leaf)
    }
}

$json = $report | ConvertTo-Json -Depth 8
Set-Content -Path $OutputPath -Value $json -Encoding utf8

$report.summary
if ($FailOnUnknown -and $unknownCount -gt 0) {
    throw "License report contains $unknownCount dependencies that need review."
}
