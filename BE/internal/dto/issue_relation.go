package dto

import "time"

type CreateIssueRelationRequest struct {
	RelatedIdentifier string `json:"related_identifier" validate:"required"`
	Type              string `json:"type" validate:"required,oneof=related blocked_by blocking duplicate"`
}

type IssueRelationResponse struct {
	ID             string         `json:"id"`
	IssueID        string         `json:"issue_id"`
	RelatedIssueID string         `json:"related_issue_id"`
	Type           string         `json:"type"`
	RelatedIssue   *IssueResponse `json:"related_issue,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
}
