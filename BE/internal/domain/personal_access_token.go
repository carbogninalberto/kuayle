package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

const (
	// TokenPrefix identifies personal access tokens in the Authorization
	// header and enables secret-scanning tools to spot leaked tokens.
	TokenPrefix = "kuayle_pat_"

	tokenRandomLength = 40
	tokenAlphabet     = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

	// tokenPrefixVisible is how many random characters are stored in
	// token_prefix so users can tell tokens apart in lists and logs.
	tokenPrefixVisible = 8
)

// PersonalAccessToken is a long-lived credential for non-interactive API
// access. Its effective permissions are the intersection of Scopes (the
// ceiling) and the owner's workspace RBAC role (the floor).
type PersonalAccessToken struct {
	ID               uuid.UUID      `json:"id" db:"id"`
	UserID           uuid.UUID      `json:"user_id" db:"user_id"`
	Name             string         `json:"name" db:"name"`
	TokenHash        string         `json:"-" db:"token_hash"`
	TokenPrefix      string         `json:"token_prefix" db:"token_prefix"`
	Scopes           pq.StringArray `json:"scopes" db:"scopes"`
	WorkspaceSlugs   pq.StringArray `json:"workspace_slugs" db:"workspace_slugs"` // nil = all workspaces
	ExpiresAt        *time.Time     `json:"expires_at" db:"expires_at"`           // nil = never expires
	LastUsedAt       *time.Time     `json:"last_used_at" db:"last_used_at"`
	ConnectionLinkID *uuid.UUID     `json:"connection_link_id,omitempty" db:"connection_link_id"`
	CreatedAt        time.Time      `json:"created_at" db:"created_at"`
	RevokedAt        *time.Time     `json:"revoked_at" db:"revoked_at"`
}

// GenerateToken creates a new token. The plaintext is returned once and never
// stored; only its SHA-256 hex hash persists (same convention as refresh
// tokens — a high-entropy random string needs no slow hash).
func GenerateToken() (plaintext, hash, prefix string, err error) {
	random, err := randomString(tokenRandomLength)
	if err != nil {
		return "", "", "", err
	}
	plaintext = TokenPrefix + random
	return plaintext, HashToken(plaintext), TokenPrefix + random[:tokenPrefixVisible], nil
}

// HashToken returns the SHA-256 hex digest of a plaintext token.
func HashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// IsValidScope reports whether scope is a permission code a token may request.
func IsValidScope(scope string) bool {
	return validTokenScopes[scope]
}

// TokenScopes returns every scope a personal access token may request.
func TokenScopes() []string {
	scopes := make([]string, 0, len(validTokenScopes))
	for scope := range validTokenScopes {
		scopes = append(scopes, scope)
	}
	sort.Strings(scopes)
	return scopes
}

// Active reports whether the token can authenticate right now.
func (t *PersonalAccessToken) Active() bool {
	if t.RevokedAt != nil {
		return false
	}
	if t.ExpiresAt != nil && !time.Now().Before(*t.ExpiresAt) {
		return false
	}
	return true
}

// validTokenScopes is the public contract of scopes: the write permission
// codes already enforced by route annotations, plus the *:read codes.
var validTokenScopes = map[string]bool{
	// Write codes (as annotated on routes)
	PermWorkspaceManage:    true,
	PermWorkspaceTransfer:  true,
	PermTeamManage:         true,
	PermIssueCreate:        true,
	PermIssueUpdate:        true,
	PermIssueDelete:        true,
	PermIssueDeleteOwn:     true,
	PermProjectManage:      true,
	PermLabelManage:        true,
	PermMemberInvite:       true,
	PermCycleManage:        true,
	PermViewManage:         true,
	PermAccountWrite:       true,
	PermNotificationsWrite: true,
	PermDevMachineRead:     true,
	PermDevMachineCreate:   true,
	PermDevMachineManage:   true,
	PermDevMachineAdmin:    true,
	PermWikiWrite:          true,
	// Read codes
	PermIssuesRead:        true,
	PermCommentsRead:      true,
	PermProjectsRead:      true,
	PermCyclesRead:        true,
	PermLabelsRead:        true,
	PermTeamsRead:         true,
	PermMembersRead:       true,
	PermTemplatesRead:     true,
	PermViewsRead:         true,
	PermAnalyticsRead:     true,
	PermNotificationsRead: true,
	PermWorkspacesRead:    true,
	PermAccountRead:       true,
	PermAssetsRead:        true,
	PermWikiRead:          true,
	// Member-only read codes (not held by guests)
	PermAgentTasksRead: true,
	PermWorkflowsRead:  true,
}
