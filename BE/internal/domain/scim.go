package domain

import (
	"time"

	"github.com/google/uuid"
)

type SCIMName struct {
	Formatted  string `json:"formatted,omitempty"`
	FamilyName string `json:"familyName,omitempty"`
	GivenName  string `json:"givenName,omitempty"`
	MiddleName string `json:"middleName,omitempty"`
}

type SCIMEmail struct {
	Value   string `json:"value"`
	Type    string `json:"type,omitempty"`
	Primary bool   `json:"primary,omitempty"`
}

type SCIMUserProfile struct {
	UserName    string      `json:"userName"`
	ExternalID  string      `json:"externalId,omitempty"`
	Name        SCIMName    `json:"name,omitzero"`
	DisplayName string      `json:"displayName,omitempty"`
	Emails      []SCIMEmail `json:"emails,omitempty"`
}

type SCIMUser struct {
	SCIMUserProfile
	ID        uuid.UUID `json:"id"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"-"`
	UpdatedAt time.Time `json:"-"`
}

type SCIMGroupMember struct {
	Value uuid.UUID `json:"value"`
}

type SCIMGroup struct {
	ID          uuid.UUID         `json:"id" db:"id"`
	ExternalID  string            `json:"externalId,omitempty" db:"external_id"`
	DisplayName string            `json:"displayName" db:"display_name"`
	Members     []SCIMGroupMember `json:"members,omitempty" db:"-"`
	CreatedAt   time.Time         `json:"-" db:"created_at"`
	UpdatedAt   time.Time         `json:"-" db:"updated_at"`
	TeamID      *uuid.UUID        `json:"-" db:"team_id"`
}

type SCIMQuery struct {
	Attribute string
	Value     string
	Start     int
	Count     int
}

type SCIMTokenStatus struct {
	Enabled   bool       `json:"enabled" db:"enabled"`
	RotatedAt *time.Time `json:"rotated_at,omitempty" db:"rotated_at"`
}
