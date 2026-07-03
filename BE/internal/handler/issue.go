package handler

import (
	"context"
	"net/http"

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
	commentSvc     *service.CommentService
	userRepo       repository.UserRepo
	teamStatusRepo repository.TeamStatusRepo
}

func NewIssueHandler(issueSvc *service.IssueService, commentSvc *service.CommentService, userRepo repository.UserRepo, teamStatusRepo repository.TeamStatusRepo) *IssueHandler {
	return &IssueHandler{issueSvc: issueSvc, commentSvc: commentSvc, userRepo: userRepo, teamStatusRepo: teamStatusRepo}
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

		// Populate user objects
		h.enrichUserFields(ctx, &resp, issue)
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

func (h *IssueHandler) Get(c echo.Context) error {
	ws := c.Get("workspace").(*domain.Workspace)
	identifier := c.Param("identifier")

	issue, err := h.issueSvc.GetByIdentifier(c.Request().Context(), ws.ID, identifier)
	if err != nil || issue == nil {
		return response.NotFound(c, "Issue")
	}

	resp := toIssueResponse(*issue)
	h.enrichIssueResponse(c.Request().Context(), &resp, *issue)

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
	return response.Success(c, http.StatusOK, resp)
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

	if err := h.issueSvc.Delete(c.Request().Context(), ws.ID, identifier); err != nil {
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

	return response.Success(c, http.StatusOK, history)
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

	h.enrichUserFields(ctx, resp, issue)
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
