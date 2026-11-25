package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/config"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/labstack/echo/v5"
	log "github.com/sirupsen/logrus"
)

// InstanceConfigHandler serves Settings -> Instance configuration: every
// variable the API reads, for instance sysadmins (SYSADMINS) only, and the
// runtime overrides of the few a running server reads per use.
type InstanceConfigHandler struct {
	svc        *service.InstanceConfigService
	isSysAdmin func(uuid.UUID) bool
}

func NewInstanceConfigHandler(svc *service.InstanceConfigService, isSysAdmin func(uuid.UUID) bool) *InstanceConfigHandler {
	if isSysAdmin == nil {
		isSysAdmin = func(uuid.UUID) bool { return false }
	}
	return &InstanceConfigHandler{svc: svc, isSysAdmin: isSysAdmin}
}

// allowed marks every response uncacheable, then admits sysadmins only.
func (h *InstanceConfigHandler) allowed(c *echo.Context) (uuid.UUID, bool) {
	c.Response().Header().Set("Cache-Control", "private, no-store")
	user := middleware.GetUserID(c)
	return user, h.isSysAdmin(user)
}

func instanceConfigError(c *echo.Context, err error) error {
	var invalid *service.InstanceConfigInvalidError
	switch {
	case errors.As(err, &invalid):
		return response.Error(c, http.StatusBadRequest, "INVALID_CONFIG_VALUE", invalid.Message)
	case errors.Is(err, service.ErrInstanceConfigReadOnly):
		return response.Error(c, http.StatusConflict, "CONFIG_READ_ONLY", "This variable is set in .env and needs a restart")
	case errors.Is(err, service.ErrInstanceConfigUnknown):
		return response.NotFound(c, "Configuration variable")
	case errors.Is(err, service.ErrInstanceRestartUnsupported):
		return response.Error(c, http.StatusConflict, "RESTART_UNSUPPORTED",
			"Apply and restart needs the API to run under a supervisor that starts it again; set RESTART_SUPERVISED=true where one does")
	case errors.Is(err, service.ErrInstanceRestartActive):
		return response.Error(c, http.StatusConflict, "RESTART_IN_PROGRESS", "An apply and restart is already in progress")
	case errors.Is(err, service.ErrInstanceRestartNoChanges):
		return response.Error(c, http.StatusBadRequest, "NO_CHANGES", "No setting would change")
	default:
		log.WithError(err).Error("instance configuration request failed")
		return response.InternalError(c)
	}
}

func (h *InstanceConfigHandler) List(c *echo.Context) error {
	if _, ok := h.allowed(c); !ok {
		return response.Forbidden(c)
	}
	entries, err := h.svc.List(c.Request().Context())
	if err != nil {
		return instanceConfigError(c, err)
	}
	status, err := h.svc.RestartStatus(c.Request().Context())
	if err != nil {
		return instanceConfigError(c, err)
	}
	out := dto.InstanceConfigResponse{
		Groups: config.Groups(), Variables: make([]dto.InstanceConfigVariableResponse, 0, len(entries)),
		Restart: dto.InstanceRestartStatusResponse{Supported: status.Supported, Notice: status.Notice},
	}
	for _, entry := range entries {
		out.Variables = append(out.Variables, instanceConfigVariable(entry))
	}
	if status.Run != nil {
		run := restartRunResponse(*status.Run)
		out.Restart.Run = &run
	}
	return response.Success(c, http.StatusOK, out)
}

// ValidateRestart is the dry run of restart-only values: POST
// {"changes": {"NAME": "value" | null}}. It checks type, range, enumeration,
// URL format and the whole configuration, and stores nothing.
func (h *InstanceConfigHandler) ValidateRestart(c *echo.Context) error {
	if _, ok := h.allowed(c); !ok {
		return response.Forbidden(c)
	}
	var req dto.InstanceRestartRequest
	if err := bindConversation(c, &req); err != nil || len(req.Changes) == 0 {
		return response.Error(c, http.StatusBadRequest, "INVALID_CONFIG_VALUE", "Changes are required")
	}
	plan, err := h.svc.PlanRestart(c.Request().Context(), req.Changes)
	if err != nil {
		return instanceConfigError(c, err)
	}
	return response.Success(c, http.StatusOK, restartPlanResponse(plan))
}

// ApplyRestart stores restart-only values and restarts the API: POST
// {"changes": {...}}. The values are validated again; the API exits shortly
// after the response, and its supervisor starts it with the new values. It
// answers 202 with the run, whose status the page then polls.
func (h *InstanceConfigHandler) ApplyRestart(c *echo.Context) error {
	user, ok := h.allowed(c)
	if !ok {
		return response.Forbidden(c)
	}
	var req dto.InstanceRestartRequest
	if err := bindConversation(c, &req); err != nil || len(req.Changes) == 0 {
		return response.Error(c, http.StatusBadRequest, "INVALID_CONFIG_VALUE", "Changes are required")
	}
	run, err := h.svc.ApplyRestart(c.Request().Context(), user, req.Changes)
	var invalid *service.RestartInvalidError
	if errors.As(err, &invalid) {
		details := make([]dto.ErrorDetail, 0, len(invalid.Issues))
		for _, issue := range invalid.Issues {
			details = append(details, dto.ErrorDetail{Field: issue.Name, Message: issue.Message})
		}
		return c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: dto.ErrorBody{
			Code: "INVALID_CONFIG_VALUE", Message: invalid.Error(), Details: details,
		}})
	}
	if err != nil {
		return instanceConfigError(c, err)
	}
	return response.Success(c, http.StatusAccepted, restartRunResponse(*run))
}

func restartRunResponse(run domain.InstanceRestartRun) dto.InstanceRestartRunResponse {
	return dto.InstanceRestartRunResponse{
		ID: run.ID, Status: run.Status, RequestedBy: run.RequestedBy, RequestedAt: run.RequestedAt,
		FinishedAt: run.FinishedAt, Failure: run.Failure, Changes: restartChangesResponse(run.Changes),
	}
}

func restartChangesResponse(changes []domain.InstanceRestartChange) []dto.InstanceRestartChangeResponse {
	out := make([]dto.InstanceRestartChangeResponse, 0, len(changes))
	for _, change := range changes {
		out = append(out, dto.InstanceRestartChangeResponse{Name: change.Name, From: change.From, To: change.To})
	}
	return out
}

func restartPlanResponse(plan *service.RestartPlan) dto.InstanceRestartPlanResponse {
	out := dto.InstanceRestartPlanResponse{Valid: plan.Valid(), Changes: restartChangesResponse(plan.Changes), Issues: []dto.InstanceRestartIssueResponse{}}
	for _, issue := range plan.Issues {
		out.Issues = append(out.Issues, dto.InstanceRestartIssueResponse{Name: issue.Name, Message: issue.Message})
	}
	return out
}

// Update overrides a runtime variable: PUT {"value": "..."}.
func (h *InstanceConfigHandler) Update(c *echo.Context) error {
	user, ok := h.allowed(c)
	if !ok {
		return response.Forbidden(c)
	}
	var req dto.UpdateInstanceConfigRequest
	if err := bindConversation(c, &req); err != nil || req.Value == nil {
		return response.Error(c, http.StatusBadRequest, "INVALID_CONFIG_VALUE", "A value is required")
	}
	entry, err := h.svc.Set(c.Request().Context(), user, c.Param("name"), *req.Value)
	if err != nil {
		return instanceConfigError(c, err)
	}
	return response.Success(c, http.StatusOK, instanceConfigVariable(*entry))
}

// Reset returns a runtime variable to its environment value or default.
func (h *InstanceConfigHandler) Reset(c *echo.Context) error {
	user, ok := h.allowed(c)
	if !ok {
		return response.Forbidden(c)
	}
	entry, err := h.svc.Reset(c.Request().Context(), user, c.Param("name"))
	if err != nil {
		return instanceConfigError(c, err)
	}
	return response.Success(c, http.StatusOK, instanceConfigVariable(*entry))
}

func instanceConfigVariable(entry service.InstanceConfigEntry) dto.InstanceConfigVariableResponse {
	out := dto.InstanceConfigVariableResponse{
		Name: entry.Name, Group: entry.Group, Description: entry.Description, Kind: string(entry.Kind),
		Secret: entry.Secret, Set: entry.Set, Withheld: entry.Withheld, Source: string(entry.Source),
		Tunable: entry.Runtime != nil, UpdatedAt: entry.UpdatedAt, RestartEditable: entry.EditableOnRestart,
	}
	if entry.Secret {
		// Belt and braces: the registry never fills a secret's value.
		return out
	}
	out.Value, out.Default = entry.Value, entry.Default
	if entry.EditableOnRestart && entry.Restart != nil {
		out.BaseValue, out.BaseSource = entry.BaseValue, string(entry.BaseSource)
		out.Rule = restartRuleResponse(entry.Restart)
	}
	if entry.Runtime != nil {
		out.BaseValue, out.BaseSource = entry.BaseValue, string(entry.BaseSource)
		if entry.Runtime.Kind == config.KindDuration {
			out.Min, out.Max = config.FormatDuration(entry.Runtime.Min), config.FormatDuration(entry.Runtime.Max)
		}
	}
	return out
}

func restartRuleResponse(rule *config.RestartRule) *dto.InstanceConfigRuleResponse {
	out := &dto.InstanceConfigRuleResponse{Format: string(rule.Format), Choices: rule.Choices, AllowEmpty: rule.AllowEmpty}
	switch rule.Kind {
	case config.KindDuration:
		out.Min, out.Max = config.FormatDuration(rule.Min), config.FormatDuration(rule.Max)
	case config.KindInt:
		out.Min, out.Max = strconv.FormatInt(rule.IntMin, 10), strconv.FormatInt(rule.IntMax, 10)
	}
	return out
}
