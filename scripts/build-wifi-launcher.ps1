$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
$savedEnvironment = @{}
foreach ($name in @('GOPATH', 'GOMODCACHE', 'GOCACHE', 'GOOS', 'GOARCH', 'CGO_ENABLED')) {
    $savedEnvironment[$name] = [Environment]::GetEnvironmentVariable($name, 'Process')
}
Push-Location -LiteralPath $projectRoot
try {
    $go = Join-Path $projectRoot '.tools/go/bin/go.exe'
    if (-not (Test-Path -LiteralPath $go)) { $go = (Get-Command go -ErrorAction Stop).Source }
    $env:GOPATH = Join-Path $projectRoot '.tools/gopath'
    $env:GOMODCACHE = Join-Path $projectRoot '.tools/gomodcache'
    $env:GOCACHE = Join-Path $projectRoot '.tools/gocache'
    $env:GOOS = 'windows'
    $env:GOARCH = 'amd64'
    $env:CGO_ENABLED = '0'
    & $go build -trimpath -buildvcs=false -ldflags='-s -w' -o Civora-WiFi.exe ./cmd/civora-wifi
    if ($LASTEXITCODE -ne 0) { throw 'Compilation du lanceur Wi-Fi impossible' }
    Write-Host ('Lanceur genere : ' + (Join-Path $projectRoot 'Civora-WiFi.exe'))
} finally {
    foreach ($name in $savedEnvironment.Keys) {
        [Environment]::SetEnvironmentVariable($name, $savedEnvironment[$name], 'Process')
    }
    Pop-Location
}
