package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

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
