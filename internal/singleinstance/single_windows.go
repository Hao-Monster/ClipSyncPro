//go:build windows

package singleinstance

import (
	"syscall"
	"unsafe"
)

const (
	errorAlreadyExists = 183
	swRestore          = 9
)

var (
	kernel32                = syscall.NewLazyDLL("kernel32.dll")
	user32                  = syscall.NewLazyDLL("user32.dll")
	procCreateMutex         = kernel32.NewProc("CreateMutexW")
	procReleaseMutex        = kernel32.NewProc("ReleaseMutex")
	procCloseHandle         = kernel32.NewProc("CloseHandle")
	procFindWindow          = user32.NewProc("FindWindowW")
	procShowWindow          = user32.NewProc("ShowWindow")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
)

type Lock struct {
	handle uintptr
}

func Acquire(name string) (*Lock, bool, error) {
	handle, _, err := procCreateMutex.Call(0, 1, uintptr(unsafe.Pointer(utf16Ptr(name))))
	if handle == 0 {
		return nil, false, err
	}
	if errno, ok := err.(syscall.Errno); ok && errno == errorAlreadyExists {
		return &Lock{handle: handle}, true, nil
	}
	return &Lock{handle: handle}, false, nil
}

func (l *Lock) Release() {
	if l == nil || l.handle == 0 {
		return
	}
	procReleaseMutex.Call(l.handle)
	procCloseHandle.Call(l.handle)
	l.handle = 0
}

func ActivateExistingWindow(className, title string) bool {
	hwnd, _, _ := procFindWindow.Call(
		uintptr(unsafe.Pointer(utf16Ptr(className))),
		uintptr(unsafe.Pointer(utf16Ptr(title))),
	)
	if hwnd == 0 {
		return false
	}
	procShowWindow.Call(hwnd, swRestore)
	procSetForegroundWindow.Call(hwnd)
	return true
}

func utf16Ptr(s string) *uint16 {
	ptr, _ := syscall.UTF16PtrFromString(s)
	return ptr
}
