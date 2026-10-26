package sanitize

import (
	"encoding/json"
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
