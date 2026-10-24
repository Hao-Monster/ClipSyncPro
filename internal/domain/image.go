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

const MaxClipboardImageBytes = 64 << 20

func NewClipboardImageRecord(userID, deviceID string, platform Platform, mimeType string, data []byte, now time.Time) (ClipboardRecord, error) {
	if len(data) == 0 || len(data) > MaxClipboardImageBytes {
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
		ImageMIMEType:   mimeType,
		ImageData:       append([]byte(nil), data...),
		ContentHash:     hex.EncodeToString(sum[:]),
		IsSensitive:     false,
		SourceDeviceID:  deviceID,
		SourcePlatform:  platform,
		ClientCreatedAt: now,
		UpdatedAt:       now,
		SyncStatus:      SyncPending,
	}, nil
}

// DecodeClipboardRecordImage reads the binary representation first and falls
// back to the legacy data URL used by older databases and clients.
func DecodeClipboardRecordImage(rec ClipboardRecord) (mimeType string, data []byte, err error) {
	if rec.ContentType != ContentImage {
		return "", nil, ErrInvalidImageContent
	}
	if len(rec.ImageData) > 0 {
		mimeType = normalizeImageMIME(rec.ImageMIMEType)
		if mimeType == "" {
			return "", nil, ErrInvalidImageContent
		}
		return mimeType, rec.ImageData, nil
	}
	return DecodeClipboardImage(rec.ContentText)
}

// ClipboardRecordForWire keeps the existing JSON contract while the local
// store uses BLOBs. List endpoints must continue to use summary records so
// this conversion only happens for explicit single-record sync operations.
func ClipboardRecordForWire(rec ClipboardRecord) (ClipboardRecord, error) {
	if rec.ContentType != ContentImage {
		return rec, nil
	}
	if rec.ContentText == "" {
		mimeType, data, err := DecodeClipboardRecordImage(rec)
		if err != nil {
			return ClipboardRecord{}, err
		}
		rec.ContentText = "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(data)
	}
	rec.ImageMIMEType = ""
	rec.ImageData = nil
	return rec, nil
}

// ClipboardRecordForStorage normalizes both new in-process images and legacy
// wire images to the binary representation used by local stores.
func ClipboardRecordForStorage(rec ClipboardRecord) (ClipboardRecord, error) {
	if rec.ContentType != ContentImage {
		return rec, nil
	}
	mimeType, data, err := DecodeClipboardRecordImage(rec)
	if err != nil {
		return ClipboardRecord{}, err
	}
	rec.ContentText = ""
	rec.ImageMIMEType = mimeType
	rec.ImageData = append([]byte(nil), data...)
	return rec, nil
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
	if err != nil || len(data) == 0 || len(data) > MaxClipboardImageBytes {
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
