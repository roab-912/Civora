param([string]$Directory='.dev-network',[int]$Port=27650,[string]$Fixture='spec/fixtures/accounts.json')
$ErrorActionPreference='Stop'
$projectRoot=Split-Path -Parent $PSScriptRoot
Push-Location -LiteralPath $projectRoot
$children=@()
try {
    $localGo=Join-Path $projectRoot '.tools/go/bin/go.exe'
    if (Test-Path -LiteralPath $localGo) { $goExe=$localGo } else { $goExe=(Get-Command go -ErrorAction Stop).Source }
    $env:GOPATH=Join-Path $projectRoot '.tools/gopath'
    $env:GOMODCACHE=Join-Path $projectRoot '.tools/gomodcache'
    $env:GOCACHE=Join-Path $projectRoot '.tools/gocache'
    $binary=Join-Path $projectRoot '.tools/civnet.exe'
    New-Item -ItemType Directory -Force -Path (Split-Path -Parent $binary) | Out-Null
    & $goExe build -buildvcs=false -o $binary ./cmd/civnet
    if ($LASTEXITCODE -ne 0) { throw 'Compilation civnet failed' }
    if ([System.IO.Path]::IsPathRooted($Directory)) { $networkRoot=[System.IO.Path]::GetFullPath($Directory) } else { $networkRoot=[System.IO.Path]::GetFullPath((Join-Path $projectRoot $Directory)) }
    if (-not (Test-Path -LiteralPath $networkRoot)) {
        & $binary init -dir $networkRoot -port $Port -fixture $Fixture
        if ($LASTEXITCODE -ne 0) { throw 'Development network initialization failed' }
    }
    $logRoot=Join-Path $networkRoot 'logs'
    New-Item -ItemType Directory -Force -Path $logRoot | Out-Null
    $stamp=[guid]::NewGuid().ToString('N')
    foreach ($seat in 0..6) {
        $nodeHome=Join-Path $networkRoot ('node'+$seat)
        $children+=Start-Process -FilePath $binary -ArgumentList @('serve','-home',('"'+$nodeHome+'"'),'-verbose') -WindowStyle Hidden -PassThru -RedirectStandardOutput (Join-Path $logRoot ($stamp+'-'+$seat+'.out.log')) -RedirectStandardError (Join-Path $logRoot ($stamp+'-'+$seat+'.err.log'))
    }
    Write-Host 'Seven loopback TEST_ONLY nodes. Public private keys; development only.'
    $savedNode=Get-Content -LiteralPath (Join-Path $networkRoot 'node0/development.json') -Raw | ConvertFrom-Json
    Write-Host ('RPC node0: http://127.0.0.1:'+([int]$savedNode.port+1)+'/status')
    Write-Host ('Logs: '+$logRoot+' ; Ctrl+C stops these processes and preserves signer state.')
    while ($true) {
        foreach ($child in $children) { if ($child.HasExited) { throw ('Node exited; inspect logs. PID '+$child.Id) } }
        Start-Sleep -Seconds 1
    }
} finally {
    foreach ($child in $children) { if (-not $child.HasExited) { Stop-Process -Id $child.Id -Force -ErrorAction SilentlyContinue } }
    Pop-Location
}
