package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBackupDecryptCommand(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "missing.enc")
	out := filepath.Join(dir, "out.tar.gz")

	require.ErrorContains(t, backupCommand([]string{"decrypt"}, strings.NewReader(""), ""), "Usage")
	require.ErrorContains(t, backupCommand([]string{"decrypt", in, out}, strings.NewReader(""), ""), "no passphrase")

	require.NoError(t, os.WriteFile(in, []byte("not encrypted"), 0o600))
	err := backupCommand([]string{"decrypt", in, out}, bytes.NewBufferString("pass\n"), "")
	require.Error(t, err)
	require.NoFileExists(t, out, "a failed decrypt leaves no output")
	require.NoFileExists(t, out+".partial")
}
