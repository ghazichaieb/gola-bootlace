package httpd

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func server(t *testing.T) (*httptest.Server, string) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "My Linux.iso"), []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Server{ISODir: dir, Title: "Test", Base: "http://10.0.0.1:8080"}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts, dir
}

func get(t *testing.T, url string, header map[string]string) (int, string) {
	req, _ := http.NewRequest("GET", url, nil)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b)
}

func TestTheMenuListsTheIsosAndBootsThemOverHTTP(t *testing.T) {
	ts, _ := server(t)
	code, body := get(t, ts.URL+"/boot.ipxe", nil)
	if code != 200 || !strings.Contains(body, "chain http://10.0.0.1:8080/menu?mac=${mac}") {
		t.Fatalf("the boot script must ask for the menu with the PC's identity: %d %q", code, body)
	}
	code, body = get(t, ts.URL+"/menu?mac=aa:bb:cc:dd:ee:ff&ip=10.0.0.9", nil)
	if code != 200 || !strings.HasPrefix(body, "#!ipxe") || !strings.Contains(body, "My Linux") || !strings.Contains(body, "sanboot --no-describe --drive 0x80 http://10.0.0.1:8080/iso/My%20Linux.iso") || !strings.Contains(body, "as a disk") {
		t.Fatalf("%d %q", code, body)
	}
}

func TestIsosAreServedWithRangesAndUnknownNamesAreRefused(t *testing.T) {
	ts, _ := server(t)
	if code, body := get(t, ts.URL+"/iso/My%20Linux.iso", nil); code != 200 || body != "0123456789" {
		t.Fatalf("%d %q", code, body)
	}
	if code, body := get(t, ts.URL+"/iso/My%20Linux.iso", map[string]string{"Range": "bytes=2-4"}); code != 206 || body != "234" {
		t.Fatalf("range: %d %q", code, body)
	}
	for _, name := range []string{"/iso/missing.iso", "/iso/../x", "/iso/"} {
		if code, _ := get(t, ts.URL+name, nil); code != 404 && code != 301 {
			t.Fatalf("%s: %d", name, code)
		}
	}
}

func TestFreeBSDsLoaderConfigurationGetsTheMemoryDiskLines(t *testing.T) {
	got := string(loaderConf([]byte("autoboot_delay=\"2\"")))
	for _, want := range []string{"autoboot_delay=\"2\"\n", "mdroot_load=\"YES\"", "mdroot_type=\"md_image\"", "mdroot_name=\"/boot.iso\"", "vfs.root.mountfrom=\"cd9660:/dev/md0\""} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if !isLoaderName("boot/loader.efi") || !isLoaderName("boot/pxeboot") || isLoaderName("boot/kernel/kernel") {
		t.Error("only the two loaders say which ISO a PC chose")
	}
}
