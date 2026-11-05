param([switch]$Check)
$ErrorActionPreference='Stop'
$projectRoot=Split-Path -Parent $PSScriptRoot
Push-Location -LiteralPath $projectRoot
$children=@()
try {
    $goExe=Join-Path $projectRoot '.tools/go/bin/go.exe'
    if (-not (Test-Path -LiteralPath $goExe)) { $goExe=(Get-Command go -ErrorAction Stop).Source }
    $env:GOPATH=Join-Path $projectRoot '.tools/gopath'
    $env:GOMODCACHE=Join-Path $projectRoot '.tools/gomodcache'
    $env:GOCACHE=Join-Path $projectRoot '.tools/gocache'
    $binary=Join-Path $projectRoot '.tools/civnode.exe'
    New-Item -ItemType Directory -Force -Path (Split-Path -Parent $binary) | Out-Null
    & $goExe build -buildvcs=false -o $binary ./cmd/civnode
    if ($LASTEXITCODE -ne 0) { throw 'Citizen host build failed' }
    $citizenRoot=Join-Path $projectRoot '.dev-host'
    $logRoot=Join-Path $citizenRoot 'logs'
    New-Item -ItemType Directory -Force -Path $logRoot | Out-Null
    $stamp=[guid]::NewGuid().ToString('N')
    foreach ($i in 0..1) {
        $source=Join-Path $citizenRoot ('source-'+$i)
        $children+=Start-Process -FilePath $binary -ArgumentList @('serve','--home',('"'+$source+'"'),'--fixture','spec/fixtures/v0.6/archive.json','--listen',('127.0.0.1:'+(8786+$i))) -WindowStyle Hidden -PassThru -RedirectStandardOutput (Join-Path $logRoot ($stamp+'-source-'+$i+'.out.log')) -RedirectStandardError (Join-Path $logRoot ($stamp+'-source-'+$i+'.err.log'))
    }
    foreach ($port in 8786..8787) {
        $deadline=[DateTime]::UtcNow.AddSeconds(60)
        $ready=$false
        while ([DateTime]::UtcNow -lt $deadline) {
            try { $null=Invoke-RestMethod -Uri ('http://127.0.0.1:'+ $port+'/v1/network') -TimeoutSec 2; $ready=$true; break } catch { Start-Sleep -Milliseconds 250 }
        }
        if (-not $ready) { throw ('Archive source unavailable; inspect '+$logRoot) }
    }
    $observer=Join-Path $citizenRoot 'observer'
    if (-not (Test-Path -LiteralPath $observer)) {
        & $binary init --home $observer --manifest spec/fixtures/v0.6/network.json --genesis spec/fixtures/v0.6/genesis.json
        if ($LASTEXITCODE -ne 0) { throw 'Observer initialization failed' }
    }
    $children+=Start-Process -FilePath $binary -ArgumentList @('start','--home',('"'+$observer+'"'),'--mode','archive','--transport','http','--lanes','0','--listen','127.0.0.1:8788','--workers=false') -WindowStyle Hidden -PassThru -RedirectStandardOutput (Join-Path $logRoot ($stamp+'-observer.out.log')) -RedirectStandardError (Join-Path $logRoot ($stamp+'-observer.err.log'))
    $deadline=[DateTime]::UtcNow.AddSeconds(60)
    $ready=$false
    while ([DateTime]::UtcNow -lt $deadline) {
        try { $status=Invoke-RestMethod -Uri 'http://127.0.0.1:8788/v1/network' -TimeoutSec 2; if ([int]$status.height -ge 15) { $ready=$true; break } } catch {}
        Start-Sleep -Milliseconds 250
    }
    if (-not $ready) { throw ('Observer did not synchronize; inspect '+$logRoot) }
    Write-Host 'Development archive: two independent HTTP processes, real cryptography, synthetic TEST_ONLY commits.'
    Write-Host 'Anonymous observer main + lane 0: http://127.0.0.1:8788/v1/network'
    Write-Host ('Logs: '+$logRoot+' ; Ctrl+C preserves the archives. No account or consensus key is installed in the observer.')
    if ($Check) {
        & $binary verify --home $observer --from genesis
        if ($LASTEXITCODE -ne 0) { throw 'Observer replay failed' }
        $poll=(Get-Content -LiteralPath 'spec/fixtures/v0.6/record/record.json' -Raw|ConvertFrom-Json).frozen.context.poll_id
        $record=Join-Path $citizenRoot 'export'
        & $binary election export --manifest spec/fixtures/v0.6/network.json --poll-id $poll --output $record
        if ($LASTEXITCODE -ne 0) { throw 'Selective export failed' }
        & $binary election verify --manifest spec/fixtures/v0.6/network.json --input $record
        if ($LASTEXITCODE -ne 0) { throw 'Selective replay failed' }
    } else {
        while ($true) { foreach ($child in $children) { if ($child.HasExited) { throw ('Host exited; inspect logs. PID '+$child.Id) } }; Start-Sleep -Seconds 1 }
    }
} finally {
    foreach ($child in $children) { if (-not $child.HasExited) { Stop-Process -Id $child.Id -Force -ErrorAction SilentlyContinue } }
    Pop-Location
}
