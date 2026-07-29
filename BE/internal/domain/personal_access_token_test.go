package domain

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateToken(t *testing.T) {
	plaintext, hash, prefix, err := GenerateToken()
	require.NoError(t, err)

	assert.True(t, strings.HasPrefix(plaintext, TokenPrefix))
	assert.Len(t, plaintext, len(TokenPrefix)+40)
	assert.Equal(t, HashToken(plaintext), hash)
	assert.Equal(t, plaintext[:len(TokenPrefix)+8], prefix)

	other, _, _, err := GenerateToken()
	require.NoError(t, err)
	assert.NotEqual(t, plaintext, other)
}

func TestHashToken(t *testing.T) {
	// SHA-256 hex of "test", same convention as refresh tokens.
	assert.Equal(t, "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08", HashToken("test"))
}

func TestIsValidScope(t *testing.T) {
	valid := []string{
		"issues:read", "comments:read", "projects:read", "cycles:read",
		"labels:read", "teams:read", "members:read", "templates:read",
		"views:read", "analytics:read", "notifications:read",
		"workspaces:read", "account:read",
		"issue:create", "issue:update", "issue:delete_own",
		"project:manage", "label:manage", "team:manage", "member:invite",
		"workspace:manage",
		"dev_machine:read", "dev_machine:create", "dev_machine:manage", "dev_machine:admin",
	}
	for _, scope := range valid {
		assert.True(t, IsValidScope(scope), scope)
	}

	invalid := []string{
		"", "admin", "read", "issue:read", // renamed to issues:read
		"issue:delete",   // exists as RBAC code but is not annotated on any route
		"cycle:manage",   // not annotated on any route
		"view:manage",    // not annotated on any route
		"comment:create", // comments reuse issue:create
	}
	for _, scope := range invalid {
		assert.False(t, IsValidScope(scope), scope)
	}
}

func TestPersonalAccessTokenActive(t *testing.T) {
	now := time.Now()
	past := now.Add(-time.Minute)
	future := now.Add(time.Minute)

	tests := []struct {
		name  string
		token PersonalAccessToken
		want  bool
	}{
		{"no expiry, not revoked", PersonalAccessToken{}, true},
		{"expires in future", PersonalAccessToken{ExpiresAt: &future}, true},
		{"expired", PersonalAccessToken{ExpiresAt: &past}, false},
		{"revoked", PersonalAccessToken{RevokedAt: &now}, false},
		{"revoked and not expired", PersonalAccessToken{RevokedAt: &now, ExpiresAt: &future}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.token.ID = uuid.New()
			assert.Equal(t, tt.want, tt.token.Active())
		})
	}
}

func TestReadPermissionsGrantedToAllRoles(t *testing.T) {
	// Annotating read routes must not change JWT authorization: every role
	// holds every read code.
	for _, role := range []string{RoleOwner, RoleAdmin, RoleMember, RoleGuest} {
		for _, perm := range readPermissions {
			assert.True(t, HasPermission(role, perm), "%s should hold %s", role, perm)
		}
	}
	assert.False(t, HasPermission(RoleGuest, PermIssueCreate))
}
