package service

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
)

const bulkUpdateAction = "bulk_update_issues"

const bulkRowParameters = `{"type":"object","properties":{"identifier":{"type":"string","minLength":1,"maxLength":32},"status":{"type":"string","enum":["backlog","todo","in_progress","in_review","done","cancelled"]},"priority":{"type":"integer","minimum":0,"maximum":4},"assignee_id":{"type":"string"},"label_ids":{"type":"array","items":{"type":"string","format":"uuid"},"uniqueItems":true},"cycle_id":{"type":"string"},"project_id":{"type":"string"}},"required":["identifier"],"minProperties":2,"additionalProperties":false}`

func bulkUpdateParameters(rowSchema string) string {
	return `{"type":"object","properties":{"changes":{"type":"array","minItems":1,"maxItems":100,"items":` + rowSchema + `}},"required":["changes"],"additionalProperties":false}`
}

var bulkUpdateTool = assistantTool{
	name: bulkUpdateAction, approval: true, write: true,
	description: "When asked to change several issues, including closing them, propose all their changes in one call for one approval. Use done to close issues. label_ids replaces all labels; assignee_id replaces the assignment. Use [] for no labels and an empty string to clear the assignee, cycle or project. Report approval as pending until the user decides.",
	parameters:  bulkUpdateParameters(bulkRowParameters),
}

// Keep the proposed rows as submitted. Decode them independently so one bad
// field does not hide the other proposals or prevent them from being applied.
type bulkUpdatePayload struct {
	Changes  []json.RawMessage `json:"changes"`
	InFlight bool              `json:"in_flight,omitempty"`
	Results  []bulkIssueResult `json:"results,omitempty"`
}

type bulkIssueResult struct {
	Identifier string `json:"identifier"`
	Success    bool   `json:"success"`
	Error      string `json:"error,omitempty"`
}

func prepareBulkUpdate(arguments string) (*bulkUpdatePayload, error) {
	var input struct {
		Changes []json.RawMessage `json:"changes"`
	}
	if err := decodeToolInput(arguments, &input); err != nil {
		return nil, err
	}
	if len(input.Changes) < 1 || len(input.Changes) > 100 {
		return nil, toolFailure("give 1 to 100 issue changes")
	}
	return &bulkUpdatePayload{Changes: input.Changes}, nil
}

func (t *AssistantTools) SetIssueLabels(labels repository.LabelRepo) { t.issueLabels = labels }

func (t *AssistantTools) applyBulkChange(ctx context.Context, ws, actor uuid.UUID, raw json.RawMessage) bulkIssueResult {
	var input struct {
		Identifier string   `json:"identifier"`
		Status     *string  `json:"status"`
		Priority   *int     `json:"priority"`
		AssigneeID *string  `json:"assignee_id"`
		LabelIDs   []string `json:"label_ids"`
		CycleID    *string  `json:"cycle_id"`
		ProjectID  *string  `json:"project_id"`
	}
	err := decodeToolInput(string(raw), &input)
	result := bulkIssueResult{Identifier: truncateRunes(strings.ToUpper(strings.TrimSpace(input.Identifier)), 32)}
	fail := func(message string) bulkIssueResult {
		result.Error = message
		return result
	}
	if schemaErr := validateAssistantArguments(bulkUpdateAction, assistantArgumentSchemas()["bulk-row"], string(raw)); schemaErr != nil {
		return fail(toolErrorText(schemaErr))
	}
	if err != nil {
		return fail(toolArgumentErrorText(bulkUpdateAction, assistantArgumentSchemas()["bulk-row"], err))
	}
	if result.Identifier == "" || len(input.Identifier) > 32 {
		return fail("invalid issue identifier")
	}
	if _, err := t.authorizeTool(ctx, ws, actor, bulkUpdateTool); err != nil {
		return fail(toolErrorText(err))
	}
	request := dto.UpdateIssueRequest{Status: input.Status, Priority: input.Priority, LabelIDs: input.LabelIDs, CycleID: input.CycleID, ProjectID: input.ProjectID}
	for _, ref := range []*string{input.CycleID, input.ProjectID} {
		if ref != nil && *ref != "" && uuid.Validate(*ref) != nil {
			return fail("invalid cycle or project ID")
		}
	}
	var assignees []uuid.UUID
	if input.AssigneeID != nil {
		request.AssigneeIDs = []string{}
		if *input.AssigneeID != "" {
			id, err := uuid.Parse(*input.AssigneeID)
			if err != nil {
				return fail("invalid assignee ID")
			}
			member, err := t.members.GetMember(ctx, ws, id)
			if err != nil || member == nil {
				return fail("assignee not found in this workspace")
			}
			request.AssigneeIDs = append(request.AssigneeIDs, id.String())
			assignees = append(assignees, id)
		}
	}
	var labels []uuid.UUID
	for _, value := range input.LabelIDs {
		id, err := uuid.Parse(value)
		if err != nil {
			return fail("invalid label ID")
		}
		if t.issueLabels == nil {
			return fail("labels are unavailable")
		}
		if slices.Contains(labels, id) {
			return fail("duplicate label ID")
		}
		label, err := t.issueLabels.GetByID(ctx, id)
		if err != nil || label == nil || label.WorkspaceID != ws {
			return fail("label not found in this workspace")
		}
		labels = append(labels, id)
	}
	_, id, err := t.updateIssue(domain.WithActingAgent(ctx, "assistant"), ws, actor, result.Identifier, request, assistantIssueFields{})
	if err != nil {
		return fail(toolErrorText(err))
	}
	// The issue service treats junction-table writes as best effort. Confirm
	// the requested sets before reporting this row as successful.
	if input.LabelIDs != nil {
		got, err := t.issues.GetLabels(ctx, *id)
		var actual []uuid.UUID
		for _, label := range got {
			actual = append(actual, label.ID)
		}
		if err != nil || !sameUUIDSet(labels, actual) {
			return fail("could not apply the requested labels")
		}
	}
	if input.AssigneeID != nil {
		actual, err := t.issues.GetAssignees(ctx, *id)
		if err != nil || !sameUUIDSet(assignees, actual) {
			return fail("could not apply the requested assignee")
		}
	}
	result.Success = true
	return result
}

func (s *AssistantApprovals) bulkUpdate(ctx context.Context, ws, conversation uuid.UUID, approval *domain.ConversationApproval) (*domain.ConversationApproval, error) {
	unlock, held, err := s.conversations.LockApprovalEffect(ctx, approval.ID)
	if err != nil || !held {
		return approval, err
	}
	defer unlock()
	status, data, err := s.conversations.ApprovalState(ctx, approval.ID)
	if err != nil {
		return nil, err
	}
	if status != "approving" {
		current, _, err := s.conversations.ClaimApproval(ctx, ws, conversation, approval.ID, true)
		return current, err
	}
	var payload bulkUpdatePayload
	if s.issueTools == nil || json.Unmarshal(data, &payload) != nil || len(payload.Changes) == 0 || len(payload.Changes) > 100 || len(payload.Results) > len(payload.Changes) {
		return s.conversations.FinishApproval(ctx, ws, conversation, approval.ID, nil, "This approval cannot be executed")
	}
	// Checkpoint before each effect. If a process dies between the issue write
	// and its receipt, a retry must not overwrite a newer edit to that issue.
	save := func() error {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		_, err = s.conversations.RecordApprovalProgress(ctx, ws, conversation, approval.ID, data)
		return err
	}
	for _, raw := range payload.Changes[len(payload.Results):] {
		var result bulkIssueResult
		if payload.InFlight {
			var row struct {
				Identifier string `json:"identifier"`
			}
			_ = json.Unmarshal(raw, &row)
			result = bulkIssueResult{Identifier: truncateRunes(row.Identifier, 32), Error: "Update interrupted; check this issue before retrying"}
		} else {
			payload.InFlight = true
			if err := save(); err != nil {
				return nil, err
			}
			result = s.issueTools.applyBulkChange(ctx, ws, approval.ActorID, raw)
		}
		payload.Results = append(payload.Results, result)
		payload.InFlight = false
	}
	if err := save(); err != nil {
		return nil, err
	}
	failed := 0
	for _, result := range payload.Results {
		if !result.Success {
			failed++
		}
	}
	failure := ""
	if failed > 0 {
		failure = fmt.Sprintf("%d of %d issue updates failed", failed, len(payload.Results))
	}
	return s.conversations.FinishApproval(ctx, ws, conversation, approval.ID, nil, failure)
}
