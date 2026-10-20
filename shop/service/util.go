package service

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode"

	"github.com/Mxucc/StorFlow/shop/store"
)

// newToken returns n random bytes rendered as lowercase hex.
func newToken(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%x", n)
	}
	return hex.EncodeToString(buf)
}

// newID returns a fresh module identifier.
func newID() string { return store.NewID() }

// slugify converts a shop name into a URL friendly slug. Non ASCII characters
// are dropped, which keeps the result safe in URLs; an empty result falls back
// to the user id based default computed by the caller.
func slugify(raw string) string {
	var sb strings.Builder
	lastDash := true
	for _, r := range strings.ToLower(strings.TrimSpace(raw)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			sb.WriteRune(r)
			lastDash = false
		case unicode.IsSpace(r), r == '-', r == '_', r == '.':
			if !lastDash && sb.Len() > 0 {
				sb.WriteByte('-')
				lastDash = true
			}
		}
		if sb.Len() >= 48 {
			break
		}
	}
	return strings.Trim(sb.String(), "-")
}

// encodePathToken turns a relative folder path into an opaque, URL safe token so
// that a storefront visitor cannot reconstruct a storflow:// URI by hand.
func encodePathToken(path string) string {
	if path == "" {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString([]byte(strings.Trim(path, "/")))
}

// decodePathToken reverses encodePathToken. Invalid input yields an empty path,
// which means "the root of the product folder".
func decodePathToken(token string) (string, error) {
	if token == "" {
		return "", nil
	}

	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return "", fmt.Errorf("invalid path token: %w", err)
	}

	path := strings.Trim(string(raw), "/")
	for _, segment := range strings.Split(path, "/") {
		if segment == "" || segment == "." || segment == ".." || strings.ContainsAny(segment, "/\\\x00") {
			return "", fmt.Errorf("invalid path token")
		}
	}
	return path, nil
}
