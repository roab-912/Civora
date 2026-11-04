param([switch]$Fetch,[switch]$NewProofs)
$ErrorActionPreference='Stop'
$projectRoot=Split-Path -Parent $PSScriptRoot
Push-Location -LiteralPath $projectRoot
try {
    $lock=Get-Content -Raw -Encoding utf8 spec/crypto/electionguard-v2.lock.json | ConvertFrom-Json
    $cache=Join-Path $projectRoot '.tools/electionguard'
    New-Item -ItemType Directory -Force -Path $cache | Out-Null
    function Get-LockedFile($url,$path,$hash) {
        if (!(Test-Path -LiteralPath $path)) {
            if (!$Fetch) { throw "Missing $path; use -Fetch for the initial toolchain download" }
            $temporary=$path+'.'+[guid]::NewGuid().ToString('N')+'.download'
            Invoke-WebRequest -UseBasicParsing $url -OutFile $temporary
            if ((Get-FileHash -LiteralPath $temporary -Algorithm SHA256).Hash.ToLowerInvariant() -ne $hash) { throw "SHA256 mismatch: $url" }
            Move-Item -LiteralPath $temporary -Destination $path
        }
        if ((Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash.ToLowerInvariant() -ne $hash) { throw "SHA256 mismatch: $path" }
    }
    $zip=Join-Path $cache 'sdk-pinned.zip'
    Get-LockedFile $lock.sdk.archive.url $zip $lock.sdk.archive.sha256
    $sourceParent=Join-Path $cache 'sdk'
    $sdk=Join-Path $sourceParent ('electionguard-kotlin-multiplatform-'+$lock.sdk.commit)
    if (!(Test-Path -LiteralPath $sdk)) { Expand-Archive -LiteralPath $zip -DestinationPath $sourceParent }
    $jdkZip=Join-Path $cache 'jdk17.zip'
    Get-LockedFile $lock.sdk.jdk_windows.url $jdkZip $lock.sdk.jdk_windows.sha256
    $jdk=Join-Path (Join-Path $cache 'jdk17') $lock.sdk.jdk_windows.directory
    if (!(Test-Path -LiteralPath $jdk)) { Expand-Archive -LiteralPath $jdkZip -DestinationPath (Join-Path $cache 'jdk17') }
    # Compare every upstream Kotlin/Gradle source against the exact archive.
    # Only our appended task, nonce-extraction harness and wrapper hash differ.
    Add-Type -AssemblyName System.IO.Compression.FileSystem
    $archive=[System.IO.Compression.ZipFile]::OpenRead($zip)
    try {
        $prefix='electionguard-kotlin-multiplatform-'+$lock.sdk.commit+'/'
        foreach ($entry in $archive.Entries) {
            $relative=$entry.FullName.Substring($prefix.Length)
            if (!$relative -or $entry.FullName.EndsWith('/')) { continue }
            if ($relative -notmatch '\.(kt|kts|toml|jar|properties)$') { continue }
            $reader=New-Object System.IO.MemoryStream
            $stream=$entry.Open()
            try { $stream.CopyTo($reader); $expected=$reader.ToArray() } finally { $stream.Dispose();$reader.Dispose() }
            $path=Join-Path $sdk $relative
            if ($relative -eq 'egklib/build.gradle.kts') {
                $task=[System.IO.File]::ReadAllText((Join-Path $projectRoot 'tools/electionguard/civic-task.gradle.kts'))
                [System.IO.File]::WriteAllText($path,[System.Text.Encoding]::UTF8.GetString($expected)+[Environment]::NewLine+$task)
            } elseif ($relative -eq 'gradle/wrapper/gradle-wrapper.properties') {
                [System.IO.File]::WriteAllText($path,[System.Text.Encoding]::UTF8.GetString($expected)+[Environment]::NewLine+'distributionSha256Sum='+$lock.sdk.gradle_sha256+[Environment]::NewLine)
            } else {
                $actual=[System.IO.File]::ReadAllBytes($path)
                $hasher=[System.Security.Cryptography.SHA256]::Create()
                try {
                    if ([Convert]::ToBase64String($hasher.ComputeHash($actual)) -ne [Convert]::ToBase64String($hasher.ComputeHash($expected))) { throw "Modified upstream source: $relative" }
                } finally { $hasher.Dispose() }
            }
        }
    } finally { $archive.Dispose() }
    Copy-Item -LiteralPath tools/electionguard/CivicVectors.kt -Destination (Join-Path $sdk 'egklib/src/jvmMain/kotlin/electionguard/cli/CivicVectors.kt')
    & npm.cmd run build
    if ($LASTEXITCODE -ne 0) { throw 'TypeScript build failed' }
    & node scripts/prepare-electionguard-oracle.mjs
    if ($LASTEXITCODE -ne 0) { throw 'SDK request preparation failed' }
    $oracle=Join-Path $projectRoot 'spec/fixtures/v0.4/oracle.TEST_ONLY.json'
    if ($NewProofs -and (Test-Path -LiteralPath $oracle)) {
        Move-Item -LiteralPath $oracle -Destination (Join-Path $cache ('previous-'+[guid]::NewGuid().ToString('N')+'.TEST_ONLY.json'))
    }
    $env:JAVA_HOME=$jdk
    $env:GRADLE_USER_HOME=Join-Path $cache 'gradle-cache'
    $env:CIVIC_ORACLE_REQUEST=Join-Path $cache 'request.json'
    $env:CIVIC_ORACLE_OUTPUT=$oracle
    $arguments=@(':egklib:civicVectors','--no-daemon','--console=plain','-Pkotlin.compiler.execution.strategy=in-process')
    if (!$Fetch) { $arguments+='--offline' }
    Push-Location -LiteralPath $sdk
    try { & .\gradlew.bat @arguments; if ($LASTEXITCODE -ne 0) { throw 'SDK oracle failed' } } finally { Pop-Location }
    if (!$NewProofs) {
        & node scripts/materialize-electionguard.mjs --check
        if ($LASTEXITCODE -ne 0) { throw 'SDK conversion differs' }
    }
} finally { Pop-Location }
