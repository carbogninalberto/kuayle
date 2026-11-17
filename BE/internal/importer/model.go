// Package importer defines the source-agnostic model that every tracker
// adapter produces and the mapping that turns it into Kuayle entities. The Jira
// adapter lives in the jira subpackage; no importer here makes a live call in
// tests.
package importer

import (
	"context"
	"io"
	"time"

	"github.com/kuayle/kuayle-backend/internal/domain"
)

// SourceUser is a reporter, assignee or comment author as the source knows
// them. Email may be empty: Jira Cloud often hides it from API responses, in
// which case the user cannot be matched and is attributed to the importer.
type SourceUser struct {
	ExternalID  string
	Email       string
	DisplayName string
}

// SourceAttachment is metadata only; the bytes are opened separately through
// Source.OpenAttachment so oversized items can be skipped before download.
type SourceAttachment struct {
	ExternalID  string
	Filename    string
	ContentType string
	Size        int64
	URL         string
}

type SourceComment struct {
	ExternalID string
	Author     SourceUser
	Body       string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type SourceRelation struct {
	ExternalID       string
	SourceExternalID string
	TargetExternalID string
	Type             domain.IssueRelationType
}

type SourceIssue struct {
	ExternalID        string
	ExternalKey       string
	URL               string
	Title             string
	Description       string
	Status            string
	Priority          string
	ProjectExternalID string
	ParentExternalID  string
	EpicExternalID    string
	SprintIDs         []string
	Labels            []string
	Assignee          *SourceUser
	Reporter          *SourceUser
	CreatedAt         time.Time
	UpdatedAt         time.Time
	Comments          []SourceComment
	Attachments       []SourceAttachment
	Relations         []SourceRelation
	HistoryMarkdown   string
}

type SourceSprint struct {
	ExternalID string
	Name       string
	StartDate  *time.Time
	EndDate    *time.Time
}

type SourceProject struct {
	ExternalID string
	Name       string
	Key        string
}

// SourcePreview is the result of a dry run. It writes nothing and reports what
// a write phase would encounter, including fields no mapping can take yet.
type SourcePreview struct {
	Counts         map[string]int
	Statuses       []string
	Priorities     []string
	Labels         []string
	Users          []SourceUser
	Projects       []SourceProject
	UnmappedFields []string
}

// EmitFunc receives one issue at a time so a large project never has to be
// held in memory all at once.
type EmitFunc func(SourceIssue) error

// Source is implemented by each tracker adapter.
type Source interface {
	Preview(ctx context.Context) (*SourcePreview, error)
	Fetch(ctx context.Context, emit EmitFunc) error
	OpenAttachment(ctx context.Context, attachment SourceAttachment) (io.ReadCloser, error)
}
