package machine

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/internal/domain"
	cryptoutil "github.com/kuayle/kuayle-backend/pkg/crypto"
	"github.com/stretchr/testify/require"
)

type sudoStoreFake struct {
	*managerStoreFake
	sudoAllowed bool
	sudoChecks  int
}

func (f *sudoStoreFake) SudoAllowed(context.Context, uuid.UUID) (bool, error) {
	f.sudoChecks++
	return f.sudoAllowed, nil
}

// A machine created with sudo does not start once the workspace turned sudo
// off: its developer containers keep the relaxed configuration they were
// created with, so it is not silently started with it either.
func TestStartingASudoMachineIsRefusedWhenSudoIsNoLongerAllowed(t *testing.T) {
	for _, action := range []domain.DevMachineOperationAction{domain.DevMachineOpSpawn, domain.DevMachineOpStart} {
		machine := &domain.DevMachine{ID: uuid.New(), WorkspaceID: uuid.New(), RoutingKey: "0123456789abcdef0123", Status: domain.DevMachineStatusStopped, DesiredStatus: domain.DevMachineStatusRunning, Generation: 1, SudoEnabled: true}
		store := &sudoStoreFake{managerStoreFake: &managerStoreFake{machine: machine, services: []domain.DevMachineService{{ServiceKey: "terminal", ServiceType: "terminal"}}}}
		runtime := &runtimeFake{}
		manager := NewManager(store, runtime, agent.NewRegistry(), nil, cryptoutil.DeriveKey("sudo-policy-test"), nil, "test")
		err := manager.processOperation(context.Background(), &domain.DevMachineOperation{MachineID: machine.ID, WorkspaceID: machine.WorkspaceID, Action: action, Generation: 1})
		var terminal *terminalOperationError
		require.ErrorAs(t, err, &terminal, action)
		require.Equal(t, "sudo_not_allowed", terminal.code)
		require.Zero(t, runtime.spawned+runtime.started, "nothing is started")
		require.Equal(t, 1, store.sudoChecks)
	}
}

// Machines without sudo never consult the switch: turning it off does not
// affect them.
func TestMachinesWithoutSudoIgnoreTheSudoSwitch(t *testing.T) {
	machine := &domain.DevMachine{ID: uuid.New(), WorkspaceID: uuid.New(), RoutingKey: "0123456789abcdef0123", Status: domain.DevMachineStatusStopped, DesiredStatus: domain.DevMachineStatusRunning, Generation: 1}
	store := &sudoStoreFake{managerStoreFake: &managerStoreFake{machine: machine, services: []domain.DevMachineService{{ServiceKey: "terminal", ServiceType: "terminal"}}}}
	manager := NewManager(store, &runtimeFake{}, agent.NewRegistry(), nil, cryptoutil.DeriveKey("sudo-policy-test"), nil, "test")
	err := manager.processOperation(context.Background(), &domain.DevMachineOperation{MachineID: machine.ID, WorkspaceID: machine.WorkspaceID, Action: domain.DevMachineOpStart, Generation: 1})
	var terminal *terminalOperationError
	if errors.As(err, &terminal) {
		require.NotEqual(t, "sudo_not_allowed", terminal.code)
	}
	require.Zero(t, store.sudoChecks)
}
