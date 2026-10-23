package admin

import (
	"strings"
	"testing"

	"github.com/ghazichaieb/gola-bootlace/internal/backup"
	"github.com/ghazichaieb/gola-bootlace/internal/config"
)

func TestAnIsoPasswordIsStoredAsAHashAndKeptOutOfBackupsWithoutSecrets(t *testing.T) {
	r := newRig(t)
	r.login()
	if code, _ := r.do("PUT", "/api/isos/0123456789", map[string]any{"bootPassword": "abc"}, nil); code != 400 {
		t.Fatalf("too short: %d", code)
	}
	if code, _ := r.do("PUT", "/api/isos/0123456789", map[string]any{"bootPassword": "wipe-2026"}, nil); code != 200 {
		t.Fatal(code)
	}
	hash := r.store.Get().BootPasswords["0123456789"]
	if hash == "" || strings.Contains(hash, "wipe-2026") || !CheckPassword(hash, "wipe-2026") || CheckPassword(hash, "wipe-2027") {
		t.Fatalf("the password must be kept as a hash that checks: %q", hash)
	}
	// the backup and the library copy without secrets never hold it; the backup with secrets does
	if got := backup.Strip(r.store.Get(), false); len(got.BootPasswords) != 0 {
		t.Fatalf("leaked: %v", got.BootPasswords)
	}
	if got := backup.Strip(r.store.Get(), true); got.BootPasswords["0123456789"] != hash {
		t.Fatal("a backup with secrets keeps it")
	}
	// restoring a backup that has none leaves this container's passwords alone
	in := &backup.Contents{Manifest: backup.Manifest{Format: 1}, Config: config.Config{}}
	if m := backup.Merge(r.store.Get(), in); m.BootPasswords["0123456789"] != hash {
		t.Fatalf("a restore without secrets must keep the passwords: %v", m.BootPasswords)
	}
	// the list says which ISOs are protected, never how
	r.backend.isos[0].Protected = true
	_, out := r.do("GET", "/api/isos", nil, nil)
	if first := out["isos"].([]any)[0].(map[string]any); first["protected"] != true || strings.Contains(strings.ToLower(toString(first)), "wipe-2026") {
		t.Fatalf("%v", first)
	}
	if code, _ := r.do("PUT", "/api/isos/0123456789", map[string]any{"bootPassword": ""}, nil); code != 200 || len(r.store.Get().BootPasswords) != 0 {
		t.Fatalf("an empty password takes it away: %d %v", code, r.store.Get().BootPasswords)
	}
}

func toString(m map[string]any) string {
	var b strings.Builder
	for k, v := range m {
		b.WriteString(k)
		b.WriteString("=")
		b.WriteString(strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(fmtAny(v), "\n", " "), "\t", " ")))
		b.WriteString(";")
	}
	return b.String()
}

func fmtAny(v any) string {
	switch x := v.(type) {
	case string:
		return x
	default:
		return ""
	}
}
