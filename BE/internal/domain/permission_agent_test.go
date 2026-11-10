package domain

import "testing"

// Agent task and workflow reads are for workspace members, never guests, and
// tokens must opt in to them with their own scopes.
func TestAgentReadPermissionsExcludeGuests(t *testing.T) {
	for _, perm := range []string{PermAgentTasksRead, PermWorkflowsRead} {
		if !IsValidScope(perm) {
			t.Fatalf("%s is not a valid token scope", perm)
		}
		for _, role := range []string{RoleOwner, RoleAdmin, RoleMember} {
			if !HasPermission(role, perm) {
				t.Errorf("%s lacks %s", role, perm)
			}
		}
		if HasPermission(RoleGuest, perm) {
			t.Errorf("guest has %s", perm)
		}
	}
}
