param([string]$Label = "reading")
$d = Get-PSDrive C
$gib = [math]::Round($d.Free / 1GB, 2).ToString([Globalization.CultureInfo]::InvariantCulture)
$line = "{0} {1} free_bytes={2} free_GiB={3}" -f (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ"), $Label, $d.Free, $gib
[IO.File]::AppendAllText((Join-Path $PSScriptRoot "c-drive.txt"), $line + "`n")
$line
