// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package diskimg

import (
	"bufio"
	"errors"
	"fmt"
	"io"
)

// maxDecoderWindow is the largest history window (Zstandard) or dictionary (XZ) the server accepts when it unpacks a compressed disk image. Images made with the
// usual settings use 8 to 64 MiB; the limit keeps a small hostile file from asking the decoder for gigabytes before the free-space check can help.
const maxDecoderWindow = 128 << 20

// xzPeek is how much of the start of an XZ file checkXZDictionary needs: the 12-byte stream header and the first block header (at most 1024 bytes, usually 12 to 20).
const xzPeek = 12 + 1024

var errXZNotChecked = errors.New("xz header too short to check")

// openXZ returns a reader for an XZ stream after refusing a stream whose first block asks for a dictionary larger than maxDecoderWindow. The library raises its own
// limit to whatever the file declares, so the file's claim has to be read before the decoder is created. Only the first block header is read: a stream whose later blocks
// ask for more is not caught here (the XZ tools write the same dictionary size in every block), and that is a known limit.
func openXZ(r io.Reader, newReader func(io.Reader) (io.Reader, error)) (io.Reader, error) {
	br := bufio.NewReaderSize(r, xzPeek)
	head, err := br.Peek(xzPeek)
	if err != nil && err != io.EOF && err != bufio.ErrBufferFull {
		return nil, err
	}
	if err := checkXZDictionary(head); err != nil && !errors.Is(err, errXZNotChecked) {
		return nil, err
	}
	return newReader(br)
}

// checkXZDictionary reads the dictionary size the first block of an XZ file declares (LZMA2 filter) and refuses one above maxDecoderWindow. Bytes that are not an XZ
// stream, or too short to read, are left to the decoder to reject.
func checkXZDictionary(head []byte) error {
	magic := []byte{0xFD, '7', 'z', 'X', 'Z', 0x00}
	if len(head) < 13 || string(head[:6]) != string(magic) {
		return errXZNotChecked
	}
	block := head[12:]
	if block[0] == 0 { // an index follows: an empty stream
		return nil
	}
	size := (int(block[0]) + 1) * 4
	if size > len(block) {
		return errXZNotChecked
	}
	block = block[:size]
	flags := block[1]
	pos := 2
	varint := func() (uint64, bool) {
		var v uint64
		for i := 0; i < 9; i++ {
			if pos >= len(block) {
				return 0, false
			}
			b := block[pos]
			pos++
			v |= uint64(b&0x7F) << (7 * uint(i))
			if b&0x80 == 0 {
				return v, true
			}
		}
		return 0, false
	}
	if flags&0x40 != 0 { // compressed size
		if _, ok := varint(); !ok {
			return errXZNotChecked
		}
	}
	if flags&0x80 != 0 { // uncompressed size
		if _, ok := varint(); !ok {
			return errXZNotChecked
		}
	}
	for f := 0; f <= int(flags&0x03); f++ {
		id, ok := varint()
		if !ok {
			return errXZNotChecked
		}
		n, ok := varint()
		if !ok || pos+int(n) > len(block) {
			return errXZNotChecked
		}
		props := block[pos : pos+int(n)]
		pos += int(n)
		if id != 0x21 || len(props) != 1 { // only LZMA2 carries a dictionary size
			continue
		}
		d := uint64(props[0] & 0x3F)
		if d > 40 {
			return fmt.Errorf("xz dictionary size code %d is invalid", d)
		}
		dict := uint64(0xFFFFFFFF)
		if d < 40 {
			dict = (2 | (d & 1)) << (d/2 + 11)
		}
		if dict > maxDecoderWindow {
			return fmt.Errorf("the xz file needs a dictionary of %d MiB, more than the %d MiB the server accepts: repack it with a smaller dictionary (for example xz --lzma2=dict=64MiB) or unpack it outside Bootlace", dict>>20, maxDecoderWindow>>20)
		}
	}
	return nil
}
