package agent

import "testing"

func TestClipboardWriteGuardSuppressesOnlyExactSelfWrittenSequence(t *testing.T) {
	var sequence uint32 = 100
	guard := newClipboardWriteGuard(func() uint32 { return sequence })

	err := guard.write(func() error {
		sequence = 101
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !guard.shouldSuppress(101) {
		t.Fatal("self-written clipboard sequence was not suppressed")
	}
	if guard.shouldSuppress(101) {
		t.Fatal("suppression must be consumed after one observation")
	}
	if guard.shouldSuppress(102) {
		t.Fatal("external clipboard sequence must not be suppressed")
	}
}

func TestClipboardWriteGuardDoesNotSuppressFailedWrite(t *testing.T) {
	var sequence uint32 = 200
	guard := newClipboardWriteGuard(func() uint32 { return sequence })
	wantErr := errTestClipboardWrite

	if err := guard.write(func() error { return wantErr }); err != wantErr {
		t.Fatalf("write error = %v, want %v", err, wantErr)
	}
	if guard.shouldSuppress(200) {
		t.Fatal("failed write must not suppress a clipboard sequence")
	}
}

type clipboardWriteError string

func (e clipboardWriteError) Error() string { return string(e) }

const errTestClipboardWrite = clipboardWriteError("write failed")
