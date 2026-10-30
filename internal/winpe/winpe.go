// Package winpe writes the small files that make Windows' own set-up environment (WinPE, the boot.wim of a Windows ISO) find its installation files
// over the network: the iPXE script that loads it with wimboot, the file that tells WinPE what to run, and the command file it runs.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package winpe

import (
	"fmt"
	"strings"
)

// Settings say where things are.
type Settings struct {
	Base     string // web address of this server, e.g. http://192.168.88.1:8080
	FS       string // web address of the inside of the ISO, e.g. http://192.168.88.1:8080/fs/ab12cd34ef
	Win      string // web address of the generated files, e.g. http://192.168.88.1:8080/w/ab12cd34ef
	Host     string // the server's address as a name for Windows, e.g. 192.168.88.1
	Share    string // the SMB share
	Folder   string // the folder of the share that shows this ISO (the share is mapped as a whole: mapping a folder of it makes Windows probe it for ever)
	User     string
	Password string
	Port     int  // SMB port (445 unless Windows cannot reach the server there)
	Hold     bool // test aid: connect to the share but leave Setup for the person at the keyboard to start
	PEOnly   bool // a Windows PE with no installation image: nothing is added to it
	// Bypass names the checks of Windows 11 Setup to skip, as the names of the LabConfig values (BypassTPMCheck ...): each is written as 1 before Setup starts, which is how Setup is told to
	// go on on a PC that does not have what Windows 11 asks for. Empty skips none.
	Bypass []string
	// Drivers says that the boot.wim carries a folder X:Drivers (see drivers.go): its packages are loaded with drvload before the network starts, and Setup is told to add them to the
	// Windows it installs. PatchedWIM makes the iPXE script load the server's copy of boot.wim (the one that holds them) instead of the ISO's own.
	Drivers, PatchedWIM bool
}

// IPXE is the script that loads the set-up environment: wimboot with the boot files of the ISO and the two files of this package. The same script
// serves UEFI and legacy BIOS PCs.
func IPXE(s Settings) string {
	var b strings.Builder
	line := func(f string, a ...any) { fmt.Fprintf(&b, f+"\n", a...) }
	// wimboot learns a file's name from the image name on a UEFI PC (--name) and from the image's command line on a BIOS PC (the word after the address):
	// both are given, so that one script serves both.
	file := func(name, url string, a ...any) { line("initrd --name %s %s %s", name, fmt.Sprintf(url, a...), name) }
	line("#!ipxe")
	line("kernel %s/ipxe/wimboot", s.Base)
	line("iseq ${platform} efi && goto uefi || goto bios")
	line(":uefi")
	file("bootmgfw.efi", "%s/efi/boot/bootx64.efi", s.FS)
	file("BCD", "%s/efi/microsoft/boot/bcd", s.FS)
	line("goto common")
	line(":bios")
	file("bootmgr", "%s/bootmgr", s.FS)
	file("BCD", "%s/boot/bcd", s.FS)
	line(":common")
	file("boot.sdi", "%s/boot/boot.sdi", s.FS)
	if s.PatchedWIM {
		file("boot.wim", "%s/boot.wim", s.Win)
	} else {
		file("boot.wim", "%s/sources/boot.wim", s.FS)
	}
	if !s.PEOnly { // a PE that is not an installer keeps its own start-up
		file("winpeshl.ini", "%s/winpeshl.ini", s.Win)
		file("bootlace.cmd", "%s/bootlace.cmd", s.Win)
	}
	line("boot")
	return b.String()
}

// ShellIni tells WinPE to run the command file instead of its own set-up launcher, with a command window so that what happens can be read.
func ShellIni() string {
	return `[LaunchApps]` + "\r\n" + `%SYSTEMROOT%\System32\cmd.exe, /k %SYSTEMROOT%\System32\bootlace.cmd` + "\r\n"
}

// Cmd is the command file WinPE runs: start the network, connect to the ISO's folder on the share (trying again for a while, the network card
// takes a moment) and start Windows Setup from it.
func Cmd(s Settings) string {
	var b strings.Builder
	line := func(f string, a ...any) { fmt.Fprintf(&b, f+"\r\n", a...) }
	raw := func(t string) { b.WriteString(strings.ReplaceAll(t, "@ID@", s.Folder) + "\r\n") }
	port := ""
	if s.Port != 0 && s.Port != 445 {
		port = fmt.Sprintf(" /TCPPORT:%d", s.Port)
	}
	connect := fmt.Sprintf(`net use Z: \\%s\%s /user:%s %s%s`, s.Host, s.Share, s.User, s.Password, port)
	line("@echo off")
	line("title Gola Bootlace")
	// The drivers are loaded twice: before wpeinit, so that a disk or a card that Windows does not know is there when the network and Setup start, and again after it. On a real PC (an ASUS
	// with an Intel I219 card) the first load left the card without a driver, while the same load run by hand after wpeinit gave it a driver and an address. Loading a driver that is
	// already loaded is harmless. What drvload says is kept in a file that the failure branch shows: a driver that Windows PE refuses is otherwise invisible.
	loadDrivers := func(what string) {
		if s.Drivers {
			raw(`if exist "%SystemDrive%\$WinPEDriver$\nul" echo Loading the drivers` + what + ` ...`)
			raw(`if exist "%SystemDrive%\$WinPEDriver$\nul" for /r "%SystemDrive%\$WinPEDriver$" %%f in (*.inf) do drvload "%%f" >> "%SystemDrive%\drvload.log" 2>&1`)
		}
	}
	loadDrivers("")
	line("echo Gola Bootlace: starting the network ...")
	line("wpeinit")
	loadDrivers(" again")
	line("set tries=0")
	line(":try")
	line("echo Connecting to the installation files on %s ...", s.Host)
	line(connect + " > nul 2>&1")
	line("if not errorlevel 1 goto up")
	line("set /a tries+=1")
	line("if %%tries%% geq 40 goto failed")
	line("ping -n 4 127.0.0.1 > nul")
	line("goto try")
	line(":failed")
	line("echo The installation files could not be reached. The last answer was:")
	line(connect)
	line("echo.")
	line("ipconfig /all")
	// no adapter in that list usually means that Windows PE has no driver for the network card: name the devices it has no driver for (the PCI\VEN_ and DEV_ numbers
	// are what a driver is looked up by) and say where a driver package goes
	line("echo.")
	line("echo Devices that Windows has no driver for ^(the network card has Ethernet or Network in its name, its PCI\\VEN_xxxx^&DEV_xxxx numbers say which chip it is^):")
	line(`pnputil /enum-devices /problem | find /i "Instance ID"`) // (find: findstr is not part of Windows PE)
	if s.Drivers {
		line("echo.")
		line("echo Drivers that Windows refused to load ^(none listed: all loaded^):")
		line(`if exist "%%SystemDrive%%\drvload.log" type "%%SystemDrive%%\drvload.log" | find /i "Unable"`)
	}
	line("echo.")
	// (< and > are redirections for the command interpreter: they are escaped)
	line(`echo If the network card is there, put its driver ^(the folder with the .inf, .sys and .cat files^) in the library, in drivers\all\^<name^>\, and start this PC again.`)
	line("goto end")
	line(":up")
	line("echo Starting Windows Setup ...")
	if s.Hold {
		line("echo Held: start Setup by hand.")
		line("goto end")
	}
	// Setup is the one of the set-up environment itself (X:\sources), pointed at the installation image on the share and started from the ISO's
	// folder (without that it cannot find the licence terms): the setup.exe on the share is the one for upgrading a running Windows and does nothing here.
	// A PC that the firmware's own boot manager started over the network leaves a record of that in the registry, and Windows 11 Setup then starts in "WDS mode"
	// (Windows Deployment Services client), which Microsoft has blocked for Windows 11 ("WdsClient: There was a problem initializing WDS Mode"). Without the record
	// Setup starts as it does from a DVD.
	line(`reg delete HKLM\SYSTEM\CurrentControlSet\Control\PXE /f > nul 2>&1`)
	for _, name := range s.Bypass {
		line(`reg add HKLM\SYSTEM\Setup\LabConfig /v %s /t REG_DWORD /d 1 /f > nul 2>&1`, name)
	}
	line(`cd /d Z:\%s`, s.Folder)
	line(`set IMAGE=Z:\%s\sources\install.wim`, s.Folder)
	line(`if not exist %%IMAGE%% set IMAGE=Z:\%s\sources\install.esd`, s.Folder)
	line(`set OPTIONS=/installfrom:%%IMAGE%%`)
	// The answer file of this PC (issue #17). The set-up environment has no web client, so the file is read from the share: it shows `_bootlace\<MAC>\autounattend.xml` in the
	// ISO's folder while one is attached to the PC or to the ISO (made for that PC when it is read). The MAC addresses are read from `ipconfig /all`, whatever the language of
	// Windows (every line that holds a dash is looked at, and only a word of the shape aa-bb-cc-dd-ee-ff is tried). A file attached here wins over one in the ISO's own folder.
	raw(`set UNATTEND=`)
	raw(`for /f "tokens=1,* delims=:" %%a in ('ipconfig /all ^| find "-"') do call :answer %%b`)
	raw(`if defined UNATTEND goto haveanswer`)
	raw(`if exist Z:\@ID@\autounattend.xml set UNATTEND=Z:\@ID@\autounattend.xml`)
	raw(`:haveanswer`)
	raw(`if defined UNATTEND set OPTIONS=%OPTIONS% /unattend:%UNATTEND%`)
	line(`%%SystemDrive%%\sources\setup.exe %%OPTIONS%%`)
	line(":end")
	raw(`goto :eof`)
	raw(`:answer`)
	raw(`if "%~1"=="" exit /b 0`)
	raw(`set cand=%~1`)
	for _, at := range []string{"2", "5", "8", "11", "14"} {
		raw(`if not "%cand:~` + at + `,1%"=="-" exit /b 0`)
	}
	raw(`if not "%cand:~17%"=="" exit /b 0`)
	raw(`if defined UNATTEND exit /b 0`)
	raw(`copy Z:\@ID@\_bootlace\%cand%\autounattend.xml %SystemDrive%\autounattend.xml > nul 2>&1`)
	raw(`if not errorlevel 1 set UNATTEND=%SystemDrive%\autounattend.xml`)
	raw(`exit /b 0`)
	return b.String()
}
