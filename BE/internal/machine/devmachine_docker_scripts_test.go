package machine

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// posixShell is dash when available: machine images run their scripts with it.
func posixShell() string {
	if _, err := os.Stat("/bin/dash"); err == nil {
		return "/bin/dash"
	}
	return "sh"
}

func runDockerClientSetup(t *testing.T, home string, env ...string) (string, error) {
	t.Helper()
	common := filepath.Join(repoRootFromCaller(t), "devmachine", "common", "start-common.sh")
	script := `. "$1"; configure_docker_client; printf 'DOCKER_HOST=%s\nDOCKER_TLS_VERIFY=%s\nDOCKER_CERT_PATH=%s\nNO_PROXY=%s\nno_proxy=%s\nLEFT=%s%s%s%s\n' "${DOCKER_HOST:-}" "${DOCKER_TLS_VERIFY:-}" "${DOCKER_CERT_PATH:-}" "${NO_PROXY:-}" "${no_proxy:-}" "${KUAYLE_DOCKER_CA:-}" "${KUAYLE_DOCKER_CERT:-}" "${KUAYLE_DOCKER_KEY:-}" "${KUAYLE_DOCKER_HOST:-}"`
	cmd := exec.Command(posixShell(), "-eu", "-c", script, "kuayle", common)
	cmd.Env = append([]string{"PATH=" + os.Getenv("PATH"), "HOME=" + home}, env...)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

// With the machine daemon's client material, the CLI is pointed at the daemon
// with TLS verification, the key is written only to the private ~/.docker and
// removed from the environment, the daemon bypasses the egress proxy, and
// builds and containers get the proxy.
func TestStartCommonPointsTheDockerCLIAtTheMachineDaemon(t *testing.T) {
	home := t.TempDir()
	output, err := runDockerClientSetup(t, home,
		"KUAYLE_DOCKER_CA=ca-pem", "KUAYLE_DOCKER_CERT=client-cert", "KUAYLE_DOCKER_KEY=client-key-secret",
		"KUAYLE_DOCKER_HOST=tcp://abc123-docker:2376",
		"HTTP_PROXY=http://abc123-egress:3128", "HTTPS_PROXY=http://abc123-egress:3128", "NO_PROXY=localhost,abc123-collector")
	require.NoError(t, err, output)
	require.Contains(t, output, "DOCKER_HOST=tcp://abc123-docker:2376\n")
	require.Contains(t, output, "DOCKER_TLS_VERIFY=1\n")
	require.Contains(t, output, "DOCKER_CERT_PATH="+filepath.Join(home, ".docker")+"\n")
	require.Contains(t, output, "NO_PROXY=localhost,abc123-collector,docker,abc123-docker\n")
	require.Contains(t, output, "no_proxy=localhost,abc123-collector,docker,abc123-docker\n")
	require.Contains(t, output, "LEFT=\n", "the material is removed from the environment")
	require.NotContains(t, output, "client-key-secret")
	for file, content := range map[string]string{"ca.pem": "ca-pem", "cert.pem": "client-cert", "key.pem": "client-key-secret"} {
		path := filepath.Join(home, ".docker", file)
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Equal(t, content, string(data))
		info, err := os.Stat(path)
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o600), info.Mode().Perm(), file)
	}
	info, err := os.Stat(filepath.Join(home, ".docker"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o700), info.Mode().Perm())
	var config struct {
		Proxies map[string]struct {
			HTTPProxy  string `json:"httpProxy"`
			HTTPSProxy string `json:"httpsProxy"`
			NoProxy    string `json:"noProxy"`
		} `json:"proxies"`
	}
	data, err := os.ReadFile(filepath.Join(home, ".docker", "config.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &config), string(data))
	require.Equal(t, "http://abc123-egress:3128", config.Proxies["default"].HTTPProxy)
	require.Equal(t, "http://abc123-egress:3128", config.Proxies["default"].HTTPSProxy)
	require.Contains(t, strings.Split(config.Proxies["default"].NoProxy, ","), "localhost")

	// A developer container gets DOCKER_HOST from its environment; running the
	// setup again (a restart) keeps NO_PROXY unchanged.
	output, err = runDockerClientSetup(t, home,
		"KUAYLE_DOCKER_CA=ca-pem", "KUAYLE_DOCKER_CERT=client-cert", "KUAYLE_DOCKER_KEY=client-key-secret",
		"DOCKER_HOST=tcp://abc123-docker:2376", "NO_PROXY=localhost,docker,abc123-docker")
	require.NoError(t, err, output)
	require.Contains(t, output, "NO_PROXY=localhost,docker,abc123-docker\n")
	data, err = os.ReadFile(filepath.Join(home, ".docker", "config.json"))
	require.NoError(t, err)
	require.Equal(t, "{}\n", string(data), "without a proxy nothing is configured")
}

func TestStartCommonLeavesDockerAloneWithoutADaemon(t *testing.T) {
	home := t.TempDir()
	output, err := runDockerClientSetup(t, home, "KUAYLE_DOCKER_HOST=tcp://abc123-docker:2376")
	require.NoError(t, err, output)
	require.Contains(t, output, "DOCKER_HOST=\n")
	require.NoDirExists(t, filepath.Join(home, ".docker"))

	output, err = runDockerClientSetup(t, home, "KUAYLE_DOCKER_CA=a", "KUAYLE_DOCKER_CERT=b", "KUAYLE_DOCKER_KEY=c", "KUAYLE_DOCKER_HOST=unix:///var/run/docker.sock")
	require.Error(t, err, "only the machine daemon's TLS endpoint is accepted")
	require.Contains(t, output, "invalid Docker host")
	output, err = runDockerClientSetup(t, home, "KUAYLE_DOCKER_CA=a", "KUAYLE_DOCKER_CERT=b", "KUAYLE_DOCKER_KEY=c", "KUAYLE_DOCKER_HOST=tcp://a'b:2376")
	require.Error(t, err)
	require.Contains(t, output, "invalid Docker host")
	require.NoFileExists(t, filepath.Join(home, ".docker", "key.pem"))
}

func dockerStartFixture(t *testing.T) (string, string, string) {
	t.Helper()
	dir := t.TempDir()
	secrets, certs := filepath.Join(dir, "secrets"), filepath.Join(dir, "certs")
	require.NoError(t, os.MkdirAll(secrets, 0o700))
	require.NoError(t, os.MkdirAll(filepath.Join(certs, "ca"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(certs, "ca", "key.pem"), []byte("stale ca key"), 0o600))
	entrypoint := filepath.Join(dir, "entrypoint")
	require.NoError(t, os.WriteFile(entrypoint, []byte("#!/bin/sh\nprintf 'args=%s\\n' \"$*\"\nls \"$DOCKER_TLS_CERTDIR\"\ncat \"$DOCKER_TLS_CERTDIR/server/ca.pem\" \"$DOCKER_TLS_CERTDIR/server/cert.pem\" \"$DOCKER_TLS_CERTDIR/server/key.pem\"\n"), 0o700))
	return secrets, certs, entrypoint
}

func runDockerStart(t *testing.T, secrets, certs, entrypoint string) (string, error) {
	t.Helper()
	script := filepath.Join(repoRootFromCaller(t), "devmachine", "docker", "kuayle-docker-start")
	cmd := exec.Command(posixShell(), script, "--debug")
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "KUAYLE_SECRETS_DIR=" + secrets, "DOCKER_TLS_CERTDIR=" + certs, "KUAYLE_DOCKERD_ENTRYPOINT=" + entrypoint}
	output, err := cmd.CombinedOutput()
	return string(output), err
}

// The daemon starts only with its delivered TLS material, which leaves the
// secrets tmpfs; a CA key left in the certificate directory is removed so the
// image entrypoint never issues certificates itself.
func TestDockerDaemonStartInstallsOnlyTheDeliveredServerCertificate(t *testing.T) {
	secrets, certs, entrypoint := dockerStartFixture(t)
	for name, value := range map[string]string{"KUAYLE_DOCKER_TLS_CA": "CA\n", "KUAYLE_DOCKER_TLS_CERT": "CERT\n", "KUAYLE_DOCKER_TLS_KEY": "KEY\n", ".ready": ""} {
		require.NoError(t, os.WriteFile(filepath.Join(secrets, name), []byte(value), 0o600))
	}
	output, err := runDockerStart(t, secrets, certs, entrypoint)
	require.NoError(t, err, output)
	require.Contains(t, output, "args=--debug\n")
	require.Contains(t, output, "CA\nCERT\nKEY\n")
	require.Contains(t, output, "server\n")
	require.NotContains(t, output, "ca\n", "no CA directory is left for the entrypoint")
	entries, err := os.ReadDir(secrets)
	require.NoError(t, err)
	require.Empty(t, entries, "the secrets are consumed")
	info, err := os.Stat(filepath.Join(certs, "server", "key.pem"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	secrets, certs, entrypoint = dockerStartFixture(t)
	require.NoError(t, os.WriteFile(filepath.Join(secrets, "KUAYLE_DOCKER_TLS_CA"), []byte("CA"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(secrets, ".ready"), nil, 0o600))
	output, err = runDockerStart(t, secrets, certs, entrypoint)
	require.Error(t, err)
	require.Contains(t, output, "missing Docker TLS secret KUAYLE_DOCKER_TLS_CERT")
	require.NotContains(t, output, "args=", "the daemon does not start without TLS")
}

func TestDockerCLIInstallerRejectsUnknownArchitectureAndTamperedDownloads(t *testing.T) {
	installer := filepath.Join(repoRootFromCaller(t), "devmachine", "common", "install-docker-cli.sh")
	cmd := exec.Command(posixShell(), installer, "riscv64")
	cmd.Env = []string{"PATH=" + os.Getenv("PATH")}
	output, err := cmd.CombinedOutput()
	require.Error(t, err)
	require.Contains(t, string(output), "unsupported architecture: riscv64")

	fakeBin := filepath.Join(t.TempDir(), "bin")
	require.NoError(t, os.MkdirAll(fakeBin, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(fakeBin, "curl"), []byte("#!/bin/sh\nwhile [ \"$#\" -gt 0 ]; do if [ \"$1\" = -o ]; then shift; printf tampered > \"$1\"; fi; shift; done\n"), 0o700))
	prefix := t.TempDir()
	cmd = exec.Command(posixShell(), installer, "amd64")
	cmd.Env = []string{"PATH=" + fakeBin + string(os.PathListSeparator) + os.Getenv("PATH"), "KUAYLE_DOCKER_CLI_PREFIX=" + prefix}
	var combined bytes.Buffer
	cmd.Stdout, cmd.Stderr = &combined, &combined
	require.Error(t, cmd.Run())
	require.Contains(t, combined.String(), "FAILED", "the checksum check fails")
	require.NoFileExists(t, filepath.Join(prefix, "bin", "docker"), "nothing is installed from a tampered download")
}

// The daemon image is pinned by digest, and the CLI pins match its Docker
// version; the developer and agent images install the checked CLI.
func TestDockerImagesArePinned(t *testing.T) {
	root := repoRootFromCaller(t)
	dockerfile, err := os.ReadFile(filepath.Join(root, "devmachine", "docker", "Dockerfile"))
	require.NoError(t, err)
	from := regexp.MustCompile(`(?m)^FROM docker:([0-9.]+)-dind-rootless@sha256:[0-9a-f]{64}$`).FindStringSubmatch(string(dockerfile))
	require.Len(t, from, 2, "the daemon image is pinned by version and digest")
	installer, err := os.ReadFile(filepath.Join(root, "devmachine", "common", "install-docker-cli.sh"))
	require.NoError(t, err)
	require.Contains(t, string(installer), "DOCKER_VERSION="+from[1]+"\n", "the CLI matches the daemon")
	require.Len(t, regexp.MustCompile(`(?m)^DOCKER_(X86_64|AARCH64)_SHA256=[0-9a-f]{64}$`).FindAllString(string(installer), -1), 2)
	require.NotContains(t, string(installer), "github.com/docker/compose/releases", "plugins are built from source, not downloaded")
	// Compose and Buildx are built from pinned release commits with their
	// dependency floors, in a cross-compiling stage of each image.
	plugins, err := os.ReadFile(filepath.Join(root, "devmachine", "common", "build-docker-plugins.sh"))
	require.NoError(t, err)
	require.Regexp(t, `(?m)^COMPOSE_COMMIT=[0-9a-f]{40}$`, string(plugins))
	require.Regexp(t, `(?m)^BUILDX_COMMIT=[0-9a-f]{40}$`, string(plugins))
	require.Contains(t, string(plugins), "golang.org/x/net=golang.org/x/net@v0.60.0")
	for _, image := range []string{"ide", "headless", "agent"} {
		content, err := os.ReadFile(filepath.Join(root, "devmachine", image, "Dockerfile"))
		require.NoError(t, err)
		require.Regexp(t, `install-docker-cli\.sh "\$TARGETARCH"`, string(content), image)
		require.Regexp(t, `(?m)^FROM --platform=\$BUILDPLATFORM golang:[0-9.]+-alpine AS docker-plugins$`, string(content), image)
		require.Contains(t, string(content), "COPY --from=docker-plugins /out/docker-compose /out/docker-buildx /usr/local/lib/docker/cli-plugins/", image)
	}
}
