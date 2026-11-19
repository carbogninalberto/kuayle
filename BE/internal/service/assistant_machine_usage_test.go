package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

type machineUsageStore struct {
	*devMachineStoreFake
	usage *domain.DevMachineUsage
	reads int
}

func (s *machineUsageStore) GetResourceUsage(context.Context, uuid.UUID, uuid.UUID) (*domain.DevMachineUsage, error) {
	s.reads++
	return s.usage, nil
}

func TestAssistantMachineUsageAccessAndResolution(t *testing.T) {
	ws, creator, admin, owner, member, guest := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	issue := &domain.Issue{ID: uuid.New(), TeamID: uuid.New(), Identifier: "ENG-1"}
	machine := &domain.DevMachine{ID: uuid.New(), WorkspaceID: ws, CreatedByUserID: &creator, Name: "test-machine", CPUMillis: 4000, MemoryMB: 8192, DiskGB: 20}
	task := &domain.AgentTask{ID: uuid.New(), WorkspaceID: ws, IssueID: issue.ID, TeamID: issue.TeamID, CreatorID: creator, MachineID: &machine.ID}
	store := &machineUsageStore{devMachineStoreFake: &devMachineStoreFake{machine: machine}}
	members := prMembers{creator: domain.RoleMember, admin: domain.RoleAdmin, owner: domain.RoleOwner, member: domain.RoleMember, guest: domain.RoleGuest}
	tools := NewAssistantTools(prIssues{issue: issue}, nil, members, nil)
	tasks := &machineTasksFake{task: task}
	tools.SetMachines(tasks, newTestDevMachineService(store))

	ctx := context.Background()
	byIssue := `{"identifier":" eng-1 "}`
	byMachine := fmt.Sprintf(`{"machine_id":%q}`, machine.ID)
	for _, tc := range []struct {
		name        string
		user        uuid.UUID
		arguments   string
		wantIssueID *uuid.UUID
	}{
		{"creator by issue", creator, byIssue, &issue.ID},
		{"admin by issue", admin, byIssue, &issue.ID},
		{"owner by issue", owner, byIssue, &issue.ID},
		{"creator by id", creator, byMachine, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output, issueID, err := tools.run(ctx, ws, tc.user, getMachineUsageTool, tc.arguments)
			require.NoError(t, err)
			require.Equal(t, tc.wantIssueID, issueID)
			result := output.(map[string]any)
			require.Equal(t, machine.ID, result["machine_id"])
			require.Equal(t, map[string]int{"cpu_millis": 4000, "memory_mb": 8192, "disk_gb": 20}, result["limits"])
			require.Nil(t, result["sampled_at"])
			require.Nil(t, result["latest"])
			require.Nil(t, result["peak"])
			require.Contains(t, result["note"], "No resource samples")
			if issueID != nil {
				require.Equal(t, "ENG-1", result["identifier"])
				require.Equal(t, task.ID, result["task_id"])
			}
		})
	}

	for _, tc := range []struct {
		name      string
		user      uuid.UUID
		arguments string
		message   string
	}{
		{"unrelated member by issue", member, byIssue, "only workspace admins or the task's creator"},
		{"unrelated member by id", member, byMachine, "machine not found"},
		{"admin by id", admin, byMachine, "machine not found"},
		{"guest", guest, byIssue, "you no longer have access"},
		{"outsider", uuid.New(), byIssue, "you no longer have access"},
		{"both targets", creator, fmt.Sprintf(`{"identifier":"ENG-1","machine_id":%q}`, machine.ID), "give machine_id or identifier, not both"},
		{"no target", creator, `{}`, "give machine_id or identifier"},
		{"invalid id", creator, `{"machine_id":"unknown"}`, "machine_id is not a machine id"},
		{"missing issue", creator, `{"identifier":"ENG-2"}`, "issue not found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := tools.run(ctx, ws, tc.user, getMachineUsageTool, tc.arguments)
			require.ErrorContains(t, err, tc.message)
		})
	}

	_, _, err := tools.run(ctx, uuid.New(), creator, getMachineUsageTool, byMachine)
	require.ErrorContains(t, err, "machine not found")
	task.TeamID = uuid.New()
	_, _, err = tools.run(ctx, ws, creator, getMachineUsageTool, byIssue)
	require.ErrorContains(t, err, "previous team")
	task.TeamID, task.MachineID = issue.TeamID, nil
	_, _, err = tools.run(ctx, ws, creator, getMachineUsageTool, byIssue)
	require.ErrorContains(t, err, "no machine yet")
	tasks.task = nil
	_, _, err = tools.run(ctx, ws, creator, getMachineUsageTool, byIssue)
	require.ErrorContains(t, err, "no coding task")

	store.usage = &domain.DevMachineUsage{
		SampledAt: time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC),
		Latest:    domain.DevMachineUsageValues{CPUPercent: 78.5, MemoryBytes: 2 << 30, DiskBytes: 10 << 30},
		Peak:      domain.DevMachineUsageValues{CPUPercent: 180, MemoryBytes: 5 << 30, DiskBytes: 18 << 30},
	}
	output, _, err := tools.run(ctx, ws, creator, getMachineUsageTool, byMachine)
	require.NoError(t, err)
	data, err := json.Marshal(output)
	require.NoError(t, err)
	require.JSONEq(t, fmt.Sprintf(`{
		"machine_id":%q,"machine":"test-machine","sampled_at":"2026-10-30T12:00:00Z",
		"limits":{"cpu_millis":4000,"memory_mb":8192,"disk_gb":20},
		"latest":{"cpu_percent":78.5,"memory_bytes":2147483648,"disk_bytes":10737418240},
		"peak":{"cpu_percent":180,"memory_bytes":5368709120,"disk_bytes":19327352832}
	}`, machine.ID), string(data))

	tools.SetMachineAccess(NewMachineAccess(func(uuid.UUID) bool { return false }))
	_, _, err = tools.run(ctx, ws, creator, getMachineUsageTool, byMachine)
	require.ErrorContains(t, err, demoMachineRefusal)
	require.Equal(t, 5, store.reads, "only authorized, resolved machines reach the sample query")
}

func TestAssistantMachineUsageRespectsIssueVisibility(t *testing.T) {
	f, tasks, _ := liveFixture(t)
	machines := repository.NewDevMachineRepository(f.db)
	f.service.tools.SetMachines(tasks, newTestDevMachineService(machines))
	_, err := f.db.Exec(`UPDATE teams SET is_private=TRUE WHERE workspace_id=$1`, f.ws)
	require.NoError(t, err)
	_, _, err = f.service.tools.run(f.ctx, f.ws, f.user, getMachineUsageTool, `{"identifier":"ENG-1"}`)
	require.ErrorContains(t, err, "issue not found", "task ownership cannot bypass issue visibility")
}

func TestAssistantMachineUsageReportsLatestAndIndependentPeaks(t *testing.T) {
	f, tasks, olderTask := liveFixture(t)
	machines := repository.NewDevMachineRepository(f.db)
	f.service.tools.SetMachines(tasks, newTestDevMachineService(machines))
	insertMachine := func(name string) uuid.UUID {
		t.Helper()
		id := uuid.New()
		_, err := f.db.Exec(`INSERT INTO dev_machines
			(id, workspace_id, created_by_user_id, name, routing_key, machine_size, cpu_millis, memory_mb, disk_gb)
			VALUES ($1,$2,$3,$4,$5,'custom',4000,8192,20)`, id, f.ws, f.user, name, strings.ReplaceAll(id.String(), "-", ""))
		require.NoError(t, err)
		return id
	}
	olderMachine, machine := insertMachine("old-machine"), insertMachine("task-machine")
	_, err := f.db.Exec(`UPDATE agent_tasks SET machine_id=$2, state='failed' WHERE id=$1`, olderTask, olderMachine)
	require.NoError(t, err)
	var task uuid.UUID
	err = f.db.Get(&task, `INSERT INTO agent_tasks (workspace_id,creator_id,issue_id,team_id,request_id,prompt,provider,state,machine_id,created_at)
		SELECT workspace_id,creator_id,issue_id,team_id,gen_random_uuid(),prompt,provider,'failed',$2,created_at+interval '1 second'
		FROM agent_tasks WHERE id=$1 RETURNING id`, olderTask, machine)
	require.NoError(t, err)

	at := time.Now().UTC().Truncate(time.Second)
	const giB int64 = 1 << 30
	insertSample := func(id uuid.UUID, offset time.Duration, cpu float64, memory, disk int64) {
		t.Helper()
		_, err := f.db.Exec(`INSERT INTO dev_machine_resource_samples (workspace_id,machine_id,created_at,cpu_percent,memory_bytes,disk_bytes)
			VALUES ($1,$2,$3,$4,$5,$6)`, f.ws, id, at.Add(offset), cpu, memory, disk)
		require.NoError(t, err)
	}
	insertSample(olderMachine, time.Minute, 400, 8*giB, 20*giB)
	insertSample(machine, 0, 12.5, 2*giB, 10*giB)
	insertSample(machine, -time.Minute, 320, 3*giB, 12*giB)
	insertSample(machine, -2*time.Minute, 20, 5*giB, 15*giB)
	insertSample(machine, -3*time.Minute, 25, giB, 18*giB)

	content, _, approvals := liveToolResult(t, f, getMachineUsageTool, `{"identifier":"eng-1"}`)
	require.Empty(t, approvals)
	require.JSONEq(t, fmt.Sprintf(`{"untrusted_data":{
		"identifier":"ENG-1","task_id":%q,"machine_id":%q,"machine":"task-machine","sampled_at":%q,
		"limits":{"cpu_millis":4000,"memory_mb":8192,"disk_gb":20},
		"latest":{"cpu_percent":12.5,"memory_bytes":%d,"disk_bytes":%d},
		"peak":{"cpu_percent":320,"memory_bytes":%d,"disk_bytes":%d}
	}}`, task, machine, at.Format(time.RFC3339), 2*giB, 10*giB, 5*giB, 18*giB), content)

	_, err = f.db.Exec(`DELETE FROM dev_machine_resource_samples WHERE machine_id=$1`, machine)
	require.NoError(t, err)
	output, issueID, err := f.service.tools.run(f.ctx, f.ws, f.user, getMachineUsageTool, fmt.Sprintf(`{"machine_id":%q}`, machine))
	require.NoError(t, err)
	require.Nil(t, issueID)
	data, err := json.Marshal(output)
	require.NoError(t, err)
	require.JSONEq(t, fmt.Sprintf(`{"machine_id":%q,"machine":"task-machine","sampled_at":null,"latest":null,"peak":null,
		"limits":{"cpu_millis":4000,"memory_mb":8192,"disk_gb":20},"note":"No resource samples are available for this machine."}`, machine), string(data))
}
