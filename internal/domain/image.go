package domain

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

var ErrInvalidImageContent = errors.New("invalid clipboard image content")

// NewClipboardImageRecord stores binary clipboard images as a data URL so the
// existing record and sync contracts remain backwards compatible.
func NewClipboardImageRecord(userID, deviceID string, platform Platform, mimeType string, data []byte, now time.Time) (ClipboardRecord, error) {
	if len(data) == 0 {
		return ClipboardRecord{}, ErrInvalidImageContent
	}
	mimeType = normalizeImageMIME(mimeType)
	if mimeType == "" {
		return ClipboardRecord{}, ErrInvalidImageContent
	}
	id, err := NewRecordID(now)
	if err != nil {
		return ClipboardRecord{}, err
	}
	sum := sha256.Sum256(data)
	return ClipboardRecord{
		ID:              id,
		UserID:          userID,
		ContentType:     ContentImage,
		ContentText:     "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(data),
		ContentHash:     hex.EncodeToString(sum[:]),
		IsSensitive:     false,
		SourceDeviceID:  deviceID,
		SourcePlatform:  platform,
		ClientCreatedAt: now,
		UpdatedAt:       now,
		SyncStatus:      SyncPending,
	}, nil
}

func DecodeClipboardImage(content string) (mimeType string, data []byte, err error) {
	header, encoded, ok := strings.Cut(content, ",")
	if !ok || !strings.HasPrefix(header, "data:") || !strings.HasSuffix(strings.ToLower(header), ";base64") {
		return "", nil, ErrInvalidImageContent
	}
	mimeType = normalizeImageMIME(strings.TrimSuffix(strings.TrimPrefix(header, "data:"), ";base64"))
	if mimeType == "" {
		return "", nil, ErrInvalidImageContent
	}
	data, err = base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(data) == 0 {
		return "", nil, ErrInvalidImageContent
	}
	return mimeType, data, nil
}

func normalizeImageMIME(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "image/png":
		return "image/png"
	case "image/bmp", "image/x-ms-bmp":
		return "image/bmp"
	default:
		return ""
	}
}
