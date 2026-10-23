package tftp

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io/fs"
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

// A lookup that depends on who asks is tried before the files of the FS, and the FS answers whatever it does not know.
func TestALookupAnswersBeforeTheFilesDo(t *testing.T) {
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	files := fstest.MapFS{"a.txt": {Data: []byte("from the fs")}, "b.txt": {Data: []byte("also the fs")}}
	s := &Server{FS: files, Timeout: 300 * time.Millisecond, Retries: 3, Lookup: func(client net.IP, name string) (fs.File, bool) {
		if name != "a.txt" || !client.IsLoopback() {
			return nil, false
		}
		f, err := fstest.MapFS{"a.txt": {Data: []byte("from the lookup")}}.Open("a.txt")
		return f, err == nil
	}}
	go s.Serve(conn)
	addr := conn.LocalAddr().(*net.UDPAddr)
	if data, _, msg := get(t, addr, "a.txt"); string(data) != "from the lookup" {
		t.Fatalf("a.txt: %q %q", data, msg)
	}
	if data, _, msg := get(t, addr, "b.txt"); string(data) != "also the fs" {
		t.Fatalf("b.txt: %q %q", data, msg)
	}
}

// loaderLike is a client that behaves like FreeBSD's loader: it takes the block it expects and acknowledges it, answers a block it has seen before with the
// acknowledgement of the last one it took, and drops a block from the future without an answer. drop says which packets get lost on the way.
func loaderLike(t *testing.T, server *net.UDPAddr, file string, drop func(block uint16, seen int) bool, opts ...string) []byte {
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
	conn.WriteTo(req, server)
	buf := make([]byte, 70000)
	var peer net.Addr
	var data []byte
	expect := uint16(1)
	blockSize := 512
	seen := map[uint16]int{}
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
			t.Fatalf("error: %s", buf[4:n])
		case opOACK:
			parts := strings.Split(string(buf[2:n]), "\x00")
			for i := 0; i+1 < len(parts); i += 2 {
				if parts[i] == "blksize" {
					fmt.Sscanf(parts[i+1], "%d", &blockSize)
				}
			}
			conn.WriteTo([]byte{0, opACK, 0, 0}, peer)
		case opDATA:
			b := binary.BigEndian.Uint16(buf[2:])
			seen[b]++
			if drop != nil && drop(b, seen[b]) {
				continue
			}
			switch {
			case b == expect:
				data = append(data, buf[4:n]...)
				expect++
				conn.WriteTo([]byte{0, opACK, buf[2], buf[3]}, peer)
				if n-4 < blockSize {
					return data
				}
			case int16(b-expect) < 0: // a block it has already: it says what it has
				last := expect - 1
				conn.WriteTo([]byte{0, opACK, byte(last >> 8), byte(last)}, peer)
			}
			// a block from the future: no answer
		}
	}
}

func startWindow(t *testing.T, files fstest.MapFS, window int) *net.UDPAddr {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	s := &Server{FS: files, Timeout: 200 * time.Millisecond, Retries: 5, Window: func(net.IP, string) int { return window }}
	go s.Serve(conn)
	return conn.LocalAddr().(*net.UDPAddr)
}

func TestAWindowOfBlocksArrivesIntactWithLossAndDuplicates(t *testing.T) {
	data := content(1428*300 + 77)
	srv := startWindow(t, fstest.MapFS{"big": {Data: data}}, 4)
	for _, tc := range []struct {
		name string
		drop func(uint16, int) bool
	}{
		{"nothing lost", nil},
		{"a block lost once", func(b uint16, seen int) bool { return b == 10 && seen == 1 }},
		{"the first and the last block lost once", func(b uint16, seen int) bool { return (b == 1 || b == 301) && seen == 1 }},
		{"every 17th block lost once", func(b uint16, seen int) bool { return b%17 == 0 && seen == 1 }},
	} {
		got := loaderLike(t, srv, "big", tc.drop, "blksize\x001428")
		if !bytes.Equal(got, data) {
			t.Errorf("%s: %d bytes arrived, want %d, or the content differs", tc.name, len(got), len(data))
		}
	}
}

func TestAWindowSurvivesBlockNumbersGoingRoundAndAnExactMultiple(t *testing.T) {
	data := content(8*70000 + 0) // an exact multiple of the block size: ends with an empty block; 70000 blocks go past 65535
	srv := startWindow(t, fstest.MapFS{"long": {Data: data}}, 4)
	if got := loaderLike(t, srv, "long", nil, "blksize\x008"); !bytes.Equal(got, data) {
		t.Errorf("%d bytes arrived, want %d, or the content differs", len(got), len(data))
	}
}

func TestAClientThatAsksForAWindowIsLeftToTheNormalWay(t *testing.T) {
	data := content(1428*20 + 5)
	srv := startWindow(t, fstest.MapFS{"f": {Data: data}}, 8)
	got, _, errMsg := get(t, srv, "f", "blksize\x001428", "windowsize\x004")
	if errMsg != "" || !bytes.Equal(got, data) {
		t.Errorf("err %q, %d bytes", errMsg, len(got))
	}
}

func TestTheBlockSizeCanBeCapped(t *testing.T) {
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	limit := 1024
	s := &Server{FS: fstest.MapFS{"f": {Data: content(5000)}}, Timeout: 300 * time.Millisecond, Retries: 3, MaxBlock: func() int { return limit }}
	go s.Serve(conn)
	server := conn.LocalAddr().(*net.UDPAddr)
	got, oack, msg := get(t, server, "f", "blksize", "1428")
	if msg != "" || !bytes.Equal(got, content(5000)) || oack["blksize"] != "1024" {
		t.Fatalf("error %q, %d bytes, %v", msg, len(got), oack)
	}
	if _, oack, _ = get(t, server, "f", "blksize", "700"); oack["blksize"] != "700" {
		t.Errorf("a smaller block size than the cap is kept: %v", oack)
	}
	limit = 0 // changed in the web interface while the server runs: no cap
	if _, oack, _ = get(t, server, "f", "blksize", "1428"); oack["blksize"] != "1428" {
		t.Errorf("no cap: %v", oack)
	}
}
