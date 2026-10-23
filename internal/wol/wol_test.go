package wol

import (
	"bytes"
	"net"
	"testing"
	"time"
)

func TestThePacketIsSixFFThenTheMACSixteenTimes(t *testing.T) {
	mac, _ := net.ParseMAC("aa:bb:cc:dd:ee:01")
	p, err := Packet(mac)
	if err != nil || len(p) != 102 || !bytes.Equal(p[:6], bytes.Repeat([]byte{0xFF}, 6)) {
		t.Fatalf("%x %v", p, err)
	}
	for i := 0; i < 16; i++ {
		if !bytes.Equal(p[6+i*6:12+i*6], mac) {
			t.Fatalf("repeat %d: %x", i, p)
		}
	}
	if _, err := Packet(net.HardwareAddr{1, 2, 3}); err == nil {
		t.Error("a short address")
	}
}

func TestBroadcast(t *testing.T) {
	if got := Broadcast(net.ParseIP("192.168.8.201"), net.CIDRMask(24, 32)); got.String() != "192.168.8.255" {
		t.Error(got)
	}
	if got := Broadcast(net.ParseIP("10.1.2.3"), net.CIDRMask(16, 32)); got.String() != "10.1.255.255" {
		t.Error(got)
	}
	if got := Broadcast(net.ParseIP("10.1.2.3"), nil); !got.Equal(net.IPv4bcast) {
		t.Error(got)
	}
}

func TestSendDeliversThePacket(t *testing.T) {
	l, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	mac, _ := net.ParseMAC("02:00:00:00:00:09")
	if err := Send(mac, net.ParseIP("127.0.0.1"), "nonsense", l.LocalAddr().String()); err != nil {
		t.Fatal(err)
	}
	_ = l.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 200)
	n, _, err := l.ReadFrom(buf)
	if err != nil || n != 102 || !bytes.Equal(buf[6:12], mac) {
		t.Fatalf("%d %v", n, err)
	}
	if err := Send(mac, net.ParseIP("127.0.0.1"), "nonsense"); err == nil {
		t.Error("nothing was sent")
	}
}
