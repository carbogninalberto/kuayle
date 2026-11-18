package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/importer"
	"github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/response"
)

// ImportJobHandler only binds requests and maps results; the service owns the
// lifecycle and the middleware in routes.go enforces workspace-admin access.
type ImportJobHandler struct {
	svc *service.ImportService
}

func NewImportJobHandler(svc *service.ImportService) *ImportJobHandler {
	return &ImportJobHandler{svc: svc}
}

func importHTTPError(c *echo.Context, err error) error {
	var validation *service.ImportValidationError
	var source *service.ImportSourceError
	switch {
	case errors.As(err, &validation):
		return response.Error(c, http.StatusBadRequest, "INVALID_IMPORT", validation.Message)
	case errors.As(err, &source):
		// The tracker refused or could not be reached: a gateway failure with
		// a message the admin can act on, not a bare 500.
		return response.Error(c, http.StatusBadGateway, "IMPORT_SOURCE_FAILED", source.Message)
	case errors.Is(err, service.ErrImportNotFound):
		return response.NotFound(c, "Import")
	case errors.Is(err, service.ErrImportNotEditable):
		return response.Error(c, http.StatusConflict, "IMPORT_NOT_EDITABLE", "This import can no longer be changed")
	case errors.Is(err, service.ErrPreviewRequired):
		return response.Error(c, http.StatusConflict, "PREVIEW_REQUIRED", "Preview this import before starting it")
	case errors.Is(err, service.ErrInvalidImportMapping):
		return response.Error(c, http.StatusBadRequest, "INVALID_IMPORT", err.Error())
	default:
		return response.InternalError(c)
	}
}

func importID(c *echo.Context) uuid.UUID {
	id, err := parseUUID(c.Param("id"))
	if err != nil {
		return uuid.Nil
	}
	return id
}

func importContext(c *echo.Context) (*domain.Workspace, uuid.UUID, error) {
	ws := middleware.GetWorkspace(c)
	if ws == nil {
		return nil, uuid.Nil, response.NotFound(c, "Workspace")
	}
	return ws, middleware.GetUserID(c), nil
}

func (h *ImportJobHandler) Create(c *echo.Context) error {
	ws, userID, err := importContext(c)
	if err != nil {
		return err
	}
	var req dto.CreateImportRequest
	if err := bindConversation(c, &req); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid request")
	}
	job, err := h.svc.Create(c.Request().Context(), ws.ID, userID, service.ImportCreateInput{
		Source: req.Source, Config: req.Config, Credentials: req.Credentials,
	})
	if err != nil {
		return importHTTPError(c, err)
	}
	return response.Success(c, http.StatusCreated, importJobResponse(job))
}

func (h *ImportJobHandler) List(c *echo.Context) error {
	ws, _, err := importContext(c)
	if err != nil {
		return err
	}
	jobs, err := h.svc.List(c.Request().Context(), ws.ID, 50, 0)
	if err != nil {
		return importHTTPError(c, err)
	}
	out := make([]dto.ImportJobResponse, 0, len(jobs))
	for _, job := range jobs {
		out = append(out, importJobResponse(&job))
	}
	return response.Success(c, http.StatusOK, dto.ImportJobsResponse{Imports: out})
}

func (h *ImportJobHandler) Get(c *echo.Context) error {
	ws, _, err := importContext(c)
	if err != nil {
		return err
	}
	job, err := h.svc.Get(c.Request().Context(), ws.ID, importID(c))
	if err != nil {
		return importHTTPError(c, err)
	}
	return response.Success(c, http.StatusOK, importJobResponse(job))
}

func (h *ImportJobHandler) UpdateMapping(c *echo.Context) error {
	ws, _, err := importContext(c)
	if err != nil {
		return err
	}
	var req dto.UpdateImportMappingRequest
	if err := bindConversation(c, &req); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid request")
	}
	if err := h.svc.UpdateMapping(c.Request().Context(), ws.ID, importID(c), req.Mapping); err != nil {
		return importHTTPError(c, err)
	}
	return h.Get(c)
}

func (h *ImportJobHandler) Preview(c *echo.Context) error {
	ws, _, err := importContext(c)
	if err != nil {
		return err
	}
	preview, err := h.svc.Preview(c.Request().Context(), ws.ID, importID(c))
	if err != nil {
		return importHTTPError(c, err)
	}
	return response.Success(c, http.StatusOK, importPreviewResponse(preview))
}

func (h *ImportJobHandler) Start(c *echo.Context) error {
	ws, _, err := importContext(c)
	if err != nil {
		return err
	}
	if err := h.svc.Start(c.Request().Context(), ws.ID, importID(c)); err != nil {
		return importHTTPError(c, err)
	}
	return h.Get(c)
}

func (h *ImportJobHandler) Cancel(c *echo.Context) error {
	ws, _, err := importContext(c)
	if err != nil {
		return err
	}
	if err := h.svc.Cancel(c.Request().Context(), ws.ID, importID(c)); err != nil {
		return importHTTPError(c, err)
	}
	return h.Get(c)
}

// defaultItemPage is the item-report page size when the client sends none.
const defaultItemPage = 200

// parseItemPage reads the item-report page size and offset from query strings.
// The service clamps the size to repository.ImportItemsPageMax and detects
// whether another page exists.
func parseItemPage(limitRaw, offsetRaw string) (limit, offset int) {
	limit = defaultItemPage
	if n, err := strconv.Atoi(limitRaw); err == nil {
		limit = n
	}
	if n, err := strconv.Atoi(offsetRaw); err == nil && n >= 0 {
		offset = n
	}
	return limit, offset
}

func (h *ImportJobHandler) ListItems(c *echo.Context) error {
	ws, _, err := importContext(c)
	if err != nil {
		return err
	}
	limit, offset := parseItemPage(c.QueryParam("limit"), c.QueryParam("offset"))
	items, hasMore, err := h.svc.ListItems(c.Request().Context(), ws.ID, importID(c), limit, offset)
	if err != nil {
		return importHTTPError(c, err)
	}
	out := make([]dto.ImportItemResponse, 0, len(items))
	for _, item := range items {
		out = append(out, dto.ImportItemResponse{
			ExternalType: item.ExternalType, ExternalID: item.ExternalID, ExternalKey: item.ExternalKey,
			Outcome: item.Outcome, Reason: item.Reason, CreatedAt: item.CreatedAt,
		})
	}
	return response.Success(c, http.StatusOK, dto.ImportItemsResponse{Items: out, HasMore: hasMore})
}

func (h *ImportJobHandler) Options(c *echo.Context) error {
	ws, _, err := importContext(c)
	if err != nil {
		return err
	}
	options, err := h.svc.Options(c.Request().Context(), ws.ID)
	if err != nil {
		return importHTTPError(c, err)
	}
	teams := make([]dto.ImportTeamOption, 0, len(options.Teams))
	for _, team := range options.Teams {
		teams = append(teams, dto.ImportTeamOption{ID: team.ID, Name: team.Name, Key: team.Key, IsPrivate: team.IsPrivate})
	}
	statuses := make(map[uuid.UUID][]dto.ImportStatusOption, len(options.Statuses))
	for teamID, list := range options.Statuses {
		converted := make([]dto.ImportStatusOption, 0, len(list))
		for _, status := range list {
			converted = append(converted, dto.ImportStatusOption{ID: status.ID, Name: status.Name, Category: string(status.Category)})
		}
		statuses[teamID] = converted
	}
	members := make([]dto.ImportMemberOption, 0, len(options.Members))
	for _, member := range options.Members {
		members = append(members, dto.ImportMemberOption{UserID: member.UserID, Email: member.Email, DisplayName: member.DisplayName})
	}
	return response.Success(c, http.StatusOK, dto.ImportOptionsResponse{Teams: teams, Statuses: statuses, Members: members})
}

func importJobResponse(job *domain.ImportJob) dto.ImportJobResponse {
	return dto.ImportJobResponse{
		ID: job.ID, Source: job.Source, SourceSite: job.SourceSite, Status: job.Status,
		Config: job.Config, Mapping: job.Mapping, Preview: job.Preview,
		Total: job.Total, Processed: job.Processed, CreatedCount: job.CreatedCount,
		UpdatedCount: job.UpdatedCount, SkippedCount: job.SkippedCount, FailedCount: job.FailedCount,
		Error: job.Error, CancelRequested: job.CancelRequested,
		CreatedAt: job.CreatedAt, UpdatedAt: job.UpdatedAt, StartedAt: job.StartedAt, FinishedAt: job.FinishedAt,
	}
}

func importPreviewResponse(preview *importer.SourcePreview) dto.ImportPreviewResponse {
	users := make([]dto.ImportSourceUser, 0, len(preview.Users))
	for _, user := range preview.Users {
		users = append(users, dto.ImportSourceUser{ExternalID: user.ExternalID, Email: user.Email, DisplayName: user.DisplayName})
	}
	projects := make([]dto.ImportSourceProject, 0, len(preview.Projects))
	for _, project := range preview.Projects {
		projects = append(projects, dto.ImportSourceProject{ExternalID: project.ExternalID, Name: project.Name, Key: project.Key})
	}
	return dto.ImportPreviewResponse{
		Counts: preview.Counts, Statuses: preview.Statuses, Priorities: preview.Priorities,
		Labels: preview.Labels, Users: users, Projects: projects, UnmappedFields: preview.UnmappedFields,
		MappingGaps: preview.MappingGaps,
	}
}
