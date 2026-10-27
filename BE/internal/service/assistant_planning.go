package service

import (
	"context"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
)

type plannedBlocker struct {
	Identifier string `json:"identifier"`
	Title      string `json:"title"`
	Done       bool   `json:"done"`
}

// visibleBlockers returns each issue's blockers that the user can see. Hidden
// blockers are reported only as a count, never by identifier or title.
func (t *AssistantTools) visibleBlockers(ctx context.Context, ws uuid.UUID, issues []uuid.UUID) (map[uuid.UUID][]plannedBlocker, error) {
	blockers, err := t.dependencies.Blockers(ctx, ws, issues)
	if err != nil {
		return nil, err
	}
	visible := map[string]bool{}
	result := map[uuid.UUID][]plannedBlocker{}
	for _, issue := range issues {
		result[issue] = []plannedBlocker{}
	}
	for _, blocker := range blockers {
		seen, checked := visible[blocker.Identifier]
		if !checked {
			issue, err := t.issues.GetByIdentifier(ctx, ws, blocker.Identifier)
			seen = err == nil && issue != nil
			visible[blocker.Identifier] = seen
		}
		if !seen {
			if !blocker.Done {
				result[blocker.BlockedID] = append(result[blocker.BlockedID], plannedBlocker{Identifier: "hidden", Title: "An issue you cannot see"})
			}
			continue
		}
		result[blocker.BlockedID] = append(result[blocker.BlockedID], plannedBlocker{Identifier: blocker.Identifier, Title: truncateRunes(blocker.Title, 200), Done: blocker.Done})
	}
	return result, nil
}

// planIssues groups issues into waves: every issue in a wave depends only on
// earlier waves or on finished issues. Issues left over form a cycle.
func (t *AssistantTools) planIssues(ctx context.Context, ws uuid.UUID, arguments string) (any, *uuid.UUID, error) {
	if t.dependencies == nil {
		return nil, nil, toolFailure("planning is not available")
	}
	var input struct {
		Identifiers []string `json:"identifiers"`
	}
	if err := decodeToolInput(arguments, &input); err != nil {
		return nil, nil, err
	}
	if len(input.Identifiers) == 0 || len(input.Identifiers) > 20 {
		return nil, nil, toolFailure("plan between 1 and 20 issues")
	}
	issues := []domain.Issue{}
	notFound := []string{}
	for _, raw := range input.Identifiers {
		identifier := strings.ToUpper(strings.TrimSpace(raw))
		if slices.ContainsFunc(issues, func(issue domain.Issue) bool { return issue.Identifier == identifier }) {
			continue
		}
		issue, err := t.issues.GetByIdentifier(ctx, ws, identifier)
		if err != nil || issue == nil {
			notFound = append(notFound, truncateRunes(identifier, 32))
			continue
		}
		issues = append(issues, *issue)
	}
	ids := make([]uuid.UUID, len(issues))
	inPlan := map[string]bool{}
	for i, issue := range issues {
		ids[i] = issue.ID
		inPlan[issue.Identifier] = true
	}
	blockers, err := t.visibleBlockers(ctx, ws, ids)
	if err != nil {
		return nil, nil, err
	}
	placed := map[string]bool{}
	waves := [][]map[string]any{}
	for len(placed) < len(issues) {
		wave := []map[string]any{}
		for _, issue := range issues {
			if placed[issue.Identifier] {
				continue
			}
			ready, waiting := true, []string{}
			for _, blocker := range blockers[issue.ID] {
				if blocker.Done {
					continue
				}
				if inPlan[blocker.Identifier] && !placed[blocker.Identifier] {
					ready = false
				}
				if !inPlan[blocker.Identifier] {
					waiting = append(waiting, blocker.Identifier)
				}
			}
			if ready {
				entry := issueSummary(issue)
				if len(waiting) > 0 {
					entry["waiting_for_outside_plan"] = waiting
				}
				wave = append(wave, entry)
			}
		}
		if len(wave) == 0 {
			break
		}
		for _, entry := range wave {
			placed[entry["identifier"].(string)] = true
		}
		waves = append(waves, wave)
	}
	cycle := []string{}
	for _, issue := range issues {
		if !placed[issue.Identifier] {
			cycle = append(cycle, issue.Identifier)
		}
	}
	result := map[string]any{"waves": waves}
	if len(cycle) > 0 {
		result["cycle"] = cycle
	}
	if len(notFound) > 0 {
		result["not_found"] = notFound
	}
	return result, nil, nil
}

func (t *AssistantTools) linkIssues(ctx context.Context, ws uuid.UUID, arguments string) (any, *uuid.UUID, error) {
	if t.dependencies == nil || t.relations == nil {
		return nil, nil, toolFailure("planning is not available")
	}
	var input struct {
		Identifier string `json:"identifier"`
		BlockedBy  string `json:"blocked_by"`
	}
	if err := decodeToolInput(arguments, &input); err != nil {
		return nil, nil, err
	}
	issue, err := t.issues.GetByIdentifier(ctx, ws, strings.ToUpper(strings.TrimSpace(input.Identifier)))
	if err != nil || issue == nil {
		return nil, nil, toolFailure("issue not found")
	}
	blocker, err := t.issues.GetByIdentifier(ctx, ws, strings.ToUpper(strings.TrimSpace(input.BlockedBy)))
	if err != nil || blocker == nil {
		return nil, nil, toolFailure("blocking issue not found")
	}
	if issue.ID == blocker.ID {
		return nil, nil, toolFailure("an issue cannot block itself")
	}
	cycle, err := t.dependencies.Blocks(ctx, issue.ID, blocker.ID)
	if err != nil {
		return nil, nil, err
	}
	if cycle {
		return nil, nil, toolFailure("%s already depends on %s; this would create a cycle", blocker.Identifier, issue.Identifier)
	}
	if _, err = t.relations.Create(ctx, ws, issue.Identifier, dto.CreateIssueRelationRequest{RelatedIdentifier: blocker.Identifier, Type: "blocked_by"}); err != nil {
		return nil, nil, toolFailure("the dependency could not be recorded")
	}
	return map[string]any{"identifier": issue.Identifier, "blocked_by": blocker.Identifier}, &issue.ID, nil
}
