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
