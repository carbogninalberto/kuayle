package machine

import (
	"context"
	"fmt"
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
