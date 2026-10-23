// Package speedtest measures how fast a folder can be written and read: the library and the container's own cache folder. A library on a slow network mount is the usual
// reason an install is slow, and the numbers say how many installs it can carry at once.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package speedtest

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/ghazichaieb/gola-bootlace/internal/library"
)

// Result is what one folder measured. Speeds are in MB (10^6 bytes) a second.
type Result struct {
	Dir          string  `json:"dir"`
	Bytes        int64   `json:"bytes"`        // the size of the test file
	WriteMBps    float64 `json:"writeMBps"`    // written and flushed to the disk
	ReadMBps     float64 `json:"readMBps"`     // read back
	CacheDropped bool    `json:"cacheDropped"` // the operating system's file cache was emptied before the read (so the read speed is the disk's, not memory's)
	Installs     int     `json:"installs"`     // how many installs at once the read speed carries at InstallMBps each
	Error        string  `json:"error,omitempty"`
}

// InstallMBps is what one install is counted as needing while it reads its ISO: a modest figure for a Windows or Linux set-up over a network.
const InstallMBps = 25.0

const (
	chunk   = 1 << 20
	minSize = 16 << 20
	limit   = 25 * time.Second // for the write and for the read, each
)

// MaxSize is the largest test file (a test sets it lower).
var MaxSize int64 = 512 << 20

// Size is the size of the test file for a folder with this much room: 512 MB at most and a tenth of the room at most (under 16 MB the test is refused).
func Size(free uint64) int64 {
	n := int64(free / 10)
	if n > MaxSize {
		n = MaxSize
	}
	n = n / chunk * chunk
	return n
}

// Measure writes a test file of about size bytes in dir (a folder that exists), reads it back and removes it. It stops at the time limit and then reports what it
// reached; the figures are for the part that was done.
func Measure(ctx context.Context, dir string, size int64) (Result, error) {
	r := Result{Dir: dir}
	if size < minSize {
		return r, fmt.Errorf("not enough room for a test: %d MB would be tested, at least %d MB are needed", size>>20, minSize>>20)
	}
	f, err := os.CreateTemp(dir, ".speedtest-*")
	if err != nil {
		return r, err
	}
	name := f.Name()
	defer os.Remove(name)

	buf := make([]byte, chunk)
	fill(buf)
	start := time.Now()
	var written int64
	for written < size && time.Since(start) < limit && ctx.Err() == nil {
		n, err := f.Write(buf)
		written += int64(n)
		if err != nil {
			f.Close()
			return r, err
		}
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return r, err
	}
	took := time.Since(start)
	f.Close()
	if err := ctx.Err(); err != nil {
		return r, err
	}
	r.Bytes, r.WriteMBps = written, mbps(written, took)

	r.CacheDropped = dropCaches()
	in, err := os.Open(name)
	if err != nil {
		return r, err
	}
	defer in.Close()
	start = time.Now()
	var read int64
	for read < written && time.Since(start) < limit && ctx.Err() == nil {
		n, err := in.Read(buf)
		read += int64(n)
		if err != nil {
			break
		}
	}
	r.ReadMBps = mbps(read, time.Since(start))
	r.Installs = int(r.ReadMBps / InstallMBps)
	return r, ctx.Err()
}

func mbps(n int64, d time.Duration) float64 {
	if d <= 0 {
		return 0
	}
	return float64(n) / 1e6 / d.Seconds()
}

// fill makes data that does not compress (a network mount or a disk that compresses would otherwise report a speed no real file gets).
func fill(b []byte) {
	x := uint64(0x9E3779B97F4A7C15)
	for i := 0; i+8 <= len(b); i += 8 {
		x ^= x << 13
		x ^= x >> 7
		x ^= x << 17
		for j := 0; j < 8; j++ {
			b[i+j] = byte(x >> (8 * j))
		}
	}
}

// dropCaches empties the operating system's file cache where that can be done (Linux, as root): without it a file just written is read back from memory.
func dropCaches() bool {
	return os.WriteFile("/proc/sys/vm/drop_caches", []byte("1"), 0o200) == nil
}

// Folder measures the folder dir, choosing the size from its free space.
func Folder(ctx context.Context, dir string) (Result, error) {
	st, err := os.Stat(dir)
	if err != nil || !st.IsDir() {
		return Result{Dir: dir}, errors.New("the folder is not there")
	}
	free, total, err := library.DiskSpace(dir)
	if err != nil {
		free = uint64(MaxSize) * 10 // the disk cannot say: the write fails by itself if there is no room
	} else if r := library.Reserve(total); free > r {
		free -= r
	} else {
		free = 0
	}
	return Measure(ctx, dir, Size(free))
}

// Summary is a sentence for the person.
func (r Result) Summary() string {
	if r.Error != "" {
		return r.Error
	}
	return fmt.Sprintf("writes %.0f MB/s, reads %.0f MB/s: about %d install(s) at once at %.0f MB/s each", r.WriteMBps, r.ReadMBps, r.Installs, InstallMBps)
}
