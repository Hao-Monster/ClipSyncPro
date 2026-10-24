package storage

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"testing"
)

func TestPrepareLegacyImageCandidateValidatesAndDecodes(t *testing.T) {
	data := []byte{0x89, 'P', 'N', 'G', 1, 2, 3}
	sum := sha256.Sum256(data)
	candidate := legacyImageCandidate{
		id:          "rec_1",
		contentText: "data:image/png;base64," + base64.StdEncoding.EncodeToString(data),
		contentHash: hex.EncodeToString(sum[:]),
	}
	if err := prepareLegacyImageCandidate(&candidate); err != nil {
		t.Fatal(err)
	}
	if candidate.mimeType != "image/png" || string(candidate.data) != string(data) {
		t.Fatalf("decoded candidate = %#v", candidate)
	}
}

func TestPrepareLegacyImageCandidateRejectsHashMismatch(t *testing.T) {
	candidate := legacyImageCandidate{
		id:          "rec_1",
		contentText: "data:image/png;base64,iVBORw0KGgo=",
		contentHash: "not-the-image-hash",
	}
	if err := prepareLegacyImageCandidate(&candidate); err == nil {
		t.Fatal("expected hash mismatch")
	}
}
