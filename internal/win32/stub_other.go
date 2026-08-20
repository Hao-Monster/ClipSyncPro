//go:build !windows

package win32

import (
	"context"
	"errors"
	"os/exec"
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
	return ClipboardSnapshot{}, errors.New("clipboard is only implemented on Windows")
}

func ReadClipboardText() (ClipboardSnapshot, error) {
	return ClipboardSnapshot{}, errors.New("clipboard is only implemented on Windows")
}

func WriteClipboardText(string) error {
	return errors.New("clipboard is only implemented on Windows")
}

func WriteClipboardImage(string, []byte) error {
	return errors.New("clipboard is only implemented on Windows")
}

func ClipboardSequenceNumber() uint32 {
	return 0
}

func PasteText(string) error {
	return errors.New("paste is only implemented on Windows")
}

func PasteImage(string, []byte) error {
	return errors.New("paste is only implemented on Windows")
}

func RegisterAltV(ctx context.Context, onPress func()) error {
	<-ctx.Done()
	return ctx.Err()
}

func OpenURL(url string) error {
	return exec.Command("xdg-open", url).Start()
}
