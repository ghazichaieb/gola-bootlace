package importer

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ghazichaieb/gola-bootlace/internal/library"
)

func TestAnUploadContinuesFromWhereTheServerHasItAfterABreak(t *testing.T) {
	r := newRig(t)
	content := isoBytes(700<<10, 'r')
	up := UploadRequest{Name: "big.iso", Size: int64(len(content)), SHA256: sha(content)}
	s, _, err := r.m.StartUpload(up)
	if err != nil || s.Received != 0 {
		t.Fatalf("%+v %v", s, err)
	}
	// the first piece, then a piece that breaks off in the middle
	if got, err := r.m.UploadChunk(s.ID, 0, bytes.NewReader(content[:300<<10])); err != nil || got.Received != 300<<10 {
		t.Fatalf("%+v %v", got, err)
	}
	broken := &breakingReader{data: content[300<<10 : 600<<10], after: 100 << 10}
	got, err := r.m.UploadChunk(s.ID, 300<<10, broken)
	if err == nil || got.Received != 400<<10 {
		t.Fatalf("a piece that breaks still counts for what arrived: %+v %v", got, err)
	}
	// a page that is reloaded starts the same upload again and finds it where it was
	again, _, err := r.m.StartUpload(up)
	if err != nil || again.ID != s.ID || again.Received != 400<<10 {
		t.Fatalf("the open upload should be found: %+v %v", again, err)
	}
	// a piece at the wrong place is refused and says where to go on
	_, err = r.m.UploadChunk(s.ID, 0, bytes.NewReader(content[:10]))
	var oe *OffsetError
	if !errors.As(err, &oe) || oe.Received != 400<<10 {
		t.Fatalf("wrong place: %v", err)
	}
	if _, err := r.m.UploadChunk(s.ID, 400<<10, bytes.NewReader(content[400<<10:])); err != nil {
		t.Fatal(err)
	}
	dest, warn, err := r.m.FinishUpload(s.ID)
	if err != nil || dest != "big.iso" || len(warn) != 0 {
		t.Fatalf("%q %v %v", dest, warn, err)
	}
	if b, _ := os.ReadFile(filepath.Join(r.dir, "big.iso")); sha(b) != sha(content) {
		t.Fatal("the file is not whole")
	}
	if rec, ok := r.idx.Get("big.iso"); !ok || rec.SHA256 != sha(content) {
		t.Fatalf("index: %+v %v", rec, ok)
	}
	if left, _ := filepath.Glob(filepath.Join(r.dir, library.MetaDir, "incoming", "*")); len(left) != 0 {
		t.Fatalf("nothing is left behind: %v", left)
	}
	if _, err := r.m.UploadChunk(s.ID, 0, bytes.NewReader(nil)); !errors.Is(err, ErrNoSession) {
		t.Fatalf("a finished upload is gone: %v", err)
	}
}

type breakingReader struct {
	data  []byte
	after int
	read  int
}

func (b *breakingReader) Read(p []byte) (int, error) {
	if b.read >= b.after {
		return 0, errors.New("connection reset")
	}
	n := copy(p, b.data[b.read:min(b.after, len(b.data))])
	b.read += n
	return n, nil
}

func TestUploadsInPiecesAreCheckedLikeOtherUploads(t *testing.T) {
	r := newRig(t)
	good := isoBytes(200<<10, 'g')
	run := func(req UploadRequest, body []byte) error {
		s, _, err := r.m.StartUpload(req)
		if err != nil {
			return err
		}
		for off := 0; off < len(body); off += 64 << 10 {
			end := min(off+64<<10, len(body))
			if _, err := r.m.UploadChunk(s.ID, int64(off), bytes.NewReader(body[off:end])); err != nil {
				_ = r.m.AbortUpload(s.ID)
				return err
			}
		}
		_, _, err = r.m.FinishUpload(s.ID)
		return err
	}
	if err := run(UploadRequest{Name: "ok.iso", Size: int64(len(good)), SHA256: sha(good)}, good); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		req  UploadRequest
		body []byte
		want string
	}{
		"wrong checksum": {UploadRequest{Name: "a.iso", Size: int64(len(good)), SHA256: sha([]byte("x"))}, good, "checksum does not match"},
		"a web page":     {UploadRequest{Name: "b.iso", Size: 21 + 100<<10}, []byte("<!DOCTYPE html><html>" + strings.Repeat(" ", 100<<10)), "not an ISO"},
		"no size":        {UploadRequest{Name: "c.iso"}, good, "size"},
		"not an iso":     {UploadRequest{Name: "d.txt", Size: 10}, []byte("0123456789"), "has to end in"},
	} {
		if err := run(c.req, c.body); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v (wanted %q)", name, err, c.want)
		}
	}
	if has(filepath.Join(r.dir, "a.iso")) || has(filepath.Join(r.dir, "b.iso")) {
		t.Error("a refused file was placed")
	}
	if left, _ := filepath.Glob(filepath.Join(r.dir, library.MetaDir, "incoming", "*")); len(left) != 0 {
		t.Errorf("refused uploads leave nothing behind: %v", left)
	}
	// more than announced, and finishing early
	s, _, _ := r.m.StartUpload(UploadRequest{Name: "e.iso", Size: 100})
	if _, err := r.m.UploadChunk(s.ID, 0, bytes.NewReader(good[:200])); err == nil || !strings.Contains(err.Error(), "more data") {
		t.Errorf("more than announced: %v", err)
	}
	_ = r.m.AbortUpload(s.ID)
	s, _, _ = r.m.StartUpload(UploadRequest{Name: "f.iso", Size: 1000})
	if _, err := r.m.UploadChunk(s.ID, 0, bytes.NewReader(good[:100])); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.m.FinishUpload(s.ID); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Errorf("an early finish: %v", err)
	}
	_ = r.m.AbortUpload(s.ID)
	if len(r.m.Uploads()) != 0 {
		t.Errorf("aborted uploads are gone: %+v", r.m.Uploads())
	}
}

func TestOpenUploadsAreKeptForTheCleanUpAndExpire(t *testing.T) {
	r := newRig(t)
	s, _, err := r.m.StartUpload(UploadRequest{Name: "keep.iso", Size: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	if !r.m.UploadParts()[s.Part] {
		t.Fatalf("the part of an open upload belongs to it: %v", r.m.UploadParts())
	}
	if n := r.m.ExpireUploads(); n != 0 {
		t.Fatalf("a fresh upload does not expire: %d", n)
	}
	// make it old: the record says it was last touched long ago
	path, _ := r.m.sessionFile(s.ID)
	old := *s
	old.Updated = time.Now().Add(-SessionTTL - time.Hour)
	data, _ := json.Marshal(old)
	os.WriteFile(path, data, 0o600)
	if n := r.m.ExpireUploads(); n != 1 {
		t.Fatalf("an upload nobody touched for a week expires: %d", n)
	}
	if left, _ := filepath.Glob(filepath.Join(r.dir, library.MetaDir, "incoming", "*")); len(left) != 0 {
		t.Fatalf("its part goes too: %v", left)
	}
	if _, err := r.m.UploadState("not-an-id"); !errors.Is(err, ErrNoSession) {
		t.Fatalf("a made-up id: %v", err)
	}
	if _, err := r.m.UploadState("../../etc/pass"); !errors.Is(err, ErrNoSession) {
		t.Fatalf("an id with a path: %v", err)
	}
}

func TestResumeDoesNotReuseADifferentChecksum(t *testing.T) {
	r := newRig(t)
	content := isoBytes(64<<10, 'a')
	u := UploadRequest{Name: "same.iso", Size: int64(len(content))}
	first, _, err := r.m.StartUpload(u)
	if err != nil {
		t.Fatal(err)
	}
	u.SHA256 = sha(content)
	second, _, err := r.m.StartUpload(u)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID || second.Request.SHA256 != u.SHA256 {
		t.Fatal("the new checksum must not be silently discarded")
	}
	u.SHA256 = strings.ToUpper(u.SHA256)
	again, _, err := r.m.StartUpload(u)
	if err != nil || again.ID != second.ID {
		t.Fatalf("equivalent checksums should resume: %+v %v", again, err)
	}
}

func TestUploadSpaceCheckCountsOnlyBytesStillNeeded(t *testing.T) {
	r := newRig(t)
	free, _, err := library.DiskSpace(r.dir)
	if err != nil {
		t.Skip(err)
	}
	u := UploadRequest{Name: "large.iso", Size: int64(free) + 1}
	if c := r.m.CheckUpload(u); len(c.Problems) == 0 {
		t.Fatal("a new upload larger than free space must fail")
	}
	if c := r.m.checkUpload(u, u.Size); len(c.Problems) != 0 {
		t.Fatalf("already received bytes need no additional space: %v", c.Problems)
	}
}
