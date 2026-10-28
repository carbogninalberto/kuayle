package handler

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/labstack/echo/v4"
)

func attachmentID(c echo.Context) (uuid.UUID, error) {
	id, err := uuid.Parse(c.Param("attachmentId"))
	if err != nil {
		return uuid.Nil, repository.ErrConversationNotFound
	}
	return id, nil
}

// UploadAttachment accepts one image in the multipart field "file". The
// content decides the type; the name is only kept for display.
func (h *ConversationHandler) UploadAttachment(c echo.Context) error {
	id, err := conversationID(c)
	if err != nil {
		return conversationError(c, err)
	}
	c.Request().Body = http.MaxBytesReader(c.Response(), c.Request().Body, domain.ConversationAttachmentMaxBytes+64<<10)
	reader, err := c.Request().MultipartReader()
	if err != nil {
		return conversationError(c, repository.ErrConversationInvalid)
	}
	for {
		part, err := reader.NextPart()
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				return conversationError(c, service.ErrAttachmentTooLarge)
			}
			return conversationError(c, repository.ErrConversationInvalid)
		}
		if part.FormName() != "file" {
			_ = part.Close()
			continue
		}
		result, err := h.svc.Upload(c.Request().Context(), middleware.GetWorkspace(c).ID, id, part.FileName(), part)
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			err = service.ErrAttachmentTooLarge
		}
		if err != nil {
			return conversationError(c, err)
		}
		return response.Success(c, http.StatusCreated, result)
	}
}

// Attachment streams one image to a current conversation reader. It is never
// served as markup: the stored type is a sniffed raster type, nosniff is set
// and the response cannot run script even if opened directly.
func (h *ConversationHandler) Attachment(c echo.Context) error {
	c.Response().Header().Set("Cache-Control", "private, no-store")
	id, err := conversationID(c)
	if err != nil {
		return conversationError(c, err)
	}
	attachment, err := attachmentID(c)
	if err != nil {
		return conversationError(c, err)
	}
	a, body, err := h.svc.OpenAttachment(c.Request().Context(), middleware.GetWorkspace(c).ID, id, attachment)
	if err != nil {
		return conversationError(c, err)
	}
	defer body.Close()
	contentType := "application/octet-stream"
	if _, ok := domain.ConversationAttachmentTypes[a.ContentType]; ok {
		contentType = a.ContentType
	}
	header := c.Response().Header()
	header.Set("Content-Type", contentType)
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	header.Set("Cross-Origin-Resource-Policy", "same-origin")
	header.Set("Content-Length", strconv.FormatInt(a.Size, 10))
	disposition := "inline"
	if c.QueryParam("download") == "1" {
		disposition = "attachment"
	}
	header.Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": a.Filename}))
	c.Response().WriteHeader(http.StatusOK)
	_, err = io.Copy(c.Response(), io.LimitReader(body, a.Size))
	return err
}

// RemoveAttachment discards a pending image removed from the composer.
func (h *ConversationHandler) RemoveAttachment(c echo.Context) error {
	id, err := conversationID(c)
	if err != nil {
		return conversationError(c, err)
	}
	attachment, err := attachmentID(c)
	if err != nil {
		return conversationError(c, err)
	}
	if err = h.svc.Repo.RemovePendingAttachment(c.Request().Context(), middleware.GetWorkspace(c).ID, id, attachment); err != nil {
		return conversationError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}
