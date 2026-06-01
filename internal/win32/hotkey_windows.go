//go:build windows

package win32

import (
	"context"
	"os/exec"
	"unsafe"
)

const (
	modAlt   = 0x0001
	vkAltV   = 0x56
	wmHotkey = 0x0312
)

var (
	procRegisterHotKey   = user32.NewProc("RegisterHotKey")
	procUnregisterHotKey = user32.NewProc("UnregisterHotKey")
	procGetMessage       = user32.NewProc("GetMessageW")
)

type point struct {
	X int32
	Y int32
}

type msg struct {
	HWnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
}

func RegisterAltV(ctx context.Context, onPress func()) error {
	const id = 37618
	if ok, _, err := procRegisterHotKey.Call(0, id, modAlt, vkAltV); ok == 0 {
		return lastError(err, "RegisterHotKey failed")
	}
	defer procUnregisterHotKey.Call(0, id)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		var m msg
		ret, _, err := procGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(ret) == -1 {
			return lastError(err, "GetMessage failed")
		}
		if ret == 0 {
			return nil
		}
		if m.Message == wmHotkey && m.WParam == id {
			onPress()
		}
	}
}

func OpenURL(url string) error {
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}
