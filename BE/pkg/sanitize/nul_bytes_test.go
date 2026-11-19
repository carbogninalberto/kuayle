package sanitize

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStripNULRemovesEveryNUL(t *testing.T) {
	require.Equal(t, "ab", StripNUL("a\x00b\x00"))
	require.Equal(t, "", StripNUL("\x00"))
	require.Equal(t, "plain", StripNUL("plain"))
}

func TestPostgresTextReplacesNULAndInvalidUTF8(t *testing.T) {
	require.Equal(t, "a�b�c", PostgresText("a\x00b\xed\xa0\x80c"))
	require.Equal(t, `\u0000 😀`, PostgresText(`\u0000 😀`))
}

func TestPostgresJSON(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"nested string", `{"item":{"text":"a\u0000b"}}`, `{"item":{"text":"a\ufffdb"}}`},
		{"object keys", `{"k\u0000":"v","k\uDFFF":"w"}`, `{"k\ufffd":"v","k\ufffd":"w"}`},
		{"nested arrays", `["a\uD800", ["\uDC00b", "\u0000"]]`, `["a\ufffd", ["\ufffdb", "\ufffd"]]`},
		{"valid pair beside lone surrogate", `"\uD800\uD83D\uDE00"`, `"\ufffd\uD83D\uDE00"`},
		{"low then high surrogate", `"\uDC00\uD800"`, `"\ufffd\ufffd"`},
		{"backslashes", `["\\u0000","\\\u0000","\\uD800"]`, `["\\u0000","\\\ufffd","\\uD800"]`},
		{"ordinary Unicode escape", `"\u20ac"`, `"\u20ac"`},
		{"preserves formatting and numbers", `{ "z": 12345678901234567890, "f": 1.5e300, "a": "<b>\u0000" }`, `{ "z": 12345678901234567890, "f": 1.5e300, "a": "<b>\ufffd" }`},
		{"invalid UTF-8", "\"\xed\xb0\x80\"", "\"�\""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := []byte(tc.input)
			got := PostgresJSON(input)
			require.Equal(t, tc.want, string(got))
			require.True(t, json.Valid(got))
			require.Equal(t, tc.input, string(input))
		})
	}
}
