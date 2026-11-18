param([string]$Distribution='Ubuntu')
$ErrorActionPreference='Stop'
$project=Split-Path $PSScriptRoot
Push-Location $project
$previous=@{}
foreach($name in @('GOMODCACHE','GOCACHE','GOOS','CGO_ENABLED')) {
    $previous[$name]=[Environment]::GetEnvironmentVariable($name,'Process')
}
try {
    & ./scripts/init-local.ps1
    $env:GOMODCACHE=Join-Path $project '.cache/mod'
    $env:GOCACHE=Join-Path $project '.cache/go'
    go test ./...
    if($LASTEXITCODE){throw 'Go tests failed'}
    go vet ./...
    if($LASTEXITCODE){throw 'Go vet failed'}
    node --test web/events.test.mjs
    if($LASTEXITCODE){throw 'Frontend state tests failed'}
    npm --prefix web run build
    if($LASTEXITCODE){throw 'Frontend build failed'}
    $env:GOOS='linux'
    $env:CGO_ENABLED='0'
    go test -c -o .cache/m2-integration ./cmd/researchpilot
    if($LASTEXITCODE){throw 'Integration build failed'}
    wsl -d $Distribution -u root -- bash scripts/verify-wsl.sh
    if($LASTEXITCODE){throw 'Database integration tests failed'}
} finally {
    foreach($name in $previous.Keys) {
        [Environment]::SetEnvironmentVariable($name,$previous[$name],'Process')
    }
    Pop-Location
}
