// Putting Bootlace's two files into a copy of the ISO's boot.wim. The Windows boot manager that the PC's own firmware starts (the only way to boot Windows with UEFI
// Secure Boot on) loads the boot.wim as it is; wimboot, which adds files while loading, cannot be used there. So the files go into the WIM beforehand, with wimlib.
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package winpe

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"time"
)

// Wimlib finds wimlib-imagex: the path given, or the one on the PATH. Empty when there is none.
func Wimlib(given string) string {
	if given != "" {
		return given
	}
	if p, err := exec.LookPath("wimlib-imagex"); err == nil {
		return p
	}
	return ""
}

var imageCount = regexp.MustCompile(`(?m)^Image Count:\s+(\d+)`)

// PatchBootWIM copies the boot.wim read from src to dst and adds the files (path inside the WIM, with forward slashes -> content) and the folders (path inside the WIM -> folder on disk,
// copied with everything in it) to every image of it.
// dst is written under another name and renamed when it is whole, so that a half-made file is never used.
func PatchBootWIM(wimlib string, src io.Reader, dst string, files map[string][]byte, dirs map[string]string) error {
	if wimlib == "" {
		return errors.New("wimlib-imagex is not installed")
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return err
	}
	tmp := dst + ".part"
	defer os.Remove(tmp)
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, src); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	info, err := exec.CommandContext(ctx, wimlib, "info", tmp).Output()
	if err != nil {
		return fmt.Errorf("wimlib-imagex info: %w", err)
	}
	m := imageCount.FindSubmatch(info)
	if m == nil {
		return errors.New("the boot.wim lists no images")
	}
	n, _ := strconv.Atoi(string(m[1]))
	dir, err := os.MkdirTemp("", "bootlace-wim-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	var script bytes.Buffer
	i := 0
	for name, data := range files {
		i++
		local := filepath.Join(dir, fmt.Sprintf("f%d", i))
		if err := os.WriteFile(local, data, 0o600); err != nil {
			return err
		}
		fmt.Fprintf(&script, "add '%s' '%s'", filepath.ToSlash(local), name)
		script.WriteByte(10)
	}
	dirNames := make([]string, 0, len(dirs))
	for name := range dirs {
		dirNames = append(dirNames, name)
	}
	sort.Strings(dirNames)
	for _, name := range dirNames {
		fmt.Fprintf(&script, "add '%s' '%s'", filepath.ToSlash(dirs[name]), name)
		script.WriteByte(10)
	}
	for image := 1; image <= n; image++ {
		cmd := exec.CommandContext(ctx, wimlib, "update", tmp, strconv.Itoa(image))
		cmd.Stdin = bytes.NewReader(script.Bytes())
		if outb, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("wimlib-imagex update (image %d): %w: %s", image, err, bytes.TrimSpace(outb))
		}
	}
	return os.Rename(tmp, dst)
}
