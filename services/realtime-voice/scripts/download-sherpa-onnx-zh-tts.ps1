param(
    [string]$ModelRoot = "data\models\tts"
)

$ErrorActionPreference = "Stop"

$repoRoot = Resolve-Path (Join-Path $PSScriptRoot "..\..\..")
$targetRoot = Join-Path $repoRoot $ModelRoot
New-Item -ItemType Directory -Force -Path $targetRoot | Out-Null

$name = "vits-icefall-zh-aishell3"
$archive = Join-Path $targetRoot "$name.tar.bz2"
$modelDir = Join-Path $targetRoot $name

if (!(Test-Path $archive)) {
    Invoke-WebRequest `
        -Uri "https://github.com/k2-fsa/sherpa-onnx/releases/download/tts-models/$name.tar.bz2" `
        -OutFile $archive
}

if (!(Test-Path $modelDir)) {
    tar -xjf $archive -C $targetRoot
}

[PSCustomObject]@{
    model_dir = $modelDir
    model = Join-Path $modelDir "model.onnx"
    tokens = Join-Path $modelDir "tokens.txt"
    lexicon = Join-Path $modelDir "lexicon.txt"
}
