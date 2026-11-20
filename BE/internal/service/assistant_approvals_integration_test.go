package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
	gh "github.com/kuayle/kuayle-backend/pkg/github"
	"github.com/stretchr/testify/require"
)

// approvalTaskFake keeps Dev Machine policy out of the test but uses the real
// task repository, so task identity, authorization and replay are genuine.
type approvalTaskFake struct {
	repo       *repository.AgentTaskRepository
	providers  []string // the coding agents offered; codex when empty
	optionsErr error
	createErr  error
	mu         sync.Mutex
	creates    int
	requests   []domain.AgentTaskRequest // every Create call, in order
}

func (f *approvalTaskFake) Options(context.Context, uuid.UUID, uuid.UUID) (*AgentTaskOptions, error) {
	if f.optionsErr != nil {
		return nil, f.optionsErr
	}
	providers := []dto.AgentProviderResponse{{ID: "codex"}}
	if len(f.providers) > 0 {
		providers = nil
		for _, id := range f.providers {
			providers = append(providers, dto.AgentProviderResponse{ID: id})
		}
	}
	return &AgentTaskOptions{Providers: providers, ModelServers: []dto.ModelServerResponse{{IsDefault: true, Harnesses: []string{"codex", "claude-code"}, DefaultModel: "deepseek-flash", Thinking: "high"}}}, nil
}
func (f *approvalTaskFake) Create(ctx context.Context, ws, conversation uuid.UUID, input domain.AgentTaskRequest) (*domain.AgentTask, error) {
	f.mu.Lock()
	f.creates++
	f.requests = append(f.requests, input)
	err := f.createErr
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if accepted, err := f.repo.ReplayCreate(ctx, ws, conversation, input); err != nil || accepted != nil {
		return accepted, err
	}
	return f.repo.Create(ctx, ws, conversation, input)
}

// requestDelegation drives a real assistant reply that asks to delegate ENG-1.
func requestDelegation(t *testing.T, f toolFixture) (domain.ConversationAttempt, domain.ConversationApproval, *approvalTaskFake) {
	t.Helper()
	var team uuid.UUID
	require.NoError(t, f.db.Get(&team, `SELECT id FROM teams WHERE workspace_id=$1`, f.ws))
	_, err := f.db.Exec(`INSERT INTO issues(workspace_id,team_id,number,identifier_text,title,creator_id,status_id) VALUES($1,$2,1,'ENG-1','Dark mode',$3,(SELECT id FROM team_statuses WHERE team_id=$2 LIMIT 1))`, f.ws, team, f.user)
	require.NoError(t, err)
	tasks := &approvalTaskFake{repo: repository.NewAgentTaskRepository(f.db)}
	f.service.SetApprovals(NewAssistantApprovals(f.repo, tasks))
	a := f.attempt(t, "Implement dark mode on ENG-1")
	provider, requests := scriptedProvider(t, toolCallStream("delegate_issue", `{"identifier":"eng-1","instructions":"Implement dark mode and add tests"}`), finalAnswer)
	require.NoError(t, f.service.converse(f.ctx, a, provider.URL, "key", []chatCompletionMessage{{Role: "user", Content: "Delegate"}}, func(string) error { return nil }))
	history := (*requests)[1]["messages"].([]any)
	require.Contains(t, history[len(history)-1].(map[string]any)["content"], "pending_user_approval")
	var count int
	require.NoError(t, f.db.Get(&count, `SELECT count(*) FROM agent_tasks WHERE workspace_id=$1`, f.ws))
	require.Zero(t, count, "requesting delegation starts nothing")
	h, err := f.repo.History(f.ctx, f.ws, a.Conversation.ID, 0)
	require.NoError(t, err)
	require.Len(t, h.Approvals, 1)
	approval := h.Approvals[0]
	require.Equal(t, "pending", approval.Status)
	require.Equal(t, f.user, approval.ActorID)
	var payload domain.DelegationPayload
	require.NoError(t, json.Unmarshal(approval.Payload, &payload))
	require.Equal(t, domain.DelegationPayload{Identifier: "ENG-1", Title: "Dark mode", Instructions: "Implement dark mode and add tests"}, payload)
	return a, approval, tasks
}

func TestApprovedDelegationStartsExactlyOneTask(t *testing.T) {
	f := newToolFixture(t)
	a, approval, tasks := requestDelegation(t, f)
	var wg sync.WaitGroup
	results := make([]*domain.ConversationApproval, 4)
	errs := make([]error, 4)
	for i := range results {
		wg.Go(func() {
			results[i], errs[i] = f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, approval.ID, true)
		})
	}
	wg.Wait()
	for i := range results {
		require.NoError(t, errs[i])
		require.NotNil(t, results[i].TaskID)
		require.Equal(t, *results[0].TaskID, *results[i].TaskID, "double clicks and two clients converge on one task")
	}
	var tasksCreated int
	require.NoError(t, f.db.Get(&tasksCreated, `SELECT count(*) FROM agent_tasks WHERE workspace_id=$1`, f.ws))
	require.Equal(t, 1, tasksCreated)
	var task struct {
		Prompt   string `db:"prompt"`
		Provider string `db:"provider"`
		Model    string `db:"model"`
		Issue    string `db:"identifier_text"`
	}
	require.NoError(t, f.db.Get(&task, `SELECT t.prompt,t.provider,t.model,i.identifier_text FROM agent_tasks t JOIN issues i ON i.id=t.issue_id WHERE t.id=$1`, *results[0].TaskID))
	require.Equal(t, "Implement dark mode and add tests", task.Prompt, "the approved instructions, not later model text, are executed")
	require.Equal(t, "codex", task.Provider)
	require.Equal(t, "deepseek-flash", task.Model)
	require.Equal(t, "ENG-1", task.Issue)
	_, err := f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, approval.ID, false)
	require.ErrorIs(t, err, repository.ErrConversationConflict, "an approved decision cannot be denied")
	var events []string
	require.NoError(t, f.db.Select(&events, `SELECT type FROM conversation_events WHERE conversation_id=$1 AND type LIKE 'approval.%' ORDER BY cursor`, a.Conversation.ID))
	require.Equal(t, []string{"approval.requested", "approval.updated", "approval.resolved"}, events)
	require.LessOrEqual(t, tasks.creates, 4)
}

func TestApprovalDenialExpiryAndActorBinding(t *testing.T) {
	t.Run("deny_is_final", func(t *testing.T) {
		f := newToolFixture(t)
		a, approval, tasks := requestDelegation(t, f)
		denied, err := f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, approval.ID, false)
		require.NoError(t, err)
		require.Equal(t, "denied", denied.Status)
		_, err = f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, approval.ID, true)
		require.ErrorIs(t, err, repository.ErrConversationConflict)
		again, err := f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, approval.ID, false)
		require.NoError(t, err)
		require.Equal(t, "denied", again.Status)
		require.Zero(t, tasks.creates)
	})
	t.Run("expired", func(t *testing.T) {
		f := newToolFixture(t)
		a, approval, tasks := requestDelegation(t, f)
		_, err := f.db.Exec(`UPDATE conversation_approvals SET expires_at=now()-interval '1 second' WHERE id=$1`, approval.ID)
		require.NoError(t, err)
		_, err = f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, approval.ID, true)
		require.ErrorIs(t, err, repository.ErrApprovalExpired)
		_, err = f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, approval.ID, true)
		require.ErrorIs(t, err, repository.ErrApprovalExpired)
		require.Zero(t, tasks.creates)
	})
	t.Run("other_member_cannot_resolve", func(t *testing.T) {
		f := newToolFixture(t)
		a, approval, tasks := requestDelegation(t, f)
		other := uuid.New()
		_, err := f.db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Other','hash')`, other, other.String()+"@test.invalid")
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = f.db.Exec(`DELETE FROM users WHERE id=$1`, other) })
		_, err = f.db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'admin')`, f.ws, other)
		require.NoError(t, err)
		otherCtx := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: f.ws, UserID: other})
		_, err = f.service.ResolveApproval(otherCtx, f.ws, a.Conversation.ID, approval.ID, true)
		require.ErrorIs(t, err, repository.ErrConversationNotFound)
		_, err = f.service.ResolveApproval(f.ctx, f.ws, uuid.New(), approval.ID, true)
		require.ErrorIs(t, err, repository.ErrConversationNotFound, "an approval is bound to its conversation")
		require.Zero(t, tasks.creates)
	})
	t.Run("permanent_and_transient_failures", func(t *testing.T) {
		f := newToolFixture(t)
		a, approval, tasks := requestDelegation(t, f)
		tasks.createErr = errors.New("database briefly unavailable")
		_, err := f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, approval.ID, true)
		require.Error(t, err)
		var status string
		require.NoError(t, f.db.Get(&status, `SELECT status FROM conversation_approvals WHERE id=$1`, approval.ID))
		require.Equal(t, "approving", status, "a transient failure can be resumed")
		tasks.createErr = ErrDevMachinesDisabled
		failed, err := f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, approval.ID, true)
		require.NoError(t, err)
		require.Equal(t, "failed", failed.Status)
		require.Equal(t, "Coding agents are not enabled for this workspace", *failed.Error)
		_, err = f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, approval.ID, true)
		require.ErrorIs(t, err, repository.ErrConversationConflict)
	})
}

type mergeWorkFake struct {
	target *repository.MergeTarget
	recent []repository.AgentWorkItem
	asked  []string
}

func (f *mergeWorkFake) MergedPullRequest(context.Context, uuid.UUID, uuid.UUID) (*domain.MergedPullRequest, error) {
	return nil, nil
}
func (f *mergeWorkFake) RecentAgentWork(_ context.Context, _ uuid.UUID, state string, limit int) ([]repository.AgentWorkItem, error) {
	f.asked = append(f.asked, fmt.Sprintf("%s/%d", state, limit))
	return f.recent, nil
}

func (f *mergeWorkFake) IssueAgentWork(context.Context, uuid.UUID, uuid.UUID) ([]repository.IssueAgentWork, error) {
	return nil, nil
}
func (f *mergeWorkFake) MergeTarget(_ context.Context, _ uuid.UUID, _, _ *uuid.UUID) (*repository.MergeTarget, error) {
	if f.target == nil {
		return nil, repository.ErrNoMergeTarget
	}
	copy := *f.target
	return &copy, nil
}

func (f *mergeWorkFake) FollowUpTarget(context.Context, uuid.UUID, uuid.UUID) (*repository.FollowUpTarget, error) {
	return nil, repository.ErrNoMergeTarget
}

type mergerFake struct {
	err    error
	merged []string
}

func (f *mergerFake) MergeTaskPullRequest(_ context.Context, _ uuid.UUID, repo string, number int, sha, _ string) error {
	if f.err != nil {
		return f.err
	}
	f.merged = append(f.merged, fmt.Sprintf("%s#%d@%s", repo, number, sha))
	return nil
}

// A merge approval binds the reviewed commit; the merge happens only after the
// user approves, and only while the task still awaits review at that commit.
func TestMergeApprovalMergesOnlyTheReviewedCommitAfterApproval(t *testing.T) {
	f := newToolFixture(t)
	a, delegation, _ := requestDelegation(t, f)
	started, err := f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, delegation.ID, true)
	require.NoError(t, err)
	work := &mergeWorkFake{target: &repository.MergeTarget{TaskID: *started.TaskID, IssueID: delegation.IssueID, CreatorID: f.user, Repository: "acme/app", Number: 12, URL: "https://github.com/acme/app/pull/12", Commit: "abc"}}
	merger := &mergerFake{}
	f.service.tools.SetAgentWork(work)
	f.service.approvals.SetMerging(work, repository.NewWorkspaceRepository(f.db), merger)
	request := func() (domain.ConversationAttempt, domain.ConversationApproval) {
		t.Helper()
		a := f.attempt(t, "Merge ENG-1")
		provider, _ := scriptedProvider(t, toolCallStream("merge_pull_request", `{"identifier":"eng-1"}`), finalAnswer)
		require.NoError(t, f.service.converse(f.ctx, a, provider.URL, "key", []chatCompletionMessage{{Role: "user", Content: "Merge"}}, func(string) error { return nil }))
		h, err := f.repo.History(f.ctx, f.ws, a.Conversation.ID, 0)
		require.NoError(t, err)
		require.Len(t, h.Approvals, 1)
		return a, h.Approvals[0]
	}
	resolve := func(a domain.ConversationAttempt, approval domain.ConversationApproval) *domain.ConversationApproval {
		t.Helper()
		result, err := f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, approval.ID, true)
		require.NoError(t, err)
		return result
	}

	a, approval := request()
	require.Equal(t, "merge_pull_request", approval.Action)
	var payload domain.MergePayload
	require.NoError(t, json.Unmarshal(approval.Payload, &payload))
	require.Equal(t, domain.MergePayload{Identifier: "ENG-1", Title: "Dark mode", TaskID: *started.TaskID, Repository: "acme/app", PullRequest: 12, PullRequestURL: "https://github.com/acme/app/pull/12", Commit: "abc"}, payload)
	require.Empty(t, merger.merged, "requesting a merge merges nothing")
	work.target.Commit = "def"
	result := resolve(a, approval)
	require.Equal(t, "failed", result.Status)
	require.Contains(t, *result.Error, "changed after approval")
	require.Empty(t, merger.merged, "a push after the request invalidates it")

	a, approval = request()
	work.target.CreatorID = uuid.New()
	result = resolve(a, approval)
	require.Equal(t, "failed", result.Status)
	require.Contains(t, *result.Error, "admins or the task's creator")
	work.target.CreatorID = f.user

	a, approval = request()
	merger.err = &gh.ErrMergeRefused{Reason: "Required status check is failing"}
	result = resolve(a, approval)
	require.Equal(t, "failed", result.Status)
	require.Contains(t, *result.Error, "Required status check is failing")
	merger.err = nil

	a, approval = request()
	merger.err = errors.New("connection reset")
	_, err = f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, approval.ID, true)
	require.Error(t, err, "a transient failure can be retried")
	merger.err = nil
	result = resolve(a, approval)
	require.Equal(t, "approved", result.Status)
	require.Equal(t, *started.TaskID, *result.TaskID)
	require.Equal(t, []string{"acme/app#12@def"}, merger.merged)

	work.target = nil
	_, _, err = f.service.tools.prepareMerge(f.ctx, f.ws, f.user, `{"identifier":"ENG-1"}`)
	require.ErrorContains(t, err, "no pull request of this issue is awaiting review")
}

// KUA-207 (incident on PR #276): approving the merge of a branch one commit
// behind its base brings it up to date and leaves the approval waiting, not
// failed. The poller merges the updated head once its checks pass, as the
// approver, and tells them.
func TestMergeApprovalWaitsForTheUpdatedBranchAndMergesIt(t *testing.T) {
	f := newToolFixture(t)
	a, delegation, _ := requestDelegation(t, f)
	started, err := f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, delegation.ID, true)
	require.NoError(t, err)
	work := &mergeWorkFake{target: &repository.MergeTarget{TaskID: *started.TaskID, IssueID: delegation.IssueID, CreatorID: f.user, Repository: "acme/app", Number: 276, URL: "https://github.com/acme/app/pull/276", Commit: "abc"}}
	merger := &mergerFake{}
	pulls := &prGitHub{method: "squash", status: PullRequestStatus{State: "open", HeadSHA: "abc", BaseRef: "main", MergeBase: "base1", Behind: 1, MergeableState: "behind"}, checksHead: "abc"}
	verifiedUpdate(pulls, "abc", "def", "base2")
	update := pulls.onUpdate
	pulls.onUpdate = func() {
		update()
		pulls.checks = []PullRequestCheck{{Name: "Tests", State: "pending"}}
	}
	notifications, events := &notificationsFake{}, &userEventsFake{}
	f.service.tools.SetAgentWork(work)
	f.service.approvals.SetMerging(work, repository.NewWorkspaceRepository(f.db), merger)
	f.service.approvals.SetPullRequests(pulls, &prTargets{})
	f.service.approvals.SetNotifications(notifications, events)
	b := f.attempt(t, "Merge it")
	provider, _ := scriptedProvider(t, toolCallStream("merge_pull_request", `{"identifier":"eng-1"}`), finalAnswer)
	require.NoError(t, f.service.converse(f.ctx, b, provider.URL, "key", []chatCompletionMessage{{Role: "user", Content: "Merge it"}}, func(string) error { return nil }))
	h, err := f.repo.History(f.ctx, f.ws, b.Conversation.ID, 0)
	require.NoError(t, err)
	require.Len(t, h.Approvals, 1)
	approval := h.Approvals[0]

	waiting, err := f.service.ResolveApproval(f.ctx, f.ws, b.Conversation.ID, approval.ID, true)
	require.NoError(t, err)
	require.Equal(t, domain.ApprovalWaitingChecks, waiting.Status, "the approved merge waits, it does not fail")
	require.Nil(t, waiting.Error)
	require.Nil(t, waiting.ResolvedAt)
	require.NotNil(t, waiting.NextCheckAt)
	var payload domain.MergePayload
	require.NoError(t, json.Unmarshal(waiting.Payload, &payload))
	require.Equal(t, "abc", payload.Wait.Approved)
	require.Equal(t, "def", payload.Wait.Head)
	require.Equal(t, 1, payload.Wait.Updates)
	require.Contains(t, payload.Wait.Waiting, "merges it once the checks on the new head pass")
	require.Empty(t, merger.merged)

	again, err := f.service.ResolveApproval(f.ctx, f.ws, b.Conversation.ID, approval.ID, true)
	require.NoError(t, err)
	require.Equal(t, domain.ApprovalWaitingChecks, again.Status, "approving again changes nothing")
	_, err = f.service.ResolveApproval(f.ctx, f.ws, b.Conversation.ID, approval.ID, false)
	require.ErrorIs(t, err, repository.ErrConversationConflict, "an approved merge cannot be denied")

	due := func() {
		t.Helper()
		_, err := f.db.Exec(`UPDATE conversation_approvals SET next_check_at=now()-interval '1 second' WHERE id=$1`, approval.ID)
		require.NoError(t, err)
	}
	ctx := context.Background()
	require.Zero(t, f.service.approvals.ContinueMergeWaits(ctx), "a wait is read again only when due")
	due()
	require.Equal(t, 1, f.service.approvals.ContinueMergeWaits(ctx))
	require.Zero(t, f.service.approvals.ContinueMergeWaits(ctx), "the next check is a minute away, never at once")
	state, data, err := f.repo.ApprovalState(ctx, approval.ID)
	require.NoError(t, err)
	require.Equal(t, domain.ApprovalWaitingChecks, state)
	require.NoError(t, json.Unmarshal(data, &payload))
	require.Contains(t, payload.Wait.Waiting, "still running on def")
	require.Empty(t, merger.merged)

	pulls.checks = passingChecks
	due()
	require.Equal(t, 1, f.service.approvals.ContinueMergeWaits(ctx))
	require.Equal(t, []string{"acme/app#276@def"}, merger.merged, "the verified update is what merges")
	h, err = f.repo.History(f.ctx, f.ws, b.Conversation.ID, 0)
	require.NoError(t, err)
	require.Equal(t, "approved", h.Approvals[0].Status)
	require.Equal(t, *started.TaskID, *h.Approvals[0].TaskID)
	require.Nil(t, h.Approvals[0].NextCheckAt)
	require.Len(t, notifications.created, 2, "the request, then the result")
	finished := notifications.created[1]
	require.Equal(t, notificationApprovalFinished, finished.kind)
	require.Equal(t, "Merged pull request #276 of ENG-1", finished.title)
	require.Equal(t, approval.ConversationID.String(), events.events[1].payload["conversation_id"])
	require.Equal(t, "approved", events.events[1].payload["status"])
	var resolved int
	require.NoError(t, f.db.Get(&resolved, `SELECT count(*) FROM conversation_events WHERE conversation_id=$1 AND type='approval.resolved'`, b.Conversation.ID))
	require.Equal(t, 1, resolved, "the card in the conversation shows the result")
	require.Zero(t, f.service.approvals.ContinueMergeWaits(ctx))
}

type acceptorFake struct {
	err      error
	accepted []string
}

func (f *acceptorFake) AcceptAsApprover(_ context.Context, _ uuid.UUID, id uuid.UUID, generation int64) (*domain.AgentTask, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.accepted = append(f.accepted, fmt.Sprintf("%s@%d", id, generation))
	return &domain.AgentTask{ID: id, State: "completed", Generation: generation}, nil
}

// Accepting work through the assistant binds the reviewed attempt and pull
// request; nothing completes until the user approves, and nothing merges.
func TestAcceptApprovalCompletesTheReviewedAttemptWithoutMerging(t *testing.T) {
	f := newToolFixture(t)
	a, delegation, _ := requestDelegation(t, f)
	started, err := f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, delegation.ID, true)
	require.NoError(t, err)
	work := &mergeWorkFake{target: &repository.MergeTarget{TaskID: *started.TaskID, IssueID: delegation.IssueID, CreatorID: f.user, Generation: 3, Repository: "acme/app", Number: 12, URL: "https://github.com/acme/app/pull/12", Commit: "abc"}}
	merger, acceptor := &mergerFake{}, &acceptorFake{}
	f.service.tools.SetAgentWork(work)
	f.service.approvals.SetMerging(work, repository.NewWorkspaceRepository(f.db), merger)
	f.service.approvals.SetAccepting(acceptor)
	request := func() (domain.ConversationAttempt, domain.ConversationApproval) {
		t.Helper()
		a := f.attempt(t, "Accept ENG-1")
		provider, _ := scriptedProvider(t, toolCallStream("accept_agent_work", `{"identifier":"eng-1"}`), finalAnswer)
		require.NoError(t, f.service.converse(f.ctx, a, provider.URL, "key", []chatCompletionMessage{{Role: "user", Content: "Accept it without merging"}}, func(string) error { return nil }))
		h, err := f.repo.History(f.ctx, f.ws, a.Conversation.ID, 0)
		require.NoError(t, err)
		require.Len(t, h.Approvals, 1)
		return a, h.Approvals[0]
	}
	resolve := func(a domain.ConversationAttempt, approval domain.ConversationApproval) *domain.ConversationApproval {
		t.Helper()
		result, err := f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, approval.ID, true)
		require.NoError(t, err)
		return result
	}

	a, approval := request()
	require.Equal(t, "accept_agent_work", approval.Action)
	var payload domain.AcceptPayload
	require.NoError(t, json.Unmarshal(approval.Payload, &payload))
	require.Equal(t, domain.AcceptPayload{Identifier: "ENG-1", Title: "Dark mode", TaskID: *started.TaskID, Generation: 3, PullRequest: 12, PullRequestURL: "https://github.com/acme/app/pull/12", Commit: "abc"}, payload)
	require.Empty(t, acceptor.accepted, "requesting acceptance accepts nothing")

	acceptor.err = repository.ErrAgentTaskConflict
	result := resolve(a, approval)
	require.Equal(t, "failed", result.Status)
	require.Contains(t, *result.Error, "changed after approval")

	a, approval = request()
	acceptor.err = repository.ErrConversationNotFound
	result = resolve(a, approval)
	require.Equal(t, "failed", result.Status)
	require.Contains(t, *result.Error, "admins or the task's creator")

	a, approval = request()
	acceptor.err = errors.New("connection reset")
	_, err = f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, approval.ID, true)
	require.Error(t, err, "a transient failure can be retried")
	acceptor.err = nil
	result = resolve(a, approval)
	require.Equal(t, "approved", result.Status)
	require.Equal(t, *started.TaskID, *result.TaskID)
	require.Equal(t, []string{fmt.Sprintf("%s@3", *started.TaskID)}, acceptor.accepted)
	require.Empty(t, merger.merged, "acceptance never merges")

	work.target.CreatorID = uuid.New()
	_, _, err = f.service.tools.prepareAccept(f.ctx, f.ws, f.user, `{"identifier":"ENG-1"}`)
	require.ErrorContains(t, err, "admins or the task's creator", "a member cannot accept another member's task")
	work.target = nil
	_, _, err = f.service.tools.prepareAccept(f.ctx, f.ws, f.user, `{"identifier":"ENG-1"}`)
	require.ErrorContains(t, err, "no pull request of this issue is awaiting review")
}

type headsFake struct {
	head  domain.PullRequestHead
	err   error
	reads []string
	// inspectErr fails only the detailed comparison, not the head read.
	inspectErr error
}

func (f *headsFake) InspectPullRequestHead(_ context.Context, _ uuid.UUID, _ string, _ int, published string) (*domain.PullRequestHead, error) {
	f.reads = append(f.reads, published)
	if f.inspectErr != nil {
		return nil, f.inspectErr
	}
	if f.err != nil {
		return nil, f.err
	}
	head := f.head
	return &head, nil
}

// KUA-66 / PR #108: a maintainer merged main into the pull request branch, so
// the head is no longer the commit Kuayle published. The approval shows the new
// commit and what changed, merges exactly that commit, and refuses it again if
// the head moves once more before or during the merge.
func TestMergeApprovalShowsAndMergesTheHeadOthersPushed(t *testing.T) {
	f := newToolFixture(t)
	a, delegation, _ := requestDelegation(t, f)
	started, err := f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, delegation.ID, true)
	require.NoError(t, err)
	work := &mergeWorkFake{target: &repository.MergeTarget{TaskID: *started.TaskID, IssueID: delegation.IssueID, CreatorID: f.user, Repository: "acme/app", Number: 108, URL: "https://github.com/acme/app/pull/108", Commit: "15af2c3a"}}
	change := &domain.PullRequestHeadChange{Kind: "merge", Commits: []domain.PullRequestHeadCommit{{SHA: "3220dbc9", Author: "maintainer", Subject: "Merge branch 'main'", Merge: true}}}
	heads := &headsFake{head: domain.PullRequestHead{SHA: "3220dbc9", Change: change}}
	merger := &mergerFake{}
	f.service.tools.SetAgentWork(work)
	f.service.tools.SetPullRequestHeads(heads)
	f.service.approvals.SetMerging(work, repository.NewWorkspaceRepository(f.db), merger)
	f.service.approvals.SetPullRequestHeads(heads)
	request := func() (domain.ConversationAttempt, domain.ConversationApproval) {
		t.Helper()
		a := f.attempt(t, "Merge ENG-1")
		provider, _ := scriptedProvider(t, toolCallStream("merge_pull_request", `{"identifier":"eng-1"}`), finalAnswer)
		require.NoError(t, f.service.converse(f.ctx, a, provider.URL, "key", []chatCompletionMessage{{Role: "user", Content: "Merge"}}, func(string) error { return nil }))
		h, err := f.repo.History(f.ctx, f.ws, a.Conversation.ID, 0)
		require.NoError(t, err)
		require.Len(t, h.Approvals, 1)
		return a, h.Approvals[0]
	}
	resolve := func(a domain.ConversationAttempt, approval domain.ConversationApproval) *domain.ConversationApproval {
		t.Helper()
		result, err := f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, approval.ID, true)
		require.NoError(t, err)
		return result
	}

	a, approval := request()
	var payload domain.MergePayload
	require.NoError(t, json.Unmarshal(approval.Payload, &payload))
	require.Equal(t, "3220dbc9", payload.Commit, "the card shows the commit that will be merged")
	require.Equal(t, "15af2c3a", payload.PublishedCommit)
	require.Equal(t, change, payload.HeadChange)
	require.Empty(t, merger.merged)

	// The head moved again after the card was shown: nothing is merged.
	heads.head.SHA = "9999aaaa"
	result := resolve(a, approval)
	require.Equal(t, "failed", result.Status)
	require.Contains(t, *result.Error, "head moved to 9999aaaa")
	require.Empty(t, merger.merged)

	// An outsider cannot cause a merge, and GitHub is not read for them.
	heads.head.SHA = "3220dbc9"
	a, approval = request()
	work.target.CreatorID = uuid.New()
	heads.reads = nil
	result = resolve(a, approval)
	require.Equal(t, "failed", result.Status)
	require.Contains(t, *result.Error, "admins or the task's creator")
	require.Empty(t, heads.reads)
	work.target.CreatorID = f.user

	// A push that lands between the fresh read and the merge is refused by the
	// pin with a message that says so.
	a, approval = request()
	merger.err = &gh.ErrMergeRefused{Reason: "Head branch was modified. Review and try the merge again."}
	result = resolve(a, approval)
	require.Equal(t, "failed", result.Status)
	require.Contains(t, *result.Error, "head moved after it was approved (3220dbc9)")
	merger.err = nil

	// A published commit that changed since the request invalidates it.
	a, approval = request()
	work.target.Commit = "bbbbbbbb"
	result = resolve(a, approval)
	require.Equal(t, "failed", result.Status)
	require.Contains(t, *result.Error, "changed after approval")
	work.target.Commit = "15af2c3a"

	a, approval = request()
	result = resolve(a, approval)
	require.Equal(t, "approved", result.Status)
	require.Equal(t, []string{"acme/app#108@3220dbc9"}, merger.merged, "merged at the commit shown, not the published one")

	// The comparison is only for the card: once the user approved, a failure of
	// it must not block the merge, while a failed head read must.
	a, approval = request()
	heads.inspectErr = errors.New("compare failed")
	merger.merged = nil
	result = resolve(a, approval)
	require.Equal(t, "approved", result.Status)
	require.Equal(t, []string{"acme/app#108@3220dbc9"}, merger.merged)
	heads.inspectErr = nil

	a, approval = request()
	heads.err = errors.New("GitHub not connected")
	_, err = f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, approval.ID, true)
	require.ErrorContains(t, err, "GitHub not connected")
	heads.err = nil

	heads.err = errors.New("GitHub not connected")
	_, _, err = f.service.tools.prepareMerge(f.ctx, f.ws, f.user, `{"identifier":"ENG-1"}`)
	require.ErrorContains(t, err, "could not read the pull request from GitHub")
}

func (f *headsFake) ReadPullRequestHead(context.Context, uuid.UUID, string, int) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.head.SHA, nil
}

func TestPendingApprovalRemainsAvailableOutsideTheLatestHistoryPage(t *testing.T) {
	f := newToolFixture(t)
	a, pending, _ := requestDelegation(t, f)
	_, err := f.db.Exec(`INSERT INTO conversation_messages(conversation_id,request_id,role,content,status)
		SELECT $1,gen_random_uuid(),'user','Later message','completed' FROM generate_series(1,51)`, a.Conversation.ID)
	require.NoError(t, err)
	history, err := f.repo.History(f.ctx, f.ws, a.Conversation.ID, 0)
	require.NoError(t, err)
	require.Empty(t, history.Tools)
	require.Len(t, history.Approvals, 1)
	require.Equal(t, pending.ID, history.Approvals[0].ID)
	require.Equal(t, a.Message.Model, history.Approvals[0].MessageModel)
	var streamed string
	require.NoError(t, f.db.Get(&streamed, `SELECT payload->>'message_model' FROM conversation_events WHERE conversation_id=$1 AND type='approval.requested'`, a.Conversation.ID))
	require.Equal(t, a.Message.Model, streamed)
}
