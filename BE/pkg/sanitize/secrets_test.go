package sanitize

import (
	"encoding/json"
	"strings"
	"testing"
)

// fakeCodexAuth is shaped like the Codex sign-in file (auth.json), which
// Codex writes pretty-printed. Every value is fake.
const fakeCodexAuth = `{
  "OPENAI_API_KEY": null,
  "tokens": {
    "id_token": "fake-id-token-0001",
    "access_token": "fake-access-token-0001",
    "refresh_token": "fake-refresh-token-0001",
    "account_id": "fake-account-id-0001"
  },
  "last_refresh": "2026-11-18T09:00:00.000000Z"
}`

var fakeCodexTokens = []string{"fake-id-token-0001", "fake-access-token-0001", "fake-refresh-token-0001", "fake-account-id-0001"}

func jsonLine(t *testing.T, value any) string {
	t.Helper()
	line, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(line)
}

// Production (KUA-227): the lines "{" and "}" of the Codex sign-in were
// redaction keys, so every brace of a run's JSON output became [REDACTED] and
// the reviewer's verdict was lost.
func TestSecretReplacerKeepsThePunctuationOfAMultilineJSONSecret(t *testing.T) {
	replacer := SecretReplacer([]string{fakeCodexAuth})
	for _, text := range []string{
		`{"type":"item.completed","item":{"id":"item_3","type":"agent_message","text":"Verdict: approve\n\nNo findings."}}`,
		"func main() {\n\tfmt.Println([]int{1, 2})\n}\n",
		"[\n  {\n    \"a\": [1],\n  },\n],\n}",
		"  },\n  ],\n",
	} {
		if got := replacer.Replace(text); got != text {
			t.Errorf("Replace(%q) = %q, want it unchanged", text, got)
		}
	}
}

// No value of a JSON credential is left in output, however it is printed: on
// its own, inside a JSON event, or JSON-escaped with the rest of the file.
func TestSecretReplacerRedactsEveryValueOfAJSONSecret(t *testing.T) {
	replacer := SecretReplacer([]string{fakeCodexAuth})
	firstLines := strings.Join(strings.Split(fakeCodexAuth, "\n")[:5], "\n")
	for _, token := range fakeCodexTokens {
		for name, text := range map[string]string{
			"alone":           token,
			"in plain text":   "Authorization: Bearer " + token + "\n",
			"in a JSON event": jsonLine(t, map[string]any{"type": "item.completed", "item": map[string]any{"type": "agent_message", "text": "The token is " + token + "."}}),
			"in the escaped file": jsonLine(t, map[string]any{"type": "item.completed", "item": map[string]any{"type": "command_execution", "command": "cat ~/.codex/auth.json",
				"aggregated_output": fakeCodexAuth + "\n"}}),
			"in part of the escaped file": jsonLine(t, map[string]any{"type": "item.completed", "item": map[string]any{"type": "command_execution", "command": "head -5 ~/.codex/auth.json",
				"aggregated_output": firstLines + "\n"}}),
		} {
			got := replacer.Replace(text)
			if strings.Contains(got, token) {
				t.Errorf("%s: %q leaks %s", name, got, token)
			}
			if strings.HasPrefix(text, "{") && !json.Valid([]byte(got)) {
				t.Errorf("%s: redaction broke the JSON event: %q", name, got)
			}
		}
	}
	if got := replacer.Replace(fakeCodexAuth); got != "[REDACTED]" {
		t.Errorf("the whole secret is redacted as one: got %q", got)
	}
	escaped := jsonLine(t, fakeCodexAuth)
	if got := replacer.Replace(escaped); got != `"[REDACTED]"` {
		t.Errorf("the whole escaped secret is redacted as one: got %q", got)
	}
}

// Values are found in nested objects and arrays, and a value that JSON
// escapes is also redacted in its escaped form.
func TestSecretReplacerRedactsNestedAndEscapedJSONValues(t *testing.T) {
	secret := `{"accounts":[{"password":"fake\"pass\\word-0001"}],"pin":"1234"}`
	value := "fake\"pass\\word-0001"
	replacer := SecretReplacer([]string{secret})
	for _, text := range []string{value, jsonLine(t, map[string]string{"text": "password " + value})} {
		got := replacer.Replace(text)
		if strings.Contains(got, "word-0001") {
			t.Errorf("Replace(%q) = %q leaks the password", text, got)
		}
	}
	if got := replacer.Replace("pin 1234"); got != "pin 1234" {
		t.Errorf("a value under 8 characters is not a key on its own: got %q", got)
	}
}

// A multiline credential that is not JSON keeps its meaningful lines as keys,
// so a line printed on its own is still redacted.
func TestSecretReplacerRedactsMeaningfulLinesOfAMultilineSecret(t *testing.T) {
	secret := "-----BEGIN FAKE KEY-----\nZmFrZS1rZXktbGluZS0wMDAx\n  ZmFrZS1rZXktbGluZS0wMDAy  \n}\n---\n-----END FAKE KEY-----\n"
	replacer := SecretReplacer([]string{secret})
	for text, want := range map[string]string{
		"ZmFrZS1rZXktbGluZS0wMDAx":                     "[REDACTED]",
		"key: ZmFrZS1rZXktbGluZS0wMDAy\n":              "key: [REDACTED]\n",
		"-----BEGIN FAKE KEY-----":                     "[REDACTED]",
		"if ok {\n\treturn\n}\n---\n":                  "if ok {\n\treturn\n}\n---\n",
		secret:                                         "[REDACTED]",
		strings.ReplaceAll(secret, "\n", "\r\n") + "x": "[REDACTED]\r\n[REDACTED]\r\n  [REDACTED]  \r\n}\r\n---\r\n[REDACTED]\r\nx",
	} {
		if got := replacer.Replace(text); got != want {
			t.Errorf("Replace(%q) = %q, want %q", text, got, want)
		}
	}
}

// Longest first: a secret that extends another is redacted whole, so no
// suffix of it is left.
func TestSecretReplacerRedactsOverlappingSecretsLongestFirst(t *testing.T) {
	replacer := SecretReplacer([]string{"fake-overlap-0001", "fake-overlap-0001-suffix", ""})
	if got := replacer.Replace("a fake-overlap-0001-suffix b fake-overlap-0001 c"); got != "a [REDACTED] b [REDACTED] c" {
		t.Errorf("got %q", got)
	}
}
