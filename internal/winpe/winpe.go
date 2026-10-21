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
	Share    string // the SMB share that shows the ISO
	User     string
	Password string
	Port     int // SMB port (445 unless Windows cannot reach the server there)
}

// IPXE is the script that loads the set-up environment: wimboot with the boot files of the ISO and the two files of this package. The same script
// serves UEFI and legacy BIOS PCs.
func IPXE(s Settings) string {
	var b strings.Builder
	line := func(f string, a ...any) { fmt.Fprintf(&b, f+"\n", a...) }
	line("#!ipxe")
	line("kernel %s/ipxe/wimboot", s.Base)
	line("iseq ${platform} efi && goto uefi || goto bios")
	line(":uefi")
	line("initrd --name bootmgfw.efi %s/efi/boot/bootx64.efi", s.FS)
	line("initrd --name BCD %s/efi/microsoft/boot/bcd", s.FS)
	line("goto common")
	line(":bios")
	line("initrd --name bootmgr %s/bootmgr", s.FS)
	line("initrd --name BCD %s/boot/bcd", s.FS)
	line(":common")
	line("initrd --name boot.sdi %s/boot/boot.sdi", s.FS)
	line("initrd --name boot.wim %s/sources/boot.wim", s.FS)
	line("initrd --name winpeshl.ini %s/winpeshl.ini", s.Win)
	line("initrd --name bootlace.cmd %s/bootlace.cmd", s.Win)
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
	line(`Z:\setup.exe`)
	line(":end")
	return b.String()
}
