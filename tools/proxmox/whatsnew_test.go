// Tests of the version comparison and the "what's new" list that bootlace-lxc.sh writes into the container's update command (and uses itself):
// the shell text is taken out of the script as it is and run with the system's sh and awk.
//
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package proxmox

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// sampleChangelog is a changelog with a release, its pre-release and older ones, in the shape of the real CHANGELOG.md.
const sampleChangelog = `# Changelog

## Unreleased

## 0.5.0 - 2026-10-29

- **Secure Boot is built in (#10).** More text that the list does not show.
- **Entries survive a restart (#70).** More text.

## 0.5.0-rc.1 - 2026-10-27

- **A prototype of the Secure Boot route.** More text.

## 0.4.2 - 2026-10-26

- **The update command renews itself.** More text.

## 0.4.1 - 2026-10-26

- **An older change.** More text.
`

// shellLibrary returns the text of the script between "<<'WHATSNEW'" and the line "WHATSNEW": the functions the update command gets.
func shellLibrary(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("bootlace-lxc.sh")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	_, rest, ok := strings.Cut(text, "<<'WHATSNEW'\n")
	if !ok {
		t.Fatal("no WHATSNEW here-document in bootlace-lxc.sh")
	}
	library, _, ok := strings.Cut(rest, "\nWHATSNEW\n")
	if !ok {
		t.Fatal("the WHATSNEW here-document is not closed")
	}
	return library + "\n"
}

// runShell runs script with the library loaded and returns what it printed; the test is skipped where there is no sh or awk.
func runShell(t *testing.T, script string) string {
	t.Helper()
	for _, tool := range []string{"sh", "awk"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("no %s on this computer", tool)
		}
	}
	dir := t.TempDir()
	lib := filepath.Join(dir, "whatsnew.sh")
	if err := os.WriteFile(lib, []byte(shellLibrary(t)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "CHANGELOG.md"), []byte(sampleChangelog), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", "-c", ". ./whatsnew.sh\n"+script)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	return string(out)
}

// TestVersionComparison checks vcmp: a pre-release is lower than its release (what "sort -V" gets wrong), a test build is higher than the release it follows.
func TestVersionComparison(t *testing.T) {
	cases := []struct {
		a, b string
		want string
	}{
		{"0.5.0-rc.1", "0.5.0", "-1"},
		{"0.5.0", "0.5.0-rc.1", "1"},
		{"0.5.0", "0.5.0", "0"},
		{"0.4.2", "0.5.0-rc.1", "-1"},
		{"0.5.0-rc.1", "0.4.2", "1"},
		{"0.5.0-rc.2", "0.5.0-rc.10", "-1"},
		{"0.3.0-9-gabc", "0.3.0", "1"},
		{"0.3.0-9-gabc", "0.3.1", "-1"},
		{"0.3.0-10-gabc", "0.3.0-9-gdef", "1"},
		{"0.10.0", "0.9.0", "1"},
		{"0.4.2", "0.4.10", "-1"},
		{"0.5.0", "0.5.1-rc.1", "-1"},
	}
	var script strings.Builder
	for _, c := range cases {
		script.WriteString("echo \"" + c.a + " " + c.b + " $(vcmp " + c.a + " " + c.b + ")\"\n")
	}
	lines := strings.Split(strings.TrimSpace(runShell(t, script.String())), "\n")
	if len(lines) != len(cases) {
		t.Fatalf("%d lines for %d cases:\n%s", len(lines), len(cases), strings.Join(lines, "\n"))
	}
	for i, c := range cases {
		if want := c.a + " " + c.b + " " + c.want; strings.TrimSpace(lines[i]) != want {
			t.Errorf("got %q, want %q", lines[i], want)
		}
	}
}

// TestWhatsNew checks which releases are listed: all after the installed one up to the offered one, nothing for a downgrade or no change, and the release itself
// (not nothing) when the installed version is its own pre-release, which is the case that once printed no list at all.
func TestWhatsNew(t *testing.T) {
	cases := []struct {
		name, installed, offered string
		want, notWant            []string
	}{
		{"release after its own pre-release", "0.5.0-rc.1", "0.5.0",
			[]string{"0.5.0 (2026-10-29)", "- Secure Boot is built in (#10)", "- Entries survive a restart (#70)"},
			[]string{"0.5.0-rc.1", "prototype"}},
		{"two releases behind", "0.4.1", "0.5.0",
			[]string{"0.5.0 (2026-10-29)", "0.5.0-rc.1 (2026-10-27)", "0.4.2 (2026-10-26)"},
			[]string{"An older change"}},
		{"nothing installed is known", "", "0.5.0",
			[]string{"0.5.0 (2026-10-29)"},
			[]string{"0.4.2"}},
		{"a test build after 0.4.2", "0.4.2-9-gabc", "0.5.0",
			[]string{"0.5.0 (2026-10-29)", "0.5.0-rc.1 (2026-10-27)"},
			[]string{"0.4.2 (2026-10-26)"}},
		{"a downgrade", "0.5.0", "0.4.2", nil, []string{"0.4", "0.5"}},
		{"no change", "0.5.0", "0.5.0", nil, []string{"0.5", "0.4"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := runShell(t, "whatsnew CHANGELOG.md '"+c.installed+"' "+c.offered)
			for _, w := range c.want {
				if !strings.Contains(out, w) {
					t.Errorf("the list lacks %q:\n%s", w, out)
				}
			}
			for _, w := range c.notWant {
				if strings.Contains(out, w) {
					t.Errorf("the list has %q but should not:\n%s", w, out)
				}
			}
		})
	}
}
