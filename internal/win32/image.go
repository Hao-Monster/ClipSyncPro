package win32

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image/color"
	"image/png"
)

var errInvalidClipboardImage = errors.New("invalid clipboard image")

const (
	cfDIB   = 8
	cfDIBV5 = 17
)

func clipboardBMPFromDIB(dib []byte) ([]byte, error) {
	offset, err := dibPixelOffset(dib)
	if err != nil {
		return nil, err
	}
	bmp := make([]byte, 14+len(dib))
	copy(bmp[:2], "BM")
	binary.LittleEndian.PutUint32(bmp[2:6], uint32(len(bmp)))
	binary.LittleEndian.PutUint32(bmp[10:14], uint32(14+offset))
	copy(bmp[14:], dib)
	return bmp, nil
}

func clipboardDIBFromBMP(bmp []byte) ([]byte, uint32, error) {
	if len(bmp) < 18 || string(bmp[:2]) != "BM" {
		return nil, 0, errInvalidClipboardImage
	}
	headerSize := binary.LittleEndian.Uint32(bmp[14:18])
	if headerSize < 12 || int(14+headerSize) > len(bmp) {
		return nil, 0, errInvalidClipboardImage
	}
	format := uint32(cfDIB)
	if headerSize >= 124 {
		format = cfDIBV5
	}
	dib := append([]byte(nil), bmp[14:]...)
	return dib, format, nil
}

func clipboardDIBFromPNG(data []byte) ([]byte, error) {
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, errInvalidClipboardImage
	}
	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width <= 0 || height <= 0 || int64(width)*int64(height) > 100_000_000 {
		return nil, errInvalidClipboardImage
	}
	pixelBytes := int64(width) * int64(height) * 4
	if pixelBytes > int64(^uint(0)>>1)-40 {
		return nil, errInvalidClipboardImage
	}
	dib := make([]byte, 40+int(pixelBytes))
	binary.LittleEndian.PutUint32(dib[0:4], 40)
	binary.LittleEndian.PutUint32(dib[4:8], uint32(width))
	binary.LittleEndian.PutUint32(dib[8:12], uint32(height))
	binary.LittleEndian.PutUint16(dib[12:14], 1)
	binary.LittleEndian.PutUint16(dib[14:16], 32)
	binary.LittleEndian.PutUint32(dib[20:24], uint32(pixelBytes))

	for y := 0; y < height; y++ {
		sourceY := bounds.Min.Y + height - 1 - y
		row := dib[40+y*width*4 : 40+(y+1)*width*4]
		for x := 0; x < width; x++ {
			pixel := color.NRGBAModel.Convert(img.At(bounds.Min.X+x, sourceY)).(color.NRGBA)
			pos := x * 4
			row[pos] = pixel.B
			row[pos+1] = pixel.G
			row[pos+2] = pixel.R
			row[pos+3] = pixel.A
		}
	}
	return dib, nil
}

func dibPixelOffset(dib []byte) (int, error) {
	if len(dib) < 12 {
		return 0, errInvalidClipboardImage
	}
	headerSize := int(binary.LittleEndian.Uint32(dib[:4]))
	if headerSize < 12 || headerSize > len(dib) {
		return 0, errInvalidClipboardImage
	}
	if headerSize == 12 {
		bitCount := int(binary.LittleEndian.Uint16(dib[10:12]))
		colors := 0
		if bitCount > 0 && bitCount <= 8 {
			colors = 1 << bitCount
		}
		offset := headerSize + colors*3
		if offset > len(dib) {
			return 0, errInvalidClipboardImage
		}
		return offset, nil
	}
	if headerSize < 40 || len(dib) < 40 {
		return 0, errInvalidClipboardImage
	}
	bitCount := int(binary.LittleEndian.Uint16(dib[14:16]))
	compression := binary.LittleEndian.Uint32(dib[16:20])
	colors := int(binary.LittleEndian.Uint32(dib[32:36]))
	if colors == 0 && bitCount > 0 && bitCount <= 8 {
		colors = 1 << bitCount
	}
	extraMasks := 0
	if headerSize == 40 {
		switch compression {
		case 3:
			extraMasks = 12
		case 6:
			extraMasks = 16
		}
	}
	offset := headerSize + extraMasks + colors*4
	if offset > len(dib) {
		return 0, errInvalidClipboardImage
	}
	return offset, nil
}
