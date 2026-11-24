package machine

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/containerd/errdefs"
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
// Docker volume, checks them, and switches the volume's `current` link to
// them. Agent runs and the developer terminal and IDE mount the volume
// read-only with its bin directory ahead of the image's CLIs on PATH (a
// person's own `npm i -g` in the workspace still comes first), so neither an
// agent nor a person can change what other runs execute. Crush, Goose and Kimi
// are release binaries with pinned checksums and keep the image's versions.
//
// The volume outlives releases, so a CLI on it is used only while it fits the
// release's headless image: every package at the image's version or newer,
// each command from its package (OpenCode 2 moved to @opencode/cli), and the
// image's flag contract (check-agent-cli-flags.sh) passing. The manager checks
// this when it starts, before it leases work, and every refresh checks it
// again before installing and checks the new CLIs before switching. A current
// release that does not fit is set aside (renamed `rejected`), so PATH falls
// through to the image's CLIs; new CLIs that do not fit are discarded. Agent
// runs mount the volume only while this manager found it to fit. The volume
// holds only CLIs: sign-ins, sessions, notebooks and settings live elsewhere
// and are never touched.
const (
	agentCLIVolume  = "kuayle-agent-clis"
	agentCLIRoot    = "/opt/kuayle/agent-clis"
	agentCLIBin     = agentCLIRoot + "/current/node_modules/.bin"
	agentCLIService = "agent-cli-refresh"
	agentCLITimeout = 20 * time.Minute
	// agentCLICheckTimeout bounds the check at manager start.
	agentCLICheckTimeout = 5 * time.Minute
	// defaultImagePath is used when an image does not set PATH.
	defaultImagePath = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
	agentCLIReport   = "KUAYLE_AGENT_CLIS"
)

// agentCLIPackages are the refreshed npm packages and the command each
// provides; install scripts run only for those the images allow them for.
var agentCLIPackages = []struct{ name, command string }{
	{"@anthropic-ai/claude-code", "claude"},
	{"@opencode/cli", "opencode"},
	{"@openai/codex", "codex"},
	{"@earendil-works/pi-coding-agent", "pi"},
	{"@qwen-code/qwen-code", "qwen"},
}

// agentCLIScripts lists the packages whose install scripts may run, as the
// images allow them; npm reads it from the release directory's .npmrc.
const agentCLIScripts = "allow-scripts[]=@anthropic-ai/claude-code\\nallow-scripts[]=@opencode/cli\\nallow-scripts[]=@qwen-code/qwen-code\\n"

// agentCLIResolver picks a package's version: the newest stable version
// published before the cutoff and not past its latest tag (so a withdrawn
// release is not taken), or the image's pinned version when that is newer, so
// the shared CLIs never replace a newer image CLI. It reads `npm view <name>
// time dist-tags --json` on stdin; arguments: the image's global node_modules
// (for npm's semver), cutoff, image version.
const agentCLIResolver = `const [modules, before, image] = process.argv.slice(1);
const semver = require(modules + '/npm/node_modules/semver');
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

// agentCLIValidator checks a release directory against the image: each
// package installed as a release version, not older than the image's, and its
// command linked from that package (not from an old or renamed one). It
// prints name=version pairs, or the problems on stderr and exits 1.
// Arguments: the image's global node_modules, the release, name=command pairs.
const agentCLIValidator = `const [modules, release, ...pairs] = process.argv.slice(1);
const fs = require('fs');
const path = require('path');
const semver = require(modules + '/npm/node_modules/semver');
const version = (dir) => {
  try { return JSON.parse(fs.readFileSync(path.join(dir, 'package.json'), 'utf8')).version || ''; } catch { return ''; }
};
const real = (file) => { try { return fs.realpathSync(file); } catch { return ''; } };
const problems = [];
const versions = [];
for (const pair of pairs) {
  const [name, command] = pair.split('=');
  const dir = path.join(release, 'node_modules', name);
  const installed = version(dir);
  const image = version(path.join(modules, name));
  if (!installed) problems.push(name + ' is not installed');
  else if (!semver.valid(installed) || semver.prerelease(installed)) problems.push(name + ' ' + installed + ' is not a release version');
  else if (semver.valid(image) && semver.lt(installed, image)) problems.push(name + ' ' + installed + " is older than the image's " + image);
  const home = real(dir);
  if (!home || !real(path.join(release, 'node_modules', '.bin', command)).startsWith(home + path.sep)) problems.push(command + ' is not linked from ' + name);
  versions.push(name + '=' + installed);
}
if (problems.length) {
  console.error(problems.join('\n'));
  process.exit(1);
}
console.log(versions.join(' '));`

// agentCLIRefreshScript is the helper. Arguments: the mode (check or
// refresh), the cutoff date, then name=command pairs. It first checks the
// current release and reports it ("kept" with its versions, "rejected" when
// it was set aside, or "none"); a check ends there. A refresh then installs
// into a new release directory, checks it the same way, and reports
// "changed" or "unchanged" with the versions; npm's own output goes to
// stderr. The volume root, the image's global node_modules and the flag check
// are fixed in the image; tests point them elsewhere.
const agentCLIRefreshScript = `set -eu
root=${KUAYLE_AGENT_CLI_ROOT:-` + agentCLIRoot + `}
modules=${KUAYLE_IMAGE_NODE_MODULES:-/usr/local/lib/node_modules}
flag_check=${KUAYLE_AGENT_CLI_FLAG_CHECK:-/usr/local/lib/kuayle/check-agent-cli-flags.sh}
mode=$1
before=$2
shift 2
tmp=${TMPDIR:-/tmp}
export HOME="$tmp" npm_config_cache="$tmp/npm-cache" npm_config_update_notifier=false
pairs=$*
commands=
for pair in "$@"; do
  commands="$commands ${pair#*=}"
done

# fits RELEASE sets versions when the release passes the validator and the
# image's flag contract with its CLIs first on PATH; otherwise it fails and
# the reasons are on stderr.
fits() {
  # shellcheck disable=SC2086
  versions=$(node -e "$KUAYLE_VALIDATOR" "$modules" "$1" $pairs) || return 1
  # shellcheck disable=SC2086
  PATH="$1/node_modules/.bin:$PATH" timeout 300 sh "$flag_check" $commands >&2 || return 1
}

# rename moves a link over another in one step, as machines may be reading it.
rename() {
  node -e 'require("fs").renameSync(process.argv[1], process.argv[2])' "$1" "$2"
}

state=none
versions=
if [ -L "$root/current" ]; then
  if fits "$root/current"; then
    state=kept
  else
    echo "the shared agent CLIs in $(readlink "$root/current") do not fit this release; machines use their image's" >&2
    rename "$root/current" "$root/rejected"
    state=rejected
    versions=
  fi
fi
echo "` + agentCLIReport + ` $state $versions"
[ "$mode" = refresh ] || exit 0

mkdir -p "$root/releases"
target=$(mktemp -d "$root/releases/$(date -u +%Y%m%d%H%M%S).XXXXXX")
trap 'rm -rf "$target"' EXIT
# Machines read the volume as their own user.
chmod 0755 "$target"
printf '` + agentCLIScripts + `' > "$target/.npmrc"
specs=
for pair in "$@"; do
  name=${pair%%=*}
  image=$(node -p 'require(process.argv[1]).version' "$modules/$name/package.json" 2>/dev/null || true)
  version=$(npm view "$name" time dist-tags --json | node -e "$KUAYLE_RESOLVER" "$modules" "$before" "$image")
  specs="$specs $name@$version"
done
cd "$target"
# shellcheck disable=SC2086
npm install --prefix "$target" --no-audit --no-fund --no-save $specs >&2
for pair in "$@"; do
  command=${pair#*=}
  timeout 60 "$target/node_modules/.bin/$command" --version >/dev/null 2>&1 || {
    echo "$command does not start" >&2
    exit 1
  }
done
fits "$target" || {
  echo "the new agent CLIs do not fit this release and are discarded" >&2
  exit 1
}
# shellcheck disable=SC2086
printf '%s\n' $versions > "$target/versions.txt"
if [ "$state" = kept ] && cmp -s "$target/versions.txt" "$root/current/versions.txt"; then
  echo "` + agentCLIReport + ` unchanged $versions"
  exit 0
fi
# The release a run may still be using (the current one, or the one just set
# aside) stays until the next switch.
previous=$(readlink "$root/current" 2>/dev/null || readlink "$root/rejected" 2>/dev/null || true)
ln -sfn "releases/${target##*/}" "$root/current.next"
trap - EXIT
rename "$root/current.next" "$root/current"
rm -f "$root/rejected"
for release in "$root"/releases/*; do
  case "releases/${release##*/}" in "releases/${target##*/}"|"$previous") ;; *) rm -rf "$release" ;; esac
done
echo "` + agentCLIReport + ` changed $versions"
`

// agentCLIHelperArgs are the helper's shell arguments.
func agentCLIHelperArgs(mode string, before time.Time) []string {
	args := []string{"-c", agentCLIRefreshScript, "kuayle-agent-cli-refresh", mode, before.UTC().Format(time.RFC3339)}
	for _, item := range agentCLIPackages {
		args = append(args, item.name+"="+item.command)
	}
	return args
}

func agentCLIHelperEnv() []string {
	return []string{"KUAYLE_RESOLVER=" + agentCLIResolver, "KUAYLE_VALIDATOR=" + agentCLIValidator}
}

// AgentCLIRefresh is a helper's report. State is "changed" or "unchanged"
// after a refresh; for the release that was current before it, "kept",
// "rejected" (set aside: it did not fit this release) or "none". Versions are
// those of the current release when it fits; Detail explains a rejection.
type AgentCLIRefresh struct {
	State    string
	Versions map[string]string
	Detail   string
}

// Changed reports whether a refresh switched to new versions.
func (r AgentCLIRefresh) Changed() bool { return r.State == "changed" }

// Usable reports whether the volume's current CLIs fit this release.
func (r AgentCLIRefresh) Usable() bool {
	return r.State == "kept" || r.State == "changed" || r.State == "unchanged"
}

// agentCLIRefresher is implemented by runtimes that keep the shared CLIs.
type agentCLIRefresher interface {
	RefreshAgentCLIs(ctx context.Context, image string, before time.Time) (AgentCLIRefresh, error)
	CheckAgentCLIs(ctx context.Context, image string) (AgentCLIRefresh, error)
}

// EnableAgentCLIs mounts the shared CLI volume into agent runs and developer
// containers created from now on.
func (r *DockerRuntime) EnableAgentCLIs() {
	r.agentCLIs = true
}

// useAgentCLIs adds the read-only CLI volume to a new container and puts its
// bin directory on PATH; developer containers always get it. The container
// keeps the mount, so a later refresh or set-aside reaches it; a missing
// volume or `current` link falls through to the image's CLIs.
func (r *DockerRuntime) useAgentCLIs(hostConfig *container.HostConfig, environment []string) []string {
	if !r.agentCLIs {
		return environment
	}
	hostConfig.Mounts = append(hostConfig.Mounts, mount.Mount{Type: mount.TypeVolume, Source: agentCLIVolume, Target: agentCLIRoot, ReadOnly: true})
	return withAgentCLIPath(environment)
}

// useAgentCLIsForRun is useAgentCLIs for an agent run, which passes this
// release's argv: only while a check or refresh of this manager found the
// volume's CLIs to fit this release. Otherwise the run uses its image's CLIs.
func (r *DockerRuntime) useAgentCLIsForRun(hostConfig *container.HostConfig, environment []string) []string {
	if !r.agentCLIsFit.Load() {
		return environment
	}
	return r.useAgentCLIs(hostConfig, environment)
}

// recordAgentCLIs notes, from a helper's report, whether the volume's current
// CLIs fit this release. A helper that did not report changes nothing.
func (r *DockerRuntime) recordAgentCLIs(result AgentCLIRefresh) {
	if result.State != "" {
		r.agentCLIsFit.Store(result.Usable())
	}
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
// receives no machine data or secrets: the CLI volume is its only mount.
func (r *DockerRuntime) RefreshAgentCLIs(ctx context.Context, image string, before time.Time) (AgentCLIRefresh, error) {
	return r.runAgentCLIHelper(ctx, image, "refresh", before)
}

// CheckAgentCLIs checks the volume's current CLIs against image without
// installing anything, in the same helper with no network, and sets them
// aside when they do not fit. The manager calls it once, when it starts, so a
// refresh helper still running is a previous manager's: it is removed first,
// as it could otherwise switch `current` to CLIs of that release afterwards.
func (r *DockerRuntime) CheckAgentCLIs(ctx context.Context, image string) (AgentCLIRefresh, error) {
	helpers, err := r.client.ContainerList(ctx, client.ContainerListOptions{
		All: true, Filters: make(client.Filters).Add("label", "com.kuayle.managed=true").Add("label", "com.kuayle.service="+agentCLIService),
	})
	if err != nil {
		return AgentCLIRefresh{}, err
	}
	for _, helper := range helpers.Items {
		if _, err := r.client.ContainerRemove(ctx, helper.ID, client.ContainerRemoveOptions{Force: true}); err != nil && !errdefs.IsNotFound(err) {
			return AgentCLIRefresh{}, err
		}
	}
	return r.runAgentCLIHelper(ctx, image, "check", time.Time{})
}

// agentCLIHelperContainer is the helper's container: the CLI volume is its
// only mount, so it never sees a machine's workspace (sessions, notebooks'
// files, repositories), sign-ins or secrets. Only a refresh has a network.
func agentCLIHelperContainer(image, mode string, before time.Time) client.ContainerCreateOptions {
	networkMode := container.NetworkMode("none")
	var networking *network.NetworkingConfig
	if mode == "refresh" {
		networkMode = container.NetworkMode(egressNetworkName)
		networking = &network.NetworkingConfig{EndpointsConfig: map[string]*network.EndpointSettings{egressNetworkName: {}}}
	}
	pids := int64(1024)
	return client.ContainerCreateOptions{
		Config: &container.Config{
			Image: image, User: "0:0", Entrypoint: []string{"/bin/sh"}, Cmd: agentCLIHelperArgs(mode, before), Env: agentCLIHelperEnv(),
			Labels: map[string]string{"com.kuayle.managed": "true", "com.kuayle.service": agentCLIService},
		},
		HostConfig: &container.HostConfig{
			NetworkMode: networkMode, ReadonlyRootfs: true,
			CapDrop: []string{"ALL"}, SecurityOpt: []string{"no-new-privileges=true"}, LogConfig: boundedLogConfig(),
			Tmpfs:  map[string]string{"/tmp": "rw,exec,nosuid,size=1g"},
			Mounts: []mount.Mount{{Type: mount.TypeVolume, Source: agentCLIVolume, Target: agentCLIRoot}},
			Resources: container.Resources{
				Memory: 2 << 30, MemorySwap: 2 << 30, NanoCPUs: 2_000_000_000, PidsLimit: &pids,
			},
		},
		NetworkingConfig: networking,
	}
}

func (r *DockerRuntime) runAgentCLIHelper(ctx context.Context, image, mode string, before time.Time) (AgentCLIRefresh, error) {
	if image == "" {
		return AgentCLIRefresh{}, errors.New("no image for the agent CLI " + mode)
	}
	ctx, cancel := context.WithTimeout(ctx, agentCLITimeout)
	defer cancel()
	if err := r.ensureImage(ctx, image); err != nil {
		return AgentCLIRefresh{}, err
	}
	if mode == "refresh" {
		if _, err := r.ensureNetwork(ctx, egressNetworkName, false, map[string]string{"com.kuayle.kind": "machine-egress"}); err != nil {
			return AgentCLIRefresh{}, err
		}
	}
	response, err := r.client.ContainerCreate(ctx, agentCLIHelperContainer(image, mode, before))
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
		// A failed refresh still reports the release machines keep using.
		report, reportErr := parseAgentCLIRefresh(stdout)
		if reportErr == nil {
			if report.State == "rejected" {
				report.Detail = lastLines(stderr, 5)
			}
			r.recordAgentCLIs(report)
		}
		if result.StatusCode != 0 {
			return report, fmt.Errorf("agent CLI %s exited %d: %s", mode, result.StatusCode, lastLines(stderr, 5))
		}
		return report, reportErr
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

// parseAgentCLIRefresh reads the helper's last report line.
func parseAgentCLIRefresh(output string) (AgentCLIRefresh, error) {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for index := len(lines) - 1; index >= 0; index-- {
		fields := strings.Fields(lines[index])
		if len(fields) < 2 || fields[0] != agentCLIReport {
			continue
		}
		result := AgentCLIRefresh{State: fields[1]}
		switch result.State {
		case "rejected", "none":
			if len(fields) > 2 {
				return AgentCLIRefresh{}, fmt.Errorf("agent CLI report %q lists versions", result.State)
			}
			return result, nil
		case "kept", "changed", "unchanged":
			if len(fields) < 3 {
				return AgentCLIRefresh{}, fmt.Errorf("agent CLI report %q lists no versions", result.State)
			}
		default:
			return AgentCLIRefresh{}, fmt.Errorf("unexpected agent CLI refresh state %q", result.State)
		}
		result.Versions = map[string]string{}
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

// AgentCLIVersions are the versions of the shared CLIs agent runs use, while
// they fit this release; empty when runs use their image's CLIs.
func (m *Manager) AgentCLIVersions() map[string]string {
	m.agentCLIMu.Lock()
	defer m.agentCLIMu.Unlock()
	return maps.Clone(m.agentCLIVersions)
}

// recordAgentCLIs keeps the versions a helper reported, or none when the
// volume's CLIs do not fit. A helper that did not report changes nothing.
func (m *Manager) recordAgentCLIs(result AgentCLIRefresh) {
	if result.State == "" {
		return
	}
	m.agentCLIMu.Lock()
	defer m.agentCLIMu.Unlock()
	m.agentCLIVersions = nil
	if result.Usable() {
		m.agentCLIVersions = result.Versions
	}
}

// checkAgentCLIs runs when the manager starts, before it leases any work. A
// deploy brings new images while the volume still holds what the previous
// release installed; CLIs that do not fit this release are set aside, so the
// machines it starts, and those already running, use their image's CLIs
// until a refresh installs ones that fit.
func (m *Manager) checkAgentCLIs(ctx context.Context) {
	refresher, ok := m.runtime.(agentCLIRefresher)
	m.agentCLIMu.Lock()
	enabled := ok && m.agentCLIUpdates.Interval > 0 && m.images.Headless != ""
	m.agentCLIMu.Unlock()
	if !enabled {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, agentCLICheckTimeout)
	defer cancel()
	result, err := refresher.CheckAgentCLIs(ctx, m.images.Headless)
	m.recordAgentCLIs(result)
	if err != nil {
		log.WithError(err).Warn("agent CLI check failed; agent runs use their image's CLIs until a refresh succeeds")
		return
	}
	logAgentCLIs(result, "agent CLIs checked")
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
	m.recordAgentCLIs(result)
	if err != nil {
		m.agentCLIMu.Lock()
		m.agentCLINext = now.Add(min(agentCLIRetry, updates.Interval))
		m.agentCLIMu.Unlock()
		log.WithError(err).WithField("current", result.State).Warn("agent CLI refresh failed; machines keep the current CLIs if they fit, or their image's")
		return
	}
	logAgentCLIs(result, "agent CLIs refreshed")
}

func logAgentCLIs(result AgentCLIRefresh, message string) {
	fields := log.Fields{"state": result.State}
	for name, version := range result.Versions {
		fields[name] = version
	}
	if result.State == "rejected" {
		log.WithFields(fields).WithField("reason", result.Detail).Warn(message + ": the shared CLIs do not fit this release and were set aside; machines use their image's")
		return
	}
	log.WithFields(fields).Info(message)
}
