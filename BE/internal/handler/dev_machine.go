package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/kuayle/kuayle-backend/pkg/validate"
	"github.com/labstack/echo/v4"
)

type DevMachineHandler struct {
	service *service.DevMachineService
}

func NewDevMachineHandler(machineService *service.DevMachineService) *DevMachineHandler {
	return &DevMachineHandler{service: machineService}
}

func (h *DevMachineHandler) List(c echo.Context) error {
	workspace := middleware.GetWorkspace(c)
	var params dto.DevMachineListParams
	if err := c.Bind(&params); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid query parameters")
	}
	params.Defaults()
	if err := validate.Struct(&params); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid pagination parameters")
	}
	machines, total, err := h.service.List(c.Request().Context(), workspace.ID, middleware.GetUserID(c), params)
	if err != nil {
		return machineError(c, err)
	}
	return response.Success(c, http.StatusOK, dto.ListResponse[domain.DevMachine]{
		Data: machines, TotalCount: total, Page: params.Page, PerPage: params.PerPage,
		HasMore: params.Page*params.PerPage < total,
	})
}

func (h *DevMachineHandler) Create(c echo.Context) error {
	c.Request().Body = http.MaxBytesReader(c.Response().Writer, c.Request().Body, 1024*1024)
	var request dto.CreateDevMachineRequest
	if err := bindAndValidate(c, &request); err != nil {
		return err
	}
	if request.EnvironmentBuilder && middleware.GetWorkspaceRole(c) != domain.RoleOwner && middleware.GetWorkspaceRole(c) != domain.RoleAdmin {
		return response.Error(c, http.StatusForbidden, "FORBIDDEN", "Environment Builders require workspace administration")
	}
	for _, provider := range request.Agents {
		if provider.Provider == "custom" && middleware.GetWorkspaceRole(c) != domain.RoleOwner && middleware.GetWorkspaceRole(c) != domain.RoleAdmin {
			return response.Error(c, http.StatusForbidden, "FORBIDDEN", "Custom agent providers require workspace administration")
		}
	}
	workspace := middleware.GetWorkspace(c)
	machine, _, err := h.service.Create(c.Request().Context(), workspace.ID, middleware.GetUserID(c), request)
	if err != nil {
		return machineError(c, err)
	}
	return response.Success(c, http.StatusCreated, machine)
}

func (h *DevMachineHandler) NameSuggestion(c echo.Context) error {
	workspace := middleware.GetWorkspace(c)
	name, err := h.service.GenerateName(c.Request().Context(), workspace.ID, middleware.GetUserID(c))
	if err != nil {
		return machineError(c, err)
	}
	return response.Success(c, http.StatusOK, dto.DevMachineNameAvailabilityResponse{Name: name, Available: true})
}

func (h *DevMachineHandler) NameAvailability(c echo.Context) error {
	workspace := middleware.GetWorkspace(c)
	name := strings.TrimSpace(c.QueryParam("name"))
	available, err := h.service.NameAvailable(c.Request().Context(), workspace.ID, middleware.GetUserID(c), name)
	if err != nil {
		return machineError(c, err)
	}
	return response.Success(c, http.StatusOK, dto.DevMachineNameAvailabilityResponse{Name: name, Available: available})
}

func (h *DevMachineHandler) Get(c echo.Context) error {
	workspace := middleware.GetWorkspace(c)
	machineID, err := uuid.Parse(c.Param("machineId"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid machine ID")
	}
	machine, err := h.service.GetForUser(c.Request().Context(), workspace.ID, machineID, middleware.GetUserID(c))
	if err != nil {
		return machineError(c, err)
	}
	return response.Success(c, http.StatusOK, machine)
}

func (h *DevMachineHandler) Update(c echo.Context) error {
	var request dto.UpdateDevMachineRequest
	if err := bindAndValidate(c, &request); err != nil {
		return err
	}
	machineID, err := uuid.Parse(c.Param("machineId"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid machine ID")
	}
	machine, err := h.service.Update(c.Request().Context(), middleware.GetWorkspace(c).ID, machineID, middleware.GetUserID(c), request)
	if err != nil {
		return machineError(c, err)
	}
	return response.Success(c, http.StatusOK, machine)
}

func (h *DevMachineHandler) Delete(c echo.Context) error {
	machineID, err := uuid.Parse(c.Param("machineId"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid machine ID")
	}
	operation, err := h.service.Delete(c.Request().Context(), middleware.GetWorkspace(c).ID, machineID, middleware.GetUserID(c))
	if err != nil {
		return machineError(c, err)
	}
	if operation == nil {
		return c.NoContent(http.StatusAccepted)
	}
	return response.Success(c, http.StatusAccepted, operationResponse(operation))
}

func (h *DevMachineHandler) PermanentDelete(c echo.Context) error {
	machineID, err := uuid.Parse(c.Param("machineId"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid machine ID")
	}
	if err := h.service.PermanentDelete(c.Request().Context(), middleware.GetWorkspace(c).ID, machineID, middleware.GetUserID(c)); err != nil {
		return machineError(c, err)
	}
	return c.NoContent(http.StatusAccepted)
}

func (h *DevMachineHandler) BulkDelete(c echo.Context) error {
	var request dto.BulkDeleteDevMachinesRequest
	if err := bindAndValidate(c, &request); err != nil {
		return err
	}
	result, err := h.service.BulkDelete(c.Request().Context(), middleware.GetWorkspace(c).ID, middleware.GetUserID(c), request)
	if err != nil {
		return machineError(c, err)
	}
	return response.Success(c, http.StatusAccepted, result)
}

func (h *DevMachineHandler) BulkPermanentDelete(c echo.Context) error {
	var request dto.PurgeDevMachinesRequest
	if err := bindAndValidate(c, &request); err != nil {
		return err
	}
	count, err := h.service.BulkPermanentDelete(c.Request().Context(), middleware.GetWorkspace(c).ID, request)
	if err != nil {
		return machineError(c, err)
	}
	return response.Success(c, http.StatusOK, map[string]int{"count": count})
}

func (h *DevMachineHandler) TouchActivity(c echo.Context) error {
	machineID, err := uuid.Parse(c.Param("machineId"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid machine ID")
	}
	if err := h.service.TouchActivity(c.Request().Context(), middleware.GetWorkspace(c).ID, machineID, middleware.GetUserID(c)); err != nil {
		return machineError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *DevMachineHandler) ScopeSettings(c echo.Context) error {
	settings, err := h.service.ListScopeSettings(c.Request().Context(), middleware.GetWorkspace(c).ID)
	if err != nil {
		return machineError(c, err)
	}
	return response.Success(c, http.StatusOK, settings)
}

func (h *DevMachineHandler) ScopeSetting(c echo.Context) error {
	scopeType := c.QueryParam("scope_type")
	var scopeID *uuid.UUID
	if raw := c.QueryParam("scope_id"); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid scope ID")
		}
		scopeID = &parsed
	}
	setting, err := h.service.GetScopeSetting(c.Request().Context(), middleware.GetWorkspace(c).ID, scopeType, scopeID)
	if err != nil {
		return machineError(c, err)
	}
	return response.Success(c, http.StatusOK, setting)
}

func (h *DevMachineHandler) UpdateScopeSetting(c echo.Context) error {
	var request dto.DevMachineScopeSettingRequest
	if err := bindAndValidate(c, &request); err != nil {
		return err
	}
	role := middleware.GetWorkspaceRole(c)
	if request.ScopeType != "issue" && role != domain.RoleOwner && role != domain.RoleAdmin {
		return response.Error(c, http.StatusForbidden, "FORBIDDEN", "Workspace, team, and project development defaults require workspace administration")
	}
	setting, err := h.service.UpdateScopeSetting(c.Request().Context(), middleware.GetWorkspace(c).ID, request)
	if err != nil {
		return machineError(c, err)
	}
	return response.Success(c, http.StatusOK, setting)
}

func (h *DevMachineHandler) DeleteScopeSetting(c echo.Context) error {
	scopeType := c.QueryParam("scope_type")
	role := middleware.GetWorkspaceRole(c)
	if scopeType != "issue" && role != domain.RoleOwner && role != domain.RoleAdmin {
		return response.Error(c, http.StatusForbidden, "FORBIDDEN", "Workspace, team, and project development defaults require workspace administration")
	}
	var scopeID *uuid.UUID
	if raw := c.QueryParam("scope_id"); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid scope ID")
		}
		scopeID = &parsed
	}
	if err := h.service.DeleteScopeSetting(c.Request().Context(), middleware.GetWorkspace(c).ID, scopeType, scopeID); err != nil {
		return machineError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *DevMachineHandler) Checkouts(c echo.Context) error {
	machineID, err := uuid.Parse(c.Param("machineId"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid machine ID")
	}
	checkouts, err := h.service.ListCheckouts(c.Request().Context(), middleware.GetWorkspace(c).ID, machineID, middleware.GetUserID(c))
	if err != nil {
		return machineError(c, err)
	}
	return response.Success(c, http.StatusOK, checkouts)
}

func (h *DevMachineHandler) CheckoutIssue(c echo.Context) error {
	var request dto.CheckoutIssueRequest
	if err := bindAndValidate(c, &request); err != nil {
		return err
	}
	machineID, err := uuid.Parse(c.Param("machineId"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid machine ID")
	}
	checkout, err := h.service.CheckoutIssue(c.Request().Context(), middleware.GetWorkspace(c).ID, machineID, middleware.GetUserID(c), request)
	if err != nil {
		return machineError(c, err)
	}
	return response.Success(c, http.StatusAccepted, checkout)
}

func (h *DevMachineHandler) Environments(c echo.Context) error {
	environments, err := h.service.ListEnvironments(c.Request().Context(), middleware.GetWorkspace(c).ID)
	if err != nil {
		return machineError(c, err)
	}
	return response.Success(c, http.StatusOK, environments)
}

func (h *DevMachineHandler) GetEnvironment(c echo.Context) error {
	environmentID, err := uuid.Parse(c.Param("environmentId"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid environment ID")
	}
	environment, err := h.service.GetEnvironment(c.Request().Context(), middleware.GetWorkspace(c).ID, environmentID)
	if err != nil {
		return machineError(c, err)
	}
	return response.Success(c, http.StatusOK, environment)
}

func (h *DevMachineHandler) SnapshotEnvironment(c echo.Context) error {
	var request dto.CreateDevMachineEnvironmentRequest
	if err := bindAndValidate(c, &request); err != nil {
		return err
	}
	environment, err := h.service.SnapshotEnvironment(c.Request().Context(), middleware.GetWorkspace(c).ID, middleware.GetUserID(c), request)
	if err != nil {
		return machineError(c, err)
	}
	return response.Success(c, http.StatusAccepted, environment)
}

func (h *DevMachineHandler) DeleteEnvironment(c echo.Context) error {
	environmentID, err := uuid.Parse(c.Param("environmentId"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid environment ID")
	}
	if err := h.service.RequestEnvironmentDeletion(c.Request().Context(), middleware.GetWorkspace(c).ID, environmentID); err != nil {
		return machineError(c, err)
	}
	return c.NoContent(http.StatusAccepted)
}

func (h *DevMachineHandler) Start(c echo.Context) error {
	return h.lifecycle(c, domain.DevMachineOpStart)
}
func (h *DevMachineHandler) Stop(c echo.Context) error {
	return h.lifecycle(c, domain.DevMachineOpStop)
}
func (h *DevMachineHandler) Pause(c echo.Context) error {
	return h.lifecycle(c, domain.DevMachineOpPause)
}
func (h *DevMachineHandler) Teardown(c echo.Context) error {
	return h.lifecycle(c, domain.DevMachineOpTeardown)
}

func (h *DevMachineHandler) lifecycle(c echo.Context, action domain.DevMachineOperationAction) error {
	workspace := middleware.GetWorkspace(c)
	machineID, err := uuid.Parse(c.Param("machineId"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid machine ID")
	}
	operation, err := h.service.Lifecycle(c.Request().Context(), workspace.ID, machineID, middleware.GetUserID(c), action, c.Request().Header.Get("Idempotency-Key"))
	if err != nil {
		return machineError(c, err)
	}
	return response.Success(c, http.StatusAccepted, operationResponse(operation))
}

func (h *DevMachineHandler) Events(c echo.Context) error {
	workspace := middleware.GetWorkspace(c)
	machineID, err := uuid.Parse(c.Param("machineId"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid machine ID")
	}
	var params dto.EventListParams
	if err := c.Bind(&params); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid query parameters")
	}
	params.Defaults()
	events, err := h.service.ListEvents(c.Request().Context(), workspace.ID, machineID, middleware.GetUserID(c), params.AfterID, params.Limit)
	if err != nil {
		return machineError(c, err)
	}
	return response.Success(c, http.StatusOK, events)
}

func (h *DevMachineHandler) Logs(c echo.Context) error {
	workspace := middleware.GetWorkspace(c)
	machineID, err := uuid.Parse(c.Param("machineId"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid machine ID")
	}
	var params dto.LogListParams
	if err := c.Bind(&params); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid query parameters")
	}
	params.Defaults()
	var runID *uuid.UUID
	if params.RunID != nil {
		parsed, err := uuid.Parse(*params.RunID)
		if err != nil {
			return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid agent_run_id")
		}
		runID = &parsed
	}
	logs, err := h.service.ListLogs(c.Request().Context(), workspace.ID, machineID, middleware.GetUserID(c), runID, params.AfterID, params.Limit)
	if err != nil {
		return machineError(c, err)
	}
	return response.Success(c, http.StatusOK, logs)
}

func (h *DevMachineHandler) Services(c echo.Context) error {
	workspace := middleware.GetWorkspace(c)
	machineID, err := uuid.Parse(c.Param("machineId"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid machine ID")
	}
	services, err := h.service.ListServices(c.Request().Context(), workspace.ID, machineID, middleware.GetUserID(c))
	if err != nil {
		return machineError(c, err)
	}
	return response.Success(c, http.StatusOK, services)
}

func (h *DevMachineHandler) MachineProviders(c echo.Context) error {
	workspace := middleware.GetWorkspace(c)
	machineID, err := uuid.Parse(c.Param("machineId"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid machine ID")
	}
	providers, err := h.service.ConfiguredProviders(c.Request().Context(), workspace.ID, machineID, middleware.GetUserID(c))
	if err != nil {
		return machineError(c, err)
	}
	return response.Success(c, http.StatusOK, providers)
}

func (h *DevMachineHandler) ResourceUsage(c echo.Context) error {
	workspace := middleware.GetWorkspace(c)
	machineID, err := uuid.Parse(c.Param("machineId"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid machine ID")
	}
	samples, err := h.service.ListResourceSamples(c.Request().Context(), workspace.ID, machineID, middleware.GetUserID(c), 120)
	if err != nil {
		return machineError(c, err)
	}
	return response.Success(c, http.StatusOK, samples)
}

func (h *DevMachineHandler) LaunchService(c echo.Context) error {
	workspace := middleware.GetWorkspace(c)
	machineID, err := uuid.Parse(c.Param("machineId"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid machine ID")
	}
	var checkoutID *uuid.UUID
	if raw := c.QueryParam("checkout_id"); raw != "" {
		parsed, parseErr := uuid.Parse(raw)
		if parseErr != nil {
			return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid checkout ID")
		}
		checkoutID = &parsed
	}
	launch, err := h.service.LaunchService(c.Request().Context(), workspace.ID, machineID, middleware.GetUserID(c), c.Param("service"), checkoutID)
	if err != nil {
		return machineError(c, err)
	}
	status := http.StatusCreated
	if launch.Status != "" && launch.Status != "ready" {
		status = http.StatusAccepted
	}
	return response.Success(c, status, launch)
}

func (h *DevMachineHandler) ListTerminalSessions(c echo.Context) error {
	workspace := middleware.GetWorkspace(c)
	machineID, err := uuid.Parse(c.Param("machineId"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid machine ID")
	}
	sessions, err := h.service.ListTerminalSessions(c.Request().Context(), workspace.ID, machineID, middleware.GetUserID(c))
	if err != nil {
		return machineError(c, err)
	}
	return response.Success(c, http.StatusOK, sessions)
}

func (h *DevMachineHandler) CreateTerminalSession(c echo.Context) error {
	c.Request().Body = http.MaxBytesReader(c.Response().Writer, c.Request().Body, 64*1024)
	var request dto.CreateTerminalSessionRequest
	if err := bindAndValidate(c, &request); err != nil {
		return err
	}
	workspace := middleware.GetWorkspace(c)
	machineID, err := uuid.Parse(c.Param("machineId"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid machine ID")
	}
	launch, err := h.service.CreateTerminalSession(c.Request().Context(), workspace.ID, machineID, middleware.GetUserID(c), request)
	if err != nil {
		return machineError(c, err)
	}
	status := http.StatusCreated
	if launch.Status != "" && launch.Status != "ready" {
		status = http.StatusAccepted
	}
	return response.Success(c, status, launch)
}

func (h *DevMachineHandler) CloseTerminalSession(c echo.Context) error {
	workspace := middleware.GetWorkspace(c)
	machineID, err := uuid.Parse(c.Param("machineId"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid machine ID")
	}
	sessionID, err := uuid.Parse(c.Param("sessionId"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid terminal session ID")
	}
	session, err := h.service.CloseTerminalSession(c.Request().Context(), workspace.ID, machineID, middleware.GetUserID(c), sessionID)
	if err != nil {
		return machineError(c, err)
	}
	return response.Success(c, http.StatusOK, session)
}

func (h *DevMachineHandler) CreateAgentRun(c echo.Context) error {
	c.Request().Body = http.MaxBytesReader(c.Response().Writer, c.Request().Body, 512*1024)
	var request dto.CreateAgentRunRequest
	if err := bindAndValidate(c, &request); err != nil {
		return err
	}
	workspace := middleware.GetWorkspace(c)
	machineID, err := uuid.Parse(c.Param("machineId"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid machine ID")
	}
	run, err := h.service.CreateAgentRun(c.Request().Context(), workspace.ID, machineID, middleware.GetUserID(c), request)
	if err != nil {
		return machineError(c, err)
	}
	return response.Success(c, http.StatusAccepted, run)
}

func (h *DevMachineHandler) ListMachineAgentRuns(c echo.Context) error {
	workspace := middleware.GetWorkspace(c)
	machineID, err := uuid.Parse(c.Param("machineId"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid machine ID")
	}
	var params dto.PaginationParams
	if err := c.Bind(&params); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid query parameters")
	}
	params.Defaults()
	if err := validate.Struct(&params); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid pagination parameters")
	}
	runs, total, err := h.service.ListAgentRuns(c.Request().Context(), workspace.ID, middleware.GetUserID(c), &machineID, params.Page, params.PerPage)
	if err != nil {
		return machineError(c, err)
	}
	return response.Success(c, http.StatusOK, dto.ListResponse[domain.DevMachineAgentRun]{
		Data: runs, TotalCount: total, Page: params.Page, PerPage: params.PerPage, HasMore: params.Page*params.PerPage < total,
	})
}

func (h *DevMachineHandler) ListAgentRuns(c echo.Context) error {
	workspace := middleware.GetWorkspace(c)
	var params dto.PaginationParams
	if err := c.Bind(&params); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid query parameters")
	}
	params.Defaults()
	if err := validate.Struct(&params); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid pagination parameters")
	}
	runs, total, err := h.service.ListAgentRuns(c.Request().Context(), workspace.ID, middleware.GetUserID(c), nil, params.Page, params.PerPage)
	if err != nil {
		return machineError(c, err)
	}
	return response.Success(c, http.StatusOK, dto.ListResponse[domain.DevMachineAgentRun]{
		Data: runs, TotalCount: total, Page: params.Page, PerPage: params.PerPage, HasMore: params.Page*params.PerPage < total,
	})
}

func (h *DevMachineHandler) GetAgentRun(c echo.Context) error {
	workspace := middleware.GetWorkspace(c)
	runID, err := uuid.Parse(c.Param("agentRunId"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid agent run ID")
	}
	run, err := h.service.GetAgentRun(c.Request().Context(), workspace.ID, runID, middleware.GetUserID(c))
	if err != nil {
		return machineError(c, err)
	}
	return response.Success(c, http.StatusOK, run)
}

func (h *DevMachineHandler) CancelAgentRun(c echo.Context) error {
	workspace := middleware.GetWorkspace(c)
	runID, err := uuid.Parse(c.Param("agentRunId"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid agent run ID")
	}
	if err := h.service.CancelAgentRun(c.Request().Context(), workspace.ID, runID, middleware.GetUserID(c)); err != nil {
		return machineError(c, err)
	}
	return c.NoContent(http.StatusAccepted)
}

func (h *DevMachineHandler) AgentRunTrace(c echo.Context) error {
	workspace := middleware.GetWorkspace(c)
	runID, err := uuid.Parse(c.Param("agentRunId"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid agent run ID")
	}
	var params dto.TraceListParams
	if err := c.Bind(&params); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid query parameters")
	}
	trace, err := h.service.GetAgentRunTrace(c.Request().Context(), workspace.ID, runID, middleware.GetUserID(c), params)
	if err != nil {
		return machineError(c, err)
	}
	return response.Success(c, http.StatusOK, trace)
}

func (h *DevMachineHandler) Providers(c echo.Context) error {
	workspace := middleware.GetWorkspace(c)
	providers, err := h.service.AvailableProviders(c.Request().Context(), workspace.ID)
	if err != nil {
		return machineError(c, err)
	}
	role := middleware.GetWorkspaceRole(c)
	if role != domain.RoleOwner && role != domain.RoleAdmin {
		filtered := providers[:0]
		for _, provider := range providers {
			if !provider.Custom {
				filtered = append(filtered, provider)
			}
		}
		providers = filtered
	}
	return response.Success(c, http.StatusOK, providers)
}

func (h *DevMachineHandler) GetPolicy(c echo.Context) error {
	workspace := middleware.GetWorkspace(c)
	policy, err := h.service.GetPolicy(c.Request().Context(), workspace.ID)
	if err != nil {
		return machineError(c, err)
	}
	return response.Success(c, http.StatusOK, policy)
}

func (h *DevMachineHandler) UpdatePolicy(c echo.Context) error {
	c.Request().Body = http.MaxBytesReader(c.Response().Writer, c.Request().Body, 256*1024)
	var request dto.DevMachinePolicyRequest
	if err := bindAndValidate(c, &request); err != nil {
		return err
	}
	workspace := middleware.GetWorkspace(c)
	policy, err := h.service.UpdatePolicy(c.Request().Context(), workspace.ID, request)
	if err != nil {
		return machineError(c, err)
	}
	return response.Success(c, http.StatusOK, policy)
}

func (h *DevMachineHandler) IngestEvent(c echo.Context) error {
	c.Request().Body = http.MaxBytesReader(c.Response().Writer, c.Request().Body, 1024*1024)
	var input dto.CollectorEventInput
	if err := bindAndValidate(c, &input); err != nil {
		return err
	}
	if err := h.service.IngestEvent(c.Request().Context(), bearerToken(c), input); err != nil {
		if errors.Is(err, service.ErrMachineAuthentication) {
			return response.Unauthorized(c)
		}
		return machineError(c, err)
	}
	return c.NoContent(http.StatusAccepted)
}

func (h *DevMachineHandler) IngestLog(c echo.Context) error {
	c.Request().Body = http.MaxBytesReader(c.Response().Writer, c.Request().Body, 128*1024)
	var input dto.CollectorLogInput
	if err := bindAndValidate(c, &input); err != nil {
		return err
	}
	if err := h.service.IngestLog(c.Request().Context(), bearerToken(c), input); err != nil {
		if errors.Is(err, service.ErrMachineAuthentication) {
			return response.Unauthorized(c)
		}
		return machineError(c, err)
	}
	return c.NoContent(http.StatusAccepted)
}

func bearerToken(c echo.Context) string {
	parts := strings.Fields(c.Request().Header.Get("Authorization"))
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		return parts[1]
	}
	return ""
}

func bindAndValidate(c echo.Context, request any) error {
	if err := c.Bind(request); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid request body")
	}
	if err := validate.Struct(request); err != nil {
		details := make([]dto.ErrorDetail, 0)
		for _, item := range validate.FormatErrors(err) {
			details = append(details, dto.ErrorDetail{Field: item["field"], Message: item["message"]})
		}
		return response.ValidationError(c, details)
	}
	return nil
}

func operationResponse(operation *domain.DevMachineOperation) dto.DevMachineOperationResponse {
	return dto.DevMachineOperationResponse{
		ID: operation.ID.String(), Action: string(operation.Action), Status: string(operation.Status),
		Generation: operation.Generation, IdempotencyKey: operation.IdempotencyKey, Attempts: operation.Attempts,
		ErrorCode: operation.ErrorCode, ErrorMessage: operation.ErrorMessage, CreatedAt: operation.CreatedAt,
		CompletedAt: operation.CompletedAt,
	}
}

func machineError(c echo.Context, err error) error {
	switch {
	case errors.Is(err, service.ErrMachineNotFound):
		return response.NotFound(c, "Dev Machine")
	case errors.Is(err, service.ErrAgentRunNotFound):
		return response.NotFound(c, "Agent Run")
	case errors.Is(err, service.ErrEnvironmentNotFound):
		return response.NotFound(c, "Development Environment")
	case errors.Is(err, service.ErrCheckoutNotFound):
		return response.NotFound(c, "Checkout")
	case errors.Is(err, service.ErrTerminalSessionNotFound):
		return response.NotFound(c, "Terminal Session")
	case errors.Is(err, service.ErrDevMachinesDisabled), errors.Is(err, service.ErrProviderNotAllowed), errors.Is(err, service.ErrRepositoryNotAllowed):
		return response.Error(c, http.StatusForbidden, "FORBIDDEN", err.Error())
	case errors.Is(err, service.ErrMachineAuthentication):
		return response.Unauthorized(c)
	case errors.Is(err, service.ErrMachineQuota):
		return response.Error(c, http.StatusConflict, "QUOTA_EXCEEDED", err.Error())
	case errors.Is(err, service.ErrMachineNameConflict):
		return response.Error(c, http.StatusConflict, "MACHINE_NAME_CONFLICT", "Machine name is already in use")
	case errors.Is(err, service.ErrInvalidOperation):
		return response.Error(c, http.StatusConflict, "INVALID_OPERATION", err.Error())
	case errors.Is(err, service.ErrCheckoutNotEligible):
		return response.Error(c, http.StatusConflict, "CHECKOUT_NOT_ELIGIBLE", err.Error())
	case errors.Is(err, service.ErrCheckoutNotReady):
		return response.Error(c, http.StatusConflict, "CHECKOUT_NOT_READY", err.Error())
	case errors.Is(err, service.ErrTerminalSessionRequired):
		return response.Error(c, http.StatusConflict, "TERMINAL_SESSION_REQUIRED", err.Error())
	case errors.Is(err, service.ErrEnvironmentInUse):
		return response.Error(c, http.StatusConflict, "ENVIRONMENT_IN_USE", err.Error())
	case errors.Is(err, service.ErrEnvironmentInvalidState):
		return response.Error(c, http.StatusConflict, "ENVIRONMENT_INVALID_STATE", err.Error())
	case errors.Is(err, service.ErrEnvironmentCleanupActive):
		return response.Error(c, http.StatusConflict, "ENVIRONMENT_CLEANUP_ACTIVE", err.Error())
	case errors.Is(err, service.ErrServiceNotAvailable):
		return response.Error(c, http.StatusNotFound, "SERVICE_NOT_AVAILABLE", err.Error())
	case errors.Is(err, service.ErrInvalidMachineInput):
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	default:
		return response.InternalError(c)
	}
}
