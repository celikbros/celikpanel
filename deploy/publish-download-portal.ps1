# The single supported operator entry point for a download-portal transaction.
# A timeout, disconnect, nonzero promoter exit, or absent success marker leaves
# the outcome UNKNOWN. Never retry this script before read-only inspection.
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$IdentityFile,
    [Parameter(Mandatory = $true)][string]$Target,
    [Parameter(Mandatory = $true)][string]$Package,
    [Parameter(Mandatory = $true)][string]$PreviousVersion,
    [Parameter(Mandatory = $true)][string]$TargetVersion,
    [Parameter(Mandatory = $true)][long]$PackageSize,
    [Parameter(Mandatory = $true)][string]$PackageSha256,
    [Parameter(Mandatory = $true)][string]$PublicBaseUrl,
    [Parameter(Mandatory = $true)][string]$RemoteRoot,
    [string]$RemoteLive,
    [string]$RemoteBackups,
    [string]$RemoteLock,
    [string]$Promoter = (Join-Path $PSScriptRoot 'promote-download-portal.py'),
    [string]$PublicVerifier = (Join-Path $PSScriptRoot 'verify-download-portal-public.py'),
    [string]$SshExecutable,
    [string]$ScpExecutable,
    [ValidateRange(1, 65535)][int]$SshPort = 22,
    [ValidateRange(5, 120)][int]$ControlTimeoutSeconds = 30,
    [ValidateRange(30, 3600)][int]$TransferTimeoutSeconds = 900,
    [ValidateRange(30, 900)][int]$PromotionTimeoutSeconds = 300,
    [ValidateRange(1, 60)][int]$PublicRequestTimeoutSeconds = 30,
    [ValidateRange(15, 600)][int]$PublicTotalTimeoutSeconds = 180
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$SuccessMarker = 'CELIKPANEL_DOWNLOAD_PORTAL_PUBLISHED'
$VersionPattern = '^v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$'
$ShaPattern = '^[0-9a-f]{64}$'
$RemotePathPattern = '^/[A-Za-z0-9._+/-]+$'

function Assert-RemotePath([string]$Value, [string]$Label) {
    if ($Value -notmatch $RemotePathPattern -or $Value.Contains('//') -or
        $Value.Contains('/./') -or $Value.Contains('/../') -or
        $Value.EndsWith('/.') -or $Value.EndsWith('/..')) {
        throw "$Label is not a canonical safe absolute remote path: $Value"
    }
}

function ConvertTo-BashLiteral([string]$Value) {
    if ($Value.Contains("'")) { throw 'A shell argument contains a forbidden single quote.' }
    return "'" + $Value + "'"
}

function ConvertTo-NativeArgument([string]$Value) {
    if ($Value.Length -gt 0 -and $Value -notmatch '[\s"]') { return $Value }
    $Builder = [Text.StringBuilder]::new()
    [void]$Builder.Append('"')
    $Backslashes = 0
    foreach ($Character in $Value.ToCharArray()) {
        if ($Character -eq '\') {
            $Backslashes++
            continue
        }
        if ($Character -eq '"') {
            [void]$Builder.Append(('\' * (($Backslashes * 2) + 1)))
            [void]$Builder.Append('"')
            $Backslashes = 0
            continue
        }
        if ($Backslashes -gt 0) {
            [void]$Builder.Append(('\' * $Backslashes))
            $Backslashes = 0
        }
        [void]$Builder.Append($Character)
    }
    if ($Backslashes -gt 0) { [void]$Builder.Append(('\' * ($Backslashes * 2))) }
    [void]$Builder.Append('"')
    return $Builder.ToString()
}

function ConvertTo-LfUtf8Bytes([byte[]]$Bytes) {
    $Utf8 = [Text.UTF8Encoding]::new($false, $true)
    $Text = $Utf8.GetString($Bytes)
    if ($Text.Length -gt 0 -and [int]$Text[0] -eq 0xFEFF) {
        $Text = $Text.Substring(1)
    }
    $Cr = [string][char]13
    $Lf = [string][char]10
    $Text = $Text.Replace($Cr + $Lf, $Lf).Replace($Cr, $Lf)
    return $Utf8.GetBytes($Text)
}

function Get-BytesSha256([byte[]]$Bytes) {
    $Hasher = [Security.Cryptography.SHA256]::Create()
    try { return [BitConverter]::ToString($Hasher.ComputeHash($Bytes)).Replace('-', '').ToLowerInvariant() }
    finally { $Hasher.Dispose() }
}

function Get-RandomHex([int]$ByteCount) {
    [byte[]]$Bytes = New-Object byte[] $ByteCount
    $Generator = [Security.Cryptography.RandomNumberGenerator]::Create()
    try { $Generator.GetBytes($Bytes) }
    finally { $Generator.Dispose() }
    return [BitConverter]::ToString($Bytes).Replace('-', '').ToLowerInvariant()
}

function Invoke-BoundedChild {
    param(
        [Parameter(Mandatory = $true)][string]$FilePath,
        [Parameter(Mandatory = $true)][string[]]$Arguments,
        [Parameter(Mandatory = $true)][int]$TimeoutSeconds,
        [byte[]]$InputBytes
    )
    $Start = [Diagnostics.ProcessStartInfo]::new()
    $Start.FileName = $FilePath
    $Start.Arguments = (($Arguments | ForEach-Object { ConvertTo-NativeArgument $_ }) -join ' ')
    $Start.UseShellExecute = $false
    $Start.CreateNoWindow = $true
    $Start.RedirectStandardInput = $true
    $Start.RedirectStandardOutput = $true
    $Start.RedirectStandardError = $true
    $Process = [Diagnostics.Process]::new()
    $Process.StartInfo = $Start
    $SavedInputEncoding = [Console]::InputEncoding
    try {
        [Console]::InputEncoding = [Text.UTF8Encoding]::new($false)
        if (-not $Process.Start()) { throw "Could not start child process: $FilePath" }
    }
    finally {
        [Console]::InputEncoding = $SavedInputEncoding
    }
    $Clock = [Diagnostics.Stopwatch]::StartNew()
    $StdoutTask = $Process.StandardOutput.ReadToEndAsync()
    $StderrTask = $Process.StandardError.ReadToEndAsync()
    try {
        $TimedOut = $false
        if ($null -ne $InputBytes -and $InputBytes.Length -gt 0) {
            $WriteTask = $Process.StandardInput.BaseStream.WriteAsync(
                $InputBytes, 0, $InputBytes.Length
            )
            $Remaining = ($TimeoutSeconds * 1000) - [int]$Clock.ElapsedMilliseconds
            if ($Remaining -le 0 -or -not $WriteTask.Wait($Remaining)) {
                $TimedOut = $true
            } else {
                [void]$WriteTask.GetAwaiter().GetResult()
            }
        }
        if (-not $TimedOut) {
            $Process.StandardInput.Close()
            $Remaining = ($TimeoutSeconds * 1000) - [int]$Clock.ElapsedMilliseconds
            if ($Remaining -le 0 -or -not $Process.WaitForExit($Remaining)) {
                $TimedOut = $true
            }
        }
        if ($TimedOut) {
            try { $Process.Kill($true) } catch { try { $Process.Kill() } catch {} }
            if (-not $Process.WaitForExit(5000)) {
                return [pscustomobject]@{
                    ExitCode = $null
                    TimedOut = $true
                    Stdout = ''
                    Stderr = 'Child process did not terminate within the bounded kill grace.'
                }
            }
        }
        [Threading.Tasks.Task[]]$DrainTasks = @($StdoutTask, $StderrTask)
        if (-not [Threading.Tasks.Task]::WaitAll($DrainTasks, 5000)) {
            return [pscustomobject]@{
                ExitCode = $null
                TimedOut = $true
                Stdout = ''
                Stderr = 'Child output streams did not close within the bounded drain grace.'
            }
        }
        $Stdout = $StdoutTask.Result
        $Stderr = $StderrTask.Result
        $OutputLimit = 1024 * 1024
        if ($Stdout.Length -gt $OutputLimit) { $Stdout = $Stdout.Substring(0, $OutputLimit) }
        if ($Stderr.Length -gt $OutputLimit) { $Stderr = $Stderr.Substring(0, $OutputLimit) }
        return [pscustomobject]@{
            ExitCode = if ($Process.HasExited) { $Process.ExitCode } else { $null }
            TimedOut = $TimedOut
            Stdout = $Stdout
            Stderr = $Stderr
        }
    }
    catch {
        if (-not $Process.HasExited) {
            try { $Process.Kill($true) } catch { try { $Process.Kill() } catch {} }
            [void]$Process.WaitForExit(5000)
        }
        throw
    }
    finally { $Process.Dispose() }
}

function Show-ChildOutput($Result) {
    if (-not [string]::IsNullOrWhiteSpace($Result.Stdout)) { Write-Host $Result.Stdout.TrimEnd() }
    if (-not [string]::IsNullOrWhiteSpace($Result.Stderr)) { Write-Warning $Result.Stderr.TrimEnd() }
}

if ($PreviousVersion -notmatch $VersionPattern -or $TargetVersion -notmatch $VersionPattern) {
    throw 'PreviousVersion and TargetVersion must be canonical v-prefixed semantic versions.'
}
if ($PreviousVersion -eq $TargetVersion) { throw 'PreviousVersion and TargetVersion must differ.' }
if ($PackageSize -le 0 -or $PackageSha256 -notmatch $ShaPattern) {
    throw 'Package size or lowercase SHA-256 pin is invalid.'
}
if ($Target -notmatch '^[A-Za-z0-9._-]+@[A-Za-z0-9.:\[\]-]+$') {
    throw 'Target must be a plain user@host value; SSH options are separate parameters.'
}

$PublicUri = [Uri]$PublicBaseUrl
if (-not $PublicUri.IsAbsoluteUri -or $PublicUri.Scheme -ne 'https' -or
    -not [string]::IsNullOrEmpty($PublicUri.UserInfo) -or
    ($PublicUri.AbsolutePath -ne '/') -or
    -not [string]::IsNullOrEmpty($PublicUri.Query) -or
    -not [string]::IsNullOrEmpty($PublicUri.Fragment)) {
    throw 'PublicBaseUrl must be a credential-free HTTPS origin without path, query, or fragment.'
}

if ([string]::IsNullOrEmpty($RemoteLive)) { $RemoteLive = $RemoteRoot.TrimEnd('/') + '/httpdocs' }
if ([string]::IsNullOrEmpty($RemoteBackups)) { $RemoteBackups = $RemoteRoot.TrimEnd('/') + '/portal-backups' }
if ([string]::IsNullOrEmpty($RemoteLock)) { $RemoteLock = $RemoteRoot.TrimEnd('/') + '/.portal-deploy.lock' }
foreach ($Pair in @(
    @($RemoteRoot, 'RemoteRoot'),
    @($RemoteLive, 'RemoteLive'),
    @($RemoteBackups, 'RemoteBackups'),
    @($RemoteLock, 'RemoteLock')
)) {
    Assert-RemotePath $Pair[0] $Pair[1]
}
if (([IO.Path]::GetDirectoryName($RemoteLive)).Replace('\', '/') -ne $RemoteRoot.TrimEnd('/') -or
    ([IO.Path]::GetDirectoryName($RemoteBackups)).Replace('\', '/') -ne $RemoteRoot.TrimEnd('/') -or
    ([IO.Path]::GetDirectoryName($RemoteLock)).Replace('\', '/') -ne $RemoteRoot.TrimEnd('/')) {
    throw 'Remote live, backups, and lock must be direct children of RemoteRoot.'
}

if ([string]::IsNullOrEmpty($SshExecutable) -or [string]::IsNullOrEmpty($ScpExecutable)) {
    $OpenSshRoot = if ([Environment]::Is64BitProcess) {
        Join-Path $env:WINDIR 'System32\OpenSSH'
    } else {
        Join-Path $env:WINDIR 'Sysnative\OpenSSH'
    }
    if ([string]::IsNullOrEmpty($SshExecutable)) { $SshExecutable = Join-Path $OpenSshRoot 'ssh.exe' }
    if ([string]::IsNullOrEmpty($ScpExecutable)) { $ScpExecutable = Join-Path $OpenSshRoot 'scp.exe' }
}
foreach ($RequiredFile in @($IdentityFile, $Package, $Promoter, $PublicVerifier, $SshExecutable, $ScpExecutable)) {
    if (-not (Test-Path -LiteralPath $RequiredFile -PathType Leaf)) {
        throw "Required regular file is unavailable: $RequiredFile"
    }
}

$PackageItem = Get-Item -LiteralPath $Package -ErrorAction Stop
if ($PackageItem.Length -ne $PackageSize -or
    (Get-FileHash -LiteralPath $Package -Algorithm SHA256).Hash.ToLowerInvariant() -ne $PackageSha256) {
    throw 'Local portal package differs from the approved size/SHA-256 pin.'
}
[byte[]]$PromoterBytes = ConvertTo-LfUtf8Bytes ([IO.File]::ReadAllBytes($Promoter))
[byte[]]$VerifierBytes = [IO.File]::ReadAllBytes($PublicVerifier)
$VerifierSize = $VerifierBytes.LongLength
$VerifierSha256 = Get-BytesSha256 $VerifierBytes
if ($VerifierSize -le 0) { throw 'Pinned public verifier is empty.' }

$SafeTargetVersion = $TargetVersion -replace '[^A-Za-z0-9._+-]', '-'
$Stamp = [DateTime]::UtcNow.ToString('yyyyMMddTHHmmssZ')
$UploadName = ".upload-portal-$Stamp-$SafeTargetVersion-$(Get-RandomHex 8)"
$RemoteUpload = $RemoteRoot.TrimEnd('/') + '/' + $UploadName
$RemotePackage = $RemoteUpload + '/portal.tar.gz'
$RemoteVerifier = $RemoteUpload + '/verify-download-portal-public.py'
foreach ($Pair in @(
    @($RemoteUpload, 'RemoteUpload'),
    @($RemotePackage, 'RemotePackage'),
    @($RemoteVerifier, 'RemoteVerifier')
)) {
    Assert-RemotePath $Pair[0] $Pair[1]
}

$SshCommon = @(
    '-i', $IdentityFile,
    '-p', $SshPort.ToString(),
    '-o', 'BatchMode=yes',
    '-o', 'StrictHostKeyChecking=yes',
    '-o', 'UpdateHostKeys=no',
    '-o', 'ConnectTimeout=15',
    '-o', 'ServerAliveInterval=15',
    '-o', 'ServerAliveCountMax=2',
    $Target
)
$ScpCommon = @(
    '-i', $IdentityFile,
    '-P', $SshPort.ToString(),
    '-o', 'BatchMode=yes',
    '-o', 'StrictHostKeyChecking=yes',
    '-o', 'UpdateHostKeys=no',
    '-o', 'ConnectTimeout=15'
)
$Utf8NoBom = [Text.UTF8Encoding]::new($false)

$PreflightTemplate = @'
#!/bin/bash
set -eu
root=__ROOT__
live=__LIVE__
backups=__BACKUPS__
lock=__LOCK__
upload=__UPLOAD__
previous=__PREVIOUS__
target=__TARGET__
test -d "$root" && test ! -L "$root"
test -d "$live" && test ! -L "$live"
test -d "$backups" && test ! -L "$backups"
test -f "$lock" && test ! -L "$lock"
test "$(stat -c %d -- "$root")" = "$(stat -c %d -- "$live")"
test "$(stat -c %d -- "$root")" = "$(stat -c %d -- "$backups")"
test "$(cat -- "$live/releases/latest.txt")" = "$previous"
test ! -e "$live/releases/$target" && test ! -L "$live/releases/$target"
test ! -e "$upload" && test ! -L "$upload"
exec 9<>"$lock"
flock -n 9
umask 077
mkdir -- "$upload"
chmod 700 -- "$upload"
printf '%s\n' CELIKPANEL_PORTAL_UPLOAD_READY
'@
$Preflight = $PreflightTemplate.
    Replace('__ROOT__', (ConvertTo-BashLiteral $RemoteRoot)).
    Replace('__LIVE__', (ConvertTo-BashLiteral $RemoteLive)).
    Replace('__BACKUPS__', (ConvertTo-BashLiteral $RemoteBackups)).
    Replace('__LOCK__', (ConvertTo-BashLiteral $RemoteLock)).
    Replace('__UPLOAD__', (ConvertTo-BashLiteral $RemoteUpload)).
    Replace('__PREVIOUS__', (ConvertTo-BashLiteral $PreviousVersion)).
    Replace('__TARGET__', (ConvertTo-BashLiteral $TargetVersion))
[byte[]]$PreflightBytes = ConvertTo-LfUtf8Bytes ($Utf8NoBom.GetBytes($Preflight))
$PreflightResult = Invoke-BoundedChild -FilePath $SshExecutable -Arguments ($SshCommon + @('bash -s --')) -TimeoutSeconds $ControlTimeoutSeconds -InputBytes $PreflightBytes
Show-ChildOutput $PreflightResult
if ($PreflightResult.TimedOut -or $PreflightResult.ExitCode -ne 0 -or
    $PreflightResult.Stdout -notmatch '(?m)^CELIKPANEL_PORTAL_UPLOAD_READY$') {
    throw 'Remote preflight failed; promotion was not started. Inspect the unique upload path before retrying.'
}

$PackageUpload = Invoke-BoundedChild -FilePath $ScpExecutable -Arguments ($ScpCommon + @($Package, ($Target + ':' + $RemotePackage))) -TimeoutSeconds $TransferTimeoutSeconds
Show-ChildOutput $PackageUpload
if ($PackageUpload.TimedOut -or $PackageUpload.ExitCode -ne 0) {
    throw 'Package upload failed; promotion was not started and the unique upload directory is retained.'
}
$VerifierUpload = Invoke-BoundedChild -FilePath $ScpExecutable -Arguments ($ScpCommon + @($PublicVerifier, ($Target + ':' + $RemoteVerifier))) -TimeoutSeconds $ControlTimeoutSeconds
Show-ChildOutput $VerifierUpload
if ($VerifierUpload.TimedOut -or $VerifierUpload.ExitCode -ne 0) {
    throw 'Verifier upload failed; promotion was not started and the unique upload directory is retained.'
}

$PackageAfter = Get-Item -LiteralPath $Package -ErrorAction Stop
if ($PackageAfter.Length -ne $PackageSize -or
    (Get-FileHash -LiteralPath $Package -Algorithm SHA256).Hash.ToLowerInvariant() -ne $PackageSha256 -or
    (Get-BytesSha256 ([IO.File]::ReadAllBytes($PublicVerifier))) -ne $VerifierSha256) {
    throw 'A local pinned input changed during upload; promotion was not started.'
}

$RemoteVerifyTemplate = @'
#!/bin/bash
set -eu
upload=__UPLOAD__
package=__PACKAGE__
verifier=__VERIFIER__
test -d "$upload" && test ! -L "$upload"
test "$(find "$upload" -mindepth 1 -maxdepth 1 -printf x | wc -c)" = 2
test -f "$package" && test ! -L "$package"
test -f "$verifier" && test ! -L "$verifier"
chmod 600 -- "$package" "$verifier"
test "$(stat -c %s -- "$package")" = __PACKAGE_SIZE__
test "$(stat -c %s -- "$verifier")" = __VERIFIER_SIZE__
test "$(sha256sum -- "$package" | awk '{print $1}')" = __PACKAGE_SHA__
test "$(sha256sum -- "$verifier" | awk '{print $1}')" = __VERIFIER_SHA__
printf '%s\n' CELIKPANEL_PORTAL_UPLOAD_PINNED
'@
$RemoteVerify = $RemoteVerifyTemplate.
    Replace('__UPLOAD__', (ConvertTo-BashLiteral $RemoteUpload)).
    Replace('__PACKAGE__', (ConvertTo-BashLiteral $RemotePackage)).
    Replace('__VERIFIER__', (ConvertTo-BashLiteral $RemoteVerifier)).
    Replace('__PACKAGE_SIZE__', (ConvertTo-BashLiteral $PackageSize.ToString())).
    Replace('__VERIFIER_SIZE__', (ConvertTo-BashLiteral $VerifierSize.ToString())).
    Replace('__PACKAGE_SHA__', (ConvertTo-BashLiteral $PackageSha256)).
    Replace('__VERIFIER_SHA__', (ConvertTo-BashLiteral $VerifierSha256))
[byte[]]$RemoteVerifyBytes = ConvertTo-LfUtf8Bytes ($Utf8NoBom.GetBytes($RemoteVerify))
$VerifyResult = Invoke-BoundedChild -FilePath $SshExecutable -Arguments ($SshCommon + @('bash -s --')) -TimeoutSeconds $ControlTimeoutSeconds -InputBytes $RemoteVerifyBytes
Show-ChildOutput $VerifyResult
if ($VerifyResult.TimedOut -or $VerifyResult.ExitCode -ne 0 -or
    $VerifyResult.Stdout -notmatch '(?m)^CELIKPANEL_PORTAL_UPLOAD_PINNED$') {
    throw 'Remote upload pin verification failed; promotion was not started. The upload is retained.'
}

$PromoterArguments = @(
    '--root', $RemoteRoot,
    '--live', $RemoteLive,
    '--backups', $RemoteBackups,
    '--lock', $RemoteLock,
    '--upload-dir', $RemoteUpload,
    '--package', $RemotePackage,
    '--package-size', $PackageSize.ToString(),
    '--package-sha256', $PackageSha256,
    '--verifier', $RemoteVerifier,
    '--verifier-size', $VerifierSize.ToString(),
    '--verifier-sha256', $VerifierSha256,
    '--previous-version', $PreviousVersion,
    '--target-version', $TargetVersion,
    '--public-base-url', $PublicBaseUrl,
    '--public-timeout', $PublicRequestTimeoutSeconds.ToString(),
    '--public-total-timeout', $PublicTotalTimeoutSeconds.ToString()
)
$RemotePromotionCommand = 'python3 - ' + (($PromoterArguments | ForEach-Object { ConvertTo-BashLiteral $_ }) -join ' ')

# The promoter is streamed exactly once. There is intentionally no retry loop.
$PromotionResult = Invoke-BoundedChild -FilePath $SshExecutable -Arguments ($SshCommon + @($RemotePromotionCommand)) -TimeoutSeconds $PromotionTimeoutSeconds -InputBytes $PromoterBytes
Show-ChildOutput $PromotionResult
$MarkerCount = @($PromotionResult.Stdout -split "\r?\n" | Where-Object { $_ -eq $SuccessMarker }).Count
if ($PromotionResult.TimedOut -or $PromotionResult.ExitCode -ne 0 -or $MarkerCount -ne 1) {
    throw "Portal promotion outcome is UNKNOWN. DO NOT RETRY. Run read-only live/backup/stage/failed/lock inspection first. Retained upload: $RemoteUpload"
}
