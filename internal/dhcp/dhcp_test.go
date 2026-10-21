package dhcp

import (
	"bytes"
	"net"
	"testing"
)

func pxeRequest(msg byte, arch uint16, userClass string) []byte {
	p := &Packet{
		Op: bootRequest, HType: 1, HLen: 6, XID: 0xdeadbeef, Flags: flagBroadcast,
		CHAddr: [16]byte{0x00, 0x15, 0x5d, 0x08, 0x02, 0x2d},
		Opts: map[byte][]byte{
			OptMessageType: {msg},
			OptVendorClass: []byte("PXEClient:Arch:00007:UNDI:003016"),
			OptClientArch:  {byte(arch >> 8), byte(arch)},
		},
	}
	if userClass != "" {
		p.Opts[OptUserClass] = []byte(userClass)
	}
	return p.Marshal()
}

func boot() Boot {
	return Boot{
		ServerIP: net.IPv4(192, 168, 1, 10),
		Files:    map[Arch]string{ArchBIOS: "bootlace-bios.kpxe", ArchX64EFI: "bootlace-x64.efi"},
		Script:   "http://192.168.1.10:8080/boot.ipxe",
	}
}

func TestAPXEClientGetsTheBootFileForItsArchitecture(t *testing.T) {
	for _, tc := range []struct {
		arch uint16
		file string
	}{{0, "bootlace-bios.kpxe"}, {7, "bootlace-x64.efi"}, {9, "bootlace-x64.efi"}} {
		req, err := Parse(pxeRequest(Discover, tc.arch, ""))
		if err != nil {
			t.Fatal(err)
		}
		reply, ok := boot().Reply(req)
		if !ok {
			t.Fatalf("arch %d: no answer", tc.arch)
		}
		if reply.File != tc.file || reply.MessageType() != Offer || reply.Op != bootReply || reply.XID != req.XID {
			t.Errorf("arch %d: file %q type %d op %d xid %x", tc.arch, reply.File, reply.MessageType(), reply.Op, reply.XID)
		}
		if !reply.YIAddr.Equal(net.IPv4zero) || !reply.SIAddr.Equal(net.IPv4(192, 168, 1, 10)) {
			t.Errorf("a proxy must not give an address (yiaddr %v) and must name itself (siaddr %v)", reply.YIAddr, reply.SIAddr)
		}
	}
}

func TestARequestIsAcknowledgedAndIPXEGetsTheScriptNotAFile(t *testing.T) {
	req, _ := Parse(pxeRequest(Request, 7, "iPXE"))
	reply, ok := boot().Reply(req)
	if !ok || reply.MessageType() != ACK || reply.File != "http://192.168.1.10:8080/boot.ipxe" {
		t.Fatalf("got %+v ok=%v", reply, ok)
	}
}

func TestOrdinaryComputersAndUnknownArchitecturesAreLeftAlone(t *testing.T) {
	plain := &Packet{Op: bootRequest, HType: 1, HLen: 6, Opts: map[byte][]byte{OptMessageType: {Discover}}}
	parsed, _ := Parse(plain.Marshal())
	if _, ok := boot().Reply(parsed); ok {
		t.Error("a computer that is not doing PXE must get no answer from a proxy")
	}
	unknown, _ := Parse(pxeRequest(Discover, 11, ""))
	if _, ok := boot().Reply(unknown); ok {
		t.Error("an architecture without a boot file must get no answer")
	}
	offer := &Packet{Op: bootReply, HType: 1, HLen: 6, Opts: map[byte][]byte{OptMessageType: {Offer}, OptVendorClass: []byte("PXEClient")}}
	parsedOffer, _ := Parse(offer.Marshal())
	if IsPXE(parsedOffer) {
		t.Error("another server's offer is not a PXE request")
	}
}

func TestThePacketSurvivesAWriteAndRead(t *testing.T) {
	req, _ := Parse(pxeRequest(Discover, 7, ""))
	reply, _ := boot().Reply(req)
	wire := reply.Marshal()
	back, err := Parse(wire)
	if err != nil {
		t.Fatal(err)
	}
	if back.File != reply.File || !bytes.Equal(back.Opts[OptVendorOpts], []byte{6, 1, 8, 255}) || string(back.Opts[OptVendorClass]) != "PXEClient" {
		t.Errorf("lost something: %+v", back)
	}
	if !bytes.Equal(wire, back.Marshal()) {
		t.Error("writing the same packet twice must give the same bytes")
	}
	if wire[240] != OptMessageType {
		t.Errorf("the message type must be the first option, got %d", wire[240])
	}
}

func TestBrokenPacketsAreRefusedNotTrusted(t *testing.T) {
	good := pxeRequest(Discover, 0, "")
	if _, err := Parse(good[:100]); err == nil {
		t.Error("a short packet must be refused")
	}
	noCookie := append([]byte(nil), good...)
	noCookie[236] = 0
	if _, err := Parse(noCookie); err == nil {
		t.Error("a packet without the DHCP magic cookie must be refused")
	}
	truncated := append(append([]byte(nil), good[:240]...), OptVendorClass, 200, 'P')
	if _, err := Parse(truncated); err == nil {
		t.Error("an option that claims more bytes than the packet has must be refused")
	}
}

func TestRepliesGoToTheRightPlace(t *testing.T) {
	req := &Packet{CIAddr: net.IPv4zero, GIAddr: net.IPv4zero}
	if d := Destination(req, 68); !d.IP.Equal(net.IPv4bcast) || d.Port != 68 {
		t.Errorf("broadcast expected, got %v", d)
	}
	req.CIAddr = net.IPv4(10, 0, 0, 5)
	if d := Destination(req, 68); !d.IP.Equal(net.IPv4(10, 0, 0, 5)) || d.Port != 68 {
		t.Errorf("the client's own address expected, got %v", d)
	}
	req.GIAddr = net.IPv4(10, 0, 0, 1)
	if d := Destination(req, 68); !d.IP.Equal(net.IPv4(10, 0, 0, 1)) || d.Port != 67 {
		t.Errorf("the relay expected, got %v", d)
	}
}

func TestAPCSentToFreeBSDsLoaderGetsTheLoaderAsBootFileFromItsFirmwareOnly(t *testing.T) {
	b := boot()
	b.Handoff = func(mac string, efi bool) (string, bool) {
		if mac != "00:15:5d:08:02:2d" {
			return "", false
		}
		if efi {
			return "boot/loader.efi", true
		}
		return "boot/pxeboot", true
	}
	req, _ := Parse(pxeRequest(Discover, 7, ""))
	if reply, ok := b.Reply(req); !ok || reply.File != "boot/loader.efi" {
		t.Fatalf("efi: %+v %v", reply, ok)
	}
	req, _ = Parse(pxeRequest(Discover, 0, ""))
	if reply, _ := b.Reply(req); reply.File != "boot/pxeboot" {
		t.Fatalf("bios: %q", reply.File)
	}
	req, _ = Parse(pxeRequest(Discover, 7, "iPXE"))
	if reply, _ := b.Reply(req); reply.File != "http://192.168.1.10:8080/boot.ipxe" {
		t.Fatalf("a client that already runs iPXE keeps its script: %q", reply.File)
	}
}
