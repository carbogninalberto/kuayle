package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

// Each refusal of an approved command or temporary machine says what happened
// and what to do next; a transient error has no message, so the claim stays.
func TestCommandApprovalFailureNamesEachRefusal(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"not a member", repository.ErrConversationNotFound, "You no longer have access to this workspace"},
		{"guest", repository.ErrCommandRunGuest, "Guests cannot run commands; ask a workspace member to run it, or ask an admin to change your role"},
		{"role without Dev Machines", repository.ErrCommandRunForbidden, "Your role cannot use Dev Machines; ask a workspace member to run it, or ask an admin to change your role"},
		{"wrapped guest", fmt.Errorf("create: %w", repository.ErrCommandRunGuest), "Guests cannot run commands; ask a workspace member to run it, or ask an admin to change your role"},
		{"machine", repository.ErrCommandMachineUnavailable, upperFirst(notMachineOwner)},
		{"transient", errors.New("connection reset"), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, commandApprovalFailure(tc.err))
		})
	}
}

// commandStoreStub stands in for the command repository in the machine-choice
// unit tests. Every machine lookup defaults to "unavailable" so a test only
// sets the machine it wants the assistant to find.
type commandStoreStub struct {
	conversation      *domain.DevMachine
	conversationErr   error
	reusable          *domain.DevMachine
	reusableErr       error
	userTemporary     *domain.DevMachine
	userTemporaryErr  error
	used, limit, mine int
	quotaErr          error
}

func (f *commandStoreStub) QueueRun(context.Context, uuid.UUID, uuid.UUID, repository.CommandRunRequest) (*domain.CommandRun, error) {
	return nil, nil
}
func (f *commandStoreStub) Run(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (*domain.CommandRun, error) {
	return nil, nil
}
func (f *commandStoreStub) RunForApproval(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (*domain.CommandRun, error) {
	return nil, nil
}
func (f *commandStoreStub) UsableMachine(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (*domain.DevMachine, error) {
	return nil, repository.ErrCommandMachineUnavailable
}
func (f *commandStoreStub) ConversationMachine(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (*domain.DevMachine, error) {
	if f.conversationErr != nil {
		return nil, f.conversationErr
	}
	if f.conversation != nil {
		return f.conversation, nil
	}
	return nil, repository.ErrCommandMachineUnavailable
}
func (f *commandStoreStub) ReusableMachine(context.Context, uuid.UUID, uuid.UUID) (*domain.DevMachine, error) {
	if f.reusableErr != nil {
		return nil, f.reusableErr
	}
	if f.reusable != nil {
		return f.reusable, nil
	}
	return nil, repository.ErrCommandMachineUnavailable
}
func (f *commandStoreStub) UserTemporaryMachine(context.Context, uuid.UUID, uuid.UUID) (*domain.DevMachine, error) {
	if f.userTemporaryErr != nil {
		return nil, f.userTemporaryErr
	}
	if f.userTemporary != nil {
		return f.userTemporary, nil
	}
	return nil, repository.ErrCommandMachineUnavailable
}
func (f *commandStoreStub) TemporaryMachineQuota(context.Context, uuid.UUID, uuid.UUID) (int, int, int, error) {
	return f.used, f.limit, f.mine, f.quotaErr
}
func (f *commandStoreStub) CheckRunner(context.Context, uuid.UUID, uuid.UUID) error { return nil }

func commandToolsWith(store *commandStoreStub, user uuid.UUID) *AssistantTools {
	tools := &AssistantTools{members: prMembers{user: domain.RoleMember}}
	tools.SetCommands(store, nil)
	return tools
}

func commandAttempt(user uuid.UUID) domain.ConversationAttempt {
	return domain.ConversationAttempt{Conversation: domain.Conversation{ID: uuid.New(), WorkspaceID: uuid.New(), CreatorID: user}}
}

func liveTemporaryMachineNamed(name string) *domain.DevMachine {
	expires := time.Now().Add(domain.TemporaryMachineTTL)
	return &domain.DevMachine{ID: uuid.New(), Name: name, Status: domain.DevMachineStatusRunning, TemporaryExpiresAt: &expires,
		CPUMillis: domain.TemporaryMachineResources.CPUMillis, MemoryMB: domain.TemporaryMachineResources.MemoryMB, DiskGB: domain.TemporaryMachineResources.DiskGB}
}

// A command with no machine_id reuses a temporary machine another conversation
// started, but only once it is running; with no machine at all the model is
// told it can start one.
func TestCommandMachineReusesTheUsersTemporaryMachine(t *testing.T) {
	user, ws := uuid.New(), uuid.New()
	tmp := liveTemporaryMachineNamed("tmp-other")
	store := &commandStoreStub{userTemporary: tmp}
	tools := commandToolsWith(store, user)

	machine, reused, err := tools.commandMachine(context.Background(), ws, user, uuid.New(), "")
	require.NoError(t, err)
	require.True(t, reused, "a temporary machine from another conversation is reused")
	require.Same(t, tmp, machine)

	tmp.Status = domain.DevMachineStatusSpawning
	_, _, err = tools.commandMachine(context.Background(), ws, user, uuid.New(), "")
	require.EqualError(t, err, "The temporary machine tmp-other is still starting; ask again in a minute.")

	store.userTemporary = nil
	_, _, err = tools.commandMachine(context.Background(), ws, user, uuid.New(), "")
	require.ErrorContains(t, err, "start_temporary_machine", "with the next step")
}

// start_temporary_machine returns the user's temporary machine from another
// conversation at once, in this conversation's folder, with no approval card.
func TestPrepareTemporaryMachineReusesTheUsersTemporaryMachine(t *testing.T) {
	user := uuid.New()
	tmp := liveTemporaryMachineNamed("tmp-other")
	tools := commandToolsWith(&commandStoreStub{userTemporary: tmp}, user)
	a := commandAttempt(user)

	payload, approval, err := tools.prepareTemporaryMachine(context.Background(), a, `{}`)
	require.NoError(t, err)
	require.Nil(t, payload, "nothing is started, so no approval is needed")
	require.NotNil(t, approval)
	require.Equal(t, true, approval["reused"])
	require.Equal(t, true, approval["temporary"])
	require.Equal(t, tmp.ID, approval["machine_id"])
	require.Equal(t, domain.CommandFolder(a.Conversation.ID), approval["working_directory"], "each conversation keeps its own folder")
	require.Contains(t, approval["note"], "another conversation")
}

// A temporary machine that counts toward the quota but cannot run commands is
// refused before a card is shown that approval would then reject.
func TestPrepareTemporaryMachineRefusesUnusableTemporaryMachineBeforeTheCard(t *testing.T) {
	user := uuid.New()
	tools := commandToolsWith(&commandStoreStub{used: 1, limit: 2, mine: 1}, user)

	_, _, err := tools.prepareTemporaryMachine(context.Background(), commandAttempt(user), `{}`)
	require.EqualError(t, err, "You already have a temporary machine in this workspace that cannot run commands now; wait until it expires, delete it, or give machine_id of one of your running machines.")
}

// With no machine of their own, the approval card still carries the quota.
func TestPrepareTemporaryMachineStillOffersQuotaWhenNoneIsLive(t *testing.T) {
	user := uuid.New()
	tools := commandToolsWith(&commandStoreStub{used: 1, limit: 2}, user)

	payload, approval, err := tools.prepareTemporaryMachine(context.Background(), commandAttempt(user), `{}`)
	require.NoError(t, err)
	require.NotNil(t, payload)
	require.Nil(t, approval)
	require.Equal(t, 1, payload.QuotaUsed)
	require.Equal(t, 2, payload.QuotaLimit)
}

func TestPrepareCommandTimeout(t *testing.T) {
	user := uuid.New()
	tools := commandToolsWith(&commandStoreStub{reusable: runningMachineNamed("dev")}, user)
	for _, action := range []string{"run_command", "run_command_write"} {
		for _, tc := range []struct {
			input string
			want  int
		}{
			{`{"command":"pwd"}`, 60},
			{`{"command":"pwd","timeout_seconds":1}`, 1},
			{`{"command":"pwd","timeout_seconds":600}`, 600},
			{`{"command":"pwd","timeout_seconds":0}`, 0},
			{`{"command":"pwd","timeout_seconds":-1}`, 0},
			{`{"command":"pwd","timeout_seconds":601}`, 0},
			{`{"command":"pwd","timeout_seconds":1.5}`, 0},
		} {
			payload, err := tools.prepareCommand(t.Context(), commandAttempt(user), action, tc.input)
			if tc.want == 0 {
				require.Error(t, err, tc.input)
			} else {
				require.NoError(t, err, tc.input)
				require.Equal(t, tc.want, payload.TimeoutSeconds)
			}
		}
	}
}

func TestCommandApprovalRefusesOutOfRangeTimeout(t *testing.T) {
	store := &queueRunStore{}
	approvals := &AssistantApprovals{conversations: &repository.ConversationRepository{}, commands: store}
	approval := &domain.ConversationApproval{Action: "run_command", Payload: []byte(`{"command":"pwd","timeout_seconds":601}`)}
	_, _ = approvals.runCommand(t.Context(), uuid.New(), uuid.New(), approval)
	require.Empty(t, store.queued)
}
