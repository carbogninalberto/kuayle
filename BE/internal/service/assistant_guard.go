package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

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
