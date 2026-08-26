[CmdletBinding()]
param(
    [string]$Publisher = (Join-Path $PSScriptRoot 'publish-download-portal.ps1')
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$Tokens = $null
$ParseErrors = $null
$Ast = [Management.Automation.Language.Parser]::ParseFile(
    $Publisher,
    [ref]$Tokens,
    [ref]$ParseErrors
)
if ($ParseErrors.Count -ne 0) {
    throw "publisher has PowerShell parse errors: $($ParseErrors[0].Message)"
}
$PublisherSource = [IO.File]::ReadAllText($Publisher)
if ($PublisherSource -match '(?m)^\s*Write-Host \$SuccessMarker\s*$') {
    throw 'Publisher must not echo the promoter success marker a second time.'
}

foreach ($Name in @(
    'ConvertTo-NativeArgument',
    'ConvertTo-LfUtf8Bytes',
    'Invoke-BoundedChild'
)) {
    $Definition = @($Ast.FindAll({
        param($Node)
        $Node -is [Management.Automation.Language.FunctionDefinitionAst] -and
            $Node.Name -eq $Name
    }, $true))
    if ($Definition.Count -ne 1) {
        throw "expected exactly one production function named $Name"
    }
    Invoke-Expression $Definition[0].Extent.Text
}

$Probe = 'CELIKPANEL_PUBLISHER_STDIN_PROBE'
$Utf8 = [Text.UTF8Encoding]::new($false)
[byte[]]$WireBytes = ConvertTo-LfUtf8Bytes (
    $Utf8.GetBytes("$Probe`r`n")
)
$ChildPowerShell = Join-Path $env:WINDIR 'System32\WindowsPowerShell\v1.0\powershell.exe'
$ByteProbeCommand = '$s=[Console]::OpenStandardInput();$b=New-Object byte[] 128;$n=$s.Read($b,0,$b.Length);[BitConverter]::ToString($b,0,$n)'
$Result = Invoke-BoundedChild `
    -FilePath $ChildPowerShell `
    -Arguments @('-NoProfile', '-NonInteractive', '-Command', $ByteProbeCommand) `
    -TimeoutSeconds 10 `
    -InputBytes $WireBytes

if (@($Result).Count -ne 1) {
    throw 'Invoke-BoundedChild leaked an extra pipeline object.'
}
foreach ($Property in @('ExitCode', 'TimedOut', 'Stdout', 'Stderr')) {
    if ($Result.PSObject.Properties.Name -notcontains $Property) {
        throw "Invoke-BoundedChild result is missing $Property."
    }
}
if ($Result.TimedOut -or $Result.ExitCode -ne 0 -or
    $Result.Stdout.Trim() -ne [BitConverter]::ToString($WireBytes)) {
    throw 'Invoke-BoundedChild did not round-trip non-empty stdin.'
}

if ($Utf8.GetString($WireBytes) -ne "$Probe`n" -or $WireBytes -contains 13) {
    throw 'Publisher wire input was not normalized to LF-only UTF-8.'
}

Write-Host 'download portal publisher PowerShell 5.1 runtime contract passed'
