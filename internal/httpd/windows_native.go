// Windows started by the PC's own firmware, the way Windows Deployment Services does it: the firmware fetches Microsoft's boot manager by TFTP, and the
// boot manager fetches its boot configuration (BCD), boot.sdi and boot.wim from the same server. Every file of that chain is signed by Microsoft, so this
// works with UEFI Secure Boot on, which iPXE and wimboot (not signed by anyone the firmware knows) cannot. The boot.wim is a copy of the ISO's with
// Bootlace's two files added (see package winpe), made once with wimlib and kept in the cache folder.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package httpd

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/ghazichaieb/gola-bootlace/internal/config"
	"github.com/ghazichaieb/gola-bootlace/internal/disc"
	"github.com/ghazichaieb/gola-bootlace/internal/handoff"
	"github.com/ghazichaieb/gola-bootlace/internal/isos"
	"github.com/ghazichaieb/gola-bootlace/internal/smbshare"
	"github.com/ghazichaieb/gola-bootlace/internal/winpe"
)

// windowsName turns what the boot manager asks for into the path of the file inside the ISO: it asks with backslashes and in any case, and the media's
// own BCD for UEFI is not where a PXE boot manager looks.
func windowsName(name string) string {
	n := strings.ToLower(strings.TrimLeft(strings.ReplaceAll(name, `\`, "/"), "/"))
	switch n {
	case "bootmgfw.efi":
		return "efi/boot/bootx64.efi"
	case "boot/bcd", "boot/x64/default.bcd", "boot/default.bcd":
		return "efi/microsoft/boot/bcd"
	}
	return n
}

// windowsPEOnly says whether the ISO is a Windows PE with no installation image (Bootlace then adds nothing to it).
func (s *Server) windowsPEOnly(iso isos.ISO) bool {
	b, ok := s.recognise(iso)
	return ok && b.Family == "Windows PE"
}

// winSettings are the settings of the files that make Windows' set-up environment find this ISO.
func (s *Server) winSettings(iso isos.ISO) (winpe.Settings, error) {
	u, err := url.Parse(s.Base)
	if err != nil {
		return winpe.Settings{}, err
	}
	set := winpe.Settings{Base: s.Base, FS: s.Base + "/fs/" + iso.ID, Win: s.Base + "/w/" + iso.ID, Host: u.Hostname(), Share: smbshare.ShareName, Folder: iso.ID,
		User: "bootlace", Password: s.SMBPassword, Port: s.SMBPort, Hold: os.Getenv("BOOTLACE_WINPE_HOLD") != "", Bypass: s.settings().SetupChecks.LabConfig()}
	if use, _, _, err := s.driverPlan(iso); err != nil {
		s.logf("drivers for %s are not used: %v", iso.Title, err)
	} else if len(use) > 0 {
		if winpe.Wimlib(s.Wimlib) == "" {
			s.logf("drivers for %s are not used: wimlib-imagex is not installed (on Debian or Ubuntu: apt install wimtools), and the copy of boot.wim that holds them is made with it", iso.Title)
		} else {
			set.Drivers, set.PatchedWIM = true, true
		}
	}
	return set, nil
}

// driverPlan is what the drivers folder of the library adds to the boot.wim copy of this ISO (nothing unless the setting is on and a folder holds driver packages).
func (s *Server) driverPlan(iso isos.ISO) (use []winpe.Set, print string, bytes int64, err error) {
	if !s.settings().SetupChecks.Drivers {
		return nil, "", 0, nil
	}
	return winpe.Plan(winpe.SetsFor(s.isoDir(), iso.Name))
}

// patchedBootWIM is the copy of the ISO's boot.wim with Bootlace's files in it, made when it is first needed. The file's name says what it was made for (the
// ISO, this server's address and share password), so a change of any of them makes a new one.
func (s *Server) patchedBootWIM(iso isos.ISO) (string, error) {
	set, err := s.winSettings(iso)
	if err != nil {
		return "", err
	}
	cmd := winpe.Cmd(set)
	ini := winpe.ShellIni()
	use, drvPrint, drvBytes, derr := s.driverPlan(iso)
	if derr != nil {
		use, drvPrint, drvBytes = nil, "", 0 // (winSettings has said so; the copy is then made without them, as its command file expects)
	}
	sum := sha1.Sum([]byte(strings.Join([]string{iso.Path, fmt.Sprint(iso.Size), iso.ModTime.String(), cmd, ini, drvPrint}, "\x00")))
	name := fmt.Sprintf("boot-%s-%s.wim", iso.ID, hex.EncodeToString(sum[:])[:10])
	s.patchMu.Lock()
	defer s.patchMu.Unlock()
	if p, ok := s.cacheFind(name); ok { // (in the container's own cache folder or in the library's)
		return p, nil
	}
	lib := winpe.Wimlib(s.Wimlib)
	if lib == "" {
		return "", fmt.Errorf("wimlib-imagex is not installed (on Debian or Ubuntu: apt install wimtools)")
	}
	f, err := os.Open(iso.Path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	img, err := disc.Open(f, iso.Size)
	if err != nil {
		return "", err
	}
	wim, err := img.Open("sources/boot.wim")
	if err != nil {
		return "", fmt.Errorf("%s has no sources/boot.wim", iso.Title)
	}
	start := time.Now()
	dst := filepath.Join(s.cachePlace(wim.Size()+drvBytes+64<<20), name) // (the copy is about as big as the original)
	s.logf("making the boot.wim of %s with Bootlace's files in it (once), in %s ...", iso.Title, filepath.Dir(dst))
	files := map[string][]byte{"/Windows/System32/winpeshl.ini": []byte(ini), "/Windows/System32/bootlace.cmd": []byte(cmd)}
	dirs := map[string]string{}
	for _, d := range use {
		dirs[d.WIM] = d.Dir
	}
	if len(dirs) > 0 {
		s.logf("adding %d MB of drivers to it", drvBytes>>20)
	}
	if err := winpe.PatchBootWIM(lib, wim.Reader(), dst, files, dirs); err != nil {
		return "", err
	}
	for _, dir := range s.cacheDirs() { // the copies made for earlier settings
		old, _ := filepath.Glob(filepath.Join(dir, "boot-"+iso.ID+"-*.wim"))
		for _, o := range old {
			if o != dst {
				os.Remove(o)
			}
		}
	}
	s.logf("boot.wim of %s ready in %s", iso.Title, time.Since(start).Round(time.Second))
	return dst, nil
}

// windowsTFTP answers the boot manager's requests from the ISO the PC chose.
func (s *Server) windowsTFTP(client net.IP, id, name string) (fs.File, bool) {
	list, err := s.scan()
	if err != nil {
		return nil, false
	}
	want := windowsName(name)
	for _, iso := range list {
		if iso.ID != id {
			continue
		}
		if want == "sources/boot.wim" && !s.windowsPEOnly(iso) {
			p, err := s.patchedBootWIM(iso)
			if err != nil {
				s.logf("tftp: %s asks for the boot.wim of %s: %v", client, iso.Title, err)
				return nil, false
			}
			f, err := os.Open(p)
			if err != nil {
				return nil, false
			}
			s.logf("tftp: %s gets sources/boot.wim (with Bootlace's files) of %s", client, iso.Title)
			return f, true
		}
		f, err := os.Open(iso.Path)
		if err != nil {
			return nil, false
		}
		img, err := disc.Open(f, iso.Size)
		if err != nil {
			f.Close()
			return nil, false
		}
		file, err := img.Open(want)
		if err != nil {
			f.Close()
			s.logf("tftp: %s asks for %s: not in %s", client, name, iso.Title)
			return nil, false
		}
		s.logf("tftp: %s gets %s from %s", client, name, iso.Title)
		return &isoFile{ReadSeeker: file.Reader(), name: path.Base(want), size: file.Size(), closer: f}, true
	}
	return nil, false
}

// NativeBootFile is the boot file for a PC that a "native" rule sends to an ISO started by the firmware itself. The rule is used up when the file is first named
// (the PC that has installed Windows must not install it again at its next network boot), and the choice is kept for a while in the hand-off registry.
func (s *Server) NativeBootFile(mac string, efi bool) (string, bool) {
	if !efi || s.Config == nil || s.Handoff == nil {
		return "", false
	}
	m, ok := config.NormalizeMAC(mac)
	if !ok {
		return "", false
	}
	var id string
	for _, r := range s.Config.Get().Rules {
		if r.MAC == m && strings.HasPrefix(r.Action, "native:") {
			id = strings.TrimPrefix(r.Action, "native:")
		}
	}
	if id == "" {
		return "", false
	}
	list, err := s.entryInfos(s.settings())
	if err != nil {
		return "", false
	}
	for _, e := range list {
		if e.ISO.ID != id {
			continue
		}
		kind := ""
		switch e.Boot.Family {
		case "Windows setup", "Windows PE":
			kind = handoff.Windows
		case "Ubuntu (casper)":
			kind = handoff.Ubuntu
		default:
			s.logf("%s: the rule names %s, which cannot be started this way yet (only Windows set-up and Ubuntu can): showing the menu", m, e.Title)
			return "", false
		}
		_ = s.Config.Update(func(c *config.Config) error {
			for i, r := range c.Rules {
				if r.MAC == m && r.Action == "native:"+id {
					c.Rules = append(c.Rules[:i], c.Rules[i+1:]...)
					break
				}
			}
			return nil
		})
		s.Handoff.Pending(m, id, kind)
		s.grantAnswers(m)
		s.setAction(m, "native: "+e.Title)
		s.logf("%s: its rule starts %s with the firmware's own signed boot loaders (works with Secure Boot); the rule is used up", m, e.Title)
		return s.Handoff.BootFile(m, true)
	}
	s.logf("%s: its rule names an ISO that is not there any more", m)
	return "", false
}

// NoteDHCP lists a PC that asked for a boot file, even when it never gets as far as the menu.
func (s *Server) NoteDHCP(mac string, efi, ipxe bool) {
	m, ok := config.NormalizeMAC(mac)
	if !ok {
		return
	}
	now := s.now()
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
	c := s.clients[m]
	if c == nil {
		c = s.admitLocked(m, now) // bounded like the menu path: a flood of DHCP requests with made-up MAC addresses cannot grow the list
		c.LastAction = "asked for a boot file"
	}
	c.LastSeen = now
	c.note("asked for a boot file", now) // (so that a PC which never gets as far as the menu has a history to read)
	if c.Platform == "" {
		c.Platform = "pcbios"
		if efi {
			c.Platform = "efi"
		}
	}
	s.changed()
}
