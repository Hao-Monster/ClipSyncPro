package win32

import "testing"

func TestClipboardWriteSequenceIsConsumedExactlyOnce(t *testing.T) {
	markClipboardWrite(42)
	if ConsumeClipboardWrite(41) {
		t.Fatal("suppressed a different sequence")
	}
	if !ConsumeClipboardWrite(42) {
		t.Fatal("did not suppress the app-originated sequence")
	}
	if ConsumeClipboardWrite(42) {
		t.Fatal("suppressed the same sequence more than once")
	}
}
