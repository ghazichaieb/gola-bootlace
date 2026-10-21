package iso9660

import (
	"os"
	"testing"
)

// This test reads a real ISO: set BOOTLACE_TEST_ISO to an Alpine Linux ISO (the files it names are Alpine's) to run it.
func TestRealISO(t *testing.T) {
	name := os.Getenv("BOOTLACE_TEST_ISO")
	if name == "" {
		t.Skip("set BOOTLACE_TEST_ISO to an Alpine Linux ISO to run this")
	}
	f, err := os.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	st, _ := f.Stat()
	img, err := Open(f, st.Size())
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"/", "/boot", "/apks/x86_64"} {
		list, err := img.List(dir)
		if err != nil {
			t.Fatalf("%s: %v", dir, err)
		}
		t.Log(dir, list[:min(len(list), 12)])
	}
	k, err := img.Open("/boot/vmlinuz-virt")
	if err != nil {
		t.Fatal(err)
	}
	head := make([]byte, 2)
	if _, err := k.ReadAt(head, 0); err != nil || string(head) != "MZ" && head[0] == 0 {
		t.Fatalf("kernel head %v %v", head, err)
	}
	t.Log("kernel size", k.Size(), "head", head)
}
