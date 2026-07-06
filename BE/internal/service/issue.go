package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/realtime"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/pkg/sanitize"
	log "github.com/sirupsen/logrus"
)

type IssueService struct {
	issueRepo      repository.IssueRepo
	teamRepo       repository.TeamRepo
	projectRepo    repository.ProjectRepo
	teamStatusRepo repository.TeamStatusRepo
	historyRepo    repository.IssueHistoryRepo
	hub            *realtime.Hub
	notifSvc       *NotificationService
}

func NewIssueService(issueRepo repository.IssueRepo, teamRepo repository.TeamRepo, teamStatusRepo repository.TeamStatusRepo, historyRepo repository.IssueHistoryRepo, hub *realtime.Hub, notifSvc *NotificationService, projectRepo ...repository.ProjectRepo) *IssueService {
	svc := &IssueService{issueRepo: issueRepo, teamRepo: teamRepo, teamStatusRepo: teamStatusRepo, historyRepo: historyRepo, hub: hub, notifSvc: notifSvc}
	if len(projectRepo) > 0 {
		svc.projectRepo = projectRepo[0]
	}
	return svc
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

	// Sanitize user input
	req.Title = sanitize.PlainText(req.Title)
	if req.Description != nil {
		clean := sanitize.SanitizeEditorContent(*req.Description)
		req.Description = &clean
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
		pid, _, err := s.validateParentID(ctx, workspaceID, uuid.Nil, *req.ParentID)
		if err != nil {
			return nil, err
		}
		issue.ParentID = pid
	}
	if req.DueDate != nil && *req.DueDate != "" {
		t, err := time.Parse("2006-01-02", *req.DueDate)
		if err == nil {
			issue.DueDate = &t
		}
	}

	// Resolve status_id
	if req.StatusID != nil {
		sid, err := uuid.Parse(*req.StatusID)
		if err == nil {
			// Validate that the status belongs to the same team
			ts, _ := s.teamStatusRepo.GetByID(ctx, sid)
			if ts != nil && ts.TeamID == teamID {
				issue.StatusID = &sid
				issue.Status = domain.IssueStatus(ts.Slug)
			}
		}
	}
	if issue.StatusID == nil {
		// Look up the matching team_status by slug
		ts, err := s.teamStatusRepo.GetByTeamAndSlug(ctx, teamID, string(status))
		if err == nil && ts != nil {
			issue.StatusID = &ts.ID
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

	// Notify assignees (except the creator)
	s.notifyAssignees(ctx, issue, creatorID, workspaceID)

	return issue, nil
}

func (s *IssueService) CreateSubIssue(ctx context.Context, workspaceID, creatorID uuid.UUID, parentIdentifier string, req dto.CreateSubIssueRequest) (*domain.Issue, error) {
	parent, err := s.issueRepo.GetByIdentifier(ctx, workspaceID, parentIdentifier)
	if err != nil || parent == nil {
		return nil, fmt.Errorf("parent issue not found")
	}

	priority := int(parent.Priority)
	if req.Priority != nil {
		priority = *req.Priority
	}
	parentID := parent.ID.String()
	createReq := dto.CreateIssueRequest{
		Title:       req.Title,
		Description: req.Description,
		Status:      req.Status,
		StatusID:    req.StatusID,
		Priority:    &priority,
		TeamID:      parent.TeamID.String(),
		ProjectID:   req.ProjectID,
		AssigneeID:  req.AssigneeID,
		AssigneeIDs: req.AssigneeIDs,
		LabelIDs:    req.LabelIDs,
		ParentID:    &parentID,
		DueDate:     req.DueDate,
		CycleID:     req.CycleID,
	}

	if createReq.ProjectID == nil && parent.ProjectID != nil {
		pid := parent.ProjectID.String()
		createReq.ProjectID = &pid
	}
	if createReq.CycleID == nil && parent.CycleID != nil {
		active, _ := s.issueRepo.CycleIsActive(ctx, *parent.CycleID)
		if active {
			cid := parent.CycleID.String()
			createReq.CycleID = &cid
		}
	}
	if len(createReq.AssigneeIDs) == 0 && createReq.AssigneeID == nil {
		createReq.AssigneeIDs = s.inheritSubIssueAssignees(ctx, parent, creatorID)
	}

	return s.Create(ctx, workspaceID, creatorID, createReq)
}

func (s *IssueService) BulkCreateSubIssues(ctx context.Context, workspaceID, creatorID uuid.UUID, parentIdentifier string, req dto.BulkCreateSubIssueRequest) ([]domain.Issue, error) {
	created := make([]domain.Issue, 0, len(req.Issues))
	for _, subReq := range req.Issues {
		issue, err := s.CreateSubIssue(ctx, workspaceID, creatorID, parentIdentifier, subReq)
		if err != nil {
			return nil, err
		}
		created = append(created, *issue)
	}
	return created, nil
}

func (s *IssueService) Duplicate(ctx context.Context, workspaceID, creatorID uuid.UUID, identifier string, includeSubIssues bool) (*domain.Issue, error) {
	original, err := s.issueRepo.GetByIdentifier(ctx, workspaceID, identifier)
	if err != nil || original == nil {
		return nil, fmt.Errorf("issue not found")
	}

	duplicated, err := s.duplicateIssue(ctx, workspaceID, creatorID, original, nil, true)
	if err != nil {
		return nil, err
	}

	if includeSubIssues {
		subIssues, err := s.issueRepo.ListSubIssues(ctx, original.ID)
		if err != nil {
			return nil, err
		}
		for i := range subIssues {
			if _, err := s.duplicateIssue(ctx, workspaceID, creatorID, &subIssues[i], &duplicated.ID, false); err != nil {
				return nil, err
			}
		}
	}

	return duplicated, nil
}

func (s *IssueService) ConvertToProject(ctx context.Context, workspaceID, userID uuid.UUID, identifier string) (*domain.Project, error) {
	if s.projectRepo == nil {
		return nil, fmt.Errorf("project repository unavailable")
	}
	issue, err := s.issueRepo.GetByIdentifier(ctx, workspaceID, identifier)
	if err != nil || issue == nil {
		return nil, fmt.Errorf("issue not found")
	}

	project := &domain.Project{
		ID:          uuid.New(),
		WorkspaceID: workspaceID,
		TeamID:      &issue.TeamID,
		Name:        sanitize.PlainText(sanitize.StripHTML(issue.Title)),
		Description: issue.Description,
		Status:      domain.ProjectStatusPlanned,
		SortOrder:   issue.SortOrder,
	}
	if err := s.projectRepo.Create(ctx, project); err != nil {
		return nil, err
	}

	issuesToMove := []*domain.Issue{issue}
	subIssues, err := s.issueRepo.ListSubIssues(ctx, issue.ID)
	if err != nil {
		return nil, err
	}
	for i := range subIssues {
		issuesToMove = append(issuesToMove, &subIssues[i])
	}

	projectID := project.ID
	for _, item := range issuesToMove {
		oldProject := ""
		if item.ProjectID != nil {
			oldProject = item.ProjectID.String()
		}
		oldParent := ""
		if item.ParentID != nil {
			oldParent = item.ParentID.String()
		}
		newProject := projectID.String()
		item.ProjectID = &projectID
		item.ParentID = nil
		if err := s.issueRepo.Update(ctx, item); err != nil {
			return nil, err
		}
		if oldProject != newProject {
			s.recordHistory(ctx, item.ID, userID, "project", &oldProject, &newProject)
		}
		if oldParent != "" {
			newParent := ""
			s.recordHistory(ctx, item.ID, userID, "parent", &oldParent, &newParent)
		}
		s.hub.Broadcast(workspaceID, realtime.Event{Type: "issue.updated", Payload: item})
	}

	return project, nil
}

func (s *IssueService) duplicateIssue(ctx context.Context, workspaceID, creatorID uuid.UUID, original *domain.Issue, parentID *uuid.UUID, appendCopy bool) (*domain.Issue, error) {
	labels, _ := s.issueRepo.GetLabels(ctx, original.ID)
	labelIDs := make([]string, 0, len(labels))
	for _, label := range labels {
		labelIDs = append(labelIDs, label.ID.String())
	}
	assignees, _ := s.issueRepo.GetAssignees(ctx, original.ID)
	assigneeIDs := make([]string, 0, len(assignees))
	for _, assignee := range assignees {
		assigneeIDs = append(assigneeIDs, assignee.String())
	}
	if len(assigneeIDs) == 0 && original.AssigneeID != nil {
		assigneeIDs = append(assigneeIDs, original.AssigneeID.String())
	}

	title := original.Title
	if appendCopy {
		title = title + " (copy)"
	}
	priority := int(original.Priority)
	req := dto.CreateIssueRequest{
		Title:       title,
		Description: original.Description,
		Status:      string(original.Status),
		Priority:    &priority,
		TeamID:      original.TeamID.String(),
		LabelIDs:    labelIDs,
		AssigneeIDs: assigneeIDs,
	}
	if original.StatusID != nil {
		statusID := original.StatusID.String()
		req.StatusID = &statusID
	}
	if original.ProjectID != nil {
		projectID := original.ProjectID.String()
		req.ProjectID = &projectID
	}
	if original.CycleID != nil {
		cycleID := original.CycleID.String()
		req.CycleID = &cycleID
	}
	if original.DueDate != nil {
		due := original.DueDate.Format("2006-01-02")
		req.DueDate = &due
	}
	if parentID != nil {
		parentIDString := parentID.String()
		req.ParentID = &parentIDString
	}
	return s.Create(ctx, workspaceID, creatorID, req)
}

func (s *IssueService) GetByIdentifier(ctx context.Context, workspaceID uuid.UUID, identifier string) (*domain.Issue, error) {
	return s.issueRepo.GetByIdentifier(ctx, workspaceID, identifier)
}

func (s *IssueService) GetByID(ctx context.Context, id uuid.UUID) (*domain.Issue, error) {
	return s.issueRepo.GetByID(ctx, id)
}

func (s *IssueService) List(ctx context.Context, workspaceID uuid.UUID, params dto.IssueFilterParams) ([]domain.Issue, int, error) {
	return s.issueRepo.List(ctx, workspaceID, params)
}

func (s *IssueService) Update(ctx context.Context, workspaceID, userID uuid.UUID, identifier string, req dto.UpdateIssueRequest) (*domain.Issue, error) {
	issue, err := s.issueRepo.GetByIdentifier(ctx, workspaceID, identifier)
	if err != nil || issue == nil {
		return nil, fmt.Errorf("issue not found")
	}

	// Sanitize user input
	if req.Title != nil {
		clean := sanitize.PlainText(*req.Title)
		req.Title = &clean
	}
	if req.Description != nil {
		clean := sanitize.SanitizeEditorContent(*req.Description)
		req.Description = &clean
	}

	// Capture old description before overwriting (for mention diff)
	var oldDescription string
	if issue.Description != nil {
		oldDescription = *issue.Description
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
	statusChanged := false
	if req.StatusID != nil {
		sid, err := uuid.Parse(*req.StatusID)
		if err == nil {
			// Look up old and new status names for history
			var oldName string
			if issue.StatusID != nil {
				oldStatus, _ := s.teamStatusRepo.GetByID(ctx, *issue.StatusID)
				if oldStatus != nil {
					oldName = oldStatus.Name
				}
			}
			newStatus, _ := s.teamStatusRepo.GetByID(ctx, sid)
			// Validate that the status belongs to the same team as the issue
			if newStatus != nil && newStatus.TeamID == issue.TeamID {
				newName := newStatus.Name
				statusChanged = issue.StatusID == nil || *issue.StatusID != sid
				issue.StatusID = &sid
				// Update legacy status field for backward compat
				issue.Status = domain.IssueStatus(newStatus.Slug)
				s.recordHistory(ctx, issue.ID, userID, "status", &oldName, &newName)
			}
		}
	} else if req.Status != nil && *req.Status != string(issue.Status) {
		old := string(issue.Status)
		issue.Status = domain.IssueStatus(*req.Status)
		statusChanged = true
		s.recordHistory(ctx, issue.ID, userID, "status", &old, req.Status)
		// Also update status_id to match the new legacy status slug
		ts, err := s.teamStatusRepo.GetByTeamAndSlug(ctx, issue.TeamID, *req.Status)
		if err == nil && ts != nil {
			issue.StatusID = &ts.ID
		}
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
		old := ""
		if issue.ProjectID != nil {
			old = issue.ProjectID.String()
		}
		if *req.ProjectID == "" {
			issue.ProjectID = nil
		} else {
			pid, _ := uuid.Parse(*req.ProjectID)
			issue.ProjectID = &pid
		}
		newVal := ""
		if issue.ProjectID != nil {
			newVal = issue.ProjectID.String()
		}
		if old != newVal {
			s.recordHistory(ctx, issue.ID, userID, "project", &old, &newVal)
		}
	}
	if req.CycleID != nil {
		old := ""
		if issue.CycleID != nil {
			old = issue.CycleID.String()
		}
		if *req.CycleID == "" {
			issue.CycleID = nil
		} else {
			cid, _ := uuid.Parse(*req.CycleID)
			issue.CycleID = &cid
		}
		newVal := ""
		if issue.CycleID != nil {
			newVal = issue.CycleID.String()
		}
		if old != newVal {
			s.recordHistory(ctx, issue.ID, userID, "cycle", &old, &newVal)
		}
	}
	if req.ParentID != nil {
		old := ""
		if issue.ParentID != nil {
			old = issue.ParentID.String()
		}
		if *req.ParentID == "" {
			issue.ParentID = nil
		} else {
			pid, _, err := s.validateParentID(ctx, workspaceID, issue.ID, *req.ParentID)
			if err != nil {
				return nil, err
			}
			issue.ParentID = pid
		}
		newVal := ""
		if issue.ParentID != nil {
			newVal = issue.ParentID.String()
		}
		if old != newVal {
			s.recordHistory(ctx, issue.ID, userID, "parent", &old, &newVal)
		}
	}
	if req.DueDate != nil {
		oldVal := ""
		if issue.DueDate != nil {
			oldVal = issue.DueDate.Format("2006-01-02")
		}
		if *req.DueDate == "" {
			issue.DueDate = nil
		} else {
			t, err := time.Parse("2006-01-02", *req.DueDate)
			if err == nil {
				issue.DueDate = &t
			}
		}
		newVal := *req.DueDate
		if oldVal != newVal {
			s.recordHistory(ctx, issue.ID, userID, "due_date", &oldVal, &newVal)
		}
	}
	if req.SortOrder != nil {
		issue.SortOrder = *req.SortOrder
	}

	if err := s.issueRepo.Update(ctx, issue); err != nil {
		return nil, err
	}

	if req.LabelIDs != nil {
		// Track label changes
		oldLabels, _ := s.issueRepo.GetLabels(ctx, issue.ID)
		oldNames := make([]string, len(oldLabels))
		for i, l := range oldLabels {
			oldNames[i] = l.Name
		}

		labelUUIDs := make([]uuid.UUID, len(req.LabelIDs))
		for i, lid := range req.LabelIDs {
			labelUUIDs[i], _ = uuid.Parse(lid)
		}
		if err := s.issueRepo.SetLabels(ctx, issue.ID, labelUUIDs); err != nil {
			log.WithError(err).Warn("failed to set labels")
		}

		newLabels, _ := s.issueRepo.GetLabels(ctx, issue.ID)
		newNames := make([]string, len(newLabels))
		for i, l := range newLabels {
			newNames[i] = l.Name
		}
		oldStr := strings.Join(oldNames, ", ")
		newStr := strings.Join(newNames, ", ")
		if oldStr != newStr {
			s.recordHistory(ctx, issue.ID, userID, "labels", &oldStr, &newStr)
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
	if statusChanged {
		s.applyStatusAutomation(ctx, workspaceID, userID, issue, map[uuid.UUID]bool{})
	}

	// Send notifications for field changes
	s.sendUpdateNotifications(ctx, issue, userID, req, oldDescription)

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
	if issue.ParentID != nil {
		if parent, _ := s.issueRepo.GetByID(ctx, *issue.ParentID); parent != nil {
			s.hub.Broadcast(workspaceID, realtime.Event{Type: "issue.updated", Payload: parent})
		}
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
		old := string(issue.Status)
		issue.Status = domain.IssueStatusCancelled
		newVal := string(domain.IssueStatusCancelled)
		s.recordHistory(ctx, issue.ID, userID, "status", &old, &newVal)
		// Look up the team's cancelled status
		cancelledStatus, _ := s.teamStatusRepo.GetByTeamAndSlug(ctx, issue.TeamID, string(domain.IssueStatusCancelled))
		if cancelledStatus != nil {
			issue.StatusID = &cancelledStatus.ID
		}
	}

	if err := s.issueRepo.Update(ctx, issue); err != nil {
		return nil, err
	}
	if !accept {
		s.applyStatusAutomation(ctx, workspaceID, userID, issue, map[uuid.UUID]bool{})
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

func (s *IssueService) CountSubIssuesForIssues(ctx context.Context, issueIDs []uuid.UUID) (map[uuid.UUID]domain.SubIssueCount, error) {
	return s.issueRepo.CountSubIssuesForIssues(ctx, issueIDs)
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

	var statusID *uuid.UUID
	if req.StatusID != nil {
		sid, err := uuid.Parse(*req.StatusID)
		if err != nil {
			return 0, fmt.Errorf("invalid status_id")
		}
		statusID = &sid
	}

	if req.ParentID != nil {
		return s.bulkUpdateParent(ctx, workspaceID, userID, issueIDs, *req.ParentID)
	}

	n, err := s.issueRepo.BulkUpdate(ctx, workspaceID, issueIDs, req.Status, req.Priority, assigneeID, statusID)
	if err != nil {
		return 0, err
	}
	if req.Status != nil || req.StatusID != nil {
		for _, id := range issueIDs {
			issue, err := s.issueRepo.GetByID(ctx, id)
			if err == nil && issue != nil && issue.WorkspaceID == workspaceID {
				s.applyStatusAutomation(ctx, workspaceID, userID, issue, map[uuid.UUID]bool{})
			}
		}
	}

	s.hub.Broadcast(workspaceID, realtime.Event{
		Type:    "issues.bulk_updated",
		Payload: map[string]interface{}{"count": n},
	})

	return n, nil
}

func (s *IssueService) validateParentID(ctx context.Context, workspaceID, issueID uuid.UUID, rawParentID string) (*uuid.UUID, *domain.Issue, error) {
	if rawParentID == "" {
		return nil, nil, nil
	}
	parentID, err := uuid.Parse(rawParentID)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid parent_id")
	}
	if issueID != uuid.Nil && parentID == issueID {
		return nil, nil, fmt.Errorf("issue cannot be its own parent")
	}
	parent, err := s.issueRepo.GetByID(ctx, parentID)
	if err != nil || parent == nil {
		return nil, nil, fmt.Errorf("parent issue not found")
	}
	if parent.WorkspaceID != workspaceID {
		return nil, nil, fmt.Errorf("parent issue must belong to the same workspace")
	}
	if issueID != uuid.Nil {
		cycle, err := s.issueRepo.WouldCreateCycle(ctx, issueID, parentID)
		if err != nil {
			return nil, nil, err
		}
		if cycle {
			return nil, nil, fmt.Errorf("parent would create a sub-issue cycle")
		}
	}
	return &parentID, parent, nil
}

func (s *IssueService) inheritSubIssueAssignees(ctx context.Context, parent *domain.Issue, creatorID uuid.UUID) []string {
	parentAssignees, _ := s.issueRepo.GetAssignees(ctx, parent.ID)
	if len(parentAssignees) == 0 && parent.AssigneeID != nil {
		parentAssignees = []uuid.UUID{*parent.AssigneeID}
	}
	if len(parentAssignees) == 0 {
		return nil
	}
	for _, id := range parentAssignees {
		if id == creatorID {
			return []string{creatorID.String()}
		}
	}
	if len(parentAssignees) != 1 {
		return nil
	}

	subIssues, err := s.issueRepo.ListSubIssues(ctx, parent.ID)
	if err != nil {
		return nil
	}
	for _, sub := range subIssues {
		if sub.AssigneeID == nil || *sub.AssigneeID != parentAssignees[0] {
			return nil
		}
	}
	return []string{parentAssignees[0].String()}
}

func (s *IssueService) bulkUpdateParent(ctx context.Context, workspaceID, userID uuid.UUID, issueIDs []uuid.UUID, rawParentID string) (int, error) {
	issues := make([]*domain.Issue, 0, len(issueIDs))
	var parentID *uuid.UUID
	if rawParentID != "" {
		parsedParentID, _, err := s.validateParentID(ctx, workspaceID, uuid.Nil, rawParentID)
		if err != nil {
			return 0, err
		}
		parentID = parsedParentID
	}

	for _, id := range issueIDs {
		issue, err := s.issueRepo.GetByID(ctx, id)
		if err != nil || issue == nil || issue.WorkspaceID != workspaceID {
			return 0, fmt.Errorf("issue not found")
		}
		if parentID != nil {
			if *parentID == issue.ID {
				return 0, fmt.Errorf("issue cannot be its own parent")
			}
			cycle, err := s.issueRepo.WouldCreateCycle(ctx, issue.ID, *parentID)
			if err != nil {
				return 0, err
			}
			if cycle {
				return 0, fmt.Errorf("parent would create a sub-issue cycle")
			}
		}
		issues = append(issues, issue)
	}

	updated := 0
	for _, issue := range issues {
		old := ""
		if issue.ParentID != nil {
			old = issue.ParentID.String()
		}
		newVal := ""
		if parentID != nil {
			newVal = parentID.String()
		}
		if old == newVal {
			continue
		}
		issue.ParentID = parentID
		if err := s.issueRepo.Update(ctx, issue); err != nil {
			return updated, err
		}
		s.recordHistory(ctx, issue.ID, userID, "parent", &old, &newVal)
		updated++
		s.hub.Broadcast(workspaceID, realtime.Event{Type: "issue.updated", Payload: issue})
	}

	return updated, nil
}

func (s *IssueService) applyStatusAutomation(ctx context.Context, workspaceID, actorID uuid.UUID, issue *domain.Issue, visited map[uuid.UUID]bool) {
	if issue == nil || visited[issue.ID] {
		return
	}
	visited[issue.ID] = true

	category := s.issueStatusCategory(ctx, issue)
	if category == domain.StatusCategoryCompleted {
		team, err := s.teamRepo.GetByID(ctx, issue.TeamID)
		if err == nil && team != nil && team.SubIssueAutoCloseEnabled {
			s.autoCloseSubIssues(ctx, workspaceID, actorID, issue.ID, visited)
		}
	}

	if issue.ParentID != nil {
		s.maybeAutoCloseParent(ctx, workspaceID, actorID, *issue.ParentID, visited)
	}
}

func (s *IssueService) maybeAutoCloseParent(ctx context.Context, workspaceID, actorID, parentID uuid.UUID, visited map[uuid.UUID]bool) {
	parent, err := s.issueRepo.GetByID(ctx, parentID)
	if err != nil || parent == nil || parent.WorkspaceID != workspaceID || visited[parent.ID] {
		return
	}
	team, err := s.teamRepo.GetByID(ctx, parent.TeamID)
	if err != nil || team == nil || !team.ParentAutoCloseEnabled {
		return
	}
	total, done, err := s.issueRepo.CountSubIssues(ctx, parent.ID)
	if err != nil || total == 0 || total != done {
		return
	}
	s.moveIssueToCompleted(ctx, workspaceID, actorID, parent, visited)
}

func (s *IssueService) autoCloseSubIssues(ctx context.Context, workspaceID, actorID, parentID uuid.UUID, visited map[uuid.UUID]bool) {
	subIssues, err := s.issueRepo.ListSubIssues(ctx, parentID)
	if err != nil {
		return
	}
	for i := range subIssues {
		sub := subIssues[i]
		if sub.WorkspaceID != workspaceID || s.isTerminalStatus(ctx, &sub) {
			continue
		}
		s.moveIssueToCompleted(ctx, workspaceID, actorID, &sub, visited)
	}
}

func (s *IssueService) moveIssueToCompleted(ctx context.Context, workspaceID, actorID uuid.UUID, issue *domain.Issue, visited map[uuid.UUID]bool) {
	if issue == nil || s.isTerminalStatus(ctx, issue) {
		return
	}
	completedStatus, err := s.completedStatusForTeam(ctx, issue.TeamID)
	if err != nil || completedStatus == nil {
		return
	}

	old := string(issue.Status)
	issue.Status = domain.IssueStatus(completedStatus.Slug)
	issue.StatusID = &completedStatus.ID
	if err := s.issueRepo.Update(ctx, issue); err != nil {
		log.WithError(err).WithField("issue_id", issue.ID).Warn("failed to auto-close issue")
		return
	}
	newVal := completedStatus.Slug
	s.recordHistory(ctx, issue.ID, actorID, "status", &old, &newVal)
	s.hub.Broadcast(workspaceID, realtime.Event{Type: "issue.updated", Payload: issue})
	s.applyStatusAutomation(ctx, workspaceID, actorID, issue, visited)
}

func (s *IssueService) completedStatusForTeam(ctx context.Context, teamID uuid.UUID) (*domain.TeamStatus, error) {
	status, err := s.teamStatusRepo.GetByTeamAndSlug(ctx, teamID, string(domain.IssueStatusDone))
	if err == nil && status != nil {
		return status, nil
	}
	statuses, err := s.teamStatusRepo.ListByTeam(ctx, teamID)
	if err != nil {
		return nil, err
	}
	for i := range statuses {
		if statuses[i].Category == domain.StatusCategoryCompleted {
			return &statuses[i], nil
		}
	}
	return nil, nil
}

func (s *IssueService) isTerminalStatus(ctx context.Context, issue *domain.Issue) bool {
	category := s.issueStatusCategory(ctx, issue)
	return category == domain.StatusCategoryCompleted || category == domain.StatusCategoryCancelled
}

func (s *IssueService) issueStatusCategory(ctx context.Context, issue *domain.Issue) domain.StatusCategory {
	if issue.StatusID != nil {
		status, err := s.teamStatusRepo.GetByID(ctx, *issue.StatusID)
		if err == nil && status != nil {
			return status.Category
		}
	}
	switch issue.Status {
	case domain.IssueStatusDone:
		return domain.StatusCategoryCompleted
	case domain.IssueStatusCancelled:
		return domain.StatusCategoryCancelled
	case domain.IssueStatusInProgress, domain.IssueStatusInReview:
		return domain.StatusCategoryStarted
	case domain.IssueStatusTodo:
		return domain.StatusCategoryUnstarted
	default:
		return domain.StatusCategoryBacklog
	}
}

func (s *IssueService) BulkDelete(ctx context.Context, workspaceID, userID uuid.UUID, canDeleteAny bool, req dto.BulkDeleteIssueRequest) (int, error) {
	issueIDs := make([]uuid.UUID, len(req.IssueIDs))
	for i, id := range req.IssueIDs {
		parsed, err := uuid.Parse(id)
		if err != nil {
			return 0, fmt.Errorf("invalid issue_id: %s", id)
		}
		issueIDs[i] = parsed
	}

	// Members can only delete their own issues
	if !canDeleteAny {
		for _, id := range issueIDs {
			issue, err := s.issueRepo.GetByID(ctx, id)
			if err != nil || issue == nil {
				return 0, fmt.Errorf("issue not found")
			}
			if issue.CreatorID != userID {
				return 0, fmt.Errorf("forbidden")
			}
		}
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

func (s *IssueService) notify(ctx context.Context, userID uuid.UUID, issue *domain.Issue, notifType, title string) {
	if err := s.notifSvc.Create(ctx, userID, issue.WorkspaceID, &issue.ID, notifType, title); err != nil {
		log.WithError(err).Warn("failed to create notification")
		return
	}
	s.hub.BroadcastToUser(issue.WorkspaceID, userID, realtime.Event{
		Type:    "notification.created",
		Payload: map[string]string{"type": notifType},
	})
}

func (s *IssueService) notifyAssignees(ctx context.Context, issue *domain.Issue, actorID, workspaceID uuid.UUID) {
	notified := make(map[uuid.UUID]bool)

	// Single assignee
	if issue.AssigneeID != nil && *issue.AssigneeID != actorID {
		s.notify(ctx, *issue.AssigneeID, issue, "assigned",
			fmt.Sprintf("You were assigned to %s: %s", issue.Identifier, issue.Title))
		notified[*issue.AssigneeID] = true
	}

	// Multi-assignees
	assignees, _ := s.issueRepo.GetAssignees(ctx, issue.ID)
	for _, uid := range assignees {
		if uid != actorID && !notified[uid] {
			s.notify(ctx, uid, issue, "assigned",
				fmt.Sprintf("You were assigned to %s: %s", issue.Identifier, issue.Title))
		}
	}
}

func (s *IssueService) sendUpdateNotifications(ctx context.Context, issue *domain.Issue, actorID uuid.UUID, req dto.UpdateIssueRequest, oldDescription string) {
	// Build recipient list: all assignees except the actor
	assignees, _ := s.issueRepo.GetAssignees(ctx, issue.ID)
	recipients := make([]uuid.UUID, 0, len(assignees))
	seen := make(map[uuid.UUID]bool)
	for _, uid := range assignees {
		if uid != actorID && !seen[uid] {
			recipients = append(recipients, uid)
			seen[uid] = true
		}
	}
	if issue.AssigneeID != nil && *issue.AssigneeID != actorID && !seen[*issue.AssigneeID] {
		recipients = append(recipients, *issue.AssigneeID)
		seen[*issue.AssigneeID] = true
	}

	// Assignee change: notify the new assignee specifically
	if req.AssigneeID != nil {
		newAID, err := uuid.Parse(*req.AssigneeID)
		if err == nil && newAID != actorID {
			s.notify(ctx, newAID, issue, "assigned",
				fmt.Sprintf("You were assigned to %s: %s", issue.Identifier, issue.Title))
		}
	}

	// Mention notifications from description (always, regardless of assignees).
	// Only notify for NEW mentions — skip users already mentioned in the old description.
	if req.Description != nil {
		oldMentions := make(map[uuid.UUID]bool)
		for _, uid := range extractMentionedUserIDs(oldDescription) {
			oldMentions[uid] = true
		}
		for _, uid := range extractMentionedUserIDs(*req.Description) {
			if uid == actorID || seen[uid] || oldMentions[uid] {
				continue
			}
			s.notify(ctx, uid, issue, "mentioned",
				fmt.Sprintf("You were mentioned in %s: %s", issue.Identifier, issue.Title))
			seen[uid] = true
		}
	}

	if len(recipients) == 0 {
		return
	}

	// Status change
	if req.StatusID != nil || req.Status != nil {
		for _, uid := range recipients {
			s.notify(ctx, uid, issue, "status_changed",
				fmt.Sprintf("%s status changed to %s", issue.Identifier, issue.Status))
		}
	}

	// Priority change
	if req.Priority != nil {
		for _, uid := range recipients {
			s.notify(ctx, uid, issue, "priority_changed",
				fmt.Sprintf("%s priority changed", issue.Identifier))
		}
	}

	// Due date change
	if req.DueDate != nil {
		for _, uid := range recipients {
			s.notify(ctx, uid, issue, "due_date_changed",
				fmt.Sprintf("%s due date changed", issue.Identifier))
		}
	}

	// Cycle change
	if req.CycleID != nil {
		for _, uid := range recipients {
			s.notify(ctx, uid, issue, "cycle_changed",
				fmt.Sprintf("%s cycle changed", issue.Identifier))
		}
	}

	// Label change
	if req.LabelIDs != nil {
		for _, uid := range recipients {
			s.notify(ctx, uid, issue, "label_added",
				fmt.Sprintf("%s labels updated", issue.Identifier))
		}
	}
}
