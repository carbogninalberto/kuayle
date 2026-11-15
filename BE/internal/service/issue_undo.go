package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/realtime"
	"github.com/kuayle/kuayle-backend/internal/repository"
)

// DefaultIssueDeleteGracePeriod is how long a deleted issue can be restored
// before the purge job removes it for good.
const DefaultIssueDeleteGracePeriod = 30 * 24 * time.Hour

var (
	// ErrIssueEditedMeanwhile: the issue changed after the edit being undone.
	ErrIssueEditedMeanwhile = repository.ErrEditedMeanwhile
	// ErrIssueUndoUnavailable: a previous value can no longer be applied.
	ErrIssueUndoUnavailable = repository.ErrUndoUnavailable
	// ErrIssueRestoreExpired: the deleted issue is past its restore period.
	ErrIssueRestoreExpired = repository.ErrRestoreExpired
	// ErrIssueForbidden: the caller may not change this issue this way.
	ErrIssueForbidden = errors.New("forbidden")
	// ErrIssueUndoInvalid: the undo request is malformed.
	ErrIssueUndoInvalid = errors.New("invalid undo request")
)

// SetDeleteGracePeriod sets how long deleted issues stay restorable.
func (s *IssueService) SetDeleteGracePeriod(grace time.Duration) {
	s.deleteGrace = grace
}

// DeleteGracePeriod is how long deleted issues stay restorable.
func (s *IssueService) DeleteGracePeriod() time.Duration {
	if s.deleteGrace <= 0 {
		return DefaultIssueDeleteGracePeriod
	}
	return s.deleteGrace
}

// updateUndo describes how to put back the fields an edit changed. Only
// status, priority, assignee(s), labels, project, cycle and the sort order
// that comes with a status move are undoable; an edit that also changed the
// title, description, due date or parent has no undo (those keep their own
// history), nor does an edit that changed nothing.
func updateUndo(before, after *domain.Issue, req dto.UpdateIssueRequest, changes *issueChangeSet, labelsBefore labelSnapshot, assigneesBefore []uuid.UUID) *dto.IssueUndo {
	for _, field := range []string{"title", "description", "due date", "parent"} {
		if changes.fieldSet[field] {
			return nil
		}
	}
	var prev dto.IssueUndoPrevious
	changed := false
	if changes.fieldSet["status"] && before.StatusID != nil {
		id := before.StatusID.String()
		prev.StatusID = &id
		changed = true
		if req.SortOrder != nil && before.SortOrder != after.SortOrder {
			order := before.SortOrder
			prev.SortOrder = &order
		}
	}
	if changes.fieldSet["priority"] {
		priority := int(before.Priority)
		prev.Priority = &priority
		changed = true
	}
	if changes.fieldSet["assignee"] {
		prev.AssigneeID = uuidOrEmpty(before.AssigneeID)
		changed = true
	}
	if changes.fieldSet["assignees"] {
		ids := uuidStrings(assigneesBefore)
		prev.AssigneeIDs = &ids
		changed = true
	}
	if changes.fieldSet["project"] {
		prev.ProjectID = uuidOrEmpty(before.ProjectID)
		changed = true
	}
	if changes.fieldSet["cycle"] {
		prev.CycleID = uuidOrEmpty(before.CycleID)
		changed = true
	}
	// A project move may drop labels scoped to the project left behind.
	if changes.fieldSet["labels"] || (changes.fieldSet["project"] && labelsBefore.ids != "") {
		ids := []string{}
		if labelsBefore.ids != "" {
			ids = strings.Split(labelsBefore.ids, ",")
		}
		prev.LabelIDs = &ids
		changed = true
	}
	if !changed {
		return nil
	}
	return &dto.IssueUndo{ExpectedVersion: after.Version, Previous: prev}
}

func uuidOrEmpty(id *uuid.UUID) *string {
	value := ""
	if id != nil {
		value = id.String()
	}
	return &value
}

func uuidStrings(ids []uuid.UUID) []string {
	values := make([]string, len(ids))
	for i, id := range ids {
		values[i] = id.String()
	}
	return values
}

// BulkUpdateWithUndo is BulkUpdate that also returns how to undo the whole
// edit as one step. Parent changes have no undo.
func (s *IssueService) BulkUpdateWithUndo(ctx context.Context, workspaceID, userID uuid.UUID, req dto.BulkUpdateIssueRequest) (int, *dto.BulkIssueUndo, error) {
	if req.ParentID != nil && req.ProjectID == nil {
		n, err := s.BulkUpdate(ctx, workspaceID, userID, req)
		return n, nil, err
	}
	ids := make([]uuid.UUID, 0, len(req.IssueIDs))
	for _, raw := range req.IssueIDs {
		if id, err := uuid.Parse(raw); err == nil {
			ids = append(ids, id)
		}
	}
	before, err := s.issueRepo.ListByIDs(ctx, workspaceID, ids)
	if err != nil {
		return 0, nil, err
	}
	var labelsBefore map[uuid.UUID][]domain.Label
	if req.ProjectID != nil {
		if labelsBefore, err = s.issueRepo.GetLabelsForIssues(ctx, ids); err != nil {
			return 0, nil, err
		}
	}
	n, err := s.BulkUpdate(ctx, workspaceID, userID, req)
	if err != nil || n == 0 {
		return n, nil, err
	}
	after, err := s.issueRepo.ListByIDs(ctx, workspaceID, ids)
	if err != nil {
		return n, nil, nil
	}
	var labelsAfter map[uuid.UUID][]domain.Label
	if req.ProjectID != nil {
		labelsAfter, _ = s.issueRepo.GetLabelsForIssues(ctx, ids)
	}
	afterByID := make(map[uuid.UUID]domain.Issue, len(after))
	for _, issue := range after {
		afterByID[issue.ID] = issue
	}
	undo := &dto.BulkIssueUndo{Items: []dto.BulkIssueUndoItem{}}
	for i := range before {
		old := &before[i]
		current, ok := afterByID[old.ID]
		if !ok || current.Version == old.Version {
			continue
		}
		var prev dto.IssueUndoPrevious
		if (req.StatusID != nil || req.Status != nil) && old.StatusID != nil {
			id := old.StatusID.String()
			prev.StatusID = &id
		}
		if req.Priority != nil {
			priority := int(old.Priority)
			prev.Priority = &priority
		}
		if req.AssigneeID != nil {
			prev.AssigneeID = uuidOrEmpty(old.AssigneeID)
		}
		if req.CycleID != nil {
			prev.CycleID = uuidOrEmpty(old.CycleID)
		}
		if req.ProjectID != nil {
			prev.ProjectID = uuidOrEmpty(old.ProjectID)
			if labelIDs(labelsBefore[old.ID]) != labelIDs(labelsAfter[old.ID]) {
				ids := []string{}
				for _, label := range labelsBefore[old.ID] {
					ids = append(ids, label.ID.String())
				}
				prev.LabelIDs = &ids
			}
		}
		undo.Items = append(undo.Items, dto.BulkIssueUndoItem{ID: old.ID.String(), ExpectedVersion: current.Version, Previous: prev})
	}
	if len(undo.Items) == 0 {
		return n, nil, nil
	}
	return n, undo, nil
}

func labelIDs(labels []domain.Label) string {
	ids := make([]string, len(labels))
	for i, label := range labels {
		ids[i] = label.ID.String()
	}
	return strings.Join(ids, ",")
}

// UndoIssue puts back the values an edit of one issue changed; see Undo.
func (s *IssueService) UndoIssue(ctx context.Context, workspaceID, userID uuid.UUID, identifier string, req dto.IssueUndo) (*domain.Issue, error) {
	issue, err := s.issueRepo.GetByIdentifier(ctx, workspaceID, identifier)
	if err != nil {
		return nil, err
	}
	if issue == nil {
		return nil, ErrIssueNotFound
	}
	undone, err := s.Undo(ctx, workspaceID, userID, []dto.BulkIssueUndoItem{{ID: issue.ID.String(), ExpectedVersion: req.ExpectedVersion, Previous: req.Previous}})
	if err != nil {
		return nil, err
	}
	return &undone[0], nil
}

// Undo puts back previous values as one step: every issue must still be at
// its expected version, else nothing changes (ErrIssueEditedMeanwhile). It
// records history and broadcasts issue.updated like any edit. Notifications
// already sent for the edit are not retracted, and the undo sends none.
func (s *IssueService) Undo(ctx context.Context, workspaceID, userID uuid.UUID, items []dto.BulkIssueUndoItem) ([]domain.Issue, error) {
	undos := make([]domain.IssueUndo, len(items))
	ids := make([]uuid.UUID, len(items))
	seen := make(map[uuid.UUID]bool, len(items))
	for i, item := range items {
		id, err := uuid.Parse(item.ID)
		if err != nil || seen[id] {
			return nil, ErrIssueUndoInvalid
		}
		seen[id] = true
		prev, err := parseUndoPrevious(item.Previous)
		if err != nil {
			return nil, err
		}
		undos[i] = domain.IssueUndo{IssueID: id, ExpectedVersion: item.ExpectedVersion, Previous: prev}
		ids[i] = id
	}
	before, err := s.issueRepo.ListByIDs(ctx, workspaceID, ids)
	if err != nil {
		return nil, err
	}
	beforeByID := make(map[uuid.UUID]domain.Issue, len(before))
	for _, issue := range before {
		beforeByID[issue.ID] = issue
	}
	labelsBefore := make(map[uuid.UUID]labelSnapshot)
	assigneesBefore := make(map[uuid.UUID][]uuid.UUID)
	for _, undo := range undos {
		if undo.Previous.LabelIDs != nil || undo.Previous.ProjectSet {
			labelsBefore[undo.IssueID] = s.labelSnapshot(ctx, undo.IssueID)
		}
		if undo.Previous.AssigneeIDs != nil {
			assigneesBefore[undo.IssueID], _ = s.issueRepo.GetAssignees(ctx, undo.IssueID)
		}
	}

	undone, err := s.issueRepo.ApplyUndo(repository.WithIssueHistoryActor(ctx, userID), workspaceID, undos)
	if errors.Is(err, repository.ErrBulkIssueNotFound) {
		return nil, ErrIssueNotFound
	}
	if err != nil {
		return nil, err
	}

	for i := range undone {
		issue := &undone[i]
		old, ok := beforeByID[issue.ID]
		if !ok {
			continue
		}
		statusChanged := s.recordUndoHistory(ctx, userID, &old, issue)
		if labels, ok := labelsBefore[issue.ID]; ok {
			s.recordLabelChange(ctx, issue.ID, userID, labels)
		}
		if previous, ok := assigneesBefore[issue.ID]; ok {
			current, _ := s.issueRepo.GetAssignees(ctx, issue.ID)
			if !sameUUIDSet(previous, current) {
				oldStr, newStr := uuidListString(previous), uuidListString(current)
				s.recordHistory(ctx, issue.ID, userID, "assignees", &oldStr, &newStr)
			}
		}
		s.hub.Broadcast(workspaceID, realtime.Event{Type: "issue.updated", Payload: issue})
		if statusChanged {
			s.applyStatusAutomation(ctx, workspaceID, userID, issue, map[uuid.UUID]bool{})
		}
	}
	return undone, nil
}

// recordUndoHistory records the scalar fields an undo changed, as Update
// does, and reports whether the status changed.
func (s *IssueService) recordUndoHistory(ctx context.Context, userID uuid.UUID, old, issue *domain.Issue) bool {
	statusChanged := !sameUUIDPtr(old.StatusID, issue.StatusID)
	if statusChanged {
		oldName, newName := s.statusName(ctx, old.StatusID), s.statusName(ctx, issue.StatusID)
		s.recordHistory(ctx, issue.ID, userID, "status", &oldName, &newName)
	}
	if old.Priority != issue.Priority {
		oldValue, newValue := strconv.Itoa(int(old.Priority)), strconv.Itoa(int(issue.Priority))
		s.recordHistory(ctx, issue.ID, userID, "priority", &oldValue, &newValue)
	}
	for _, field := range []struct {
		name     string
		old, new *uuid.UUID
	}{{"assignee_id", old.AssigneeID, issue.AssigneeID}, {"project", old.ProjectID, issue.ProjectID}, {"cycle", old.CycleID, issue.CycleID}} {
		if !sameUUIDPtr(field.old, field.new) {
			s.recordHistory(ctx, issue.ID, userID, field.name, uuidOrEmpty(field.old), uuidOrEmpty(field.new))
		}
	}
	return statusChanged
}

func (s *IssueService) statusName(ctx context.Context, id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	if status, _ := s.teamStatusRepo.GetByID(ctx, *id); status != nil {
		return status.Name
	}
	return ""
}

func sameUUIDPtr(a, b *uuid.UUID) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

func parseUndoPrevious(in dto.IssueUndoPrevious) (domain.IssueUndoPrevious, error) {
	var out domain.IssueUndoPrevious
	optional := func(raw *string) (*uuid.UUID, error) {
		if raw == nil || *raw == "" {
			return nil, nil
		}
		id, err := uuid.Parse(*raw)
		if err != nil {
			return nil, ErrIssueUndoInvalid
		}
		return &id, nil
	}
	list := func(raw *[]string) (*[]uuid.UUID, error) {
		if raw == nil {
			return nil, nil
		}
		ids := make([]uuid.UUID, len(*raw))
		for i, value := range *raw {
			id, err := uuid.Parse(value)
			if err != nil {
				return nil, ErrIssueUndoInvalid
			}
			ids[i] = id
		}
		return &ids, nil
	}
	var err error
	if out.StatusID, err = optional(in.StatusID); err != nil {
		return out, err
	}
	if in.Priority != nil {
		if *in.Priority < 0 || *in.Priority > 4 {
			return out, ErrIssueUndoInvalid
		}
		priority := domain.IssuePriority(*in.Priority)
		out.Priority = &priority
	}
	out.AssigneeSet = in.AssigneeID != nil
	if out.AssigneeID, err = optional(in.AssigneeID); err != nil {
		return out, err
	}
	if out.AssigneeIDs, err = list(in.AssigneeIDs); err != nil {
		return out, err
	}
	if out.LabelIDs, err = list(in.LabelIDs); err != nil {
		return out, err
	}
	out.ProjectSet = in.ProjectID != nil
	if out.ProjectID, err = optional(in.ProjectID); err != nil {
		return out, err
	}
	out.CycleSet = in.CycleID != nil
	if out.CycleID, err = optional(in.CycleID); err != nil {
		return out, err
	}
	out.SortOrder = in.SortOrder
	if out.Empty() {
		return out, ErrIssueUndoInvalid
	}
	return out, nil
}

// GetDeletedByIdentifier returns a soft-deleted issue the caller can see, or nil.
func (s *IssueService) GetDeletedByIdentifier(ctx context.Context, workspaceID uuid.UUID, identifier string) (*domain.DeletedIssue, error) {
	return s.issueRepo.GetDeletedByIdentifier(ctx, workspaceID, identifier)
}

// RestoreIssue brings back a deleted issue within the grace period. Without
// canRestoreAny only the issue's creator may restore it, as for Delete.
func (s *IssueService) RestoreIssue(ctx context.Context, workspaceID, userID uuid.UUID, identifier string, canRestoreAny bool) (*domain.Issue, error) {
	deleted, err := s.issueRepo.GetDeletedByIdentifier(ctx, workspaceID, identifier)
	if err != nil {
		return nil, err
	}
	if deleted == nil {
		// Already restored (another tab, or a second Undo): nothing to do.
		if live, err := s.issueRepo.GetByIdentifier(ctx, workspaceID, identifier); err == nil && live != nil {
			if !canRestoreAny && live.CreatorID != userID {
				return nil, ErrIssueForbidden
			}
			return live, nil
		}
		return nil, ErrIssueNotFound
	}
	restored, err := s.restore(ctx, workspaceID, userID, []domain.DeletedIssue{*deleted}, canRestoreAny)
	if err != nil {
		return nil, err
	}
	if len(restored) == 0 {
		return &deleted.Issue, nil
	}
	return &restored[0], nil
}

// BulkRestore brings back deleted issues as one step: all of them or none.
func (s *IssueService) BulkRestore(ctx context.Context, workspaceID, userID uuid.UUID, canRestoreAny bool, req dto.BulkRestoreIssueRequest) (int, error) {
	ids := make([]uuid.UUID, 0, len(req.IssueIDs))
	seen := make(map[uuid.UUID]bool, len(req.IssueIDs))
	for _, raw := range req.IssueIDs {
		id, err := uuid.Parse(raw)
		if err != nil {
			return 0, fmt.Errorf("invalid issue_id: %s", raw)
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	deleted, err := s.issueRepo.ListDeletedByIDs(ctx, workspaceID, ids)
	if err != nil {
		return 0, err
	}
	if len(deleted) != len(ids) {
		// Some are live already or not visible: Restore below tells them apart.
		live, err := s.issueRepo.ListByIDs(ctx, workspaceID, ids)
		if err != nil {
			return 0, err
		}
		if len(live)+len(deleted) != len(ids) {
			return 0, ErrIssueNotFound
		}
		for _, issue := range live {
			if !canRestoreAny && issue.CreatorID != userID {
				return 0, ErrIssueForbidden
			}
		}
	}
	restored, err := s.restore(ctx, workspaceID, userID, deleted, canRestoreAny)
	return len(restored), err
}

func (s *IssueService) restore(ctx context.Context, workspaceID, userID uuid.UUID, deleted []domain.DeletedIssue, canRestoreAny bool) ([]domain.Issue, error) {
	if len(deleted) == 0 {
		return nil, nil
	}
	ids := make([]uuid.UUID, len(deleted))
	for i, issue := range deleted {
		if !canRestoreAny && issue.CreatorID != userID {
			return nil, ErrIssueForbidden
		}
		ids[i] = issue.ID
	}
	restored, err := s.issueRepo.Restore(ctx, workspaceID, ids, time.Now().Add(-s.DeleteGracePeriod()))
	if errors.Is(err, repository.ErrBulkIssueNotFound) {
		return nil, ErrIssueNotFound
	}
	if err != nil {
		return nil, err
	}
	parents := map[uuid.UUID]bool{}
	for i := range restored {
		issue := &restored[i]
		s.hub.Broadcast(workspaceID, realtime.Event{Type: "issue.restored", Payload: issue})
		if issue.ParentID != nil && !parents[*issue.ParentID] {
			parents[*issue.ParentID] = true
			if parent, _ := s.issueRepo.GetByID(ctx, *issue.ParentID); parent != nil {
				s.hub.Broadcast(workspaceID, realtime.Event{Type: "issue.updated", Payload: parent})
			}
		}
	}
	return restored, nil
}
