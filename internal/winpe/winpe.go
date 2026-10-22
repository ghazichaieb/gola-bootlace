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
	file("boot.wim", "%s/sources/boot.wim", s.FS)
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
	port := ""
	if s.Port != 0 && s.Port != 445 {
		port = fmt.Sprintf(" /TCPPORT:%d", s.Port)
	}
	connect := fmt.Sprintf(`net use Z: \\%s\%s /user:%s %s%s`, s.Host, s.Share, s.User, s.Password, port)
	line("@echo off")
	line("title Gola Bootlace")
	line("echo Gola Bootlace: starting the network ...")
	line("wpeinit")
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
	line(`cd /d Z:\%s`, s.Folder)
	line(`set IMAGE=Z:\%s\sources\install.wim`, s.Folder)
	line(`if not exist %%IMAGE%% set IMAGE=Z:\%s\sources\install.esd`, s.Folder)
	line(`set OPTIONS=/installfrom:%%IMAGE%%`)
	line(`if exist Z:\%s\autounattend.xml set OPTIONS=%%OPTIONS%% /unattend:Z:\%s\autounattend.xml`, s.Folder, s.Folder)
	line(`%%SystemDrive%%\sources\setup.exe %%OPTIONS%%`)
	line(":end")
	return b.String()
}
