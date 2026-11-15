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

func TestJSONWithoutNULRemovesEscapedNULFromStringsAndKeys(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"nested string value", `{"type":"item.completed","item":{"text":"a\u0000b"}}`, `{"item":{"text":"ab"},"type":"item.completed"}`},
		{"object key", `{"k\u0000":"v"}`, `{"k":"v"}`},
		{"array element", `["a\u0000", ["\u0000b"]]`, `["a",["b"]]`},
		{"top-level string", `"x\u0000y"`, `"xy"`},
		{"keeps HTML characters", `{"html":"<a href=\"x\">&</a>\u0000"}`, `{"html":"<a href=\"x\">&</a>"}`},
		{"keeps large integers exactly", `{"n":12345678901234567890,"f":1.5e300,"s":"\u0000"}`, `{"f":1.5e300,"n":12345678901234567890,"s":""}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := JSONWithoutNUL([]byte(tc.input))
			require.NoError(t, err)
			require.Equal(t, tc.want, string(got))
			require.NotContains(t, string(got), `\u0000`)
			require.True(t, json.Valid(got))
		})
	}
}

func TestJSONWithoutNULKeepsAnEscapedBackslashBeforeU0000(t *testing.T) {
	// `\\u0000` is a backslash followed by the text "u0000", not a NUL.
	got, err := JSONWithoutNUL([]byte(`{"path":"C:\\u0000"}`))
	require.NoError(t, err)
	var decoded map[string]string
	require.NoError(t, json.Unmarshal(got, &decoded))
	require.Equal(t, `C:\u0000`, decoded["path"])
}

func TestJSONWithoutNULReturnsNULFreeInputUnchanged(t *testing.T) {
	input := []byte(`{ "z": 1,  "a": "<b>", "n": 12345678901234567890 }`)
	got, err := JSONWithoutNUL(input)
	require.NoError(t, err)
	require.Equal(t, input, got)
}

func TestJSONWithoutNULRejectsInvalidJSON(t *testing.T) {
	for _, input := range []string{`{"a":"\u0000"`, `{"a":"\u0000"} {"b":1}`, `\u0000`} {
		_, err := JSONWithoutNUL([]byte(input))
		require.Error(t, err, input)
	}
}
