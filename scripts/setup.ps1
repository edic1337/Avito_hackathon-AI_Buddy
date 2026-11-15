$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
$target = Join-Path $root '.env.team'
$proxySetting = '^\s*(?:export\s+)?BUDDY_PROXY_TOKEN\s*='

if (Test-Path -LiteralPath $target) {
    $lines = @(Get-Content -LiteralPath $target -Encoding UTF8)
    $proxyLines = @($lines | Where-Object { $_ -match $proxySetting })
    if ($proxyLines.Count -eq 1 -and $proxyLines[0] -cmatch '^BUDDY_PROXY_TOKEN=[0-9a-f]{64}$') {
        Write-Host '.env.team is ready.'
        return
    }
} else {
    $values = @{}
    $old = Join-Path $root '.env'
    if (Test-Path -LiteralPath $old) {
        foreach ($line in Get-Content -LiteralPath $old -Encoding UTF8) {
            if ($line -match '^\s*(OPENROUTER_API_KEY|OPENROUTER_API_URL|OPENROUTER_MODEL)\s*=(.*)$') {
                $values[$Matches[1]] = $Matches[2]
            }
        }
    }
    $lines = @(Get-Content -LiteralPath (Join-Path $root '.env.example') -Encoding UTF8 | ForEach-Object {
        if ($_ -match '^(OPENROUTER_API_KEY|OPENROUTER_API_URL|OPENROUTER_MODEL)=' -and $values.ContainsKey($Matches[1])) {
            "$($Matches[1])=$($values[$Matches[1]])"
        } else {
            $_
        }
    })
}

$bytes = New-Object byte[] 32
$rng = [System.Security.Cryptography.RandomNumberGenerator]::Create()
try { $rng.GetBytes($bytes) } finally { $rng.Dispose() }
$token = -join ($bytes | ForEach-Object { $_.ToString('x2') })
$lines = @($lines | Where-Object { $_ -notmatch $proxySetting })
$lines += "BUDDY_PROXY_TOKEN=$token"
[System.IO.File]::WriteAllLines($target, $lines, (New-Object System.Text.UTF8Encoding($false)))
Write-Host '.env.team is ready. Check the OpenRouter settings before the first start.'
