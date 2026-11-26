package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/pkg/crypto"
	"github.com/stretchr/testify/require"
)

func TestScheduledJobLocalTimeAndBounds(t *testing.T) {
	zone, err := time.LoadLocation("Europe/Rome")
	require.NoError(t, err)
	now := time.Date(2026, 3, 28, 12, 0, 0, 0, time.UTC)
	spec := jobSpec{Kind: "cron", Cron: "0 9 * * *", Action: "notify", Instructions: "Morning report"}
	next, _, err := spec.schedule(now, zone)
	require.NoError(t, err)
	require.Equal(t, "2026-03-29T07:00:00Z", next.UTC().Format(time.RFC3339))
	spec = jobSpec{Kind: "interval", IntervalSeconds: 1, Action: "notify", Instructions: "Ping"}
	_, _, err = spec.schedule(now, zone)
	require.Error(t, err)
}

func TestScheduledJobProbeAndLimits(t *testing.T) {
	f := newWatchFixture()
	svc := ConversationService{tools: f.tools}
	for _, name := range []string{"schedule_job", "list_scheduled_jobs", "cancel_scheduled_job"} {
		tool, ok := lookupAssistantTool(name)
		require.True(t, ok)
		require.False(t, f.tools.available(tool))
	}
	svc.SetScheduledJobs(nil, nil)
	require.False(t, svc.scheduledToolAllowed("stop_preview"))
	require.False(t, svc.scheduledToolAllowed("create_issue"))
	require.False(t, svc.scheduledToolAllowed("schedule_job"))
	require.True(t, svc.scheduledToolAllowed("delegate_issue"))
	c := jobCondition{Path: "disks.0.usage", Op: "gt", Value: 90.0}
	matched, err := c.matches(map[string]any{"disks": []any{map[string]string{"usage": "95\n"}}})
	require.NoError(t, err)
	require.True(t, matched)
	_, err = c.matches(map[string]any{"disks": []any{}})
	require.Error(t, err)
}

func TestScheduledJobNotificationUsesChatAndIssueScope(t *testing.T) {
	j := domain.ConversationJob{ID: uuid.New(), UserID: uuid.New(), WorkspaceID: uuid.New(), ConversationID: uuid.New(), IssueID: new(uuid.New())}
	notifications, events := &notificationsFake{}, &userEventsFake{}
	svc := ConversationService{jobNotifications: notifications, jobEvents: events}
	svc.notifyJob(t.Context(), j, "Check the notes")
	require.Equal(t, []createdNotification{{user: j.UserID, workspace: j.WorkspaceID, issue: j.IssueID, kind: "scheduled_job", title: "Check the notes"}}, notifications.created)
	require.Len(t, events.events, 1)
	event := notificationEvent{Type: "scheduled_job", IssueID: j.IssueID, Extra: events.events[0].payload}
	payload := makePushPayload("workspace", event)
	require.Equal(t, "/workspace/assistant?conversation="+j.ConversationID.String(), payload.Data.URL)
	require.Equal(t, "scheduled_job:"+j.ID.String(), payload.Tag)
	access := pushAccess{authorizeChat: func(_ context.Context, ws, chat uuid.UUID) error {
		require.Equal(t, j.WorkspaceID, ws)
		require.Equal(t, j.ConversationID, chat)
		return repository.ErrConversationNotFound
	}}
	require.ErrorIs(t, access.authorizeEvent(t.Context(), pushJob{workspaceID: j.WorkspaceID, userID: j.UserID, event: event}), repository.ErrConversationNotFound)
}

func TestScheduledJobQueuesReleaseApprovalInOriginalChat(t *testing.T) {
	f := newToolFixture(t)
	setRole(t, f, "admin")
	events := &userEventsFake{}
	f.service.SetScheduledJobs(&notificationsFake{}, events)
	github := newReleaseFake()
	f.service.tools.SetReleases(github, releaseRepos{}, repository.NewAssistantSkillRepository(f.db))
	approvals := NewAssistantApprovals(f.repo, nil)
	approvals.SetMerging(nil, repository.NewWorkspaceRepository(f.db), nil)
	approvals.SetReleases(github)
	f.service.SetApprovals(approvals)
	// A provider may return a tool it was never offered.
	write := strings.Replace(toolCallStream("create_issue", `{"team_key":"ENG","title":"Unattended issue"}`), "call_1", "unexpected_write", 1)
	provider, requests := scriptedProvider(t, write, toolCallStream("create_release", createArgs(releaseNotes)), finalAnswer)
	key := []byte("01234567890123456789012345678901")
	encrypted, err := crypto.Encrypt("test", key)
	require.NoError(t, err)
	settings := repository.NewAISettingsRepository(f.db)
	f.service.ai = NewAISettingsService(settings, nil, nil, key, WithAIPrivateNetworkAccess(true))
	require.NoError(t, settings.Upsert(f.ctx, &domain.AISettings{WorkspaceID: f.ws, Provider: "openai_compatible", BaseURL: provider.URL, Model: "fixture", APIKeyEncrypted: &encrypted}))

	a := f.attempt(t, "Publish v1.3.0 when my team exists")
	spec := jobSpec{Kind: "condition", Action: "continue", Instructions: "Propose release v1.3.0", IntervalSeconds: 60, Deadline: time.Now().Add(time.Hour).Format(time.RFC3339), Condition: &jobCondition{Tool: "list_teams", Arguments: json.RawMessage(`{}`), Path: "teams.0.key", Op: "eq", Value: "ENG"}}
	raw, err := json.Marshal(spec)
	require.NoError(t, err)
	call := chatToolCall{ID: "register", Type: "function"}
	call.Function.Name, call.Function.Arguments = "schedule_job", string(raw)
	changes := 0
	result, err := f.service.runToolCall(f.ctx, a, 0, call, &changes)
	require.NoError(t, err)
	require.Contains(t, result, "Registered")
	// A condition may match while the registration reply still owns the conversation.
	busy, err := f.repo.ClaimScheduledJob(f.ctx, time.Minute)
	require.NoError(t, err)
	require.NotNil(t, busy)
	require.NoError(t, f.service.deliverJob(f.ctx, *busy))
	require.Empty(t, events.events)
	_, err = f.db.Exec(`UPDATE teams SET key='OPS' WHERE workspace_id=$1`, f.ws)
	require.NoError(t, err)
	_, err = f.db.Exec(`UPDATE conversation_jobs SET next_run_at=now() WHERE id=$1`, busy.ID)
	require.NoError(t, err)
	require.NoError(t, f.repo.Finish(f.ctx, a, "completed"))
	j, err := f.repo.ClaimScheduledJob(f.ctx, time.Minute)
	require.NoError(t, err)
	require.NotNil(t, j)
	require.NoError(t, f.service.deliverJob(f.ctx, *j))
	require.ErrorIs(t, f.service.deliverJob(f.ctx, *j), repository.ErrConversationConflict)
	require.Len(t, events.events, 1)
	require.Equal(t, "true", events.events[0].payload["live_only"])
	queued, err := f.repo.Claim(f.ctx)
	require.NoError(t, err)
	require.NotNil(t, queued)
	require.Equal(t, a.Conversation.ID, queued.Conversation.ID)
	require.Equal(t, &j.ID, queued.Message.ScheduledJobID)
	f.service.execute(f.ctx, *queued)
	h, err := f.repo.History(f.ctx, f.ws, a.Conversation.ID, 0)
	require.NoError(t, err)
	require.Len(t, h.Jobs, 1)
	require.Equal(t, "completed", h.Jobs[0].State)
	require.Equal(t, "Approval pending", h.Jobs[0].LastOutcome)
	require.Len(t, h.Messages, 4)
	require.Equal(t, spec.Instructions, h.Messages[2].Content)
	require.Equal(t, "completed", h.Messages[3].Status)
	require.Len(t, *requests, 3)
	messages := (*requests)[1]["messages"].([]any)
	require.Contains(t, messages[len(messages)-1].(map[string]any)["content"], "a scheduled turn may only read or request approval")
	var issueCount int
	require.NoError(t, f.db.Get(&issueCount, `SELECT count(*) FROM issues WHERE workspace_id=$1 AND title='Unattended issue'`, f.ws))
	require.Zero(t, issueCount)
	require.EqualValues(t, 2048, (*requests)[0]["max_tokens"])
	require.Len(t, h.Approvals, 1)
	require.Equal(t, "create_release", h.Approvals[0].Action)
	require.Equal(t, "pending", h.Approvals[0].Status)
	require.Empty(t, github.created)

	recurring := *j
	recurring.Kind = "interval"
	recurring.Spec = json.RawMessage(`{"kind":"interval","interval_seconds":60}`)
	recurring.FireCount = maxJobContinuations - 2
	require.Equal(t, "scheduled", f.service.jobOutcome(f.ctx, recurring).State)
	recurring.FireCount++
	require.Equal(t, "completed", f.service.jobOutcome(f.ctx, recurring).State)

	// Removing the originating job must never leave a retryable reply without its restrictions.
	_, err = f.db.Exec(`DELETE FROM conversation_jobs WHERE id=$1`, j.ID)
	require.NoError(t, err)
	var replyExists bool
	require.NoError(t, f.db.Get(&replyExists, `SELECT EXISTS (SELECT 1 FROM conversation_messages WHERE id=$1)`, queued.Message.ID))
	require.False(t, replyExists)
}

func TestScheduledJobNotifyExpiryAndRevocation(t *testing.T) {
	for _, scenario := range []string{"notify", "expire", "revoke"} {
		t.Run(scenario, func(t *testing.T) {
			f := newToolFixture(t)
			events := &userEventsFake{}
			notifications := &notificationsFake{}
			f.service.SetScheduledJobs(notifications, events)
			a := f.attempt(t, "Remind me in a minute")
			result, _, err := f.service.scheduledJobTool(f.ctx, a, uuid.New(), "schedule_job", `{"kind":"once","delay_seconds":60,"action":"notify","instructions":"Review CI"}`)
			require.NoError(t, err)
			job := result.(*domain.ConversationJob)
			// A linked account can be looking at a different chat when the job fires.
			_, err = f.db.Exec(`INSERT INTO telegram_settings(workspace_id,bot_id,token_encrypted,webhook_secret,enabled,allowed_user_ids) VALUES($1,(random()*1e12)::bigint+1,'test','test',true,'{12345}')`, f.ws)
			require.NoError(t, err)
			_, err = f.db.Exec(`INSERT INTO telegram_links(workspace_id,user_id,telegram_user_id) VALUES($1,$2,12345)`, f.ws, f.user)
			require.NoError(t, err)
			_, err = f.db.Exec(`UPDATE conversation_jobs SET next_run_at=now() WHERE id=$1`, job.ID)
			require.NoError(t, err)
			if scenario == "expire" {
				_, err = f.db.Exec(`UPDATE conversation_jobs SET deadline=now() WHERE id=$1`, job.ID)
				require.NoError(t, err)
			}
			if scenario == "revoke" {
				setRole(t, f, "guest")
			}
			j, err := f.repo.ClaimScheduledJob(f.ctx, time.Minute)
			require.NoError(t, err)
			require.NotNil(t, j)
			require.NoError(t, f.service.deliverJob(f.ctx, *j))
			var count int
			require.NoError(t, f.db.Get(&count, `SELECT count(*) FROM conversation_messages WHERE conversation_id=$1`, a.Conversation.ID))
			if scenario == "revoke" {
				require.Empty(t, events.events)
				require.Equal(t, 2, count)
			} else {
				require.Len(t, events.events, 1)
				require.Empty(t, notifications.created)
				require.Equal(t, 3, count)
				require.NoError(t, f.db.Get(&count, `SELECT count(*) FROM telegram_deliveries WHERE conversation_id=$1 AND message_id IS NOT NULL`, a.Conversation.ID))
				require.Equal(t, 1, count)
				require.ErrorIs(t, f.service.deliverJob(f.ctx, *j), repository.ErrConversationConflict)
				require.Len(t, events.events, 1)
			}
		})
	}
}

func TestScheduledJobProbeErrorsBackOff(t *testing.T) {
	f := newWatchFixture()
	now := time.Now()
	s := ConversationService{tools: f.tools, now: func() time.Time { return now }}
	spec := jobSpec{Kind: "condition", IntervalSeconds: 60, Condition: &jobCondition{
		Tool: "get_agent_work", Arguments: json.RawMessage(`{"identifier":"MISSING"}`), Path: "tasks.0.state", Op: "eq", Value: "completed",
	}}
	raw, err := json.Marshal(spec)
	require.NoError(t, err)
	job := domain.ConversationJob{WorkspaceID: f.ws, UserID: f.creator, Kind: "condition", Action: "notify", Spec: raw, Deadline: now.Add(time.Hour)}
	for _, tc := range []struct{ attempt, minutes int }{{1, 1}, {2, 2}, {10, 60}} {
		job.Attempts = tc.attempt
		out := s.jobOutcome(t.Context(), job)
		require.False(t, out.Fired)
		require.Equal(t, now.Add(time.Duration(tc.minutes)*time.Minute), out.Next)
		require.NotEmpty(t, out.Error)
	}
	s.now = func() time.Time { return job.Deadline }
	require.Equal(t, "expired", s.jobOutcome(t.Context(), job).State)
}

func TestScheduledJobWriteProbeRequestsApproval(t *testing.T) {
	f := newToolFixture(t)
	setRole(t, f, "admin")
	github := newReleaseFake()
	f.service.tools.SetReleases(github, releaseRepos{}, repository.NewAssistantSkillRepository(f.db))
	f.service.SetScheduledJobs(nil, nil)
	a := f.attempt(t, "Check a release")
	spec := jobSpec{Kind: "condition", Action: "notify", Instructions: "Report success", IntervalSeconds: 60, Deadline: time.Now().Add(time.Hour).Format(time.RFC3339),
		Condition: &jobCondition{Tool: "create_release", Arguments: json.RawMessage(createArgs(releaseNotes)), Path: "state", Op: "eq", Value: "done"}}
	raw, err := json.Marshal(spec)
	require.NoError(t, err)
	call := chatToolCall{ID: "schedule", Type: "function"}
	call.Function.Name, call.Function.Arguments = "schedule_job", string(raw)
	changes := 0
	output, err := f.service.runToolCall(f.ctx, a, 0, call, &changes)
	require.NoError(t, err)
	require.Contains(t, output, "pending_user_approval")
	h, err := f.repo.History(f.ctx, f.ws, a.Conversation.ID, 0)
	require.NoError(t, err)
	require.Empty(t, h.Jobs)
	require.Len(t, h.Approvals, 1)
	require.Equal(t, "create_release", h.Approvals[0].Action)
	require.Empty(t, github.created)
}

func TestScheduledJobQuotasAndAccess(t *testing.T) {
	f := newToolFixture(t)
	f.service.SetScheduledJobs(nil, nil)
	a := f.attempt(t, "Remind me")
	var job *domain.ConversationJob
	for range 10 {
		result, _, err := f.service.scheduledJobTool(f.ctx, a, uuid.New(), "schedule_job", `{"kind":"once","delay_seconds":120,"action":"notify","instructions":"Reminder"}`)
		require.NoError(t, err)
		job = result.(*domain.ConversationJob)
	}
	_, _, err := f.service.scheduledJobTool(f.ctx, a, uuid.New(), "schedule_job", `{"kind":"once","delay_seconds":120,"action":"notify","instructions":"Overflow"}`)
	require.ErrorContains(t, err, "active job limit")
	other := domain.WithAccess(f.ctx, domain.Access{WorkspaceID: f.ws, UserID: uuid.New()})
	_, err = f.service.CancelScheduledJob(other, f.ws, a.Conversation.ID, job.ID)
	require.ErrorIs(t, err, repository.ErrConversationNotFound)
	_, err = f.service.CancelScheduledJob(f.ctx, f.ws, a.Conversation.ID, job.ID)
	require.NoError(t, err)
	_, _, err = f.service.scheduledJobTool(f.ctx, a, uuid.New(), "schedule_job", `{"kind":"interval","interval_seconds":3600,"action":"notify","instructions":"Hourly reminder"}`)
	require.NoError(t, err)
}

func TestScheduledJobPollsSameCustomRunUntilResult(t *testing.T) {
	f := newToolFixture(t)
	tools := withCustomTools(t, f)
	addCustomTool(t, f, tools, "disk_usage", "active", domain.AssistantToolRunAuto)
	machine := ownMachine(t, f)
	a := f.attempt(t, "Tell me when disk usage is high")
	f.service.SetScheduledJobs(nil, nil)
	c := jobCondition{Tool: "custom_disk_usage", Arguments: json.RawMessage(`{"machine_id":"` + machine.String() + `","inputs":{"spec":"disk"}}`), Path: "output_tail", Op: "gt", Value: 90.0}
	j := domain.ConversationJob{WorkspaceID: f.ws, UserID: f.user, ConversationID: a.Conversation.ID}
	matched, run, err := f.service.probeJob(f.ctx, j, c)
	require.NoError(t, err)
	require.False(t, matched)
	require.NotNil(t, run)
	j.ProbeRunID = run
	matched, again, err := f.service.probeJob(f.ctx, j, c)
	require.NoError(t, err)
	require.False(t, matched)
	require.Equal(t, run, again)
	_, err = f.db.Exec(`UPDATE assistant_tool_runs SET status='succeeded',exit_code=0,output='93',started_at=now(),finished_at=now() WHERE id=$1`, run)
	require.NoError(t, err)
	matched, _, err = f.service.probeJob(f.ctx, j, c)
	require.NoError(t, err)
	require.True(t, matched)
}
