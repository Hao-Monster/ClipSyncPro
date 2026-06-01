package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"hash"

	"clipsyncpro/internal/domain"
)

const (
	AlgorithmAES256GCM = "AES-256-GCM"
	KDFPBKDF2SHA256    = "PBKDF2-HMAC-SHA256"
	DefaultIterations  = 120000
)

var ErrMissingSecret = errors.New("CLIPSYNC_SECRET is required for sensitive encryption")

func EncryptString(secret, plaintext string) (*domain.CipherText, error) {
	if secret == "" {
		return nil, ErrMissingSecret
	}
	salt, err := randomBytes(16)
	if err != nil {
		return nil, err
	}
	key := pbkdf2([]byte(secret), salt, DefaultIterations, 32, sha256.New)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce, err := randomBytes(gcm.NonceSize())
	if err != nil {
		return nil, err
	}
	ciphertext := gcm.Seal(nil, nonce, []byte(plaintext), nil)
	return &domain.CipherText{
		Algorithm:  AlgorithmAES256GCM,
		KDF:        KDFPBKDF2SHA256,
		Iterations: DefaultIterations,
		Salt:       base64.StdEncoding.EncodeToString(salt),
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
		Ciphertext: base64.StdEncoding.EncodeToString(ciphertext),
	}, nil
}

func DecryptString(secret string, box *domain.CipherText) (string, error) {
	if secret == "" {
		return "", ErrMissingSecret
	}
	if box == nil || box.Ciphertext == "" {
		return "", nil
	}
	if box.Algorithm != AlgorithmAES256GCM {
		return "", errors.New("unsupported encryption algorithm")
	}
	if box.KDF != KDFPBKDF2SHA256 {
		return "", errors.New("unsupported key derivation function")
	}
	salt, err := base64.StdEncoding.DecodeString(box.Salt)
	if err != nil {
		return "", err
	}
	nonce, err := base64.StdEncoding.DecodeString(box.Nonce)
	if err != nil {
		return "", err
	}
	ciphertext, err := base64.StdEncoding.DecodeString(box.Ciphertext)
	if err != nil {
		return "", err
	}
	key := pbkdf2([]byte(secret), salt, box.Iterations, 32, sha256.New)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func randomBytes(length int) ([]byte, error) {
	buf := make([]byte, length)
	_, err := rand.Read(buf)
	return buf, err
}

func pbkdf2(password, salt []byte, iter, keyLen int, h func() hash.Hash) []byte {
	prf := hmac.New(h, password)
	hashLen := prf.Size()
	numBlocks := (keyLen + hashLen - 1) / hashLen
	var buf [4]byte
	dk := make([]byte, 0, numBlocks*hashLen)
	for block := 1; block <= numBlocks; block++ {
		prf.Reset()
		prf.Write(salt)
		buf[0] = byte(block >> 24)
		buf[1] = byte(block >> 16)
		buf[2] = byte(block >> 8)
		buf[3] = byte(block)
		prf.Write(buf[:])
		u := prf.Sum(nil)
		t := append([]byte(nil), u...)
		for i := 1; i < iter; i++ {
			prf.Reset()
			prf.Write(u)
			u = prf.Sum(nil)
			for x := range u {
				t[x] ^= u[x]
			}
		}
		dk = append(dk, t...)
	}
	return dk[:keyLen]
}
