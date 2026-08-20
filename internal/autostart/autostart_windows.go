//go:build windows

package autostart

import (
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

const (
	hkeyCurrentUser = 0x80000001
	keySetValue     = 0x0002
	regSZ           = 1
)

var (
	advapi32            = syscall.NewLazyDLL("advapi32.dll")
	procRegCreateKeyExW = advapi32.NewProc("RegCreateKeyExW")
	procRegSetValueExW  = advapi32.NewProc("RegSetValueExW")
	procRegCloseKey     = advapi32.NewProc("RegCloseKey")
)

// EnsureEnabled registers the current executable in the per-user Run key.
// It is intentionally idempotent and does not require elevation.
func EnsureEnabled(valueName string) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return err
	}
	keyPath, err := syscall.UTF16PtrFromString(`Software\Microsoft\Windows\CurrentVersion\Run`)
	if err != nil {
		return err
	}
	name, err := syscall.UTF16PtrFromString(valueName)
	if err != nil {
		return err
	}
	command, err := syscall.UTF16FromString(commandForExecutable(executable))
	if err != nil {
		return err
	}
	var key uintptr
	result, _, _ := procRegCreateKeyExW.Call(
		hkeyCurrentUser,
		uintptr(unsafe.Pointer(keyPath)),
		0,
		0,
		0,
		keySetValue,
		0,
		uintptr(unsafe.Pointer(&key)),
		0,
	)
	if result != 0 {
		return syscall.Errno(result)
	}
	defer procRegCloseKey.Call(key)
	result, _, _ = procRegSetValueExW.Call(
		key,
		uintptr(unsafe.Pointer(name)),
		0,
		regSZ,
		uintptr(unsafe.Pointer(&command[0])),
		uintptr(len(command)*2),
	)
	if result != 0 {
		return syscall.Errno(result)
	}
	return nil
}
