package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	gh "github.com/kuayle/kuayle-backend/pkg/github"
	"github.com/stretchr/testify/require"
)

type prIssues struct {
	repository.IssueRepo
	issue *domain.Issue
}

func (f prIssues) GetByIdentifier(_ context.Context, _ uuid.UUID, identifier string) (*domain.Issue, error) {
	if f.issue == nil || f.issue.Identifier != identifier {
		return nil, errors.New("not found")
	}
	return f.issue, nil
}

type prMembers map[uuid.UUID]string

func (m prMembers) GetMember(_ context.Context, _, user uuid.UUID) (*domain.WorkspaceMember, error) {
	role, ok := m[user]
	if !ok {
		return nil, nil
	}
	return &domain.WorkspaceMember{Role: role}, nil
}

type prTargets struct {
	target *repository.PullRequestTarget
}

func (f *prTargets) PullRequestTarget(context.Context, uuid.UUID, uuid.UUID) (*repository.PullRequestTarget, error) {
	if f.target == nil {
		return nil, repository.ErrNoPullRequest
	}
	copy := *f.target
	return &copy, nil
}

// prGitHub records every call so tests can prove what was and was not sent.
type prGitHub struct {
	status                            PullRequestStatus
	checks                            []PullRequestCheck
	comments                          []PullRequestComment
	threadsUnavailable, listTruncated bool
	readErr                           error
	writeErr                          error
	methodErr                         error
	method                            string
	calls                             []string
	replied                           string
	statusCalls                       int
}

func (f *prGitHub) PullRequestStatus(context.Context, uuid.UUID, string, int) (*PullRequestStatus, error) {
	f.statusCalls++
	status := f.status
	return &status, f.readErr
}
func (f *prGitHub) PullRequestChecks(context.Context, uuid.UUID, string, int) (string, []PullRequestCheck, error) {
	return "head1", f.checks, f.readErr
}
func (f *prGitHub) PullRequestComments(context.Context, uuid.UUID, string, int) (*PullRequestCommentList, error) {
	return &PullRequestCommentList{Comments: append([]PullRequestComment(nil), f.comments...), ThreadsUnavailable: f.threadsUnavailable, Truncated: f.listTruncated}, f.readErr
}
func (f *prGitHub) ReplyToReviewComment(_ context.Context, _ uuid.UUID, _ string, _ int, id int64, body string) error {
	f.calls, f.replied = append(f.calls, "reply"), body
	return f.writeErr
}
func (f *prGitHub) ResolveReviewThread(_ context.Context, _ uuid.UUID, _ string, _ int, thread string) error {
	f.calls = append(f.calls, "resolve:"+thread)
	return f.writeErr
}
func (f *prGitHub) UpdatePullRequestBranch(_ context.Context, _ uuid.UUID, _ string, _ int, head string) (string, error) {
	f.calls = append(f.calls, "update:"+head)
	return "head2", f.writeErr
}
func (f *prGitHub) SetPullRequestDraft(_ context.Context, _ uuid.UUID, _ string, _ int, draft bool) error {
	f.calls = append(f.calls, map[bool]string{true: "draft", false: "ready"}[draft])
	return f.writeErr
}
func (f *prGitHub) ResolveMergeMethod(context.Context, uuid.UUID, string) (string, error) {
	return f.method, f.methodErr
}

type prWork struct {
	assistantAgentWork
	followUp *repository.FollowUpTarget
	merge    *repository.MergeTarget
}

func (f prWork) FollowUpTarget(context.Context, uuid.UUID, uuid.UUID) (*repository.FollowUpTarget, error) {
	if f.followUp == nil {
		return nil, repository.ErrNoMergeTarget
	}
	return f.followUp, nil
}
func (f prWork) MergeTarget(context.Context, uuid.UUID, *uuid.UUID, *uuid.UUID) (*repository.MergeTarget, error) {
	if f.merge == nil {
		return nil, repository.ErrNoMergeTarget
	}
	return f.merge, nil
}

type prFixture struct {
	tools   *AssistantTools
	github  *prGitHub
	targets *prTargets
	ws      uuid.UUID
	creator uuid.UUID
	admin   uuid.UUID
	other   uuid.UUID
	guest   uuid.UUID
	issue   *domain.Issue
	task    uuid.UUID
}

func newPRFixture() *prFixture {
	f := &prFixture{ws: uuid.New(), creator: uuid.New(), admin: uuid.New(), other: uuid.New(), guest: uuid.New(), task: uuid.New()}
	f.issue = &domain.Issue{ID: uuid.New(), Identifier: "ENG-1", Title: "Dark mode"}
	f.github = &prGitHub{method: "squash", status: PullRequestStatus{State: "open", HeadSHA: "head1", BaseRef: "main", Behind: 0, ChangedFiles: 3}}
	f.targets = &prTargets{target: &repository.PullRequestTarget{TaskID: f.task, IssueID: f.issue.ID, CreatorID: f.creator, State: "awaiting_review", Repository: "acme/app", Number: 12, URL: "https://github.com/acme/app/pull/12", Commit: "pub1"}}
	members := prMembers{f.creator: domain.RoleMember, f.admin: domain.RoleAdmin, f.other: domain.RoleMember, f.guest: domain.RoleGuest}
	f.tools = NewAssistantTools(prIssues{issue: f.issue}, nil, members, nil)
	f.tools.SetPullRequests(f.github, f.targets)
	return f
}

func (f *prFixture) run(t *testing.T, name, arguments string) (map[string]any, error) {
	t.Helper()
	result, _, err := f.tools.runPullRequestTool(context.Background(), f.ws, name, arguments)
	if err != nil {
		return nil, err
	}
	return result.(map[string]any), nil
}

func TestPullRequestToolCatalogRequiresApprovalForEveryChange(t *testing.T) {
	approval := map[string]bool{}
	for _, tool := range assistantPullRequestToolCatalog {
		approval[tool.name] = tool.approval
		require.True(t, tool.pulls)
		require.False(t, (&AssistantTools{}).available(tool), "%s is offered only once wired", tool.name)
		require.True(t, newPRFixture().tools.available(tool))
	}
	require.Equal(t, map[string]bool{
		"get_pull_request": false, "get_pull_request_checks": false, "list_pull_request_comments": false,
		"reply_to_review_comment": true, "resolve_review_thread": true, "update_pull_request_branch": true, "mark_pull_request_ready": true,
	}, approval)
}

func TestGetPullRequestReportsDraftConflictsAndBehindWithTheNextStep(t *testing.T) {
	f := newPRFixture()
	no := false
	f.github.status = PullRequestStatus{State: "open", Draft: true, Mergeable: &no, MergeableState: "dirty", HeadSHA: "head2", BaseRef: "main", Ahead: 2, Behind: 4, ChangedFiles: 7}
	result, err := f.run(t, "get_pull_request", `{"identifier":"eng-1"}`)
	require.NoError(t, err)
	require.Equal(t, true, result["draft"])
	require.Equal(t, false, result["mergeable"])
	require.Equal(t, 4, result["behind_base_by"])
	require.Equal(t, 7, result["changed_files"])
	require.Equal(t, "head2", result["head_sha"])
	require.Equal(t, true, result["head_moved_since_published"])
	steps := result["next_steps"].([]string)
	require.Len(t, steps, 2)
	require.Contains(t, steps[0], "draft")
	require.Contains(t, steps[0], "mark_pull_request_ready")
	require.Contains(t, steps[1], "conflicts")

	// Not mergeable yet is "unknown", not false.
	f.github.status = PullRequestStatus{State: "open", HeadSHA: "head1", BaseRef: "main", Behind: -1}
	result, err = f.run(t, "get_pull_request", `{"identifier":"ENG-1"}`)
	require.NoError(t, err)
	require.Equal(t, "unknown", result["mergeable"])
	require.NotContains(t, result, "behind_base_by")
}

func TestPullRequestNextStepsForClosedMergedAndReadyPullRequests(t *testing.T) {
	require.Contains(t, pullRequestNextSteps(&PullRequestStatus{State: "merged"})[0], "already merged")
	require.Contains(t, pullRequestNextSteps(&PullRequestStatus{State: "closed"})[0], "closed without merging")
	yes := true
	ready := pullRequestNextSteps(&PullRequestStatus{State: "open", Mergeable: &yes, MergeableState: "clean", Behind: 0})
	require.Len(t, ready, 1)
	require.Contains(t, ready[0], "Nothing blocks")
	behind := pullRequestNextSteps(&PullRequestStatus{State: "open", Mergeable: &yes, MergeableState: "behind", Behind: 3, BaseRef: "main"})
	require.Contains(t, behind[0], "3 commit(s) behind main")
}

func TestPullRequestToolsReportMissingPullRequestsAndGitHubFailuresClearly(t *testing.T) {
	f := newPRFixture()
	f.targets.target = nil
	_, err := f.run(t, "get_pull_request", `{"identifier":"ENG-1"}`)
	require.ErrorContains(t, err, "has no pull request yet")
	require.ErrorContains(t, err, "get_agent_work")
	_, err = f.run(t, "get_pull_request", `{"identifier":"ENG-9"}`)
	require.ErrorContains(t, err, "issue not found")
	_, err = f.run(t, "get_pull_request", `{"identifier":"ENG-1","x":1}`)
	require.ErrorContains(t, err, "invalid arguments")

	f = newPRFixture()
	for _, tc := range []struct {
		err  error
		want string
	}{
		{&gh.APIError{Status: 403, Message: "Resource not accessible by integration"}, "GitHub App is not allowed"},
		{&gh.APIError{Status: 404}, "cannot find the pull request"},
		{&gh.APIError{Status: 502, Message: "Bad gateway"}, "Bad gateway"},
		{errors.New("dial tcp: timeout"), "could not be reached"},
	} {
		f.github.readErr = tc.err
		for _, tool := range []string{"get_pull_request", "get_pull_request_checks", "list_pull_request_comments"} {
			_, err := f.run(t, tool, `{"identifier":"ENG-1"}`)
			require.ErrorContains(t, err, tc.want, tool)
			require.NotContains(t, toolErrorText(err), "dial tcp", "internal errors stay out of the chat")
		}
	}
}

func TestGetPullRequestChecksListsFailingFirstWithLogLinks(t *testing.T) {
	f := newPRFixture()
	f.github.checks = []PullRequestCheck{
		{Name: "lint", State: "passed"},
		{Name: "unit", State: "failed", URL: "https://github.com/acme/app/actions/runs/1/job/2", Summary: "3 tests failed"},
		{Name: "e2e", State: "pending"},
	}
	result, err := f.run(t, "get_pull_request_checks", `{"identifier":"ENG-1"}`)
	require.NoError(t, err)
	require.Equal(t, "failing", result["state"])
	require.Equal(t, map[string]int{"passed": 1, "failed": 1, "pending": 1, "total": 3}, result["summary"])
	checks := result["checks"].([]PullRequestCheck)
	require.Equal(t, "unit", checks[0].Name, "failing checks come first")
	require.Equal(t, "https://github.com/acme/app/actions/runs/1/job/2", checks[0].URL)
	require.Contains(t, result["next_step"], "follow_up_task")

	f.github.checks = []PullRequestCheck{{Name: "e2e", State: "pending"}}
	result, _ = f.run(t, "get_pull_request_checks", `{"identifier":"ENG-1"}`)
	require.Equal(t, "pending", result["state"])
	require.Contains(t, result["next_step"], "still running")
	f.github.checks = nil
	result, _ = f.run(t, "get_pull_request_checks", `{"identifier":"ENG-1"}`)
	require.Equal(t, "none", result["state"])
	f.github.checks = []PullRequestCheck{{Name: "lint", State: "passed"}}
	result, _ = f.run(t, "get_pull_request_checks", `{"identifier":"ENG-1"}`)
	require.Equal(t, "passing", result["state"])
}

func TestCheckRunStateMapsConclusions(t *testing.T) {
	for conclusion, want := range map[string]string{"success": "passed", "neutral": "passed", "skipped": "passed", "failure": "failed", "timed_out": "failed", "cancelled": "failed", "action_required": "failed"} {
		require.Equal(t, want, checkRunState(gh.CheckRun{Status: "completed", Conclusion: conclusion}), conclusion)
	}
	require.Equal(t, "pending", checkRunState(gh.CheckRun{Status: "in_progress"}))
	require.Equal(t, "pending", checkRunState(gh.CheckRun{Status: "queued"}))
}

var reviewComments = []PullRequestComment{
	{ID: 1, Author: "chatgpt-codex-connector", Bot: true, Path: "a.go", Line: 4, Body: "Possible nil dereference", ThreadID: "T1"},
	{ID: 2, Author: "dev", Path: "a.go", Body: "Fixed", ReplyTo: 1, ThreadID: "T1"},
	{ID: 3, Author: "maintainer", Path: "b.go", Body: "Rename", ThreadID: "T2", Resolved: true},
	{ID: 4, Author: "maintainer", Path: "c.go", Body: "Add a test", ThreadID: "T3"},
}

func TestListPullRequestCommentsCountsUnresolvedThreadsIncludingBots(t *testing.T) {
	f := newPRFixture()
	f.github.comments = reviewComments
	result, err := f.run(t, "list_pull_request_comments", `{"identifier":"ENG-1"}`)
	require.NoError(t, err)
	require.Equal(t, 2, result["unresolved_threads"])
	require.Equal(t, 1, result["unresolved_by_bots"])
	comments := result["comments"].([]PullRequestComment)
	require.True(t, comments[0].Bot || !comments[0].Resolved, "unresolved threads are listed first")
	require.Equal(t, "T2", comments[len(comments)-1].ThreadID)
	require.Contains(t, result["next_step"], "2 review thread(s) are unresolved")
	require.Contains(t, result["next_step"], "follow_up_task")

	f.github.comments = reviewComments[2:3]
	result, _ = f.run(t, "list_pull_request_comments", `{"identifier":"ENG-1"}`)
	require.Equal(t, 0, result["unresolved_threads"])
	require.Equal(t, "No unresolved review threads.", result["next_step"])
}

func TestPullRequestChangesAreRefusedForThoseWhoCannotMerge(t *testing.T) {
	f := newPRFixture()
	f.github.comments = reviewComments
	ctx := context.Background()
	reply := `{"identifier":"ENG-1","comment_id":1,"body":"Fixed in abc"}`
	_, _, _, err := f.tools.preparePullRequestAction(ctx, f.ws, f.other, "reply_to_review_comment", reply)
	require.ErrorContains(t, err, "only workspace admins or the task's creator")
	_, _, _, err = f.tools.preparePullRequestAction(ctx, f.ws, f.guest, "reply_to_review_comment", reply)
	require.ErrorContains(t, err, "no longer have access")
	_, _, _, err = f.tools.preparePullRequestAction(ctx, f.ws, uuid.New(), "reply_to_review_comment", reply)
	require.ErrorContains(t, err, "no longer have access")
	for _, user := range []uuid.UUID{f.creator, f.admin} {
		_, _, action, err := f.tools.preparePullRequestAction(ctx, f.ws, user, "reply_to_review_comment", reply)
		require.NoError(t, err)
		require.Equal(t, "reply_to_review_comment", action)
	}
	require.Empty(t, f.github.calls, "preparing a request sends nothing to GitHub")
	f.targets.target = nil
	_, _, _, err = f.tools.preparePullRequestAction(ctx, f.ws, f.admin, "reply_to_review_comment", reply)
	require.ErrorContains(t, err, "has no pull request yet")
}

func TestReplyAndResolveBindTheCommentTheUserSees(t *testing.T) {
	f := newPRFixture()
	f.github.comments = reviewComments
	ctx := context.Background()
	issue, payload, action, err := f.tools.preparePullRequestAction(ctx, f.ws, f.creator, "reply_to_review_comment", `{"identifier":"ENG-1","comment_id":2,"body":"  Fixed in abc  "}`)
	require.NoError(t, err)
	require.Equal(t, f.issue.ID, issue)
	require.Equal(t, "reply_to_review_comment", action)
	bound := payload.(domain.PullRequestActionPayload)
	require.EqualValues(t, 1, bound.CommentID, "a reply to a reply goes to the first comment of the thread")
	require.Equal(t, "chatgpt-codex-connector", bound.Author)
	require.True(t, bound.Bot)
	require.Equal(t, "Fixed in abc", bound.Body)
	require.Equal(t, f.task, bound.TaskID)
	require.Equal(t, 12, bound.PullRequest)

	for _, body := range []string{"", "   ", string(make([]rune, 4001))} {
		args, _ := json.Marshal(map[string]any{"identifier": "ENG-1", "comment_id": 1, "body": body})
		_, _, _, err = f.tools.preparePullRequestAction(ctx, f.ws, f.creator, "reply_to_review_comment", string(args))
		require.ErrorContains(t, err, "body must be 1 to 4000")
	}
	_, _, _, err = f.tools.preparePullRequestAction(ctx, f.ws, f.creator, "reply_to_review_comment", `{"identifier":"ENG-1","comment_id":99,"body":"x"}`)
	require.ErrorContains(t, err, "list_pull_request_comments")

	_, payload, _, err = f.tools.preparePullRequestAction(ctx, f.ws, f.creator, "resolve_review_thread", `{"identifier":"ENG-1","thread_id":"T3"}`)
	require.NoError(t, err)
	require.Equal(t, "T3", payload.(domain.PullRequestActionPayload).ThreadID)
	require.Equal(t, "c.go", payload.(domain.PullRequestActionPayload).Path)
	_, _, _, err = f.tools.preparePullRequestAction(ctx, f.ws, f.creator, "resolve_review_thread", `{"identifier":"ENG-1","thread_id":"T2"}`)
	require.ErrorContains(t, err, "already resolved")
	_, _, _, err = f.tools.preparePullRequestAction(ctx, f.ws, f.creator, "resolve_review_thread", `{"identifier":"ENG-1","thread_id":"nope"}`)
	require.ErrorContains(t, err, "not on the pull request")
}

func TestMarkReadyRefusesNoOpsAndClosedPullRequests(t *testing.T) {
	f := newPRFixture()
	ctx := context.Background()
	f.github.status.Draft = true
	_, payload, action, err := f.tools.preparePullRequestAction(ctx, f.ws, f.creator, "mark_pull_request_ready", `{"identifier":"ENG-1","draft":false}`)
	require.NoError(t, err)
	require.Equal(t, "mark_pull_request_ready", action)
	require.False(t, payload.(domain.PullRequestActionPayload).Draft)
	_, _, _, err = f.tools.preparePullRequestAction(ctx, f.ws, f.creator, "mark_pull_request_ready", `{"identifier":"ENG-1","draft":true}`)
	require.ErrorContains(t, err, "already a draft")
	f.github.status.Draft = false
	_, _, _, err = f.tools.preparePullRequestAction(ctx, f.ws, f.creator, "mark_pull_request_ready", `{"identifier":"ENG-1","draft":false}`)
	require.ErrorContains(t, err, "already ready for review")
	_, _, _, err = f.tools.preparePullRequestAction(ctx, f.ws, f.creator, "mark_pull_request_ready", `{"identifier":"ENG-1"}`)
	require.ErrorContains(t, err, "invalid arguments")
	f.github.status.State = "merged"
	_, _, _, err = f.tools.preparePullRequestAction(ctx, f.ws, f.creator, "mark_pull_request_ready", `{"identifier":"ENG-1","draft":true}`)
	require.ErrorContains(t, err, "is merged")
	require.ErrorContains(t, err, "already merged")
}

func TestUpdateBranchPrefersTheTaskAgentThenGitHub(t *testing.T) {
	f := newPRFixture()
	ctx := context.Background()
	f.github.status.Behind = 3
	f.tools.work = prWork{followUp: &repository.FollowUpTarget{TaskID: f.task, CreatorID: f.creator, State: "awaiting_review", Generation: 2}}

	_, payload, action, err := f.tools.preparePullRequestAction(ctx, f.ws, f.creator, "update_pull_request_branch", `{"identifier":"ENG-1"}`)
	require.NoError(t, err)
	require.Equal(t, "follow_up_task", action, "the task agent merges the base in its own checkout")
	followUp := payload.(domain.FollowUpPayload)
	require.Equal(t, f.task, followUp.TaskID)
	require.EqualValues(t, 2, followUp.Generation)
	require.Contains(t, followUp.Instructions, "origin/main")
	require.Contains(t, followUp.Instructions, "no rebase or force-push")
	require.Equal(t, "ENG-1", pullRequestIdentifier(payload))

	_, payload, action, err = f.tools.preparePullRequestAction(ctx, f.ws, f.creator, "update_pull_request_branch", `{"identifier":"ENG-1","method":"github"}`)
	require.NoError(t, err)
	require.Equal(t, "update_pull_request_branch", action)
	require.Equal(t, "head1", payload.(domain.PullRequestActionPayload).Head, "GitHub updates only the head the user saw")
	require.Equal(t, 3, payload.(domain.PullRequestActionPayload).Behind)

	// An admin who did not create the task cannot ask its agent, so GitHub updates it.
	_, _, action, err = f.tools.preparePullRequestAction(ctx, f.ws, f.admin, "update_pull_request_branch", `{"identifier":"ENG-1"}`)
	require.NoError(t, err)
	require.Equal(t, "update_pull_request_branch", action)
	_, _, _, err = f.tools.preparePullRequestAction(ctx, f.ws, f.admin, "update_pull_request_branch", `{"identifier":"ENG-1","method":"task_agent"}`)
	require.ErrorContains(t, err, "cannot take a follow-up now")

	// A running task is not interrupted.
	f.tools.work = prWork{followUp: &repository.FollowUpTarget{TaskID: f.task, CreatorID: f.creator, State: "running"}}
	_, _, action, _ = f.tools.preparePullRequestAction(ctx, f.ws, f.creator, "update_pull_request_branch", `{"identifier":"ENG-1"}`)
	require.Equal(t, "update_pull_request_branch", action)
	require.Empty(t, f.github.calls)
}

func TestUpdateBranchExplainsConflictsAndUpToDateBranches(t *testing.T) {
	f := newPRFixture()
	ctx := context.Background()
	_, _, _, err := f.tools.preparePullRequestAction(ctx, f.ws, f.creator, "update_pull_request_branch", `{"identifier":"ENG-1"}`)
	require.ErrorContains(t, err, "already up to date with main")

	f.github.status.Behind, f.github.status.MergeableState = 2, "dirty"
	f.tools.work = prWork{followUp: &repository.FollowUpTarget{TaskID: f.task, CreatorID: f.creator, State: "blocked"}}
	_, _, _, err = f.tools.preparePullRequestAction(ctx, f.ws, f.creator, "update_pull_request_branch", `{"identifier":"ENG-1","method":"github"}`)
	require.ErrorContains(t, err, "merge conflicts")
	require.ErrorContains(t, err, "task agent resolves them")
	_, _, action, err := f.tools.preparePullRequestAction(ctx, f.ws, f.creator, "update_pull_request_branch", `{"identifier":"ENG-1"}`)
	require.NoError(t, err)
	require.Equal(t, "follow_up_task", action)

	f.tools.work = nil
	_, _, _, err = f.tools.preparePullRequestAction(ctx, f.ws, f.creator, "update_pull_request_branch", `{"identifier":"ENG-1"}`)
	require.ErrorContains(t, err, "conflicts GitHub cannot resolve")

	f.github.status.State = "closed"
	_, _, _, err = f.tools.preparePullRequestAction(ctx, f.ws, f.creator, "update_pull_request_branch", `{"identifier":"ENG-1"}`)
	require.ErrorContains(t, err, "is closed")
}

func TestMergeRequestIsRefusedForDraftsWithTheNextStepAndShowsTheMethod(t *testing.T) {
	f := newPRFixture()
	ctx := context.Background()
	f.tools.work = prWork{merge: &repository.MergeTarget{TaskID: f.task, IssueID: f.issue.ID, CreatorID: f.creator, Repository: "acme/app", Number: 12, URL: "u", Commit: "pub1"}}
	f.github.status.Draft = true
	_, _, err := f.tools.prepareMerge(ctx, f.ws, f.creator, `{"identifier":"ENG-1"}`)
	require.ErrorContains(t, err, "cannot be merged yet")
	require.ErrorContains(t, err, "mark_pull_request_ready")

	f.github.status.Draft = false
	f.github.method = "rebase"
	_, payload, err := f.tools.prepareMerge(ctx, f.ws, f.creator, `{"identifier":"ENG-1"}`)
	require.NoError(t, err)
	require.Equal(t, "rebase", payload.MergeMethod)

	f.github.methodErr = errors.Join(ErrMergeMethodNotAllowed)
	_, _, err = f.tools.prepareMerge(ctx, f.ws, f.creator, `{"identifier":"ENG-1"}`)
	require.ErrorContains(t, err, "development settings")

	f.github.methodErr = nil
	f.github.readErr = &gh.APIError{Status: 403}
	_, _, err = f.tools.prepareMerge(ctx, f.ws, f.creator, `{"identifier":"ENG-1"}`)
	require.ErrorContains(t, err, "GitHub App is not allowed")
}

func approvalFor(f *prFixture, actor uuid.UUID, action string, payload domain.PullRequestActionPayload) (*AssistantApprovals, *domain.ConversationApproval) {
	approvals := NewAssistantApprovals(nil, nil)
	approvals.SetPullRequests(f.github, f.targets)
	approvals.members = prMembers{f.creator: domain.RoleMember, f.admin: domain.RoleAdmin, f.other: domain.RoleMember, f.guest: domain.RoleGuest}
	payload.TaskID, payload.Repository, payload.PullRequest = f.task, "acme/app", 12
	data, _ := json.Marshal(payload)
	return approvals, &domain.ConversationApproval{ID: uuid.New(), IssueID: f.issue.ID, ActorID: actor, Action: action, Payload: data}
}

func TestApprovedPullRequestChangesRunAsTheApproverAndAreBoundToThePullRequest(t *testing.T) {
	ctx := context.Background()
	for action, tc := range map[string]struct {
		payload domain.PullRequestActionPayload
		call    string
	}{
		"reply_to_review_comment":    {domain.PullRequestActionPayload{CommentID: 1, Body: "Fixed"}, "reply"},
		"resolve_review_thread":      {domain.PullRequestActionPayload{ThreadID: "T1"}, "resolve:T1"},
		"update_pull_request_branch": {domain.PullRequestActionPayload{Head: "head1"}, "update:head1"},
		"mark_pull_request_ready":    {domain.PullRequestActionPayload{Draft: true}, "draft"},
	} {
		t.Run(action, func(t *testing.T) {
			f := newPRFixture()
			approvals, approval := approvalFor(f, f.admin, action, tc.payload)
			task, message, err := approvals.applyPullRequestAction(ctx, f.ws, approval)
			require.NoError(t, err)
			require.Empty(t, message)
			require.Equal(t, f.task, task)
			require.Equal(t, []string{tc.call}, f.github.calls)
			if action == "reply_to_review_comment" {
				require.Equal(t, "Fixed", f.github.replied)
			}

			// The task's creator may act too; anyone else, or a guest, may not.
			f = newPRFixture()
			approvals, approval = approvalFor(f, f.creator, action, tc.payload)
			_, message, err = approvals.applyPullRequestAction(ctx, f.ws, approval)
			require.NoError(t, err)
			require.Empty(t, message)
			for _, actor := range []uuid.UUID{f.other, f.guest, uuid.New()} {
				f = newPRFixture()
				approvals, approval = approvalFor(f, actor, action, tc.payload)
				_, message, err = approvals.applyPullRequestAction(ctx, f.ws, approval)
				require.NoError(t, err)
				require.Contains(t, message, "Only workspace admins or the task's creator")
				require.Empty(t, f.github.calls, "a refused approval changes nothing on GitHub")
			}

			// A different pull request, or none, invalidates the approval.
			f = newPRFixture()
			approvals, approval = approvalFor(f, f.admin, action, tc.payload)
			f.targets.target.Number = 13
			_, message, _ = approvals.applyPullRequestAction(ctx, f.ws, approval)
			require.Contains(t, message, "changed after approval")
			f.targets.target.Number, f.targets.target.TaskID = 12, uuid.New()
			_, message, _ = approvals.applyPullRequestAction(ctx, f.ws, approval)
			require.Contains(t, message, "changed after approval")
			f.targets.target = nil
			_, message, _ = approvals.applyPullRequestAction(ctx, f.ws, approval)
			require.Contains(t, message, "no longer available")
			require.Empty(t, f.github.calls)
		})
	}
}

func TestApprovedPullRequestChangesReportGitHubRefusalsWithTheNextStep(t *testing.T) {
	ctx := context.Background()
	for name, tc := range map[string]struct {
		err       error
		action    string
		want      string
		transient bool
	}{
		"app lacks permission": {&gh.APIError{Status: 403, Message: "Resource not accessible by integration"}, "resolve_review_thread", "lacks the permission", false},
		"gone":                 {&gh.APIError{Status: 404}, "reply_to_review_comment", "cannot find it", false},
		"branch conflict":      {&gh.APIError{Status: 422, Message: "merge conflict between base and head"}, "update_pull_request_branch", "task agent merges the base itself", false},
		"head moved":           {&gh.APIError{Status: 422, Message: "expected head sha didn't match current head ref"}, "update_pull_request_branch", "expected head sha", false},
		"github down":          {&gh.APIError{Status: 503}, "mark_pull_request_ready", "", true},
		"network":              {errors.New("connection reset"), "mark_pull_request_ready", "", true},
	} {
		t.Run(name, func(t *testing.T) {
			f := newPRFixture()
			f.github.writeErr = tc.err
			approvals, approval := approvalFor(f, f.admin, tc.action, domain.PullRequestActionPayload{CommentID: 1, Body: "x", ThreadID: "T1", Head: "head1"})
			_, message, err := approvals.applyPullRequestAction(ctx, f.ws, approval)
			if tc.transient {
				require.Error(t, err, "a transient failure keeps the claim so approving again resumes it")
				require.Empty(t, message)
				return
			}
			require.NoError(t, err)
			require.Contains(t, message, tc.want)
		})
	}
}

func TestApprovedPullRequestChangeWithoutWiringOrPayloadCannotRun(t *testing.T) {
	f := newPRFixture()
	approvals, approval := approvalFor(f, f.admin, "resolve_review_thread", domain.PullRequestActionPayload{ThreadID: "T1"})
	approval.Payload = json.RawMessage(`not json`)
	_, message, err := approvals.applyPullRequestAction(context.Background(), f.ws, approval)
	require.NoError(t, err)
	require.Equal(t, "This approval cannot be executed", message)
	approvals.pulls = nil
	_, message, _ = approvals.applyPullRequestAction(context.Background(), f.ws, approval)
	require.Equal(t, "This approval cannot be executed", message)
}

func TestMergeRefusalsNameTheNextStep(t *testing.T) {
	for reason, want := range map[string]string{
		"Head branch was modified. Review and try the merge again.": "head moved after it was approved (abc1234d)",
		"Pull Request is still a draft":                             "mark_pull_request_ready",
		"Required status check \"unit\" is failing":                 "get_pull_request_checks",
		"At least 1 approving review is required":                   "list_pull_request_comments",
		"Merge commits are not allowed on this repository":          "development settings",
		"Something else": "GitHub refused the merge: Something else",
	} {
		require.Contains(t, mergeRefusalMessage(reason, "abc1234def"), want, reason)
	}
}

type mergeSettingsFake struct {
	method string
	err    error
}

func (f mergeSettingsFake) GetMergeMethod(context.Context, uuid.UUID, string) (string, error) {
	return f.method, f.err
}
func (mergeSettingsFake) SetMergeMethod(context.Context, uuid.UUID, string, string) error { return nil }
func (mergeSettingsFake) ListMergeMethods(context.Context, uuid.UUID) (map[string]string, error) {
	return nil, nil
}

func TestResolveMergeMethodUsesTheSettingOtherwiseWhatTheRepositoryAllows(t *testing.T) {
	ctx := context.Background()
	allowing := func(settings gh.RepositoryMergeSettings, err error) func(context.Context, uuid.UUID, string) (*gh.RepositoryMergeSettings, error) {
		return func(context.Context, uuid.UUID, string) (*gh.RepositoryMergeSettings, error) { return &settings, err }
	}
	for name, tc := range map[string]struct {
		configured string
		allowed    gh.RepositoryMergeSettings
		readErr    error
		want       string
		wantErr    bool
	}{
		"configured and allowed":       {"rebase", gh.RepositoryMergeSettings{AllowSquashMerge: true, AllowRebaseMerge: true}, nil, "rebase", false},
		"configured but disabled":      {"merge", gh.RepositoryMergeSettings{AllowSquashMerge: true}, nil, "", true},
		"default prefers squash":       {"", gh.RepositoryMergeSettings{AllowSquashMerge: true, AllowMergeCommit: true, AllowRebaseMerge: true}, nil, "squash", false},
		"default first allowed":        {"", gh.RepositoryMergeSettings{AllowRebaseMerge: true, AllowMergeCommit: true}, nil, "merge", false},
		"nothing allowed":              {"", gh.RepositoryMergeSettings{}, nil, "", true},
		"rules unreadable, no setting": {"", gh.RepositoryMergeSettings{}, errors.New("403"), "squash", false},
		"rules unreadable, setting":    {"rebase", gh.RepositoryMergeSettings{}, errors.New("403"), "rebase", false},
	} {
		t.Run(name, func(t *testing.T) {
			s := &GitHubService{mergeSettings: mergeSettingsFake{method: tc.configured}, repositoryMergeSettings: allowing(tc.allowed, tc.readErr)}
			method, err := s.ResolveMergeMethod(ctx, uuid.New(), "acme/app")
			if tc.wantErr {
				require.ErrorIs(t, err, ErrMergeMethodNotAllowed)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, method)
		})
	}
	s := &GitHubService{mergeSettings: mergeSettingsFake{err: errors.New("db down")}, repositoryMergeSettings: allowing(gh.RepositoryMergeSettings{AllowSquashMerge: true}, nil)}
	_, err := s.ResolveMergeMethod(ctx, uuid.New(), "acme/app")
	require.Error(t, err)
}

func TestListPullRequestCommentsReportsUnknownThreadStateInsteadOfUnresolved(t *testing.T) {
	f := newPRFixture()
	f.github.comments = []PullRequestComment{{ID: 1, Author: "dev", Path: "a.go", Body: "Rename"}}
	f.github.threadsUnavailable = true
	result, err := f.run(t, "list_pull_request_comments", `{"identifier":"ENG-1"}`)
	require.NoError(t, err)
	require.Equal(t, true, result["threads_unavailable"])
	require.NotContains(t, result, "unresolved_threads")
	require.Contains(t, result["next_step"], "could not be read")
	require.NotContains(t, result["next_step"], "unresolved. Fix")
}

func TestListPullRequestCommentsCountsRepliesWhoseRootWasCutOffAndKeepsThreadsWhole(t *testing.T) {
	f := newPRFixture()
	f.github.comments = []PullRequestComment{
		{ID: 12, Author: "dev", Body: "orphan reply", ReplyTo: 11, ThreadID: "T9"},
		{ID: 20, Author: "dev", Body: "root", ThreadID: "T5", Resolved: true},
		{ID: 21, Author: "dev", Body: "reply", ReplyTo: 20, ThreadID: "T5", Resolved: true},
	}
	f.github.listTruncated = true
	result, err := f.run(t, "list_pull_request_comments", `{"identifier":"ENG-1"}`)
	require.NoError(t, err)
	require.Equal(t, 1, result["unresolved_threads"], "an unresolved thread counts once even without its root")
	require.Equal(t, true, result["list_truncated"])
	require.Contains(t, result["next_step"], "more than this tool reads")

	// A cut never lists a reply without its root.
	var comments []PullRequestComment
	for i := 0; i < 30; i++ {
		id := int64(100 + i*2)
		thread := fmt.Sprintf("T%d", i)
		comments = append(comments, PullRequestComment{ID: id + 1, ReplyTo: id, ThreadID: thread}, PullRequestComment{ID: id, ThreadID: thread})
	}
	f.github.comments, f.github.listTruncated = comments, false
	result, _ = f.run(t, "list_pull_request_comments", `{"identifier":"ENG-1"}`)
	listed := result["comments"].([]PullRequestComment)
	require.Equal(t, 60-len(listed), result["comments_not_listed"])
	require.LessOrEqual(t, len(listed), 50)
	for i := 0; i < len(listed); i += 2 {
		require.Zero(t, listed[i].ReplyTo, "each thread starts with its root")
		require.Equal(t, listed[i].ID, listed[i+1].ReplyTo)
	}
}

func TestApprovedBranchUpdateReportsAHeadItCouldNotRecord(t *testing.T) {
	f := newPRFixture()
	f.github.writeErr = &HeadSyncError{Err: errors.New("database down")}
	approvals, approval := approvalFor(f, f.admin, "update_pull_request_branch", domain.PullRequestActionPayload{Head: "head1"})
	_, message, err := approvals.applyPullRequestAction(context.Background(), f.ws, approval)
	require.NoError(t, err, "GitHub did update the branch, so approving again would not help")
	require.Contains(t, message, "could not record the new head")
	require.Contains(t, message, "follow_up_task")
}
