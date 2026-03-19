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

type IssueRelationHandler struct {
	relationSvc *service.IssueRelationService
}

func NewIssueRelationHandler(relationSvc *service.IssueRelationService) *IssueRelationHandler {
	return &IssueRelationHandler{relationSvc: relationSvc}
}

func (h *IssueRelationHandler) Create(c echo.Context) error {
	var req dto.CreateIssueRelationRequest
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
	identifier := c.Param("identifier")

	rel, err := h.relationSvc.Create(c.Request().Context(), ws.ID, identifier, req)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	}

	return response.Success(c, http.StatusCreated, dto.IssueRelationResponse{
		ID:             rel.ID.String(),
		IssueID:        rel.IssueID.String(),
		RelatedIssueID: rel.RelatedIssueID.String(),
		Type:           string(rel.Type),
		CreatedAt:      rel.CreatedAt,
	})
}

func (h *IssueRelationHandler) List(c echo.Context) error {
	ws := c.Get("workspace").(*domain.Workspace)
	identifier := c.Param("identifier")

	relations, err := h.relationSvc.ListByIssue(c.Request().Context(), ws.ID, identifier)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	}

	resp := make([]dto.IssueRelationResponse, len(relations))
	for i, rel := range relations {
		resp[i] = dto.IssueRelationResponse{
			ID:             rel.ID.String(),
			IssueID:        rel.IssueID.String(),
			RelatedIssueID: rel.RelatedIssueID.String(),
			Type:           string(rel.Type),
			CreatedAt:      rel.CreatedAt,
		}
	}

	return response.Success(c, http.StatusOK, resp)
}

func (h *IssueRelationHandler) Delete(c echo.Context) error {
	id, err := uuid.Parse(c.Param("relationId"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid relation ID")
	}

	if err := h.relationSvc.Delete(c.Request().Context(), id); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	}

	return response.Success(c, http.StatusOK, map[string]string{"status": "deleted"})
}
