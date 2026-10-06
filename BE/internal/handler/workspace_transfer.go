package handler

import (
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strings"

	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/labstack/echo/v4"
	log "github.com/sirupsen/logrus"
)

const maxWorkspaceImportRequestBytes = int64(1024*1024*1024 + 1024*1024)

type WorkspaceTransferHandler struct {
	service *service.WorkspaceTransferService
}

func NewWorkspaceTransferHandler(service *service.WorkspaceTransferService) *WorkspaceTransferHandler {
	return &WorkspaceTransferHandler{service: service}
}

func (h *WorkspaceTransferHandler) Export(c echo.Context) error {
	workspace := middleware.GetWorkspace(c)
	if workspace == nil {
		return response.NotFound(c, "Workspace")
	}
	archive, err := h.service.Export(c.Request().Context(), workspace, middleware.GetUserID(c))
	if err != nil {
		var transferErr *service.WorkspaceTransferError
		if errors.As(err, &transferErr) {
			return transferHTTPError(c, err)
		}
		log.WithError(err).WithFields(log.Fields{
			"workspace_id":   workspace.ID.String(),
			"workspace_slug": workspace.Slug,
			"actor_id":       middleware.GetUserID(c).String(),
		}).Error("workspace export failed")
		return response.InternalError(c)
	}
	defer os.Remove(archive.Path)
	c.Response().Header().Set(echo.HeaderContentDisposition, `attachment; filename="`+archive.Filename+`"`)
	c.Response().Header().Set(echo.HeaderContentType, "application/zip")
	return c.File(archive.Path)
}

func (h *WorkspaceTransferHandler) Preview(c echo.Context) error {
	path, cleanup, err := receiveWorkspaceArchive(c)
	if err != nil {
		return err
	}
	defer cleanup()
	preview, err := h.service.Preview(c.Request().Context(), path, middleware.GetUserID(c))
	if err != nil {
		return transferHTTPError(c, err)
	}
	return response.Success(c, http.StatusOK, preview)
}

func (h *WorkspaceTransferHandler) Import(c echo.Context) error {
	path, cleanup, err := receiveWorkspaceArchive(c)
	if err != nil {
		return err
	}
	defer cleanup()
	result, err := h.service.Import(
		c.Request().Context(), path, c.FormValue("name"), c.FormValue("slug"), middleware.GetUserID(c),
	)
	if err != nil {
		return transferHTTPError(c, err)
	}
	return response.Success(c, http.StatusCreated, result)
}

func receiveWorkspaceArchive(c echo.Context) (string, func(), error) {
	c.Request().Body = http.MaxBytesReader(c.Response().Writer, c.Request().Body, maxWorkspaceImportRequestBytes)
	cleanupMultipart := func() {
		if c.Request().MultipartForm != nil {
			_ = c.Request().MultipartForm.RemoveAll()
		}
	}
	fileHeader, err := c.FormFile("file")
	if err != nil {
		cleanupMultipart()
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return "", func() {}, response.Error(c, http.StatusRequestEntityTooLarge, "WORKSPACE_ARCHIVE_TOO_LARGE", "Workspace archive is too large")
		}
		return "", func() {}, response.Error(c, http.StatusBadRequest, "INVALID_WORKSPACE_ARCHIVE", "A workspace archive is required")
	}
	source, err := openMultipartFile(fileHeader)
	if err != nil {
		cleanupMultipart()
		return "", func() {}, response.Error(c, http.StatusBadRequest, "INVALID_WORKSPACE_ARCHIVE", "Cannot read workspace archive")
	}
	defer source.Close()
	temp, err := os.CreateTemp("", "kuayle-workspace-import-*.zip")
	if err != nil {
		cleanupMultipart()
		return "", func() {}, response.InternalError(c)
	}
	cleanup := func() {
		_ = os.Remove(temp.Name())
		cleanupMultipart()
	}
	written, copyErr := io.Copy(temp, io.LimitReader(source, maxWorkspaceImportRequestBytes+1))
	closeErr := temp.Close()
	if copyErr != nil || closeErr != nil {
		cleanup()
		return "", func() {}, response.Error(c, http.StatusBadRequest, "INVALID_WORKSPACE_ARCHIVE", "Cannot read workspace archive")
	}
	if written > maxWorkspaceImportRequestBytes {
		cleanup()
		return "", func() {}, response.Error(c, http.StatusRequestEntityTooLarge, "WORKSPACE_ARCHIVE_TOO_LARGE", "Workspace archive is too large")
	}
	return temp.Name(), cleanup, nil
}

func openMultipartFile(header *multipart.FileHeader) (multipart.File, error) { return header.Open() }

func transferHTTPError(c echo.Context, err error) error {
	var transferErr *service.WorkspaceTransferError
	if !errors.As(err, &transferErr) {
		return response.InternalError(c)
	}
	status := http.StatusBadRequest
	switch transferErr.Code {
	case "WORKSPACE_SLUG_TAKEN":
		status = http.StatusConflict
	case "UNSUPPORTED_WORKSPACE_ARCHIVE":
		status = http.StatusUnprocessableEntity
	case "PRIVATE_WORKSPACE_TRANSFER_DISABLED":
		status = http.StatusForbidden
	}
	details := make([]dto.ErrorDetail, 0, len(transferErr.MissingUsers))
	for _, email := range transferErr.MissingUsers {
		details = append(details, dto.ErrorDetail{Field: "missing_user", Message: strings.ToLower(email)})
	}
	if len(details) == 0 {
		return response.Error(c, status, transferErr.Code, transferErr.Message)
	}
	return c.JSON(status, dto.ErrorResponse{Error: dto.ErrorBody{Code: transferErr.Code, Message: transferErr.Message, Details: details}})
}
