package tftp

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func start(t *testing.T, files fstest.MapFS) *net.UDPAddr {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	s := &Server{FS: files, Timeout: 300 * time.Millisecond, Retries: 3}
	go s.Serve(conn)
	return conn.LocalAddr().(*net.UDPAddr)
}

// get is a small TFTP client: it asks for a file with the given options and returns what the server sent (or the error message).
func get(t *testing.T, server *net.UDPAddr, file string, opts ...string) (data []byte, oack map[string]string, errMsg string) {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	req := append([]byte{0, opRRQ}, []byte(file+"\x00octet\x00")...)
	for _, o := range opts {
		req = append(req, []byte(o+"\x00")...)
	}
	if _, err := conn.WriteTo(req, server); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 70000)
	var peer net.Addr
	expect := uint16(1)
	blockSize := 512
	for {
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		n, from, err := conn.ReadFrom(buf)
		if err != nil {
			t.Fatalf("no answer: %v", err)
		}
		if peer == nil {
			peer = from
		}
		switch binary.BigEndian.Uint16(buf) {
		case opERROR:
			return nil, nil, string(bytes.TrimRight(buf[4:n], "\x00"))
		case opOACK:
			oack = map[string]string{}
			parts := strings.Split(string(buf[2:n]), "\x00")
			for i := 0; i+1 < len(parts); i += 2 {
				oack[parts[i]] = parts[i+1]
				if parts[i] == "blksize" {
					fmt.Sscanf(parts[i+1], "%d", &blockSize)
				}
			}
			ack := []byte{0, opACK, 0, 0}
			conn.WriteTo(ack, peer)
		case opDATA:
			if binary.BigEndian.Uint16(buf[2:]) == expect {
				data = append(data, buf[4:n]...)
				expect++
			}
			conn.WriteTo([]byte{0, opACK, buf[2], buf[3]}, peer)
			if n-4 < blockSize {
				return data, oack, ""
			}
		}
	}
}

func content(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i*7 + i/251)
	}
	return b
}

func TestFilesOfEveryAwkwardSizeArriveIntact(t *testing.T) {
	files := fstest.MapFS{}
	sizes := []int{0, 1, 511, 512, 513, 1024, 2000, 100000}
	for _, n := range sizes {
		files[fmt.Sprintf("f%d", n)] = &fstest.MapFile{Data: content(n)}
	}
	server := start(t, files)
	for _, n := range sizes {
		got, _, msg := get(t, server, fmt.Sprintf("f%d", n))
		if msg != "" || !bytes.Equal(got, content(n)) {
			t.Errorf("%d bytes: error %q, got %d bytes", n, msg, len(got))
		}
	}
}

func TestOptionsAreNegotiatedAndTheSizeIsReported(t *testing.T) {
	server := start(t, fstest.MapFS{"boot.ipxe": {Data: content(5000)}})
	got, oack, msg := get(t, server, "boot.ipxe", "blksize", "1428", "tsize", "0")
	if msg != "" || !bytes.Equal(got, content(5000)) {
		t.Fatalf("error %q, %d bytes", msg, len(got))
	}
	if oack["blksize"] != "1428" || oack["tsize"] != "5000" {
		t.Errorf("option acknowledgement %v", oack)
	}
	_, oack, _ = get(t, server, "boot.ipxe", "blksize", "99999999")
	if oack["blksize"] != "65464" {
		t.Errorf("an absurd block size must be cut down, got %v", oack)
	}
}

func TestMissingFilesAndEscapingPathsAreRefused(t *testing.T) {
	server := start(t, fstest.MapFS{"ok.txt": {Data: []byte("hi")}, "dir/inner.txt": {Data: []byte("x")}})
	for _, name := range []string{"nope", "../ok.txt", "..\\ok.txt", "dir", "/etc/passwd", "dir/../../ok.txt"} {
		got, _, msg := get(t, server, name)
		if name == "dir/../../ok.txt" || name == "../ok.txt" || name == "..\\ok.txt" || name == "/etc/passwd" {
			// paths are cleaned to stay inside the folder: either not found, or the folder's own file, never a file outside
			if msg == "" && string(got) != "hi" {
				t.Errorf("%q gave %q", name, got)
			}
			continue
		}
		if msg == "" {
			t.Errorf("%q must be refused, got %d bytes", name, len(got))
		}
	}
}

func TestWritingIsRefused(t *testing.T) {
	server := start(t, fstest.MapFS{})
	conn, _ := net.ListenPacket("udp", "127.0.0.1:0")
	defer conn.Close()
	conn.WriteTo(append([]byte{0, opWRQ}, []byte("x\x00octet\x00")...), server)
	buf := make([]byte, 600)
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err := conn.ReadFrom(buf)
	if err != nil || binary.BigEndian.Uint16(buf) != opERROR || binary.BigEndian.Uint16(buf[2:]) != errAccess {
		t.Fatalf("expected an access violation error, got %v %v", n, err)
	}
}
