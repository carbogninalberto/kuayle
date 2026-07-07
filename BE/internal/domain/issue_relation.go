package domain

import (
	"time"

	"github.com/google/uuid"
)

type IssueRelationType string

const (
	RelationRelated   IssueRelationType = "related"
	RelationBlockedBy IssueRelationType = "blocked_by"
	RelationBlocking  IssueRelationType = "blocking"
	RelationDuplicate IssueRelationType = "duplicate"
)

func (t IssueRelationType) Inverse() IssueRelationType {
	switch t {
	case RelationBlocking:
		return RelationBlockedBy
	case RelationBlockedBy:
		return RelationBlocking
	case RelationRelated:
		return RelationRelated
	case RelationDuplicate:
		return RelationDuplicate
	default:
		return t
	}
}

type IssueRelation struct {
	ID             uuid.UUID         `json:"id" db:"id"`
	IssueID        uuid.UUID         `json:"issue_id" db:"issue_id"`
	RelatedIssueID uuid.UUID         `json:"related_issue_id" db:"related_issue_id"`
	Type           IssueRelationType `json:"type" db:"type"`
	RelatedIssue   *Issue            `json:"related_issue,omitempty" db:"-"`
	CreatedAt      time.Time         `json:"created_at" db:"created_at"`
}

type IssueRelationCounts struct {
	Related   int `json:"related"`
	BlockedBy int `json:"blocked_by"`
	Blocking  int `json:"blocking"`
	Duplicate int `json:"duplicate"`
}

type IssueRelationSummary struct {
	Counts    IssueRelationCounts
	Related   []Issue
	BlockedBy []Issue
	Blocking  []Issue
	Duplicate []Issue
}
