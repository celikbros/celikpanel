$ErrorActionPreference = 'Stop'

$OpenSshRoot = if ([Environment]::Is64BitProcess) { Join-Path $env:WINDIR 'System32\OpenSSH' } else { Join-Path $env:WINDIR 'Sysnative\OpenSSH' }
$Ssh = Join-Path $OpenSshRoot 'ssh.exe'
$Scp = Join-Path $OpenSshRoot 'scp.exe'
$Key = 'C:\Users\alice\.ssh\celikpanel_deploy'
$Target = 'celikpanel.net_93@185.95.0.123'
$Package = 'C:\tmp\celikpanel-alpha35-portal-candidate-run32518010435\celikpanel-alpha35-portal-31c28e9-seq35.tar.gz'
$PackageSize = 44457897
$PackageSha = 'a288f9a1be381552f59a35a41c666d2e1aee989e72715c78259d10c9b6016256'
$Upload = '/var/www/vhosts/celikpanel.net/.upload-alpha35-user-20260821-31c28e9-a288f9a1be38'
$PromotionScript = 'C:\tmp\celikpanel-alpha35-portal-candidate-run32518010435\promote-alpha35-portal.py'
$PromotionScriptSize = 26937
$PromotionScriptSha = '087be6fb989e8ee933703f960d521820f30b95c07cce38cb386ae09d33f535ee'

foreach ($Tool in @($Ssh, $Scp, $Key, $Package, $PromotionScript)) {
    if (-not (Test-Path -LiteralPath $Tool -PathType Leaf)) {
        throw "Required file not found: $Tool"
    }
}

$PackageFile = Get-Item -LiteralPath $Package -ErrorAction Stop
if ($PackageFile.Length -ne $PackageSize) { throw 'Portal package size mismatch.' }
if ((Get-FileHash -LiteralPath $Package -Algorithm SHA256).Hash.ToLowerInvariant() -ne $PackageSha) {
    throw 'Portal package SHA-256 mismatch.'
}

[byte[]]$PromotionBytes = [System.IO.File]::ReadAllBytes($PromotionScript)
if ($PromotionBytes.LongLength -ne $PromotionScriptSize) { throw 'Promotion script size mismatch.' }
$PromotionHasher = [System.Security.Cryptography.SHA256]::Create()
try {
    $PromotionSnapshotSha = [System.BitConverter]::ToString($PromotionHasher.ComputeHash($PromotionBytes)).Replace('-', '').ToLowerInvariant()
} finally {
    $PromotionHasher.Dispose()
}
if ($PromotionSnapshotSha -ne $PromotionScriptSha) {
    throw 'Promotion script SHA-256 mismatch.'
}

$Prepare = 'set -eu; root=/var/www/vhosts/celikpanel.net; live=$root/httpdocs; backups=$root/portal-backups; lock=$root/.portal-deploy.lock; upload=$root/.upload-alpha35-user-20260821-31c28e9-a288f9a1be38; test "$(readlink -f -- "$root")" = "$root"; test "$(readlink -f -- "$live")" = "$live"; test "$(readlink -f -- "$backups")" = "$backups"; test -d "$live"; test ! -L "$live"; test -d "$backups"; test ! -L "$backups"; test -f "$lock"; test ! -L "$lock"; test "$(stat -c %a -- "$live")" = 755; test "$(stat -c %d -- "$live")" = "$(stat -c %d -- "$backups")"; test "$(cat -- "$live/releases/latest.txt")" = v0.1.0-alpha.34; test -f "$live/releases/index.json"; test ! -L "$live/releases/index.json"; test -f "$live/releases/latest.json"; test ! -L "$live/releases/latest.json"; test -f "$live/releases/latest.txt"; test ! -L "$live/releases/latest.txt"; test ! -e "$live/releases/v0.1.0-alpha.35"; for n in 9 10 11 12 13 14 15 16 17 18 19 20 21 22 23 24 25 26 27 28 30 31 32 33 34; do test -d "$live/releases/v0.1.0-alpha.$n"; test ! -L "$live/releases/v0.1.0-alpha.$n"; done; test "$(find "$live/releases" -mindepth 1 -maxdepth 1 -printf x | wc -c)" = 28; for spec in .upload-alpha13-20260812T132500Z:1049164 .upload-alpha14-20260813T0825Z:1048601 .upload-alpha16-20260813T1919Z:1049166; do name=${spec%%:*}; inode=${spec#*:}; p=$root/$name; test -d "$p"; test ! -L "$p"; test "$(stat -c %a -- "$p")" = 700; test "$(stat -c %u -- "$p")" = "$(id -u)"; test "$(stat -c %d -- "$p")" = 2050; test "$(stat -c %i -- "$p")" = "$inode"; done; test ! -e "$upload"; umask 077; mkdir -- "$upload"; chmod 700 -- "$upload"'
& $Ssh -i $Key -o BatchMode=yes -o StrictHostKeyChecking=yes -o UpdateHostKeys=no $Target $Prepare
if ($LASTEXITCODE -ne 0) { throw 'Remote preflight failed; package was not uploaded.' }

& $Scp -i $Key -o BatchMode=yes -o StrictHostKeyChecking=yes -o UpdateHostKeys=no $Package ($Target + ':' + $Upload + '/portal.tar.gz')
if ($LASTEXITCODE -ne 0) { throw 'Package upload failed.' }

$Verify = 'set -eu; f=/var/www/vhosts/celikpanel.net/.upload-alpha35-user-20260821-31c28e9-a288f9a1be38/portal.tar.gz; test -f "$f"; test ! -L "$f"; chmod 600 -- "$f"; test "$(stat -c %h -- "$f")" = 1; test "$(stat -c %u -- "$f")" = "$(id -u)"; test "$(stat -c %s -- "$f")" = 44457897; set -- $(sha256sum -- "$f"); test "$1" = a288f9a1be381552f59a35a41c666d2e1aee989e72715c78259d10c9b6016256; echo UPLOAD_VERIFIED'
& $Ssh -i $Key -o BatchMode=yes -o StrictHostKeyChecking=yes -o UpdateHostKeys=no $Target $Verify
if ($LASTEXITCODE -ne 0) { throw 'Remote package verification failed; promotion was not started.' }

$PromotionStartInfo = [System.Diagnostics.ProcessStartInfo]::new()
$PromotionStartInfo.FileName = $Ssh
$PromotionStartInfo.Arguments = '-i "' + $Key + '" -o BatchMode=yes -o StrictHostKeyChecking=yes -o UpdateHostKeys=no ' + $Target + ' "python3 -"'
$PromotionStartInfo.UseShellExecute = $false
$PromotionStartInfo.RedirectStandardInput = $true
$PromotionStartInfo.CreateNoWindow = $true
$PromotionProcess = [System.Diagnostics.Process]::new()
$PromotionProcess.StartInfo = $PromotionStartInfo
$PromotionStarted = $false
try {
    $PromotionStarted = $PromotionProcess.Start()
    if (-not $PromotionStarted) { throw 'Could not start portal promotion SSH process.' }
    $PromotionInput = $PromotionProcess.StandardInput.BaseStream
    try {
        $PromotionInput.Write($PromotionBytes, 0, $PromotionBytes.Length)
        $PromotionInput.Flush()
    } finally {
        $PromotionInput.Close()
    }
    $PromotionProcess.WaitForExit()
    if ($PromotionProcess.ExitCode -ne 0) { throw 'Portal promotion failed or was rolled back automatically.' }
} finally {
    try {
        if ($PromotionStarted -and -not $PromotionProcess.HasExited) {
            $PromotionProcess.Kill()
            $PromotionProcess.WaitForExit()
        }
    } finally {
        $PromotionProcess.Dispose()
    }
}

Write-Host 'ALPHA35_PORTAL_PUBLISHED'
