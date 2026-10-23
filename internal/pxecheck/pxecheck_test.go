package pxecheck

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/ghazichaieb/gola-bootlace/internal/dhcp"
)

func freeUDP(t *testing.T) string {
	t.Helper()
	c, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	return c.LocalAddr().String()
}

// reply is an answer of a fake server to the question it was sent.
func reply(q *dhcp.Packet, serverID string, yiaddr, file string, pxe bool) []byte {
	p := &dhcp.Packet{Op: 2, HType: 1, HLen: 6, XID: q.XID, CHAddr: q.CHAddr, File: file, Opts: map[byte][]byte{}}
	p.YIAddr, p.SIAddr, p.CIAddr, p.GIAddr = net.IPv4zero, net.IPv4zero, net.IPv4zero, net.IPv4zero
	if yiaddr != "" {
		p.YIAddr = net.ParseIP(yiaddr)
	}
	p.Opts[dhcp.OptMessageType] = []byte{dhcp.Offer}
	p.Opts[dhcp.OptServerID] = net.ParseIP(serverID).To4()
	if pxe {
		p.Opts[dhcp.OptVendorClass] = []byte("PXEClient")
	}
	return p.Marshal()
}

func TestAnswersAreToldApart(t *testing.T) {
	listen := freeUDP(t)
	server, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	go func() {
		buf := make([]byte, 1500)
		n, _, err := server.ReadFrom(buf)
		if err != nil {
			return
		}
		q, err := dhcp.Parse(buf[:n])
		if err != nil || string(q.Opts[dhcp.OptVendorClass][:9]) != "PXEClient" || q.Opts[dhcp.OptClientArch][1] != 7 || q.Flags&0x8000 == 0 {
			return // (a question that is not a PXE discover with the broadcast flag gets no answers, and the test fails)
		}
		to, _ := net.ResolveUDPAddr("udp4", listen)
		server.WriteTo(reply(q, "10.0.0.1", "10.0.0.50", "", false), to) // the network's own DHCP server
		server.WriteTo(reply(q, "10.0.0.7", "", "ipxe.efi", true), to)   // a proxy that boots PCs
		server.WriteTo(reply(q, "127.0.0.1", "", "ipxe.efi", true), to)  // this server
		server.WriteTo(reply(q, "10.0.0.7", "", "ipxe.efi", true), to)   // (the same one again: counted once)
		stray := reply(q, "10.0.0.9", "10.0.0.51", "", false)            // an answer to another question
		stray[4] ^= 0xff
		server.WriteTo(stray, to)
	}()
	res, err := Scan(context.Background(), Options{Local: net.ParseIP("127.0.0.1"), ListenAddr: listen, SendTo: server.LocalAddr().String(), Wait: 700 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Answers) != 3 || !res.Answers[0].Self {
		t.Fatalf("this server comes first and every answer once: %+v", res.Answers)
	}
	byID := map[string]Answer{}
	for _, a := range res.Answers {
		byID[a.ServerID] = a
	}
	if a := byID["10.0.0.1"]; a.Conflict || a.Offered != "10.0.0.50" || a.PXE {
		t.Errorf("the network's DHCP server is normal in proxy mode: %+v", a)
	}
	if a := byID["10.0.0.7"]; !a.Conflict || !a.PXE || a.BootFile != "ipxe.efi" {
		t.Errorf("another PXE server is a conflict: %+v", a)
	}
	if byID["127.0.0.1"].Conflict || res.Conflicts() != 1 {
		t.Errorf("this server is no conflict: %+v", res.Answers)
	}

	// in full mode (this server hands out addresses) the other DHCP server is a conflict too
	listen = freeUDP(t)
	go func() {
		buf := make([]byte, 1500)
		n, _, _ := server.ReadFrom(buf)
		q, err := dhcp.Parse(buf[:n])
		if err != nil {
			return
		}
		to, _ := net.ResolveUDPAddr("udp4", listen)
		server.WriteTo(reply(q, "10.0.0.1", "10.0.0.50", "", false), to)
	}()
	res, err = Scan(context.Background(), Options{Local: net.ParseIP("127.0.0.1"), Full: true, ListenAddr: listen, SendTo: server.LocalAddr().String(), Wait: 500 * time.Millisecond})
	if err != nil || res.Conflicts() != 1 {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestNobodyAnsweringIsAnEmptyListNotAnError(t *testing.T) {
	silent, _ := net.ListenPacket("udp4", "127.0.0.1:0")
	defer silent.Close()
	res, err := Scan(context.Background(), Options{Local: net.ParseIP("127.0.0.1"), ListenAddr: freeUDP(t), SendTo: silent.LocalAddr().String(), Wait: 300 * time.Millisecond})
	if err != nil || len(res.Answers) != 0 || res.Conflicts() != 0 {
		t.Fatalf("%+v %v", res, err)
	}
}
