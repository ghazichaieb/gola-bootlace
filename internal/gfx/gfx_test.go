package gfx

import (
	"bytes"
	"image/png"
	"testing"
)

func TestBackground(t *testing.T) {
	img, err := png.Decode(bytes.NewReader(Background()))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != Width || b.Dy() != Height {
		t.Fatalf("size %v", b)
	}
	if len(Background()) > 400<<10 {
		t.Fatalf("the picture is %d bytes: too much to fetch at every boot", len(Background()))
	}
}
