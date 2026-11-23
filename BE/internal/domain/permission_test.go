package domain

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPermissionCatalogCoversEveryOwnerPermission(t *testing.T) {
	var codes []string
	for _, group := range PermissionGroups {
		require.NotEmpty(t, group.Area)
		for _, permission := range group.Permissions {
			require.NotEmpty(t, strings.TrimSpace(permission.Hint), permission.Code)
			codes = append(codes, permission.Code)
		}
	}
	require.ElementsMatch(t, RolePermissions[RoleOwner], codes)
}

func TestWorkspaceTransferPermissionIsLimitedToOwnerAndAdmin(t *testing.T) {
	require.True(t, HasPermission(RoleOwner, PermWorkspaceTransfer))
	require.True(t, HasPermission(RoleAdmin, PermWorkspaceTransfer))
	require.False(t, HasPermission(RoleMember, PermWorkspaceTransfer))
	require.False(t, HasPermission(RoleGuest, PermWorkspaceTransfer))
}

// Every role may read wikis it can see; guests stay read-only.
func TestWikiPermissions(t *testing.T) {
	for _, role := range []string{RoleOwner, RoleAdmin, RoleMember, RoleGuest} {
		require.True(t, HasPermission(role, PermWikiRead), role)
	}
	for _, role := range []string{RoleOwner, RoleAdmin, RoleMember} {
		require.True(t, HasPermission(role, PermWikiWrite), role)
	}
	require.False(t, HasPermission(RoleGuest, PermWikiWrite))
	require.True(t, IsValidScope(PermWikiRead))
	require.True(t, IsValidScope(PermWikiWrite))
}
