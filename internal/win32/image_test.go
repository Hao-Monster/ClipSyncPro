package win32

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestClipboardDIBBMPRoundTrip(t *testing.T) {
	dib := make([]byte, 40+8)
	binary.LittleEndian.PutUint32(dib[0:4], 40)
	binary.LittleEndian.PutUint32(dib[4:8], 2)
	binary.LittleEndian.PutUint32(dib[8:12], 1)
	binary.LittleEndian.PutUint16(dib[12:14], 1)
	binary.LittleEndian.PutUint16(dib[14:16], 32)
	bmp, err := clipboardBMPFromDIB(dib)
	if err != nil {
		t.Fatal(err)
	}
	got, format, err := clipboardDIBFromBMP(bmp)
	if err != nil {
		t.Fatal(err)
	}
	if format != cfDIB || !bytes.Equal(got, dib) {
		t.Fatalf("DIB round trip mismatch, format=%d", format)
	}
	if offset := binary.LittleEndian.Uint32(bmp[10:14]); offset != 54 {
		t.Fatalf("unexpected BMP pixel offset: %d", offset)
	}
}

func TestClipboardPNGToDIB(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.NRGBA{R: 255, A: 255})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	dib, err := clipboardDIBFromPNG(encoded.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(dib) != 40+16 || binary.LittleEndian.Uint16(dib[14:16]) != 32 {
		t.Fatalf("unexpected DIB header or size: %d", len(dib))
	}
}
