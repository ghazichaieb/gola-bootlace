package dhcp

import (
	"net"
	"testing"
	"time"
)

// echo answers every request with an offer (to a discover) or an acknowledgement (to a request) carrying the same transaction.
type echo struct{}

func (echo) Respond(req *Packet) (*Packet, bool) {
	reply := discover(1, false)
	reply.Op, reply.XID, reply.Flags = bootReply, req.XID, 0
	reply.Opts = map[byte][]byte{OptMessageType: {Offer}}
	if req.MessageType() == Request {
		reply.Opts[OptMessageType] = []byte{ACK}
	}
	return reply, true
}

func TestOnlyADiscoverIsHeldBack(t *testing.T) {
	d := Delayed{Responder: echo{}, Offer: 300 * time.Millisecond}
	asked := discover(1, false)
	if got := d.Delay(asked); got != 300*time.Millisecond {
		t.Fatalf("a discover is held back %v, want 300ms", got)
	}
	asked.Opts[OptMessageType] = []byte{Request}
	if got := d.Delay(asked); got != 0 {
		t.Fatalf("a request is held back %v: only the offer is", got)
	}
}

func TestNoDelayLeavesTheResponderAsItIs(t *testing.T) {
	if _, wrapped := WithOfferDelay(echo{}, 0).(Delayed); wrapped {
		t.Fatal("a zero delay wrapped the responder")
	}
	if WithOfferDelay(nil, time.Second) != nil {
		t.Fatal("DHCP that is off became a responder")
	}
}

// TestTheOfferArrivesAfterTheDelayAndTheAcknowledgementAtOnce runs the real serve loop on the loopback address: the PC's socket is on port 68, where answers
// to a client that has an address go. The test is skipped where that port cannot be opened (a user without the right to bind a port below 1024).
func TestTheOfferArrivesAfterTheDelayAndTheAcknowledgementAtOnce(t *testing.T) {
	pc, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 68})
	if err != nil {
		t.Skipf("cannot open port 68: %v", err)
	}
	defer pc.Close()
	srv, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	go Serve(srv, WithOfferDelay(echo{}, 300*time.Millisecond), nil)

	ask := func(typ byte) (byte, time.Duration) {
		req := discover(1, false)
		req.Flags = 0 // the answer goes to the PC's own address, port 68
		req.Opts[OptMessageType] = []byte{typ}
		start := time.Now()
		if _, err := pc.WriteToUDP(req.Marshal(), srv.LocalAddr().(*net.UDPAddr)); err != nil {
			t.Fatal(err)
		}
		_ = pc.SetReadDeadline(time.Now().Add(3 * time.Second))
		buf := make([]byte, 1500)
		n, _, err := pc.ReadFromUDP(buf)
		if err != nil {
			t.Fatalf("no answer to message type %d: %v", typ, err)
		}
		reply, err := Parse(buf[:n])
		if err != nil {
			t.Fatal(err)
		}
		return reply.MessageType(), time.Since(start)
	}

	if typ, took := ask(Request); typ != ACK || took > 200*time.Millisecond {
		t.Fatalf("the acknowledgement was type %d after %v, want an ACK at once", typ, took)
	}
	if typ, took := ask(Discover); typ != Offer || took < 280*time.Millisecond {
		t.Fatalf("the offer was type %d after %v, want an offer after the delay", typ, took)
	}
}
