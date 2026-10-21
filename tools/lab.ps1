<#
  The test bench in one command: builds the server, starts it for the lab network and starts a fresh UEFI VM that boots from it.
  Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.

    powershell -File tools\lab.ps1 -IsoDir C:\isos -MemoryGB 8 -Entry 1

  Needs the Hyper-V switch "bootlace-lab" (an internal switch; the host's own address on it is used as the server address), Go on the PATH or in
  C:\tools\go\bin, and a firewall rule that lets bin\bootlace.exe in. -Entry is the menu entry (counting from 1) that starts by itself; -Pick 'ubuntu-22' chooses the entry whose name contains that text.
#>
param(
    [string] $IsoDir = 'C:\bootlace-lab\isos',
    [int] $MemoryGB = 4,
    [int] $Entry = 1,
    [string] $Pick = '',
    [string] $Name = 'uefi1',
    [switch] $Bios,                     # a legacy BIOS (generation 1) VM instead of a UEFI one
    [string] $SmbForward = '',          # for finding faults: pass the share connections on to another SMB server (host:port) and log them
    [string] $LogDir = 'C:\bootlace-lab'
)
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot
$env:Path += ';C:\tools\go\bin'
Get-Process bootlace -ErrorAction SilentlyContinue | Stop-Process -Force
Start-Sleep -Seconds 1
Push-Location $root
go build -o bin\bootlace.exe .\cmd\bootlace
Pop-Location
$ip = (Get-NetIPAddress -InterfaceAlias 'vEthernet (bootlace-lab)' -AddressFamily IPv4).IPAddress
$prefix = ($ip -split '\.')[0..2] -join '.'
$serverArgs = @('serve', '-iso-dir', $IsoDir, '-ip', $ip, '-dhcp', 'full', '-pxe-only', '-mask', '255.255.0.0', '-range-start', "$prefix.150", '-range-end', "$prefix.200",
          '-menu-default', $Entry, '-menu-timeout', '3', '-smb-port', '4445', '-smb-password', 'lab-pass-1')   # Windows' own file sharing holds 445 on this PC

if ($Pick) { $serverArgs += @('-menu-pick', $Pick) }
if ($SmbForward) { $serverArgs += @('-smb-forward', $SmbForward) }
Start-Process -FilePath (Join-Path $root 'bin\bootlace.exe') -ArgumentList $serverArgs -RedirectStandardOutput (Join-Path $LogDir 'bootlace-out.log') `
    -RedirectStandardError (Join-Path $LogDir 'bootlace-err.log') -WindowStyle Hidden
Start-Sleep -Seconds 2
& (Join-Path $PSScriptRoot 'pxe_vm.ps1') -Action create -Name $Name -MemoryGB $MemoryGB -Bios:$Bios
