package admin

import (
	"errors"
	"net"
	"testing"
)

func TestAWakePacketIsSentForAKnownMACOnly(t *testing.T) {
	r := newRig(t)
	r.login()
	if code, _ := r.do("POST", "/api/pcs/aa:bb:cc:dd:ee:01/wake", map[string]any{}, nil); code != 404 {
		t.Fatalf("without a way to send: %d", code)
	}
	var sent []string
	r.admin.o.Wake = func(mac net.HardwareAddr) error { sent = append(sent, mac.String()); return nil }
	if code, _ := r.do("POST", "/api/pcs/not-a-mac/wake", map[string]any{}, nil); code != 400 || len(sent) != 0 {
		t.Fatalf("a bad MAC: %d %v", code, sent)
	}
	if code, _ := r.do("POST", "/api/pcs/AA-BB-CC-DD-EE-01/wake", map[string]any{}, nil); code != 200 || len(sent) != 1 || sent[0] != "aa:bb:cc:dd:ee:01" {
		t.Fatalf("%d %v", code, sent)
	}
	r.admin.o.Wake = func(net.HardwareAddr) error { return errors.New("network is down") }
	if code, out := r.do("POST", "/api/pcs/aa:bb:cc:dd:ee:01/wake", map[string]any{}, nil); code != 502 || out["error"] == nil {
		t.Fatalf("%d %v", code, out)
	}
}
