package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/google/uuid"
)

type SharedLinkService struct {
	sharedLinkRepo repository.SharedLinkRepo
	workspaceRepo  repository.WorkspaceRepo
	teamRepo       repository.TeamRepo
	projectRepo    repository.ProjectRepo
	viewRepo       repository.ViewRepo
	issueRepo      repository.IssueRepo
	userRepo       repository.UserRepo
	teamStatusRepo repository.TeamStatusRepo
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
) *SharedLinkService {
	return &SharedLinkService{
		sharedLinkRepo: sharedLinkRepo,
		workspaceRepo:  workspaceRepo,
		teamRepo:       teamRepo,
		projectRepo:    projectRepo,
		viewRepo:       viewRepo,
		issueRepo:      issueRepo,
		userRepo:       userRepo,
		teamStatusRepo: teamStatusRepo,
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
		team, err := s.teamRepo.GetByID(ctx, *scopeID)
		if err != nil || team == nil {
			return nil, fmt.Errorf("team not found")
		}
	case domain.SharedLinkScopeProject:
		if scopeID == nil {
			return nil, fmt.Errorf("scope_id is required for project scope")
		}
		project, err := s.projectRepo.GetByID(ctx, *scopeID)
		if err != nil || project == nil {
			return nil, fmt.Errorf("project not found")
		}
	case domain.SharedLinkScopeView:
		if scopeID == nil {
			return nil, fmt.Errorf("scope_id is required for view scope")
		}
		view, err := s.viewRepo.GetByID(ctx, *scopeID)
		if err != nil || view == nil {
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
			if err == nil && team != nil {
				scopeName = team.Name
				statuses = s.loadTeamStatuses(ctx, team.ID)
			}
		}
	case domain.SharedLinkScopeProject:
		if link.ScopeID != nil {
			project, err := s.projectRepo.GetByID(ctx, *link.ScopeID)
			if err == nil && project != nil {
				scopeName = project.Name
			}
		}
	case domain.SharedLinkScopeView:
		if link.ScopeID != nil {
			view, err := s.viewRepo.GetByID(ctx, *link.ScopeID)
			if err == nil && view != nil {
				scopeName = view.Name
			}
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
	link, err := s.resolveActiveLink(ctx, token)
	if err != nil {
		return nil, err
	}

	// Build filter params: start from link scope, then merge stored filters, then narrow with query params
	params := s.buildPublicFilterParams(link, queryParams)

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
			Estimate:   issue.Estimate,
			DueDate:    issue.DueDate,
			CreatedAt:  issue.CreatedAt,
			UpdatedAt:  issue.UpdatedAt,
		}

		if link.IncludeDescription {
			resp.Description = issue.Description
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

// buildPublicFilterParams merges the link's scope and stored filters with public query parameters.
// Public query params can only narrow, never widen the scope.
func (s *SharedLinkService) buildPublicFilterParams(link *domain.SharedLink, query dto.IssueFilterParams) dto.IssueFilterParams {
	params := dto.IssueFilterParams{}

	// 1. Set scope-based filters (immutable from link)
	switch link.Scope {
	case domain.SharedLinkScopeTeam:
		if link.ScopeID != nil {
			params.TeamID = link.ScopeID.String()
		}
	case domain.SharedLinkScopeProject:
		if link.ScopeID != nil {
			params.ProjectID = link.ScopeID.String()
		}
	case domain.SharedLinkScopeView:
		// For view scope, apply the view's stored filters
		if link.ScopeID != nil {
			view, err := s.viewRepo.GetByID(context.Background(), *link.ScopeID)
			if err == nil && view != nil {
				var viewFilters map[string]string
				if err := json.Unmarshal(view.Filters, &viewFilters); err == nil {
					applyStoredFilters(&params, viewFilters)
				}
			}
		}
	}

	// 2. Apply link's stored base filters (can further restrict)
	var storedFilters map[string]string
	if err := json.Unmarshal(link.Filters, &storedFilters); err == nil {
		applyStoredFilters(&params, storedFilters)
	}

	// 3. Layer on public query params (can only narrow, not replace scope params)
	if query.Status != "" {
		params.Status = query.Status
	}
	if query.Priority != "" {
		params.Priority = query.Priority
	}
	if query.LabelID != "" {
		params.LabelID = query.LabelID
	}
	if query.Search != "" {
		params.Search = query.Search
	}
	if query.DueBefore != "" {
		params.DueBefore = query.DueBefore
	}
	if query.DueAfter != "" {
		params.DueAfter = query.DueAfter
	}
	if query.GroupBy != "" {
		params.GroupBy = query.GroupBy
	}
	if query.Sort != "" {
		params.Sort = query.Sort
	}
	if query.Order != "" {
		params.Order = query.Order
	}

	// Pagination from query
	params.Page = query.Page
	params.PerPage = query.PerPage
	params.Defaults()

	return params
}

func applyStoredFilters(params *dto.IssueFilterParams, filters map[string]string) {
	if v, ok := filters["status"]; ok && params.Status == "" {
		params.Status = v
	}
	if v, ok := filters["priority"]; ok && params.Priority == "" {
		params.Priority = v
	}
	if v, ok := filters["assignee"]; ok && params.AssigneeID == "" {
		params.AssigneeID = v
	}
	if v, ok := filters["team"]; ok && params.TeamID == "" {
		params.TeamID = v
	}
	if v, ok := filters["project"]; ok && params.ProjectID == "" {
		params.ProjectID = v
	}
	if v, ok := filters["label"]; ok && params.LabelID == "" {
		params.LabelID = v
	}
	if v, ok := filters["cycle"]; ok && params.CycleID == "" {
		params.CycleID = v
	}
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
