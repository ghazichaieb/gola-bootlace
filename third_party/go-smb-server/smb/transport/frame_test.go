package transport

import (
	"errors"
	"testing"
)

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

// steps feeds the bytes in the given pieces and answers a timeout between them, like a socket with a read deadline whose data arrives late.
type steps struct {
	pieces [][]byte
	late   bool
}

func (s *steps) Read(p []byte) (int, error) {
	if s.late {
		s.late = false
		return 0, timeoutErr{}
	}
	if len(s.pieces) == 0 {
		return 0, errors.New("end")
	}
	n := copy(p, s.pieces[0])
	if n < len(s.pieces[0]) {
		s.pieces[0] = s.pieces[0][n:]
	} else {
		s.pieces = s.pieces[1:]
	}
	s.late = true
	return n, nil
}

// A timeout in the middle of a frame must not lose what was already read.
func TestReadMessageGoesOnAfterATimeoutInTheMiddleOfAFrame(t *testing.T) {
	f := &FramedConn{r: &steps{pieces: [][]byte{{0x00, 0x00}, {0x00, 0x05, 'h', 'e'}, {'l', 'l', 'o'}, {0x00, 0x00, 0x00, 0x02, 'o', 'k'}}}}
	var got []string
	for i := 0; i < 40 && len(got) < 2; i++ {
		msg, err := f.ReadMessage()
		if err != nil {
			var ne interface{ Timeout() bool }
			if errors.As(err, &ne) && ne.Timeout() {
				continue
			}
			t.Fatal(err)
		}
		got = append(got, string(msg))
	}
	if len(got) != 2 || got[0] != "hello" || got[1] != "ok" {
		t.Fatalf("got %q", got)
	}
}
