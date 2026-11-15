package service

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

// KUA-193: follow_up_task on an issue whose work was already merged tells the
// assistant to ask the user first, and needs confirm_merged to propose it.
func TestFollowUpOnMergedWorkNeedsAnExplicitConfirmation(t *testing.T) {
	f := newPRFixture()
	ctx := context.Background()
	merged := &domain.MergedPullRequest{Number: 12, URL: "https://github.com/acme/app/pull/12", Repository: "acme/app"}
	f.tools.work = prWork{followUp: &repository.FollowUpTarget{TaskID: f.task, CreatorID: f.creator, State: "failed", Generation: 2}, merged: merged}

	_, _, err := f.tools.prepareFollowUp(ctx, f.ws, f.creator, `{"identifier":"ENG-1","instructions":"Fix the flaky test"}`)
	require.ErrorContains(t, err, "This issue's work was merged in #12 (https://github.com/acme/app/pull/12). Retry anyway?")
	require.ErrorContains(t, err, "ask the user")
	require.ErrorContains(t, err, "confirm_merged")
	_, _, err = f.tools.prepareFollowUp(ctx, f.ws, f.creator, `{"identifier":"ENG-1","instructions":"Fix the flaky test","confirm_merged":false}`)
	require.ErrorContains(t, err, "merged in #12")

	_, payload, err := f.tools.prepareFollowUp(ctx, f.ws, f.creator, `{"identifier":"ENG-1","instructions":"Fix the flaky test","confirm_merged":true}`)
	require.NoError(t, err)
	require.Equal(t, merged, payload.MergedPullRequest, "the approval card shows what the user confirms")

	// Without merged work the argument changes nothing.
	f.tools.work = prWork{followUp: &repository.FollowUpTarget{TaskID: f.task, CreatorID: f.creator, State: "failed", Generation: 2}}
	for _, arguments := range []string{`{"identifier":"ENG-1","instructions":"Fix it"}`, `{"identifier":"ENG-1","instructions":"Fix it","confirm_merged":true}`} {
		_, payload, err = f.tools.prepareFollowUp(ctx, f.ws, f.creator, arguments)
		require.NoError(t, err)
		require.Nil(t, payload.MergedPullRequest, arguments)
	}
	// Refusals that do not depend on merged work come first.
	f.tools.work = prWork{followUp: &repository.FollowUpTarget{TaskID: f.task, CreatorID: f.creator, State: "running"}, merged: merged}
	_, _, err = f.tools.prepareFollowUp(ctx, f.ws, f.creator, `{"identifier":"ENG-1","instructions":"Fix it"}`)
	require.ErrorContains(t, err, "the task is running")
}

// Bringing the task's open pull request up to date through its agent is what
// the user asked for; the card names the issue's merged work all the same.
func TestBranchUpdateByTheTaskAgentShowsMergedWork(t *testing.T) {
	f := newPRFixture()
	f.github.status.Behind = 3
	merged := &domain.MergedPullRequest{Number: 9, URL: "https://github.com/acme/app/pull/9", Repository: "acme/app"}
	f.tools.work = prWork{followUp: &repository.FollowUpTarget{TaskID: f.task, CreatorID: f.creator, State: "awaiting_review", Generation: 2}, merged: merged}
	_, payload, action, err := f.tools.preparePullRequestAction(context.Background(), f.ws, f.creator, "update_pull_request_branch", `{"identifier":"ENG-1"}`)
	require.NoError(t, err)
	require.Equal(t, "follow_up_task", action)
	require.Equal(t, merged, payload.(domain.FollowUpPayload).MergedPullRequest)
}

// Review, CI and comment follow-ups continue the task's own open pull request
// on their own; they keep working without a person to confirm.
func TestAutomaticFollowUpsDoNotStopAtMergedWork(t *testing.T) {
	store := &feedbackStoreFake{finished: map[uuid.UUID]*int64{}, followUps: []domain.AgentTaskFollowUp{{
		Signal: domain.AgentTaskSignal{ID: uuid.New(), Kind: "ci", Name: "Tests", Outcome: "failure", RequestID: uuid.New()},
		Task:   domain.AgentTask{ID: uuid.New(), CreatorID: uuid.New(), Generation: 2, Prompt: "Implement dark mode"},
	}}}
	retrier := &retrierFake{}
	require.NoError(t, NewAgentTaskFeedback(store, retrier).Record(context.Background(), uuid.New(), "acme/app", 12, domain.AgentTaskSignalInput{}))
	require.Len(t, retrier.requests, 1)
	require.True(t, retrier.requests[0].ConfirmMerged)
}
