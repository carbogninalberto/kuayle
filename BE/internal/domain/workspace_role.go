package domain

import (
	"slices"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

type WorkspaceRole struct {
	Key             string         `db:"key" json:"key"`
	Name            string         `db:"name" json:"name"`
	Description     string         `db:"description" json:"description"`
	Permissions     pq.StringArray `db:"permissions" json:"permissions"`
	MemberCount     int            `db:"member_count" json:"member_count"`
	TeamAssignments int            `db:"team_assignments" json:"team_assignments"`
}

// RoleAccess is loaded afresh for a single workspace request.
type RoleAccess struct {
	Role        string
	Permissions []string
	Teams       map[uuid.UUID][]string
}

func (a RoleAccess) Allows(team uuid.UUID, permission string) bool {
	if permissions, ok := a.Teams[team]; ok {
		return slices.Contains(permissions, permission)
	}
	return slices.Contains(a.Permissions, permission)
}

// AllowsEverywhere protects operations spanning several teams when their
// queries cannot apply a separate permission predicate to each resource.
func (a RoleAccess) AllowsEverywhere(permission string) bool {
	if !a.Allows(uuid.Nil, permission) {
		return false
	}
	for _, permissions := range a.Teams {
		if !slices.Contains(permissions, permission) {
			return false
		}
	}
	return true
}

func BuiltinRole(key string) bool {
	_, ok := RolePermissions[key]
	return ok
}

type TeamRole struct {
	TeamID uuid.UUID `db:"team_id" json:"team_id"`
	Role   string    `db:"role_key" json:"role"`
}
