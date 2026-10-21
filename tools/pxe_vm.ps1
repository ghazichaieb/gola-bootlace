<#
  Test rig for Gola Bootlace (issue #1): a Hyper-V generation 2 VM that boots from the network, and a picture of its screen.
  Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.

    powershell -File tools\pxe_vm.ps1 -Action create -Name uefi1 -VSwitch bootlace-lab        # makes the VM (no disk needed), starts it
    powershell -File tools\pxe_vm.ps1 -Action shot   -Name uefi1 -Out screen.png             # a picture of what it shows
    powershell -File tools\pxe_vm.ps1 -Action key    -Name uefi1 -Key 13                     # presses a key (virtual key code: 13 = Enter, 40 = Down)
    powershell -File tools\pxe_vm.ps1 -Action remove -Name uefi1                             # stops and deletes it

  UEFI Secure Boot is switched off: the iPXE files Bootlace ships are not signed (issue #10). Needs an account in "Hyper-V Administrators".
#>
param(
    [Parameter(Mandatory)] [ValidateSet('create', 'shot', 'key', 'remove')] [string] $Action,
    [Parameter(Mandatory)] [string] $Name,
    [string] $VSwitch = 'bootlace-lab',
    [int] $MemoryGB = 4,
    [string] $Out = '',
    [int] $Key = 0,
    [string] $Folder = (Join-Path $env:TEMP 'bootlace-vms')
)
$ErrorActionPreference = 'Stop'
$vmName = "bl-$Name" -replace '^bl-bl-', 'bl-'

function Computer { Get-CimInstance -Namespace root\virtualization\v2 -ClassName Msvm_ComputerSystem -Filter "ElementName='$vmName'" }

switch ($Action) {
    'create' {
        if (Get-VM -Name $vmName -ErrorAction SilentlyContinue) { Stop-VM -Name $vmName -TurnOff -Force -ErrorAction SilentlyContinue; Remove-VM -Name $vmName -Force }
        Remove-Item -Recurse -Force (Join-Path $Folder $vmName) -ErrorAction SilentlyContinue
        $vm = New-VM -Name $vmName -Generation 2 -MemoryStartupBytes ($MemoryGB * 1GB) -Path $Folder -NewVHDPath (Join-Path $Folder "$vmName\disk.vhdx") -NewVHDSizeBytes 32GB -SwitchName $VSwitch
        Set-VM -VM $vm -StaticMemory -AutomaticCheckpointsEnabled $false -CheckpointType Disabled
        Set-VMProcessor -VM $vm -Count 2
        Set-VMFirmware -VM $vm -EnableSecureBoot Off -BootOrder @((Get-VMNetworkAdapter -VM $vm))
        Start-VM -VM $vm
        "started $vmName"
    }
    'key' {
        $keyboard = Get-CimAssociatedInstance -InputObject (Computer) -ResultClassName Msvm_Keyboard
        Invoke-CimMethod -InputObject $keyboard -MethodName PressKey -Arguments @{ keyCode = [uint32]$Key } | Out-Null
        Start-Sleep -Milliseconds 250
        Invoke-CimMethod -InputObject $keyboard -MethodName ReleaseKey -Arguments @{ keyCode = [uint32]$Key } | Out-Null
    }
    'shot' {
        $service = Get-CimInstance -Namespace root\virtualization\v2 -ClassName Msvm_VirtualSystemManagementService
        $settings = Get-CimAssociatedInstance -InputObject (Computer) -ResultClassName Msvm_VirtualSystemSettingData | Where-Object VirtualSystemType -eq 'Microsoft:Hyper-V:System:Realized'
        $r = Invoke-CimMethod -InputObject $service -MethodName GetVirtualSystemThumbnailImage -Arguments @{ TargetSystem = $settings; WidthPixels = [uint16]1024; HeightPixels = [uint16]768 }
        Add-Type -AssemblyName System.Drawing
        $bmp = New-Object System.Drawing.Bitmap(1024, 768, [System.Drawing.Imaging.PixelFormat]::Format16bppRgb565)
        $data = $bmp.LockBits((New-Object System.Drawing.Rectangle(0, 0, 1024, 768)), [System.Drawing.Imaging.ImageLockMode]::WriteOnly, $bmp.PixelFormat)
        [System.Runtime.InteropServices.Marshal]::Copy($r.ImageData, 0, $data.Scan0, [Math]::Min($r.ImageData.Length, $data.Stride * 768))
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
