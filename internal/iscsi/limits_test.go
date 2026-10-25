package iscsi

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

type countingReader struct{ calls int }

func (r *countingReader) ReadAt(p []byte, off int64) (int, error) {
	r.calls++
	return len(p), nil
}

// One READ command must not be able to ask for the size of the disk (the limit is 8 MB; this asks for 512 MB): the buffer for the whole read is made at once.
func TestAReadOfHundredsOfMegabytesIsRefusedBeforeAnythingIsAllocated(t *testing.T) {
	reader := &countingReader{}
	var out bytes.Buffer
	s := &conn{w: bufio.NewWriter(&out), disc: Disc{R: reader, Size: 8 << 30}, maxRecv: 4096}
	p := &pdu{}
	p.bhs[0] = opSCSICmd
	p.bhs[32] = 0x88 // READ(16)
	binary.BigEndian.PutUint64(p.bhs[34:], 0)
	binary.BigEndian.PutUint32(p.bhs[42:], 1<<20) // 512 MB
	binary.BigEndian.PutUint32(p.bhs[20:], 1<<29)
	if err := s.command(p); err != nil {
		t.Fatal(err)
	}
	if err := s.w.Flush(); err != nil {
		t.Fatal(err)
	}
	r, err := readPDU(&out)
	if err != nil || r.opcode() != opSCSIResp || r.bhs[3] != statusCheckCondition {
		t.Fatalf("the read was not refused with a check condition (%v)", err)
	}
	if reader.calls != 0 {
		t.Fatal("the disk was read")
	}
}

// A flood of connections must not open more sessions than the limit, and a connection that never logs in must not hold its place for ever.
func TestOnlyAFewSessionsAreServedAtOnceAndASilentOneIsDropped(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	var mu sync.Mutex
	var logged []string
	target := &Target{Logf: func(format string, args ...any) {
		mu.Lock()
		logged = append(logged, format)
		mu.Unlock()
	}}
	go target.Serve(l) //nolint:errcheck
	var conns []net.Conn
	for i := 0; i < maxSessions+5; i++ {
		c, err := net.Dial("tcp", l.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		conns = append(conns, c)
		defer c.Close()
	}
	time.Sleep(300 * time.Millisecond)
	refused := 0
	for _, c := range conns[maxSessions:] { // (the ones above the limit are closed at once; the others wait for a login)
		c.SetReadDeadline(time.Now().Add(500 * time.Millisecond)) //nolint:errcheck
		if _, err := c.Read(make([]byte, 1)); err == io.EOF {
			refused++
		}
	}
	if refused != 5 {
		t.Errorf("%d of the 5 connections above the limit of %d were closed", refused, maxSessions)
	}
	conns[0].SetReadDeadline(time.Now().Add(200 * time.Millisecond)) //nolint:errcheck
	if _, err := conns[0].Read(make([]byte, 1)); err == io.EOF {
		t.Error("a connection within the limit was closed")
	}
	if loginTime > time.Minute || idleTime > 12*time.Hour {
		t.Errorf("the deadlines are too long: %v and %v", loginTime, idleTime)
	}
}
