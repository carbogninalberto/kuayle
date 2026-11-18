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
	ExternalID  string `json:"external_id"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
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
	Sprints           []SourceSprint
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

// Sprint states as the source reports them. A sprint with no known state is
// imported as a completed cycle, so it never shows up as current work.
const (
	SprintStateActive = "active"
	SprintStateFuture = "future"
	SprintStateClosed = "closed"
)

// SourceSprint is a sprint an issue belongs to. Sprints match team cycles by
// name; state and dates are used only when the cycle is created.
type SourceSprint struct {
	ExternalID string
	Name       string
	State      string
	StartDate  *time.Time
	EndDate    *time.Time
}

type SourceProject struct {
	ExternalID string `json:"external_id"`
	Name       string `json:"name"`
	Key        string `json:"key"`
}

// SourcePreview is the result of a dry run. It writes nothing and reports what
// a write phase would encounter. The job stores it as JSON in the same shape
// the API returns, so the UI reads it back from the job unchanged.
//
// UnmappedFields lists source data no mapping can carry (custom fields, users
// without an email, unsupported description nodes). MappingGaps lists source
// values the job's current mapping does not cover yet; it is recomputed each
// time the mapping is saved.
type SourcePreview struct {
	Counts         map[string]int  `json:"counts"`
	Statuses       []string        `json:"statuses"`
	Priorities     []string        `json:"priorities"`
	Labels         []string        `json:"labels"`
	Users          []SourceUser    `json:"users"`
	Projects       []SourceProject `json:"projects"`
	UnmappedFields []string        `json:"unmapped_fields"`
	MappingGaps    []string        `json:"mapping_gaps"`
}

// EmitFunc receives one issue at a time while fetching. The write phase
// collects the emitted issues before writing any of them, because parents,
// epics and relations need the whole set to be known first.
type EmitFunc func(SourceIssue) error

// Source is implemented by each tracker adapter.
type Source interface {
	Preview(ctx context.Context) (*SourcePreview, error)
	Fetch(ctx context.Context, emit EmitFunc) error
	OpenAttachment(ctx context.Context, attachment SourceAttachment) (io.ReadCloser, error)
}
