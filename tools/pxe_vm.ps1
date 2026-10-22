<#
  Test rig for Gola Bootlace (issue #1): a Hyper-V generation 2 VM that boots from the network, and a picture of its screen.
  Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.

    powershell -File tools\pxe_vm.ps1 -Action create -Name uefi1 -VSwitch bootlace-lab        # makes the VM (no disk needed), starts it
    powershell -File tools\pxe_vm.ps1 -Action shot   -Name uefi1 -Out screen.png             # a picture of what it shows
    powershell -File tools\pxe_vm.ps1 -Action key    -Name uefi1 -Key 13                     # presses a key (virtual key code: 13 = Enter, 40 = Down)
    powershell -File tools\pxe_vm.ps1 -Action type  -Name uefi1 -Text "ip link`n"                 # types text (a new line presses Enter)
    powershell -File tools\pxe_vm.ps1 -Action remove -Name uefi1                             # stops and deletes it

  UEFI Secure Boot is switched off: the iPXE files Bootlace ships are not signed (issue #10). Needs an account in "Hyper-V Administrators".
#>
param(
    [Parameter(Mandatory)] [ValidateSet('create', 'shot', 'key', 'type', 'remove')] [string] $Action,
    [Parameter(Mandatory)] [string] $Name,
    [string] $VSwitch = 'bootlace-lab',
    [int] $MemoryGB = 4,
    [switch] $Bios,            # a generation 1 (legacy BIOS) VM instead of a UEFI one
    [string] $Out = '',
    [int] $Key = 0,
    [string] $Text = '',
    [string] $Folder = (Join-Path $env:TEMP 'bootlace-vms')
)
$ErrorActionPreference = 'Stop'
$vmName = "bl-$Name" -replace '^bl-bl-', 'bl-'

function Computer { Get-CimInstance -Namespace root\virtualization\v2 -ClassName Msvm_ComputerSystem -Filter "ElementName='$vmName'" }

switch ($Action) {
    'create' {
        if (Get-VM -Name $vmName -ErrorAction SilentlyContinue) { Stop-VM -Name $vmName -TurnOff -Force -ErrorAction SilentlyContinue; Remove-VM -Name $vmName -Force }
        Remove-Item -Recurse -Force (Join-Path $Folder $vmName) -ErrorAction SilentlyContinue
        $generation = if ($Bios) { 1 } else { 2 }
        $vm = New-VM -Name $vmName -Generation $generation -MemoryStartupBytes ($MemoryGB * 1GB) -Path $Folder -NewVHDPath (Join-Path $Folder "$vmName\disk.vhdx") -NewVHDSizeBytes 80GB -SwitchName $VSwitch
        Set-VM -VM $vm -StaticMemory -AutomaticCheckpointsEnabled $false -CheckpointType Disabled
        Set-VMProcessor -VM $vm -Count 2
        if ($Bios) {
            # a generation 1 VM starts from the network only through a "legacy" network card (the BIOS PXE ROM)
            Get-VMNetworkAdapter -VM $vm | Remove-VMNetworkAdapter
            Add-VMNetworkAdapter -VM $vm -IsLegacy $true -SwitchName $VSwitch
            Set-VMBios -VM $vm -StartupOrder @('LegacyNetworkAdapter', 'IDE', 'CD', 'Floppy')
        } else {
            Set-VMFirmware -VM $vm -EnableSecureBoot Off -BootOrder @((Get-VMNetworkAdapter -VM $vm))
        }
        Start-VM -VM $vm
        "started $vmName"
    }
    'key' {
        $keyboard = Get-CimAssociatedInstance -InputObject (Computer) -ResultClassName Msvm_Keyboard
        Invoke-CimMethod -InputObject $keyboard -MethodName PressKey -Arguments @{ keyCode = [uint32]$Key } | Out-Null
        Start-Sleep -Milliseconds 250
        Invoke-CimMethod -InputObject $keyboard -MethodName ReleaseKey -Arguments @{ keyCode = [uint32]$Key } | Out-Null
    }
    'type' {
        # Hyper-V's own TypeText mangles many characters on a Linux console, so every character is sent as the key press a US keyboard would make
        $keyboard = Get-CimAssociatedInstance -InputObject (Computer) -ResultClassName Msvm_Keyboard
        $plain = @{ ' ' = 32; '-' = 189; '=' = 187; '/' = 191; '.' = 190; ',' = 188; ';' = 186; "'" = 222; '[' = 219; ']' = 221; '\' = 220; '`' = 192 }
        $shifted = @{ '|' = 220; '_' = 189; '+' = 187; '?' = 191; '>' = 190; '<' = 188; ':' = 186; '"' = 222; '{' = 219; '}' = 221; '!' = 49; '@' = 50; '#' = 51; '$' = 52; '%' = 53; '&' = 55; '*' = 56; '(' = 57; ')' = 48 }
        function Press([int] $code, [bool] $shift) {
            if ($shift) { Invoke-CimMethod -InputObject $keyboard -MethodName PressKey -Arguments @{ keyCode = [uint32]16 } | Out-Null }
            Invoke-CimMethod -InputObject $keyboard -MethodName PressKey -Arguments @{ keyCode = [uint32]$code } | Out-Null
            Start-Sleep -Milliseconds 40
            Invoke-CimMethod -InputObject $keyboard -MethodName ReleaseKey -Arguments @{ keyCode = [uint32]$code } | Out-Null
            if ($shift) { Invoke-CimMethod -InputObject $keyboard -MethodName ReleaseKey -Arguments @{ keyCode = [uint32]16 } | Out-Null }
            Start-Sleep -Milliseconds 60
        }
        foreach ($character in $Text.ToCharArray()) {
            $s = [string]$character
            if ($character -eq "`n") { Press 13 $false }
            elseif ($character -cmatch '[a-z0-9]') { Press ([int][char]$s.ToUpper()) $false }
            elseif ($character -cmatch '[A-Z]') { Press ([int]$character) $true }
            elseif ($plain.ContainsKey($s)) { Press $plain[$s] $false }
            elseif ($shifted.ContainsKey($s)) { Press $shifted[$s] $true }
        }
    }
    'shot' {
        $service = Get-CimInstance -Namespace root\virtualization\v2 -ClassName Msvm_VirtualSystemManagementService
        $settings = Get-CimAssociatedInstance -InputObject (Computer) -ResultClassName Msvm_VirtualSystemSettingData | Where-Object VirtualSystemType -eq 'Microsoft:Hyper-V:System:Realized'
        # a generation 2 VM's screen can be asked for at 1024 x 768; a generation 1 VM's (legacy video) only at 640 x 480
        foreach ($size in @(@(1024, 768), @(640, 480))) {
            $w, $h = $size
            $r = Invoke-CimMethod -InputObject $service -MethodName GetVirtualSystemThumbnailImage -Arguments @{ TargetSystem = $settings; WidthPixels = [uint16]$w; HeightPixels = [uint16]$h }
            if ($r.ImageData -and $r.ImageData.Length -gt 0) { break }
        }
        Add-Type -AssemblyName System.Drawing
        $bmp = New-Object System.Drawing.Bitmap($w, $h, [System.Drawing.Imaging.PixelFormat]::Format16bppRgb565)
        $data = $bmp.LockBits((New-Object System.Drawing.Rectangle(0, 0, $w, $h)), [System.Drawing.Imaging.ImageLockMode]::WriteOnly, $bmp.PixelFormat)
        [System.Runtime.InteropServices.Marshal]::Copy($r.ImageData, 0, $data.Scan0, [Math]::Min($r.ImageData.Length, $data.Stride * $h))
        $bmp.UnlockBits($data)
        $bmp.Save($Out, [System.Drawing.Imaging.ImageFormat]::Png)
        "saved $Out"
    }
    'remove' {
        if (Get-VM -Name $vmName -ErrorAction SilentlyContinue) { Stop-VM -Name $vmName -TurnOff -Force -ErrorAction SilentlyContinue; Remove-VM -Name $vmName -Force }
        Remove-Item -Recurse -Force (Join-Path $Folder $vmName) -ErrorAction SilentlyContinue
        "removed $vmName"
    }
}
