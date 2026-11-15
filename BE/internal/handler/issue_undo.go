package handler

import (
	"errors"
	"net/http"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/kuayle/kuayle-backend/pkg/validate"
	"github.com/labstack/echo/v5"
	log "github.com/sirupsen/logrus"
)

// EditedMeanwhileMessage is what a refused undo tells the user.
const EditedMeanwhileMessage = "This item was edited in the meantime, so nothing was undone."

// undoError maps the refusals of undo and restore to their HTTP statuses.
func undoError(c *echo.Context, err error) error {
	switch {
	case errors.Is(err, service.ErrIssueEditedMeanwhile):
		return response.Error(c, http.StatusConflict, "EDITED_MEANWHILE", EditedMeanwhileMessage)
	case errors.Is(err, service.ErrIssueUndoUnavailable):
		return response.Error(c, http.StatusConflict, "UNDO_UNAVAILABLE", err.Error())
	case errors.Is(err, service.ErrIssueRestoreExpired):
		return response.Error(c, http.StatusGone, "GRACE_EXPIRED", "This item is past its restore period and cannot be restored.")
	case errors.Is(err, service.ErrIssueNotFound):
		return response.NotFound(c, "Issue")
	case errors.Is(err, service.ErrIssueForbidden):
		return response.Forbidden(c)
	case errors.Is(err, service.ErrIssueUndoInvalid):
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	}
	log.WithError(err).Error("issue undo failed")
	return response.InternalError(c)
}

// bindValid binds and validates req. When it reports false it has written
// the error response, and its error is the result of writing it.
func bindValid(c *echo.Context, req any) (bool, error) {
	if err := c.Bind(req); err != nil {
		return false, response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid request body")
	}
	if err := validate.Struct(req); err != nil {
		details := make([]dto.ErrorDetail, 0)
		for _, e := range validate.FormatErrors(err) {
			details = append(details, dto.ErrorDetail{Field: e["field"], Message: e["message"]})
		}
		return false, response.ValidationError(c, details)
	}
	return true, nil
}

// Undo puts back the values an edit changed if the issue was not edited since.
func (h *IssueHandler) Undo(c *echo.Context) error {
	var req dto.IssueUndo
	if ok, err := bindValid(c, &req); !ok {
		return err
	}
	ws := c.Get("workspace").(*domain.Workspace)
	userID := middleware.GetUserID(c)
	issue, err := h.issueSvc.UndoIssue(c.Request().Context(), ws.ID, userID, c.Param("identifier"), req)
	if err != nil {
		return undoError(c, err)
	}
	resp := toIssueResponse(*issue)
	h.enrichIssueResponse(c.Request().Context(), &resp, *issue)
	h.enrichIssueSubscription(c.Request().Context(), &resp, issue.ID, userID)
	return response.Success(c, http.StatusOK, resp)
}

// BulkUndo undoes a bulk edit as one step: every issue or none.
func (h *IssueHandler) BulkUndo(c *echo.Context) error {
	var req dto.BulkIssueUndo
	if ok, err := bindValid(c, &req); !ok {
		return err
	}
	ws := c.Get("workspace").(*domain.Workspace)
	undone, err := h.issueSvc.Undo(c.Request().Context(), ws.ID, middleware.GetUserID(c), req.Items)
	if err != nil {
		return undoError(c, err)
	}
	return response.Success(c, http.StatusOK, dto.BulkUndoIssueResponse{Updated: len(undone)})
}

// canRestoreAny mirrors Delete: owners and admins restore any issue, with a
// token that also carries issue:delete; everyone else only issues they created.
func canRestoreAny(c *echo.Context) bool {
	role, _ := c.Get("workspace_role").(string)
	return domain.HasPermission(role, domain.PermIssueDelete) && middleware.TokenAllows(c, domain.PermIssueDelete)
}

// Restore brings back a deleted issue within the grace period.
func (h *IssueHandler) Restore(c *echo.Context) error {
	ws := c.Get("workspace").(*domain.Workspace)
	userID := middleware.GetUserID(c)
	issue, err := h.issueSvc.RestoreIssue(c.Request().Context(), ws.ID, userID, c.Param("identifier"), canRestoreAny(c))
	if err != nil {
		return undoError(c, err)
	}
	resp := toIssueResponse(*issue)
	h.enrichIssueResponse(c.Request().Context(), &resp, *issue)
	h.enrichIssueSubscription(c.Request().Context(), &resp, issue.ID, userID)
	return response.Success(c, http.StatusOK, resp)
}

// BulkRestore brings back deleted issues as one step: every issue or none.
func (h *IssueHandler) BulkRestore(c *echo.Context) error {
	var req dto.BulkRestoreIssueRequest
	if ok, err := bindValid(c, &req); !ok {
		return err
	}
	ws := c.Get("workspace").(*domain.Workspace)
	restored, err := h.issueSvc.BulkRestore(c.Request().Context(), ws.ID, middleware.GetUserID(c), canRestoreAny(c), req)
	if err != nil {
		return undoError(c, err)
	}
	return response.Success(c, http.StatusOK, dto.BulkRestoreIssueResponse{Restored: restored})
}
