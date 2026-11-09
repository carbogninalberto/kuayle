package domain

import (
	"time"

	"github.com/google/uuid"
)

// WikiScopeKind names what a wiki belongs to. Every page has exactly one scope.
type WikiScopeKind string

const (
	WikiScopeTeam    WikiScopeKind = "team"
	WikiScopeProject WikiScopeKind = "project"
)

// WikiScope identifies the team or project whose wiki a request addresses.
// Callers build it from the route, never from a request body.
type WikiScope struct {
	Kind WikiScopeKind
	ID   uuid.UUID
}

func TeamWiki(teamID uuid.UUID) WikiScope { return WikiScope{Kind: WikiScopeTeam, ID: teamID} }

func ProjectWiki(projectID uuid.UUID) WikiScope {
	return WikiScope{Kind: WikiScopeProject, ID: projectID}
}

const (
	WikiTitleMaxLength   = 200
	WikiContentMaxBytes  = 512 * 1024
	wikiScopeUnsupported = "unsupported wiki scope"
)

// Valid reports whether the scope names a supported kind and a concrete ID.
func (s WikiScope) Valid() bool {
	return (s.Kind == WikiScopeTeam || s.Kind == WikiScopeProject) && s.ID != uuid.Nil
}

func (s WikiScope) String() string {
	if !s.Valid() {
		return wikiScopeUnsupported
	}
	return string(s.Kind) + ":" + s.ID.String()
}

// WikiPage is one page of a team or project wiki. Version increases on every
// update so concurrent editors cannot silently overwrite each other.
type WikiPage struct {
	ID            uuid.UUID  `db:"id"`
	WorkspaceID   uuid.UUID  `db:"workspace_id"`
	TeamID        *uuid.UUID `db:"team_id"`
	ProjectID     *uuid.UUID `db:"project_id"`
	Title         string     `db:"title"`
	Content       string     `db:"content"`
	Version       int        `db:"version"`
	CreatedBy     *uuid.UUID `db:"created_by"`
	UpdatedBy     *uuid.UUID `db:"updated_by"`
	UpdatedByName *string    `db:"updated_by_name"`
	CreatedAt     time.Time  `db:"created_at"`
	UpdatedAt     time.Time  `db:"updated_at"`
}

// Scope returns the team or project the page belongs to.
func (p WikiPage) Scope() WikiScope {
	if p.TeamID != nil {
		return TeamWiki(*p.TeamID)
	}
	if p.ProjectID != nil {
		return ProjectWiki(*p.ProjectID)
	}
	return WikiScope{}
}
