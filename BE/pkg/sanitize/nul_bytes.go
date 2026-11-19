package sanitize

import (
	"bytes"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// StripNUL removes NUL characters, which PostgreSQL text and jsonb reject.
func StripNUL(s string) string { return strings.ReplaceAll(s, "\x00", "") }

// PostgresText replaces NUL and invalid UTF-8 before storing plain text.
func PostgresText(s string) string {
	return strings.ReplaceAll(strings.ToValidUTF8(s, "\uFFFD"), "\x00", "\uFFFD")
}

// PostgresJSON replaces Unicode sequences that jsonb rejects. It changes
// escapes in place on a copy so keys, numbers, and valid escapes retain their
// order and spelling. The database still validates the JSON syntax.
func PostgresJSON(raw []byte) []byte {
	if !utf8.Valid(raw) {
		raw = bytes.ToValidUTF8(raw, []byte("\uFFFD"))
	}
	var cleaned []byte
	for offset := 0; offset < len(raw); {
		next := bytes.IndexByte(raw[offset:], '\\')
		if next < 0 {
			break
		}
		start := offset + next
		offset = start + 2 // Skip backslash escapes, including an escaped backslash.
		code := jsonEscapeRune(raw[start:])
		if code < 0 {
			continue
		}
		offset = start + 6
		surrogate := utf16.IsSurrogate(code)
		if surrogate && utf16.DecodeRune(code, jsonEscapeRune(raw[offset:])) != utf8.RuneError {
			offset += 6
			continue
		}
		if code == 0 || surrogate {
			if cleaned == nil {
				cleaned = bytes.Clone(raw)
			}
			copy(cleaned[start:offset], `\ufffd`)
		}
	}
	if cleaned != nil {
		return cleaned
	}
	return raw
}

func jsonEscapeRune(raw []byte) rune {
	if len(raw) < 6 || raw[0] != '\\' || raw[1] != 'u' {
		return -1
	}
	value, err := strconv.ParseUint(string(raw[2:6]), 16, 16)
	if err != nil {
		return -1
	}
	return rune(value)
}
