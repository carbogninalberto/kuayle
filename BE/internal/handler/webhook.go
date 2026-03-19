package handler

import (
	"net/http"

	"github.com/carbon/carbon-backend/internal/domain"
	"github.com/carbon/carbon-backend/internal/dto"
	"github.com/carbon/carbon-backend/internal/service"
	"github.com/carbon/carbon-backend/pkg/response"
	"github.com/carbon/carbon-backend/pkg/validate"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

type WebhookHandler struct {
	webhookSvc *service.WebhookService
}

func NewWebhookHandler(webhookSvc *service.WebhookService) *WebhookHandler {
	return &WebhookHandler{webhookSvc: webhookSvc}
}

func (h *WebhookHandler) List(c echo.Context) error {
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

func (h *WebhookHandler) Create(c echo.Context) error {
	var req dto.CreateWebhookRequest
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
	w, err := h.webhookSvc.Create(c.Request().Context(), ws.ID, req)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	}
	return response.Success(c, http.StatusCreated, toWebhookResponse(*w))
}

func (h *WebhookHandler) Update(c echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid webhook ID")
	}
	var req dto.UpdateWebhookRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid request body")
	}
	w, err := h.webhookSvc.Update(c.Request().Context(), id, req)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	}
	return response.Success(c, http.StatusOK, toWebhookResponse(*w))
}

func (h *WebhookHandler) Delete(c echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid webhook ID")
	}
	if err := h.webhookSvc.Delete(c.Request().Context(), id); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	}
	return response.Success(c, http.StatusOK, map[string]string{"status": "deleted"})
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
