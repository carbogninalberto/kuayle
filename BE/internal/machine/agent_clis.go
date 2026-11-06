package machine

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	log "github.com/sirupsen/logrus"
)

// Agent CLI updates
//
// Releases pin the built-in agent CLIs in the machine images. Between
// releases the manager keeps the npm-distributed ones current: on an interval
// it installs the latest versions published at least MinAge ago (npm
// --before, so a compromised release has time to be pulled) into one shared
// Docker volume, checks that each starts, and switches the volume's `current`
// link to them. Agent runs and the developer terminal and IDE mount the volume
// read-only with its bin directory ahead of the image's CLIs on PATH (a
// person's own `npm i -g` in the workspace still comes first), so neither an
// agent nor a person can change what other runs execute. Crush, Goose and Kimi
// are release binaries with pinned checksums and keep the image's versions.
const (
	agentCLIVolume  = "kuayle-agent-clis"
	agentCLIRoot    = "/opt/kuayle/agent-clis"
	agentCLIBin     = agentCLIRoot + "/current/node_modules/.bin"
	agentCLIService = "agent-cli-refresh"
	agentCLITimeout = 20 * time.Minute
	// defaultImagePath is used when an image does not set PATH.
	defaultImagePath = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
	agentCLIReport   = "KUAYLE_AGENT_CLIS"
)

// agentCLIPackages are the refreshed npm packages and the command each
// provides; install scripts run only for those the images allow them for.
var agentCLIPackages = []struct{ name, command string }{
	{"@anthropic-ai/claude-code", "claude"},
	{"opencode-ai", "opencode"},
	{"@openai/codex", "codex"},
	{"@qwen-code/qwen-code", "qwen"},
}

// agentCLIScripts lists the packages whose install scripts may run, as the
// images allow them; npm reads it from the release directory's .npmrc.
const agentCLIScripts = "allow-scripts[]=@anthropic-ai/claude-code\\nallow-scripts[]=opencode-ai\\nallow-scripts[]=@qwen-code/qwen-code\\n"

// agentCLIResolver picks a package's version: the newest stable version
// published before the cutoff and not past its latest tag (so a withdrawn
// release is not taken), or the image's pinned version when that is newer, so
// the shared CLIs never replace a newer image CLI. It reads `npm view <name>
// time dist-tags --json` on stdin; arguments: cutoff, image version.
const agentCLIResolver = `const semver = require('/usr/local/lib/node_modules/npm/node_modules/semver');
const [before, image] = process.argv.slice(1);
let input = '';
process.stdin.on('data', (chunk) => (input += chunk)).on('end', () => {
  const view = JSON.parse(input);
  const latest = view['dist-tags'] && view['dist-tags'].latest;
  const cutoff = Date.parse(before);
  const candidates = Object.keys(view.time || {}).filter((version) => semver.valid(version) && !semver.prerelease(version) &&
    Date.parse(view.time[version]) <= cutoff && (!latest || semver.lte(version, latest)));
  let best = candidates.sort(semver.rcompare)[0] || '';
  if (image && semver.valid(image) && (!best || semver.gt(image, best))) best = image;
  if (!best) process.exit(1);
  console.log(best);
});`

// agentCLIRefreshScript installs into a new release directory and reports
// the versions on its last line; npm's own output goes to stderr. Arguments:
// the cutoff date, then name=command pairs.
const agentCLIRefreshScript = `set -eu
root=` + agentCLIRoot + `
before=$1
shift
export HOME=/tmp npm_config_cache=/tmp/npm-cache npm_config_update_notifier=false
stamp=$(date -u +%Y%m%d%H%M%S)
target=$root/releases/$stamp
mkdir -p "$target"
printf '` + agentCLIScripts + `' > "$target/.npmrc"
specs=
for pair in "$@"; do
  name=${pair%%=*}
  image=$(node -p "require('/usr/local/lib/node_modules/$name/package.json').version" 2>/dev/null || true)
  version=$(npm view "$name" time dist-tags --json | node -e "$KUAYLE_RESOLVER" "$before" "$image")
  specs="$specs $name@$version"
done
cd "$target"
# shellcheck disable=SC2086
npm install --prefix "$target" --no-audit --no-fund --no-save $specs >&2
report=
for pair in "$@"; do
  name=${pair%%=*}
  command=${pair#*=}
  version=$(node -p "require('$target/node_modules/$name/package.json').version")
  timeout 60 "$target/node_modules/.bin/$command" --version >/dev/null 2>&1 || {
    echo "$command $version does not start" >&2
    rm -rf "$target"
    exit 1
  }
  report="$report $name=$version"
done
printf '%s\n' $report > "$target/versions.txt"
if [ -f "$root/current/versions.txt" ] && cmp -s "$target/versions.txt" "$root/current/versions.txt"; then
  rm -rf "$target"
  echo "` + agentCLIReport + ` unchanged$report"
  exit 0
fi
previous=$(readlink "$root/current" 2>/dev/null || true)
ln -sfn "releases/$stamp" "$root/current.next"
mv -T "$root/current.next" "$root/current"
for release in "$root"/releases/*; do
  case "releases/${release##*/}" in "releases/$stamp"|"$previous") ;; *) rm -rf "$release" ;; esac
done
echo "` + agentCLIReport + ` changed$report"
`

// AgentCLIRefresh is one refresh's outcome: the installed versions and
// whether they replaced the previous ones.
type AgentCLIRefresh struct {
	Changed  bool
	Versions map[string]string
}

// agentCLIRefresher is implemented by runtimes that keep the shared CLIs.
type agentCLIRefresher interface {
	RefreshAgentCLIs(ctx context.Context, image string, before time.Time) (AgentCLIRefresh, error)
}

// EnableAgentCLIs mounts the shared CLI volume into agent runs and developer
// containers created from now on.
func (r *DockerRuntime) EnableAgentCLIs() {
	r.agentCLIs = true
}

// useAgentCLIs adds the read-only CLI volume to a new container and puts its
// bin directory on PATH. A missing volume is created empty, so PATH then
// falls through to the image's CLIs.
func (r *DockerRuntime) useAgentCLIs(hostConfig *container.HostConfig, environment []string) []string {
	if !r.agentCLIs {
		return environment
	}
	hostConfig.Mounts = append(hostConfig.Mounts, mount.Mount{Type: mount.TypeVolume, Source: agentCLIVolume, Target: agentCLIRoot, ReadOnly: true})
	return withAgentCLIPath(environment)
}

// withAgentCLIPath puts the shared CLIs first on PATH, after the workspace's
// own npm prefix when the image lists it.
func withAgentCLIPath(environment []string) []string {
	result := make([]string, 0, len(environment)+1)
	found := false
	for _, entry := range environment {
		value, ok := strings.CutPrefix(entry, "PATH=")
		if !ok {
			result = append(result, entry)
			continue
		}
		found = true
		parts := strings.Split(value, ":")
		if !slices.Contains(parts, agentCLIBin) {
			if len(parts) > 0 && parts[0] == workspaceNpmBin {
				parts = append([]string{workspaceNpmBin, agentCLIBin}, parts[1:]...)
			} else {
				parts = append([]string{agentCLIBin}, parts...)
			}
		}
		result = append(result, "PATH="+strings.Join(parts, ":"))
	}
	if !found {
		result = append(result, "PATH="+agentCLIBin+":"+defaultImagePath)
	}
	return result
}

// RefreshAgentCLIs runs the refresh in a helper container from image (one
// with node and npm: the headless developer image). It runs as root, which
// owns the volume, with no capabilities, and reaches the registry directly on
// the egress network that only machine egress proxies otherwise join; it
// receives no machine data or secrets.
func (r *DockerRuntime) RefreshAgentCLIs(ctx context.Context, image string, before time.Time) (AgentCLIRefresh, error) {
	if image == "" {
		return AgentCLIRefresh{}, errors.New("no image for the agent CLI refresh")
	}
	ctx, cancel := context.WithTimeout(ctx, agentCLITimeout)
	defer cancel()
	if err := r.ensureImage(ctx, image); err != nil {
		return AgentCLIRefresh{}, err
	}
	if _, err := r.ensureNetwork(ctx, egressNetworkName, false, map[string]string{"com.kuayle.kind": "machine-egress"}); err != nil {
		return AgentCLIRefresh{}, err
	}
	args := []string{"-c", agentCLIRefreshScript, "kuayle-agent-cli-refresh", before.UTC().Format(time.RFC3339)}
	for _, item := range agentCLIPackages {
		args = append(args, item.name+"="+item.command)
	}
	pids := int64(1024)
	response, err := r.client.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config: &container.Config{
			Image: image, User: "0:0", Entrypoint: []string{"/bin/sh"}, Cmd: args, Env: []string{"KUAYLE_RESOLVER=" + agentCLIResolver},
			Labels: map[string]string{"com.kuayle.managed": "true", "com.kuayle.service": agentCLIService},
		},
		HostConfig: &container.HostConfig{
			NetworkMode: container.NetworkMode(egressNetworkName), ReadonlyRootfs: true,
			CapDrop: []string{"ALL"}, SecurityOpt: []string{"no-new-privileges=true"}, LogConfig: boundedLogConfig(),
			Tmpfs:  map[string]string{"/tmp": "rw,exec,nosuid,size=1g"},
			Mounts: []mount.Mount{{Type: mount.TypeVolume, Source: agentCLIVolume, Target: agentCLIRoot}},
			Resources: container.Resources{
				Memory: 2 << 30, MemorySwap: 2 << 30, NanoCPUs: 2_000_000_000, PidsLimit: &pids,
			},
		},
		NetworkingConfig: &network.NetworkingConfig{EndpointsConfig: map[string]*network.EndpointSettings{egressNetworkName: {}}},
	})
	if err != nil {
		return AgentCLIRefresh{}, err
	}
	defer func() {
		_, _ = r.client.ContainerRemove(context.Background(), response.ID, client.ContainerRemoveOptions{Force: true})
	}()
	if _, err := r.client.ContainerStart(ctx, response.ID, client.ContainerStartOptions{}); err != nil {
		return AgentCLIRefresh{}, err
	}
	wait := r.client.ContainerWait(ctx, response.ID, client.ContainerWaitOptions{Condition: container.WaitConditionNotRunning})
	select {
	case result := <-wait.Result:
		stdout, stderr, _ := r.containerLogs(ctx, response.ID)
		if result.StatusCode != 0 {
			return AgentCLIRefresh{}, fmt.Errorf("agent CLI refresh exited %d: %s", result.StatusCode, lastLines(stderr, 5))
		}
		return parseAgentCLIRefresh(stdout)
	case err := <-wait.Error:
		return AgentCLIRefresh{}, err
	}
}

func lastLines(value string, count int) string {
	lines := strings.Split(strings.TrimSpace(value), "\n")
	if len(lines) > count {
		lines = lines[len(lines)-count:]
	}
	return strings.Join(lines, " | ")
}

// parseAgentCLIRefresh reads the helper's report line.
func parseAgentCLIRefresh(output string) (AgentCLIRefresh, error) {
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[0] != agentCLIReport {
			continue
		}
		if fields[1] != "changed" && fields[1] != "unchanged" {
			return AgentCLIRefresh{}, fmt.Errorf("unexpected agent CLI refresh state %q", fields[1])
		}
		result := AgentCLIRefresh{Changed: fields[1] == "changed", Versions: map[string]string{}}
		for _, field := range fields[2:] {
			name, version, ok := strings.Cut(field, "=")
			if !ok || name == "" || version == "" {
				return AgentCLIRefresh{}, fmt.Errorf("unexpected agent CLI version %q", field)
			}
			result.Versions[name] = version
		}
		return result, nil
	}
	return AgentCLIRefresh{}, errors.New("agent CLI refresh did not report its versions")
}

// AgentCLIUpdates configures the refresh: how often, and how old a version
// must be. A zero Interval turns it off.
type AgentCLIUpdates struct {
	Interval time.Duration
	MinAge   time.Duration
}

// agentCLIRetry is the wait after a failed refresh.
const agentCLIRetry = time.Hour

func (m *Manager) SetAgentCLIUpdates(updates AgentCLIUpdates) {
	m.agentCLIMu.Lock()
	defer m.agentCLIMu.Unlock()
	m.agentCLIUpdates = updates
}

// AgentCLIVersions are the shared CLIs' versions after the last refresh.
func (m *Manager) AgentCLIVersions() map[string]string {
	m.agentCLIMu.Lock()
	defer m.agentCLIMu.Unlock()
	return maps.Clone(m.agentCLIVersions)
}

// refreshAgentCLIs refreshes the shared CLIs when the interval has passed
// since the last attempt (an hour after a failure).
func (m *Manager) refreshAgentCLIs(ctx context.Context, now time.Time) {
	refresher, ok := m.runtime.(agentCLIRefresher)
	m.agentCLIMu.Lock()
	updates := m.agentCLIUpdates
	due := ok && updates.Interval > 0 && m.images.Headless != "" && !now.Before(m.agentCLINext)
	if due {
		m.agentCLINext = now.Add(updates.Interval)
	}
	m.agentCLIMu.Unlock()
	if !due {
		return
	}
	result, err := refresher.RefreshAgentCLIs(ctx, m.images.Headless, now.Add(-updates.MinAge))
	if err != nil {
		m.agentCLIMu.Lock()
		m.agentCLINext = now.Add(min(agentCLIRetry, updates.Interval))
		m.agentCLIMu.Unlock()
		log.WithError(err).Warn("agent CLI refresh failed; machines keep the current CLIs")
		return
	}
	m.agentCLIMu.Lock()
	m.agentCLIVersions = result.Versions
	m.agentCLIMu.Unlock()
	fields := log.Fields{"changed": result.Changed}
	for name, version := range result.Versions {
		fields[name] = version
	}
	log.WithFields(fields).Info("agent CLIs refreshed")
}
