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

func TestClipboardDIBToPNGRetainsPixelsAndOrientation(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	img.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
	img.SetNRGBA(1, 0, color.NRGBA{G: 255, A: 255})
	img.SetNRGBA(0, 1, color.NRGBA{B: 255, A: 255})
	img.SetNRGBA(1, 1, color.NRGBA{R: 255, G: 255, A: 255})
	var source bytes.Buffer
	if err := png.Encode(&source, img); err != nil {
		t.Fatal(err)
	}
	dib, err := clipboardDIBFromPNG(source.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := clipboardPNGFromDIB(dib)
	if err != nil {
		t.Fatal(err)
	}
	got, err := png.Decode(bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			want := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			actual := color.NRGBAModel.Convert(got.At(x, y)).(color.NRGBA)
			if actual != want {
				t.Fatalf("pixel (%d,%d) = %#v, want %#v", x, y, actual, want)
			}
		}
	}
}

func TestClipboardDIBToPNGRejectsCompressedFormats(t *testing.T) {
	dib := make([]byte, 44)
	binary.LittleEndian.PutUint32(dib[0:4], 40)
	binary.LittleEndian.PutUint32(dib[4:8], 1)
	binary.LittleEndian.PutUint32(dib[8:12], 1)
	binary.LittleEndian.PutUint16(dib[12:14], 1)
	binary.LittleEndian.PutUint16(dib[14:16], 32)
	binary.LittleEndian.PutUint32(dib[16:20], 1)
	if _, err := clipboardPNGFromDIB(dib); err == nil {
		t.Fatal("expected unsupported compressed DIB to fail")
	}
}
