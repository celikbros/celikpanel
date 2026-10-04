# upd12: process-level "system required" request for the duration of the run only (no setting changed).
# Ends when the stop file exists or after 8 hours, whichever comes first; the request ends with the process.
$stop = Join-Path $PSScriptRoot "keepawake.stop"
$log = Join-Path $PSScriptRoot "keepawake.log"
Add-Type -Namespace Upd12 -Name Power -MemberDefinition '[DllImport("kernel32.dll")] public static extern uint SetThreadExecutionState(uint esFlags);'
$ES_CONTINUOUS = [uint32]2147483648
$ES_SYSTEM_REQUIRED = [uint32]1
$prev = [Upd12.Power]::SetThreadExecutionState($ES_CONTINUOUS -bor $ES_SYSTEM_REQUIRED)
Add-Content -Path $log -Value ("{0} started pid={1} previous_state={2}" -f (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ"), $PID, $prev)
$deadline = (Get-Date).AddHours(8)
while (-not (Test-Path $stop) -and (Get-Date) -lt $deadline) { Start-Sleep -Seconds 30 }
[void][Upd12.Power]::SetThreadExecutionState($ES_CONTINUOUS)
Add-Content -Path $log -Value ("{0} ended (stop file present: {1})" -f (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ"), (Test-Path $stop))
