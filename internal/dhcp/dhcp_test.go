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

// FreeBSD's loader asks for an address again after the firmware has started it: a vendor class of PXEClient, but no architecture.
func loaderRequest(msg byte, serverID net.IP) *Packet {
	p := &Packet{
		Op: bootRequest, HType: 1, HLen: 6, XID: 0x1234, Flags: flagBroadcast,
		CHAddr: [16]byte{0x04, 0x42, 0x1a, 0xed, 0xec, 0xe6},
		Opts:   map[byte][]byte{OptMessageType: {msg}, OptVendorClass: []byte("PXEClient:Arch:00000:UNDI:002001")},
	}
	if serverID != nil {
		p.Opts[OptServerID] = v4(serverID)
	}
	return p
}

func loaderBoot() Boot {
	b := boot()
	b.Mask = net.CIDRMask(24, 32)
	b.LoaderAddress = func(mac string) (net.IP, bool) {
		if mac == "04:42:1a:ed:ec:e6" {
			return net.IPv4(192, 168, 8, 2).To4(), true
		}
		return nil, false
	}
	return b
}

func TestTheLoaderGetsItsAddressAndThisServerInOneAnswer(t *testing.T) {
	for _, tc := range []struct {
		msg  byte
		want byte
	}{{Discover, Offer}, {Request, ACK}} {
		reply, ok := loaderBoot().Reply(loaderRequest(tc.msg, nil))
		if !ok {
			t.Fatalf("message %d: no answer", tc.msg)
		}
		if reply.MessageType() != tc.want || !reply.YIAddr.Equal(net.IPv4(192, 168, 8, 2)) || !reply.SIAddr.Equal(net.IPv4(192, 168, 1, 10)) {
			t.Errorf("message %d: type %d yiaddr %v siaddr %v", tc.msg, reply.MessageType(), reply.YIAddr, reply.SIAddr)
		}
		if string(reply.Opts[OptSubnetMask]) != string(net.CIDRMask(24, 32)) || len(reply.Opts[OptLeaseTime]) != 4 {
			t.Errorf("message %d: mask %v lease %v", tc.msg, reply.Opts[OptSubnetMask], reply.Opts[OptLeaseTime])
		}
	}
}

func TestARequestForTheNetworksOwnServerIsLeftAlone(t *testing.T) {
	if _, ok := loaderBoot().Reply(loaderRequest(Request, net.IPv4(192, 168, 1, 1))); ok {
		t.Error("the loader chose the router's offer: this server must stay silent")
	}
	if _, ok := loaderBoot().Reply(loaderRequest(Request, net.IPv4(192, 168, 1, 10))); !ok {
		t.Error("a request to this server must be answered")
	}
}

func TestAnOrdinaryPXEBootOfThatPCIsStillAProxyAnswer(t *testing.T) {
	req, _ := Parse(pxeRequest(Discover, 7, "")) // with an architecture: the firmware, not the loader
	req.CHAddr = [16]byte{0x04, 0x42, 0x1a, 0xed, 0xec, 0xe6}
	reply, ok := loaderBoot().Reply(req)
	if !ok || !reply.YIAddr.Equal(net.IPv4zero) || reply.File != "bootlace-x64.efi" {
		t.Fatalf("got %+v ok=%v", reply, ok)
	}
	other := loaderRequest(Discover, nil) // another PC without a hand-off
	other.CHAddr = [16]byte{1, 2, 3, 4, 5, 6}
	if r, ok := loaderBoot().Reply(other); !ok || !r.YIAddr.Equal(net.IPv4zero) {
		t.Errorf("a PC with no hand-off gets the usual proxy answer, got %+v ok=%v", r, ok)
	}
}
