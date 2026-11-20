[CmdletBinding()]
param(
    [Parameter(Mandatory, Position = 0)]
    [string]$File
)

$ErrorActionPreference = 'Stop'
$binary = (Resolve-Path -LiteralPath $File).ProviderPath

$thumbprint = ($env:RESEARCHPILOT_SIGNING_THUMBPRINT -replace '\s', '')
if ($thumbprint -notmatch '^[a-fA-F0-9]{40}$') {
    throw 'Set RESEARCHPILOT_SIGNING_THUMBPRINT to the signing certificate thumbprint.'
}

$store = $env:RESEARCHPILOT_SIGNING_STORE
if (!$store) { $store = 'CurrentUser' }
if ($store -notin @('CurrentUser', 'LocalMachine')) {
    throw 'RESEARCHPILOT_SIGNING_STORE must be CurrentUser or LocalMachine.'
}

$certificate = Get-Item -LiteralPath "Cert:\$store\My\$thumbprint"
$now = Get-Date
$codeSigning = '1.3.6.1.5.5.7.3.3'
if (!$certificate.HasPrivateKey -or $certificate.NotBefore -gt $now -or $certificate.NotAfter -le $now -or
    $codeSigning -notin $certificate.EnhancedKeyUsageList.ObjectId) {
    throw 'The selected certificate must be valid, include code-signing usage, and have an accessible private key.'
}

$chain = [System.Security.Cryptography.X509Certificates.X509Chain]::new()
try {
    $chain.ChainPolicy.ApplicationPolicy.Add([System.Security.Cryptography.Oid]::new($codeSigning)) | Out-Null
    if (!$chain.Build($certificate) -or $chain.ChainElements.Count -lt 2) {
        throw 'The code-signing certificate must chain to a trusted authority.'
    }
} finally {
    $chain.Dispose()
}

$timestamp = $null
if (![uri]::TryCreate($env:RESEARCHPILOT_SIGNING_TIMESTAMP_URL, [UriKind]::Absolute, [ref]$timestamp) -or $timestamp.Scheme -ne 'https') {
    throw 'Set RESEARCHPILOT_SIGNING_TIMESTAMP_URL to the HTTPS RFC 3161 timestamp endpoint.'
}

$signtool = (Get-Command signtool.exe -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1).Source
if (!$signtool) {
    $sdk = Join-Path ${env:ProgramFiles(x86)} 'Windows Kits\10\bin'
    $signtool = Get-ChildItem -Path (Join-Path $sdk '10.*\x64\signtool.exe') -File -ErrorAction SilentlyContinue |
        Sort-Object { [version]$_.Directory.Parent.Name } -Descending |
        Select-Object -First 1 -ExpandProperty FullName
}
if (!$signtool) { throw 'Install SignTool from the Windows SDK or add it to PATH.' }

$signArgs = @('sign', '/s', 'My', '/sha1', $thumbprint, '/fd', 'SHA256', '/tr', $timestamp.AbsoluteUri, '/td', 'SHA256')
if ($store -eq 'LocalMachine') { $signArgs += '/sm' }
& $signtool @signArgs $binary
if ($LASTEXITCODE -ne 0) { throw "SignTool signing failed with exit code $LASTEXITCODE." }

& $signtool verify /pa /all /tw $binary
if ($LASTEXITCODE -ne 0) { throw "SignTool verification failed with exit code $LASTEXITCODE." }

$result = Get-AuthenticodeSignature -LiteralPath $binary
if ($result.Status -ne 'Valid' -or $result.SignerCertificate.Thumbprint -ne $thumbprint -or !$result.TimeStamperCertificate) {
    throw 'The verified signature must match the selected publisher and have a timestamp.'
}
