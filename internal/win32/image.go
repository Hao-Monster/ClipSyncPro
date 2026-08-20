package win32

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/png"
	"math/bits"
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

func clipboardPNGFromDIB(dib []byte) ([]byte, error) {
	if len(dib) < 40 {
		return nil, errInvalidClipboardImage
	}
	headerSize := int(binary.LittleEndian.Uint32(dib[0:4]))
	if headerSize < 40 || headerSize > len(dib) {
		return nil, errInvalidClipboardImage
	}
	width := int64(int32(binary.LittleEndian.Uint32(dib[4:8])))
	rawHeight := int64(int32(binary.LittleEndian.Uint32(dib[8:12])))
	if width <= 0 || rawHeight == 0 || binary.LittleEndian.Uint16(dib[12:14]) != 1 {
		return nil, errInvalidClipboardImage
	}
	height := rawHeight
	if height < 0 {
		height = -height
	}
	if width*height > 100_000_000 || width > int64(^uint(0)>>1) || height > int64(^uint(0)>>1) {
		return nil, errInvalidClipboardImage
	}
	bitCount := int(binary.LittleEndian.Uint16(dib[14:16]))
	compression := binary.LittleEndian.Uint32(dib[16:20])
	if (bitCount != 24 && bitCount != 32) || (compression != 0 && compression != 3 && compression != 6) {
		return nil, errInvalidClipboardImage
	}
	if bitCount == 24 && compression != 0 {
		return nil, errInvalidClipboardImage
	}
	offset, err := dibPixelOffset(dib)
	if err != nil {
		return nil, err
	}
	rowStride := ((width*int64(bitCount) + 31) / 32) * 4
	pixelBytes := rowStride * height
	if pixelBytes < 0 || int64(offset)+pixelBytes > int64(len(dib)) {
		return nil, errInvalidClipboardImage
	}

	redMask, greenMask, blueMask, alphaMask := uint32(0), uint32(0), uint32(0), uint32(0)
	if compression == 3 || compression == 6 {
		maskOffset := 40
		if maskOffset+12 > len(dib) || (headerSize == 40 && maskOffset+12 > offset) {
			return nil, errInvalidClipboardImage
		}
		redMask = binary.LittleEndian.Uint32(dib[maskOffset : maskOffset+4])
		greenMask = binary.LittleEndian.Uint32(dib[maskOffset+4 : maskOffset+8])
		blueMask = binary.LittleEndian.Uint32(dib[maskOffset+8 : maskOffset+12])
		if headerSize >= 56 && len(dib) >= 56 {
			alphaMask = binary.LittleEndian.Uint32(dib[52:56])
		} else if compression == 6 && headerSize == 40 && maskOffset+16 <= offset {
			alphaMask = binary.LittleEndian.Uint32(dib[maskOffset+12 : maskOffset+16])
		}
		if redMask == 0 || greenMask == 0 || blueMask == 0 {
			return nil, errInvalidClipboardImage
		}
	}

	img := image.NewNRGBA(image.Rect(0, 0, int(width), int(height)))
	useStoredAlpha := false
	if bitCount == 32 && compression == 0 {
		for y := int64(0); y < height && !useStoredAlpha; y++ {
			row := dib[int64(offset)+y*rowStride : int64(offset)+(y+1)*rowStride]
			for x := int64(0); x < width; x++ {
				if row[x*4+3] != 0 {
					useStoredAlpha = true
					break
				}
			}
		}
	}
	for sourceY := int64(0); sourceY < height; sourceY++ {
		targetY := sourceY
		if rawHeight > 0 {
			targetY = height - 1 - sourceY
		}
		row := dib[int64(offset)+sourceY*rowStride : int64(offset)+(sourceY+1)*rowStride]
		for x := int64(0); x < width; x++ {
			var r, g, b, a uint8
			if bitCount == 24 {
				pos := x * 3
				b, g, r, a = row[pos], row[pos+1], row[pos+2], 255
			} else if compression == 0 {
				pos := x * 4
				b, g, r = row[pos], row[pos+1], row[pos+2]
				a = 255
				if useStoredAlpha {
					a = row[pos+3]
				}
			} else {
				value := binary.LittleEndian.Uint32(row[x*4 : x*4+4])
				r = maskedColor(value, redMask, 0)
				g = maskedColor(value, greenMask, 0)
				b = maskedColor(value, blueMask, 0)
				a = maskedColor(value, alphaMask, 255)
			}
			img.SetNRGBA(int(x), int(targetY), color.NRGBA{R: r, G: g, B: b, A: a})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		return nil, errInvalidClipboardImage
	}
	return encoded.Bytes(), nil
}

func maskedColor(value, mask uint32, fallback uint8) uint8 {
	if mask == 0 {
		return fallback
	}
	shift := bits.TrailingZeros32(mask)
	width := bits.OnesCount32(mask)
	if width <= 0 {
		return fallback
	}
	component := (value & mask) >> shift
	maximum := uint64(1<<width) - 1
	return uint8((uint64(component)*255 + maximum/2) / maximum)
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
