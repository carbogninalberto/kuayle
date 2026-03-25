package handler

import (
	"net/http"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/kuayle/kuayle-backend/pkg/validate"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

type CycleHandler struct {
	cycleSvc *service.CycleService
}

func NewCycleHandler(cycleSvc *service.CycleService) *CycleHandler {
	return &CycleHandler{cycleSvc: cycleSvc}
}

func (h *CycleHandler) List(c echo.Context) error {
	teamID, err := uuid.Parse(c.Param("teamId"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid team ID")
	}

	cycles, err := h.cycleSvc.ListByTeam(c.Request().Context(), teamID)
	if err != nil {
		return response.InternalError(c)
	}

	resp := make([]dto.CycleResponse, len(cycles))
	for i, cy := range cycles {
		r := toCycleResponse(cy)
		// Include progress stats for each cycle
		stats, _ := h.cycleSvc.GetStats(c.Request().Context(), cy.ID)
		if stats != nil {
			r.Progress = stats
		}
		resp[i] = r
	}

	return response.Success(c, http.StatusOK, resp)
}

func (h *CycleHandler) Create(c echo.Context) error {
	teamID, err := uuid.Parse(c.Param("teamId"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid team ID")
	}

	var req dto.CreateCycleRequest
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

	cycle, err := h.cycleSvc.Create(c.Request().Context(), teamID, req)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	}

	return response.Success(c, http.StatusCreated, toCycleResponse(*cycle))
}

func (h *CycleHandler) Get(c echo.Context) error {
	id, err := uuid.Parse(c.Param("cycleId"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid cycle ID")
	}

	cycle, err := h.cycleSvc.GetByID(c.Request().Context(), id)
	if err != nil || cycle == nil {
		return response.NotFound(c, "Cycle")
	}

	r := toCycleResponse(*cycle)
	stats, _ := h.cycleSvc.GetStats(c.Request().Context(), cycle.ID)
	if stats != nil {
		r.Progress = stats
	}

	return response.Success(c, http.StatusOK, r)
}

func (h *CycleHandler) Update(c echo.Context) error {
	id, err := uuid.Parse(c.Param("cycleId"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid cycle ID")
	}

	var req dto.UpdateCycleRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid request body")
	}

	cycle, err := h.cycleSvc.Update(c.Request().Context(), id, req)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	}

	return response.Success(c, http.StatusOK, toCycleResponse(*cycle))
}

func (h *CycleHandler) Complete(c echo.Context) error {
	id, err := uuid.Parse(c.Param("cycleId"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid cycle ID")
	}

	var req dto.CompleteCycleRequest
	// Bind optional JSON body; ignore errors (body may be empty)
	_ = c.Bind(&req)

	cycle, carriedOver, err := h.cycleSvc.Complete(c.Request().Context(), id, req)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	}

	resp := toCycleResponse(*cycle)
	return response.Success(c, http.StatusOK, map[string]interface{}{
		"cycle":              resp,
		"carried_over_count": carriedOver,
	})
}

func (h *CycleHandler) Velocity(c echo.Context) error {
	teamID, err := uuid.Parse(c.Param("teamId"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid team ID")
	}
	points, err := h.cycleSvc.GetVelocity(c.Request().Context(), teamID)
	if err != nil {
		return response.InternalError(c)
	}
	return response.Success(c, http.StatusOK, points)
}

func (h *CycleHandler) Delete(c echo.Context) error {
	id, err := uuid.Parse(c.Param("cycleId"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid cycle ID")
	}

	if err := h.cycleSvc.Delete(c.Request().Context(), id); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	}

	return response.Success(c, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *CycleHandler) Burndown(c echo.Context) error {
	id, err := uuid.Parse(c.Param("cycleId"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid cycle ID")
	}

	points, err := h.cycleSvc.GetBurndown(c.Request().Context(), id)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	}

	return response.Success(c, http.StatusOK, points)
}

func toCycleResponse(cy domain.Cycle) dto.CycleResponse {
	return dto.CycleResponse{
		ID:            cy.ID.String(),
		TeamID:        cy.TeamID.String(),
		Name:          cy.Name,
		Number:        cy.Number,
		Status:        string(cy.Status),
		Description:   cy.Description,
		Goals:         cy.Goals,
		Retrospective: cy.Retrospective,
		StartDate:     cy.StartDate,
		EndDate:       cy.EndDate,
		CompletedAt:   cy.CompletedAt,
		CreatedAt:     cy.CreatedAt,
		UpdatedAt:     cy.UpdatedAt,
	}
}
