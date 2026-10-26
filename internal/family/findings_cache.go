// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package family

import (
	"encoding/json"
	"errors"
	"strings"
	"unicode"
)

// Persist only inspected facts, never generated boot commands: those contain the
// current server addresses, and a Boot may carry a non-serializable ExtraBuilder.
// Rebuilding from facts reconstructs the builder against the current ISO tree.
const maxSavedFindings = 128
const maxSavedFindingsBytes = 1 << 20

type savedFinding struct {
	Flavor                                        flavor `json:"flavor"`
	Version                                       string `json:"kernelVersion"`
	NIC, NBD, ArchisoNet, Netroot, ISCSI, Livenet bool
}

type savedFindings struct {
	Format int                     `json:"format"`
	Stamp  string                  `json:"stamp"`
	Facts  map[string]savedFinding `json:"facts"`
}

// MarshalStamp snapshots only the findings belonging to this version of one ISO.
// stamp must identify its path, size and modification time, as Context.Stamp does.
func (m *Findings) MarshalStamp(stamp string) ([]byte, error) {
	out := savedFindings{Format: 1, Stamp: stamp, Facts: map[string]savedFinding{}}
	m.mu.Lock()
	for key, f := range m.facts {
		if strings.HasPrefix(key, stamp+"|") {
			out.Facts[key] = savedFinding{f.flavor, f.version, f.nicDrivers, f.nbdDriver, f.archisoNet, f.netroot, f.iscsi, f.livenet}
		}
	}
	m.mu.Unlock()
	if len(out.Facts) > maxSavedFindings {
		return nil, errors.New("too many saved boot findings")
	}
	data, err := json.Marshal(out)
	if len(data) > maxSavedFindingsBytes {
		return nil, errors.New("saved boot findings are too large")
	}
	return data, err
}

// RestoreStamp validates a complete snapshot before installing any of it. A cache
// from another ISO version or schema is a miss, never a source of boot commands.
func (m *Findings) RestoreStamp(stamp string, data []byte) error {
	if len(data) > maxSavedFindingsBytes {
		return errors.New("saved boot findings are too large")
	}
	var saved savedFindings
	if err := json.Unmarshal(data, &saved); err != nil {
		return err
	}
	if saved.Format != 1 || saved.Stamp != stamp || len(saved.Facts) > maxSavedFindings {
		return errors.New("incompatible saved boot findings")
	}
	decoded := map[string]facts{}
	for key, f := range saved.Facts {
		if !strings.HasPrefix(key, stamp+"|") || len(key) > 4096 || len(f.Version) > 256 || strings.IndexFunc(f.Version, unicode.IsControl) >= 0 {
			return errors.New("invalid saved boot finding")
		}
		switch f.Flavor {
		case flavorUnknown, flavorCasper, flavorLiveBoot, flavorArchiso, flavorDracut:
		default:
			return errors.New("unknown saved boot strategy")
		}
		decoded[key] = facts{f.Flavor, f.Version, f.NIC, f.NBD, f.ArchisoNet, f.Netroot, f.ISCSI, f.Livenet}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.facts == nil {
		m.facts = map[string]facts{}
	}
	for key, f := range decoded {
		m.facts[key] = f
	}
	return nil
}

// CachedOnly is an isolated snapshot for rebuilding a start synchronously. A
// missing fact returns false instead of reading an entire initramfs unexpectedly.
func (m *Findings) CachedOnly() *Findings {
	out := &Findings{facts: map[string]facts{}, cachedOnly: true}
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, f := range m.facts {
		out.facts[key] = f
	}
	return out
}
