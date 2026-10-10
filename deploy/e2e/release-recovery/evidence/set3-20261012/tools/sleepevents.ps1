# set3: read-only: the Kernel-Power events of the run's window (modern standby entries and exits), in UTC.
param([int]$Hours = 6)
$out = Join-Path $PSScriptRoot "sleep-events.txt"
$events = Get-WinEvent -FilterHashtable @{LogName='System'; ProviderName='Microsoft-Windows-Kernel-Power'; StartTime=(Get-Date).AddHours(-$Hours)} -ErrorAction SilentlyContinue |
    Where-Object { $_.Id -in 42, 105, 107, 506, 507 } | Sort-Object TimeCreated
$lines = @("Kernel-Power events (System log) of the last $Hours hours, UTC; 506 = entering modern standby, 507 = leaving it, 105 = power source change")
foreach ($e in $events) { $lines += ("{0} id={1}" -f $e.TimeCreated.ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ"), $e.Id) }
if (-not $events) { $lines += "none" }
[IO.File]::WriteAllLines($out, $lines)
$lines
