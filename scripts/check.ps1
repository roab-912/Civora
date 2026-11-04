$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
Push-Location -LiteralPath $projectRoot
try {
    $localGo = Join-Path $projectRoot '.tools/go/bin/go.exe'
    if (Test-Path -LiteralPath $localGo) { $goExe = $localGo } else { $goExe = (Get-Command go -ErrorAction Stop).Source }
    $env:GOPATH = Join-Path $projectRoot '.tools/gopath'
    $env:GOMODCACHE = Join-Path $projectRoot '.tools/gomodcache'
    $env:GOCACHE = Join-Path $projectRoot '.tools/gocache'
    & npm.cmd run schemas:check
    if ($LASTEXITCODE -ne 0) { throw 'Generated schemas differ' }
    & npm.cmd run format:check
    if ($LASTEXITCODE -ne 0) { throw 'TypeScript formatting differs' }
    & npm.cmd test
    if ($LASTEXITCODE -ne 0) { throw 'TypeScript tests failed' }
    & $goExe test ./... -count=1 -timeout 120s
    if ($LASTEXITCODE -ne 0) { throw 'Go tests failed' }
    & $goExe vet ./...
    if ($LASTEXITCODE -ne 0) { throw 'Go vet failed' }
    $runDirectory = Join-Path $projectRoot ('.tools/acceptance/' + [guid]::NewGuid().ToString('N'))
    $accountsStore = Join-Path $runDirectory 'accounts'
    $activationStore = Join-Path $runDirectory 'activation'
    & $goExe run ./cmd/civnode replay -fixture spec/fixtures/accounts.json -store $accountsStore
    if ($LASTEXITCODE -ne 0) { throw 'Account replay failed' }
    & $goExe run ./cmd/civnode replay -fixture spec/fixtures/accounts.json -store $accountsStore
    if ($LASTEXITCODE -ne 0) { throw 'Account resume failed' }
    & $goExe run ./cmd/civnode replay -fixture spec/fixtures/activation.json -store $activationStore -supported-epoch 2
    if ($LASTEXITCODE -ne 0) { throw 'Activation replay failed' }
} finally { Pop-Location }
