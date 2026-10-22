package winpe

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Needs wimlib-imagex and a real boot.wim (BOOTLACE_TEST_WIM): otherwise skipped.
func TestPatchBootWIMAddsTheFilesToEveryImage(t *testing.T) {
	wim := os.Getenv("BOOTLACE_TEST_WIM")
	lib := Wimlib(os.Getenv("BOOTLACE_WIMLIB"))
	if wim == "" || lib == "" {
		t.Skip("set BOOTLACE_TEST_WIM (a boot.wim) and have wimlib-imagex to run this")
	}
	src, err := os.Open(wim)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	dst := filepath.Join(t.TempDir(), "patched.wim")
	files := map[string][]byte{"/Windows/System32/winpeshl.ini": []byte(ShellIni()), "/Windows/System32/bootlace.cmd": []byte("@echo test\r\n")}
	if err := PatchBootWIM(lib, src, dst, files); err != nil {
		t.Fatal(err)
	}
	for _, image := range []string{"1", "2"} {
		out, err := exec.Command(lib, "dir", dst, image).CombinedOutput()
		if err != nil {
			t.Fatal(err, string(out))
		}
		if !strings.Contains(strings.ToLower(string(out)), `\windows\system32\bootlace.cmd`) || !strings.Contains(strings.ToLower(string(out)), `\windows\system32\winpeshl.ini`) {
			t.Fatalf("image %s lacks the files; listing has %d bytes; wimlib said: %.200s", image, len(out), out)
		}
	}
}
