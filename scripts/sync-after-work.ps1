$ErrorActionPreference = 'Stop'
$project = Split-Path -Parent $PSScriptRoot

git -C $project status --short --branch --untracked-files=all --ignore-submodules=none
if ($LASTEXITCODE -ne 0) {
    exit $LASTEXITCODE
}

Write-Output 'Review the status and diff. Commit any intended changes and push any local commits manually when ready.'
