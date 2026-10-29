package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/internal/dto"
	cryptoutil "github.com/kuayle/kuayle-backend/pkg/crypto"
	"github.com/stretchr/testify/require"
)

// A machine records whether the workspace allowed sudo when it was created;
// its developer containers are configured from that record.
func TestCreateRecordsSudoOnlyWhenTheWorkspaceAllowsIt(t *testing.T) {
	for _, allowed := range []bool{false, true} {
		workspaceID, userID := uuid.New(), uuid.New()
		policy := testPolicy(workspaceID)
		policy.AllowSudo = allowed
		store := &devMachineStoreFake{policy: policy}
		svc := NewDevMachineService(store, agent.NewRegistry(), true, "machines.example.test", cryptoutil.DeriveKey("test"), time.Minute, DevMachineImages{})
		machine, _, err := svc.Create(context.Background(), workspaceID, userID, dto.CreateDevMachineRequest{Name: "sudo-machine", Size: "small", Services: dto.DevMachineServicesInput{IDE: true}})
		require.NoError(t, err)
		require.Equal(t, allowed, machine.SudoEnabled)
		require.Equal(t, allowed, store.createdMachine.SudoEnabled)
	}
}
