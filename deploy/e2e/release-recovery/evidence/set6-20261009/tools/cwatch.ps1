# set6: Windows C: free space by Get-PSDrive, one reading every 30 s, for the cell queue inside WSL (which cannot
# run PowerShell itself). Ends when cwatch.stop exists or after 16 hours. Under 40 GiB it writes c-drive.below40.
$dir = $PSScriptRoot
$deadline = (Get-Date).AddHours(16)
while (-not (Test-Path (Join-Path $dir "cwatch.stop")) -and (Get-Date) -lt $deadline) {
    $d = Get-PSDrive C
    $gib = [math]::Round($d.Free / 1GB, 2).ToString([Globalization.CultureInfo]::InvariantCulture)
    $line = "{0} watch free_bytes={1} free_GiB={2}" -f (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ"), $d.Free, $gib
    [IO.File]::AppendAllText((Join-Path $dir "c-drive-watch.txt"), $line + "`n")
    if ($d.Free -lt 40GB) { [IO.File]::AppendAllText((Join-Path $dir "c-drive.below40"), $line + "`n") }
    Start-Sleep -Seconds 30
}
