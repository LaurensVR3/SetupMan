// Package tray shows SetupMan's icon in the Windows system tray (spec D-70 … D-72).
package tray

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
)

// glyphS is a 5×7 pixel "S".
var glyphS = []string{
	" ####",
	"#    ",
	"#    ",
	" ### ",
	"    #",
	"    #",
	"#### ",
}

// Icon returns SetupMan's tray icon as .ico bytes: a dark tile with a white S and a
// red-and-white kerb stripe (the app's signature), drawn in code so no image file is needed.
func Icon() []byte {
	const size = 64
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	dark := color.NRGBA{0x1a, 0x1f, 0x26, 0xff}
	red := color.NRGBA{0xd7, 0x26, 0x3d, 0xff}
	white := color.NRGBA{0xff, 0xff, 0xff, 0xff}

	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			if !insideRounded(x, y, size, 10) {
				continue // transparent corners
			}
			switch {
			case y >= size-14: // kerb stripe
				if (x/16)%2 == 0 {
					img.Set(x, y, red)
				} else {
					img.Set(x, y, white)
				}
			default:
				img.Set(x, y, dark)
			}
		}
	}
	const scale, ox, oy = 6, 17, 4
	for gy, row := range glyphS {
		for gx, c := range row {
			if c != '#' {
				continue
			}
			for dy := 0; dy < scale; dy++ {
				for dx := 0; dx < scale; dx++ {
					img.Set(ox+gx*scale+dx, oy+gy*scale+dy, white)
				}
			}
		}
	}

	var pngBuf bytes.Buffer
	_ = png.Encode(&pngBuf, img)
	return wrapICO(pngBuf.Bytes(), size)
}

func insideRounded(x, y, size, r int) bool {
	cx, cy := x, y
	switch {
	case x < r && y < r:
		cx, cy = r, r
	case x >= size-r && y < r:
		cx, cy = size-r-1, r
	case x < r && y >= size-r:
		cx, cy = r, size-r-1
	case x >= size-r && y >= size-r:
		cx, cy = size-r-1, size-r-1
	default:
		return true
	}
	dx, dy := x-cx, y-cy
	return dx*dx+dy*dy <= r*r
}

// wrapICO puts one PNG image into an .ico container (supported since Windows Vista).
func wrapICO(pngData []byte, size int) []byte {
	var b bytes.Buffer
	binary.Write(&b, binary.LittleEndian, struct{ Reserved, Type, Count uint16 }{0, 1, 1})
	binary.Write(&b, binary.LittleEndian, struct {
		W, H, Colors, Reserved uint8
		Planes, BPP            uint16
		Size, Offset           uint32
	}{uint8(size), uint8(size), 0, 0, 1, 32, uint32(len(pngData)), 6 + 16})
	b.Write(pngData)
	return b.Bytes()
}
