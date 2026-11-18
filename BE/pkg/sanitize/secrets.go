package sanitize

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"
)

// SecretReplacer redacts known credentials, escaped forms and multiline fragments.
// Longest-first replacement avoids leaking a suffix when secret values overlap.
func SecretReplacer(secrets []string) *strings.Replacer {
	variants := map[string]bool{}
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		encoded, _ := json.Marshal(secret)
		for _, form := range []string{secret, string(encoded[1 : len(encoded)-1])} {
			variants[form] = true
			// A multiline credential can cross logical records; redact each nonempty
			// fragment too, rather than persisting an early fragment before the rest.
			for _, line := range strings.FieldsFunc(form, func(r rune) bool { return r == '\n' || r == '\r' }) {
				variants[line] = true
			}
		}
	}
	keys := make([]string, 0, len(variants))
	for key := range variants {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if len(keys[i]) != len(keys[j]) {
			return len(keys[i]) > len(keys[j])
		}
		return keys[i] < keys[j]
	})
	pairs := make([]string, 0, len(keys)*2)
	for _, key := range keys {
		pairs = append(pairs, key, "[REDACTED]")
	}
	return strings.NewReplacer(pairs...)
}

// dsnUserinfo matches the password of a URL userinfo, any scheme:
// scheme://user:password@ or scheme://:password@. A URL without credentials
// has no user:password@ to match, and the password class excludes the slash,
// so the path is never eaten.
var dsnUserinfo = regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.-]*://[^\s:/@"'<>]*:)([^\s@/"'<>]+)(@)`)

// dsnKeyValue matches a key/value DSN password: password=..., PASSWORD=...,
// PGPASSWORD=..., DB_PASSWORD=..., SOURCE_PASSWORD=..., passwd=... The
// optional separator underscore covers the prefixed spellings; PWD= never
// matches.
var dsnKeyValue = regexp.MustCompile(`(?i)\b((?:pg|db|mysql|source_)?_?password|passwd)=([^\s'"]+)`)

// RedactDSNs masks credentials a migration tool might echo: the password of
// a URL userinfo (any scheme) and key/value DSN password forms. User names,
// hosts and paths stay; a URL without credentials and plain text are
// untouched.
func RedactDSNs(content string) string {
	content = dsnUserinfo.ReplaceAllString(content, "${1}[REDACTED]${3}")
	return dsnKeyValue.ReplaceAllString(content, "$1=[REDACTED]")
}
