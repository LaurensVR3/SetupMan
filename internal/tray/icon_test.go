package tray

import (
	"bytes"
	"encoding/binary"
	"image/png"
	"os"
	"testing"
)

func TestIconIsValidICO(t *testing.T) {
	ico := Icon()
	var hdr struct{ Reserved, Type, Count uint16 }
	binary.Read(bytes.NewReader(ico), binary.LittleEndian, &hdr)
	if hdr.Type != 1 || hdr.Count != 1 {
		t.Fatalf("bad ico header: %+v", hdr)
	}
	img, err := png.Decode(bytes.NewReader(ico[22:]))
	if err != nil || img.Bounds().Dx() != 64 {
		t.Fatalf("embedded png: %v", err)
	}
	// Optional: write the icon as PNG to look at it (set SETUPMAN_ICON_OUT to a file path).
	if out := os.Getenv("SETUPMAN_ICON_OUT"); out != "" {
		os.WriteFile(out, ico[22:], 0o644)
	}
}
