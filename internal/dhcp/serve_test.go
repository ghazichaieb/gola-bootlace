package dhcp

import (
	"net"
	"testing"
	"time"
)

func discover(mac byte, ipxe bool) *Packet {
	p := &Packet{Op: bootRequest, HType: 1, HLen: 6, XID: 7, Flags: flagBroadcast, CIAddr: net.IPv4zero, YIAddr: net.IPv4zero, SIAddr: net.IPv4zero, GIAddr: net.IPv4zero,
		Opts: map[byte][]byte{OptMessageType: {Discover}, OptVendorClass: []byte("PXEClient:Arch:00007"), OptClientArch: {0, 7}}}
	p.CHAddr[0], p.CHAddr[5] = 0x02, mac
	if ipxe {
		p.Opts[OptUserClass] = []byte("iPXE")
	}
	return p
}

func full() *Full {
	return &Full{Boot: Boot{ServerIP: net.IPv4(10, 0, 0, 1), Script: "http://10.0.0.1:8080/boot.ipxe", Files: map[Arch]string{ArchX64EFI: "ipxe.efi"}},
		First: net.IPv4(10, 0, 0, 100), Last: net.IPv4(10, 0, 0, 101), Mask: net.CIDRMask(24, 32), Router: net.IPv4(10, 0, 0, 1), Lease: time.Hour}
}

func TestFullModeOffersAnAddressAndTheBootFile(t *testing.T) {
	f := full()
	r, ok := f.Respond(discover(1, false))
	if !ok || r.MessageType() != Offer || !r.YIAddr.Equal(net.IPv4(10, 0, 0, 100)) || r.File != "ipxe.efi" || string(r.Opts[67]) != "ipxe.efi" {
		t.Fatalf("%+v %v", r, ok)
	}
	if string(r.Opts[1]) != string(net.CIDRMask(24, 32)) || !net.IP(r.Opts[3]).Equal(net.IPv4(10, 0, 0, 1)) {
		t.Fatalf("mask or router missing: %v", r.Opts)
	}
	r2, _ := f.Respond(discover(1, true))
	if r2.File != "http://10.0.0.1:8080/boot.ipxe" {
		t.Fatalf("a client that runs iPXE gets the script: %q", r2.File)
	}
}

func TestFullModeKeepsOneAddressPerPCAndRunsOut(t *testing.T) {
	f := full()
	a, _ := f.Respond(discover(1, false))
	again, _ := f.Respond(discover(1, false))
	b, _ := f.Respond(discover(2, false))
	if !a.YIAddr.Equal(again.YIAddr) || a.YIAddr.Equal(b.YIAddr) {
		t.Fatalf("%v %v %v", a.YIAddr, again.YIAddr, b.YIAddr)
	}
	if _, ok := f.Respond(discover(3, false)); ok {
		t.Fatal("a third PC got an address from a range of two")
	}
}

func TestFullModeAcknowledgesTheRequestAndStaysSilentForAnotherServer(t *testing.T) {
	f := full()
	f.Respond(discover(1, false))
	req := discover(1, false)
	req.Opts[OptMessageType] = []byte{Request}
	req.Opts[50] = []byte{10, 0, 0, 100}
	if r, ok := f.Respond(req); !ok || r.MessageType() != ACK || !r.YIAddr.Equal(net.IPv4(10, 0, 0, 100)) {
		t.Fatalf("%+v", r)
	}
	req.Opts[OptServerID] = []byte{10, 0, 0, 9}
	if _, ok := f.Respond(req); ok {
		t.Fatal("answered a request meant for another DHCP server")
	}
}

func TestPXEOnlyAnswersAPCThatBootedFromTheNetworkWhenItsSystemAsksLater(t *testing.T) {
	f := full()
	f.PXEOnly = true
	plain := func(mac byte) *Packet {
		p := discover(mac, false)
		delete(p.Opts, OptVendorClass)
		return p
	}
	if _, ok := f.Respond(plain(1)); ok {
		t.Fatal("answered an ordinary PC in PXE-only mode")
	}
	f.Respond(discover(1, false)) // the PC network boots
	if r, ok := f.Respond(plain(1)); !ok || !r.YIAddr.Equal(net.IPv4(10, 0, 0, 100)) {
		t.Fatalf("its system was not answered: %+v %v", r, ok)
	}
	if _, ok := f.Respond(plain(2)); ok {
		t.Fatal("answered another ordinary PC")
	}
}
