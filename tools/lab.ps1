<#
  The test bench in one command: builds the server, starts it for the lab network and starts a fresh UEFI VM that boots from it.
  Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.

    powershell -File tools\lab.ps1 -IsoDir C:\isos -MemoryGB 8 -Entry 1

  Needs the Hyper-V switch "bootlace-lab" (an internal switch; the host's own address on it is used as the server address), Go on the PATH or in
  C:\tools\go\bin, and a firewall rule that lets the server program in (bin\bootlace.exe, or the file given with -Exe). -Entry is the menu entry (counting from 1) that
  starts by itself; -Pick 'ubuntu-22' chooses the entry whose name contains that text.

  The Secure Boot route (docs/secure-boot.md): -SecureBootDir C:\lab\sb starts the server with -secure-boot-dir and -SecureBoot makes the VM one with UEFI Secure Boot on
  (-SecureBootTemplate MicrosoftUEFICertificateAuthority is the one Linux shims need). -NoVm only starts the server. -Config and -Exe keep a second server instance
  apart from another one (only the server program that is running from -Exe is stopped first).
#>
param(
    [string] $IsoDir = 'C:\bootlace-lab\isos',
    [int] $MemoryGB = 4,
    [int] $Entry = 1,
    [string] $Pick = '',
    [int] $MenuTimeout = 3,           # seconds before the chosen entry starts by itself (0: wait for a choice)
    [string] $Name = 'uefi1',
    [switch] $Bios,                     # a legacy BIOS (generation 1) VM instead of a UEFI one
    [string] $SmbForward = '',          # for finding faults: pass the share connections on to another SMB server (host:port) and log them
    [string] $LogDir = 'C:\bootlace-lab',
    [string] $Exe = '',                 # where the server program is built to (default: bin\bootlace.exe of this checkout)
    [string] $Config = '',              # the settings file (default: bootlace.json in the current folder)
    [int] $HttpPort = 8080,
    [string] $SecureBootDir = '',       # the Secure Boot route: the folder with shimx64.efi, grubx64.efi (signed iPXE), mmx64.efi
    [switch] $SecureBootAll,            # ... for every 64-bit UEFI PC
    [string] $SecureBootShim = '',     # ... the file in that folder the PCs are told to boot (default shimx64.efi)
    [switch] $SecureBoot,               # the VM has UEFI Secure Boot on
    [string] $SecureBootTemplate = 'MicrosoftWindows',
    [string] $VmFolder = '',
    [switch] $NoVm
)
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot
$env:Path += ';C:\tools\go\bin'
if (-not $Exe) { $Exe = Join-Path $root 'bin\bootlace.exe' }
Get-Process bootlace -ErrorAction SilentlyContinue | Where-Object { $_.Path -eq $Exe } | Stop-Process -Force
Start-Sleep -Seconds 1
Push-Location $root
go build -o $Exe .\cmd\bootlace
Pop-Location
$ip = (Get-NetIPAddress -InterfaceAlias 'vEthernet (bootlace-lab)' -AddressFamily IPv4).IPAddress
$prefix = ($ip -split '\.')[0..2] -join '.'
$serverArgs = @('serve', '-iso-dir', $IsoDir, '-ip', $ip, '-dhcp', 'full', '-pxe-only', '-mask', '255.255.0.0', '-range-start', "$prefix.150", '-range-end', "$prefix.200",
          '-menu-default', $Entry, '-menu-timeout', $MenuTimeout, '-smb-port', '4445', '-smb-password', 'lab-pass-1', '-http-port', $HttpPort)   # Windows' own file sharing holds 445 on this PC

if ($Pick) { $serverArgs += @('-menu-pick', $Pick) }
if ($SmbForward) { $serverArgs += @('-smb-forward', $SmbForward) }
if ($Config) { $serverArgs += @('-config', $Config) }
if ($SecureBootDir) { $serverArgs += @('-secure-boot-dir', $SecureBootDir) }
if ($SecureBootAll) { $serverArgs += '-secure-boot-all' }
if ($SecureBootShim) { $serverArgs += @('-secure-boot-shim', $SecureBootShim) }
$serverArgs = $serverArgs | ForEach-Object { if ("$_" -match '\s') { '"' + $_ + '"' } else { "$_" } }   # Start-Process does not quote: a folder name with a space broke every argument after it
Start-Process -FilePath $Exe -ArgumentList $serverArgs -RedirectStandardOutput (Join-Path $LogDir 'bootlace-out.log') `
    -RedirectStandardError (Join-Path $LogDir 'bootlace-err.log') -WindowStyle Hidden
Start-Sleep -Seconds 2
if ($NoVm) { return }
$vm = @{ Action = 'create'; Name = $Name; MemoryGB = $MemoryGB; Bios = $Bios }
if ($SecureBoot) { $vm.SecureBoot = $true; $vm.SecureBootTemplate = $SecureBootTemplate }
if ($VmFolder) { $vm.Folder = $VmFolder }
& (Join-Path $PSScriptRoot 'pxe_vm.ps1') @vm
