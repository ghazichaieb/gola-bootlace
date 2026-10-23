package dhcp

import (
	"net"
	"testing"
)

func httpBootRequest(msg byte, arch uint16) *Packet {
	p := &Packet{
		Op: bootRequest, HType: 1, HLen: 6, XID: 0x1234abcd, Flags: flagBroadcast,
		CHAddr: [16]byte{0x52, 0x54, 0x00, 0x12, 0x34, 0x56},
		Opts: map[byte][]byte{
			OptMessageType: {msg},
			OptVendorClass: []byte("HTTPClient:Arch:00016:UNDI:003001"),
			OptClientArch:  {byte(arch >> 8), byte(arch)},
		},
	}
	req, err := Parse(p.Marshal())
	if err != nil {
		panic(err)
	}
	return req
}

func TestAUefiHttpBootClientIsToldTheWebAddressOfTheLoader(t *testing.T) {
	b := boot()
	b.HTTPBase = "http://192.168.1.10:8080"
	for _, tc := range []struct {
		msg  byte
		kind byte
	}{{Discover, Offer}, {Request, ACK}} {
		reply, ok := b.Reply(httpBootRequest(tc.msg, 7))
		if !ok {
			t.Fatalf("message %d: no answer", tc.msg)
		}
		want := "http://192.168.1.10:8080/ipxe/bootlace-x64.efi"
		if string(reply.Opts[OptBootFile]) != want || reply.File != want || reply.MessageType() != tc.kind {
			t.Errorf("message %d: option 67 %q, file %q, type %d", tc.msg, reply.Opts[OptBootFile], reply.File, reply.MessageType())
		}
		if string(reply.Opts[OptVendorClass]) != "HTTPClient" {
			t.Errorf("the firmware looks for the vendor class HTTPClient: %q", reply.Opts[OptVendorClass])
		}
		if !reply.YIAddr.Equal(net.IPv4zero) || !reply.SIAddr.Equal(net.IPv4zero) || reply.XID != 0x1234abcd {
			t.Errorf("a proxy answer gives no address: yiaddr %v siaddr %v", reply.YIAddr, reply.SIAddr)
		}
		if _, has := reply.Opts[OptVendorOpts]; has {
			t.Error("the PXE sub-options mean nothing to an HTTP boot client")
		}
	}
}

func TestHttpBootIsAnsweredOnlyWhenSetUpAndForAnArchitectureWithAFile(t *testing.T) {
	b := boot() // no HTTPBase
	if _, ok := b.Reply(httpBootRequest(Discover, 7)); ok {
		t.Error("without a web address there is nothing to say")
	}
	b.HTTPBase = "http://192.168.1.10:8080"
	if _, ok := b.Reply(httpBootRequest(Discover, 11)); ok {
		t.Error("ARM64 has no loader here")
	}
	if _, ok := b.Reply(httpBootRequest(Discover, 0)); ok {
		t.Error("a BIOS PC cannot HTTP boot")
	}
	// an ordinary PXE client is answered as before, and an ordinary computer is left alone
	req, _ := Parse(pxeRequest(Discover, 7, ""))
	if reply, ok := b.Reply(req); !ok || reply.File != "bootlace-x64.efi" {
		t.Errorf("PXE is unchanged: %v %v", ok, reply)
	}
	plain := httpBootRequest(Discover, 7)
	plain.Opts[OptVendorClass] = []byte("MSFT 5.0")
	if _, ok := b.Reply(plain); ok {
		t.Error("an ordinary computer is left to the network's DHCP server")
	}
}

func TestAnHttpBootClientIsTreatedLikeAPXEOneByAServerThatHandsOutAddresses(t *testing.T) {
	b := boot()
	b.HTTPBase = "http://192.168.1.10:8080"
	f := &Full{PXEOnly: true, Boot: b, First: net.IPv4(192, 168, 1, 100), Last: net.IPv4(192, 168, 1, 120), Mask: net.CIDRMask(24, 32), Lease: 3600}
	reply, ok := f.Respond(httpBootRequest(Discover, 7))
	if !ok {
		t.Fatal("no answer")
	}
	if reply.YIAddr.To4() == nil || reply.YIAddr.Equal(net.IPv4zero) {
		t.Errorf("the PC needs an address too: %v", reply.YIAddr)
	}
	if string(reply.Opts[OptBootFile]) != "http://192.168.1.10:8080/ipxe/bootlace-x64.efi" || string(reply.Opts[OptVendorClass]) != "HTTPClient" {
		t.Errorf("option 67 %q, class %q", reply.Opts[OptBootFile], reply.Opts[OptVendorClass])
	}
}
