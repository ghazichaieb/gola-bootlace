// Package gfx draws the picture a PC shows behind the boot menu: the Gola Bootlace logo on a dark background. iPXE's graphical console takes a PNG
// (UEFI builds only; the BIOS build keeps the plain text menu).
// Copyright (c) 2026 Gola Services SUARL. Licensed under GPL-3.0-or-later; see LICENSE and ADDITIONAL-TERMS.md.
package gfx

import (
	"bytes"
	_ "embed"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"sync"
)

//go:embed logo.png
var logoPNG []byte

// Width and Height are the screen size the menu asks the PC for: every UEFI firmware offers it.
const (
	Width  = 1024
	Height = 768
)

// LogoBottom is the pixel row under the logo and its rule: the menu text starts below it.
const LogoBottom = 250

var (
	once sync.Once
	out  []byte
)

// Background is the PNG shown behind the menu. It is drawn once.
func Background() []byte {
	once.Do(func() {
		var buf bytes.Buffer
		enc := png.Encoder{CompressionLevel: png.BestCompression} // iPXE refuses the pictures the default level makes
		if err := enc.Encode(&buf, draw1024()); err != nil {
			panic(err) // an image in memory: this cannot happen
		}
		out = buf.Bytes()
	})
	return out
}

func draw1024() *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, Width, Height))
	// flat: iPXE shows the picture in 8 bits per colour, and a gradient would show as bands
	draw.Draw(img, img.Bounds(), &image.Uniform{color.NRGBA{0x14, 0x15, 0x18, 0xff}}, image.Point{}, draw.Src)
	if logo, err := png.Decode(bytes.NewReader(logoPNG)); err == nil {
		const logoWidth = 560
		b := logo.Bounds()
		scaled := scale(logo, logoWidth, b.Dy()*logoWidth/b.Dx())
		x, y := (Width-scaled.Bounds().Dx())/2, 28
		draw.Draw(img, scaled.Bounds().Add(image.Pt(x, y)), scaled, image.Point{}, draw.Over)
	}
	// Gola red rule under the logo
	red := color.NRGBA{0xc1, 0x00, 0x16, 0xff}
	draw.Draw(img, image.Rect(Width/2-260, LogoBottom-6, Width/2+260, LogoBottom-4), &image.Uniform{red}, image.Point{}, draw.Src)
	return img
}

// scale shrinks (or grows) an image by averaging the pixels that fall in each new pixel; alpha is kept.
func scale(src image.Image, w, h int) *image.NRGBA {
	sb := src.Bounds()
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		y0, y1 := sb.Min.Y+y*sb.Dy()/h, sb.Min.Y+(y+1)*sb.Dy()/h
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for x := 0; x < w; x++ {
			x0, x1 := sb.Min.X+x*sb.Dx()/w, sb.Min.X+(x+1)*sb.Dx()/w
			if x1 <= x0 {
				x1 = x0 + 1
			}
			var r, g, bl, a, n uint64
			for yy := y0; yy < y1; yy++ {
				for xx := x0; xx < x1; xx++ {
					c := color.NRGBAModel.Convert(src.At(xx, yy)).(color.NRGBA)
					// weight by alpha so that transparent pixels do not darken the edges
					r += uint64(c.R) * uint64(c.A)
					g += uint64(c.G) * uint64(c.A)
					bl += uint64(c.B) * uint64(c.A)
					a += uint64(c.A)
					n++
				}
			}
			if a > 0 {
				dst.SetNRGBA(x, y, color.NRGBA{uint8(r / a), uint8(g / a), uint8(bl / a), uint8(a / n)})
			}
		}
	}
	return dst
}
