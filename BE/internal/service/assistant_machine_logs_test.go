package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

func TestAssistantMachineLogsAccess(t *testing.T) {
	ws, creator, admin := uuid.New(), uuid.New(), uuid.New()
	issue := &domain.Issue{ID: uuid.New(), TeamID: uuid.New(), Identifier: "ENG-1"}
	machine := &domain.DevMachine{ID: uuid.New(), WorkspaceID: ws, CreatedByUserID: &creator}
	task := &domain.AgentTask{ID: uuid.New(), WorkspaceID: ws, IssueID: issue.ID, TeamID: issue.TeamID, CreatorID: creator, MachineID: &machine.ID}
	tools := NewAssistantTools(prIssues{issue: issue}, nil, prMembers{creator: domain.RoleMember, admin: domain.RoleAdmin}, nil)
	tools.SetMachines(&machineTasksFake{task: task}, newTestDevMachineService(&devMachineStoreFake{machine: machine}))
	scans := &fakeWorktreeScanStore{createScan: completedWorktreeScan(t, domain.WorktreeScanResult{Content: "abc app running"})}
	tools.SetWorktreeScans(scans)
	run := func(user uuid.UUID, args string) error {
		ctx := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: ws, UserID: user})
		_, _, err := tools.run(ctx, ws, user, "get_machine_logs", args)
		return err
	}
	byIssue := `{"identifier":"ENG-1","source":"containers"}`
	byMachine := fmt.Sprintf(`{"machine_id":%q,"source":"containers"}`, machine.ID)
	require.NoError(t, run(creator, byIssue))
	require.Equal(t, &machine.ID, scans.target.MachineID)
	require.NoError(t, run(creator, byMachine))
	require.Error(t, run(admin, byIssue))
	require.Error(t, run(admin, byMachine))
	require.Error(t, run(uuid.New(), byIssue))
	for _, args := range []string{
		`{"identifier":"ENG-1","source":"agent","lines":0}`,
		`{"identifier":"ENG-1","source":"container","container":"app","lines":101}`,
		`{"identifier":"ENG-1","source":"containers","container":"app"}`,
		`{"identifier":"ENG-1","source":"container","container":"--follow"}`,
		`{"identifier":"ENG-1","source":"container"}`,
		`{"identifier":"ENG-1","source":"exec"}`,
		`{"identifier":"ENG-1","source":"agent","machine_id":"both"}`,
	} {
		require.Error(t, run(creator, args), args)
	}
	task.TeamID = uuid.New()
	require.Error(t, run(creator, byIssue))
	tools.SetMachineAccess(NewMachineAccess(func(uuid.UUID) bool { return false }))
	require.ErrorContains(t, run(creator, byMachine), demoMachineRefusal)
	require.Equal(t, 2, scans.creates)
	tools.SetMachineAccess(nil)
	scans.createErr = repository.ErrWorktreeUnavailable
	require.ErrorContains(t, run(creator, byMachine), "running machine you own")
}

func TestAgentLogOutputWithIncompleteHistory(t *testing.T) {
	chunks := []repository.MachineLogChunk{
		{Channel: "agent", Sequence: 4, Content: "suffix\nsafe"},
		{Channel: "other", Sequence: 0, Content: "standalone\n"},
		{Channel: "agent", Sequence: 1, Content: "secret-"},
	}
	out := agentLogOutput(chunks, true, 10, nil)
	require.Equal(t, domain.MachineLogResult{Content: "standalone\nsafe", Truncated: true}, out)
}

type logTailStore struct {
	*devMachineStoreFake
	task *uuid.UUID
}

func (s *logTailStore) RecentAgentLogs(_ context.Context, _, _, _ uuid.UUID, task *uuid.UUID) ([]repository.MachineLogChunk, bool, error) {
	s.task = task
	return []repository.MachineLogChunk{{Channel: "stdout", Content: "pass\nnewest\n"}, {Channel: "stderr", Content: "warning\n"}, {Channel: "stdout", Content: "old\npassword=fixture-"}}, false, nil
}

func TestAssistantMachineLogsTailsAgentRecords(t *testing.T) {
	ws, user := uuid.New(), uuid.New()
	machine := &domain.DevMachine{ID: uuid.New(), WorkspaceID: ws, CreatedByUserID: &user, Status: domain.DevMachineStatusStopped}
	issue := &domain.Issue{ID: uuid.New(), TeamID: uuid.New(), Identifier: "ENG-1"}
	task := &domain.AgentTask{ID: uuid.New(), WorkspaceID: ws, CreatorID: user, IssueID: issue.ID, TeamID: issue.TeamID, MachineID: &machine.ID}
	store := &logTailStore{devMachineStoreFake: &devMachineStoreFake{machine: machine}}
	tools := NewAssistantTools(prIssues{issue: issue}, nil, prMembers{user: domain.RoleMember}, nil)
	tools.SetMachines(&machineTasksFake{task: task}, newTestDevMachineService(store))
	require.False(t, tools.available(machineLogsTool))
	scans := &fakeWorktreeScanStore{}
	tools.SetWorktreeScans(scans)
	require.True(t, tools.available(machineLogsTool))
	require.False(t, machineLogsTool.write || machineLogsTool.approval)
	ctx := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: ws, UserID: user})
	out, issueID, err := tools.run(ctx, ws, user, getMachineLogsTool, `{"source":"agent","identifier":"ENG-1","lines":2}`)
	require.NoError(t, err)
	require.Equal(t, &issue.ID, issueID)
	require.Equal(t, &task.ID, store.task)
	require.Equal(t, domain.MachineLogResult{Content: "password=[REDACTED]\nnewest", Truncated: true}, out)
	require.Zero(t, scans.creates)
}
