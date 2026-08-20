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
	if rec.ContentText != "" {
		t.Fatalf("image body was stored in content_text: %d bytes", len(rec.ContentText))
	}
	mimeType, got, err := DecodeClipboardRecordImage(rec)
	if err != nil {
		t.Fatal(err)
	}
	if mimeType != "image/png" || !bytes.Equal(got, want) {
		t.Fatalf("round trip mismatch: %q %x", mimeType, got)
	}
	wired, err := ClipboardRecordForWire(rec)
	if err != nil {
		t.Fatal(err)
	}
	if wired.ContentText == "" || len(wired.ImageData) != 0 {
		t.Fatalf("wire record did not use legacy JSON contract: %+v", wired)
	}
	mimeType, got, err = DecodeClipboardImage(wired.ContentText)
	if err != nil || mimeType != "image/png" || !bytes.Equal(got, want) {
		t.Fatalf("wire round trip mismatch: %q %x %v", mimeType, got, err)
	}
}

func TestDecodeClipboardRecordImageSupportsLegacyDataURL(t *testing.T) {
	legacy := ClipboardRecord{
		ContentType: ContentImage,
		ContentText: "data:image/png;base64,iVBORw0KGgo=",
	}
	mimeType, data, err := DecodeClipboardRecordImage(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if mimeType != "image/png" || len(data) == 0 {
		t.Fatalf("legacy image = %q %x", mimeType, data)
	}
	stored, err := ClipboardRecordForStorage(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ContentText != "" || stored.ImageMIMEType != "image/png" || len(stored.ImageData) == 0 {
		t.Fatalf("storage record was not normalized: %+v", stored)
	}
}

func TestDecodeClipboardImageRejectsNonImage(t *testing.T) {
	if _, _, err := DecodeClipboardImage("hello"); err == nil {
		t.Fatal("expected invalid image error")
	}
}

func TestClipboardImageRecordRejectsOversizedImage(t *testing.T) {
	if _, err := NewClipboardImageRecord("local", "dev", PlatformWindows, "image/png", make([]byte, MaxClipboardImageBytes+1), time.Now()); err == nil {
		t.Fatal("expected oversized image to be rejected")
	}
}
