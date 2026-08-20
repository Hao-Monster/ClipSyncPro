package win32

import "sync/atomic"

var clipboardWriteSequence atomic.Uint64

func markClipboardWrite(sequence uint32) {
	clipboardWriteSequence.Store(uint64(sequence)<<1 | 1)
}

// ConsumeClipboardWrite reports an app-originated Windows clipboard sequence
// exactly once. Keeping this in the Win32 boundary covers every product UI,
// including the native fallback client.
func ConsumeClipboardWrite(sequence uint32) bool {
	want := uint64(sequence)<<1 | 1
	return clipboardWriteSequence.CompareAndSwap(want, 0)
}
