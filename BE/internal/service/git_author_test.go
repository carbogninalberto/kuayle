package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidGitAuthor(t *testing.T) {
	require.True(t, validGitAuthor("", ""), "no author keeps the deployment default")
	require.True(t, validGitAuthor("carbogninalberto", "dev@example.com"))
	for name, email := range map[string]string{
		"Evil <x@y.z>":     "dev@example.com",
		"line\nbreak":      "dev@example.com",
		"carbogninalberto": "not-an-email",
		"spaces":           "a b@example.com",
		"angle":            "<dev@example.com>",
	} {
		require.False(t, validGitAuthor(name, email), "%q %q", name, email)
	}
}
