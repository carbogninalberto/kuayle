package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/pkg/audit"
	log "github.com/sirupsen/logrus"
)

// assistantRole is who may request or approve an action.
type assistantRole int

const (
	roleMember assistantRole = iota
	// roleWorkspaceAdmin is a workspace owner or admin.
	roleWorkspaceAdmin
	// roleInstanceAdmin is an administrator of the whole instance.
	roleInstanceAdmin
	// roleCustom means the action's own code decides, with the same rigor
	// (merging also admits the task's creator).
	roleCustom
)

// assistantActionPolicy is the single declaration of what an action needs: the
// GitHub App permissions it uses and who may do it. Every tool that needs
// approval must have one (a test enforces it), and requesting and approving go
// through the same checks, so a tool cannot skip authorization or the audit log.
type assistantActionPolicy struct {
	github map[string]string
	role   assistantRole
}

var assistantActionPolicies = map[string]assistantActionPolicy{
	"delegate_issue":       {},
	"accept_agent_work":    {},
	"request_agent_review": {},
	"follow_up_task":       {},
	"start_preview":        {},
	"expose_port":          {},
	"run_check":            {},
	"run_custom_tool":      {},
	"merge_pull_request":   {github: map[string]string{"pull_requests": "write", "contents": "write"}, role: roleCustom},
	"upgrade_instance":     {role: roleCustom},
	// Tools that change GitHub beyond pull requests are admin-only.
	"create_tag":                 {github: map[string]string{"contents": "write"}, role: roleWorkspaceAdmin},
	"create_release":             {github: map[string]string{"contents": "write"}, role: roleWorkspaceAdmin},
	"update_pull_request_branch": {github: map[string]string{"pull_requests": "write", "contents": "write"}},
	"mark_pull_request_ready":    {github: map[string]string{"pull_requests": "write"}},
	"comment_pull_request":       {github: map[string]string{"pull_requests": "write"}},
	"reply_pull_request_comment": {github: map[string]string{"pull_requests": "write"}},
	// Read-only tools only need the preflight.
	"get_pull_request":        {github: map[string]string{"pull_requests": "read"}},
	"get_pull_request_checks": {github: map[string]string{"pull_requests": "read", "checks": "read"}},
	"list_workflow_runs":      {github: map[string]string{"actions": "read"}},
	"get_workflow_run":        {github: map[string]string{"actions": "read"}},
}

// githubAccess reports which of the needed permissions the workspace's GitHub
// App installation lacks. A nil gap means nothing is missing or it is unknown.
type githubAccess interface {
	PermissionGap(ctx context.Context, ws uuid.UUID, needs map[string]string) (*domain.GitHubAccessGap, error)
}

// SetGitHubAccess enables the GitHub permission preflight.
func (t *AssistantTools) SetGitHubAccess(access githubAccess) { t.access = access }

// SetGitHubAccess enables the GitHub permission check when approving.
func (s *AssistantApprovals) SetGitHubAccess(access githubAccess) { s.access = access }

// preflightGitHub returns the gap for an action, or nil. Failing to ask GitHub
// is not a gap: the action then fails with GitHub's own error.
func preflightGitHub(ctx context.Context, access githubAccess, ws uuid.UUID, action string) *domain.GitHubAccessGap {
	policy, ok := assistantActionPolicies[action]
	if !ok || len(policy.github) == 0 || access == nil {
		return nil
	}
	gap, err := access.PermissionGap(ctx, ws, policy.github)
	if err != nil {
		if isGitHubPermissionDenied(err) {
			return permissionDeniedGap(action)
		}
		log.WithFields(log.Fields{"workspace_id": ws, "action": action}).WithError(err).Warn("GitHub permission preflight failed")
		return nil
	}
	return gap
}

// newGitHubAccessGap words a gap so the admin knows exactly what to grant.
func newGitHubAccessGap(missing []domain.GitHubPermissionNeed, settingsURL string) *domain.GitHubAccessGap {
	if len(missing) == 0 {
		return nil
	}
	sort.Slice(missing, func(i, j int) bool { return missing[i].Name < missing[j].Name })
	parts := make([]string, len(missing))
	for i, m := range missing {
		parts[i] = fmt.Sprintf("%s: %s", m.Name, m.Level)
	}
	message := "The GitHub App lacks permissions this needs (" + strings.Join(parts, ", ") + "). A GitHub App admin must grant them"
	if settingsURL != "" {
		message += " at " + settingsURL
	}
	message += ", and the installation must accept the new permissions."
	return &domain.GitHubAccessGap{Missing: missing, SettingsURL: settingsURL, Message: message}
}

// authorizeAssistantAction is the one role check for an action, used when it is
// requested and again when it is approved, against the user's current role.
// It returns the reason for a refusal, or "".
func authorizeAssistantAction(ctx context.Context, members assistantMemberRepo, system *AssistantSystem, ws, actor uuid.UUID, action string) (string, error) {
	switch assistantActionPolicies[action].role {
	case roleWorkspaceAdmin:
		if members == nil {
			return "Only workspace owners and admins can do this", nil
		}
		member, err := members.GetMember(ctx, ws, actor)
		if err != nil {
			return "", err
		}
		if member == nil || (member.Role != domain.RoleOwner && member.Role != domain.RoleAdmin) {
			return "Only workspace owners and admins can do this", nil
		}
	case roleInstanceAdmin:
		if system == nil || !system.isAdmin(actor) {
			return "Only an instance administrator can do this", nil
		}
	}
	return "", nil
}

// auditAssistantAction records an event of the assistant's action lifecycle:
// requested, refused, approved, denied, executed or failed.
func auditAssistantAction(event string, actor, ws uuid.UUID, action string, fields map[string]interface{}) {
	merged := map[string]interface{}{"workspace_id": ws.String(), "tool": action}
	for k, v := range fields {
		merged[k] = v
	}
	audit.Log("assistant."+event, actor, merged)
}

// withGitHubAccess adds a gap to an approval's payload so the card shows it.
func withGitHubAccess(payload any, gap *domain.GitHubAccessGap) (any, error) {
	if gap == nil {
		return payload, nil
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	fields := map[string]json.RawMessage{}
	if err = json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	if fields["github_access"], err = json.Marshal(gap); err != nil {
		return nil, err
	}
	return fields, nil
}

// approvalTarget names what an approval acts on for the audit record: the
// pull request or issue it carries, or the instance for an upgrade.
func approvalTarget(approval *domain.ConversationApproval) string {
	var payload struct {
		Identifier     string `json:"identifier"`
		Repository     string `json:"repository"`
		PullRequest    int    `json:"pull_request"`
		LatestVersion  string `json:"latest_version"`
		CurrentVersion string `json:"current_version"`
	}
	_ = json.Unmarshal(approval.Payload, &payload)
	switch {
	case payload.Repository != "" && payload.PullRequest > 0:
		return fmt.Sprintf("%s#%d", payload.Repository, payload.PullRequest)
	case payload.Identifier != "":
		return payload.Identifier
	case payload.LatestVersion != "":
		return "instance " + payload.CurrentVersion + " → " + payload.LatestVersion
	}
	return approval.Action
}

// isGitHubPermissionDenied recognizes GitHub's answer to a call the
// installation's permissions do not cover.
func isGitHubPermissionDenied(err error) bool {
	if err == nil {
		return false
	}
	text := err.Error()
	return strings.Contains(text, "Resource not accessible by integration") ||
		(strings.Contains(text, "403") && strings.Contains(strings.ToLower(text), "permission"))
}

// permissionDeniedGap words a refusal by GitHub using the permissions the
// action is known to need; which one is missing is not in GitHub's answer.
func permissionDeniedGap(action string) *domain.GitHubAccessGap {
	var missing []domain.GitHubPermissionNeed
	for name, level := range assistantActionPolicies[action].github {
		missing = append(missing, domain.GitHubPermissionNeed{Name: name, Level: level})
	}
	if len(missing) == 0 {
		return &domain.GitHubAccessGap{Message: "GitHub refused the request because the GitHub App lacks a permission. Check the app's permissions in its GitHub settings."}
	}
	return newGitHubAccessGap(missing, "")
}

const (
	githubAccessTTL = 45 * time.Second
	githubAppTTL    = 10 * time.Minute
)

// cachedGitHubAccess remembers a workspace's permission check briefly, so a
// run of tool calls does not mint tokens and call GitHub each time. Approving
// uses the underlying check directly and is never cached.
type cachedGitHubAccess struct {
	inner githubAccess
	now   func() time.Time
	mu    sync.Mutex
	items map[string]cachedGap
}

type cachedGap struct {
	gap     *domain.GitHubAccessGap
	expires time.Time
}

// NewCachedGitHubAccess wraps a permission check with a short-lived cache.
func NewCachedGitHubAccess(inner githubAccess) *cachedGitHubAccess {
	return &cachedGitHubAccess{inner: inner, now: time.Now, items: map[string]cachedGap{}}
}

func (c *cachedGitHubAccess) PermissionGap(ctx context.Context, ws uuid.UUID, needs map[string]string) (*domain.GitHubAccessGap, error) {
	names := make([]string, 0, len(needs))
	for name, level := range needs {
		names = append(names, name+"="+level)
	}
	sort.Strings(names)
	key := ws.String() + "|" + strings.Join(names, ",")
	c.mu.Lock()
	if hit, ok := c.items[key]; ok && c.now().Before(hit.expires) {
		c.mu.Unlock()
		return hit.gap, nil
	}
	c.mu.Unlock()
	gap, err := c.inner.PermissionGap(ctx, ws, needs)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.items[key] = cachedGap{gap: gap, expires: c.now().Add(githubAccessTTL)}
	c.mu.Unlock()
	return gap, nil
}

// readOnlyGitHubGap checks the GitHub permissions of a tool that acts at once.
// Approval tools skip it: their check runs when the approval is requested.
func (s *ConversationService) readOnlyGitHubGap(ctx context.Context, ws uuid.UUID, tool assistantTool) *domain.GitHubAccessGap {
	if tool.approval {
		return nil
	}
	return preflightGitHub(ctx, s.tools.access, ws, tool.name)
}
