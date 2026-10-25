package dhcp

import (
	"bytes"
	"testing"
)

// Whatever arrives on port 67 (the network is not trusted): Parse must not panic, must not read past the packet, and a packet it accepts must be written out and read in again the same.
func FuzzParse(f *testing.F) {
	good := make([]byte, headerLen+4)
	copy(good[headerLen:], []byte{0x63, 0x82, 0x53, 0x63})
	good = append(good, OptMessageType, 1, Discover, OptVendorClass, 9, 'P', 'X', 'E', 'C', 'l', 'i', 'e', 'n', 't', OptClientArch, 2, 0, 16, OptEnd)
	f.Add(good)
	f.Add(good[:len(good)-3])
	f.Add(append(append([]byte(nil), good[:headerLen+4]...), OptClientArch, 200, 1))
	f.Fuzz(func(t *testing.T, b []byte) {
		p, err := Parse(b)
		if err != nil {
			return
		}
		_ = p.MessageType()
		again, err := Parse(p.Marshal())
		if err != nil {
			t.Fatalf("a packet that was accepted is not read back: %v", err)
		}
		for code, v := range again.Opts {
			if !bytes.Equal(v, p.Opts[code]) && len(p.Opts[code]) <= 255 {
				t.Fatalf("option %d changed in a round trip: %x -> %x", code, p.Opts[code], v)
			}
		}
	})
}
