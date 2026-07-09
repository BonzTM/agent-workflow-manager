package projectid

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// FromRoot derives a project identifier from the base name of projectRoot,
// normalized to 2-64 ASCII alphanumeric/"._-" characters starting with an
// alphanumeric (falling back to a hash-based id when normalization fails).
// It returns "" for a blank, "." or separator-only root.
func FromRoot(projectRoot string) string {
	root := strings.TrimSpace(projectRoot)
	if root == "" {
		return ""
	}
	base := filepath.Base(filepath.Clean(root))
	if base == "." || base == string(filepath.Separator) {
		return ""
	}
	return normalize(base)
}

func normalize(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if isValid(trimmed) {
		return trimmed
	}

	var builder strings.Builder
	lastSeparator := false
	for _, r := range trimmed {
		switch {
		case isASCIIAlphaNum(r):
			builder.WriteRune(r)
			lastSeparator = false
		case isASCIISeparator(r):
			if builder.Len() == 0 || lastSeparator {
				continue
			}
			builder.WriteRune(r)
			lastSeparator = true
		default:
			if builder.Len() == 0 || lastSeparator {
				continue
			}
			builder.WriteRune('-')
			lastSeparator = true
		}
	}

	out := strings.Trim(builder.String(), "._-")
	for len(out) > 0 {
		r, size := utf8.DecodeRuneInString(out)
		if isASCIIAlphaNum(r) {
			break
		}
		out = out[size:]
	}
	for len(out) > 64 {
		out = strings.TrimRight(out[:64], "._-")
	}
	if isValid(out) {
		return out
	}
	return fallback(trimmed)
}

func isValid(value string) bool {
	if len(value) < 2 || len(value) > 64 {
		return false
	}
	for i, r := range value {
		switch {
		case isASCIIAlphaNum(r):
		case isASCIISeparator(r) && i > 0:
		default:
			return false
		}
	}
	first, _ := utf8.DecodeRuneInString(value)
	return isASCIIAlphaNum(first)
}

func isASCIIAlphaNum(r rune) bool {
	return ('a' <= r && r <= 'z') || ('A' <= r && r <= 'Z') || ('0' <= r && r <= '9')
}

func isASCIISeparator(r rune) bool {
	return r == '.' || r == '_' || r == '-'
}

func fallback(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return "project-" + hex.EncodeToString(sum[:])[:8]
}
