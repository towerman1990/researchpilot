param([string]$BaseUrl = 'http://localhost:8080')
$ErrorActionPreference = 'Stop'
$api = "$BaseUrl/api/v1"
$password = 'local-smoke-password-2026'
$headers = @{ 'Idempotency-Key' = [guid]::NewGuid().ToString() }

function New-TestSession {
    $mail = "$([guid]::NewGuid().ToString('N'))@example.test"
    $credentials = @{ email=$mail; password=$password } | ConvertTo-Json -Compress
    Invoke-RestMethod "$api/auth/register" -Method Post -ContentType 'application/json' -Body $credentials | Out-Null
    Invoke-RestMethod "$api/auth/login" -Method Post -ContentType 'application/json' -Body $credentials -SessionVariable session | Out-Null
    return $session
}

function Assert-HttpStatus([scriptblock]$Request, [int]$Expected) {
    try {
        & $Request | Out-Null
    } catch {
        if ($_.Exception.Response -and [int]$_.Exception.Response.StatusCode -eq $Expected) { return }
        throw
    }
    throw "Expected HTTP $Expected but the request succeeded."
}

$owner = New-TestSession
$other = New-TestSession
$body = @{ goal='验证 M1 任务流转' } | ConvertTo-Json -Compress
$created = Invoke-RestMethod "$api/research/tasks" -Method Post -ContentType 'application/json; charset=utf-8' -Headers $headers -WebSession $owner -Body $body
$replay = Invoke-RestMethod "$api/research/tasks" -Method Post -ContentType 'application/json; charset=utf-8' -Headers $headers -WebSession $owner -Body $body
if ($replay.task_id -ne $created.task_id) { throw 'Idempotent request created a second task.' }
Assert-HttpStatus {
    Invoke-RestMethod "$api/research/tasks" -Method Post -ContentType 'application/json' -Headers $headers -WebSession $owner -Body '{"goal":"different"}'
} 409

$taskUrl = "$api/research/tasks/$($created.task_id)"
Assert-HttpStatus { Invoke-RestMethod $taskUrl -WebSession $other } 404
Assert-HttpStatus { Invoke-RestMethod "$taskUrl/report" -WebSession $other } 404
Assert-HttpStatus { Invoke-RestMethod "$taskUrl/events" -WebSession $other } 404
$events = Invoke-WebRequest "$taskUrl/events?after=0" -WebSession $owner -TimeoutSec 60
$eventIds = [regex]::Matches($events.Content, '(?m)^id: (\d+)\r?$') | ForEach-Object { $_.Groups[1].Value }
if (($eventIds -join ',') -ne '1,2,3,4') { throw "Unexpected SSE event IDs: $eventIds" }
if ($events.Content -notmatch '(?m)^event: report_ready') { throw 'Missing report_ready event.' }
$tail = Invoke-WebRequest "$taskUrl/events?after=0" -Headers @{'Last-Event-ID'='2'} -WebSession $owner -TimeoutSec 10
if ($tail.Content -match '(?m)^id: [12]\r?$' -or $tail.Content -notmatch '(?m)^id: 4\r?$') { throw 'SSE resume cursor failed.' }

$task = Invoke-RestMethod $taskUrl -WebSession $owner
$report = Invoke-RestMethod "$taskUrl/report" -WebSession $owner
if ($task.status -ne 'SUCCEEDED' -or $report.markdown -notmatch 'M1' -or $report.markdown -notmatch '验证 M1') {
    throw 'Terminal state or report content does not match.'
}
Write-Output "M1 smoke passed for task $($created.task_id)."
