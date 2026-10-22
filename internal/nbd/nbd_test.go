package nbd

import (
	"bytes"
	"net"
	"testing"
)

func startServer(t *testing.T, data []byte) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	s := &Server{
		Open: func(name string) (Export, bool) {
			return Export{R: bytes.NewReader(data), Size: int64(len(data))}, name == "abc"
		},
		Names: func() []string { return []string{"abc"} },
	}
	go s.Serve(l)
	return l.Addr().String()
}

func TestAClientReadsAnExportByName(t *testing.T) {
	data := make([]byte, 1<<20)
	for i := range data {
		data[i] = byte(i * 7)
	}
	c, err := net.Dial("tcp", startServer(t, data))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	size, flags, err := Handshake(c, "abc")
	if err != nil || size != int64(len(data)) || flags&transReadOnly == 0 {
		t.Fatalf("handshake: %d %x %v", size, flags, err)
	}
	for _, at := range []uint64{0, 4096, 123456, uint64(len(data)) - 2048} {
		got, err := Read(c, at+1, at, 2048)
		if err != nil || !bytes.Equal(got, data[at:at+2048]) {
			t.Fatalf("read at %d: %v", at, err)
		}
	}
	if _, err := Read(c, 9, uint64(len(data))-10, 2048); err == nil {
		t.Fatal("a read past the end must be refused")
	}
}

func TestAnUnknownExportClosesTheConnection(t *testing.T) {
	c, err := net.Dial("tcp", startServer(t, []byte("x")))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, _, err := Handshake(c, "nope"); err == nil {
		t.Fatal("expected an error")
	}
}
