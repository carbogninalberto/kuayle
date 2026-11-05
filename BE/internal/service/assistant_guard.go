package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/pkg/audit"
	gh "github.com/kuayle/kuayle-backend/pkg/github"
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
	// roleAdminOrCreator admits workspace owners and admins, and the creator of
	// the task acted on (merging a pull request). The creator is known only once
	// the target is loaded, so the action checks it there with the same gate.
	roleAdminOrCreator
)

// assistantActionPolicy is the single declaration of what an action needs: the
// GitHub App permissions it uses and who may do it. Every tool that needs
// approval must have one (a test enforces it), and requesting and approving go
// through the same checks, so a tool cannot skip authorization or the audit log.
type assistantActionPolicy struct {
	github map[string]string
	role   assistantRole
	// refusal is why the role check refuses, worded for a tool failure.
	refusal string
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
	"merge_pull_request":   {github: map[string]string{"pull_requests": "write", "contents": "write"}, role: roleAdminOrCreator, refusal: "only workspace admins or the task's creator can merge"},
	"upgrade_instance":     {role: roleInstanceAdmin, refusal: "only an instance administrator can upgrade this instance; ask one to upgrade it"},
	// Deferred to the issues that add the tools (KUA-88 releases, KUA-87 pull
	// request tools): an entry, including contents/actions permissions and the
	// admin-only role for tags and releases, is added with each tool; a test keeps this
	// table and the registered tools in step.
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

// preflightGitHub returns the gap for an action, or nil. When GitHub cannot be
// asked the gap is Unverified: shown as a warning, never a reason to refuse.
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
		return &domain.GitHubAccessGap{Unverified: true, Message: "Could not verify the GitHub App's permissions because GitHub did not answer; the action may still fail for lack of a permission."}
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
// owner is the user who owns the thing acted on, where the policy admits them
// (the creator of a task being merged); uuid.Nil when not known, which admits
// no one by ownership. It returns the reason for a refusal, or "".
func authorizeAssistantAction(ctx context.Context, members assistantMemberRepo, system *AssistantSystem, ws, actor uuid.UUID, action string, owner uuid.UUID) (string, error) {
	policy := assistantActionPolicies[action]
	refusal := policy.refusal
	if refusal == "" {
		refusal = "only workspace owners and admins can do this"
	}
	switch policy.role {
	case roleWorkspaceAdmin, roleAdminOrCreator:
		if members == nil {
			return refusal, nil
		}
		member, err := members.GetMember(ctx, ws, actor)
		if err != nil {
			return "", err
		}
		if member == nil {
			return refusal, nil
		}
		admin := member.Role == domain.RoleOwner || member.Role == domain.RoleAdmin
		creator := policy.role == roleAdminOrCreator && owner != uuid.Nil && owner == actor
		if !admin && !creator {
			return refusal, nil
		}
	case roleInstanceAdmin:
		if system == nil || !system.isAdmin(actor) {
			return refusal, nil
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

// isGitHubPermissionDenied recognizes GitHub's 403 for a call the
// installation's permissions do not cover.
func isGitHubPermissionDenied(err error) bool {
	var api *gh.APIError
	return errors.As(err, &api) && api.Status == http.StatusForbidden &&
		strings.Contains(api.Message, "Resource not accessible by integration")
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
	if gap != nil {
		// A missing permission is not remembered: the admin may grant it at any
		// moment and the next call should see that.
		return gap, nil
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
	if gap := preflightGitHub(ctx, s.tools.access, ws, tool.name); gap != nil && !gap.Unverified {
		return gap
	}
	return nil
}

// deniedGap words GitHub's refusal of an action. It asks the permission check
// again so only the permissions actually missing are named, with the settings
// link; if that cannot say, it lists what the action needs.
func deniedGap(ctx context.Context, access githubAccess, ws uuid.UUID, action string) *domain.GitHubAccessGap {
	if policy := assistantActionPolicies[action]; access != nil && len(policy.github) > 0 {
		if gap, err := access.PermissionGap(ctx, ws, policy.github); err == nil && gap != nil && !gap.Unverified && len(gap.Missing) > 0 {
			return gap
		}
	}
	return permissionDeniedGap(action)
}

// ownerScoped reports whether the action's role depends on who owns its
// target, so the gate runs where the target is loaded instead of up front.
func ownerScoped(action string) bool {
	return assistantActionPolicies[action].role == roleAdminOrCreator
}
