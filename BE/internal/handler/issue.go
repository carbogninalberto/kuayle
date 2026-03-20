package handler

import (
	"context"
	"net/http"

	"github.com/carbon/carbon-backend/internal/domain"
	"github.com/carbon/carbon-backend/internal/dto"
	"github.com/carbon/carbon-backend/internal/middleware"
	"github.com/carbon/carbon-backend/internal/repository"
	"github.com/carbon/carbon-backend/internal/service"
	"github.com/carbon/carbon-backend/pkg/response"
	"github.com/carbon/carbon-backend/pkg/validate"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

type IssueHandler struct {
	issueSvc   *service.IssueService
	commentSvc *service.CommentService
	userRepo   repository.UserRepo
}

func NewIssueHandler(issueSvc *service.IssueService, commentSvc *service.CommentService, userRepo repository.UserRepo) *IssueHandler {
	return &IssueHandler{issueSvc: issueSvc, commentSvc: commentSvc, userRepo: userRepo}
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
		return response.InternalError(c)
	}

	ctx := c.Request().Context()

	// Batch load labels for all issues
	issueIDs := make([]uuid.UUID, len(issues))
	for i, issue := range issues {
		issueIDs[i] = issue.ID
	}
	labelsMap, _ := h.issueSvc.GetLabelsForIssues(ctx, issueIDs)

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

	deleted, err := h.issueSvc.BulkDelete(c.Request().Context(), ws.ID, req)
	if err != nil {
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
		cr := dto.CommentResponse{
			ID:        comment.ID.String(),
			IssueID:   comment.IssueID.String(),
			UserID:    comment.UserID.String(),
			Body:      comment.Body,
			CreatedAt: comment.CreatedAt,
			UpdatedAt: comment.UpdatedAt,
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
		resp[i] = cr
	}

	return response.Success(c, http.StatusOK, resp)
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

	cr := dto.CommentResponse{
		ID:        comment.ID.String(),
		IssueID:   comment.IssueID.String(),
		UserID:    comment.UserID.String(),
		Body:      comment.Body,
		CreatedAt: comment.CreatedAt,
		UpdatedAt: comment.UpdatedAt,
	}
	user, _ := h.userRepo.GetByID(c.Request().Context(), userID)
	if user != nil {
		cr.User = &dto.UserResponse{
			ID:          user.ID.String(),
			Email:       user.Email,
			Name:        user.Name,
			DisplayName: user.DisplayName,
			AvatarURL:   user.AvatarURL,
		}
	}

	return response.Success(c, http.StatusCreated, cr)
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

	h.enrichUserFields(ctx, resp, issue)
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
		Title:       issue.Title,
		Description: issue.Description,
		Status:      string(issue.Status),
		Priority:    int(issue.Priority),
		TeamID:      issue.TeamID.String(),
		CreatorID:   issue.CreatorID.String(),
		Estimate:    issue.Estimate,
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
