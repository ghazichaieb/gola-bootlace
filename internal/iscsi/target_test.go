package iscsi

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"testing"
)

func TestReadRejectsOverflowAndTruncatedMedia(t *testing.T) {
	for _, tc := range []struct {
		name string
		lba  uint64
	}{
		{"overflowing LBA", ^uint64(0)},
		{"truncated medium", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			reader := &shortReader{}
			s := &conn{w: bufio.NewWriter(&out), disc: Disc{R: reader, Size: BlockSize}, maxRecv: 4096}
			p := &pdu{}
			p.bhs[0] = opSCSICmd
			p.bhs[32] = 0x88 // READ(16)
			binary.BigEndian.PutUint64(p.bhs[34:], tc.lba)
			binary.BigEndian.PutUint32(p.bhs[42:], 1)
			binary.BigEndian.PutUint32(p.bhs[20:], BlockSize)
			if err := s.command(p); err != nil {
				t.Fatal(err)
			}
			if err := s.w.Flush(); err != nil {
				t.Fatal(err)
			}
			r, err := readPDU(&out)
			if err != nil || r.opcode() != opSCSIResp || r.bhs[3] != statusCheckCondition {
				t.Fatalf("invalid read must return check condition (read error: %v)", err)
			}
			if tc.lba != 0 && reader.calls != 0 {
				t.Fatal("an overflowing LBA reached the backing reader")
			}
		})
	}
}

type shortReader struct{ calls int }

func (r *shortReader) ReadAt(p []byte, off int64) (int, error) {
	r.calls++
	return 0, io.EOF
}

type initiator struct {
	t  *testing.T
	c  net.Conn
	sn uint32
}

func (i *initiator) send(op, flags byte, itt uint32, bhs map[int][]byte, data []byte) {
	b := make([]byte, 48)
	b[0], b[1] = op, flags
	b[5], b[6], b[7] = byte(len(data)>>16), byte(len(data)>>8), byte(len(data))
	binary.BigEndian.PutUint32(b[16:], itt)
	for off, v := range bhs {
		copy(b[off:], v)
	}
	out := append(b, data...)
	for len(out)%4 != 0 {
		out = append(out, 0)
	}
	if _, err := i.c.Write(out); err != nil {
		i.t.Fatal(err)
	}
}

func (i *initiator) recv() *pdu {
	p, err := readPDU(i.c)
	if err != nil {
		i.t.Fatal(err)
	}
	return p
}

func u32(v uint32) []byte { b := make([]byte, 4); binary.BigEndian.PutUint32(b, v); return b }

func login(i *initiator, target string) {
	i.send(opLoginReq, 0x80|0<<2|1, 1, map[int][]byte{8: {0, 2, 0x3d, 0, 0, 1}, 24: u32(0)}, []byte("InitiatorName=iqn.test\x00TargetName="+target+"\x00SessionType=Normal\x00AuthMethod=None\x00"))
	if r := i.recv(); r.opcode() != opLoginResp || r.bhs[36] != 0 {
		i.t.Fatalf("login 1: %x", r.bhs)
	}
	i.send(opLoginReq, 0x80|1<<2|3, 2, map[int][]byte{8: {0, 2, 0x3d, 0, 0, 1}, 24: u32(0)}, []byte("HeaderDigest=None\x00DataDigest=None\x00MaxRecvDataSegmentLength=4096\x00InitialR2T=Yes\x00"))
	r := i.recv()
	if r.opcode() != opLoginResp || r.bhs[36] != 0 || r.flags()&0x80 == 0 {
		i.t.Fatalf("login 2: %x", r.bhs)
	}
}

func (i *initiator) scsi(cdb []byte, expected uint32) (data []byte, status byte) {
	i.sn++
	c := make([]byte, 16)
	copy(c, cdb)
	i.send(opSCSICmd, 0x80|0x40, 100+i.sn, map[int][]byte{20: u32(expected), 24: u32(i.sn), 32: c}, nil)
	for {
		r := i.recv()
		switch r.opcode() {
		case opDataIn:
			data = append(data, r.data...)
			if len(r.data) > 4096 {
				i.t.Fatalf("a Data-In PDU of %d bytes: more than the initiator's 4096", len(r.data))
			}
		case opSCSIResp:
			return data, r.bhs[3]
		default:
			i.t.Fatalf("unexpected opcode %x", r.opcode())
		}
	}
}

func TestAnInitiatorCanReadAnIsoAsACdRom(t *testing.T) {
	data := make([]byte, 64*BlockSize)
	for i := range data {
		data[i] = byte(i / 3)
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	tg := &Target{Open: func(id string) (Disc, bool) {
		return Disc{R: bytes.NewReader(data), Size: int64(len(data))}, id == "abc"
	}}
	go tg.Serve(l)
	c, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in := &initiator{t: t, c: c}
	login(in, IQNPrefix+"abc")

	if _, st := in.scsi([]byte{0x00}, 0); st != statusGood {
		t.Fatalf("test unit ready: %x", st)
	}
	d, st := in.scsi([]byte{0x12, 0, 0, 0, 36}, 36)
	if st != statusGood || len(d) != 36 || d[0] != 0x05 {
		t.Fatalf("inquiry: %x %x", st, d)
	}
	d, _ = in.scsi([]byte{0x25}, 8)
	if binary.BigEndian.Uint32(d) != 63 || binary.BigEndian.Uint32(d[4:]) != BlockSize {
		t.Fatalf("read capacity: %x", d)
	}
	rd := make([]byte, 10)
	rd[0] = 0x28
	binary.BigEndian.PutUint32(rd[2:], 5)
	binary.BigEndian.PutUint16(rd[7:], 4)
	d, st = in.scsi(rd, 4*BlockSize)
	if st != statusGood || !bytes.Equal(d, data[5*BlockSize:9*BlockSize]) {
		t.Fatalf("read: status %x, %d bytes", st, len(d))
	}
	binary.BigEndian.PutUint32(rd[2:], 62)
	if _, st = in.scsi(rd, 4*BlockSize); st != statusCheckCondition {
		t.Fatalf("a read past the end: %x", st)
	}
}

func TestAnUnknownTargetIsRefused(t *testing.T) {
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	defer l.Close()
	go (&Target{Open: func(string) (Disc, bool) { return Disc{}, false }}).Serve(l)
	c, _ := net.Dial("tcp", l.Addr().String())
	defer c.Close()
	in := &initiator{t: t, c: c}
	in.send(opLoginReq, 0x80|1, 1, nil, []byte("TargetName="+IQNPrefix+"nope\x00"))
	if r := in.recv(); r.bhs[36] == 0 {
		t.Fatalf("login should be refused: %x", r.bhs)
	}
	if _, err := io.ReadAll(c); err != nil {
		t.Fatal(err)
	}
}

func TestADiscoverySessionListsTheTargets(t *testing.T) {
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	defer l.Close()
	go (&Target{Open: func(string) (Disc, bool) { return Disc{}, false }, IDs: func() []string { return []string{"abc", "def"} }, Address: "10.0.0.1:3260,1"}).Serve(l)
	c, _ := net.Dial("tcp", l.Addr().String())
	defer c.Close()
	in := &initiator{t: t, c: c}
	in.send(opLoginReq, 0x80|0<<2|1, 1, map[int][]byte{8: {0, 2, 0x3d, 0, 0, 1}, 24: u32(0)}, []byte("InitiatorName=iqn.test\x00SessionType=Discovery\x00AuthMethod=None\x00"))
	if r := in.recv(); r.opcode() != opLoginResp || r.bhs[36] != 0 {
		t.Fatalf("login 1: %x", r.bhs)
	}
	in.send(opLoginReq, 0x80|1<<2|3, 2, map[int][]byte{8: {0, 2, 0x3d, 0, 0, 1}, 24: u32(0)}, []byte("HeaderDigest=None\x00DataDigest=None\x00MaxRecvDataSegmentLength=4096\x00"))
	if r := in.recv(); r.opcode() != opLoginResp || r.bhs[36] != 0 || r.flags()&0x80 == 0 {
		t.Fatalf("login 2: %x", r.bhs)
	}
	in.send(opTextReq, 0x80, 3, map[int][]byte{24: u32(1)}, []byte("SendTargets=All\x00"))
	r := in.recv()
	if r.opcode() != opTextResp || !bytes.Contains(r.data, []byte("TargetName="+IQNPrefix+"abc\x00TargetAddress=10.0.0.1:3260,1\x00")) || !bytes.Contains(r.data, []byte(IQNPrefix+"def")) {
		t.Fatalf("send targets: %q", r.data)
	}
}

func TestADiskImageIsShownAsAHardDisk(t *testing.T) {
	data := make([]byte, 40*DiskBlockSize)
	for i := range data {
		data[i] = byte(i % 251)
	}
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	defer l.Close()
	go (&Target{Open: func(id string) (Disc, bool) {
		return Disc{R: bytes.NewReader(data), Size: int64(len(data)), Disk: true}, id == "img"
	}}).Serve(l)
	c, _ := net.Dial("tcp", l.Addr().String())
	defer c.Close()
	in := &initiator{t: t, c: c}
	login(in, IQNPrefix+"img")
	d, st := in.scsi([]byte{0x12, 0, 0, 0, 36}, 36)
	if st != statusGood || d[0] != 0x00 || d[1]&0x80 != 0 {
		t.Fatalf("inquiry of a disk: %x %x", st, d)
	}
	d, _ = in.scsi([]byte{0x25}, 8)
	if binary.BigEndian.Uint32(d) != 39 || binary.BigEndian.Uint32(d[4:]) != DiskBlockSize {
		t.Fatalf("read capacity: %x", d)
	}
	rd := make([]byte, 10)
	rd[0] = 0x28
	binary.BigEndian.PutUint32(rd[2:], 3)
	binary.BigEndian.PutUint16(rd[7:], 4)
	d, st = in.scsi(rd, 4*DiskBlockSize)
	if st != statusGood || !bytes.Equal(d, data[3*DiskBlockSize:7*DiskBlockSize]) {
		t.Fatalf("read: %x, %d bytes", st, len(d))
	}
}
