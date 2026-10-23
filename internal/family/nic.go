// Network card drivers for start-up disks that have none. A start-up disk made for a disc or a USB stick carries only what those need; the full set of kernel modules is in the
// system image next to it (a squashfs). The common wired drivers are read from there, unpacked, and added to the extra archive, which loads them with the helper's "insmod".
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package family

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/ghazichaieb/gola-bootlace/internal/initread"
)

// nicDrivers reads the common network card drivers, and what they need, for the kernel of that version from the system image at the path in the ISO's own files. With no
// path, the ISO's squashfs files are tried, the biggest first, for the one that has the modules of that kernel.
func (c *Context) nicDrivers(image, version string) ([]initread.Module, error) {
	if image != "" {
		return c.driversIn(image, version)
	}
	for _, img := range c.systemImages() {
		if mods, err := c.driversIn(img, version); err == nil {
			return mods, nil
		}
	}
	return nil, errors.New("the network drivers: no system image of this ISO has the modules of the kernel " + version)
}

func (c *Context) driversIn(image, version string) ([]initread.Module, error) {
	sq, err := c.FS.Open(image)
	if err != nil {
		return nil, err
	}
	mods, err := initread.Modules(sq, version, initread.NICModules)
	if err != nil {
		return nil, fmt.Errorf("the network drivers: %w", err)
	}
	return mods, nil
}

// systemImages are the files of the ISO that can be system images (squashfs), the biggest first: they are looked for in the top folders only.
func (c *Context) systemImages() []string {
	type sized struct {
		name string
		size int64
	}
	var found []sized
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		names, err := c.FS.List(dir)
		if err != nil {
			return
		}
		for _, n := range names {
			p := strings.TrimRight(dir, "/") + "/" + strings.TrimSuffix(n, "/")
			if strings.HasSuffix(n, "/") {
				if depth < 3 {
					walk(p, depth+1)
				}
				continue
			}
			l := strings.ToLower(n)
			if strings.HasSuffix(l, ".squashfs") || strings.HasSuffix(l, ".sfs") || strings.HasSuffix(l, ".sqfs") || strings.HasPrefix(l, "modloop") {
				if f, err := c.FS.Open(p); err == nil {
					found = append(found, sized{p, f.Size()})
				}
			}
		}
	}
	walk("/", 0)
	sort.Slice(found, func(i, j int) bool { return found[i].size > found[j].size })
	var out []string
	for _, f := range found {
		if len(out) < 6 {
			out = append(out, f.name)
		}
	}
	return out
}
