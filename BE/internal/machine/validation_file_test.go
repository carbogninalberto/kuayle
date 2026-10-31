package machine

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// The take script reads only a bounded regular validation file, never through
// a symlink, and removes it.
func TestValidationFileTakeScript(t *testing.T) {
	for _, shell := range outboxShells(t) {
		t.Run(filepath.Base(shell), func(t *testing.T) {
			take := func(setup func(dir string)) (*domain.AgentDeclaredValidation, string) {
				root := t.TempDir()
				dir := filepath.Join(root, ".kuayle")
				require.NoError(t, os.MkdirAll(dir, 0o700))
				setup(dir)
				output, err := exec.Command(shell, "-c", validationFileTakeScript, "kuayle-validation", root).CombinedOutput()
				require.NoError(t, err, string(output))
				_, statErr := os.Lstat(filepath.Join(dir, "validation.json"))
				require.True(t, os.IsNotExist(statErr), "the file is taken")
				return parseValidationFileTake(string(output)), root
			}
			declared, _ := take(func(dir string) {
				require.NoError(t, os.WriteFile(filepath.Join(dir, "validation.json"), []byte(`{"commands":[["sh","-c","cd BE && go test ./internal/service/..."]],"summary":"service tests"}`), 0o600))
			})
			require.Equal(t, &domain.AgentDeclaredValidation{Commands: [][]string{{"sh", "-c", "cd BE && go test ./internal/service/..."}}, Summary: "service tests"}, declared)

			declared, _ = take(func(dir string) {
				require.NoError(t, os.WriteFile(filepath.Join(dir, "validation.json"), []byte(`{"commands":[["true"]],"summary":"`+strings.Repeat("x", 70000)+`"}`), 0o600))
			})
			require.Contains(t, declared.Error, "64 KiB")

			secret := filepath.Join(t.TempDir(), "secret.json")
			require.NoError(t, os.WriteFile(secret, []byte(`{"commands":[["true"]]}`), 0o600))
			declared, _ = take(func(dir string) { require.NoError(t, os.Symlink(secret, filepath.Join(dir, "validation.json"))) })
			require.Equal(t, "the file is not a regular file", declared.Error)
			_, err := os.Stat(secret)
			require.NoError(t, err, "the link target is never touched")

			declared, _ = take(func(string) {})
			require.Nil(t, declared, "no file declares nothing")
		})
	}
}
