package sanitize

import (
	"strings"
	"testing"
)

func TestRedactCredentialsRemovesKnownFormatsAndURLPasswords(t *testing.T) {
	token := "ghp_" + strings.Repeat("a", 36)
	got := RedactCredentials("push as " + token + " to https://bot:s3cr3tpass@github.com/acme/app.git, docs use https://user:<password>@host/")
	want := "push as [REDACTED] to https://bot:[REDACTED]@github.com/acme/app.git, docs use https://user:<password>@host/"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if plain := "Run `make check` before pushing."; RedactCredentials(plain) != plain {
		t.Fatalf("plain text changed: %q", RedactCredentials(plain))
	}
}
