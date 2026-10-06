package dto

import "time"

type CreateTeamRequest struct {
	IsPrivate                     bool    `json:"is_private"`
	AcknowledgePrivacyLimitations bool    `json:"acknowledge_privacy_limitations"`
	Name                          string  `json:"name" validate:"required,min=1,max=100"`
	Key                           string  `json:"key" validate:"required,min=1,max=10,alpha,uppercase"`
	Description                   *string `json:"description"`
	Color                         *string `json:"color"`
	Icon                          *string `json:"icon"`
}

type UpdateTeamRequest struct {
	Name                     *string `json:"name" validate:"omitempty,min=1,max=100"`
	Description              *string `json:"description"`
	Color                    *string `json:"color"`
	Icon                     *string `json:"icon"`
	TriageEnabled            *bool   `json:"triage_enabled"`
	ParentAutoCloseEnabled   *bool   `json:"parent_auto_close_enabled"`
	SubIssueAutoCloseEnabled *bool   `json:"sub_issue_auto_close_enabled"`
	IssueCopyPrompt          *string `json:"issue_copy_prompt" validate:"omitempty,max=8000"`
}

type TeamResponse struct {
	IsPrivate                bool      `json:"is_private"`
	ID                       string    `json:"id"`
	Name                     string    `json:"name"`
	Key                      string    `json:"key"`
	Description              *string   `json:"description"`
	Color                    *string   `json:"color"`
	Icon                     *string   `json:"icon"`
	TriageEnabled            bool      `json:"triage_enabled"`
	ParentAutoCloseEnabled   bool      `json:"parent_auto_close_enabled"`
	SubIssueAutoCloseEnabled bool      `json:"sub_issue_auto_close_enabled"`
	IssueCopyPrompt          *string   `json:"issue_copy_prompt"`
	CreatedAt                time.Time `json:"created_at"`
	UpdatedAt                time.Time `json:"updated_at"`
}

type SetTeamVisibilityRequest struct {
	IsPrivate                     bool `json:"is_private"`
	ConfirmPublic                 bool `json:"confirm_public"`
	AcknowledgePrivacyLimitations bool `json:"acknowledge_privacy_limitations"`
}
type AddTeamMemberRequest struct {
	UserID string `json:"user_id" validate:"required,uuid"`
}
