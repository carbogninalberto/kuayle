package machine

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Agent runs can write everything under /workspace, the configuration and
// hooks of the repository caches every task worktree shares included, while
// Kuayle prepares task checkouts there with the repository's installation
// token (KUA-299). Against real git: nothing an agent configured may send that
// token to another host, or run while it is at hand, and the checkouts are
// still made from GitHub.
func TestCheckoutIgnoresAgentGitConfigurationAndKeepsTheTokenOffOtherHosts(t *testing.T) {
	e := newGitScriptEnv(t)
	e.remote("octo/app", "app.txt")
	e.remote("octo/api", "api.txt")
	workspace := filepath.Join(e.root, "workspace")
	require.NoError(t, os.MkdirAll(filepath.Join(workspace, "tasks"), 0o700))
	task := func(name string) string { return filepath.Join(workspace, "tasks", name) }
	output, err := e.runCheckout(workspace, "octo/app", task("eng-1"))
	require.NoError(t, err, output)

	// Another host, which asks for credentials and records what it is given.
	var mu sync.Mutex
	var received []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		credential := ""
		if encoded, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Basic "); ok {
			decoded, _ := base64.StdEncoding.DecodeString(encoded)
			credential = string(decoded)
		}
		mu.Lock()
		received = append(received, r.URL.Path+" credential="+credential)
		mu.Unlock()
		w.Header().Set("WWW-Authenticate", `Basic realm="elsewhere"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	// A program the agent names: it records that it ran and whatever
	// credential it can reach, and passes its input through.
	marker := filepath.Join(e.root, "executed")
	hostile := filepath.Join(e.root, "hostile")
	require.NoError(t, os.WriteFile(hostile, []byte("#!/bin/sh\n{ echo \"ran $0 $*\"; env | grep -i -e token -e askpass; [ -z \"${GIT_ASKPASS:-}\" ] || \"$GIT_ASKPASS\" Password; } >> "+marker+" 2>&1\ncat\n"), 0o755))
	hooks := filepath.Join(e.root, "hooks")
	require.NoError(t, os.MkdirAll(hooks, 0o755))
	for _, name := range []string{"reference-transaction", "post-checkout", "post-merge", "pre-auto-gc"} {
		require.NoError(t, os.Symlink(hostile, filepath.Join(hooks, name)))
	}
	setConfig := func(gitDir string, settings ...[2]string) {
		for _, setting := range settings {
			e.git("", "--git-dir="+gitDir, "config", setting[0], setting[1])
		}
	}
	programs := [][2]string{
		{"core.hooksPath", hooks}, {"core.fsmonitor", hostile},
		{"filter.agent.smudge", hostile}, {"filter.agent.clean", hostile}, {"filter.agent.process", hostile},
		{"protocol.ext.allow", "always"},
	}
	// A URL rewrite longer than the one that stands for the network, so it
	// wins wherever repository configuration is read.
	redirect := [2]string{"url." + server.URL + "/octo/.insteadOf", "https://github.com/octo/"}
	cache := filepath.Join(workspace, "repos", "octo", "app.git")
	require.NoError(t, os.WriteFile(filepath.Join(cache, "info", "attributes"), []byte("* filter=agent\n"), 0o644))

	// Programs named in the shared cache, while a new task worktree is made
	// from it.
	setConfig(cache, programs...)
	output, err = e.runCheckout(workspace, "octo/app", task("eng-2"))
	assert.NoError(t, err, "a new worktree from a cache with agent programs: %s", output)

	// A rewrite of GitHub's URL in the shared cache, while a new task worktree
	// is made and while an existing one is fetched again (a pipeline member's
	// next turn).
	setConfig(cache, redirect)
	output, err = e.runCheckout(workspace, "octo/app", task("eng-3"))
	assert.NoError(t, err, "a new worktree from a cache with a URL rewrite: %s", output)
	output, err = e.runCheckout(workspace, "octo/app", task("eng-1"))
	assert.NoError(t, err, "an existing worktree fetched again: %s", output)

	// A cache an agent made ahead of the first checkout of another repository
	// of the machine.
	planted := filepath.Join(workspace, "repos", "octo", "api.git")
	e.git("", "init", "-q", "--bare", planted)
	setConfig(planted, append(programs, redirect, [2]string{"remote.origin.url", "https://github.com/octo/api"})...)
	output, err = e.runCheckout(workspace, "octo/api", task("eng-4"))
	assert.NoError(t, err, "a cache the agent made: %s", output)

	mu.Lock()
	assert.Empty(t, received, "the platform's git went to the host an agent named")
	mu.Unlock()
	ran, _ := os.ReadFile(marker)
	assert.Empty(t, string(ran), "a program an agent named ran during a checkout")
	assertNoForbiddenFileContent(t, workspace, "installation-token-")

	// The checkouts are GitHub's, as asked, and track the base branch.
	safe := func(dir string, args ...string) string {
		return e.git(dir, append([]string{"-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false"}, args...)...)
	}
	for _, checkout := range []struct{ name, repository, file string }{
		{"eng-1", "octo/app", "app.txt"}, {"eng-2", "octo/app", "app.txt"}, {"eng-3", "octo/app", "app.txt"}, {"eng-4", "octo/api", "api.txt"},
	} {
		if !assert.FileExists(t, filepath.Join(task(checkout.name), checkout.file), checkout.name) {
			continue
		}
		assert.Equal(t, "kuayle/"+checkout.name, safe(task(checkout.name), "branch", "--show-current"))
		assert.Equal(t, "https://github.com/"+checkout.repository, safe(task(checkout.name), "config", "--get", "remote.origin.url"))
		assert.Equal(t, "refs/heads/main", safe(task(checkout.name), "config", "--get", "branch.kuayle/"+checkout.name+".merge"))
		assert.Equal(t, safe(filepath.Join(e.root, "seed", checkout.repository), "rev-parse", "HEAD"), safe(task(checkout.name), "rev-parse", "HEAD"))
		// Compared without reading the files, which would run the agent's filter.
		assert.Empty(t, safe(task(checkout.name), "diff", "--cached", "--name-only"), "the index is the commit's: %s", checkout.name)
		content, err := os.ReadFile(filepath.Join(task(checkout.name), checkout.file))
		assert.NoError(t, err)
		assert.Equal(t, checkout.repository+"\n", string(content), "files are written as they are")
	}

	// Fetching an existing checkout again brings GitHub's new commits in and
	// leaves its branch where it was.
	seed := filepath.Join(e.root, "seed", "octo/app")
	before := safe(seed, "rev-parse", "HEAD")
	require.NoError(t, os.WriteFile(filepath.Join(seed, "next.txt"), []byte("next\n"), 0o600))
	safe(seed, "add", "next.txt")
	safe(seed, "commit", "-q", "-m", "next")
	safe(seed, "push", "-q", "origin", "HEAD:main")
	output, err = e.runCheckout(workspace, "octo/app", task("eng-2"))
	require.NoError(t, err, output)
	assert.Equal(t, safe(seed, "rev-parse", "HEAD"), safe(task("eng-2"), "rev-parse", "refs/remotes/origin/main"))
	assert.Equal(t, before, safe(task("eng-2"), "rev-parse", "HEAD"))
	ran, _ = os.ReadFile(marker)
	assert.Empty(t, string(ran))
	mu.Lock()
	assert.Empty(t, received)
	mu.Unlock()
}

// credentialHelpers are the three copies of the token-less credential helper
// people's IDE and terminals (and custom agents a person runs with a push)
// use, as each is written: by the developer start script, by Kuayle's
// checkout and by the custom agent bootstrap.
func credentialHelpers(t *testing.T) map[string]string {
	t.Helper()
	common, err := os.ReadFile(filepath.Join(repoRootFromCaller(t), "devmachine", "common", "start-common.sh"))
	require.NoError(t, err)
	body := func(script string) string {
		t.Helper()
		_, rest, ok := strings.Cut(script, `cat > "$credential_helper" <<'SCRIPT'`+"\n")
		require.True(t, ok)
		helper, _, ok := strings.Cut(rest, "\nSCRIPT\n")
		require.True(t, ok)
		return helper + "\n"
	}
	return map[string]string{"start-common.sh": body(string(common)), "checkoutScript": body(checkoutScript), "customAgentBootstrap": body(customAgentBootstrap)}
}

// The helper hands GITHUB_TOKEN only to https://github.com, the one host
// Kuayle's repositories live on (KUA-299). A repository's configuration,
// which agent runs can write, can point a URL anywhere: a person's git fetch
// in a checkout an agent redirected to another host, over http or over https
// with certificate checks turned off, must not send the token there.
func TestCredentialHelpersGiveTheTokenOnlyToGitHub(t *testing.T) {
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is unavailable")
	}
	const token = "person-github-token"
	var mu sync.Mutex
	var received []string
	record := func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		received = append(received, r.Host+r.URL.Path+" "+r.Header.Get("Authorization"))
		mu.Unlock()
		w.Header().Set("WWW-Authenticate", `Basic realm="elsewhere"`)
		w.WriteHeader(http.StatusUnauthorized)
	}
	plain := httptest.NewServer(http.HandlerFunc(record))
	defer plain.Close()
	tls := httptest.NewTLSServer(http.HandlerFunc(record))
	defer tls.Close()

	for name, helper := range credentialHelpers(t) {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, gitCredentialHelperScript, helper, "every copy is the same helper")
			root := t.TempDir()
			path := filepath.Join(root, ".kuayle-git-credential")
			require.NoError(t, os.WriteFile(path, []byte(helper), 0o700))
			ask := func(request string) (string, error) {
				command := exec.Command(path, "get")
				command.Env = []string{"PATH=" + os.Getenv("PATH"), "GITHUB_TOKEN=" + token}
				command.Stdin = strings.NewReader(request)
				output, err := command.Output()
				return string(output), err
			}

			// A person's checkout whose configuration an agent rewrote.
			home := filepath.Join(root, "home")
			require.NoError(t, os.MkdirAll(home, 0o700))
			require.NoError(t, os.WriteFile(filepath.Join(home, ".gitconfig"), []byte("[credential]\n\thelper = "+path+"\n"), 0o600))
			env := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0", "GITHUB_TOKEN=" + token}
			git := func(args ...string) (string, error) {
				command := exec.Command(realGit, args...)
				command.Env = env
				output, err := command.CombinedOutput()
				return string(output), err
			}
			checkout := filepath.Join(root, "checkout")
			_, err := git("init", "-q", checkout)
			require.NoError(t, err)
			_, err = git("-C", checkout, "remote", "add", "origin", "https://github.com/octo/app")
			require.NoError(t, err)
			for _, elsewhere := range []string{plain.URL, tls.URL} {
				_, err = git("-C", checkout, "config", "url."+elsewhere+"/octo/.insteadOf", "https://github.com/octo/")
				require.NoError(t, err)
				_, err = git("-C", checkout, "config", "http.sslVerify", "false")
				require.NoError(t, err)
				output, err := git("-C", checkout, "fetch", "origin")
				require.Error(t, err, "the other host refuses the fetch: %s", output)
				_, err = git("-C", checkout, "config", "--unset-all", "url."+elsewhere+"/octo/.insteadOf")
				require.NoError(t, err)
			}
			mu.Lock()
			sent := received
			received = nil
			mu.Unlock()
			require.NotEmpty(t, sent, "the fetches reached the other host")
			for _, request := range sent {
				require.NotContains(t, request, base64.StdEncoding.EncodeToString([]byte("x-access-token:"+token)), "the token went to %s", request)
			}

			// Asked directly.
			output, err := ask("protocol=https\nhost=github.com\npath=octo/app.git\n\n")
			require.NoError(t, err)
			require.Equal(t, "username=x-access-token\npassword="+token+"\n", output, "GitHub gets the token")
			for _, request := range []string{"protocol=https\nhost=evil.example\n\n", "protocol=http\nhost=github.com\n\n", "protocol=https\nhost=github.com.evil.example\n\n", "protocol=https\nhost=github.com:8443\n\n", ""} {
				output, err = ask(request)
				require.NoError(t, err, "%q", request)
				require.Empty(t, output, "%q gets nothing", request)
			}
		})
	}
}
