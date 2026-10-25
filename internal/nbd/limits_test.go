package nbd

import (
	"io"
	"net"
	"testing"
	"time"
)

// A flood of connections must not make more clients than the limit: the extra ones are closed before they get a greeting.
func TestOnlyAFewClientsAreServedAtOnce(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go (&Server{Open: func(string) (Export, bool) { return Export{}, false }}).Serve(l) //nolint:errcheck
	var conns []net.Conn
	for i := 0; i < maxSessions+5; i++ {
		c, err := net.Dial("tcp", l.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		conns = append(conns, c)
		defer c.Close()
	}
	closed, greeted := 0, 0
	for i, c := range conns {
		if i != 0 && i < maxSessions { // (a sample of those within the limit is enough)
			continue
		}
		c.SetReadDeadline(time.Now().Add(time.Second)) //nolint:errcheck
		n, err := c.Read(make([]byte, 18))
		switch {
		case n == 18:
			greeted++
		case err == io.EOF:
			closed++
		}
	}
	if closed != 5 || greeted != 1 {
		t.Errorf("%d connections above the limit were closed (want 5) and %d within it were greeted (want 1)", closed, greeted)
	}
}
