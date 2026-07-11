package handler

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/kuayle/kuayle-backend/pkg/sanitize"
	"github.com/kuayle/kuayle-backend/pkg/validate"
	"github.com/labstack/echo/v4"
	log "github.com/sirupsen/logrus"
)

type IssueHandler struct {
	issueSvc       *service.IssueService
	relationSvc    *service.IssueRelationService
	commentSvc     *service.CommentService
	userRepo       repository.UserRepo
	teamStatusRepo repository.TeamStatusRepo
	projectRepo    repository.ProjectRepo
	cycleRepo      repository.CycleRepo
}

type userBatchRepo interface {
	ListByIDs(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]domain.User, error)
}

func NewIssueHandler(issueSvc *service.IssueService, commentSvc *service.CommentService, userRepo repository.UserRepo, teamStatusRepo repository.TeamStatusRepo, projectRepo repository.ProjectRepo, cycleRepo repository.CycleRepo, relationSvc *service.IssueRelationService) *IssueHandler {
	return &IssueHandler{issueSvc: issueSvc, relationSvc: relationSvc, commentSvc: commentSvc, userRepo: userRepo, teamStatusRepo: teamStatusRepo, projectRepo: projectRepo, cycleRepo: cycleRepo}
}

func (h *IssueHandler) List(c echo.Context) error {
	ws := c.Get("workspace").(*domain.Workspace)
	var params dto.IssueFilterParams
	if err := c.Bind(&params); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid parameters")
	}
	params.Defaults()

	issues, total, err := h.issueSvc.List(c.Request().Context(), ws.ID, params)
	if err != nil {
		log.WithError(err).Error("issue list failed")
		return response.InternalError(c)
	}

	ctx := c.Request().Context()

	// Batch load labels and assignees for all issues
	issueIDs := make([]uuid.UUID, len(issues))
	for i, issue := range issues {
		issueIDs[i] = issue.ID
	}
	labelsMap, _ := h.issueSvc.GetLabelsForIssues(ctx, issueIDs)
	assigneesMap, _ := h.issueSvc.GetAssigneesForIssues(ctx, issueIDs)
	subscribedIssueIDs, _ := h.issueSvc.GetSubscribedIssueIDs(ctx, issueIDs, middleware.GetUserID(c))
	subIssueCounts, _ := h.issueSvc.CountSubIssuesForIssues(ctx, issueIDs)
	usersMap := h.usersForIssueList(ctx, issues, assigneesMap)
	relationSummaries := make(map[uuid.UUID]domain.IssueRelationSummary)
	if h.relationSvc != nil {
		relationSummaries, _ = h.relationSvc.SummariesByIssues(ctx, issueIDs)
	}

	// Batch load statuses for all issues
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
		statuses, _ := h.teamStatusRepo.GetByIDs(ctx, statusIDs)
		for i := range statuses {
			statusMap[statuses[i].ID] = &statuses[i]
		}
	}

	issueResponses := make([]dto.IssueResponse, len(issues))
	for i, issue := range issues {
		resp := toIssueResponse(issue)
		resp.IsSubscribed = subscribedIssueIDs[issue.ID]

		// Populate labels from batch
		if labels, ok := labelsMap[issue.ID]; ok && len(labels) > 0 {
			resp.Labels = make([]dto.LabelResponse, len(labels))
			for j, l := range labels {
				resp.Labels[j] = toLabelResponse(l)
			}
		}

		// Populate assignees from batch
		if uids, ok := assigneesMap[issue.ID]; ok && len(uids) > 0 {
			resp.Assignees = make([]dto.UserResponse, 0, len(uids))
			for _, uid := range uids {
				if user, ok := usersMap[uid]; ok {
					resp.Assignees = append(resp.Assignees, toUserResponse(user))
				}
			}
		}

		// Populate status_info from batch
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
		if count, ok := subIssueCounts[issue.ID]; ok && count.Total > 0 {
			total := count.Total
			done := count.Done
			resp.SubIssueCount = &total
			resp.SubIssueDone = &done
		}
		if summary, ok := relationSummaries[issue.ID]; ok {
			h.enrichIssueRelationSummary(ctx, &resp, summary)
		}

		h.enrichUserFieldsFromMap(&resp, issue, usersMap)
		issueResponses[i] = resp
	}

	return response.Success(c, http.StatusOK, dto.ListResponse[dto.IssueResponse]{
		Data:       issueResponses,
		TotalCount: total,
		Page:       params.Page,
		PerPage:    params.PerPage,
		HasMore:    params.Page*params.PerPage < total,
	})
}

func (h *IssueHandler) Create(c echo.Context) error {
	var req dto.CreateIssueRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid request body")
	}
	if err := validate.Struct(&req); err != nil {
		details := make([]dto.ErrorDetail, 0)
		for _, e := range validate.FormatErrors(err) {
			details = append(details, dto.ErrorDetail{Field: e["field"], Message: e["message"]})
		}
		return response.ValidationError(c, details)
	}

	ws := c.Get("workspace").(*domain.Workspace)
	userID := middleware.GetUserID(c)

	issue, err := h.issueSvc.Create(c.Request().Context(), ws.ID, userID, req)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	}

	resp := toIssueResponse(*issue)
	h.enrichIssueResponse(c.Request().Context(), &resp, *issue)
	return response.Success(c, http.StatusCreated, resp)
}

func (h *IssueHandler) CreateSubIssue(c echo.Context) error {
	var req dto.CreateSubIssueRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid request body")
	}
	if err := validate.Struct(&req); err != nil {
		details := make([]dto.ErrorDetail, 0)
		for _, e := range validate.FormatErrors(err) {
			details = append(details, dto.ErrorDetail{Field: e["field"], Message: e["message"]})
		}
		return response.ValidationError(c, details)
	}

	ws := c.Get("workspace").(*domain.Workspace)
	userID := middleware.GetUserID(c)
	issue, err := h.issueSvc.CreateSubIssue(c.Request().Context(), ws.ID, userID, c.Param("identifier"), req)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	}

	resp := toIssueResponse(*issue)
	h.enrichIssueResponse(c.Request().Context(), &resp, *issue)
	return response.Success(c, http.StatusCreated, resp)
}

func (h *IssueHandler) BulkCreateSubIssues(c echo.Context) error {
	var req dto.BulkCreateSubIssueRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid request body")
	}
	if err := validate.Struct(&req); err != nil {
		details := make([]dto.ErrorDetail, 0)
		for _, e := range validate.FormatErrors(err) {
			details = append(details, dto.ErrorDetail{Field: e["field"], Message: e["message"]})
		}
		return response.ValidationError(c, details)
	}

	ws := c.Get("workspace").(*domain.Workspace)
	userID := middleware.GetUserID(c)
	issues, err := h.issueSvc.BulkCreateSubIssues(c.Request().Context(), ws.ID, userID, c.Param("identifier"), req)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	}

	resp := make([]dto.IssueResponse, len(issues))
	for i, issue := range issues {
		resp[i] = toIssueResponse(issue)
		h.enrichIssueResponse(c.Request().Context(), &resp[i], issue)
	}
	return response.Success(c, http.StatusCreated, resp)
}

func (h *IssueHandler) Duplicate(c echo.Context) error {
	var req dto.DuplicateIssueRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid request body")
	}

	ws := c.Get("workspace").(*domain.Workspace)
	userID := middleware.GetUserID(c)
	issue, err := h.issueSvc.Duplicate(c.Request().Context(), ws.ID, userID, c.Param("identifier"), req.IncludeSubIssues)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	}

	resp := toIssueResponse(*issue)
	h.enrichIssueResponse(c.Request().Context(), &resp, *issue)
	return response.Success(c, http.StatusCreated, resp)
}

func (h *IssueHandler) ConvertToProject(c echo.Context) error {
	ws := c.Get("workspace").(*domain.Workspace)
	userID := middleware.GetUserID(c)
	project, err := h.issueSvc.ConvertToProject(c.Request().Context(), ws.ID, userID, c.Param("identifier"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	}
	return response.Success(c, http.StatusCreated, dto.ConvertIssueToProjectResponse{Project: toProjectResponseForIssue(*project)})
}

func (h *IssueHandler) Get(c echo.Context) error {
	ws := c.Get("workspace").(*domain.Workspace)
	identifier := c.Param("identifier")

	issue, err := h.issueSvc.GetByIdentifier(c.Request().Context(), ws.ID, identifier)
	if err != nil || issue == nil {
		return response.NotFound(c, "Issue")
	}

	resp := toIssueResponse(*issue)
	h.enrichIssueResponse(c.Request().Context(), &resp, *issue)
	h.enrichIssueSubscription(c.Request().Context(), &resp, issue.ID, middleware.GetUserID(c))

	return response.Success(c, http.StatusOK, resp)
}

func (h *IssueHandler) Update(c echo.Context) error {
	var req dto.UpdateIssueRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid request body")
	}

	ws := c.Get("workspace").(*domain.Workspace)
	userID := middleware.GetUserID(c)
	identifier := c.Param("identifier")

	issue, err := h.issueSvc.Update(c.Request().Context(), ws.ID, userID, identifier, req)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	}

	resp := toIssueResponse(*issue)
	h.enrichIssueResponse(c.Request().Context(), &resp, *issue)
	h.enrichIssueSubscription(c.Request().Context(), &resp, issue.ID, userID)
	return response.Success(c, http.StatusOK, resp)
}

func (h *IssueHandler) Subscribe(c echo.Context) error {
	ws := c.Get("workspace").(*domain.Workspace)
	userID := middleware.GetUserID(c)
	if err := h.issueSvc.Subscribe(c.Request().Context(), ws.ID, userID, c.Param("identifier")); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	}
	return response.Success(c, http.StatusOK, dto.SubscriptionResponse{IsSubscribed: true})
}

func (h *IssueHandler) Unsubscribe(c echo.Context) error {
	ws := c.Get("workspace").(*domain.Workspace)
	userID := middleware.GetUserID(c)
	if err := h.issueSvc.Unsubscribe(c.Request().Context(), ws.ID, userID, c.Param("identifier")); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	}
	return response.Success(c, http.StatusOK, dto.SubscriptionResponse{IsSubscribed: false})
}

func (h *IssueHandler) Delete(c echo.Context) error {
	ws := c.Get("workspace").(*domain.Workspace)
	identifier := c.Param("identifier")
	role, _ := c.Get("workspace_role").(string)
	userID := middleware.GetUserID(c)

	// Admin/Owner can delete any issue; members can only delete their own
	if !domain.HasPermission(role, domain.PermIssueDelete) {
		issue, err := h.issueSvc.GetByIdentifier(c.Request().Context(), ws.ID, identifier)
		if err != nil || issue == nil {
			return response.NotFound(c, "Issue")
		}
		if issue.CreatorID != userID {
			return response.Forbidden(c)
		}
	}

	if err := h.issueSvc.Delete(c.Request().Context(), ws.ID, userID, identifier); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	}

	return response.Success(c, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *IssueHandler) BulkUpdate(c echo.Context) error {
	var req dto.BulkUpdateIssueRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid request body")
	}
	if err := validate.Struct(&req); err != nil {
		details := make([]dto.ErrorDetail, 0)
		for _, e := range validate.FormatErrors(err) {
			details = append(details, dto.ErrorDetail{Field: e["field"], Message: e["message"]})
		}
		return response.ValidationError(c, details)
	}

	ws := c.Get("workspace").(*domain.Workspace)
	userID := middleware.GetUserID(c)

	updated, err := h.issueSvc.BulkUpdate(c.Request().Context(), ws.ID, userID, req)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	}

	return response.Success(c, http.StatusOK, map[string]int{"updated": updated})
}

func (h *IssueHandler) BulkDelete(c echo.Context) error {
	var req dto.BulkDeleteIssueRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid request body")
	}
	if err := validate.Struct(&req); err != nil {
		details := make([]dto.ErrorDetail, 0)
		for _, e := range validate.FormatErrors(err) {
			details = append(details, dto.ErrorDetail{Field: e["field"], Message: e["message"]})
		}
		return response.ValidationError(c, details)
	}

	ws := c.Get("workspace").(*domain.Workspace)
	role, _ := c.Get("workspace_role").(string)
	userID := middleware.GetUserID(c)
	canDeleteAny := domain.HasPermission(role, domain.PermIssueDelete)

	deleted, err := h.issueSvc.BulkDelete(c.Request().Context(), ws.ID, userID, canDeleteAny, req)
	if err != nil {
		if err.Error() == "forbidden" {
			return response.Forbidden(c)
		}
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	}

	return response.Success(c, http.StatusOK, map[string]int{"deleted": deleted})
}

func (h *IssueHandler) ListComments(c echo.Context) error {
	ws := c.Get("workspace").(*domain.Workspace)
	identifier := c.Param("identifier")

	issue, err := h.issueSvc.GetByIdentifier(c.Request().Context(), ws.ID, identifier)
	if err != nil || issue == nil {
		return response.NotFound(c, "Issue")
	}

	comments, err := h.commentSvc.ListByIssue(c.Request().Context(), issue.ID)
	if err != nil {
		return response.InternalError(c)
	}

	ctx := c.Request().Context()
	resp := make([]dto.CommentResponse, len(comments))
	for i, comment := range comments {
		cr := h.toCommentResponse(ctx, comment)

		// Fetch replies for this top-level comment
		replies, err := h.commentSvc.ListReplies(ctx, comment.ID)
		if err == nil && len(replies) > 0 {
			cr.Replies = make([]dto.CommentResponse, len(replies))
			for j, reply := range replies {
				cr.Replies[j] = h.toCommentResponse(ctx, reply)
			}
		}

		resp[i] = cr
	}

	return response.Success(c, http.StatusOK, resp)
}

func (h *IssueHandler) toCommentResponse(ctx context.Context, comment domain.Comment) dto.CommentResponse {
	cr := dto.CommentResponse{
		ID:         comment.ID.String(),
		IssueID:    comment.IssueID.String(),
		UserID:     comment.UserID.String(),
		Body:       comment.Body,
		ResolvedAt: comment.ResolvedAt,
		CreatedAt:  comment.CreatedAt,
		UpdatedAt:  comment.UpdatedAt,
	}
	if comment.ParentID != nil {
		s := comment.ParentID.String()
		cr.ParentID = &s
	}
	user, _ := h.userRepo.GetByID(ctx, comment.UserID)
	if user != nil {
		cr.User = &dto.UserResponse{
			ID:          user.ID.String(),
			Email:       user.Email,
			Name:        user.Name,
			DisplayName: user.DisplayName,
			AvatarURL:   user.AvatarURL,
		}
	}
	return cr
}

func (h *IssueHandler) CreateComment(c echo.Context) error {
	var req dto.CreateCommentRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid request body")
	}
	if err := validate.Struct(&req); err != nil {
		details := make([]dto.ErrorDetail, 0)
		for _, e := range validate.FormatErrors(err) {
			details = append(details, dto.ErrorDetail{Field: e["field"], Message: e["message"]})
		}
		return response.ValidationError(c, details)
	}

	ws := c.Get("workspace").(*domain.Workspace)
	userID := middleware.GetUserID(c)
	identifier := c.Param("identifier")

	issue, err := h.issueSvc.GetByIdentifier(c.Request().Context(), ws.ID, identifier)
	if err != nil || issue == nil {
		return response.NotFound(c, "Issue")
	}

	comment, err := h.commentSvc.Create(c.Request().Context(), issue.ID, userID, req)
	if err != nil {
		return response.InternalError(c)
	}

	cr := h.toCommentResponse(c.Request().Context(), *comment)
	return response.Success(c, http.StatusCreated, cr)
}

func (h *IssueHandler) ResolveComment(c echo.Context) error {
	commentID, err := uuid.Parse(c.Param("commentId"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid comment ID")
	}

	comment, err := h.commentSvc.GetByID(c.Request().Context(), commentID)
	if err != nil || comment == nil {
		return response.NotFound(c, "Comment")
	}

	if err := h.commentSvc.Resolve(c.Request().Context(), commentID); err != nil {
		return response.InternalError(c)
	}

	// Re-fetch to get updated resolved_at
	comment, _ = h.commentSvc.GetByID(c.Request().Context(), commentID)
	cr := h.toCommentResponse(c.Request().Context(), *comment)
	return response.Success(c, http.StatusOK, cr)
}

func (h *IssueHandler) ReopenComment(c echo.Context) error {
	commentID, err := uuid.Parse(c.Param("commentId"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid comment ID")
	}

	comment, err := h.commentSvc.GetByID(c.Request().Context(), commentID)
	if err != nil || comment == nil {
		return response.NotFound(c, "Comment")
	}

	if err := h.commentSvc.Reopen(c.Request().Context(), commentID); err != nil {
		return response.InternalError(c)
	}

	// Re-fetch to get updated state
	comment, _ = h.commentSvc.GetByID(c.Request().Context(), commentID)
	cr := h.toCommentResponse(c.Request().Context(), *comment)
	return response.Success(c, http.StatusOK, cr)
}

func (h *IssueHandler) ListSubIssues(c echo.Context) error {
	ws := c.Get("workspace").(*domain.Workspace)
	identifier := c.Param("identifier")

	subIssues, err := h.issueSvc.ListSubIssues(c.Request().Context(), ws.ID, identifier)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	}

	resp := make([]dto.IssueResponse, len(subIssues))
	for i, issue := range subIssues {
		r := toIssueResponse(issue)
		h.enrichIssueResponse(c.Request().Context(), &r, issue)
		total, done, _ := h.issueSvc.CountSubIssues(c.Request().Context(), issue.ID)
		if total > 0 {
			r.SubIssueCount = &total
			r.SubIssueDone = &done
		}
		resp[i] = r
	}

	return response.Success(c, http.StatusOK, resp)
}

func (h *IssueHandler) GetHistory(c echo.Context) error {
	ws := c.Get("workspace").(*domain.Workspace)
	identifier := c.Param("identifier")

	issue, err := h.issueSvc.GetByIdentifier(c.Request().Context(), ws.ID, identifier)
	if err != nil || issue == nil {
		return response.NotFound(c, "Issue")
	}

	history, err := h.issueSvc.GetHistory(c.Request().Context(), issue.ID)
	if err != nil {
		return response.InternalError(c)
	}

	resp := make([]dto.IssueHistoryResponse, len(history))
	for i, entry := range history {
		resp[i] = h.toIssueHistoryResponse(c.Request().Context(), entry)
	}

	return response.Success(c, http.StatusOK, resp)
}

func (h *IssueHandler) toIssueHistoryResponse(ctx context.Context, entry domain.IssueHistory) dto.IssueHistoryResponse {
	return dto.IssueHistoryResponse{
		ID:              entry.ID.String(),
		IssueID:         entry.IssueID.String(),
		UserID:          entry.UserID.String(),
		Field:           entry.Field,
		OldValue:        entry.OldValue,
		NewValue:        entry.NewValue,
		OldDisplayValue: h.historyDisplayValue(ctx, entry.Field, entry.OldValue),
		NewDisplayValue: h.historyDisplayValue(ctx, entry.Field, entry.NewValue),
		CreatedAt:       entry.CreatedAt,
	}
}

func (h *IssueHandler) historyDisplayValue(ctx context.Context, field string, value *string) *string {
	if value == nil {
		return nil
	}
	raw := strings.TrimSpace(*value)
	if raw == "" {
		return stringPtr("None")
	}

	switch field {
	case "parent", "parent_id":
		id, err := uuid.Parse(raw)
		if err != nil {
			return value
		}
		issue, _ := h.issueSvc.GetByID(ctx, id)
		if issue == nil {
			return stringPtr("Deleted issue")
		}
		return stringPtr(formatIssueHistoryName(*issue))
	case "project", "project_id":
		id, err := uuid.Parse(raw)
		if err != nil {
			return value
		}
		if h.projectRepo == nil {
			return stringPtr("Unknown project")
		}
		project, _ := h.projectRepo.GetByID(ctx, id)
		if project == nil {
			return stringPtr("Deleted project")
		}
		return stringPtr(project.Name)
	case "cycle", "cycle_id":
		id, err := uuid.Parse(raw)
		if err != nil {
			return value
		}
		if h.cycleRepo == nil {
			return stringPtr("Unknown cycle")
		}
		cycle, _ := h.cycleRepo.GetByID(ctx, id)
		if cycle == nil {
			return stringPtr("Deleted cycle")
		}
		return stringPtr(cycle.Name)
	case "assignee", "assignee_id":
		id, err := uuid.Parse(raw)
		if err != nil {
			return value
		}
		user, _ := h.userRepo.GetByID(ctx, id)
		if user == nil {
			return stringPtr("Former user")
		}
		return stringPtr(displayUserName(user))
	case "assignees":
		parts := strings.Split(raw, ",")
		names := make([]string, 0, len(parts))
		for _, part := range parts {
			uid := strings.TrimSpace(part)
			if uid == "" {
				continue
			}
			id, err := uuid.Parse(uid)
			if err != nil {
				names = append(names, "Former user")
				continue
			}
			user, _ := h.userRepo.GetByID(ctx, id)
			if user != nil {
				names = append(names, displayUserName(user))
			} else {
				names = append(names, "Former user")
			}
		}
		if len(names) == 0 {
			return stringPtr("None")
		}
		return stringPtr(strings.Join(names, ", "))
	case "status", "status_id":
		if id, err := uuid.Parse(raw); err == nil {
			status, _ := h.teamStatusRepo.GetByID(ctx, id)
			if status != nil {
				return stringPtr(status.Name)
			}
		}
		return stringPtr(statusHistoryLabel(raw))
	case "priority":
		return stringPtr(priorityHistoryLabel(raw))
	default:
		return value
	}
}

func (h *IssueHandler) TriageAccept(c echo.Context) error {
	ws := c.Get("workspace").(*domain.Workspace)
	userID := middleware.GetUserID(c)
	identifier := c.Param("identifier")

	issue, err := h.issueSvc.Triage(c.Request().Context(), ws.ID, userID, identifier, true)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	}
	return response.Success(c, http.StatusOK, toIssueResponse(*issue))
}

func (h *IssueHandler) TriageDecline(c echo.Context) error {
	ws := c.Get("workspace").(*domain.Workspace)
	userID := middleware.GetUserID(c)
	identifier := c.Param("identifier")

	issue, err := h.issueSvc.Triage(c.Request().Context(), ws.ID, userID, identifier, false)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	}
	return response.Success(c, http.StatusOK, toIssueResponse(*issue))
}

func (h *IssueHandler) enrichIssueResponse(ctx context.Context, resp *dto.IssueResponse, issue domain.Issue) {
	// Populate labels
	labels, _ := h.issueSvc.GetLabels(ctx, issue.ID)
	if labels != nil {
		resp.Labels = make([]dto.LabelResponse, len(labels))
		for i, l := range labels {
			resp.Labels[i] = toLabelResponse(l)
		}
	}

	// Populate assignees
	assigneeIDs, _ := h.issueSvc.GetAssignees(ctx, issue.ID)
	if len(assigneeIDs) > 0 {
		resp.Assignees = make([]dto.UserResponse, 0, len(assigneeIDs))
		for _, uid := range assigneeIDs {
			user, _ := h.userRepo.GetByID(ctx, uid)
			if user != nil {
				resp.Assignees = append(resp.Assignees, dto.UserResponse{
					ID:          user.ID.String(),
					Email:       user.Email,
					Name:        user.Name,
					DisplayName: user.DisplayName,
					AvatarURL:   user.AvatarURL,
				})
			}
		}
	}

	// Populate status_info
	h.enrichStatusInfo(ctx, resp, issue)

	// Populate parent summary
	if issue.ParentID != nil {
		parent, _ := h.issueSvc.GetByID(ctx, *issue.ParentID)
		if parent != nil {
			summary := h.toIssueSummaryResponse(ctx, *parent)
			resp.Parent = &summary
		}
	}

	total, done, _ := h.issueSvc.CountSubIssues(ctx, issue.ID)
	if total > 0 {
		resp.SubIssueCount = &total
		resp.SubIssueDone = &done
	}
	h.enrichRelationCounts(ctx, resp, issue.ID)

	h.enrichUserFields(ctx, resp, issue)
}

func (h *IssueHandler) enrichIssueSubscription(ctx context.Context, resp *dto.IssueResponse, issueID, userID uuid.UUID) {
	subscribed, err := h.issueSvc.IsSubscribed(ctx, issueID, userID)
	if err == nil {
		resp.IsSubscribed = subscribed
	}
}

func (h *IssueHandler) enrichRelationCounts(ctx context.Context, resp *dto.IssueResponse, issueID uuid.UUID) {
	if h.relationSvc == nil {
		return
	}
	summaries, err := h.relationSvc.SummariesByIssues(ctx, []uuid.UUID{issueID})
	if err != nil {
		return
	}
	if summary, ok := summaries[issueID]; ok {
		h.enrichIssueRelationSummary(ctx, resp, summary)
	}
}

func (h *IssueHandler) enrichIssueRelationSummary(ctx context.Context, resp *dto.IssueResponse, summary domain.IssueRelationSummary) {
	resp.RelationCounts = toIssueRelationCountsResponse(summary.Counts)
	if len(summary.Related) == 0 && len(summary.BlockedBy) == 0 && len(summary.Blocking) == 0 && len(summary.Duplicate) == 0 {
		return
	}

	relationSummary := &dto.IssueRelationSummaryResponse{}
	if len(summary.Related) > 0 {
		relationSummary.Related = make([]dto.IssueSummaryResponse, len(summary.Related))
		for i, issue := range summary.Related {
			relationSummary.Related[i] = h.toIssueSummaryResponse(ctx, issue)
		}
	}
	if len(summary.BlockedBy) > 0 {
		relationSummary.BlockedBy = make([]dto.IssueSummaryResponse, len(summary.BlockedBy))
		for i, issue := range summary.BlockedBy {
			relationSummary.BlockedBy[i] = h.toIssueSummaryResponse(ctx, issue)
		}
	}
	if len(summary.Blocking) > 0 {
		relationSummary.Blocking = make([]dto.IssueSummaryResponse, len(summary.Blocking))
		for i, issue := range summary.Blocking {
			relationSummary.Blocking[i] = h.toIssueSummaryResponse(ctx, issue)
		}
	}
	if len(summary.Duplicate) > 0 {
		relationSummary.Duplicate = make([]dto.IssueSummaryResponse, len(summary.Duplicate))
		for i, issue := range summary.Duplicate {
			relationSummary.Duplicate[i] = h.toIssueSummaryResponse(ctx, issue)
		}
	}
	resp.RelationSummary = relationSummary
}

func toIssueRelationCountsResponse(count domain.IssueRelationCounts) *dto.IssueRelationCountsResponse {
	return &dto.IssueRelationCountsResponse{
		Related:   count.Related,
		BlockedBy: count.BlockedBy,
		Blocking:  count.Blocking,
		Duplicate: count.Duplicate,
	}
}

func (h *IssueHandler) enrichStatusInfo(ctx context.Context, resp *dto.IssueResponse, issue domain.Issue) {
	if issue.StatusID == nil {
		return
	}
	ts, err := h.teamStatusRepo.GetByID(ctx, *issue.StatusID)
	if err != nil || ts == nil {
		return
	}
	var color *string
	if ts.Color != nil {
		color = ts.Color
	}
	resp.StatusInfo = &dto.StatusInfoResponse{
		ID:       ts.ID.String(),
		Name:     ts.Name,
		Category: string(ts.Category),
		Color:    color,
		Position: ts.Position,
	}
}

func (h *IssueHandler) enrichUserFields(ctx context.Context, resp *dto.IssueResponse, issue domain.Issue) {
	// Populate assignee user object
	if issue.AssigneeID != nil {
		user, _ := h.userRepo.GetByID(ctx, *issue.AssigneeID)
		if user != nil {
			resp.Assignee = &dto.UserResponse{
				ID:          user.ID.String(),
				Email:       user.Email,
				Name:        user.Name,
				DisplayName: user.DisplayName,
				AvatarURL:   user.AvatarURL,
			}
		}
	}

	// Populate creator user object
	user, _ := h.userRepo.GetByID(ctx, issue.CreatorID)
	if user != nil {
		resp.Creator = &dto.UserResponse{
			ID:          user.ID.String(),
			Email:       user.Email,
			Name:        user.Name,
			DisplayName: user.DisplayName,
			AvatarURL:   user.AvatarURL,
		}
	}
}

func (h *IssueHandler) usersForIssueList(ctx context.Context, issues []domain.Issue, assigneesMap map[uuid.UUID][]uuid.UUID) map[uuid.UUID]domain.User {
	ids := make([]uuid.UUID, 0, len(issues)*2)
	seen := make(map[uuid.UUID]struct{}, len(issues)*2)
	add := func(id uuid.UUID) {
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	for _, issue := range issues {
		add(issue.CreatorID)
		if issue.AssigneeID != nil {
			add(*issue.AssigneeID)
		}
		for _, assigneeID := range assigneesMap[issue.ID] {
			add(assigneeID)
		}
	}

	if repo, ok := h.userRepo.(userBatchRepo); ok {
		users, err := repo.ListByIDs(ctx, ids)
		if err == nil {
			return users
		}
	}

	users := make(map[uuid.UUID]domain.User, len(ids))
	for _, id := range ids {
		user, _ := h.userRepo.GetByID(ctx, id)
		if user != nil {
			users[id] = *user
		}
	}
	return users
}

func (h *IssueHandler) enrichUserFieldsFromMap(resp *dto.IssueResponse, issue domain.Issue, users map[uuid.UUID]domain.User) {
	if issue.AssigneeID != nil {
		if user, ok := users[*issue.AssigneeID]; ok {
			assignee := toUserResponse(user)
			resp.Assignee = &assignee
		}
	}
	if user, ok := users[issue.CreatorID]; ok {
		creator := toUserResponse(user)
		resp.Creator = &creator
	}
}

func toUserResponse(user domain.User) dto.UserResponse {
	return dto.UserResponse{
		ID:          user.ID.String(),
		Email:       user.Email,
		Name:        user.Name,
		DisplayName: user.DisplayName,
		AvatarURL:   user.AvatarURL,
	}
}

func toIssueResponse(issue domain.Issue) dto.IssueResponse {
	resp := dto.IssueResponse{
		ID:          issue.ID.String(),
		Identifier:  issue.Identifier,
		Title:       sanitize.PlainText(issue.Title),
		Description: issue.Description,
		Status:      string(issue.Status),
		Priority:    int(issue.Priority),
		TeamID:      issue.TeamID.String(),
		CreatorID:   issue.CreatorID.String(),
		DueDate:     issue.DueDate,
		SortOrder:   issue.SortOrder,
		CreatedAt:   issue.CreatedAt,
		UpdatedAt:   issue.UpdatedAt,
	}
	if issue.ProjectID != nil {
		s := issue.ProjectID.String()
		resp.ProjectID = &s
	}
	if issue.CycleID != nil {
		s := issue.CycleID.String()
		resp.CycleID = &s
	}
	if issue.AssigneeID != nil {
		s := issue.AssigneeID.String()
		resp.AssigneeID = &s
	}
	if issue.ParentID != nil {
		s := issue.ParentID.String()
		resp.ParentID = &s
	}
	if issue.StatusID != nil {
		s := issue.StatusID.String()
		resp.StatusID = &s
	}
	return resp
}

func toLabelResponse(l domain.Label) dto.LabelResponse {
	resp := dto.LabelResponse{
		ID:          l.ID.String(),
		Name:        l.Name,
		Color:       l.Color,
		Description: l.Description,
		CreatedAt:   l.CreatedAt,
		UpdatedAt:   l.UpdatedAt,
	}
	if l.ParentID != nil {
		s := l.ParentID.String()
		resp.ParentID = &s
	}
	return resp
}

func toProjectResponseForIssue(project domain.Project) dto.ProjectResponse {
	resp := dto.ProjectResponse{
		ID:          project.ID.String(),
		Name:        project.Name,
		Description: project.Description,
		Status:      string(project.Status),
		LeadID:      nil,
		StartDate:   project.StartDate,
		TargetDate:  project.TargetDate,
		SortOrder:   project.SortOrder,
		CreatedAt:   project.CreatedAt,
		UpdatedAt:   project.UpdatedAt,
	}
	if project.TeamID != nil {
		s := project.TeamID.String()
		resp.TeamID = &s
	}
	if project.LeadID != nil {
		s := project.LeadID.String()
		resp.LeadID = &s
	}
	return resp
}

func (h *IssueHandler) toIssueSummaryResponse(ctx context.Context, issue domain.Issue) dto.IssueSummaryResponse {
	resp := dto.IssueSummaryResponse{
		ID:          issue.ID.String(),
		Identifier:  issue.Identifier,
		Title:       sanitize.PlainText(issue.Title),
		Description: issue.Description,
		Status:      string(issue.Status),
		Priority:    int(issue.Priority),
	}
	if issue.StatusID != nil {
		statusID := issue.StatusID.String()
		resp.StatusID = &statusID
		status, _ := h.teamStatusRepo.GetByID(ctx, *issue.StatusID)
		if status != nil {
			resp.StatusInfo = &dto.StatusInfoResponse{
				ID:       status.ID.String(),
				Name:     status.Name,
				Category: string(status.Category),
				Color:    status.Color,
				Position: status.Position,
			}
		}
	}
	if issue.AssigneeID != nil {
		user, _ := h.userRepo.GetByID(ctx, *issue.AssigneeID)
		if user != nil {
			resp.Assignee = &dto.UserResponse{
				ID:          user.ID.String(),
				Email:       user.Email,
				Name:        user.Name,
				DisplayName: user.DisplayName,
				AvatarURL:   user.AvatarURL,
			}
		}
	}
	return resp
}

func formatIssueHistoryName(issue domain.Issue) string {
	return issue.Identifier + ": " + sanitize.PlainText(issue.Title)
}

func stringPtr(value string) *string {
	return &value
}

func displayUserName(user *domain.User) string {
	if user.DisplayName != "" {
		return user.DisplayName
	}
	if user.Name != "" {
		return user.Name
	}
	return user.Email
}

func statusHistoryLabel(value string) string {
	switch value {
	case "backlog":
		return "Backlog"
	case "todo":
		return "Todo"
	case "in_progress":
		return "In Progress"
	case "in_review":
		return "In Review"
	case "done":
		return "Done"
	case "cancelled":
		return "Cancelled"
	default:
		return value
	}
}

func priorityHistoryLabel(value string) string {
	switch value {
	case "0":
		return "No priority"
	case "1":
		return "Urgent"
	case "2":
		return "High"
	case "3":
		return "Medium"
	case "4":
		return "Low"
	default:
		return value
	}
}
