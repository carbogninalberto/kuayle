package handler

import (
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/kuayle/kuayle-backend/pkg/validate"
	"github.com/labstack/echo/v5"
)

type WebhookHandler struct {
	webhookSvc *service.WebhookService
}

func NewWebhookHandler(webhookSvc *service.WebhookService) *WebhookHandler {
	return &WebhookHandler{webhookSvc: webhookSvc}
}

func (h *WebhookHandler) List(c *echo.Context) error {
	ws := c.Get("workspace").(*domain.Workspace)
	webhooks, err := h.webhookSvc.List(c.Request().Context(), ws.ID)
	if err != nil {
		return response.InternalError(c)
	}
	resp := make([]dto.WebhookResponse, len(webhooks))
	for i, w := range webhooks {
		resp[i] = toWebhookResponse(w)
	}
	return response.Success(c, http.StatusOK, resp)
}

func (h *WebhookHandler) Create(c *echo.Context) error {
	var req dto.CreateWebhookRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid request body")
	}
	if err := validate.Struct(&req); err != nil {
		return response.ValidationError(c, validationDetails(err))
	}

	ws := c.Get("workspace").(*domain.Workspace)
	w, err := h.webhookSvc.Create(c.Request().Context(), ws.ID, req)
	if errors.Is(err, repository.ErrPrivateWorkspaceOperation) {
		return webhooksOffResponse(c)
	}
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	}
	return response.Success(c, http.StatusCreated, toWebhookResponse(*w))
}

func (h *WebhookHandler) Update(c *echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid webhook ID")
	}
	var req dto.UpdateWebhookRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid request body")
	}
	if err := validate.Struct(&req); err != nil {
		return response.ValidationError(c, validationDetails(err))
	}
	w, err := h.webhookSvc.Update(c.Request().Context(), id, req)
	if errors.Is(err, repository.ErrPrivateWorkspaceOperation) {
		return webhooksOffResponse(c)
	}
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	}
	return response.Success(c, http.StatusOK, toWebhookResponse(*w))
}

func (h *WebhookHandler) Delete(c *echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid webhook ID")
	}
	if err := h.webhookSvc.Delete(c.Request().Context(), id); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	}
	return response.Success(c, http.StatusOK, map[string]string{"status": "deleted"})
}

// webhooksOffResponse refuses creating or enabling a webhook in a workspace
// that has or had a private team, which delivers no webhooks.
func webhooksOffResponse(c *echo.Context) error {
	return response.Error(c, http.StatusForbidden, "PRIVATE_WORKSPACE_OPERATION_DISABLED", "Webhooks are off because this workspace has a private team")
}

// ListDeliveries returns the webhook's most recent deliveries, newest first.
func (h *WebhookHandler) ListDeliveries(c *echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid webhook ID")
	}
	deliveries, err := h.webhookSvc.ListDeliveries(c.Request().Context(), id)
	if errors.Is(err, service.ErrWebhookNotFound) {
		return response.NotFound(c, "Webhook")
	}
	if err != nil {
		return response.InternalError(c)
	}
	resp := make([]dto.WebhookDeliveryResponse, len(deliveries))
	for i, d := range deliveries {
		resp[i] = dto.WebhookDeliveryResponse{
			ID:             d.ID.String(),
			Event:          d.Event,
			Status:         d.Status,
			Attempts:       d.Attempts,
			LastStatusCode: d.LastStatusCode,
			LastError:      d.LastError,
			NextAttemptAt:  d.NextAttemptAt,
			CreatedAt:      d.CreatedAt,
			UpdatedAt:      d.UpdatedAt,
			CompletedAt:    d.CompletedAt,
		}
	}
	return response.Success(c, http.StatusOK, resp)
}

func toWebhookResponse(w domain.Webhook) dto.WebhookResponse {
	return dto.WebhookResponse{
		ID:        w.ID.String(),
		URL:       w.URL,
		Events:    w.Events,
		IsActive:  w.IsActive,
		CreatedAt: w.CreatedAt,
		UpdatedAt: w.UpdatedAt,
	}
}
