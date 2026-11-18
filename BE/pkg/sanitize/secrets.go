package sanitize

import (
	"bytes"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// minSecretFragment is the shortest line of a multiline secret, or string
// value of a JSON secret, that is redacted on its own.
const minSecretFragment = 8

// SecretReplacer redacts known credentials, their JSON-escaped forms, the
// meaningful lines of a multiline credential and the string values of a JSON
// credential. Longest-first replacement avoids leaking a suffix when secret
// values overlap.
//
// A line is redacted on its own only when it could carry a credential: at
// least minSecretFragment characters once trimmed, with a letter or digit.
// Otherwise a pretty-printed JSON credential, such as the Codex sign-in, would
// redact every "{" and "}" of the output (KUA-227). Its tokens are covered by
// its string values instead, wherever they are printed.
func SecretReplacer(secrets []string) *strings.Replacer {
	variants := map[string]bool{}
	add := func(secret string) {
		for _, form := range secretForms(secret) {
			variants[form] = true
			// A multiline credential can cross logical records; redact each
			// meaningful fragment too, rather than persisting an early
			// fragment before the rest.
			for _, line := range strings.FieldsFunc(form, func(r rune) bool { return r == '\n' || r == '\r' }) {
				if line = strings.TrimSpace(line); meaningfulFragment(line) {
					variants[line] = true
				}
			}
		}
	}
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		add(secret)
		for _, value := range jsonStringValues(secret) {
			if meaningfulFragment(strings.TrimSpace(value)) {
				add(value)
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

// secretForms are the secret and its JSON-escaped forms, with and without
// HTML escaping, since other JSON writers leave <, > and & as they are.
func secretForms(secret string) []string {
	encoded, _ := json.Marshal(secret)
	forms := []string{secret, string(encoded[1 : len(encoded)-1])}
	var plain bytes.Buffer
	encoder := json.NewEncoder(&plain)
	encoder.SetEscapeHTML(false)
	if encoder.Encode(secret) == nil {
		if form := strings.TrimSuffix(plain.String(), "\n"); len(form) >= 2 {
			forms = append(forms, form[1:len(form)-1])
		}
	}
	return forms
}

// meaningfulFragment reports whether a trimmed fragment of a secret could
// carry a credential, rather than being punctuation like "{" or "},".
func meaningfulFragment(fragment string) bool {
	return utf8.RuneCountInString(fragment) >= minSecretFragment && strings.IndexFunc(fragment, func(r rune) bool {
		return unicode.IsLetter(r) || unicode.IsDigit(r)
	}) >= 0
}

// jsonStringValues are the string values of a secret that is a JSON
// document, nested ones included; nil when it is not JSON.
func jsonStringValues(secret string) []string {
	var document any
	if json.Unmarshal([]byte(secret), &document) != nil {
		return nil
	}
	var values []string
	var walk func(any)
	walk = func(value any) {
		switch typed := value.(type) {
		case string:
			values = append(values, typed)
		case []any:
			for _, item := range typed {
				walk(item)
			}
		case map[string]any:
			for _, item := range typed {
				walk(item)
			}
		}
	}
	walk(document)
	return values
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
