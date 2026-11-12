package domain

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateCLILinkCode(t *testing.T) {
	plaintext, hash, prefix, err := GenerateCLILinkCode()
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(plaintext, CLILinkPrefix))
	assert.Len(t, plaintext, len(CLILinkPrefix)+40)
	assert.Equal(t, HashToken(plaintext), hash)
	assert.Equal(t, plaintext[:len(CLILinkPrefix)+8], prefix)

	other, _, _, err := GenerateCLILinkCode()
	require.NoError(t, err)
	assert.NotEqual(t, plaintext, other)
}

func TestRedactSecrets(t *testing.T) {
	for _, tc := range []struct {
		input, want string
	}{
		{"token kuayle_pat_abcdefghijklmnop1234 is invalid", "token kuayle_pat_abcdefgh… is invalid"},
		{"link kuayle_cl_ABCDEFGHijklmnop9999 expired", "link kuayle_cl_ABCDEFGH… expired"},
		{"nothing secret here", "nothing secret here"},
		// Short matches are not valid credentials; leave them untouched.
		{"kuayle_pat_", "kuayle_pat_"},
	} {
		assert.Equal(t, tc.want, RedactSecrets(tc.input), tc.input)
	}
}
