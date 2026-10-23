// Disk images (.img, .raw and their compressed forms) are shown to a PC as a hard disk over iSCSI; a compressed one is unpacked into the cache folder first, in the background.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package httpd

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/ghazichaieb/gola-bootlace/internal/diskimg"
	"github.com/ghazichaieb/gola-bootlace/internal/family"
	"github.com/ghazichaieb/gola-bootlace/internal/isos"
	"github.com/ghazichaieb/gola-bootlace/internal/menu"
)

// diskState is how far a disk image is (an image that is not compressed is ready at once); it never starts unpacking.
func (s *Server) diskState(iso isos.ISO) diskimg.State { return s.diskManager().Status(iso) }

// scan lists the ISO folder like isos.Scan, but a packed file that has been unpacked is listed as the file in the cache folder (so that everything else treats it as an
// ordinary ISO or disk image); one that has not stays as it is, with Packed set.
func (s *Server) scan() ([]isos.ISO, error) {
	list, err := isos.Scan(s.isoDir())
	if err != nil {
		return nil, err
	}
	for i, iso := range list {
		if iso.Packed == "" {
			continue
		}
		if st := s.diskManager().Status(iso); st.Ready {
			list[i].Path, list[i].Size, list[i].Packed, list[i].Inner = st.Path, st.Size, "", ""
		}
	}
	return list, nil
}

// diskManager is the unpacker of compressed images.
func (s *Server) diskManager() *diskimg.Manager {
	s.mu.Lock()
	if s.disks == nil {
		dir := s.localCache()
		s.disks = &diskimg.Manager{Dir: filepath.Join(dir, "images"), Logf: s.logf, Used: touchCache,
			Place: func(need int64) string { return filepath.Join(s.cachePlace(need), "images") },
			Also: func() []string {
				var d []string
				if lib := s.libraryCache(); lib != "" {
					d = append(d, filepath.Join(lib, "images"))
				}
				return d
			}}
	}
	m := s.disks
	s.mu.Unlock()
	return m
}

// unpack is /unpack/<id>: a PC that chose a packed disk image asks for it to be unpacked (iPXE fetches this address, then says so on the screen).
func (s *Server) unpack(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/unpack/")
	list, _ := isos.Scan(s.isoDir()) // (as found in the folder: the file that is packed)
	for _, iso := range list {
		if iso.ID == id && iso.Packed != "" {
			st := s.diskManager().Start(iso)
			if !st.Started && !st.Ready {
				http.Error(w, "cannot unpack", http.StatusInternalServerError)
				return
			}
			s.logf("%s asks for %s to be unpacked", r.RemoteAddr, iso.Title)
			fmt.Fprintln(w, "unpacking")
			return
		}
	}
	http.NotFound(w, r)
}

// diskEntry is the menu line of a disk image, or of a packed file that is not unpacked yet.
func (s *Server) diskEntry(iso isos.ISO, title, size string) (display string, e menu.Entry, known bool, b family.Boot) {
	st := s.diskState(iso)
	noun := "disk image"
	if !iso.Disk {
		noun = "ISO"
	}
	switch {
	case st.Err != nil:
		display = fmt.Sprintf("%s  ["+noun+", cannot be unpacked, %s]", title, size)
		b.Family = "Packed " + noun + ": cannot be unpacked"
		e = menu.Entry{Name: display, Title: title, Kind: noun, Size: size, Commands: []string{
			"echo This " + noun + " could not be unpacked on the server. See the log of the server. && sleep 8 && goto again",
		}}
	case !st.Ready && !st.Started:
		display = fmt.Sprintf("%s  ["+noun+", packed, unpacks when started, %s]", title, size)
		b.Family = "Packed " + noun + ": packed, unpacks when started"
		e = menu.Entry{Name: display, Title: title, Kind: noun, Size: size, Commands: []string{
			"imgfetch " + s.Base + "/unpack/" + iso.ID + " && echo This " + noun + " is being unpacked on the server (it takes a few minutes). Come back to this menu later. && sleep 10 && goto again",
		}}
	case !st.Ready:
		display = fmt.Sprintf("%s  ["+noun+", unpacking %d %%, %s]", title, st.Progress, size)
		b.Family = fmt.Sprintf("Packed "+noun+": unpacking %d %%", st.Progress)
		e = menu.Entry{Name: display, Title: title, Kind: noun, Size: size, Commands: []string{
			"echo This " + noun + " is still being unpacked on the server. Try again in a minute. && sleep 8 && goto again",
		}}
	case s.iscsiURL(iso) == "":
		display = fmt.Sprintf("%s  ["+noun+", needs the iSCSI server, %s]", title, size)
		b.Family = "Packed " + noun + ": needs the iSCSI server"
		e = menu.Entry{Name: display, Title: title, Kind: noun, Size: size, Commands: []string{
			"echo Disk images need the iSCSI server of Bootlace, which is switched off. && sleep 8 && goto again",
		}}
	default:
		display = fmt.Sprintf("%s  ["+noun+", %s]", title, size)
		b.Family = "Disk image (hard disk)"
		e = menu.Entry{Name: display, Title: title, Kind: noun, Size: size, Commands: []string{
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
