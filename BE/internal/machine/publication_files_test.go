package machine

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// The read script reads one bounded regular file of the worktree's .kuayle
// directory, never through a symlink, and leaves it in place.
func TestAgentFileReadScript(t *testing.T) {
	for _, shell := range outboxShells(t) {
		t.Run(filepath.Base(shell), func(t *testing.T) {
			read := func(root, name string, limit int) ([]byte, error) {
				output, err := exec.Command(shell, "-c", agentFileReadScript, "kuayle-file", root, name, strconv.Itoa(limit)).CombinedOutput()
				require.NoError(t, err, string(output))
				return parseAgentFileRead(string(output))
			}
			root := t.TempDir()
			kuayle := filepath.Join(root, ".kuayle")
			require.NoError(t, os.MkdirAll(filepath.Join(kuayle, "screenshots"), 0o700))
			require.NoError(t, os.WriteFile(filepath.Join(kuayle, "pull_request.json"), []byte(`{"description":"x"}`), 0o600))
			require.NoError(t, os.WriteFile(filepath.Join(kuayle, "screenshots", "a.png"), []byte("\x89PNG\r\n\x1a\nimage"), 0o600))

			data, err := read(root, "pull_request.json", 100)
			require.NoError(t, err)
			require.Equal(t, `{"description":"x"}`, string(data))
			require.FileExists(t, filepath.Join(kuayle, "pull_request.json"), "a retried publication reads it again")
			data, err = read(root, "screenshots/a.png", 4)
			require.NoError(t, err)
			require.Equal(t, "\x89PNG", string(data), "the read is cut at the limit")

			data, err = read(root, "screenshots/missing.png", 100)
			require.NoError(t, err)
			require.Nil(t, data, "a missing file is no file")

			secret := filepath.Join(t.TempDir(), "secret.png")
			require.NoError(t, os.WriteFile(secret, []byte("secret"), 0o600))
			require.NoError(t, os.Symlink(secret, filepath.Join(kuayle, "screenshots", "link.png")))
			_, err = read(root, "screenshots/link.png", 100)
			require.ErrorIs(t, err, errAgentFileInvalid)

			require.NoError(t, os.RemoveAll(filepath.Join(kuayle, "screenshots")))
			require.NoError(t, os.Symlink(filepath.Dir(secret), filepath.Join(kuayle, "screenshots")))
			data, err = read(root, "screenshots/secret.png", 100)
			require.NoError(t, err)
			require.Nil(t, data, "a symlinked screenshots directory is never followed")

			linked := t.TempDir()
			require.NoError(t, os.Symlink(kuayle, filepath.Join(linked, ".kuayle")))
			data, err = read(linked, "pull_request.json", 100)
			require.NoError(t, err)
			require.Nil(t, data, "a symlinked .kuayle is never followed")
		})
	}
}

// Each attempt declares its own pull request description and screenshots.
func TestOutboxPrepareScriptRemovesAnEarlierPullRequestFile(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	for _, shell := range outboxShells(t) {
		t.Run(filepath.Base(shell), func(t *testing.T) {
			repo := t.TempDir()
			git(t, repo, "init", "-q")
			kuayle := filepath.Join(repo, ".kuayle")
			require.NoError(t, os.MkdirAll(filepath.Join(kuayle, "screenshots"), 0o700))
			require.NoError(t, os.WriteFile(filepath.Join(kuayle, "pull_request.json"), []byte(`{}`), 0o600))
			require.NoError(t, os.WriteFile(filepath.Join(kuayle, "screenshots", "old.png"), []byte("x"), 0o600))
			kept := filepath.Join(t.TempDir(), "kept.png")
			require.NoError(t, os.WriteFile(kept, []byte("x"), 0o600))
			output, err := exec.Command(shell, "-c", prepareOutboxScript, "kuayle-outbox", repo).CombinedOutput()
			require.NoError(t, err, string(output))
			require.NoFileExists(t, filepath.Join(kuayle, "pull_request.json"))
			require.NoDirExists(t, filepath.Join(kuayle, "screenshots"))

			require.NoError(t, os.Symlink(filepath.Dir(kept), filepath.Join(kuayle, "screenshots")))
			output, err = exec.Command(shell, "-c", prepareOutboxScript, "kuayle-outbox", repo).CombinedOutput()
			require.NoError(t, err, string(output))
			require.FileExists(t, kept, "a symlinked screenshots directory is removed, never its target")
			_, err = os.Lstat(filepath.Join(kuayle, "screenshots"))
			require.True(t, os.IsNotExist(err))
		})
	}
}

func TestScreenshotBranchAndLinks(t *testing.T) {
	task := uuid.MustParse("6f1c0c3e-8f6b-4a51-9a39-0d3b7f3d2f10")
	require.Equal(t, "kuayle/screenshots/6f1c0c3e-8f6b-4a51-9a39-0d3b7f3d2f10", screenshotBranch(task))
	sha := strings.Repeat("c", 40)
	require.Equal(t, "https://github.com/acme/app/blob/"+sha+"/settings-dark.png?raw=true", screenshotURL("acme", "app", sha, "settings-dark.png"))
	require.Equal(t, "https://github.com/acme/my.app/blob/"+sha+"/a_b-1.webp?raw=true", screenshotURL("acme", "my.app", sha, "a_b-1.webp"))
}
