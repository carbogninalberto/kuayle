package service

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	gh "github.com/kuayle/kuayle-backend/pkg/github"
	"github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/require"
)

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
	withPolicies(t)
	gap := newGitHubAccessGap([]domain.GitHubPermissionNeed{{Name: "contents", Level: "write"}}, "https://github.com/settings/apps/x/permissions")
	access := &accessFake{gap: gap}
	require.Equal(t, gap, preflightGitHub(context.Background(), access, uuid.New(), "test_release"))
	require.Equal(t, map[string]string{"contents": "write"}, access.asked)

	require.Nil(t, preflightGitHub(context.Background(), access, uuid.New(), "run_check"), "tools without GitHub needs are not checked")
	require.Nil(t, preflightGitHub(context.Background(), nil, uuid.New(), "test_release"))
	unverified := preflightGitHub(context.Background(), &accessFake{err: errors.New("down")}, uuid.New(), "test_release")
	require.NotNil(t, unverified, "an outage is shown, not silent")
	require.True(t, unverified.Unverified)
	require.Empty(t, unverified.Missing, "an outage is not reported as a missing permission")
	require.Contains(t, unverified.Message, "Could not verify")
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
	withPolicies(t)
	ctx, ws, user := context.Background(), uuid.New(), uuid.New()
	for role, allowed := range map[string]bool{domain.RoleOwner: true, domain.RoleAdmin: true, domain.RoleMember: false, "": false} {
		refusal, err := authorizeAssistantAction(ctx, memberFake{role}, nil, ws, user, "test_release", uuid.Nil)
		require.NoError(t, err)
		require.Equal(t, allowed, refusal == "", "role %q", role)
	}
	refusal, err := authorizeAssistantAction(ctx, memberFake{domain.RoleMember}, nil, ws, user, "run_check", uuid.Nil)
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
	inner := &countingAccess{}
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

	inner.gap = newGitHubAccessGap([]domain.GitHubPermissionNeed{{Name: "contents", Level: "write"}}, "")
	now = now.Add(time.Hour)
	for i := 0; i < 2; i++ {
		gap, _ := cache.PermissionGap(ctx, ws, needs)
		require.NotNil(t, gap)
	}
	require.Equal(t, 8, inner.calls, "a missing permission is rechecked every time so a grant is seen at once")
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
	withPolicies(t)
	access := &countingAccess{}
	svc := &ConversationService{tools: &AssistantTools{access: access}}
	ctx, ws := context.Background(), uuid.New()

	svc.readOnlyGitHubGap(ctx, ws, assistantTool{name: "merge_pull_request", approval: true})
	require.Zero(t, access.calls, "approval tools are checked when the approval is requested")
	svc.readOnlyGitHubGap(ctx, ws, assistantTool{name: "test_workflow"})
	require.Equal(t, 1, access.calls)
	svc.readOnlyGitHubGap(ctx, ws, assistantTool{name: "get_issue"})
	require.Equal(t, 1, access.calls, "tools without GitHub needs make no GitHub call")
}

func TestGitHub403BecomesAnActionableMissingPermission(t *testing.T) {
	withPolicies(t)
	denied := fmt.Errorf("merging: %w", &gh.APIError{Status: 403, Message: `{"message":"Resource not accessible by integration"}`})
	gap := preflightGitHub(context.Background(), &countingAccess{err: denied}, uuid.New(), "test_release")
	require.NotNil(t, gap)
	require.Equal(t, []domain.GitHubPermissionNeed{{Name: "contents", Level: "write"}}, gap.Missing)
	require.Contains(t, gap.Message, "contents: write")

	require.True(t, isGitHubPermissionDenied(denied))
	require.False(t, isGitHubPermissionDenied(&gh.APIError{Status: 500, Message: "Resource not accessible by integration"}))
	require.False(t, isGitHubPermissionDenied(&gh.APIError{Status: 403, Message: "rate limited"}))
	require.False(t, isGitHubPermissionDenied(nil))
	require.False(t, isGitHubPermissionDenied(errors.New("GitHub API error 403: Resource not accessible by integration")), "only a typed 403 counts")
}

func TestAuditNamesActorActionTargetAndOutcome(t *testing.T) {
	merge := &domain.ConversationApproval{Action: "merge_pull_request", Payload: json.RawMessage(`{"identifier":"KUA-1","repository":"o/r","pull_request":7}`)}
	require.Equal(t, "o/r#7", approvalTarget(merge))
	require.Equal(t, "KUA-2", approvalTarget(&domain.ConversationApproval{Payload: json.RawMessage(`{"identifier":"KUA-2"}`)}))
	require.Equal(t, "instance 0.2.8 → 0.3.0", approvalTarget(&domain.ConversationApproval{Action: "upgrade_instance", Payload: json.RawMessage(`{"current_version":"0.2.8","latest_version":"0.3.0"}`)}))
	require.Equal(t, "upgrade_instance", approvalTarget(&domain.ConversationApproval{Action: "upgrade_instance", Payload: json.RawMessage(`{}`)}))
}

// withPolicies registers synthetic policies for the test, for the admin-only
// and read-only paths no registered tool uses yet.
func withPolicies(t *testing.T) {
	t.Helper()
	assistantActionPolicies["test_release"] = assistantActionPolicy{github: map[string]string{"contents": "write"}, role: roleWorkspaceAdmin}
	assistantActionPolicies["test_workflow"] = assistantActionPolicy{github: map[string]string{"actions": "read"}}
	t.Cleanup(func() {
		delete(assistantActionPolicies, "test_release")
		delete(assistantActionPolicies, "test_workflow")
	})
}

func TestPolicyTableMatchesRegisteredTools(t *testing.T) {
	registered := map[string]bool{"run_custom_tool": true} // custom tools are named by their authors
	for _, tool := range assistantTools() {
		registered[tool.name] = true
		policy, ok := assistantActionPolicies[tool.name]
		if tool.approval {
			require.True(t, ok, "%s needs approval but has no policy", tool.name)
		}
		if tool.pulls {
			// Every tool that calls GitHub for a pull request declares what it uses,
			// so the preflight can name a missing permission before GitHub refuses.
			require.True(t, ok, "%s calls GitHub but has no policy", tool.name)
			require.NotEmpty(t, policy.github, "%s calls GitHub but declares no GitHub permission", tool.name)
		}
		if tool.releases {
			require.True(t, ok, "%s calls GitHub but has no policy", tool.name)
			require.NotEmpty(t, policy.github, "%s calls GitHub but declares no GitHub permission", tool.name)
		}
		if tool.pulls && tool.approval {
			require.Equal(t, roleAdminOrCreator, policy.role, "%s changes a pull request, which needs what merging needs", tool.name)
		}
	}
	manifest := GitHubAppPermissions()
	for name, policy := range assistantActionPolicies {
		require.True(t, registered[name], "policy %q names no registered tool", name)
		for permission, level := range policy.github {
			// The manifest must request everything a tool needs, or no installation
			// created from it could ever run the tool.
			require.GreaterOrEqual(t, githubPermissionRank(manifest[permission]), githubPermissionRank(level),
				"%s needs %s: %s, which the GitHub App manifest does not request", name, permission, level)
		}
		if policy.role == roleAdminOrCreator {
			require.True(t, ownerScoped(name))
		}
	}
	// Actions is requested only because get_workflow_run uses it.
	used := false
	for _, policy := range assistantActionPolicies {
		_, ok := policy.github["actions"]
		used = used || ok
	}
	require.True(t, used, "the manifest requests Actions for get_workflow_run")
}

func TestPullRequestToolsDeclareWhatTheyCallOnGitHub(t *testing.T) {
	for name, want := range map[string]map[string]string{
		"get_pull_request":           {"pull_requests": "read", "contents": "read"},
		"get_pull_request_checks":    {"pull_requests": "read", "checks": "read", "statuses": "read"},
		"list_pull_request_comments": {"pull_requests": "read"},
		"reply_to_review_comment":    {"pull_requests": "write"},
		"resolve_review_thread":      {"pull_requests": "write"},
		"update_pull_request_branch": {"pull_requests": "write", "contents": "write"},
		"mark_pull_request_ready":    {"pull_requests": "write"},
	} {
		require.Equal(t, want, assistantActionPolicies[name].github, name)
	}
}

func TestPullRequestChangesShareTheMergeRole(t *testing.T) {
	ctx, ws, user, other := context.Background(), uuid.New(), uuid.New(), uuid.New()
	for _, action := range []string{"reply_to_review_comment", "resolve_review_thread", "update_pull_request_branch", "mark_pull_request_ready"} {
		check := func(role string, owner uuid.UUID) string {
			refusal, err := authorizeAssistantAction(ctx, memberFake{role}, nil, ws, user, action, owner)
			require.NoError(t, err)
			return refusal
		}
		require.Empty(t, check(domain.RoleAdmin, other), action)
		require.Empty(t, check(domain.RoleOwner, other), action)
		require.Empty(t, check(domain.RoleMember, user), "%s: the task's creator may", action)
		require.Contains(t, check(domain.RoleMember, other), "admins or the task's creator", action)
		require.Contains(t, check(domain.RoleGuest, user), "no longer have access", "%s: a guest is refused even as creator", action)
		require.Contains(t, check("", user), "no longer have access", action)
		require.True(t, ownerScoped(action), "%s is checked where the task's creator is known", action)
	}
}

func TestCustomToolRequestsGoThroughTheRoleTable(t *testing.T) {
	saved := assistantActionPolicies["run_custom_tool"]
	assistantActionPolicies["run_custom_tool"] = assistantActionPolicy{role: roleWorkspaceAdmin, refusal: "only admins may run custom tools"}
	defer func() { assistantActionPolicies["run_custom_tool"] = saved }()
	svc := &ConversationService{tools: &AssistantTools{members: memberFake{domain.RoleMember}}}
	a := domain.ConversationAttempt{Conversation: domain.Conversation{ID: uuid.New(), WorkspaceID: uuid.New(), CreatorID: uuid.New()}}
	events := auditEvents(t, func() {
		// Refused before the tool is even looked up (the fake has no tool store).
		_, _, err := svc.runCustomTool(context.Background(), a, uuid.New(), domain.CustomToolPrefix+"run_e2e", `{}`)
		require.ErrorContains(t, err, "only admins may run custom tools")
	})
	require.NotEmpty(t, events)
	last := events[len(events)-1]
	require.Equal(t, "assistant.request_refused", last.Data["action"])
	require.Equal(t, "run_custom_tool", last.Data["tool"])
}

type rewriteTransport struct{ calls *int }

func (r rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	*r.calls++
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Request: req,
		Body: io.NopCloser(strings.NewReader(`{"id":1,"slug":"kuayle","permissions":{"contents":"write"}}`))}, nil
}

func TestAppIsReadFromGitHubOnceAcrossPermissionChecks(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	pemKey := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	client, err := gh.NewClient(1, pemKey)
	require.NoError(t, err)
	calls := 0
	client.UseHTTPClient(&http.Client{Transport: rewriteTransport{&calls}})

	svc := NewGitHubService(nil, nil, nil, nil, nil, nil, nil, "", "", nil)
	require.NotNil(t, svc.apps, "the constructor must enable the app cache")
	ws := uuid.New()
	for i := 0; i < 3; i++ {
		app, err := svc.appFor(context.Background(), ws, client)
		require.NoError(t, err)
		require.Equal(t, "kuayle", app.Slug)
	}
	require.Equal(t, 1, calls)
	_, err = svc.appFor(context.Background(), uuid.New(), client)
	require.NoError(t, err)
	require.Equal(t, 2, calls, "each workspace has its own app")
}

func TestUnverifiedAccessNeverBlocksAndDeniedNamesOnlyWhatIsMissing(t *testing.T) {
	withPolicies(t)
	ctx, ws := context.Background(), uuid.New()
	svc := &ConversationService{tools: &AssistantTools{access: &countingAccess{err: errors.New("down")}}}
	require.Nil(t, svc.readOnlyGitHubGap(ctx, ws, assistantTool{name: "test_workflow"}), "an outage does not stop a tool")

	assistantActionPolicies["test_multi"] = assistantActionPolicy{github: map[string]string{"contents": "write", "pull_requests": "write"}}
	defer delete(assistantActionPolicies, "test_multi")
	live := newGitHubAccessGap([]domain.GitHubPermissionNeed{{Name: "contents", Level: "write"}}, "https://github.com/settings/apps/x/permissions")
	gap := deniedGap(ctx, &countingAccess{gap: live}, ws, "test_multi")
	require.Equal(t, live.Missing, gap.Missing, "permissions already granted are not named")
	require.Equal(t, live.SettingsURL, gap.SettingsURL)

	fallback := deniedGap(ctx, &countingAccess{err: errors.New("down")}, ws, "test_multi")
	require.Len(t, fallback.Missing, 2, "without a live answer it lists what the action needs")
}

func TestRealToolPoliciesEnforceTheirRoles(t *testing.T) {
	ctx, ws, user, other := context.Background(), uuid.New(), uuid.New(), uuid.New()
	check := func(members assistantMemberRepo, system *AssistantSystem, action string, owner uuid.UUID) string {
		refusal, err := authorizeAssistantAction(ctx, members, system, ws, user, action, owner)
		require.NoError(t, err)
		return refusal
	}

	// merge_pull_request: workspace admins and owners, or the task's creator.
	require.Empty(t, check(memberFake{domain.RoleAdmin}, nil, "merge_pull_request", other))
	require.Empty(t, check(memberFake{domain.RoleOwner}, nil, "merge_pull_request", other))
	require.Empty(t, check(memberFake{domain.RoleMember}, nil, "merge_pull_request", user), "the task's creator may merge")
	require.Contains(t, check(memberFake{domain.RoleMember}, nil, "merge_pull_request", other), "admins or the task's creator")
	require.NotEmpty(t, check(memberFake{domain.RoleMember}, nil, "merge_pull_request", uuid.Nil), "an unknown creator admits no one by ownership")
	require.NotEmpty(t, check(memberFake{""}, nil, "merge_pull_request", user), "a creator who left the workspace is refused")

	// upgrade_instance: instance administrators only, whatever the workspace role.
	admin := NewAssistantSystem(&updaterFake{}, func(id uuid.UUID) bool { return id == user }, "0.2.8")
	notAdmin := NewAssistantSystem(&updaterFake{}, func(uuid.UUID) bool { return false }, "0.2.8")
	require.Empty(t, check(memberFake{domain.RoleMember}, admin, "upgrade_instance", uuid.Nil))
	require.Contains(t, check(memberFake{domain.RoleOwner}, notAdmin, "upgrade_instance", uuid.Nil), "only an instance administrator can upgrade")
	require.NotEmpty(t, check(memberFake{domain.RoleOwner}, nil, "upgrade_instance", uuid.Nil))

	// The merge check is made where the creator is known.
	require.True(t, ownerScoped("merge_pull_request"))
	require.False(t, ownerScoped("upgrade_instance"))
}

func TestPreflightUsesTheRealMergePolicy(t *testing.T) {
	access := &accessFake{gap: newGitHubAccessGap([]domain.GitHubPermissionNeed{{Name: "contents", Level: "write"}}, "https://github.com/settings/apps/x/permissions")}
	gap := preflightGitHub(context.Background(), access, uuid.New(), "merge_pull_request")
	require.Equal(t, access.gap, gap)
	require.Equal(t, map[string]string{"pull_requests": "write", "contents": "write"}, access.asked)
}

func auditEvents(t *testing.T, run func()) []*logrus.Entry {
	t.Helper()
	hook := logtest.NewGlobal()
	defer logrus.StandardLogger().ReplaceHooks(make(logrus.LevelHooks))
	run()
	return hook.AllEntries()
}

func TestApprovalOutcomeIsAuditedExactlyOnce(t *testing.T) {
	ws := uuid.New()
	approval := &domain.ConversationApproval{ID: uuid.New(), ActorID: uuid.New(), Action: "merge_pull_request", Payload: json.RawMessage(`{"repository":"o/r","pull_request":7}`)}
	reason := "GitHub refused"
	done := &domain.ConversationApproval{Status: "approved"}
	failed := &domain.ConversationApproval{Status: "failed", Error: &reason}

	entries := auditEvents(t, func() { auditApprovalOutcome(ws, approval, done, true, nil) })
	require.Len(t, entries, 1)
	require.Equal(t, "assistant.action_executed", entries[0].Data["action"])
	require.Equal(t, "o/r#7", entries[0].Data["target"])

	entries = auditEvents(t, func() { auditApprovalOutcome(ws, approval, failed, true, nil) })
	require.Len(t, entries, 1)
	require.Equal(t, "assistant.action_failed", entries[0].Data["action"])
	require.Equal(t, "failed", entries[0].Data["outcome"])

	entries = auditEvents(t, func() { auditApprovalOutcome(ws, approval, done, false, nil) })
	require.Empty(t, entries, "a retry that finds the approval already finished records nothing")

	entries = auditEvents(t, func() { auditApprovalOutcome(ws, approval, nil, false, errors.New("database down")) })
	require.Len(t, entries, 1, "an error that leaves the claim in place is recorded")
	require.Equal(t, "assistant.action_error", entries[0].Data["action"])
	require.Equal(t, "database down", entries[0].Data["error"])
}
