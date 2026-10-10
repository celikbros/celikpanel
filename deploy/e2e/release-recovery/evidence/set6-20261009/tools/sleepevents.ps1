# set6: read-only: the Kernel-Power events of the run's window (sleep and modern standby entries and exits), in UTC.
param([int]$Hours = 12)
$out = Join-Path $PSScriptRoot "sleep-events.txt"
$events = Get-WinEvent -FilterHashtable @{LogName='System'; ProviderName='Microsoft-Windows-Kernel-Power'; StartTime=(Get-Date).AddHours(-$Hours)} -ErrorAction SilentlyContinue |
    Where-Object { $_.Id -in 42, 105, 107, 506, 507 } | Sort-Object TimeCreated
$lines = @("Kernel-Power events (System log) of the last $Hours hours, read at " + (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ") + ", UTC; 42 = entering sleep, 107 = resumed from sleep, 506 = entering modern standby, 507 = leaving it, 105 = power source change")
foreach ($e in $events) { $lines += ("{0} id={1}" -f $e.TimeCreated.ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ"), $e.Id) }
if (-not $events) { $lines += "none" }
[IO.File]::WriteAllText($out, (($lines -join "`n") + "`n"))
$lines
