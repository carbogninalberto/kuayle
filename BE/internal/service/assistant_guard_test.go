package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/require"
)

func TestEveryApprovalToolHasAnActionPolicy(t *testing.T) {
	for _, tool := range assistantTools() {
		if tool.approval {
			_, ok := assistantActionPolicies[tool.name]
			require.True(t, ok, "%s needs approval but has no assistantActionPolicy", tool.name)
		}
	}
	_, ok := assistantActionPolicies["run_custom_tool"]
	require.True(t, ok)
}

func TestPermissionGapNamesWhatToGrantAndWhere(t *testing.T) {
	needs := map[string]string{"contents": "write", "actions": "read"}
	const appURL, instURL = "https://github.com/settings/apps/kuayle/permissions", "https://github.com/settings/installations/1"

	require.Nil(t, permissionGap(needs, map[string]string{"contents": "write", "actions": "read"}, map[string]string{"contents": "write", "actions": "write"}, appURL, instURL),
		"a higher level satisfies a lower one")

	gap := permissionGap(needs, map[string]string{"contents": "read"}, map[string]string{"contents": "read"}, appURL, instURL)
	require.NotNil(t, gap)
	require.Equal(t, []domain.GitHubPermissionNeed{{Name: "actions", Level: "read"}, {Name: "contents", Level: "write"}}, gap.Missing)
	require.Equal(t, appURL, gap.SettingsURL, "the app itself must request the permission")
	require.Contains(t, gap.Message, "contents: write")
	require.Contains(t, gap.Message, "actions: read")
	require.Contains(t, gap.Message, appURL)

	pending := permissionGap(needs, needs, map[string]string{"actions": "read"}, appURL, instURL)
	require.Equal(t, []domain.GitHubPermissionNeed{{Name: "contents", Level: "write"}}, pending.Missing)
	require.Equal(t, instURL, pending.SettingsURL, "the installation must accept what the app already requests")
}

type accessFake struct {
	gap   *domain.GitHubAccessGap
	err   error
	asked map[string]string
}

func (a *accessFake) PermissionGap(_ context.Context, _ uuid.UUID, needs map[string]string) (*domain.GitHubAccessGap, error) {
	a.asked = needs
	return a.gap, a.err
}

func TestPreflightAsksForTheToolsPermissions(t *testing.T) {
	gap := newGitHubAccessGap([]domain.GitHubPermissionNeed{{Name: "contents", Level: "write"}}, "https://github.com/settings/apps/x/permissions")
	access := &accessFake{gap: gap}
	require.Equal(t, gap, preflightGitHub(context.Background(), access, uuid.New(), "create_release"))
	require.Equal(t, map[string]string{"contents": "write"}, access.asked)

	require.Nil(t, preflightGitHub(context.Background(), access, uuid.New(), "run_check"), "tools without GitHub needs are not checked")
	require.Nil(t, preflightGitHub(context.Background(), nil, uuid.New(), "create_release"))
	require.Nil(t, preflightGitHub(context.Background(), &accessFake{err: errors.New("down")}, uuid.New(), "create_release"),
		"an unreachable GitHub is not reported as a missing permission")
}

func TestWithGitHubAccessPutsTheGapOnTheApprovalCard(t *testing.T) {
	payload := domain.MergePayload{Identifier: "KUA-1", Repository: "o/r"}
	same, err := withGitHubAccess(payload, nil)
	require.NoError(t, err)
	require.Equal(t, payload, same)

	gap := newGitHubAccessGap([]domain.GitHubPermissionNeed{{Name: "pull_requests", Level: "write"}}, "https://github.com/settings/apps/x/permissions")
	merged, err := withGitHubAccess(payload, gap)
	require.NoError(t, err)
	data, _ := json.Marshal(merged)
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &fields))
	require.JSONEq(t, `"KUA-1"`, string(fields["identifier"]))
	var got domain.GitHubAccessGap
	require.NoError(t, json.Unmarshal(fields["github_access"], &got))
	require.Equal(t, *gap, got)
}

type memberFake struct{ role string }

func (m memberFake) GetMember(context.Context, uuid.UUID, uuid.UUID) (*domain.WorkspaceMember, error) {
	if m.role == "" {
		return nil, nil
	}
	return &domain.WorkspaceMember{Role: m.role}, nil
}

func TestAdminOnlyActionsAreRefusedForOtherRoles(t *testing.T) {
	ctx, ws, user := context.Background(), uuid.New(), uuid.New()
	for role, allowed := range map[string]bool{domain.RoleOwner: true, domain.RoleAdmin: true, domain.RoleMember: false, "": false} {
		refusal, err := authorizeAssistantAction(ctx, memberFake{role}, nil, ws, user, "create_release")
		require.NoError(t, err)
		require.Equal(t, allowed, refusal == "", "role %q", role)
	}
	refusal, err := authorizeAssistantAction(ctx, memberFake{domain.RoleMember}, nil, ws, user, "run_check")
	require.NoError(t, err)
	require.Empty(t, refusal, "members may use member-level actions")
}

func TestAssistantActionsAreWrittenToTheAuditLog(t *testing.T) {
	hook := logtest.NewGlobal()
	defer logrus.StandardLogger().ReplaceHooks(make(logrus.LevelHooks))
	actor, ws := uuid.New(), uuid.New()
	auditAssistantAction("approval_approved", actor, ws, "merge_pull_request", map[string]interface{}{"approval_id": "a1"})

	entry := hook.LastEntry()
	require.NotNil(t, entry)
	require.Equal(t, true, entry.Data["audit"])
	require.Equal(t, "assistant.approval_approved", entry.Data["action"])
	require.Equal(t, actor.String(), entry.Data["actor_id"])
	require.Equal(t, ws.String(), entry.Data["workspace_id"])
	require.Equal(t, "merge_pull_request", entry.Data["tool"])
	require.Equal(t, "a1", entry.Data["approval_id"])
}

type countingAccess struct {
	calls int
	gap   *domain.GitHubAccessGap
	err   error
}

func (c *countingAccess) PermissionGap(context.Context, uuid.UUID, map[string]string) (*domain.GitHubAccessGap, error) {
	c.calls++
	return c.gap, c.err
}

func TestCachedAccessRemembersBrieflyAndExpires(t *testing.T) {
	ctx, ws := context.Background(), uuid.New()
	needs := map[string]string{"contents": "write"}
	inner := &countingAccess{gap: newGitHubAccessGap([]domain.GitHubPermissionNeed{{Name: "contents", Level: "write"}}, "")}
	cache := NewCachedGitHubAccess(inner)
	now := time.Now()
	cache.now = func() time.Time { return now }

	for i := 0; i < 3; i++ {
		gap, err := cache.PermissionGap(ctx, ws, needs)
		require.NoError(t, err)
		require.Equal(t, inner.gap, gap)
	}
	require.Equal(t, 1, inner.calls, "repeated checks within the TTL reuse the answer")

	_, _ = cache.PermissionGap(ctx, uuid.New(), needs)
	_, _ = cache.PermissionGap(ctx, ws, map[string]string{"actions": "read"})
	require.Equal(t, 3, inner.calls, "another workspace or permission set is checked separately")

	now = now.Add(githubAccessTTL + time.Second)
	_, _ = cache.PermissionGap(ctx, ws, needs)
	require.Equal(t, 4, inner.calls, "an expired answer is read again")

	inner.err = errors.New("down")
	now = now.Add(time.Hour)
	_, err := cache.PermissionGap(ctx, ws, needs)
	require.Error(t, err)
	inner.err = nil
	_, _ = cache.PermissionGap(ctx, ws, needs)
	require.Equal(t, 6, inner.calls, "failures are not cached")
}

func TestApproveTimeCheckIsNeverServedFromTheCache(t *testing.T) {
	// Approving is wired to the live check, requesting to the cached wrapper.
	live := &countingAccess{}
	ctx, ws := context.Background(), uuid.New()
	approvals := &AssistantApprovals{access: live}
	for i := 0; i < 2; i++ {
		preflightGitHub(ctx, approvals.access, ws, "merge_pull_request")
	}
	require.Equal(t, 2, live.calls)

	cached := NewCachedGitHubAccess(live)
	tools := &AssistantTools{access: cached}
	for i := 0; i < 2; i++ {
		preflightGitHub(ctx, tools.access, ws, "merge_pull_request")
	}
	require.Equal(t, 3, live.calls, "requests share one cached check")
}

func TestOnlyToolsThatActAtOnceArePreflightedBeforeRunning(t *testing.T) {
	access := &countingAccess{}
	svc := &ConversationService{tools: &AssistantTools{access: access}}
	ctx, ws := context.Background(), uuid.New()

	svc.readOnlyGitHubGap(ctx, ws, assistantTool{name: "merge_pull_request", approval: true})
	require.Zero(t, access.calls, "approval tools are checked when the approval is requested")
	svc.readOnlyGitHubGap(ctx, ws, assistantTool{name: "get_workflow_run"})
	require.Equal(t, 1, access.calls)
	svc.readOnlyGitHubGap(ctx, ws, assistantTool{name: "get_issue"})
	require.Equal(t, 1, access.calls, "tools without GitHub needs make no GitHub call")
}

func TestGitHub403BecomesAnActionableMissingPermission(t *testing.T) {
	denied := errors.New(`GitHub API error 403: {"message":"Resource not accessible by integration"}`)
	gap := preflightGitHub(context.Background(), &countingAccess{err: denied}, uuid.New(), "create_release")
	require.NotNil(t, gap)
	require.Equal(t, []domain.GitHubPermissionNeed{{Name: "contents", Level: "write"}}, gap.Missing)
	require.Contains(t, gap.Message, "contents: write")

	require.True(t, isGitHubPermissionDenied(denied))
	require.False(t, isGitHubPermissionDenied(errors.New("GitHub API error 500: boom")))
	require.False(t, isGitHubPermissionDenied(nil))
	require.Nil(t, preflightGitHub(context.Background(), &countingAccess{err: errors.New("timeout")}, uuid.New(), "create_release"))
}

func TestAuditNamesActorActionTargetAndOutcome(t *testing.T) {
	merge := &domain.ConversationApproval{Action: "merge_pull_request", Payload: json.RawMessage(`{"identifier":"KUA-1","repository":"o/r","pull_request":7}`)}
	require.Equal(t, "o/r#7", approvalTarget(merge))
	require.Equal(t, "KUA-2", approvalTarget(&domain.ConversationApproval{Payload: json.RawMessage(`{"identifier":"KUA-2"}`)}))
	require.Equal(t, "instance 0.2.8 → 0.3.0", approvalTarget(&domain.ConversationApproval{Action: "upgrade_instance", Payload: json.RawMessage(`{"current_version":"0.2.8","latest_version":"0.3.0"}`)}))
	require.Equal(t, "upgrade_instance", approvalTarget(&domain.ConversationApproval{Action: "upgrade_instance", Payload: json.RawMessage(`{}`)}))
}
