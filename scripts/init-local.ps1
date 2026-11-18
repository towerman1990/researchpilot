$ErrorActionPreference = 'Stop'
$project = Split-Path $PSScriptRoot
$cache = Join-Path $project '.cache'
$keyPath = Join-Path $cache 'model-master.key'
New-Item -ItemType Directory -Path $cache -Force | Out-Null
if (!(Test-Path -LiteralPath $keyPath)) {
    $bytes = New-Object byte[] 32
    [Security.Cryptography.RandomNumberGenerator]::Fill($bytes)
    [IO.File]::WriteAllText($keyPath,[Convert]::ToHexString($bytes).ToLowerInvariant())
}
Write-Output 'Local model encryption key is ready.'
