package nbd

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"strconv"
	"testing"
	"time"
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

func TestInvalidReadsDoNotReachTheExport(t *testing.T) {
	for _, off := range []uint64{1<<63 - 1, 1 << 63, ^uint64(0)} {
		t.Run(strconv.FormatUint(off, 10), func(t *testing.T) {
			if errno, calls := readReply(t, off, Export{R: &countReader{}, Size: 512}); errno != errInval || calls != 0 {
				t.Fatalf("invalid offset reached reader: errno=%d calls=%d", errno, calls)
			}
		})
	}
}

type countReader struct{ calls int }

func (r *countReader) ReadAt(p []byte, off int64) (int, error) {
	r.calls++
	return 0, io.EOF
}

func TestShortReadAtExportEndReturnsIOError(t *testing.T) {
	if errno, _ := readReply(t, 0, Export{R: &countReader{}, Size: 512}); errno != errIO {
		t.Fatalf("a truncated export must not return successful zero-filled data: errno=%d", errno)
	}
}

func readReply(t *testing.T, off uint64, exp Export) (uint32, int) {
	t.Helper()
	server, client := net.Pipe()
	defer client.Close()
	_ = client.SetDeadline(time.Now().Add(5 * time.Second))
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer server.Close()
		_ = (&Server{}).transmit(server, bufio.NewReader(server), exp)
	}()
	var req [28]byte
	binary.BigEndian.PutUint32(req[:], magicRequest)
	binary.BigEndian.PutUint64(req[16:], off)
	binary.BigEndian.PutUint32(req[24:], 512)
	if _, err := client.Write(req[:]); err != nil {
		t.Fatal(err)
	}
	var reply [16]byte
	if _, err := io.ReadFull(client, reply[:]); err != nil {
		t.Fatal(err)
	}
	client.Close()
	<-done
	return binary.BigEndian.Uint32(reply[4:]), exp.R.(*countReader).calls
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
