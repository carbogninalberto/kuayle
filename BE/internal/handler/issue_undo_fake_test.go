package handler

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
)

// Soft delete, restore and undo for the in-memory testIssueRepo, following
// the repository's contract: deleted issues leave issues and move to deleted,
// and undo checks the version before writing anything.

func (r *testIssueRepo) deletedIssues() map[uuid.UUID]*domain.DeletedIssue {
	if r.deleted == nil {
		r.deleted = map[uuid.UUID]*domain.DeletedIssue{}
	}
	return r.deleted
}

func (r *testIssueRepo) softDelete(id uuid.UUID) bool {
	for key, issue := range r.issues {
		if issue.ID == id {
			delete(r.issues, key)
			r.deletedIssues()[id] = &domain.DeletedIssue{Issue: *issue, DeletedAt: time.Now()}
			return true
		}
	}
	return false
}

func (r *testIssueRepo) ListByIDs(_ context.Context, _ uuid.UUID, ids []uuid.UUID) ([]domain.Issue, error) {
	found := []domain.Issue{}
	for _, id := range ids {
		for _, issue := range r.issues {
			if issue.ID == id {
				found = append(found, *issue)
			}
		}
	}
	return found, nil
}

func (r *testIssueRepo) ApplyUndo(_ context.Context, _ uuid.UUID, items []domain.IssueUndo) ([]domain.Issue, error) {
	targets := make([]*domain.Issue, len(items))
	for i, item := range items {
		for _, issue := range r.issues {
			if issue.ID == item.IssueID {
				targets[i] = issue
			}
		}
		if targets[i] == nil {
			return nil, repository.ErrBulkIssueNotFound
		}
		if targets[i].Version != item.ExpectedVersion {
			return nil, repository.ErrEditedMeanwhile
		}
	}
	result := make([]domain.Issue, len(items))
	for i, item := range items {
		issue, prev := targets[i], item.Previous
		if prev.StatusID != nil {
			issue.StatusID = prev.StatusID
		}
		if prev.Priority != nil {
			issue.Priority = *prev.Priority
		}
		if prev.AssigneeSet {
			issue.AssigneeID = prev.AssigneeID
		}
		if prev.ProjectSet {
			issue.ProjectID = prev.ProjectID
		}
		if prev.CycleSet {
			issue.CycleID = prev.CycleID
		}
		issue.Version++
		result[i] = *issue
	}
	return result, nil
}

func (r *testIssueRepo) GetDeletedByIdentifier(_ context.Context, _ uuid.UUID, identifier string) (*domain.DeletedIssue, error) {
	for _, issue := range r.deletedIssues() {
		if issue.Identifier == identifier {
			copy := *issue
			return &copy, nil
		}
	}
	return nil, nil
}

func (r *testIssueRepo) ListDeletedByIDs(_ context.Context, _ uuid.UUID, ids []uuid.UUID) ([]domain.DeletedIssue, error) {
	found := []domain.DeletedIssue{}
	for _, id := range ids {
		if issue, ok := r.deletedIssues()[id]; ok {
			found = append(found, *issue)
		}
	}
	return found, nil
}

func (r *testIssueRepo) Restore(_ context.Context, _ uuid.UUID, ids []uuid.UUID, deletedSince time.Time) ([]domain.Issue, error) {
	for _, id := range ids {
		issue, ok := r.deletedIssues()[id]
		if !ok {
			return nil, repository.ErrBulkIssueNotFound
		}
		if issue.DeletedAt.Before(deletedSince) {
			return nil, repository.ErrRestoreExpired
		}
	}
	restored := []domain.Issue{}
	for _, id := range ids {
		issue := r.deletedIssues()[id].Issue
		delete(r.deletedIssues(), id)
		r.issues[issue.Identifier] = &issue
		restored = append(restored, issue)
	}
	return restored, nil
}
