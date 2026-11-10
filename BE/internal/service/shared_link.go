package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/pkg/assettoken"
)

var protectedPublicAssetPattern = regexp.MustCompile(`(?i)(\b(?:src|href)=["'])(/api/workspaces/[^/"']+/assets/([0-9a-fA-F-]{36}))(\?download=1)?(["'])`)

type SharedLinkService struct {
	sharedLinkRepo   repository.SharedLinkRepo
	workspaceRepo    repository.WorkspaceRepo
	teamRepo         repository.TeamRepo
	projectRepo      repository.ProjectRepo
	viewRepo         repository.ViewRepo
	issueRepo        repository.IssueRepo
	userRepo         repository.UserRepo
	teamStatusRepo   repository.TeamStatusRepo
	assetTokenSecret string
}

func NewSharedLinkService(
	sharedLinkRepo repository.SharedLinkRepo,
	workspaceRepo repository.WorkspaceRepo,
	teamRepo repository.TeamRepo,
	projectRepo repository.ProjectRepo,
	viewRepo repository.ViewRepo,
	issueRepo repository.IssueRepo,
	userRepo repository.UserRepo,
	teamStatusRepo repository.TeamStatusRepo,
	jwtSecret string,
) *SharedLinkService {
	return &SharedLinkService{
		sharedLinkRepo:   sharedLinkRepo,
		workspaceRepo:    workspaceRepo,
		teamRepo:         teamRepo,
		projectRepo:      projectRepo,
		viewRepo:         viewRepo,
		issueRepo:        issueRepo,
		userRepo:         userRepo,
		teamStatusRepo:   teamStatusRepo,
		assetTokenSecret: jwtSecret + ":prompt-assets",
	}
}

func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// CanCreateSharedLink checks if the user's role meets the workspace's minimum role requirement.
func CanCreateSharedLink(userRole string, minRole string) bool {
	roleRank := map[string]int{
		domain.RoleGuest:  0,
		domain.RoleMember: 1,
		domain.RoleAdmin:  2,
		domain.RoleOwner:  3,
	}
	return roleRank[userRole] >= roleRank[minRole]
}

func (s *SharedLinkService) Create(ctx context.Context, workspaceID, userID uuid.UUID, req dto.CreateSharedLinkRequest) (*domain.SharedLink, error) {
	// Validate scope_id references exist
	var scopeID *uuid.UUID
	if req.ScopeID != nil {
		id, err := uuid.Parse(*req.ScopeID)
		if err != nil {
			return nil, fmt.Errorf("invalid scope_id")
		}
		scopeID = &id
	}

	switch domain.SharedLinkScope(req.Scope) {
	case domain.SharedLinkScopeWorkspace:
		// No scope_id needed
	case domain.SharedLinkScopeTeam:
		if scopeID == nil {
			return nil, fmt.Errorf("scope_id is required for team scope")
		}
		team, err := s.teamRepo.GetByID(domain.PublicContext(ctx), *scopeID)
		if err != nil || team == nil || team.WorkspaceID != workspaceID || team.IsPrivate {
			return nil, fmt.Errorf("team not found")
		}
	case domain.SharedLinkScopeProject:
		if scopeID == nil {
			return nil, fmt.Errorf("scope_id is required for project scope")
		}
		project, err := s.projectRepo.GetByID(domain.PublicContext(ctx), *scopeID)
		if err != nil || project == nil || project.WorkspaceID != workspaceID {
			return nil, fmt.Errorf("project not found")
		}
	case domain.SharedLinkScopeView:
		if scopeID == nil {
			return nil, fmt.Errorf("scope_id is required for view scope")
		}
		view, err := s.viewRepo.GetByID(ctx, *scopeID)
		if err != nil || view == nil || view.WorkspaceID != workspaceID {
			return nil, fmt.Errorf("view not found")
		}
	default:
		return nil, fmt.Errorf("invalid scope")
	}

	token, err := generateToken()
	if err != nil {
		return nil, fmt.Errorf("failed to generate token")
	}

	filters := req.Filters
	if filters == nil {
		filters = json.RawMessage(`{}`)
	}

	var expiresAt *time.Time
	if req.ExpiresAt != nil {
		t, err := time.Parse(time.RFC3339, *req.ExpiresAt)
		if err != nil {
			return nil, fmt.Errorf("invalid expires_at format, use RFC3339")
		}
		if t.Before(time.Now()) {
			return nil, fmt.Errorf("expires_at must be in the future")
		}
		expiresAt = &t
	}

	link := &domain.SharedLink{
		ID:                 uuid.New(),
		Token:              token,
		WorkspaceID:        workspaceID,
		CreatedBy:          userID,
		Scope:              domain.SharedLinkScope(req.Scope),
		ScopeID:            scopeID,
		Filters:            filters,
		IncludeDescription: req.IncludeDescription,
		IsActive:           true,
		ExpiresAt:          expiresAt,
	}

	if err := s.sharedLinkRepo.Create(ctx, link); err != nil {
		return nil, err
	}

	return link, nil
}

func (s *SharedLinkService) List(ctx context.Context, workspaceID uuid.UUID) ([]domain.SharedLink, error) {
	return s.sharedLinkRepo.ListByWorkspace(ctx, workspaceID)
}

func (s *SharedLinkService) Update(ctx context.Context, id, userID uuid.UUID, req dto.UpdateSharedLinkRequest) (*domain.SharedLink, error) {
	link, err := s.sharedLinkRepo.GetByID(ctx, id)
	if err != nil || link == nil {
		return nil, fmt.Errorf("shared link not found")
	}

	if req.IsActive != nil {
		link.IsActive = *req.IsActive
	}
	if req.IncludeDescription != nil {
		link.IncludeDescription = *req.IncludeDescription
	}
	if req.ExpiresAt != nil {
		if *req.ExpiresAt == "" {
			link.ExpiresAt = nil
		} else {
			t, err := time.Parse(time.RFC3339, *req.ExpiresAt)
			if err != nil {
				return nil, fmt.Errorf("invalid expires_at format, use RFC3339")
			}
			link.ExpiresAt = &t
		}
	}

	if err := s.sharedLinkRepo.Update(ctx, link); err != nil {
		return nil, err
	}

	return link, nil
}

func (s *SharedLinkService) Delete(ctx context.Context, id uuid.UUID) error {
	link, err := s.sharedLinkRepo.GetByID(ctx, id)
	if err != nil || link == nil {
		return fmt.Errorf("shared link not found")
	}
	return s.sharedLinkRepo.Delete(ctx, id)
}

// GetPublicMeta returns metadata about a shared link for the public view.
func (s *SharedLinkService) GetPublicMeta(ctx context.Context, token string) (*dto.PublicShareMetaResponse, error) {
	ctx = domain.PublicContext(ctx)
	link, err := s.resolveActiveLink(ctx, token)
	if err != nil {
		return nil, err
	}

	ws, err := s.workspaceRepo.GetByID(ctx, link.WorkspaceID)
	if err != nil || ws == nil {
		return nil, fmt.Errorf("not found")
	}

	scopeName := ws.Name
	var statuses []dto.PublicStatusResponse

	switch link.Scope {
	case domain.SharedLinkScopeTeam:
		if link.ScopeID != nil {
			team, err := s.teamRepo.GetByID(ctx, *link.ScopeID)
			if err != nil || team == nil {
				return nil, fmt.Errorf("not found")
			}
			scopeName = team.Name
			statuses = s.loadTeamStatuses(ctx, team.ID)
		}
	case domain.SharedLinkScopeProject:
		if link.ScopeID != nil {
			project, err := s.projectRepo.GetByID(ctx, *link.ScopeID)
			if err != nil || project == nil {
				return nil, fmt.Errorf("not found")
			}
			scopeName = project.Name
		}
	case domain.SharedLinkScopeView:
		if link.ScopeID != nil {
			view, err := s.viewRepo.GetByID(ctx, *link.ScopeID)
			if err != nil || view == nil {
				return nil, fmt.Errorf("not found")
			}
			scopeName = view.Name
		}
	}

	var scopeIDStr *string
	if link.ScopeID != nil {
		s := link.ScopeID.String()
		scopeIDStr = &s
	}

	return &dto.PublicShareMetaResponse{
		Scope:         string(link.Scope),
		ScopeID:       scopeIDStr,
		ScopeName:     scopeName,
		WorkspaceName: ws.Name,
		Filters:       link.Filters,
		Statuses:      statuses,
	}, nil
}

// ListPublicIssues returns sanitized issues for a public shared link.
func (s *SharedLinkService) ListPublicIssues(ctx context.Context, token string, queryParams dto.IssueFilterParams) (*dto.ListResponse[dto.PublicIssueResponse], error) {
	ctx = domain.PublicContext(ctx)
	link, err := s.resolveActiveLink(ctx, token)
	if err != nil {
		return nil, err
	}

	// Stored filters and scope first, then public parameters, which only
	// narrow. A share whose filters cannot be enforced shows nothing.
	params, ok := s.buildPublicFilterParams(link, queryParams)
	if !ok {
		return &dto.ListResponse[dto.PublicIssueResponse]{
			Data:    []dto.PublicIssueResponse{},
			Page:    params.Page,
			PerPage: params.PerPage,
		}, nil
	}

	issues, total, err := s.issueRepo.List(ctx, link.WorkspaceID, params)
	if err != nil {
		return nil, err
	}

	// Batch load labels, assignees, statuses
	issueIDs := make([]uuid.UUID, len(issues))
	for i, issue := range issues {
		issueIDs[i] = issue.ID
	}

	labelsMap, _ := s.issueRepo.GetLabelsForIssues(ctx, issueIDs)
	assigneesMap, _ := s.issueRepo.GetAssigneesForIssues(ctx, issueIDs)

	// Batch load statuses
	statusIDSet := make(map[uuid.UUID]struct{})
	for _, issue := range issues {
		if issue.StatusID != nil {
			statusIDSet[*issue.StatusID] = struct{}{}
		}
	}
	statusMap := make(map[uuid.UUID]*domain.TeamStatus)
	if len(statusIDSet) > 0 {
		statusIDs := make([]uuid.UUID, 0, len(statusIDSet))
		for id := range statusIDSet {
			statusIDs = append(statusIDs, id)
		}
		statuses, _ := s.teamStatusRepo.GetByIDs(ctx, statusIDs)
		for i := range statuses {
			statusMap[statuses[i].ID] = &statuses[i]
		}
	}

	// Build sanitized responses
	responses := make([]dto.PublicIssueResponse, len(issues))
	for i, issue := range issues {
		resp := dto.PublicIssueResponse{
			Identifier: issue.Identifier,
			Title:      issue.Title,
			Status:     string(issue.Status),
			Priority:   int(issue.Priority),
			DueDate:    issue.DueDate,
			CreatedAt:  issue.CreatedAt,
			UpdatedAt:  issue.UpdatedAt,
		}

		if link.IncludeDescription {
			if issue.Description != nil {
				description := signPublicAssetURLs(*issue.Description, s.assetTokenSecret, issue.WorkspaceID, issue.ID)
				resp.Description = &description
			}
		}

		// Status info
		if issue.StatusID != nil {
			if ts, ok := statusMap[*issue.StatusID]; ok {
				resp.StatusInfo = &dto.StatusInfoResponse{
					ID:       ts.ID.String(),
					Name:     ts.Name,
					Category: string(ts.Category),
					Color:    ts.Color,
					Position: ts.Position,
				}
			}
		}

		// Labels
		if labels, ok := labelsMap[issue.ID]; ok && len(labels) > 0 {
			resp.Labels = make([]dto.LabelResponse, len(labels))
			for j, l := range labels {
				resp.Labels[j] = dto.LabelResponse{
					ID:          l.ID.String(),
					Name:        l.Name,
					Color:       l.Color,
					Description: l.Description,
					CreatedAt:   l.CreatedAt,
					UpdatedAt:   l.UpdatedAt,
				}
				if l.ParentID != nil {
					pid := l.ParentID.String()
					resp.Labels[j].ParentID = &pid
				}
			}
		}

		// Assignees (sanitized — no email, no ID)
		if uids, ok := assigneesMap[issue.ID]; ok && len(uids) > 0 {
			resp.Assignees = make([]dto.PublicUserResponse, 0, len(uids))
			for _, uid := range uids {
				user, _ := s.userRepo.GetByID(ctx, uid)
				if user != nil {
					resp.Assignees = append(resp.Assignees, dto.PublicUserResponse{
						Name:        user.Name,
						DisplayName: user.DisplayName,
						AvatarURL:   user.AvatarURL,
					})
				}
			}
		}

		responses[i] = resp
	}

	return &dto.ListResponse[dto.PublicIssueResponse]{
		Data:       responses,
		TotalCount: total,
		Page:       params.Page,
		PerPage:    params.PerPage,
		HasMore:    params.Page*params.PerPage < total,
	}, nil
}

func signPublicAssetURLs(description, secret string, workspaceID, issueID uuid.UUID) string {
	return protectedPublicAssetPattern.ReplaceAllStringFunc(description, func(match string) string {
		parts := protectedPublicAssetPattern.FindStringSubmatch(match)
		if len(parts) != 6 {
			return match
		}
		assetID, err := uuid.Parse(parts[3])
		if err != nil {
			return match
		}
		token, _, err := assettoken.Generate(secret, assetID, workspaceID, issueID, time.Hour)
		if err != nil {
			return match
		}
		return parts[1] + "/api/public/assets/" + token + parts[4] + parts[5]
	})
}

// resolveActiveLink fetches a shared link by token and validates it's active and not expired.
func (s *SharedLinkService) resolveActiveLink(ctx context.Context, token string) (*domain.SharedLink, error) {
	link, err := s.sharedLinkRepo.GetByToken(ctx, token)
	if err != nil || link == nil {
		return nil, fmt.Errorf("not found")
	}
	if !link.IsActive {
		return nil, fmt.Errorf("not found")
	}
	if link.ExpiresAt != nil && link.ExpiresAt.Before(time.Now()) {
		return nil, fmt.Errorf("not found")
	}
	return link, nil
}

// buildPublicFilterParams builds the issue query of a public share from the
// link's scope, the stored filters (the view's and the link's) and the public
// query parameters. Stored filters are all enforced, and public parameters
// are combined with them so that they only narrow the result. It reports
// false when the share must show nothing: the view is gone, the stored
// filters cannot be read or enforced, or a public value excludes everything
// the stored filters allow.
func (s *SharedLinkService) buildPublicFilterParams(link *domain.SharedLink, query dto.IssueFilterParams) (dto.IssueFilterParams, bool) {
	params := dto.IssueFilterParams{PublicSearchDescription: link.IncludeDescription}
	params.Page = max(query.Page, 0)
	params.PerPage = min(max(query.PerPage, 0), 100)
	params.Defaults()

	// 1. Stored filters. For a view share the view's current value wins per
	// key; the link's own filters apply to the keys the view leaves open.
	stored, ok := parseStoredFilters(link.Filters)
	if !ok {
		return params, false
	}
	if link.Scope == domain.SharedLinkScopeView {
		if link.ScopeID == nil {
			return params, false
		}
		view, err := s.viewRepo.GetByID(context.Background(), *link.ScopeID)
		if err != nil || view == nil {
			// A deleted/disabled view must never become an unfiltered share.
			return params, false
		}
		viewFilters, ok := parseStoredFilters(view.Filters)
		if !ok {
			return params, false
		}
		for key, value := range viewFilters {
			stored[key] = value
		}
	}
	if !applyStoredFilters(&params, stored) {
		return params, false
	}

	// 2. The link's scope is fixed and overrides a stored team or project.
	switch link.Scope {
	case domain.SharedLinkScopeTeam:
		if link.ScopeID == nil {
			return params, false
		}
		params.TeamID = link.ScopeID.String()
	case domain.SharedLinkScopeProject:
		if link.ScopeID == nil {
			return params, false
		}
		params.ProjectID = link.ScopeID.String()
	}

	// 3. Public query parameters: each is intersected with the stored value
	// of its key, so it can remove issues from the share but never add any.
	if query.Status != "" {
		public, ok := normalizeStatusList(query.Status)
		if !ok {
			return params, false
		}
		if params.Status, ok = narrowList(params.Status, public); !ok {
			return params, false
		}
	}
	if query.Priority != "" {
		public, ok := normalizePriorityList(query.Priority)
		if !ok {
			return params, false
		}
		if params.Priority, ok = narrowList(params.Priority, public); !ok {
			return params, false
		}
	}
	if query.LabelID != "" {
		public, ok := normalizeFilterID(query.LabelID, true)
		if !ok || (params.LabelID != "" && params.LabelID != public) {
			return params, false
		}
		params.LabelID = public
	}
	if query.DueBefore != "" {
		public, ok := normalizeFilterDate(query.DueBefore)
		if !ok {
			return params, false
		}
		if params.DueBefore == "" || public < params.DueBefore {
			params.DueBefore = public
		}
	}
	if query.DueAfter != "" {
		public, ok := normalizeFilterDate(query.DueAfter)
		if !ok {
			return params, false
		}
		if params.DueAfter == "" || public > params.DueAfter {
			params.DueAfter = public
		}
	}
	// Search is added to a stored search, and looks only at fields the share
	// shows (see IssueFilterParams.PublicSearch).
	if term := strings.TrimSpace(query.Search); term != "" {
		params.PublicSearch = append(params.PublicSearch, term)
	}

	// Presentation. Grouping by assignee or project would order the page by
	// identifiers the share does not show.
	if query.GroupBy == "status" || query.GroupBy == "priority" {
		params.GroupBy = query.GroupBy
	}
	params.Sort = query.Sort
	params.Order = query.Order

	return params, true
}

// parseStoredFilters reads the filters of a link or a view: a flat JSON
// object of strings, as the application writes them. Empty values are no
// filter. Anything else is reported as unreadable.
func parseStoredFilters(raw json.RawMessage) (map[string]string, bool) {
	filters := map[string]string{}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return filters, true
	}
	var values map[string]any
	if err := json.Unmarshal(trimmed, &values); err != nil {
		return nil, false
	}
	for key, value := range values {
		text, ok := value.(string)
		if !ok {
			return nil, false
		}
		if text != "" {
			filters[key] = text
		}
	}
	return filters, true
}

// applyStoredFilters copies stored filters into the issue query. It reports
// false for a key or a value the query cannot enforce, so that an unknown
// filter never widens a share.
func applyStoredFilters(params *dto.IssueFilterParams, filters map[string]string) bool {
	for key, value := range filters {
		ok := true
		switch key {
		case "view_scope", "view_team", "group_by", "sort", "order", "status_category_order":
			// View metadata and presentation: they select no issues.
		case "status":
			var list []string
			if list, ok = normalizeStatusList(value); ok {
				params.Status = strings.Join(list, ",")
			}
		case "status_type":
			params.StatusType = value
		case "priority":
			var list []string
			if list, ok = normalizePriorityList(value); ok {
				params.Priority = strings.Join(list, ",")
			}
		case "assignee":
			params.AssigneeID, ok = normalizeFilterID(value, true)
		case "creator":
			params.CreatorID, ok = normalizeFilterID(value, false)
		case "team":
			params.TeamID, ok = normalizeFilterID(value, false)
		case "project":
			params.ProjectID, ok = normalizeFilterID(value, true)
		case "cycle":
			params.CycleID, ok = normalizeFilterID(value, true)
		case "label":
			params.LabelID, ok = normalizeFilterID(value, true)
		case "parent_id":
			params.ParentID, ok = normalizeFilterID(value, true)
		case "sub_issues":
			switch value {
			case "exclude", "top_level", "only", "has_sub_issues":
				params.SubIssues = value
			default:
				ok = false
			}
		case "triaged":
			if ok = value == "true" || value == "false"; ok {
				params.Triaged = value
			}
		case "search":
			if term := strings.TrimSpace(value); term != "" {
				params.PublicSearch = append(params.PublicSearch, term)
			}
		case "due_before":
			params.DueBefore, ok = normalizeFilterDate(value)
		case "due_after":
			params.DueAfter, ok = normalizeFilterDate(value)
		default:
			ok = false
		}
		if !ok {
			return false
		}
	}
	return true
}

// narrowList intersects a stored comma separated filter with a public one.
// With no stored value the public one applies as it is. It reports false
// when nothing is left.
func narrowList(stored string, public []string) (string, bool) {
	if stored == "" {
		return strings.Join(public, ","), len(public) > 0
	}
	kept := make([]string, 0, len(public))
	for _, value := range strings.Split(stored, ",") {
		if slices.Contains(public, value) {
			kept = append(kept, value)
		}
	}
	return strings.Join(kept, ","), len(kept) > 0
}

// normalizeStatusList accepts status ids or status slugs, not a mix: the
// issue query picks the column from the kind of value.
func normalizeStatusList(value string) ([]string, bool) {
	parts := strings.Split(value, ",")
	ids := 0
	for i, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, false
		}
		if id, ok := normalizeFilterID(part, false); ok {
			part = id
			ids++
		}
		parts[i] = part
	}
	return parts, ids == 0 || ids == len(parts)
}

func normalizePriorityList(value string) ([]string, bool) {
	parts := strings.Split(value, ",")
	for i, part := range parts {
		priority, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || priority < 0 || priority > 4 {
			return nil, false
		}
		parts[i] = strconv.Itoa(priority)
	}
	return parts, true
}

// normalizeFilterID accepts a UUID in its standard form, and "none" where the
// issue query understands it.
func normalizeFilterID(value string, allowNone bool) (string, bool) {
	if allowNone && value == "none" {
		return value, true
	}
	id, err := uuid.Parse(value)
	if err != nil || len(value) != 36 {
		return "", false
	}
	return id.String(), true
}

// normalizeFilterDate returns the day of a date or timestamp as YYYY-MM-DD,
// which also orders correctly as text.
func normalizeFilterDate(value string) (string, bool) {
	for _, layout := range []string{"2006-01-02", time.RFC3339} {
		if day, err := time.Parse(layout, value); err == nil {
			return day.Format("2006-01-02"), true
		}
	}
	return "", false
}

func (s *SharedLinkService) loadTeamStatuses(ctx context.Context, teamID uuid.UUID) []dto.PublicStatusResponse {
	statuses, err := s.teamStatusRepo.ListByTeam(ctx, teamID)
	if err != nil {
		return nil
	}
	result := make([]dto.PublicStatusResponse, len(statuses))
	for i, st := range statuses {
		result[i] = dto.PublicStatusResponse{
			ID:       st.ID.String(),
			Name:     st.Name,
			Category: string(st.Category),
			Color:    st.Color,
			Position: st.Position,
		}
	}
	return result
}
