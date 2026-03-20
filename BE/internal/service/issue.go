package service

import (
	"context"
	"fmt"
	"time"

	"github.com/carbon/carbon-backend/internal/domain"
	"github.com/carbon/carbon-backend/internal/dto"
	"github.com/carbon/carbon-backend/internal/realtime"
	"github.com/carbon/carbon-backend/internal/repository"
	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
)

type IssueService struct {
	issueRepo   repository.IssueRepo
	teamRepo    repository.TeamRepo
	historyRepo repository.IssueHistoryRepo
	hub         *realtime.Hub
}

func NewIssueService(issueRepo repository.IssueRepo, teamRepo repository.TeamRepo, historyRepo repository.IssueHistoryRepo, hub *realtime.Hub) *IssueService {
	return &IssueService{issueRepo: issueRepo, teamRepo: teamRepo, historyRepo: historyRepo, hub: hub}
}

func (s *IssueService) Create(ctx context.Context, workspaceID, creatorID uuid.UUID, req dto.CreateIssueRequest) (*domain.Issue, error) {
	teamID, err := uuid.Parse(req.TeamID)
	if err != nil {
		return nil, fmt.Errorf("invalid team_id")
	}

	team, err := s.teamRepo.GetByID(ctx, teamID)
	if err != nil || team == nil {
		return nil, fmt.Errorf("team not found")
	}

	tx, err := s.issueRepo.BeginTx(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	number, err := s.issueRepo.NextNumber(ctx, tx, teamID)
	if err != nil {
		return nil, err
	}

	identifier := fmt.Sprintf("%s-%d", team.Key, number)

	status := domain.IssueStatusBacklog
	if req.Status != "" {
		status = domain.IssueStatus(req.Status)
	}

	priority := domain.IssuePriority(0)
	if req.Priority != nil {
		priority = domain.IssuePriority(*req.Priority)
	}

	issue := &domain.Issue{
		ID:          uuid.New(),
		WorkspaceID: workspaceID,
		TeamID:      teamID,
		Number:      number,
		Identifier:  identifier,
		Title:       req.Title,
		Description: req.Description,
		Status:      status,
		Priority:    priority,
		CreatorID:   creatorID,
		SortOrder:   float64(number) * 1000,
		Triaged:     !team.TriageEnabled,
	}

	if req.ProjectID != nil {
		pid, _ := uuid.Parse(*req.ProjectID)
		issue.ProjectID = &pid
	}
	if req.AssigneeID != nil {
		aid, _ := uuid.Parse(*req.AssigneeID)
		issue.AssigneeID = &aid
	}
	if req.CycleID != nil {
		cid, _ := uuid.Parse(*req.CycleID)
		issue.CycleID = &cid
	}
	if req.ParentID != nil {
		pid, _ := uuid.Parse(*req.ParentID)
		issue.ParentID = &pid
	}
	issue.Estimate = req.Estimate
	if req.DueDate != nil && *req.DueDate != "" {
		t, err := time.Parse("2006-01-02", *req.DueDate)
		if err == nil {
			issue.DueDate = &t
		}
	}

	if err := s.issueRepo.Create(ctx, tx, issue); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	// Set labels
	if len(req.LabelIDs) > 0 {
		labelUUIDs := make([]uuid.UUID, len(req.LabelIDs))
		for i, lid := range req.LabelIDs {
			labelUUIDs[i], _ = uuid.Parse(lid)
		}
		if err := s.issueRepo.SetLabels(ctx, issue.ID, labelUUIDs); err != nil {
			log.WithError(err).Warn("failed to set labels")
		}
	}

	// Set assignees (multi-assignee)
	if len(req.AssigneeIDs) > 0 {
		uids := make([]uuid.UUID, len(req.AssigneeIDs))
		for i, aid := range req.AssigneeIDs {
			uids[i], _ = uuid.Parse(aid)
		}
		if err := s.issueRepo.SetAssignees(ctx, issue.ID, uids); err != nil {
			log.WithError(err).Warn("failed to set assignees")
		}
	} else if issue.AssigneeID != nil {
		// Single assignee_id was provided — sync to junction table
		if err := s.issueRepo.SetAssignees(ctx, issue.ID, []uuid.UUID{*issue.AssigneeID}); err != nil {
			log.WithError(err).Warn("failed to set assignees")
		}
	}

	// Publish real-time event
	s.hub.Broadcast(workspaceID, realtime.Event{
		Type:    "issue.created",
		Payload: issue,
	})

	return issue, nil
}

func (s *IssueService) GetByIdentifier(ctx context.Context, workspaceID uuid.UUID, identifier string) (*domain.Issue, error) {
	return s.issueRepo.GetByIdentifier(ctx, workspaceID, identifier)
}

func (s *IssueService) List(ctx context.Context, workspaceID uuid.UUID, params dto.IssueFilterParams) ([]domain.Issue, int, error) {
	return s.issueRepo.List(ctx, workspaceID, params)
}

func (s *IssueService) Update(ctx context.Context, workspaceID, userID uuid.UUID, identifier string, req dto.UpdateIssueRequest) (*domain.Issue, error) {
	issue, err := s.issueRepo.GetByIdentifier(ctx, workspaceID, identifier)
	if err != nil || issue == nil {
		return nil, fmt.Errorf("issue not found")
	}

	// Track changes for history
	if req.Title != nil && *req.Title != issue.Title {
		old := issue.Title
		issue.Title = *req.Title
		s.recordHistory(ctx, issue.ID, userID, "title", &old, req.Title)
	}
	if req.Description != nil {
		old := ""
		if issue.Description != nil {
			old = *issue.Description
		}
		issue.Description = req.Description
		s.recordHistory(ctx, issue.ID, userID, "description", &old, req.Description)
	}
	if req.Status != nil && *req.Status != string(issue.Status) {
		old := string(issue.Status)
		issue.Status = domain.IssueStatus(*req.Status)
		s.recordHistory(ctx, issue.ID, userID, "status", &old, req.Status)
	}
	if req.Priority != nil && domain.IssuePriority(*req.Priority) != issue.Priority {
		old := fmt.Sprintf("%d", issue.Priority)
		issue.Priority = domain.IssuePriority(*req.Priority)
		newVal := fmt.Sprintf("%d", *req.Priority)
		s.recordHistory(ctx, issue.ID, userID, "priority", &old, &newVal)
	}
	if req.AssigneeID != nil {
		old := ""
		if issue.AssigneeID != nil {
			old = issue.AssigneeID.String()
		}
		aid, _ := uuid.Parse(*req.AssigneeID)
		issue.AssigneeID = &aid
		s.recordHistory(ctx, issue.ID, userID, "assignee_id", &old, req.AssigneeID)
	}
	if req.ProjectID != nil {
		pid, _ := uuid.Parse(*req.ProjectID)
		issue.ProjectID = &pid
	}
	if req.CycleID != nil {
		cid, _ := uuid.Parse(*req.CycleID)
		issue.CycleID = &cid
	}
	if req.ParentID != nil {
		pid, _ := uuid.Parse(*req.ParentID)
		issue.ParentID = &pid
	}
	if req.Estimate != nil {
		issue.Estimate = req.Estimate
	}
	if req.DueDate != nil {
		if *req.DueDate == "" {
			issue.DueDate = nil
		} else {
			t, err := time.Parse("2006-01-02", *req.DueDate)
			if err == nil {
				issue.DueDate = &t
			}
		}
	}
	if req.SortOrder != nil {
		issue.SortOrder = *req.SortOrder
	}

	if err := s.issueRepo.Update(ctx, issue); err != nil {
		return nil, err
	}

	if req.LabelIDs != nil {
		labelUUIDs := make([]uuid.UUID, len(req.LabelIDs))
		for i, lid := range req.LabelIDs {
			labelUUIDs[i], _ = uuid.Parse(lid)
		}
		if err := s.issueRepo.SetLabels(ctx, issue.ID, labelUUIDs); err != nil {
			log.WithError(err).Warn("failed to set labels")
		}
	}

	// Update assignees (multi-assignee)
	if req.AssigneeIDs != nil {
		uids := make([]uuid.UUID, len(req.AssigneeIDs))
		for i, aid := range req.AssigneeIDs {
			uids[i], _ = uuid.Parse(aid)
		}
		if err := s.issueRepo.SetAssignees(ctx, issue.ID, uids); err != nil {
			log.WithError(err).Warn("failed to set assignees")
		}
	}

	s.hub.Broadcast(workspaceID, realtime.Event{
		Type:    "issue.updated",
		Payload: issue,
	})

	return issue, nil
}

func (s *IssueService) Delete(ctx context.Context, workspaceID uuid.UUID, identifier string) error {
	issue, err := s.issueRepo.GetByIdentifier(ctx, workspaceID, identifier)
	if err != nil || issue == nil {
		return fmt.Errorf("issue not found")
	}

	if err := s.issueRepo.Delete(ctx, issue.ID); err != nil {
		return err
	}

	s.hub.Broadcast(workspaceID, realtime.Event{
		Type:    "issue.deleted",
		Payload: map[string]string{"identifier": identifier},
	})

	return nil
}

func (s *IssueService) Triage(ctx context.Context, workspaceID, userID uuid.UUID, identifier string, accept bool) (*domain.Issue, error) {
	issue, err := s.issueRepo.GetByIdentifier(ctx, workspaceID, identifier)
	if err != nil || issue == nil {
		return nil, fmt.Errorf("issue not found")
	}
	if issue.Triaged {
		return nil, fmt.Errorf("issue is already triaged")
	}

	issue.Triaged = true
	if !accept {
		issue.Status = domain.IssueStatusCancelled
		old := string(issue.Status)
		newVal := string(domain.IssueStatusCancelled)
		s.recordHistory(ctx, issue.ID, userID, "status", &old, &newVal)
	}

	if err := s.issueRepo.Update(ctx, issue); err != nil {
		return nil, err
	}

	s.hub.Broadcast(workspaceID, realtime.Event{
		Type:    "issue.triaged",
		Payload: issue,
	})

	return issue, nil
}

func (s *IssueService) GetLabels(ctx context.Context, issueID uuid.UUID) ([]domain.Label, error) {
	return s.issueRepo.GetLabels(ctx, issueID)
}

func (s *IssueService) GetLabelsForIssues(ctx context.Context, issueIDs []uuid.UUID) (map[uuid.UUID][]domain.Label, error) {
	return s.issueRepo.GetLabelsForIssues(ctx, issueIDs)
}

func (s *IssueService) GetAssignees(ctx context.Context, issueID uuid.UUID) ([]uuid.UUID, error) {
	return s.issueRepo.GetAssignees(ctx, issueID)
}

func (s *IssueService) GetAssigneesForIssues(ctx context.Context, issueIDs []uuid.UUID) (map[uuid.UUID][]uuid.UUID, error) {
	return s.issueRepo.GetAssigneesForIssues(ctx, issueIDs)
}

func (s *IssueService) GetHistory(ctx context.Context, issueID uuid.UUID) ([]domain.IssueHistory, error) {
	return s.historyRepo.ListByIssue(ctx, issueID)
}

func (s *IssueService) ListSubIssues(ctx context.Context, workspaceID uuid.UUID, identifier string) ([]domain.Issue, error) {
	issue, err := s.issueRepo.GetByIdentifier(ctx, workspaceID, identifier)
	if err != nil || issue == nil {
		return nil, fmt.Errorf("issue not found")
	}
	return s.issueRepo.ListSubIssues(ctx, issue.ID)
}

func (s *IssueService) CountSubIssues(ctx context.Context, issueID uuid.UUID) (int, int, error) {
	return s.issueRepo.CountSubIssues(ctx, issueID)
}

func (s *IssueService) BulkUpdate(ctx context.Context, workspaceID, userID uuid.UUID, req dto.BulkUpdateIssueRequest) (int, error) {
	issueIDs := make([]uuid.UUID, len(req.IssueIDs))
	for i, id := range req.IssueIDs {
		parsed, err := uuid.Parse(id)
		if err != nil {
			return 0, fmt.Errorf("invalid issue_id: %s", id)
		}
		issueIDs[i] = parsed
	}

	var assigneeID *uuid.UUID
	if req.AssigneeID != nil {
		aid, err := uuid.Parse(*req.AssigneeID)
		if err != nil {
			return 0, fmt.Errorf("invalid assignee_id")
		}
		assigneeID = &aid
	}

	n, err := s.issueRepo.BulkUpdate(ctx, workspaceID, issueIDs, req.Status, req.Priority, assigneeID)
	if err != nil {
		return 0, err
	}

	s.hub.Broadcast(workspaceID, realtime.Event{
		Type:    "issues.bulk_updated",
		Payload: map[string]interface{}{"count": n},
	})

	return n, nil
}

func (s *IssueService) BulkDelete(ctx context.Context, workspaceID uuid.UUID, req dto.BulkDeleteIssueRequest) (int, error) {
	issueIDs := make([]uuid.UUID, len(req.IssueIDs))
	for i, id := range req.IssueIDs {
		parsed, err := uuid.Parse(id)
		if err != nil {
			return 0, fmt.Errorf("invalid issue_id: %s", id)
		}
		issueIDs[i] = parsed
	}

	n, err := s.issueRepo.BulkDelete(ctx, workspaceID, issueIDs)
	if err != nil {
		return 0, err
	}

	s.hub.Broadcast(workspaceID, realtime.Event{
		Type:    "issues.bulk_deleted",
		Payload: map[string]interface{}{"count": n},
	})

	return n, nil
}

func (s *IssueService) recordHistory(ctx context.Context, issueID, userID uuid.UUID, field string, oldValue, newValue *string) {
	if err := s.historyRepo.Create(ctx, issueID, userID, field, oldValue, newValue); err != nil {
		log.WithError(err).Warn("failed to record issue history")
	}
}
