//go:build windows

package win32

import (
	"errors"
	"fmt"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const (
	cfUnicodeText = 13
	gmemMoveable  = 0x0002
	gmemZeroInit  = 0x0040
	maxImageBytes = 256 << 20
)

var (
	user32                   = syscall.NewLazyDLL("user32.dll")
	kernel32                 = syscall.NewLazyDLL("kernel32.dll")
	procOpenClipboard        = user32.NewProc("OpenClipboard")
	procCloseClipboard       = user32.NewProc("CloseClipboard")
	procEmptyClipboard       = user32.NewProc("EmptyClipboard")
	procIsClipboardFormat    = user32.NewProc("IsClipboardFormatAvailable")
	procGetClipboardData     = user32.NewProc("GetClipboardData")
	procSetClipboardData     = user32.NewProc("SetClipboardData")
	procRegisterClipboardFmt = user32.NewProc("RegisterClipboardFormatW")
	procGetClipboardSequence = user32.NewProc("GetClipboardSequenceNumber")
	procGlobalAlloc          = kernel32.NewProc("GlobalAlloc")
	procGlobalLock           = kernel32.NewProc("GlobalLock")
	procGlobalUnlock         = kernel32.NewProc("GlobalUnlock")
	procGlobalSize           = kernel32.NewProc("GlobalSize")
	procGlobalFree           = kernel32.NewProc("GlobalFree")
	procCopyMemory           = kernel32.NewProc("RtlMoveMemory")
	pngFormatOnce            sync.Once
	pngClipboardFormat       uint32
)

type ClipboardSnapshot struct {
	Sequence      uint32
	Text          string
	HasText       bool
	ImageData     []byte
	ImageMIMEType string
	HasImage      bool
}

func ReadClipboard() (ClipboardSnapshot, error) {
	seq := ClipboardSequenceNumber()
	if format := availableImageFormat(); format != 0 {
		data, err := readClipboardBytes(format)
		if err != nil {
			return ClipboardSnapshot{Sequence: seq}, err
		}
		mimeType := "image/png"
		if format != registeredPNGFormat() {
			data, err = clipboardBMPFromDIB(data)
			if err != nil {
				return ClipboardSnapshot{Sequence: seq}, err
			}
			mimeType = "image/bmp"
		}
		return ClipboardSnapshot{
			Sequence:      seq,
			ImageData:     data,
			ImageMIMEType: mimeType,
			HasImage:      true,
		}, nil
	}
	return readClipboardText(seq)
}

func ReadClipboardText() (ClipboardSnapshot, error) {
	return readClipboardText(ClipboardSequenceNumber())
}

func readClipboardText(seq uint32) (ClipboardSnapshot, error) {
	if ok, _, _ := procIsClipboardFormat.Call(cfUnicodeText); ok == 0 {
		return ClipboardSnapshot{Sequence: seq}, nil
	}
	if err := openClipboardWithRetry(); err != nil {
		return ClipboardSnapshot{Sequence: seq}, err
	}
	defer procCloseClipboard.Call()

	handle, _, err := procGetClipboardData.Call(cfUnicodeText)
	if handle == 0 {
		return ClipboardSnapshot{Sequence: seq}, lastError(err, "GetClipboardData failed")
	}
	ptr, _, err := procGlobalLock.Call(handle)
	if ptr == 0 {
		return ClipboardSnapshot{Sequence: seq}, lastError(err, "GlobalLock failed")
	}
	defer procGlobalUnlock.Call(handle)

	size, _, _ := procGlobalSize.Call(handle)
	if size == 0 {
		return ClipboardSnapshot{Sequence: seq, HasText: true}, nil
	}
	if size%2 != 0 {
		return ClipboardSnapshot{Sequence: seq}, errors.New("clipboard text size is invalid")
	}
	u16 := make([]uint16, int(size)/2)
	procCopyMemory.Call(uintptr(unsafe.Pointer(&u16[0])), ptr, size)
	end := 0
	for end < len(u16) && u16[end] != 0 {
		end++
	}
	return ClipboardSnapshot{
		Sequence: seq,
		Text:     syscall.UTF16ToString(u16[:end]),
		HasText:  true,
	}, nil
}

func availableImageFormat() uint32 {
	formats := []uint32{registeredPNGFormat(), cfDIBV5, cfDIB}
	for _, format := range formats {
		if format == 0 {
			continue
		}
		if ok, _, _ := procIsClipboardFormat.Call(uintptr(format)); ok != 0 {
			return format
		}
	}
	return 0
}

func registeredPNGFormat() uint32 {
	pngFormatOnce.Do(func() {
		name, _ := syscall.UTF16PtrFromString("PNG")
		format, _, _ := procRegisterClipboardFmt.Call(uintptr(unsafe.Pointer(name)))
		pngClipboardFormat = uint32(format)
	})
	return pngClipboardFormat
}

func readClipboardBytes(format uint32) ([]byte, error) {
	if err := openClipboardWithRetry(); err != nil {
		return nil, err
	}
	defer procCloseClipboard.Call()
	handle, _, err := procGetClipboardData.Call(uintptr(format))
	if handle == 0 {
		return nil, lastError(err, "GetClipboardData failed")
	}
	size, _, _ := procGlobalSize.Call(handle)
	if size == 0 || size > maxImageBytes {
		return nil, fmt.Errorf("clipboard image size is invalid: %d", size)
	}
	ptr, _, err := procGlobalLock.Call(handle)
	if ptr == 0 {
		return nil, lastError(err, "GlobalLock failed")
	}
	defer procGlobalUnlock.Call(handle)
	data := make([]byte, int(size))
	procCopyMemory.Call(uintptr(unsafe.Pointer(&data[0])), ptr, size)
	return data, nil
}

func WriteClipboardText(text string) error {
	utf16, err := syscall.UTF16FromString(text)
	if err != nil {
		return err
	}
	if err := openClipboardWithRetry(); err != nil {
		return err
	}
	defer procCloseClipboard.Call()
	if ok, _, err := procEmptyClipboard.Call(); ok == 0 {
		return lastError(err, "EmptyClipboard failed")
	}
	data := append([]byte(nil), unsafe.Slice((*byte)(unsafe.Pointer(&utf16[0])), len(utf16)*2)...)
	return setClipboardBytes(cfUnicodeText, data)
}

func WriteClipboardImage(mimeType string, data []byte) error {
	type clipboardFormatData struct {
		format uint32
		data   []byte
	}
	formats := make([]clipboardFormatData, 0, 2)
	switch mimeType {
	case "image/png":
		dib, err := clipboardDIBFromPNG(data)
		if err != nil {
			return err
		}
		formats = append(formats, clipboardFormatData{format: cfDIB, data: dib})
		if pngFormat := registeredPNGFormat(); pngFormat != 0 {
			formats = append(formats, clipboardFormatData{format: pngFormat, data: data})
		}
	case "image/bmp":
		dib, format, err := clipboardDIBFromBMP(data)
		if err != nil {
			return err
		}
		formats = append(formats, clipboardFormatData{format: format, data: dib})
	default:
		return errors.New("unsupported clipboard image type")
	}
	if err := openClipboardWithRetry(); err != nil {
		return err
	}
	defer procCloseClipboard.Call()
	if ok, _, err := procEmptyClipboard.Call(); ok == 0 {
		return lastError(err, "EmptyClipboard failed")
	}
	for _, item := range formats {
		if err := setClipboardBytes(item.format, item.data); err != nil {
			return err
		}
	}
	return nil
}

func setClipboardBytes(format uint32, data []byte) error {
	if len(data) == 0 {
		return errors.New("clipboard data is empty")
	}
	handle, _, err := procGlobalAlloc.Call(gmemMoveable|gmemZeroInit, uintptr(len(data)))
	if handle == 0 {
		return lastError(err, "GlobalAlloc failed")
	}
	owned := true
	defer func() {
		if owned {
			procGlobalFree.Call(handle)
		}
	}()
	ptr, _, err := procGlobalLock.Call(handle)
	if ptr == 0 {
		return lastError(err, "GlobalLock failed")
	}
	procCopyMemory.Call(ptr, uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)))
	procGlobalUnlock.Call(handle)
	if result, _, err := procSetClipboardData.Call(uintptr(format), handle); result == 0 {
		return lastError(err, "SetClipboardData failed")
	}
	owned = false
	return nil
}

func ClipboardSequenceNumber() uint32 {
	seq, _, _ := procGetClipboardSequence.Call()
	return uint32(seq)
}

func openClipboardWithRetry() error {
	var last syscall.Errno
	for i := 0; i < 8; i++ {
		if ok, _, err := procOpenClipboard.Call(0); ok != 0 {
			return nil
		} else if errno, ok := err.(syscall.Errno); ok {
			last = errno
		}
		time.Sleep(time.Duration(8+i*3) * time.Millisecond)
	}
	if last != 0 {
		return last
	}
	return errors.New("OpenClipboard failed")
}

func lastError(err error, fallback string) error {
	if errno, ok := err.(syscall.Errno); ok && errno != 0 {
		return errno
	}
	return errors.New(fallback)
}
