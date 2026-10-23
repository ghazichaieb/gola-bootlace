package admin

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func isoLike(n int) []byte {
	b := make([]byte, n)
	copy(b[0x8001:], "CD001") // the primary volume descriptor of an ISO
	for i := 0x9000; i < n; i++ {
		b[i] = byte(i % 251)
	}
	return b
}

func TestAFileCanBeSentInPiecesAndContinuedAfterABreak(t *testing.T) {
	r := newRig(t)
	r.login()
	dir := t.TempDir()
	if code, _ := r.do("PUT", "/api/library", map[string]any{"path": dir, "create": true}, nil); code != 200 {
		t.Fatal(code)
	}
	content := isoLike(1 << 20)
	sum := sha256.Sum256(content)
	code, out := r.do("POST", "/api/library/upload/sessions", map[string]any{"name": "pieces.iso", "size": len(content), "sha256": hex.EncodeToString(sum[:])}, nil)
	id, _ := out["id"].(string)
	if code != 200 || id == "" || out["received"].(float64) != 0 {
		t.Fatalf("%d %v", code, out)
	}
	base := "/api/library/upload/sessions/" + id
	patch := func(offset int, body []byte) (int, map[string]any) {
		code, b := r.raw("PATCH", base+"?offset="+strconv.Itoa(offset), body)
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		return code, m
	}
	if code, m := patch(0, content[:400<<10]); code != 200 || m["received"].(float64) != 400<<10 {
		t.Fatalf("%d %v", code, m)
	}
	// the page is reloaded: it asks for the open uploads and starts the same one again
	_, list := r.do("GET", "/api/library/upload/sessions", nil, nil)
	if uploads := list["uploads"].([]any); len(uploads) != 1 || uploads[0].(map[string]any)["received"].(float64) != 400<<10 {
		t.Fatalf("%v", list)
	}
	if _, again := r.do("POST", "/api/library/upload/sessions", map[string]any{"name": "pieces.iso", "size": len(content), "sha256": hex.EncodeToString(sum[:])}, nil); again["id"] != id || again["received"].(float64) != 400<<10 {
		t.Fatalf("the same upload should be found: %v", again)
	}
	// a piece sent from the wrong place is refused and says where to go on
	if code, m := patch(0, content[:10]); code != 409 || m["received"].(float64) != 400<<10 {
		t.Fatalf("%d %v", code, m)
	}
	if code, _ := patch(400<<10, content[400<<10:]); code != 200 {
		t.Fatal(code)
	}
	code, out = r.do("POST", base+"/finish", map[string]any{}, nil)
	if code != 200 || out["dest"] != "pieces.iso" {
		t.Fatalf("%d %v", code, out)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "pieces.iso")); !bytes.Equal(b, content) {
		t.Fatal("the file is not whole")
	}
	if code, _ := r.do("GET", base, nil, nil); code != 404 {
		t.Fatalf("a finished upload is gone: %d", code)
	}
	// cancelling removes what was received; a made-up id is not found
	_, out = r.do("POST", "/api/library/upload/sessions", map[string]any{"name": "other.iso", "size": 5000}, nil)
	other := "/api/library/upload/sessions/" + out["id"].(string)
	if code, _ := r.do("DELETE", other, nil, nil); code != 200 {
		t.Fatal(code)
	}
	if code, _ := r.do("GET", "/api/library/upload/sessions/0123456789abcdef", nil, nil); code != 404 {
		t.Fatal(code)
	}
	if code, _ := r.do("GET", "/api/library/upload/sessions/..%2f..%2fx", nil, nil); code != 404 {
		t.Fatalf("an id with a path: %d", code)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, ".bootlace", "incoming", "*")); len(left) != 0 {
		t.Fatalf("nothing is left behind: %v", left)
	}
}
