package domain

import (
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestTestCommandValidation(t *testing.T) {
	for _, command := range [][]string{nil, {}, {"npm", "test"}, {"sh", "-c", "printf '%s' '$literal'"}, {"check", "", strings.Repeat("x", 8192)}} {
		require.NoError(t, ValidateTestCommand(command))
	}
	for _, command := range [][]string{{""}, {" \t"}, {"run\x00bad"}, {string([]byte{0xff})}, {"run", strings.Repeat("界", 2731)}, append([]string{"run"}, make([]string, 64)...), {"run", strings.Repeat("x", 8192), strings.Repeat("x", 8192), strings.Repeat("x", 8192), strings.Repeat("x", 8192)}} {
		require.Error(t, ValidateTestCommand(command))
	}
}

func TestParseAgentValidationFile(t *testing.T) {
	valid := ParseAgentValidationFile([]byte(`{"commands":[["sh","-c","cd BE && go test ./internal/service/..."],["npm","run","check"]],"summary":"  Service tests cover the change; UI not run.  "}`), false)
	require.Empty(t, valid.Error)
	require.Equal(t, [][]string{{"sh", "-c", "cd BE && go test ./internal/service/..."}, {"npm", "run", "check"}}, valid.Commands)
	require.Equal(t, "Service tests cover the change; UI not run.", valid.Summary)
	require.Equal(t, [][]string{{"go", "test", "./..."}}, ParseAgentValidationFile([]byte(`{"commands":[["go","test","./..."]]}`), false).Commands, "the summary is optional")

	nine := `{"commands":[` + strings.TrimSuffix(strings.Repeat(`["true"],`, 9), ",") + `]}`
	for name, data := range map[string]string{
		"not JSON":         `commands: [true]`,
		"no commands":      `{"commands":[],"summary":"nothing"}`,
		"missing commands": `{"summary":"nothing"}`,
		"too many":         nine,
		"empty argv":       `{"commands":[[]]}`,
		"blank program":    `{"commands":[[" "]]}`,
		"string command":   `{"commands":["go test ./..."]}`,
		"NUL argument":     `{"commands":[["sh","-c","a\u0000b"]]}`,
		"unknown field":    `{"commands":[["true"]],"env":{"TOKEN":"x"}}`,
		"trailing data":    `{"commands":[["true"]]} {"commands":[["false"]]}`,
		"long summary":     `{"commands":[["true"]],"summary":"` + strings.Repeat("界", 2001) + `"}`,
		"array root":       `[["true"]]`,
	} {
		parsed := ParseAgentValidationFile([]byte(data), false)
		require.NotEmpty(t, parsed.Error, name)
		require.Empty(t, parsed.Commands, name)
		require.Empty(t, parsed.Summary, name)
	}
	oversize := ParseAgentValidationFile([]byte(`{"commands":[["true"]]}`), true)
	require.Contains(t, oversize.Error, "64 KiB")
	require.Empty(t, oversize.Commands)
	require.Equal(t, strings.Repeat("界", 2000), ParseAgentValidationFile([]byte(`{"commands":[["true"]],"summary":"`+strings.Repeat("界", 2000)+`"}`), false).Summary, "2000 characters fit")
}
