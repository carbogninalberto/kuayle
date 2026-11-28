package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
)

// ErrTemporaryMachinesOff means the workspace policy allows no temporary
// machines (max_temporary_machines is 0).
var ErrTemporaryMachinesOff = errors.New("temporary machines are turned off in this workspace")

// StartTemporaryMachine creates the tiny headless preset
// (domain.TemporaryMachineResources) for one conversation, as the user, with
// the same policy, quota and demo checks as any machine. It has no repository,
// agents, environment variables, Docker or sudo, so no credential is ever
// minted for it; it uses the machine egress proxy like every machine. The
// workspace allows max_temporary_machines of them and each user one, and the
// manager permanently deletes it once domain.TemporaryMachineTTL has passed.
func (s *DevMachineService) StartTemporaryMachine(ctx context.Context, workspaceID, userID, conversationID uuid.UUID) (*domain.DevMachine, error) {
	preset := domain.TemporaryMachineResources
	req := dto.CreateDevMachineRequest{
		Name: "tmp-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:8], Size: "custom",
		CPUMillis: preset.CPUMillis, MemoryMB: preset.MemoryMB, DiskGB: preset.DiskGB,
	}
	temporary := &temporaryMachine{conversation: conversationID, expiresAt: time.Now().UTC().Add(domain.TemporaryMachineTTL).Truncate(time.Second)}
	machine, _, err := s.create(ctx, workspaceID, userID, req, uuid.New(), temporary)
	if errors.Is(err, repository.ErrTemporaryMachinesOff) {
		return nil, ErrTemporaryMachinesOff
	}
	return machine, err
}
