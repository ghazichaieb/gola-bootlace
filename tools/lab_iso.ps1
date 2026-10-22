<#
  Boots one ISO in a fresh Hyper-V VM through a lab copy of the server, with no menu to click: the VM's MAC gets a rule that starts the ISO, and a picture of the
  screen is taken after a while.
  Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.

    powershell -ExecutionPolicy Bypass -File tools\lab_iso.ps1 -Iso clonezilla -MemoryGB 3 -WaitSeconds 120
    powershell -ExecutionPolicy Bypass -File tools\lab_iso.ps1 -Iso ubuntu-22 -MemoryGB 2 -SecureBoot -Native

  Needs the lab switch "bootlace-lab" (see lab.ps1), the ISO folder (a local folder or the NAS share), and wimlib-imagex on the PATH for -Native with Windows.
  -Native uses the rule that makes the PC's firmware start the ISO's signed boot files (works with Secure Boot on); without it the usual iPXE way is used.
#>
param(
    [Parameter(Mandatory)] [string] $Iso,                           # part of the ISO's file name
    [string] $Source = '\\nas\isos',     # where the ISOs are: the one for the test is copied to a local folder first (reading an ISO a sector at a time over a share is very slow)
    [string] $IsoDir = 'C:\bootlace-lab\labiso',
    [int] $MemoryGB = 3,
    [int] $WaitSeconds = 90,
    [switch] $SecureBoot,
    [switch] $Native,
    [switch] $Bios,
    [string] $Name = 'iso',
    [string] $Work = 'C:\bootlace-lab\lab',
    [string] $Shots = (Join-Path $env:TEMP 'bootlace-shots')
)
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot
$pw = 'lab-pass-1'
$base = $null
New-Item -ItemType Directory -Force $Work, $Shots | Out-Null
$ip = (Get-NetIPAddress -InterfaceAlias 'vEthernet (bootlace-lab)' -AddressFamily IPv4).IPAddress
$prefix = ($ip -split '\.')[0..2] -join '.'
$base = "http://${ip}:8080"

# stop the server of the last run first: it holds its ISO open
Get-Process bootlace -ErrorAction SilentlyContinue | Stop-Process -Force
Start-Sleep 1
# the ISO, on a local disk (only this one stays in the folder)
$src = @(Get-ChildItem $Source -Recurse -Filter *.iso | Where-Object { $_.Name -like "*$Iso*" })
if ($src.Count -ne 1) { throw "'$Iso' matches $($src.Count) ISOs in ${Source}: $(($src | ForEach-Object Name) -join ', ')" }
New-Item -ItemType Directory -Force $IsoDir | Out-Null
Get-ChildItem $IsoDir -Filter *.iso | Where-Object { $_.Name -ne $src[0].Name } | Remove-Item -Force
if (-not (Test-Path (Join-Path $IsoDir $src[0].Name)) -or (Get-Item (Join-Path $IsoDir $src[0].Name)).Length -ne $src[0].Length) {
    "copying $($src[0].Name) ($([math]::Round($src[0].Length / 1GB, 1)) GB) ..."
    Copy-Item $src[0].FullName $IsoDir -Force
}

# the server: a fresh lab copy of the current build, with the web interface on so that rules can be set
Get-Process bootlace -ErrorAction SilentlyContinue | Stop-Process -Force
Start-Sleep 1
$env:Path += ';C:\tools\go\bin;C:\tools\wimlib'
Push-Location $root; go build -o bin\bootlace.exe .\cmd\bootlace; Pop-Location
Remove-Item (Join-Path $Work 'config.json'), (Join-Path $Work 'pcs.json') -ErrorAction SilentlyContinue
$args2 = @('serve', '-iso-dir', "`"$IsoDir`"", '-ip', $ip, '-dhcp', 'full', '-pxe-only', '-mask', '255.255.0.0', '-range-start', "$prefix.150", '-range-end', "$prefix.200",
    '-smb-port', '4445', '-config', "`"$(Join-Path $Work 'config.json')`"", '-admin-password', $pw)
Start-Process -FilePath (Join-Path $root 'bin\bootlace.exe') -ArgumentList $args2 -RedirectStandardOutput (Join-Path $Work 'out.log') -RedirectStandardError (Join-Path $Work 'err.log') -WindowStyle Hidden
Start-Sleep 4

# the ISO's id
$session = New-Object Microsoft.PowerShell.Commands.WebRequestSession
$h = @{ 'X-Bootlace' = '1' }
Invoke-RestMethod "$base/api/login" -Method Post -Headers $h -ContentType 'application/json' -Body (@{ password = $pw } | ConvertTo-Json) -WebSession $session | Out-Null
$isos = (Invoke-RestMethod "$base/api/isos" -WebSession $session).isos
$match = @($isos | Where-Object { $_.name -like "*$Iso*" })
if ($match.Count -ne 1) { throw "'$Iso' matches $($match.Count) ISOs: $(($match | ForEach-Object name) -join ', ')" }
$m = $match[0]
"ISO: $($m.name)  [$($m.family) / $($m.method)]  id $($m.id)"

# the VM, stopped, to learn its MAC
$vmArgs = @{ Action = 'create'; Name = $Name; MemoryGB = $MemoryGB }
if ($Bios) { $vmArgs.Bios = $true }
if ($SecureBoot) { $vmArgs.SecureBoot = $true }
& (Join-Path $PSScriptRoot 'pxe_vm.ps1') @vmArgs | Out-Null
$vm = "bl-$Name"
Stop-VM -Name $vm -TurnOff -Force
if ($SecureBoot) { Set-VMFirmware -VMName $vm -SecureBootTemplate MicrosoftUEFICertificateAuthority }   # Linux shims need the third-party certificate
$raw = '00155D08{0:X2}{1:X2}' -f (Get-Random -Maximum 256), (Get-Random -Maximum 256)      # a fixed address, so that the rule can be made before the VM starts
Set-VMNetworkAdapter -VMName $vm -StaticMacAddress $raw
$mac = ($raw -replace '(..)(?!$)', '$1:').ToLower()
$action = if ($Native) { "native:$($m.id)" } else { "iso:$($m.id)" }
Invoke-RestMethod "$base/api/rules" -Method Post -Headers $h -ContentType 'application/json' -Body (@{ mac = $mac; action = $action; note = 'lab' } | ConvertTo-Json) -WebSession $session | Out-Null
"rule: $mac -> $action"
Start-VM -Name $vm
$start = Get-Date
$n = 0
while ($n -lt $WaitSeconds / 15) {
    Start-Sleep 15; $n++
    & (Join-Path $PSScriptRoot 'pxe_vm.ps1') -Action shot -Name $Name -Out (Join-Path $Shots "$Name-$n.png") | Out-Null
}
Copy-Item (Join-Path $Shots "$Name-$n.png") (Join-Path $Shots "$Name-last.png") -Force
"shots in $Shots ($Name-1.png ... $Name-$n.png, last = $Name-last.png), after $([int]((Get-Date) - $start).TotalSeconds) s"
Get-Content (Join-Path $Work 'out.log') | Select-String -Pattern $mac.Replace(':', '.'), 'nbd|iscsi|fetches|tftp: .* gets|rule' | Select-Object -Last 12 | ForEach-Object { $_.Line.Substring(0, [Math]::Min(170, $_.Line.Length)) }
