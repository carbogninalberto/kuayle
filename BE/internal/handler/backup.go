package handler

import (
	"errors"
	"net/http"
	"path/filepath"
	"strconv"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/labstack/echo/v5"
	log "github.com/sirupsen/logrus"
)

// BackupHandler serves Settings -> Backups. Backups contain every
// workspace, so every route is limited to instance sysadmins (SYSADMINS),
// not workspace owners or admins.
type BackupHandler struct {
	svc        *service.BackupService
	isSysAdmin func(uuid.UUID) bool
	backupDir  string
}

func NewBackupHandler(svc *service.BackupService, isSysAdmin func(uuid.UUID) bool, backupDir string) *BackupHandler {
	if isSysAdmin == nil {
		isSysAdmin = func(uuid.UUID) bool { return false }
	}
	return &BackupHandler{svc: svc, isSysAdmin: isSysAdmin, backupDir: backupDir}
}

func (h *BackupHandler) allowed(c *echo.Context) bool {
	return h.isSysAdmin(middleware.GetUserID(c))
}

func backupError(c *echo.Context, err error) error {
	var invalid *service.BackupValidationError
	switch {
	case errors.As(err, &invalid):
		return response.Error(c, http.StatusBadRequest, "INVALID_BACKUP", invalid.Message)
	case errors.Is(err, service.ErrBackupRunning):
		return response.Error(c, http.StatusConflict, "BACKUP_RUNNING", "A backup is already running")
	case errors.Is(err, service.ErrBackupRunNotFound):
		return response.NotFound(c, "Backup")
	default:
		// Repository errors carry no secrets, but stay out of the response.
		log.WithError(err).Error("backup request failed")
		return response.InternalError(c)
	}
}

func (h *BackupHandler) GetSettings(c *echo.Context) error {
	if !h.allowed(c) {
		return response.Forbidden(c)
	}
	settings, err := h.svc.GetSettings(c.Request().Context())
	if err != nil {
		return backupError(c, err)
	}
	return response.Success(c, http.StatusOK, h.settingsResponse(settings))
}

func (h *BackupHandler) UpdateSettings(c *echo.Context) error {
	userID := middleware.GetUserID(c)
	if !h.isSysAdmin(userID) {
		return response.Forbidden(c)
	}
	var req dto.UpdateBackupSettingsRequest
	if err := bindConversation(c, &req); err != nil {
		return response.Error(c, http.StatusBadRequest, "INVALID_BACKUP", "Invalid backup settings")
	}
	settings, err := h.svc.UpdateSettings(c.Request().Context(), userID, service.BackupSettingsInput{
		Enabled:           req.Enabled,
		Schedule:          domain.BackupSchedule{Kind: req.ScheduleKind, Time: req.ScheduleTime, Weekday: req.ScheduleWeekday, Cron: req.ScheduleCron},
		Retention:         domain.BackupRetention{Kind: req.RetentionKind, Value: req.RetentionValue},
		S3Enabled:         req.S3Enabled,
		S3Endpoint:        req.S3Endpoint,
		S3Bucket:          req.S3Bucket,
		S3Region:          req.S3Region,
		S3Prefix:          req.S3Prefix,
		S3AccessKey:       req.S3AccessKey,
		S3SecretKey:       req.S3SecretKey,
		ClearS3Secrets:    req.ClearS3Credentials,
		EncryptionEnabled: req.EncryptionEnabled,
		Passphrase:        req.Passphrase,
		ClearPassphrase:   req.ClearPassphrase,
	})
	if err != nil {
		return backupError(c, err)
	}
	log.WithField("user_id", userID).Info("backup settings updated")
	return response.Success(c, http.StatusOK, h.settingsResponse(settings))
}

func (h *BackupHandler) TestConnection(c *echo.Context) error {
	if !h.allowed(c) {
		return response.Forbidden(c)
	}
	var req dto.TestBackupConnectionRequest
	if err := bindConversation(c, &req); err != nil {
		return response.Error(c, http.StatusBadRequest, "INVALID_BACKUP", "Invalid connection settings")
	}
	err := h.svc.TestConnection(c.Request().Context(), service.BackupS3Destination{
		Endpoint: req.S3Endpoint, Bucket: req.S3Bucket, Region: req.S3Region, Prefix: req.S3Prefix,
		AccessKey: req.S3AccessKey, SecretKey: req.S3SecretKey,
	})
	if err != nil {
		return backupError(c, err)
	}
	return response.Success(c, http.StatusOK, map[string]bool{"ok": true})
}

func (h *BackupHandler) RunNow(c *echo.Context) error {
	userID := middleware.GetUserID(c)
	if !h.isSysAdmin(userID) {
		return response.Forbidden(c)
	}
	run, err := h.svc.RunNow(c.Request().Context(), userID)
	if err != nil {
		return backupError(c, err)
	}
	log.WithFields(log.Fields{"user_id": userID, "run_id": run.ID}).Info("manual backup started")
	return response.Success(c, http.StatusAccepted, backupRunResponse(*run))
}

func (h *BackupHandler) ListRuns(c *echo.Context) error {
	if !h.allowed(c) {
		return response.Forbidden(c)
	}
	limit, _ := strconv.Atoi(c.QueryParam("limit"))
	offset, _ := strconv.Atoi(c.QueryParam("offset"))
	runs, err := h.svc.ListRuns(c.Request().Context(), limit, offset)
	if err != nil {
		return backupError(c, err)
	}
	out := dto.BackupRunsResponse{Runs: make([]dto.BackupRunResponse, 0, len(runs))}
	for _, run := range runs {
		out.Runs = append(out.Runs, backupRunResponse(run))
	}
	return response.Success(c, http.StatusOK, out)
}

func (h *BackupHandler) Verify(c *echo.Context) error {
	if !h.allowed(c) {
		return response.Forbidden(c)
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return response.NotFound(c, "Backup")
	}
	run, err := h.svc.Verify(c.Request().Context(), id)
	if err != nil {
		return backupError(c, err)
	}
	return response.Success(c, http.StatusOK, backupRunResponse(*run))
}

func (h *BackupHandler) settingsResponse(s *domain.BackupSettings) dto.BackupSettingsResponse {
	return dto.BackupSettingsResponse{
		Enabled:           s.Enabled,
		ScheduleKind:      s.BackupSchedule.Kind,
		ScheduleTime:      s.Time,
		ScheduleWeekday:   s.Weekday,
		ScheduleCron:      s.Cron,
		RetentionKind:     s.BackupRetention.Kind,
		RetentionValue:    s.Value,
		S3Enabled:         s.S3Enabled,
		S3Endpoint:        s.S3Endpoint,
		S3Bucket:          s.S3Bucket,
		S3Region:          s.S3Region,
		S3Prefix:          s.S3Prefix,
		HasAccessKey:      s.S3AccessKeyEncrypted != "",
		HasSecretKey:      s.S3SecretKeyEncrypted != "",
		EncryptionEnabled: s.EncryptionEnabled,
		HasPassphrase:     s.PassphraseEncrypted != "",
		NextRunAt:         s.NextRunAt,
		UpdatedAt:         s.UpdatedAt,
		BackupDir:         h.backupDir,
	}
}

func backupRunResponse(run domain.BackupRun) dto.BackupRunResponse {
	name := ""
	if run.LocalPath != "" {
		name = filepath.Base(run.LocalPath)
	}
	return dto.BackupRunResponse{
		ID: run.ID, Kind: run.Kind, Status: run.Status, UploadStatus: run.UploadStatus,
		StartedAt: run.StartedAt, FinishedAt: run.FinishedAt, DurationMS: run.DurationMS,
		SizeBytes: run.SizeBytes, ChecksumSHA256: run.ChecksumSHA256, FileName: name,
		S3Key: run.S3Key, S3Bucket: run.S3Bucket, Destination: run.Destination, Encrypted: run.Encrypted, Error: run.Error,
		VerifiedAt: run.VerifiedAt, VerifyError: run.VerifyError, DeletedAt: run.DeletedAt,
	}
}
