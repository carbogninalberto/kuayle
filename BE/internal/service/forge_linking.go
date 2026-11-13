package service

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/realtime"
	"github.com/kuayle/kuayle-backend/internal/repository"
	log "github.com/sirupsen/logrus"
)

var issueIdentifierRegex = regexp.MustCompile(`(?i)([A-Z][A-Z0-9]+-\d+)`)

// forgeTransitionRules reads the workspace's automation rules. GitHub stores
// them and every forge reuses the same rows.
type forgeTransitionRules interface {
	GetAutoTransitionByEvent(ctx context.Context, workspaceID uuid.UUID, event string) (*domain.GitHubAutoTransition, error)
}

// forgeLinker holds the linking and status automation shared by the GitHub and
// generic forge webhook processors. It is deliberately independent of a
// provider so both paths behave identically.
type forgeLinker struct {
	issueRepo      repository.IssueRepo
	teamRepo       repository.TeamRepo
	teamStatusRepo repository.TeamStatusRepo
	historyRepo    repository.IssueHistoryRepo
	rules          forgeTransitionRules
	broadcast      func(uuid.UUID, realtime.Event)
}

func (l *forgeLinker) emit(workspaceID uuid.UUID, event realtime.Event) {
	if l.broadcast != nil {
		l.broadcast(workspaceID, event)
	}
}

func (l *forgeLinker) resolveIssueFromRef(ctx context.Context, workspaceID uuid.UUID, texts ...string) *domain.Issue {
	seen := make(map[string]bool)
	for _, text := range texts {
		matches := issueIdentifierRegex.FindAllString(text, -1)
		for _, m := range matches {
			if seen[m] {
				continue
			}
			seen[m] = true
			issue, err := l.issueRepo.GetByIdentifier(ctx, workspaceID, strings.ToUpper(m))
			if err == nil && issue != nil {
				return issue
			}
		}
	}
	return nil
}

// resolvePullRequestIssue requires exactly one visible issue among the
// candidate identifiers. Words such as SHA-256 and node-18 are not issues
// unless they exist in this workspace. Bound the work without choosing a
// partial/ambiguous result.
func (l *forgeLinker) resolvePullRequestIssue(ctx context.Context, workspaceID uuid.UUID, texts ...string) (*domain.Issue, error) {
	identifiers := make(map[string]bool)
	for _, text := range texts {
		for _, loc := range issueIdentifierRegex.FindAllStringIndex(text, -1) {
			isWord := func(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' }
			if loc[0] > 0 && isWord(text[loc[0]-1]) || loc[1] < len(text) && isWord(text[loc[1]]) {
				continue
			}
			identifiers[strings.ToUpper(text[loc[0]:loc[1]])] = true
			if len(identifiers) > 20 {
				return nil, nil
			}
		}
	}
	var resolved *domain.Issue
	for identifier := range identifiers {
		issue, err := l.issueRepo.GetByIdentifier(domain.PublicContext(ctx), workspaceID, identifier)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if issue == nil {
			continue
		}
		if resolved != nil && resolved.ID != issue.ID {
			return nil, nil
		}
		resolved = issue
	}
	return resolved, nil
}

// mergeRequestTransition is the pure decision of which automation rule a
// merge/pull request event applies. An updated, closed-without-merge or
// repeated open event changes no status.
func mergeRequestTransition(action string, inserted, merged bool) string {
	if merged {
		return "pr_merged"
	}
	if action == "opened" && inserted || action == "ready" {
		return "pr_opened"
	}
	return ""
}

func (l *forgeLinker) applyAutoTransition(ctx context.Context, workspaceID uuid.UUID, event string, issue *domain.Issue) {
	if issue == nil || l.rules == nil {
		return
	}
	rule, err := l.rules.GetAutoTransitionByEvent(ctx, workspaceID, event)
	if err != nil || rule == nil {
		return
	}

	oldStatus := string(issue.Status)
	newStatus := rule.TargetStatus
	if oldStatus == newStatus {
		return
	}

	issue.Status = domain.IssueStatus(newStatus)
	if rule.TargetStatusID != nil {
		issue.StatusID = rule.TargetStatusID
	} else {
		// Resolve the team's custom status ID from the slug.
		ts, err := l.teamStatusRepo.GetByTeamAndSlug(ctx, issue.TeamID, newStatus)
		if err == nil && ts != nil {
			issue.StatusID = &ts.ID
		}
	}

	if err := l.issueRepo.Update(ctx, issue); err != nil {
		log.WithError(err).WithField("issue_id", issue.ID).Warn("failed to auto-transition issue")
		return
	}

	_ = l.historyRepo.Create(ctx, issue.ID, uuid.Nil, "status", &oldStatus, &newStatus)

	l.emit(workspaceID, realtime.Event{
		Type:    "issue.updated",
		Payload: issue,
	})
	l.applyStatusAutomation(ctx, workspaceID, issue, map[uuid.UUID]bool{})
}

func (l *forgeLinker) applyStatusAutomation(ctx context.Context, workspaceID uuid.UUID, issue *domain.Issue, visited map[uuid.UUID]bool) {
	if issue == nil || visited[issue.ID] || l.teamRepo == nil {
		return
	}
	visited[issue.ID] = true

	category := l.issueStatusCategory(ctx, issue)
	if category == domain.StatusCategoryCompleted {
		team, err := l.teamRepo.GetByID(ctx, issue.TeamID)
		if err == nil && team != nil && team.SubIssueAutoCloseEnabled {
			l.autoCloseSubIssues(ctx, workspaceID, issue.ID, visited)
		}
	}
	if issue.ParentID != nil {
		l.maybeAutoCloseParent(ctx, workspaceID, *issue.ParentID, visited)
	}
}

func (l *forgeLinker) maybeAutoCloseParent(ctx context.Context, workspaceID, parentID uuid.UUID, visited map[uuid.UUID]bool) {
	parent, err := l.issueRepo.GetByID(ctx, parentID)
	if err != nil || parent == nil || parent.WorkspaceID != workspaceID || visited[parent.ID] || l.teamRepo == nil {
		return
	}
	team, err := l.teamRepo.GetByID(ctx, parent.TeamID)
	if err != nil || team == nil || !team.ParentAutoCloseEnabled {
		return
	}
	total, done, err := l.issueRepo.CountSubIssues(ctx, parent.ID)
	if err != nil || total == 0 || total != done {
		return
	}
	l.moveIssueToCompleted(ctx, workspaceID, parent, visited)
}

func (l *forgeLinker) autoCloseSubIssues(ctx context.Context, workspaceID, parentID uuid.UUID, visited map[uuid.UUID]bool) {
	subIssues, err := l.issueRepo.ListSubIssues(ctx, parentID)
	if err != nil {
		return
	}
	for i := range subIssues {
		sub := subIssues[i]
		if sub.WorkspaceID != workspaceID || l.isTerminalStatus(ctx, &sub) {
			continue
		}
		l.moveIssueToCompleted(ctx, workspaceID, &sub, visited)
	}
}

func (l *forgeLinker) moveIssueToCompleted(ctx context.Context, workspaceID uuid.UUID, issue *domain.Issue, visited map[uuid.UUID]bool) {
	if issue == nil || l.isTerminalStatus(ctx, issue) {
		return
	}
	completedStatus, err := l.completedStatusForTeam(ctx, issue.TeamID)
	if err != nil || completedStatus == nil {
		return
	}

	old := string(issue.Status)
	issue.Status = domain.IssueStatus(completedStatus.Slug)
	issue.StatusID = &completedStatus.ID
	if err := l.issueRepo.Update(ctx, issue); err != nil {
		log.WithError(err).WithField("issue_id", issue.ID).Warn("failed to auto-close issue")
		return
	}
	newVal := completedStatus.Slug
	_ = l.historyRepo.Create(ctx, issue.ID, uuid.Nil, "status", &old, &newVal)
	l.emit(workspaceID, realtime.Event{Type: "issue.updated", Payload: issue})
	l.applyStatusAutomation(ctx, workspaceID, issue, visited)
}

func (l *forgeLinker) completedStatusForTeam(ctx context.Context, teamID uuid.UUID) (*domain.TeamStatus, error) {
	status, err := l.teamStatusRepo.GetByTeamAndSlug(ctx, teamID, string(domain.IssueStatusDone))
	if err == nil && status != nil {
		return status, nil
	}
	statuses, err := l.teamStatusRepo.ListByTeam(ctx, teamID)
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

func (l *forgeLinker) isTerminalStatus(ctx context.Context, issue *domain.Issue) bool {
	category := l.issueStatusCategory(ctx, issue)
	return category == domain.StatusCategoryCompleted || category == domain.StatusCategoryCancelled
}

func (l *forgeLinker) issueStatusCategory(ctx context.Context, issue *domain.Issue) domain.StatusCategory {
	if issue.StatusID != nil {
		status, err := l.teamStatusRepo.GetByID(ctx, *issue.StatusID)
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

// linker builds the shared component from the GitHub service's dependencies.
// GitHub keeps thin methods with their original names so callers and tests are
// unchanged.
func (s *GitHubService) linker() *forgeLinker {
	return &forgeLinker{
		issueRepo:      s.issueRepo,
		teamRepo:       s.teamRepo,
		teamStatusRepo: s.teamStatusRepo,
		historyRepo:    s.historyRepo,
		rules:          s.ghRepo,
		broadcast:      s.broadcastRealtimeEvent,
	}
}

func (s *GitHubService) resolveIssueFromRef(ctx context.Context, workspaceID uuid.UUID, texts ...string) *domain.Issue {
	return s.linker().resolveIssueFromRef(ctx, workspaceID, texts...)
}

func (s *GitHubService) resolvePullRequestIssue(ctx context.Context, workspaceID uuid.UUID, texts ...string) (*domain.Issue, error) {
	return s.linker().resolvePullRequestIssue(ctx, workspaceID, texts...)
}

func (s *GitHubService) applyAutoTransition(ctx context.Context, workspaceID uuid.UUID, event string, issue *domain.Issue) {
	s.linker().applyAutoTransition(ctx, workspaceID, event, issue)
}

func (s *GitHubService) applyStatusAutomation(ctx context.Context, workspaceID uuid.UUID, issue *domain.Issue, visited map[uuid.UUID]bool) {
	s.linker().applyStatusAutomation(ctx, workspaceID, issue, visited)
}
