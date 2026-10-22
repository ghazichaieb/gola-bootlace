// Disk images (.img, .raw and their compressed forms) are shown to a PC as a hard disk over iSCSI; a compressed one is unpacked into the cache folder first, in the background.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package httpd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ghazichaieb/gola-bootlace/internal/diskimg"
	"github.com/ghazichaieb/gola-bootlace/internal/family"
	"github.com/ghazichaieb/gola-bootlace/internal/isos"
	"github.com/ghazichaieb/gola-bootlace/internal/menu"
)

// diskState is how far a disk image is (an image that is not compressed is ready at once).
func (s *Server) diskState(iso isos.ISO) diskimg.State {
	s.mu.Lock()
	if s.disks == nil {
		dir := s.CacheDir
		if dir == "" {
			dir = filepath.Join(os.TempDir(), "bootlace-cache")
		}
		s.disks = &diskimg.Manager{Dir: filepath.Join(dir, "images"), Logf: s.logf}
	}
	m := s.disks
	s.mu.Unlock()
	return m.Status(iso)
}

// diskEntry is the menu line of a disk image.
func (s *Server) diskEntry(iso isos.ISO, title, size string) (display string, e menu.Entry, known bool, b family.Boot) {
	st := s.diskState(iso)
	switch {
	case st.Err != nil:
		display = fmt.Sprintf("%s  [disk image, cannot be unpacked, %s]", title, size)
		b.Family = "Disk image: cannot be unpacked"
		e = menu.Entry{Name: display, Title: title, Kind: "disk image", Size: size, Commands: []string{
			"echo This disk image could not be unpacked on the server. See the log of the server. && sleep 8 && exit",
		}}
	case !st.Ready:
		display = fmt.Sprintf("%s  [disk image, unpacking %d %%, %s]", title, st.Progress, size)
		b.Family = fmt.Sprintf("Disk image: unpacking %d %%", st.Progress)
		e = menu.Entry{Name: display, Title: title, Kind: "disk image", Size: size, Commands: []string{
			"echo This disk image is still being unpacked on the server. Try again in a minute. && sleep 8 && exit",
		}}
	case s.iscsiURL(iso) == "":
		display = fmt.Sprintf("%s  [disk image, needs the iSCSI server, %s]", title, size)
		b.Family = "Disk image: needs the iSCSI server"
		e = menu.Entry{Name: display, Title: title, Kind: "disk image", Size: size, Commands: []string{
			"echo Disk images need the iSCSI server of Bootlace, which is switched off. && sleep 8 && exit",
		}}
	default:
		display = fmt.Sprintf("%s  [disk image, %s]", title, size)
		b.Family = "Disk image (hard disk)"
		e = menu.Entry{Name: display, Title: title, Kind: "disk image", Size: size, Commands: []string{
			"echo Starting " + oneWord(title) + " as a hard disk over the network ...",
			"sanboot --no-describe --drive 0x80 " + s.iscsiURL(iso),
		}}
	}
	return display, e, true, b
}

// loadingNote is what the menu says before an ISO that Bootlace does not know is handed to the PC as a disc.
func (s *Server) loadingNote(iso isos.ISO, title string) string {
	if s.iscsiURL(iso) != "" {
		return "echo Starting " + oneWord(title) + " as a CD-ROM over the network ..."
	}
	return "echo Loading " + oneWord(title) + " ... the whole ISO is read into memory first, this takes a while"
}
