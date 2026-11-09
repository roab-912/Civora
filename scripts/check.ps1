param([switch]$Browsers,[switch]$Scale,[switch]$SkipNetwork,[switch]$SkipPWA,[switch]$Qualification,[switch]$Distribution,[string]$Report='')
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
Push-Location -LiteralPath $projectRoot
$checksPassed=$false
$checkStarted=(Get-Date).ToUniversalTime().ToString('o')
$sourceBefore=''
try {
    $localGo = Join-Path $projectRoot '.tools/go/bin/go.exe'
    if ($env:CIVORA_GO) { $goExe=$env:CIVORA_GO }
    elseif (Test-Path -LiteralPath $localGo) { $goExe = $localGo }
    else { $goExe = (Get-Command go -ErrorAction Stop).Source }
    $env:GOPATH = Join-Path $projectRoot '.tools/gopath'
    $env:GOMODCACHE = Join-Path $projectRoot '.tools/gomodcache'
    $env:GOCACHE = Join-Path $projectRoot '.tools/gocache'
    if ($Report) {
        $sourceBefore=(& node scripts/finalize-release.mjs fingerprint).Trim()
        if ($LASTEXITCODE -ne 0) { throw 'Could not fingerprint check sources' }
    }
    & node --test scripts/distribution.test.mjs
    if ($LASTEXITCODE -ne 0) { throw 'Source snapshot and release gate tests failed' }
    & npm.cmd run schemas:check
    if ($LASTEXITCODE -ne 0) { throw 'Generated schemas differ' }
    & npm.cmd run format:check
    if ($LASTEXITCODE -ne 0) { throw 'TypeScript formatting differs' }
    & npm.cmd test
    if ($LASTEXITCODE -ne 0) { throw 'TypeScript tests failed' }
    & node --test scripts/network-service.test.mjs
    if ($LASTEXITCODE -ne 0) { throw 'Network participant presence tests failed' }
    & node scripts/generate-pwa-fixtures.mjs --check
    if ($LASTEXITCODE -ne 0) { throw 'Browser encryption fixtures differ' }
    & node scripts/build-pwa.mjs
    if ($LASTEXITCODE -ne 0) { throw 'PWA browser build failed' }
    & node scripts/check-semaphore.mjs
    if ($LASTEXITCODE -ne 0) { throw 'Semaphore lock verification failed' }
    & node scripts/generate-eligibility-fixtures.mjs --check
    if ($LASTEXITCODE -ne 0) { throw 'Eligibility SDK fixtures differ' }
    & node scripts/generate-electionguard-fixtures.mjs --check
    if ($LASTEXITCODE -ne 0) { throw 'ElectionGuard envelope fixtures differ' }
    & node scripts/prepare-electionguard-oracle.mjs --check
    if ($LASTEXITCODE -ne 0) { throw 'ElectionGuard deterministic actors differ' }
    foreach ($script in @('materialize-electionguard','generate-electionguard-negatives','generate-encrypted-fixtures','generate-scale-fixtures')) {
        & node ('scripts/'+$script+'.mjs') --check
        if ($LASTEXITCODE -ne 0) { throw "ElectionGuard fixture differs: $script" }
    }
    & node scripts/review-electionguard.mjs --require-qualified
    if ($LASTEXITCODE -ne 0) { throw 'ElectionGuard development interop gate failed' }
    & $goExe test ./... -count=1 -timeout 300s
    if ($LASTEXITCODE -ne 0) { throw 'Go tests failed' }
    & $goExe vet ./...
    if ($LASTEXITCODE -ne 0) { throw 'Go vet failed' }
    & $goExe -C tools/distribution test ./... -count=1 -timeout 90s
    if ($LASTEXITCODE -ne 0) { throw 'Distribution TUF, replay and abrupt-exit tests failed' }
    & $goExe -C tools/distribution vet ./...
    if ($LASTEXITCODE -ne 0) { throw 'Distribution vet failed' }
    $nodeDependencies = & $goExe list -buildvcs=false -deps ./cmd/civnode
    if ($LASTEXITCODE -ne 0) { throw 'Node dependency inspection failed' }
    if ($nodeDependencies -contains 'civora/internal/simulation' -or $nodeDependencies -contains 'civora/internal/recoverytest') { throw 'Test-only adapters linked into civnode' }
    $participantDependencies = & $goExe list -buildvcs=false -deps ./cmd/civparticipant
    if ($LASTEXITCODE -ne 0 -or $participantDependencies -contains 'civora/internal/citizendev' -or $participantDependencies -contains 'civora/internal/simulation' -or $participantDependencies -contains 'civora/internal/recoverytest') { throw 'Test-only authorities linked into participant binary' }
    & $goExe test -tags civora_sim ./... -count=1 -timeout 300s
    if ($LASTEXITCODE -ne 0) { throw 'Governance simulation tests failed' }
    & $goExe vet -tags civora_sim ./...
    if ($LASTEXITCODE -ne 0) { throw 'Simulation vet failed' }
    & $goExe run -tags civora_sim ./cmd/civsim fixtures --check
    if ($LASTEXITCODE -ne 0) { throw 'Governance fixtures differ' }
    & $goExe run ./cmd/civeligibility fixtures -check
    if ($LASTEXITCODE -ne 0) { throw 'Eligibility account fixture differs' }
    & $goExe run ./cmd/civcrypto fixtures -check
    if ($LASTEXITCODE -ne 0) { throw 'Encrypted replay fixtures differ' }
    & node scripts/generate-partition-ballots.mjs --check
    if ($LASTEXITCODE -ne 0) { throw 'Partition crypto fixtures differ' }
    & $goExe run ./cmd/civnet fixtures -check
    if ($LASTEXITCODE -ne 0) { throw 'Partition transport fixtures differ' }
    & $goExe run -buildvcs=false ./cmd/civnode fixtures -check -certified
    if ($LASTEXITCODE -ne 0) { throw 'Citizen archive fixtures differ' }
    & node scripts/audit-citizen.mjs -input spec/fixtures/v0.6/record -manifest spec/fixtures/v0.6/network.json
    if ($LASTEXITCODE -ne 0) { throw 'Independent TypeScript citizen audit failed' }
    $networkDependencies = & $goExe list -buildvcs=false -deps ./cmd/civnet
    if ($LASTEXITCODE -ne 0 -or $networkDependencies -contains 'civora/internal/simulation' -or $networkDependencies -contains 'civora/internal/recoverytest') { throw 'Network node dependency isolation failed' }
    if (-not $SkipNetwork) {
        & node scripts/test-consensus.mjs
        if ($LASTEXITCODE -ne 0) { throw 'Native CometBFT acceptance failed' }
        & node scripts/test-citizen-native.mjs
        if ($LASTEXITCODE -ne 0) { throw 'Native citizen host acceptance failed' }
    }
    if (-not $SkipPWA) {
        & node scripts/test-network.mjs
        if ($LASTEXITCODE -ne 0) { throw 'Network download, native observer and browser menu acceptance failed' }
        & node scripts/test-pwa-sessions.mjs
        if ($LASTEXITCODE -ne 0) { throw 'PWA workshop session isolation and rollback protection failed' }
        & node scripts/test-pwa.mjs
        if ($LASTEXITCODE -ne 0) { throw 'PWA desktop/mobile acceptance failed' }
        & node scripts/test-notifications-browser.mjs
        if ($LASTEXITCODE -ne 0) { throw 'Notifications browser acceptance failed' }
    }
    $runDirectory = Join-Path $projectRoot ('.tools/acceptance/' + [guid]::NewGuid().ToString('N'))
    $accountsStore = Join-Path $runDirectory 'accounts'
    $activationStore = Join-Path $runDirectory 'activation'
    $governanceStore = Join-Path $runDirectory 'governance'
    $eligibilityLedger = Join-Path $runDirectory 'eligibility-ledger'
    $eligibilityIndex = Join-Path $runDirectory 'eligibility-index'
    & $goExe run ./cmd/civnode replay -fixture spec/fixtures/accounts.json -store $accountsStore
    if ($LASTEXITCODE -ne 0) { throw 'Account replay failed' }
    & $goExe run ./cmd/civnode replay -fixture spec/fixtures/accounts.json -store $accountsStore
    if ($LASTEXITCODE -ne 0) { throw 'Account resume failed' }
    & $goExe run ./cmd/civnode replay -fixture spec/fixtures/activation.json -store $activationStore -supported-epoch 2
    if ($LASTEXITCODE -ne 0) { throw 'Activation replay failed' }
    & $goExe run -tags civora_sim ./cmd/civsim replay -fixture spec/fixtures/v0.2/governance.json -store $governanceStore
    if ($LASTEXITCODE -ne 0) { throw 'Governance replay failed' }
    & $goExe run -tags civora_sim ./cmd/civsim replay -fixture spec/fixtures/v0.2/governance.json -store $governanceStore
    if ($LASTEXITCODE -ne 0) { throw 'Governance resume failed' }
    & $goExe run ./cmd/civnode replay -fixture spec/fixtures/v0.3/accounts.json -store $eligibilityLedger
    if ($LASTEXITCODE -ne 0) { throw 'Eligibility account replay failed' }
    & $goExe run ./cmd/civeligibility index -ledger $eligibilityLedger -store $eligibilityIndex
    if ($LASTEXITCODE -ne 0) { throw 'Eligibility index projection failed' }
    & $goExe run ./cmd/civeligibility index -ledger $eligibilityLedger -store $eligibilityIndex
    if ($LASTEXITCODE -ne 0) { throw 'Eligibility index resume failed' }
    & $goExe run ./cmd/civeligibility path -store $eligibilityIndex -height 2 -index 1
    if ($LASTEXITCODE -ne 0) { throw 'Historical eligibility path failed' }
    foreach ($label in @('mixed','all-no')) {
        $encryptedStore=Join-Path $runDirectory ('encrypted-'+$label)
        $publicArchive=Join-Path $runDirectory ('public-'+$label+'.json')
        & $goExe run ./cmd/civnode replay -fixture ('spec/fixtures/v0.4/replay-'+$label+'.json') -store $encryptedStore
        if ($LASTEXITCODE -ne 0) { throw 'Encrypted replay failed' }
        & $goExe run ./cmd/civnode replay -fixture ('spec/fixtures/v0.4/replay-'+$label+'.json') -store $encryptedStore
        if ($LASTEXITCODE -ne 0) { throw 'Encrypted restart failed' }
        & $goExe run ./cmd/civcrypto export -store $encryptedStore -out $publicArchive
        if ($LASTEXITCODE -ne 0) { throw 'Public export failed' }
        & $goExe run ./cmd/civcrypto audit -in $publicArchive
        if ($LASTEXITCODE -ne 0) { throw 'Independent Go archive replay failed' }
        & node scripts/audit-electionguard.mjs -in $publicArchive
        if ($LASTEXITCODE -ne 0) { throw 'Independent TypeScript crypto audit failed' }
    }
    if ($Qualification) {
        & node scripts/test-qualification.mjs
        if ($LASTEXITCODE -ne 0) { throw 'Local qualification prerequisites failed' }
    }
    if ($Distribution) {
        if ($Report) { & node scripts/test-distribution.mjs --report ($Report+'.distribution.json') }
        else { & node scripts/test-distribution.mjs }
        if ($LASTEXITCODE -ne 0) { throw 'Distribution reproducible packages and standalone auditors failed' }
    }
    if ($Scale) {
        & node scripts/check-eligibility-scale.mjs
        if ($LASTEXITCODE -ne 0) { throw 'Ten-million eligibility verification failed' }
    } elseif ($Browsers) {
        & node scripts/test-eligibility-browsers.mjs
        if ($LASTEXITCODE -ne 0) { throw 'Browser proof tests failed' }
    }
    $checksPassed=$true
} finally {
  try {
    if ($Report -and $sourceBefore) {
        $sourceAfter=(& node scripts/finalize-release.mjs fingerprint).Trim()
        $sourceUnchanged=($LASTEXITCODE -eq 0 -and $sourceAfter -eq $sourceBefore)
        $record=[ordered]@{v='1';scope='local_regression';passed=($checksPassed -and $sourceUnchanged);source_sha256=$sourceBefore;source_unchanged=$sourceUnchanged;started=$checkStarted;completed=(Get-Date).ToUniversalTime().ToString('o');go=(& $goExe version).Trim();node=(& node --version).Trim();flags=@{skip_network=[bool]$SkipNetwork;skip_pwa=[bool]$SkipPWA;distribution=[bool]$Distribution;qualification=[bool]$Qualification;scale=[bool]$Scale};distribution_report=($Report+'.distribution.json')}
        $reportPath=[System.IO.Path]::GetFullPath((Join-Path $projectRoot $Report))
        [System.IO.Directory]::CreateDirectory([System.IO.Path]::GetDirectoryName($reportPath)) | Out-Null
        [System.IO.File]::WriteAllText($reportPath,($record | ConvertTo-Json -Depth 8),(New-Object System.Text.UTF8Encoding($false)))
        if ($checksPassed -and -not $sourceUnchanged) { throw 'Sources changed while checks were running' }
    }
  } finally { Pop-Location }
}
