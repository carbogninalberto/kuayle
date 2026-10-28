package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

type reviewStoreFake struct {
	*taskStoreFake
	completed int
}

func (f *reviewStoreFake) CompleteReview(context.Context, domain.AgentTask) error {
	f.completed++
	f.task.State = "completed"
	return nil
}

// A finished reviewer records its findings and completes; it never enters
// validation or publication, so its work is never pushed.
func TestReviewAttemptCompletesWithoutValidationOrPublication(t *testing.T) {
	for _, state := range []string{"running", "waiting_input"} {
		store := &reviewStoreFake{taskStoreFake: &taskStoreFake{task: domain.AgentTask{ID: uuid.New(), State: state, Kind: domain.AgentTaskKindReview, Provider: "codex"}}}
		runtime := &taskRuntimeFake{run: &domain.DevMachineAgentRun{Status: domain.DevMachineAgentRunStatusSucceeded}}
		svc := &AgentTaskService{store: store, runtime: runtime}
		require.NoError(t, svc.step(context.Background(), store.task))
		require.Equal(t, 1, store.completed)
		require.NotContains(t, store.transitions, "validating")
		require.Equal(t, "completed", store.task.State)
	}
	implement := &reviewStoreFake{taskStoreFake: &taskStoreFake{task: domain.AgentTask{ID: uuid.New(), State: "running", Kind: domain.AgentTaskKindImplement}}}
	svc := &AgentTaskService{store: implement, runtime: &taskRuntimeFake{run: &domain.DevMachineAgentRun{Status: domain.DevMachineAgentRunStatusSucceeded}}}
	require.NoError(t, svc.step(context.Background(), implement.task))
	require.Zero(t, implement.completed)
	require.Equal(t, []string{"validating"}, implement.transitions)
}

func TestReviewTransitionsNeverReachPublication(t *testing.T) {
	for _, to := range []string{"validating", "publishing", "awaiting_review"} {
		require.False(t, domain.ValidAgentTaskKindTransition(domain.AgentTaskKindReview, "running", to), to)
	}
	require.True(t, domain.ValidAgentTaskKindTransition(domain.AgentTaskKindReview, "running", "completed"))
	require.False(t, domain.ValidAgentTaskKindTransition(domain.AgentTaskKindImplement, "running", "completed"), "implementations finish by merge")
	require.True(t, domain.ValidAgentTaskKindTransition(domain.AgentTaskKindReview, "running", "cancelling"))
}

func TestReviewOutputIsParsedAndBounded(t *testing.T) {
	verdict, findings := domain.ParseAgentReview("Summary first\n**Verdict:** Request changes\n\n- a.go:1 blocker: x")
	require.Equal(t, "request_changes", verdict)
	require.Equal(t, "Summary first\n\n- a.go:1 blocker: x", findings)
	verdict, findings = domain.ParseAgentReview("Looks fine")
	require.Equal(t, "unknown", verdict, "a missing verdict is never read as approval")
	require.Equal(t, "Looks fine", findings)
	verdict, findings = domain.ParseAgentReview("Verdict: approve\n" + strings.Repeat("é", domain.MaxAgentReviewFindings) + "\x00")
	require.Equal(t, "approve", verdict)
	require.LessOrEqual(t, len(findings), domain.MaxAgentReviewFindings)
	require.True(t, strings.HasPrefix(findings, "é"))
}

type reviewCommentStoreFake struct {
	due      []repository.ReviewComment
	finished map[uuid.UUID]bool
}

func (f *reviewCommentStoreFake) ClaimReviewComment(context.Context, time.Duration) (*repository.ReviewComment, error) {
	if len(f.due) == 0 {
		return nil, nil
	}
	next := f.due[0]
	f.due = f.due[1:]
	return &next, nil
}
func (f *reviewCommentStoreFake) FinishReviewComment(_ context.Context, review domain.AgentReview, posted bool) error {
	f.finished[review.TaskID] = posted
	return nil
}

// Only comments the store hands out (requested and finished) are posted, once
// per review with its marker, as plain comments with neutralized mentions.
func TestReviewCommentsArePostedWithAMarkerAndNoMentions(t *testing.T) {
	verdict, findings := "request_changes", "- ping @octocat about `x.go:3`"
	review := domain.AgentReview{TaskID: uuid.New(), WorkspaceID: uuid.New(), Provider: "codex", CommitSHA: "0123456789abcdef0123456789abcdef01234567", Repository: "acme/app", PullRequestNumber: 7, Verdict: &verdict, Findings: &findings}
	store := &reviewCommentStoreFake{due: []repository.ReviewComment{{Review: review, IssueIdentifier: "ENG-1"}}, finished: map[uuid.UUID]bool{}}
	svc := NewGitHubService(nil, nil, nil, nil, nil, nil, nil, "https://kuayle.test", "", nil)
	type posted struct{ repository, marker, body string }
	var calls []posted
	svc.postReviewComment = func(_ context.Context, _ uuid.UUID, repository string, number int, marker, body string) error {
		require.Equal(t, 7, number)
		calls = append(calls, posted{repository, marker, body})
		return nil
	}
	count, err := svc.PostReviewComments(context.Background())
	require.NoError(t, err)
	require.Zero(t, count, "nothing is wired yet")
	svc.SetReviewComments(store)
	count, err = svc.PostReviewComments(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, count)
	require.Len(t, calls, 1)
	require.Equal(t, "acme/app", calls[0].repository)
	require.Equal(t, "<!-- kuayle-agent-review:"+review.TaskID.String()+" -->", calls[0].marker)
	require.Contains(t, calls[0].body, "Agent review by Codex")
	require.Contains(t, calls[0].body, "ENG-1")
	require.Contains(t, calls[0].body, "> - ping @​octocat")
	require.NotContains(t, calls[0].body, "@octocat")
	require.Contains(t, calls[0].body, "not a GitHub review approval")
	require.True(t, store.finished[review.TaskID])
}
