package autostart

import "testing"

func TestCommandForExecutableQuotesPath(t *testing.T) {
	got := commandForExecutable(`C:\Program Files\ClipSync Pro\ClipSyncPro.exe`)
	want := `"C:\Program Files\ClipSync Pro\ClipSyncPro.exe"`
	if got != want {
		t.Fatalf("command = %q, want %q", got, want)
	}
}
