param(
    [string]$BaseUrl = 'http://localhost:8080',
    [ValidateSet('keyword', 'hybrid', 'hybrid_milvus')][string]$ExpectedMode = 'hybrid_milvus',
    [pscredential]$Credential,
    [ValidateRange(1, 15)][int]$TimeoutMinutes = 6
)

$ErrorActionPreference = 'Stop'
$origin = $BaseUrl.TrimEnd('/')
$api = "$origin/api/v1"
$headers = @{ Origin = $origin }
$json = 'application/json; charset=utf-8'

if ($Credential) {
    $login = @{ email = $Credential.UserName; password = $Credential.GetNetworkCredential().Password } | ConvertTo-Json -Compress
    Invoke-RestMethod "$api/auth/login" -Method Post -ContentType $json -Headers $headers -Body $login -SessionVariable session | Out-Null
    Remove-Variable login
} else {
    $guest = Invoke-RestMethod "$api/auth/guest" -Method Post -ContentType $json -Headers $headers -Body '{}' -SessionVariable session
    if (!$guest.trial_enabled -or $guest.trial_remaining -lt 1) {
        throw '需要剩余访客试用次数，或用 -Credential 提供已配置模型的账户。'
    }
}
$headers['Idempotency-Key'] = [guid]::NewGuid().ToString()
$body = @{ goal = 'HttpClient 如何在复用连接池的同时应对 DNS 记录变化？'; demo_knowledge = $true } | ConvertTo-Json -Compress
$created = Invoke-RestMethod "$api/research/tasks" -Method Post -ContentType $json -Headers $headers -Body $body -WebSession $session
if (!$created.task_id) { throw '创建任务响应缺少 task_id。' }
$taskUrl = "$api/research/tasks/$($created.task_id)"
$deadline = (Get-Date).AddMinutes($TimeoutMinutes)
do {
    $task = Invoke-RestMethod $taskUrl -WebSession $session -TimeoutSec 15
    if ($task.status -in 'SUCCEEDED', 'PARTIAL', 'FAILED', 'CANCELLED') { break }
    if ((Get-Date) -ge $deadline) { throw "等待任务 $($created.task_id) 超时。" }
    Start-Sleep -Seconds 2
} while ($true)

if ($task.status -ne 'SUCCEEDED') { throw "任务 $($created.task_id) 未成功：$($task.status) $($task.error_code)" }
$stream = Invoke-WebRequest "$taskUrl/events?after=0" -WebSession $session -TimeoutSec 15
$retrieval = [regex]::Matches($stream.Content, '(?m)^data: ?([^\r\n]+)') |
    ForEach-Object { $_.Groups[1].Value | ConvertFrom-Json } |
    Where-Object { $_.payload.step_key -eq 'tool-0-knowledge' -and $_.payload.retrieval_mode }
if ($retrieval.Count -ne 1 -or $retrieval.payload.retrieval_mode -ne $ExpectedMode -or $retrieval.payload.retrieval_hits -lt 1) {
    throw "未观察到 $ExpectedMode 知识库检索及命中事件。"
}
$evidence = Invoke-RestMethod "$taskUrl/report/evidence" -WebSession $session -TimeoutSec 15
$references = @($evidence.citations | Where-Object { $_.title -match '知识库摘要 public-tech-' })
if ($references.Count -eq 0) { throw '报告未引用知识库证据。' }
Write-Output "Knowledge smoke passed: $($created.task_id), $ExpectedMode, $($retrieval.payload.retrieval_hits) hits, $($references.Count) knowledge citations."
