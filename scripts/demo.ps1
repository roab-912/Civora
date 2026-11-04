$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
Push-Location -LiteralPath $projectRoot
try {
    $localGo = Join-Path $projectRoot '.tools/go/bin/go.exe'
    if (Test-Path -LiteralPath $localGo) { $goExe = $localGo } else { $goExe = (Get-Command go -ErrorAction Stop).Source }
    $env:GOPATH = Join-Path $projectRoot '.tools/gopath'
    $env:GOMODCACHE = Join-Path $projectRoot '.tools/gomodcache'
    $env:GOCACHE = Join-Path $projectRoot '.tools/gocache'
    & $goExe run -tags civora_sim ./cmd/civsim demo
    if ($LASTEXITCODE -ne 0) { throw 'Governance demo failed' }
    & $goExe run ./cmd/civeligibility verify-membership -input spec/fixtures/v0.3/membership-16.json
    if ($LASTEXITCODE -ne 0) { throw 'Semaphore depth-16 verification failed' }
    & $goExe run ./cmd/civeligibility verify-membership -input spec/fixtures/v0.3/membership-26.json
    if ($LASTEXITCODE -ne 0) { throw 'Semaphore depth-26 verification failed' }
    $directory=Join-Path $projectRoot ('.tools/encrypted-demo/'+[guid]::NewGuid().ToString('N'))
    $ledger=Join-Path $directory 'ledger'
    $archive=Join-Path $directory 'public.json'
    & $goExe run ./cmd/civnode replay -fixture spec/fixtures/v0.4/replay-mixed.json -store $ledger
    if ($LASTEXITCODE -ne 0) { throw 'Real ElectionGuard demo failed' }
    & $goExe run ./cmd/civcrypto export -store $ledger -out $archive
    if ($LASTEXITCODE -ne 0) { throw 'Public export failed' }
    & $goExe run ./cmd/civcrypto audit -in $archive
    if ($LASTEXITCODE -ne 0) { throw 'Independent ElectionGuard replay failed' }
} finally { Pop-Location }
