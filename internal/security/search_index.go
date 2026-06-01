package security

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"unicode"
)

func BuildSearchIndex(secret string, values ...string) []string {
	seen := make(map[string]struct{})
	for _, value := range values {
		for _, token := range searchTokens(value) {
			seen[tokenHash(secret, token)] = struct{}{}
		}
	}
	result := make([]string, 0, len(seen))
	for token := range seen {
		result = append(result, token)
	}
	return result
}

func BuildQueryIndex(secret, keyword string) []string {
	return BuildSearchIndex(secret, keyword)
}

func tokenHash(secret, token string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(token))
	return hex.EncodeToString(mac.Sum(nil))
}

func searchTokens(value string) []string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return nil
	}
	parts := strings.FieldsFunc(normalized, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r)
	})
	seen := make(map[string]struct{})
	add := func(s string) {
		if s != "" {
			seen[s] = struct{}{}
		}
	}
	for _, part := range parts {
		add(part)
		runes := []rune(part)
		if len(runes) == 1 {
			add(part)
			continue
		}
		for n := 1; n <= 3; n++ {
			if len(runes) < n {
				continue
			}
			for i := 0; i+n <= len(runes); i++ {
				add(string(runes[i : i+n]))
			}
		}
	}
	allRunes := []rune(normalized)
	for n := 1; n <= 3; n++ {
		if len(allRunes) < n {
			continue
		}
		for i := 0; i+n <= len(allRunes); i++ {
			add(string(allRunes[i : i+n]))
		}
	}
	out := make([]string, 0, len(seen))
	for token := range seen {
		out = append(out, token)
	}
	return out
}
