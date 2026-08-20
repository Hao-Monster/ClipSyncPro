package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"clipsyncpro/internal/domain"
	"clipsyncpro/internal/security"
	"clipsyncpro/internal/storage"
)

var (
	ErrAccountRequired   = errors.New("账户不能为空")
	ErrPasswordRequired  = errors.New("密码不能为空")
	ErrRevealRequired    = errors.New("请先点击查看")
	ErrSensitiveRequired = errors.New("仅账户/密码记录支持修改备注")
	ErrTextRequired      = errors.New("图片不能标记为账户或密码")
)

type Service struct {
	store    storage.Store
	secret   string
	userID   string
	deviceID string
	platform domain.Platform
	now      func() time.Time
}

type Config struct {
	Secret   string
	UserID   string
	DeviceID string
	Platform domain.Platform
}

func NewService(store storage.Store, cfg Config) *Service {
	userID := cfg.UserID
	if userID == "" {
		userID = "local"
	}
	deviceID := cfg.DeviceID
	if deviceID == "" {
		deviceID = "windows"
	}
	platform := cfg.Platform
	if platform == "" {
		platform = domain.PlatformWindows
	}
	return &Service{
		store:    store,
		secret:   cfg.Secret,
		userID:   userID,
		deviceID: deviceID,
		platform: platform,
		now:      time.Now,
	}
}

func (s *Service) CaptureText(ctx context.Context, content string) (domain.ClipboardRecord, error) {
	now := s.now()
	rec, err := domain.NewClipboardRecord(s.userID, s.deviceID, s.platform, content, now)
	if err != nil {
		return domain.ClipboardRecord{}, err
	}
	if err := s.store.UpsertRecord(ctx, rec); err != nil {
		return domain.ClipboardRecord{}, err
	}
	if err := s.enqueueRecord(ctx, storage.QueueCreate, rec); err != nil {
		return domain.ClipboardRecord{}, err
	}
	return rec, nil
}

func (s *Service) CaptureImage(ctx context.Context, mimeType string, data []byte) (domain.ClipboardRecord, error) {
	now := s.now()
	rec, err := domain.NewClipboardImageRecord(s.userID, s.deviceID, s.platform, mimeType, data, now)
	if err != nil {
		return domain.ClipboardRecord{}, err
	}
	if err := s.store.UpsertRecord(ctx, rec); err != nil {
		return domain.ClipboardRecord{}, err
	}
	if err := s.enqueueRecord(ctx, storage.QueueCreate, rec); err != nil {
		return domain.ClipboardRecord{}, err
	}
	return rec, nil
}

func (s *Service) List(ctx context.Context, filter storage.Filter) ([]domain.ClipboardRecord, error) {
	if filter.UserID == "" {
		filter.UserID = s.userID
	}
	if filter.Keyword != "" {
		filter.KeywordHashes = security.BuildQueryIndex(s.secret, filter.Keyword)
	}
	return s.store.ListRecords(ctx, filter)
}

func (s *Service) MarkPassword(ctx context.Context, id, account, remark string) (domain.ClipboardRecord, error) {
	account = strings.TrimSpace(account)
	if account == "" {
		return domain.ClipboardRecord{}, ErrAccountRequired
	}
	return s.markSensitive(ctx, id, domain.SensitivePassword, account, "", remark)
}

func (s *Service) MarkAccount(ctx context.Context, id, password, remark string) (domain.ClipboardRecord, error) {
	password = strings.TrimSpace(password)
	if password == "" {
		return domain.ClipboardRecord{}, ErrPasswordRequired
	}
	return s.markSensitive(ctx, id, domain.SensitiveAccount, "", password, remark)
}

func (s *Service) markSensitive(ctx context.Context, id string, kind domain.SensitiveType, account, password, remark string) (domain.ClipboardRecord, error) {
	rec, err := s.store.GetRecord(ctx, id)
	if err != nil {
		return domain.ClipboardRecord{}, err
	}
	if rec.ContentType == domain.ContentImage {
		return domain.ClipboardRecord{}, ErrTextRequired
	}
	content := rec.ContentText
	if rec.IsSensitive {
		revealed, err := s.Reveal(ctx, id)
		if err != nil {
			return domain.ClipboardRecord{}, err
		}
		content = revealed.Content
		if account == "" {
			account = revealed.Account
		}
		if password == "" {
			password = revealed.Password
		}
	}
	if kind == domain.SensitivePassword {
		password = content
	} else {
		account = content
	}
	contentBox, err := security.EncryptString(s.secret, content)
	if err != nil {
		return domain.ClipboardRecord{}, err
	}
	accountBox, err := security.EncryptString(s.secret, account)
	if err != nil {
		return domain.ClipboardRecord{}, err
	}
	passwordBox, err := security.EncryptString(s.secret, password)
	if err != nil {
		return domain.ClipboardRecord{}, err
	}
	remarkBox, err := security.EncryptString(s.secret, remark)
	if err != nil {
		return domain.ClipboardRecord{}, err
	}
	rec.ContentText = ""
	rec.ContentCipher = contentBox
	rec.AccountCipher = accountBox
	rec.PasswordCipher = passwordBox
	rec.RemarkCipher = remarkBox
	rec.IsSensitive = true
	rec.SensitiveType = kind
	rec.SearchIndex = security.BuildSearchIndex(s.secret, content, account, password, remark)
	rec.UpdatedAt = s.now()
	rec.SyncStatus = domain.SyncPending
	if err := s.store.UpsertRecord(ctx, rec); err != nil {
		return domain.ClipboardRecord{}, err
	}
	if err := s.enqueueRecord(ctx, storage.QueueUpdate, rec); err != nil {
		return domain.ClipboardRecord{}, err
	}
	return rec, nil
}

func (s *Service) Reveal(ctx context.Context, id string) (domain.RevealedSensitive, error) {
	rec, err := s.store.GetRecord(ctx, id)
	if err != nil {
		return domain.RevealedSensitive{}, err
	}
	if !rec.IsSensitive {
		return domain.RevealedSensitive{
			RecordID: rec.ID,
			Content:  rec.ContentText,
			Expires:  s.now().Add(30 * time.Minute).Format(time.RFC3339),
		}, nil
	}
	content, err := security.DecryptString(s.secret, rec.ContentCipher)
	if err != nil {
		return domain.RevealedSensitive{}, err
	}
	account, err := security.DecryptString(s.secret, rec.AccountCipher)
	if err != nil {
		return domain.RevealedSensitive{}, err
	}
	password, err := security.DecryptString(s.secret, rec.PasswordCipher)
	if err != nil {
		return domain.RevealedSensitive{}, err
	}
	remark, err := security.DecryptString(s.secret, rec.RemarkCipher)
	if err != nil {
		return domain.RevealedSensitive{}, err
	}
	return domain.RevealedSensitive{
		RecordID: rec.ID,
		Content:  content,
		Account:  account,
		Password: password,
		Remark:   remark,
		Expires:  s.now().Add(30 * time.Minute).Format(time.RFC3339),
	}, nil
}

func (s *Service) UpdateRemark(ctx context.Context, id, remark string) (domain.ClipboardRecord, error) {
	rec, err := s.store.GetRecord(ctx, id)
	if err != nil {
		return domain.ClipboardRecord{}, err
	}
	if !rec.IsSensitive {
		return domain.ClipboardRecord{}, ErrSensitiveRequired
	}
	content, err := security.DecryptString(s.secret, rec.ContentCipher)
	if err != nil {
		return domain.ClipboardRecord{}, err
	}
	account, err := security.DecryptString(s.secret, rec.AccountCipher)
	if err != nil {
		return domain.ClipboardRecord{}, err
	}
	password, err := security.DecryptString(s.secret, rec.PasswordCipher)
	if err != nil {
		return domain.ClipboardRecord{}, err
	}
	remarkBox, err := security.EncryptString(s.secret, remark)
	if err != nil {
		return domain.ClipboardRecord{}, err
	}
	rec.RemarkCipher = remarkBox
	rec.SearchIndex = security.BuildSearchIndex(s.secret, content, account, password, remark)
	rec.UpdatedAt = s.now()
	rec.SyncStatus = domain.SyncPending
	if err := s.store.UpsertRecord(ctx, rec); err != nil {
		return domain.ClipboardRecord{}, err
	}
	if err := s.enqueueRecord(ctx, storage.QueueUpdate, rec); err != nil {
		return domain.ClipboardRecord{}, err
	}
	return rec, nil
}

func (s *Service) Delete(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	if err := s.store.DeleteRecords(ctx, ids); err != nil {
		return err
	}
	payload, err := json.Marshal(map[string][]string{"ids": ids})
	if err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := s.store.Enqueue(ctx, storage.SyncQueueItem{
			RecordID:  id,
			Action:    storage.QueueDelete,
			Payload:   string(payload),
			Status:    storage.QueuePending,
			CreatedAt: s.now(),
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) enqueueRecord(ctx context.Context, action storage.QueueAction, rec domain.ClipboardRecord) error {
	payload, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	_, err = s.store.Enqueue(ctx, storage.SyncQueueItem{
		RecordID:  rec.ID,
		Action:    action,
		Payload:   string(payload),
		Status:    storage.QueuePending,
		CreatedAt: s.now(),
	})
	return err
}
