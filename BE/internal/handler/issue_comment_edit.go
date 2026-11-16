package handler

import (
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/labstack/echo/v5"
)

// commentTarget resolves the :identifier issue and the :commentId of a
// comment route. It writes the error response itself and returns ok=false.
func (h *IssueHandler) commentTarget(c *echo.Context) (*domain.Issue, uuid.UUID, bool, error) {
	commentID, err := uuid.Parse(c.Param("commentId"))
	if err != nil {
		return nil, uuid.Nil, false, response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid comment ID")
	}
	ws := c.Get("workspace").(*domain.Workspace)
	issue, err := h.issueSvc.GetByIdentifier(c.Request().Context(), ws.ID, c.Param("identifier"))
	if err != nil || issue == nil {
		return nil, uuid.Nil, false, response.NotFound(c, "Issue")
	}
	return issue, commentID, true, nil
}

func commentError(c *echo.Context, err error) error {
	switch {
	case errors.Is(err, service.ErrCommentNotFound):
		return response.NotFound(c, "Comment")
	case errors.Is(err, service.ErrCommentForbidden):
		return response.Forbidden(c)
	case errors.Is(err, service.ErrCommentEmpty):
		return response.ValidationError(c, []dto.ErrorDetail{{Field: "body", Message: "Comment cannot be empty"}})
	case errors.Is(err, service.ErrCommentRestoreExpired):
		return response.Error(c, http.StatusGone, "GRACE_EXPIRED", "This comment is past its restore period and cannot be restored.")
	default:
		return response.InternalError(c)
	}
}

// UpdateComment changes the body of the caller's own comment. Nobody else,
// admins included, may edit it.
func (h *IssueHandler) UpdateComment(c *echo.Context) error {
	var req dto.UpdateCommentRequest
	if ok, err := bindValid(c, &req); !ok {
		return err
	}
	issue, commentID, ok, err := h.commentTarget(c)
	if !ok {
		return err
	}
	comment, err := h.commentSvc.Update(c.Request().Context(), issue, commentID, middleware.GetUserID(c), req.Body)
	if err != nil {
		return commentError(c, err)
	}
	return response.Success(c, http.StatusOK, h.toCommentResponse(c.Request().Context(), *comment))
}

// DeleteComment soft-deletes any comment on the issue; the route limits it
// to admins and owners (issue:delete). It stays restorable for the issue
// delete grace period.
func (h *IssueHandler) DeleteComment(c *echo.Context) error {
	issue, commentID, ok, err := h.commentTarget(c)
	if !ok {
		return err
	}
	if err := h.commentSvc.Delete(c.Request().Context(), issue, commentID, middleware.GetUserID(c)); err != nil {
		return commentError(c, err)
	}
	return response.Success(c, http.StatusOK, dto.DeleteCommentResponse{Status: "deleted", RestorableUntil: time.Now().Add(h.commentSvc.DeleteGracePeriod()).UTC()})
}

// RestoreComment brings back a comment deleted within the grace period.
func (h *IssueHandler) RestoreComment(c *echo.Context) error {
	issue, commentID, ok, err := h.commentTarget(c)
	if !ok {
		return err
	}
	comment, err := h.commentSvc.Restore(c.Request().Context(), issue, commentID)
	if err != nil {
		return commentError(c, err)
	}
	return response.Success(c, http.StatusOK, h.toCommentResponse(c.Request().Context(), *comment))
}
