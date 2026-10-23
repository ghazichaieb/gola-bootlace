// Network card drivers for start-up disks that have none. A start-up disk made for a disc or a USB stick carries only what those need; the full set of kernel modules is in the
// system image next to it (a squashfs). The common wired drivers are read from there, unpacked, and added to the extra archive, which loads them with the helper's "insmod".
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package family

import (
	"fmt"

	"github.com/ghazichaieb/gola-bootlace/internal/initread"
)

// nicDrivers reads the common network card drivers, and what they need, from the system image at path (in the ISO's own files) for the kernel of that version.
func (c *Context) nicDrivers(image, version string) ([]initread.Module, error) {
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
