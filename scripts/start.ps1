param([switch]$ImportCorpus)
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
Set-Location $root
if (!(Test-Path '.env.team')) { throw 'Run scripts/setup.ps1 and fill .env.team first.' }
function Compose {
    & docker compose --env-file .env.team -p ai-buddy-local -f compose.team.yml @args
    if ($LASTEXITCODE -ne 0) { throw "Docker Compose failed (exit $LASTEXITCODE)." }
}
# Fail before building if required local corpus files are missing.
if ($ImportCorpus) {
    foreach ($name in @('habr-corpus.json', 'onboarding-corpus.json')) {
        if (!(Test-Path "embeddings/$name")) { throw "Missing embeddings/$name" }
    }
}
Compose build api frontend
Compose up -d postgres embedder
Compose run --rm rag migrate
if ($ImportCorpus) {
    Compose run --rm rag import-corpus -timeout 45m -expected-documents 871 -expected-chunks 16413 /data/habr-corpus.json /data/onboarding-corpus.json
}
Write-Host 'Waiting for the embedding model. First download can take several minutes.'
$ready = $false
for ($attempt = 0; $attempt -lt 120; $attempt++) {
    try {
        $response = Invoke-WebRequest -UseBasicParsing -Uri 'http://127.0.0.1:8081/health' -TimeoutSec 5
        if ($response.StatusCode -eq 200) { $ready = $true; break }
    } catch { }
    Start-Sleep -Seconds 5
}
if (!$ready) { throw 'Embedder is not ready. Run docker compose --env-file .env.team -f compose.team.yml logs --tail 50 embedder' }
Compose up -d api frontend
$apiReady = $false
for ($attempt = 0; $attempt -lt 30; $attempt++) {
    try {
        $response = Invoke-WebRequest -UseBasicParsing -Uri 'http://127.0.0.1:8080/health' -TimeoutSec 3
        if ($response.StatusCode -eq 200) { $apiReady = $true; break }
    } catch { }
    Start-Sleep -Seconds 2
}
if (!$apiReady) { throw 'API not ready. Check API logs; a new database needs -ImportCorpus.' }
Write-Host 'Ready: http://localhost:5173'
Write-Host 'Open the website and ask an onboarding question.'
