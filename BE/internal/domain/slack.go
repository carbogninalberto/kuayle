package domain

import "github.com/google/uuid"

// SlackInstall contains the encrypted bot token. Only the team name is sent to settings.
type SlackInstall struct {
	ID          uuid.UUID `db:"id" json:"-"`
	WorkspaceID uuid.UUID `db:"workspace_id" json:"-"`
	TeamID      string    `db:"team_id" json:"-"`
	Name        string    `db:"name" json:"name"`
	Token       string    `db:"bot_token" json:"-"`
	Scope       string    `db:"scope" json:"-"`
}

type SlackBinding struct {
	TeamID    *uuid.UUID `db:"team_id" json:"team_id,omitempty"`
	ProjectID *uuid.UUID `db:"project_id" json:"project_id,omitempty"`
	ChannelID string     `db:"channel_id" json:"channel_id"`
}
