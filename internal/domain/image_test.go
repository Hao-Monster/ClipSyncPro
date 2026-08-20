package domain

import (
	"bytes"
	"testing"
	"time"
)

func TestClipboardImageRecordRoundTrip(t *testing.T) {
	want := []byte{0x89, 'P', 'N', 'G', 1, 2, 3, 4}
	rec, err := NewClipboardImageRecord("local", "dev", PlatformWindows, "image/png", want, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if rec.ContentType != ContentImage || RedactedSummary(rec) != "图片" {
		t.Fatalf("unexpected image record: %+v", rec)
	}
	mimeType, got, err := DecodeClipboardImage(rec.ContentText)
	if err != nil {
		t.Fatal(err)
	}
	if mimeType != "image/png" || !bytes.Equal(got, want) {
		t.Fatalf("round trip mismatch: %q %x", mimeType, got)
	}
}

func TestDecodeClipboardImageRejectsNonImage(t *testing.T) {
	if _, _, err := DecodeClipboardImage("hello"); err == nil {
		t.Fatal("expected invalid image error")
	}
}
