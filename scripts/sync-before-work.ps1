$ErrorActionPreference = 'Stop'
$project = Split-Path -Parent $PSScriptRoot

$status = git -C $project status --porcelain=v1 --untracked-files=all --ignore-submodules=none
if ($LASTEXITCODE -ne 0) {
    exit $LASTEXITCODE
}
if ($status) {
    $status | Write-Output
    Write-Warning 'Local changes detected. Resolve them manually before pulling.'
    exit 1
}

git -C $project pull --rebase --no-autostash
if ($LASTEXITCODE -ne 0) {
    $pullExitCode = $LASTEXITCODE
    Write-Warning 'Pull failed. Inspect Git status and resolve the issue before continuing.'
    exit $pullExitCode
}

git -C $project status --short --branch --untracked-files=all --ignore-submodules=none
exit $LASTEXITCODE
