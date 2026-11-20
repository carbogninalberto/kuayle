package domain

import (
	"encoding/pem"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

func TestMachineLogOutputRedactsBeforeTakingTail(t *testing.T) {
	got := MachineLogOutput("old\nsecond\npassword=fixture-db-pass\nknown-value\n", 2, []string{"known-value"}, false)
	require.True(t, got.Truncated)
	require.Equal(t, "password=[REDACTED]\n[REDACTED]", got.Content)
	got = MachineLogOutput(strings.Repeat("世", 8000)+"known-value\nlatest", 3, []string{"known-value"}, false)
	require.LessOrEqual(t, len(got.Content), MachineLogBytes)
	require.True(t, utf8.ValidString(got.Content))
	require.True(t, got.Truncated)
	require.True(t, strings.HasSuffix(got.Content, "[REDACTED]\nlatest"))
	require.Equal(t, MachineLogResult{}, MachineLogOutput("", 5, nil, false))
	block := strings.TrimSpace(string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("fixture")})))
	_, partial, _ := strings.Cut(block, "\n")
	for _, key := range []string{block, partial} {
		got = MachineLogOutput(key+"\nAuthorization: Bearer fixture-token\n", 5, nil, false)
		require.Equal(t, "[REDACTED]\nAuthorization: Bearer [REDACTED]", got.Content)
	}
}
