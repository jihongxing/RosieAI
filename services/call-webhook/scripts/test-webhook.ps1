$baseUrl = $env:ROSIE_BASE_URL
if (-not $baseUrl) {
  $baseUrl = "http://127.0.0.1:8000"
}

$payloadPath = Join-Path $PSScriptRoot "..\samples\jambonz-inbound-call.json"
$payload = Get-Content -Raw -Path $payloadPath

Write-Host "Health:"
Invoke-RestMethod -Uri "$baseUrl/health"

Write-Host "Inbound call response:"
Invoke-RestMethod -Uri "$baseUrl/webhooks/jambonz/call" -Method Post -ContentType "application/json" -Body $payload

Write-Host "Calls:"
Invoke-RestMethod -Uri "$baseUrl/calls" | ConvertTo-Json -Depth 5
