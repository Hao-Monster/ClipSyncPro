//go:build windows

package win32

import (
	"errors"
	"syscall"
	"time"
	"unsafe"
)

const (
	cfUnicodeText = 13
	gmemMoveable  = 0x0002
	gmemZeroInit  = 0x0040
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
	procGetClipboardSequence = user32.NewProc("GetClipboardSequenceNumber")
	procGlobalAlloc          = kernel32.NewProc("GlobalAlloc")
	procGlobalLock           = kernel32.NewProc("GlobalLock")
	procGlobalUnlock         = kernel32.NewProc("GlobalUnlock")
	procGlobalSize           = kernel32.NewProc("GlobalSize")
)

type ClipboardSnapshot struct {
	Sequence uint32
	Text     string
	HasText  bool
}

func ReadClipboardText() (ClipboardSnapshot, error) {
	seq := ClipboardSequenceNumber()
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
	u16 := unsafe.Slice((*uint16)(unsafe.Pointer(ptr)), int(size)/2)
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
	bytes := uintptr(len(utf16) * 2)
	handle, _, err := procGlobalAlloc.Call(gmemMoveable|gmemZeroInit, bytes)
	if handle == 0 {
		return lastError(err, "GlobalAlloc failed")
	}
	ptr, _, err := procGlobalLock.Call(handle)
	if ptr == 0 {
		return lastError(err, "GlobalLock failed")
	}
	dst := unsafe.Slice((*uint16)(unsafe.Pointer(ptr)), len(utf16))
	copy(dst, utf16)
	procGlobalUnlock.Call(handle)
	if data, _, err := procSetClipboardData.Call(cfUnicodeText, handle); data == 0 {
		return lastError(err, "SetClipboardData failed")
	}
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
