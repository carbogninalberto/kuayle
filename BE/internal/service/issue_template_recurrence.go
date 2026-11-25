package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
	log "github.com/sirupsen/logrus"
)

type IssueTemplateRecurrenceWorker struct {
	templates *repository.IssueTemplateRepository
	issueRepo *repository.IssueRepository
	issues    *IssueService
}

func NewIssueTemplateRecurrenceWorker(templates *repository.IssueTemplateRepository, issueRepo *repository.IssueRepository, issues *IssueService) *IssueTemplateRecurrenceWorker {
	return &IssueTemplateRecurrenceWorker{templates: templates, issueRepo: issueRepo, issues: issues}
}

func (w *IssueTemplateRecurrenceWorker) Run(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for ctx.Err() == nil {
		if err := w.runOnce(ctx); err != nil && ctx.Err() == nil {
			log.WithError(err).Warn("Failed to list due issue templates")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *IssueTemplateRecurrenceWorker) runOnce(ctx context.Context) error {
	due, err := w.templates.ListDueForRecurrence(ctx)
	if err != nil {
		return err
	}
	for _, candidate := range due {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := w.runTemplate(ctx, candidate); err != nil && ctx.Err() == nil {
			log.WithError(err).WithField("issue_template_id", candidate.ID).Warn("Failed to process recurring issue template")
		}
	}
	return nil
}

func (w *IssueTemplateRecurrenceWorker) runTemplate(ctx context.Context, candidate domain.IssueTemplate) error {
	// A timed-out attempt leaves the lease to expire and can be recovered by
	// another worker. The completion write also checks the lease in PostgreSQL.
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	tmpl, err := w.templates.ClaimRecurrence(ctx, candidate)
	if err != nil || tmpl == nil {
		return err
	}
	schedule, err := domain.ParseIssueTemplateSchedule(*tmpl.RecurrenceRule)
	if err != nil {
		return w.recordFailure(ctx, tmpl, nil, err.Error(), err)
	}
	// Process the stored slot and resume at the next future time, as the
	// automation worker does after downtime.
	now := time.Now()
	if tmpl.NextRunAt != nil && tmpl.NextRunAt.After(now) {
		now = *tmpl.NextRunAt
	}
	next, err := schedule.Next(now)
	if err != nil {
		return w.recordFailure(ctx, tmpl, nil, err.Error(), err)
	}
	if tmpl.NextRunAt == nil {
		return w.templates.CompleteRecurrence(ctx, tmpl, &next, nil, nil)
	}
	if tmpl.TeamID == nil {
		return w.recordFailure(ctx, tmpl, &next, "Select a team for this recurring template.", nil)
	}
	labels := []uuid.UUID{}
	if err := json.Unmarshal(tmpl.LabelIDs, &labels); err != nil {
		return w.recordFailure(ctx, tmpl, &next, "The template contains invalid labels.", err)
	}
	seen := make(map[uuid.UUID]bool, len(labels))
	uniqueLabels := labels[:0]
	for _, id := range labels {
		if !seen[id] {
			seen[id] = true
			uniqueLabels = append(uniqueLabels, id)
		}
	}

	actor := domain.WithAccess(ctx, domain.Access{WorkspaceID: tmpl.WorkspaceID, UserID: tmpl.CreatedBy})
	issue, team, err := w.issues.newIssue(actor, tmpl.WorkspaceID, tmpl.CreatedBy, recurringIssueRequest(tmpl, schedule.Location))
	if err != nil {
		return w.recordFailure(ctx, tmpl, &next, "Check that the template creator can access its team and that its status is valid.", err)
	}

	var created bool
	var creationErr error
	err = w.templates.CompleteRecurrence(ctx, tmpl, &next, nil, func(tx *sqlx.Tx) error {
		creationErr = repository.CreateNumberedIssue(actor, w.issues.issueRepo, tx, issue, team.Key)
		if creationErr == nil {
			creationErr = w.issueRepo.SetRecurringIssueDefaults(actor, tx, issue, uniqueLabels)
		}
		created = creationErr == nil
		return creationErr
	})
	if creationErr != nil {
		message := "Could not save the issue. Check the template's team, status and assignee."
		switch {
		case errors.Is(creationErr, repository.ErrTemplateIssueLabelUnavailable):
			message = "A template label is missing or unavailable to this team."
		case errors.Is(creationErr, repository.ErrTemplateIssueAssigneeUnavailable):
			message = "The template assignee is no longer a workspace member."
		case errors.Is(creationErr, sql.ErrNoRows):
			message = "The team, status or assignee is no longer available to the template creator."
		}
		return w.recordFailure(ctx, tmpl, &next, message, creationErr)
	}
	if err == nil && created {
		w.issues.publishCreatedIssue(actor, issue)
	}
	return err
}

func (w *IssueTemplateRecurrenceWorker) recordFailure(ctx context.Context, tmpl *domain.IssueTemplate, next *time.Time, message string, cause error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	log.WithError(cause).WithField("issue_template_id", tmpl.ID).Warn(message)
	return w.templates.CompleteRecurrence(ctx, tmpl, next, &message, nil)
}

func recurringIssueRequest(tmpl *domain.IssueTemplate, zone *time.Location) dto.CreateIssueRequest {
	title := tmpl.Title
	if tmpl.DefaultIssueTitle != nil && strings.TrimSpace(*tmpl.DefaultIssueTitle) != "" {
		title = *tmpl.DefaultIssueTitle
	}
	suffix := " - " + tmpl.NextRunAt.In(zone).Format(time.DateOnly)
	req := dto.CreateIssueRequest{
		Title:       strings.TrimSpace(truncateRunes(title, 500-len(suffix))) + suffix,
		Description: tmpl.Description,
		Priority:    tmpl.Priority,
		TeamID:      tmpl.TeamID.String(),
	}
	if tmpl.Status != nil {
		req.Status = *tmpl.Status
	}
	if tmpl.AssigneeID != nil {
		id := tmpl.AssigneeID.String()
		req.AssigneeID = &id
	}
	return req
}
