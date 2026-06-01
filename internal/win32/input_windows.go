//go:build windows

package win32

import (
	"time"
	"unsafe"
)

const (
	inputKeyboard = 1
	keyEventKeyUp = 0x0002
	vkControl     = 0x11
	vkV           = 0x56
)

var procSendInput = user32.NewProc("SendInput")

type keyboardInput struct {
	Vk        uint16
	Scan      uint16
	Flags     uint32
	Time      uint32
	ExtraInfo uintptr
}

type input struct {
	Type    uint32
	Padding uint32
	Union   [32]byte
}

func PasteText(text string) error {
	if err := WriteClipboardText(text); err != nil {
		return err
	}
	time.Sleep(60 * time.Millisecond)
	inputs := []input{
		newKeyboardInput(vkControl, 0),
		newKeyboardInput(vkV, 0),
		newKeyboardInput(vkV, keyEventKeyUp),
		newKeyboardInput(vkControl, keyEventKeyUp),
	}
	sent, _, err := procSendInput.Call(
		uintptr(len(inputs)),
		uintptr(unsafe.Pointer(&inputs[0])),
		unsafe.Sizeof(inputs[0]),
	)
	if sent != uintptr(len(inputs)) {
		return lastError(err, "SendInput failed")
	}
	return nil
}

func newKeyboardInput(vk uint16, flags uint32) input {
	var item input
	item.Type = inputKeyboard
	ki := (*keyboardInput)(unsafe.Pointer(&item.Union[0]))
	ki.Vk = vk
	ki.Flags = flags
	return item
}
