<#
  Boots several ISOs at the same time, each in its own Hyper-V VM, through one lab copy of the server, and takes pictures of their screens.
  Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.

    powershell -ExecutionPolicy Bypass -File tools\lab_run.ps1 -Tests 'clonezilla:2:120','gparted:2:120','rescuezilla:3:150' -Out C:\bootlace-lab\run1

  A test is  <part of the ISO's file name>:<memory in GB>:<seconds to wait>  and may end with :sb (Secure Boot on) and/or :native (the firmware starts the ISO's own signed
  boot files). The ISOs are copied from -Source to a local folder first (reading an ISO a sector at a time over a share is very slow). Pictures and a summary go to -Out.
  Redirect the output to a file when starting this from another script: the server it starts keeps the pipe open.
#>
param(
    [Parameter(Mandatory)] [string[]] $Tests,
    [string] $Source = '\\nas\isos',
    [string] $IsoDir = 'C:\bootlace-lab\labiso',
    [string] $Out = 'C:\bootlace-lab\run',
    [string] $Work = 'C:\bootlace-lab\lab',
    [int] $StaggerSeconds = 20
)
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot
$pw = 'lab-pass-1'
New-Item -ItemType Directory -Force $Work, $Out, $IsoDir | Out-Null
$ip = (Get-NetIPAddress -InterfaceAlias 'vEthernet (bootlace-lab)' -AddressFamily IPv4).IPAddress
$prefix = ($ip -split '\.')[0..2] -join '.'
$base = "http://${ip}:8080"

Get-Process bootlace -ErrorAction SilentlyContinue | Stop-Process -Force
Get-VM | Where-Object Name -like 'bl-*' | ForEach-Object { Stop-VM -Name $_.Name -TurnOff -Force -ErrorAction SilentlyContinue; Remove-VM -Name $_.Name -Force }
Start-Sleep 1

# what to run
$plan = @()
foreach ($t in $Tests) {
    $p = $t.Split(':')
    $e = [ordered]@{ Iso = $p[0]; Mem = [int]$p[1]; Wait = [int]$p[2]; SB = ($p -contains 'sb'); Native = ($p -contains 'native'); Bios = ($p -contains 'bios') }
    $src = @(Get-ChildItem $Source -Recurse | Where-Object { $_.Name -match '\.(iso|img|raw)(\.(gz|xz|zst|bz2))?$' } | Where-Object { $_.Name -like "*$($e.Iso)*" })
    if ($src.Count -gt 1 -and @($src | Where-Object { $_.Name -eq "$($e.Iso).iso" }).Count -eq 1) { $src = @($src | Where-Object { $_.Name -eq "$($e.Iso).iso" }) }   # an exact name wins
    if ($src.Count -ne 1) { throw "'$($e.Iso)' matches $($src.Count) ISOs: $(($src | ForEach-Object Name) -join ', ')" }
    $e.File = $src[0]
    $e.Name = 't' + (($e.Iso -replace '[^a-zA-Z0-9]', '').ToLower())
    $plan += [pscustomobject]$e
}
# the ISOs, local; the ones of earlier runs that are not needed go
$keep = $plan | ForEach-Object { $_.File.Name }
Get-ChildItem $IsoDir | Where-Object { $_.Name -match '\.(iso|img|raw)(\.(gz|xz|zst|bz2))?$' -and $keep -notcontains $_.Name } | Remove-Item -Force
foreach ($e in $plan) {
    $dst = Join-Path $IsoDir $e.File.Name
    if (-not (Test-Path $dst) -or (Get-Item $dst).Length -ne $e.File.Length) { "copying $($e.File.Name) ..."; Copy-Item $e.File.FullName $IsoDir -Force }
}

# the server
$env:Path += ';C:\tools\go\bin;C:\tools\wimlib'
Push-Location $root; go build -o bin\bootlace.exe .\cmd\bootlace; Pop-Location
Remove-Item (Join-Path $Work 'config.json'), (Join-Path $Work 'pcs.json') -ErrorAction SilentlyContinue
$sargs = @('serve', '-iso-dir', "`"$IsoDir`"", '-ip', $ip, '-dhcp', 'full', '-pxe-only', '-mask', '255.255.0.0', '-router', $ip, '-dns', $ip, '-range-start', "$prefix.150", '-range-end', "$prefix.220",
    '-smb-port', '4445', '-config', "`"$(Join-Path $Work 'config.json')`"", '-admin-password', $pw)
Start-Process -FilePath (Join-Path $root 'bin\bootlace.exe') -ArgumentList $sargs -RedirectStandardOutput (Join-Path $Work 'out.log') -RedirectStandardError (Join-Path $Work 'err.log') -WindowStyle Hidden
Start-Sleep 4
$session = New-Object Microsoft.PowerShell.Commands.WebRequestSession
$h = @{ 'X-Bootlace' = '1' }
Invoke-RestMethod "$base/api/login" -Method Post -Headers $h -ContentType 'application/json' -Body (@{ password = $pw } | ConvertTo-Json) -WebSession $session | Out-Null
$isos = (Invoke-RestMethod "$base/api/isos" -WebSession $session).isos

# the VMs: each with its own fixed address and a rule that starts its ISO
$summary = @()
foreach ($e in $plan) {
    $m = @($isos | Where-Object { $_.name -eq $e.File.Name })[0]
    $e | Add-Member Id $m.id; $e | Add-Member Family "$($m.family) / $($m.method)"
    $vmArgs = @{ Action = 'create'; Name = $e.Name; MemoryGB = $e.Mem }
    if ($e.SB) { $vmArgs.SecureBoot = $true }
    if ($e.Bios) { $vmArgs.Bios = $true }
    & (Join-Path $PSScriptRoot 'pxe_vm.ps1') @vmArgs | Out-Null
    $vm = "bl-$($e.Name)"
    Stop-VM -Name $vm -TurnOff -Force
    if ($e.SB) { Set-VMFirmware -VMName $vm -SecureBootTemplate MicrosoftUEFICertificateAuthority }
    $raw = '00155D08{0:X2}{1:X2}' -f (Get-Random -Maximum 256), (Get-Random -Maximum 256)
    Set-VMNetworkAdapter -VMName $vm -StaticMacAddress $raw
    $mac = ($raw -replace '(..)(?!$)', '$1:').ToLower()
    $action = if ($e.Native) { "native:$($m.id)" } else { "iso:$($m.id)" }
    Invoke-RestMethod "$base/api/rules" -Method Post -Headers $h -ContentType 'application/json' -Body (@{ mac = $mac; action = $action; note = 'lab' } | ConvertTo-Json) -WebSession $session | Out-Null
    $e | Add-Member Mac $mac; $e | Add-Member Vm $vm
    "$($e.Name): $($e.File.Name) [$($e.Family)] $mac -> $action"
}
$t0 = Get-Date
foreach ($e in $plan) { Start-VM -Name $e.Vm; $e | Add-Member Started (Get-Date); Start-Sleep $StaggerSeconds }

# pictures, every 20 seconds, until every test has had its time
$pending = @($plan)
while ($pending.Count -gt 0) {
    Start-Sleep 20
    foreach ($e in @($pending)) {
        $elapsed = ((Get-Date) - $e.Started).TotalSeconds
        $png = Join-Path $Out "$($e.Name).png"
        try { & (Join-Path $PSScriptRoot 'pxe_vm.ps1') -Action shot -Name $e.Name -Out $png | Out-Null } catch { "$($e.Name): no picture ($($_.Exception.Message))" }   # a VM that shut itself down has none
        if ($elapsed -ge $e.Wait) {
            Copy-Item $png (Join-Path $Out "$($e.Name)-final.png") -Force
            Stop-VM -Name $e.Vm -TurnOff -Force -ErrorAction SilentlyContinue
            $pending = @($pending | Where-Object { $_.Name -ne $e.Name })
            "$($e.Name): final picture $(Join-Path $Out "$($e.Name)-final.png") after $([int]$elapsed) s"
        }
    }
}
Copy-Item (Join-Path $Work 'out.log') (Join-Path $Out 'server.log') -Force
"done: $Out"
