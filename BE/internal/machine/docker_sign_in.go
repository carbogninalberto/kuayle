package machine

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// signInFiles is where each agent keeps its own sign-in in the user's home.
var signInFiles = map[string]string{
	"claude-code": "/home/kuayle/.claude/.credentials.json",
	"codex":       "/home/kuayle/.codex/auth.json",
}

// ReadSignIn reads an agent's sign-in from the machine's terminal, then IDE,
// container, where the user signed in. It returns "" when there is none.
func (r *DockerRuntime) ReadSignIn(ctx context.Context, services []domain.DevMachineService, provider string) (string, error) {
	path, ok := signInFiles[provider]
	if !ok {
		return "", fmt.Errorf("agent %q has no sign-in file", provider)
	}
	for _, serviceType := range []string{"terminal", "ide"} {
		for _, service := range services {
			if service.ServiceType != serviceType || service.ContainerID == nil {
				continue
			}
			content, err := r.execOutput(ctx, *service.ContainerID, []string{"sh", "-c", `[ -f "$1" ] || exit 0; cat "$1"`, "read-sign-in", path})
			if err != nil {
				return "", err
			}
			if strings.TrimSpace(content) != "" {
				return content, nil
			}
		}
	}
	return "", nil
}

// TakeSignInReturn reads and removes the refreshed sign-in an agent run handed
// back on the workspace volume, so it does not stay on disk. It returns "" when
// the run left none.
func (r *DockerRuntime) TakeSignInReturn(ctx context.Context, services []domain.DevMachineService, runID uuid.UUID) (string, error) {
	containerID := developerContainerID(services)
	if containerID == "" {
		return "", nil
	}
	path := "/workspace/.kuayle/sign-in-return/" + runID.String() + ".json"
	return r.execOutput(ctx, containerID, []string{"sh", "-c", `[ -f "$1" ] || exit 0; cat "$1"; rm -f "$1"`, "take-sign-in", path})
}

// syncAgentSignInsScript replaces ~/.kuayle/agent-sign-ins with the files of
// the tar archive on stdin. The sign-ins stay in the container's in-memory
// home, readable only by the machine user, and never appear in argv.
const syncAgentSignInsScript = `set -eu
umask 077
base="${HOME:-/home/kuayle}/.kuayle"
target="$base/agent-sign-ins"
mkdir -p "$base"
staging=$(mktemp -d "$base/.agent-sign-ins.XXXXXX")
if ! tar -x -f - -C "$staging" --no-same-owner --no-same-permissions; then
  rm -rf "$staging"
  exit 1
fi
chmod 700 "$staging"
rm -rf "$target.old"
if [ -e "$target" ] || [ -L "$target" ]; then mv "$target" "$target.old"; fi
mv "$staging" "$target"
rm -rf "$target.old"`

// SyncAgentSignIns writes the owner's agent sign-ins into the developer
// container, replacing the previous set; it does nothing without one.
func (r *DockerRuntime) SyncAgentSignIns(ctx context.Context, services []domain.DevMachineService, files map[string]string) error {
	containerID := developerContainerID(services)
	if containerID == "" {
		return nil
	}
	archive, err := signInArchive(files)
	if err != nil {
		return err
	}
	return r.execWithInput(ctx, containerID, []string{"/bin/sh", "-c", syncAgentSignInsScript, "kuayle-agent-sign-ins"}, archive)
}

// signInArchive packs the sign-in files, readable only by their owner, into a
// tar archive. Names are plain file names chosen by the manager.
func signInArchive(files map[string]string) (string, error) {
	names := make([]string, 0, len(files))
	for name := range files {
		if name == "" || strings.ContainsAny(name, "/\\") || strings.HasPrefix(name, ".") {
			return "", fmt.Errorf("invalid sign-in file name")
		}
		names = append(names, name)
	}
	sort.Strings(names)
	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	for _, name := range names {
		content := files[name]
		if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
			return "", err
		}
		if _, err := writer.Write([]byte(content)); err != nil {
			return "", err
		}
	}
	if err := writer.Close(); err != nil {
		return "", err
	}
	return buffer.String(), nil
}
