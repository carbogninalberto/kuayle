package service

import (
	"archive/zip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/pkg/audit"
	"github.com/kuayle/kuayle-backend/pkg/storage"
)

const (
	maxWorkspaceArchiveBytes      = int64(1024 * 1024 * 1024)
	maxWorkspaceUncompressedBytes = int64(2 * 1024 * 1024 * 1024)
	maxWorkspaceDataBytes         = int64(256 * 1024 * 1024)
	maxWorkspaceAssetBytes        = int64(10 * 1024 * 1024)
	maxWorkspaceArchiveEntries    = 100000
)

var transferSlugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
var transferAssetExtension = regexp.MustCompile(`^\.[A-Za-z0-9]{1,12}$`)

var (
	ErrInvalidWorkspaceArchive = errors.New("invalid workspace archive")
	ErrUnsupportedArchive      = errors.New("unsupported workspace archive version")
	ErrWorkspaceImportSlug     = errors.New("workspace slug is already taken")
	ErrWorkspaceImportUsers    = errors.New("workspace import has missing users")
)

type WorkspaceTransferError struct {
	Code         string
	Message      string
	MissingUsers []string
	Cause        error
}

func (e *WorkspaceTransferError) Error() string { return e.Message }
func (e *WorkspaceTransferError) Unwrap() error { return e.Cause }

type WorkspaceTransferService struct {
	repo  *repository.WorkspaceTransferRepository
	store storage.Backend
}

func NewWorkspaceTransferService(repo *repository.WorkspaceTransferRepository, store storage.Backend) *WorkspaceTransferService {
	return &WorkspaceTransferService{repo: repo, store: store}
}

type ExportedWorkspaceArchive struct {
	Path     string
	Filename string
}

func (s *WorkspaceTransferService) Export(ctx context.Context, workspace *domain.Workspace, actorID uuid.UUID) (*ExportedWorkspaceArchive, error) {
	guard, err := s.repo.BeginPublicExport(ctx, workspace.ID)
	if err != nil {
		if errors.Is(err, repository.ErrPrivateWorkspaceTransfer) {
			return nil, &WorkspaceTransferError{Code: "PRIVATE_WORKSPACE_TRANSFER_DISABLED", Message: err.Error(), Cause: err}
		}
		return nil, err
	}
	defer func() { _ = guard.Rollback() }()
	exportRepo := s.repo.ExportReader(guard)
	workspaceRow, err := exportRepo.WorkspaceRow(ctx, workspace.ID)
	if err != nil {
		return nil, err
	}

	data := dto.WorkspaceTransferData{Workspace: workspaceRow, Tables: make(map[string][]dto.WorkspaceTransferRow)}
	userIDs := make(map[uuid.UUID]bool)
	assetStorageKeys := make(map[string]string)

	for _, spec := range repository.WorkspaceTransferTableSpecs() {
		rows, err := exportRepo.TableRows(ctx, spec, workspace.ID)
		if err != nil {
			return nil, fmt.Errorf("export %s: %w", spec.Name, err)
		}
		for _, row := range rows {
			if spec.Name == "assets" {
				assetID := transferString(row["id"])
				assetStorageKeys[assetID] = transferString(row["storage_key"])
				row["archive_path"] = "assets/" + assetID
			}
			sanitizeExportRow(spec.Name, row)
		}
		data.Tables[spec.Name] = rows
	}
	omittedSharedLinks := omitInvalidSharedLinks(data)
	collectUserID(userIDs, workspaceRow["owner_id"])
	for _, spec := range repository.WorkspaceTransferTableSpecs() {
		for _, row := range data.Tables[spec.Name] {
			for _, field := range spec.UserFields {
				collectUserID(userIDs, row[field])
			}
		}
	}

	ids := make([]uuid.UUID, 0, len(userIDs))
	for id := range userIDs {
		ids = append(ids, id)
	}
	data.Users, err = exportRepo.UsersByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	if len(data.Users) != len(ids) {
		return nil, fmt.Errorf("export contains references to missing users")
	}
	sourceUsers := make(map[string]string, len(data.Users))
	for _, row := range data.Users {
		id := transferString(row["id"])
		sourceUsers[id] = id
	}
	validationArchive := &parsedWorkspaceArchive{
		manifest: dto.WorkspaceExportManifest{SourceWorkspaceID: workspace.ID.String(), SourceWorkspaceSlug: workspace.Slug},
		data:     data,
	}
	validationMap, _, err := prepareIdentityMap(validationArchive, workspace.ID, sourceUsers)
	if err != nil {
		return nil, fmt.Errorf("validate workspace export: %w", err)
	}
	if err := validateArchiveReferences(validationArchive, validationMap); err != nil {
		return nil, fmt.Errorf("validate workspace export: %w", err)
	}

	counts := map[string]int{"users": len(data.Users)}
	for table, rows := range data.Tables {
		counts[table] = len(rows)
	}
	warnings := []string{
		"Shared-link tokens are rotated and imported links are disabled.",
		"Webhook secrets are regenerated and imported webhooks are disabled.",
		"Dev Machine policy is imported disabled; environment references are removed from scope settings.",
	}
	if omittedSharedLinks > 0 {
		warnings = append(warnings, fmt.Sprintf("Omitted %d shared link%s with a missing scope target.", omittedSharedLinks, pluralSuffix(omittedSharedLinks)))
	}
	manifest := dto.WorkspaceExportManifest{
		Format:              dto.WorkspaceExportFormat,
		Version:             dto.WorkspaceExportVersion,
		ExportedAt:          time.Now().UTC(),
		SourceSchemaVersion: exportRepo.SchemaVersion(ctx),
		SourceWorkspaceID:   workspace.ID.String(),
		SourceWorkspaceName: workspace.Name,
		SourceWorkspaceSlug: workspace.Slug,
		Counts:              counts,
		Omitted: []string{
			"users.password_hash", "refresh_tokens", "user_preferences", "webhooks.secret",
			"github_app_configs", "github_installations.access_token", "ai_settings.api_key_encrypted",
			"dev machine instances, environments, images, volumes, logs, artifacts, sessions, tokens, credentials and secret environment variables",
		},
		Warnings:                warnings,
		RequiresReconfiguration: []string{"webhooks", "GitHub", "AI provider credentials", "Dev Machine environments"},
	}

	temp, err := os.CreateTemp("", "kuayle-workspace-export-*.zip")
	if err != nil {
		return nil, err
	}
	path := temp.Name()
	cleanup := true
	defer func() {
		_ = temp.Close()
		if cleanup {
			_ = os.Remove(path)
		}
	}()

	zw := zip.NewWriter(temp)
	if err := writeZIPJSON(zw, "manifest.json", manifest); err != nil {
		return nil, err
	}
	if err := writeZIPJSON(zw, "data.json", data); err != nil {
		return nil, err
	}
	for _, row := range data.Tables["assets"] {
		oldID := transferString(row["id"])
		storageKey := assetStorageKeys[oldID]
		if storageKey == "" {
			return nil, fmt.Errorf("asset %s has no storage key", oldID)
		}
		rc, err := s.store.Get(ctx, storageKey)
		if err != nil {
			return nil, fmt.Errorf("read asset %s: %w", oldID, err)
		}
		entry, err := zw.Create("assets/" + oldID)
		var copied int64
		if err == nil {
			copied, err = io.Copy(entry, io.LimitReader(rc, maxWorkspaceAssetBytes+1))
			if err == nil && copied > maxWorkspaceAssetBytes {
				err = fmt.Errorf("asset %s exceeds transfer limit", oldID)
			} else if err == nil && copied != transferInt64(row["size"]) {
				err = fmt.Errorf("asset %s size does not match metadata", oldID)
			}
		}
		_ = rc.Close()
		if err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	if err := temp.Close(); err != nil {
		return nil, err
	}
	cleanup = false
	audit.Log("workspace.exported", actorID, map[string]interface{}{"workspace_id": workspace.ID})
	return &ExportedWorkspaceArchive{Path: path, Filename: safeTransferFilename(workspace.Slug) + ".kuayle.zip"}, nil
}

// omitInvalidSharedLinks removes links whose polymorphic target is absent from
// this workspace export. Shared links have no database foreign key for their
// scope target, so an old deleted team, project, or view can otherwise make a
// complete workspace archive fail reference validation.
func omitInvalidSharedLinks(data dto.WorkspaceTransferData) int {
	targets := map[string]map[string]bool{
		"team":    transferRowIDs(data.Tables["teams"]),
		"project": transferRowIDs(data.Tables["projects"]),
		"view":    transferRowIDs(data.Tables["views"]),
	}
	links := data.Tables["shared_links"]
	kept := make([]dto.WorkspaceTransferRow, 0, len(links))
	omitted := 0
	for _, link := range links {
		if validSharedLinkScopeTarget(link, targets) {
			kept = append(kept, link)
			continue
		}
		omitted++
	}
	data.Tables["shared_links"] = kept
	return omitted
}

func transferRowIDs(rows []dto.WorkspaceTransferRow) map[string]bool {
	ids := make(map[string]bool, len(rows))
	for _, row := range rows {
		if id := transferString(row["id"]); id != "" {
			ids[id] = true
		}
	}
	return ids
}

func validSharedLinkScopeTarget(link dto.WorkspaceTransferRow, targets map[string]map[string]bool) bool {
	scope := transferString(link["scope"])
	targetID := transferString(link["scope_id"])
	if scope == "workspace" {
		return targetID == ""
	}
	if _, err := uuid.Parse(targetID); err != nil {
		return false
	}
	return targets[scope][targetID]
}

func pluralSuffix(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}

func collectUserID(target map[uuid.UUID]bool, value any) {
	if id, err := uuid.Parse(transferString(value)); err == nil {
		target[id] = true
	}
}

func sanitizeExportRow(table string, row dto.WorkspaceTransferRow) {
	switch table {
	case "assets":
		delete(row, "storage_key")
	case "webhooks":
		delete(row, "secret")
		row["is_active"] = false
	case "github_installations":
		delete(row, "access_token")
		delete(row, "token_expires_at")
	case "github_repos", "github_auto_transitions":
		row["is_active"] = false
	case "shared_links":
		delete(row, "token")
		row["is_active"] = false
	case "ai_settings":
		delete(row, "api_key_encrypted")
	case "dev_machine_workspace_policies":
		row["enabled"] = false
	case "dev_machine_scope_settings":
		row["environment_id"] = nil
	}
}

func writeZIPJSON(zw *zip.Writer, name string, value any) error {
	entry, err := zw.Create(name)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(entry)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
}

func safeTransferFilename(slug string) string {
	value := strings.ToLower(strings.TrimSpace(slug))
	value = regexp.MustCompile(`[^a-z0-9-]+`).ReplaceAllString(value, "-")
	value = strings.Trim(value, "-")
	if value == "" {
		return "workspace"
	}
	return value
}

type parsedWorkspaceArchive struct {
	manifest dto.WorkspaceExportManifest
	data     dto.WorkspaceTransferData
	zip      *zip.ReadCloser
	entries  map[string]*zip.File
}

func parseWorkspaceArchive(path string) (*parsedWorkspaceArchive, error) {
	info, err := os.Stat(path)
	if err != nil || info.Size() <= 0 || info.Size() > maxWorkspaceArchiveBytes {
		return nil, invalidArchive("archive size is invalid", err)
	}
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, invalidArchive("file is not a valid ZIP archive", err)
	}
	fail := func(message string, cause error) (*parsedWorkspaceArchive, error) {
		_ = zr.Close()
		return nil, invalidArchive(message, cause)
	}
	if len(zr.File) == 0 || len(zr.File) > maxWorkspaceArchiveEntries {
		return fail("archive entry count is invalid", nil)
	}
	entries := make(map[string]*zip.File, len(zr.File))
	var total uint64
	for _, file := range zr.File {
		name := file.Name
		if name == "" || filepath.IsAbs(name) || filepath.Clean(name) != name || strings.Contains(name, "\\") || strings.HasPrefix(name, "../") {
			return fail("archive contains an unsafe path", nil)
		}
		if _, exists := entries[name]; exists {
			return fail("archive contains duplicate entries", nil)
		}
		if name != "manifest.json" && name != "data.json" && !strings.HasPrefix(name, "assets/") {
			return fail("archive contains an unknown entry", nil)
		}
		limit := uint64(maxWorkspaceUncompressedBytes)
		if file.UncompressedSize64 > limit || total > limit-file.UncompressedSize64 {
			return fail("archive expands beyond the allowed size", nil)
		}
		total += file.UncompressedSize64
		entries[name] = file
	}
	manifestFile, manifestOK := entries["manifest.json"]
	dataFile, dataOK := entries["data.json"]
	if !manifestOK || !dataOK {
		return fail("archive is missing manifest.json or data.json", nil)
	}
	manifestBytes, err := readZIPEntry(manifestFile, 1024*1024)
	if err != nil {
		return fail("cannot read manifest", err)
	}
	dataBytes, err := readZIPEntry(dataFile, maxWorkspaceDataBytes)
	if err != nil {
		return fail("cannot read workspace data", err)
	}
	var manifest dto.WorkspaceExportManifest
	if err := dto.DecodeTransferJSON(manifestBytes, &manifest); err != nil {
		return fail("manifest is invalid JSON", err)
	}
	if manifest.Format != dto.WorkspaceExportFormat || manifest.Version != dto.WorkspaceExportVersion {
		_ = zr.Close()
		return nil, &WorkspaceTransferError{Code: "UNSUPPORTED_WORKSPACE_ARCHIVE", Message: "This workspace archive version is not supported", Cause: ErrUnsupportedArchive}
	}
	var data dto.WorkspaceTransferData
	if err := dto.DecodeTransferJSON(dataBytes, &data); err != nil {
		return fail("workspace data is invalid JSON", err)
	}
	parsed := &parsedWorkspaceArchive{manifest: manifest, data: data, zip: zr, entries: entries}
	if err := validateWorkspaceArchive(parsed); err != nil {
		_ = zr.Close()
		return nil, err
	}
	return parsed, nil
}

func invalidArchive(message string, cause error) error {
	return &WorkspaceTransferError{Code: "INVALID_WORKSPACE_ARCHIVE", Message: message, Cause: errors.Join(ErrInvalidWorkspaceArchive, cause)}
}

func readZIPEntry(file *zip.File, limit int64) ([]byte, error) {
	if file.UncompressedSize64 > uint64(limit) {
		return nil, errors.New("entry is too large")
	}
	rc, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("entry is too large")
	}
	return data, nil
}

func validateWorkspaceArchive(parsed *parsedWorkspaceArchive) error {
	if parsed.data.Workspace == nil || parsed.data.Tables == nil {
		return invalidArchive("workspace data is incomplete", nil)
	}
	for _, team := range parsed.data.Tables["teams"] {
		if value, present := team["is_private"]; present {
			private, valid := value.(bool)
			if !valid || private {
				return &WorkspaceTransferError{Code: "PRIVATE_WORKSPACE_TRANSFER_DISABLED", Message: "Importing private teams is not supported", Cause: ErrUnsupportedArchive}
			}
		}
	}
	if parsed.manifest.SourceWorkspaceID == "" || transferString(parsed.data.Workspace["id"]) != parsed.manifest.SourceWorkspaceID {
		return invalidArchive("workspace identity does not match the manifest", nil)
	}
	if transferString(parsed.data.Workspace["slug"]) != parsed.manifest.SourceWorkspaceSlug ||
		transferString(parsed.data.Workspace["name"]) != parsed.manifest.SourceWorkspaceName {
		return invalidArchive("workspace metadata does not match the manifest", nil)
	}
	if _, err := uuid.Parse(parsed.manifest.SourceWorkspaceID); err != nil {
		return invalidArchive("source workspace ID is invalid", err)
	}
	expected := make(map[string]repository.WorkspaceTransferTableSpec)
	for _, spec := range repository.WorkspaceTransferTableSpecs() {
		expected[spec.Name] = spec
		if _, ok := parsed.data.Tables[spec.Name]; !ok {
			return invalidArchive("workspace data is missing table "+spec.Name, nil)
		}
	}
	for table, rows := range parsed.data.Tables {
		if _, ok := expected[table]; !ok {
			return invalidArchive("workspace data contains unknown table "+table, nil)
		}
		if count, ok := parsed.manifest.Counts[table]; !ok || count != len(rows) {
			return invalidArchive("table count mismatch for "+table, nil)
		}
	}
	if parsed.manifest.Counts["users"] != len(parsed.data.Users) {
		return invalidArchive("user count does not match the manifest", nil)
	}
	assetRows := parsed.data.Tables["assets"]
	assetPaths := make(map[string]bool, len(assetRows))
	for _, row := range assetRows {
		path := transferString(row["archive_path"])
		if path == "" || !strings.HasPrefix(path, "assets/") || parsed.entries[path] == nil {
			return invalidArchive("asset payload is missing", nil)
		}
		if assetPaths[path] {
			return invalidArchive("asset payload is referenced more than once", nil)
		}
		assetPaths[path] = true
	}
	for name := range parsed.entries {
		if strings.HasPrefix(name, "assets/") && !assetPaths[name] {
			return invalidArchive("archive contains an unreferenced asset", nil)
		}
	}
	return nil
}

func (s *WorkspaceTransferService) Preview(ctx context.Context, path string, importerID uuid.UUID) (*dto.WorkspaceImportPreview, error) {
	parsed, err := parseWorkspaceArchive(path)
	if err != nil {
		return nil, err
	}
	defer parsed.zip.Close()
	_, missing, err := s.userMapping(ctx, parsed, importerID)
	if err != nil && !errors.Is(err, ErrWorkspaceImportUsers) {
		return nil, err
	}
	return &dto.WorkspaceImportPreview{
		Manifest: parsed.manifest, Name: parsed.manifest.SourceWorkspaceName,
		Slug: parsed.manifest.SourceWorkspaceSlug, MissingUsers: missing,
	}, nil
}

func (s *WorkspaceTransferService) userMapping(ctx context.Context, parsed *parsedWorkspaceArchive, importerID uuid.UUID) (map[string]string, []string, error) {
	sourceOwner := transferString(parsed.data.Workspace["owner_id"])
	if _, err := uuid.Parse(sourceOwner); err != nil {
		return nil, nil, invalidArchive("source owner is invalid", err)
	}
	importerEmail, err := s.repo.UserEmail(ctx, importerID)
	if err != nil {
		return nil, nil, err
	}
	usersByID := make(map[string]string, len(parsed.data.Users))
	emails := make([]string, 0, len(parsed.data.Users))
	for _, row := range parsed.data.Users {
		id := transferString(row["id"])
		email := strings.ToLower(strings.TrimSpace(transferString(row["email"])))
		if _, err := uuid.Parse(id); err != nil || email == "" || usersByID[id] != "" {
			return nil, nil, invalidArchive("exported user metadata is invalid", err)
		}
		usersByID[id] = email
		emails = append(emails, email)
	}
	targets, err := s.repo.UsersByEmails(ctx, emails)
	if err != nil {
		return nil, nil, err
	}
	mapping := make(map[string]string, len(usersByID))
	missingSet := make(map[string]bool)
	for sourceID, email := range usersByID {
		if sourceID == sourceOwner {
			mapping[sourceID] = importerID.String()
			continue
		}
		if email == importerEmail {
			mapping[sourceID] = importerID.String()
			continue
		}
		if target, ok := targets[email]; ok {
			mapping[sourceID] = target.String()
		} else {
			missingSet[email] = true
		}
	}
	if usersByID[sourceOwner] == "" {
		return nil, nil, invalidArchive("source owner metadata is missing", nil)
	}
	missing := make([]string, 0, len(missingSet))
	for email := range missingSet {
		missing = append(missing, email)
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		return mapping, missing, &WorkspaceTransferError{Code: "WORKSPACE_IMPORT_MISSING_USERS", Message: "Create the missing user accounts before importing this workspace", MissingUsers: missing, Cause: ErrWorkspaceImportUsers}
	}
	return mapping, nil, nil
}

func (s *WorkspaceTransferService) Import(ctx context.Context, path, name, slug string, importerID uuid.UUID) (*dto.WorkspaceImportResult, error) {
	name = strings.TrimSpace(name)
	slug = strings.ToLower(strings.TrimSpace(slug))
	if name == "" || len([]rune(name)) > 100 || len(slug) > 50 || !transferSlugPattern.MatchString(slug) {
		return nil, &WorkspaceTransferError{Code: "VALIDATION_ERROR", Message: "Workspace name or slug is invalid"}
	}
	exists, err := s.repo.SlugExists(ctx, slug)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, &WorkspaceTransferError{Code: "WORKSPACE_SLUG_TAKEN", Message: "Workspace slug is already taken", Cause: ErrWorkspaceImportSlug}
	}
	parsed, err := parseWorkspaceArchive(path)
	if err != nil {
		return nil, err
	}
	defer parsed.zip.Close()
	userMap, _, err := s.userMapping(ctx, parsed, importerID)
	if err != nil {
		return nil, err
	}

	newWorkspaceID := uuid.New()
	idMap, assetIDMap, err := prepareIdentityMap(parsed, newWorkspaceID, userMap)
	if err != nil {
		return nil, err
	}
	if err := validateArchiveReferences(parsed, idMap); err != nil {
		return nil, err
	}

	stagedKeys := make([]string, 0, len(parsed.data.Tables["assets"]))
	cleanupAssets := func() {
		for _, key := range stagedKeys {
			_ = s.store.Delete(context.Background(), key)
		}
	}
	for _, sourceRow := range parsed.data.Tables["assets"] {
		row := cloneTransferRow(sourceRow)
		oldID := transferString(row["id"])
		entry := parsed.entries[transferString(row["archive_path"])]
		if entry == nil || entry.UncompressedSize64 > uint64(maxWorkspaceAssetBytes) {
			cleanupAssets()
			return nil, invalidArchive("asset payload is invalid", nil)
		}
		ext := filepath.Ext(filepath.Base(transferString(row["filename"])))
		if !transferAssetExtension.MatchString(ext) {
			ext = ""
		}
		storageKey := uuid.New().String() + strings.ToLower(ext)
		rc, err := entry.Open()
		if err != nil {
			cleanupAssets()
			return nil, err
		}
		size, putErr := s.store.Put(ctx, storageKey, io.LimitReader(rc, maxWorkspaceAssetBytes+1), transferString(row["content_type"]))
		_ = rc.Close()
		if putErr != nil || size > maxWorkspaceAssetBytes || size != transferInt64(row["size"]) {
			_ = s.store.Delete(context.Background(), storageKey)
			cleanupAssets()
			if putErr != nil {
				return nil, putErr
			}
			return nil, invalidArchive("asset size does not match metadata for "+oldID, nil)
		}
		stagedKeys = append(stagedKeys, storageKey)
		sourceRow["storage_key"] = storageKey
		delete(sourceRow, "archive_path")
		_ = assetIDMap
	}

	tx, err := s.repo.Begin(ctx)
	if err != nil {
		cleanupAssets()
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
			cleanupAssets()
		}
	}()

	workspaceRow := remapTransferValue(cloneTransferRow(parsed.data.Workspace), idMap, parsed.manifest.SourceWorkspaceSlug, slug, assetIDMap).(dto.WorkspaceTransferRow)
	workspaceRow["id"] = newWorkspaceID.String()
	workspaceRow["owner_id"] = importerID.String()
	workspaceRow["name"] = name
	workspaceRow["slug"] = slug
	workspaceColumns, err := s.repo.Columns(ctx, tx, "workspaces")
	if err != nil {
		return nil, err
	}
	if err := s.repo.InsertRow(ctx, tx, "workspaces", workspaceRow, workspaceColumns); err != nil {
		if errors.Is(err, repository.ErrWorkspaceSlugTaken) {
			return nil, &WorkspaceTransferError{Code: "WORKSPACE_SLUG_TAKEN", Message: "Workspace slug is already taken", Cause: ErrWorkspaceImportSlug}
		}
		return nil, fmt.Errorf("insert imported workspace: %w", err)
	}

	ownerInserted := false
	ownerCreatedAt := any(time.Now().UTC())
	sourceOwnerID := transferString(parsed.data.Workspace["owner_id"])
	for _, member := range parsed.data.Tables["workspace_members"] {
		if transferString(member["user_id"]) == sourceOwnerID && member["created_at"] != nil {
			ownerCreatedAt = member["created_at"]
			break
		}
	}
	deferred := make([]deferredTransferReference, 0)
	for _, spec := range repository.WorkspaceTransferTableSpecs() {
		columns, err := s.repo.Columns(ctx, tx, spec.Name)
		if err != nil {
			return nil, err
		}
		if spec.Name == "issue_lifecycle_events" {
			if err := s.repo.DeleteGeneratedLifecycle(ctx, tx, newWorkspaceID); err != nil {
				return nil, err
			}
		}
		if spec.Name == "workspace_members" && !ownerInserted {
			ownerRow := dto.WorkspaceTransferRow{"workspace_id": newWorkspaceID.String(), "user_id": importerID.String(), "role": domain.RoleOwner, "created_at": ownerCreatedAt}
			if err := s.repo.InsertRow(ctx, tx, spec.Name, ownerRow, columns); err != nil {
				return nil, err
			}
			ownerInserted = true
		}
		for _, sourceRow := range parsed.data.Tables[spec.Name] {
			row := remapTransferValue(cloneTransferRow(sourceRow), idMap, parsed.manifest.SourceWorkspaceSlug, slug, assetIDMap).(dto.WorkspaceTransferRow)
			if spec.Name == "workspace_members" && transferString(row["user_id"]) == importerID.String() {
				continue
			}
			prepareImportedRow(spec.Name, row)
			if (spec.Name == "labels" || spec.Name == "issues" || spec.Name == "comments") && row["parent_id"] != nil {
				deferred = append(deferred, deferredTransferReference{table: spec.Name, id: row["id"], ref: row["parent_id"]})
				row["parent_id"] = nil
			}
			if err := s.repo.InsertRow(ctx, tx, spec.Name, row, columns); err != nil {
				return nil, fmt.Errorf("import %s: %w", spec.Name, err)
			}
		}
	}
	for _, item := range deferred {
		if err := s.repo.UpdateReference(ctx, tx, item.table, "id", item.id, "parent_id", item.ref); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	committed = true
	audit.Log("workspace.imported", importerID, map[string]interface{}{"workspace_id": newWorkspaceID, "source_workspace_id": parsed.manifest.SourceWorkspaceID})
	return &dto.WorkspaceImportResult{
		ID: newWorkspaceID.String(), Name: name, Slug: slug, Counts: parsed.manifest.Counts,
		Warnings: parsed.manifest.Warnings, RequiresReconfiguration: parsed.manifest.RequiresReconfiguration,
	}, nil
}

type deferredTransferReference struct {
	table string
	id    any
	ref   any
}

func prepareImportedRow(table string, row dto.WorkspaceTransferRow) {
	switch table {
	case "workspace_members":
		if transferString(row["role"]) == domain.RoleOwner {
			row["role"] = domain.RoleAdmin
		}
	case "webhooks":
		row["secret"] = randomTransferToken(32)
		row["is_active"] = false
	case "shared_links":
		row["token"] = randomTransferToken(32)
		row["is_active"] = false
	case "github_installations":
		row["installation_id"] = negativeTransferID(transferString(row["id"]))
		row["access_token"] = nil
		row["token_expires_at"] = nil
	case "github_repos", "github_auto_transitions":
		row["is_active"] = false
	case "ai_settings":
		row["api_key_encrypted"] = nil
	case "dev_machine_workspace_policies":
		row["enabled"] = false
	case "dev_machine_scope_settings":
		row["environment_id"] = nil
	}
	delete(row, "archive_path")
}

func randomTransferToken(bytes int) string {
	value := make([]byte, bytes)
	if _, err := rand.Read(value); err != nil {
		return uuid.New().String() + uuid.New().String()
	}
	return hex.EncodeToString(value)
}

func negativeTransferID(value string) int64 {
	hash := sha256.Sum256([]byte(value))
	var n int64
	for i := 0; i < 7; i++ {
		n = (n << 8) | int64(hash[i])
	}
	if n == 0 {
		n = 1
	}
	return -n
}

func prepareIdentityMap(parsed *parsedWorkspaceArchive, workspaceID uuid.UUID, users map[string]string) (map[string]string, map[string]string, error) {
	idMap := map[string]string{parsed.manifest.SourceWorkspaceID: workspaceID.String()}
	for source, target := range users {
		idMap[source] = target
	}
	assetMap := make(map[string]string)
	seen := make(map[string]string)
	for _, spec := range repository.WorkspaceTransferTableSpecs() {
		if spec.IDColumn == "" {
			continue
		}
		for _, row := range parsed.data.Tables[spec.Name] {
			oldID := transferString(row[spec.IDColumn])
			if _, err := uuid.Parse(oldID); err != nil {
				return nil, nil, invalidArchive(spec.Name+" contains an invalid ID", err)
			}
			if prior := seen[oldID]; prior != "" {
				return nil, nil, invalidArchive("duplicate logical ID appears in "+prior+" and "+spec.Name, nil)
			}
			if idMap[oldID] != "" {
				return nil, nil, invalidArchive(spec.Name+" reuses a workspace or user ID", nil)
			}
			seen[oldID] = spec.Name
			newID := uuid.New().String()
			idMap[oldID] = newID
			if spec.Name == "assets" {
				assetMap[oldID] = newID
			}
		}
	}
	return idMap, assetMap, nil
}

func validateArchiveReferences(parsed *parsedWorkspaceArchive, mapping map[string]string) error {
	validateRow := func(table string, row dto.WorkspaceTransferRow) error {
		for field, value := range row {
			if field == "id" || !strings.HasSuffix(field, "_id") || value == nil {
				continue
			}
			candidate := transferString(value)
			if _, err := uuid.Parse(candidate); err != nil {
				continue // Numeric GitHub IDs and other non-UUID identifiers are valid.
			}
			if mapping[candidate] == "" {
				return invalidArchive(fmt.Sprintf("%s.%s references data outside the archive", table, field), nil)
			}
		}
		return nil
	}
	if err := validateRow("workspaces", parsed.data.Workspace); err != nil {
		return err
	}
	for table, rows := range parsed.data.Tables {
		for _, row := range rows {
			if err := validateRow(table, row); err != nil {
				return err
			}
		}
	}
	return nil
}

func cloneTransferRow(row dto.WorkspaceTransferRow) dto.WorkspaceTransferRow {
	copy := make(dto.WorkspaceTransferRow, len(row))
	for key, value := range row {
		copy[key] = value
	}
	return copy
}

func remapTransferValue(value any, mapping map[string]string, oldSlug, newSlug string, assetMap map[string]string) any {
	switch current := value.(type) {
	case dto.WorkspaceTransferRow:
		for key, nested := range current {
			current[key] = remapTransferValue(nested, mapping, oldSlug, newSlug, assetMap)
		}
		return current
	case map[string]any:
		for key, nested := range current {
			current[key] = remapTransferValue(nested, mapping, oldSlug, newSlug, assetMap)
		}
		return current
	case []any:
		for i, nested := range current {
			current[i] = remapTransferValue(nested, mapping, oldSlug, newSlug, assetMap)
		}
		return current
	case string:
		if mapped := mapping[current]; mapped != "" {
			return mapped
		}
		for oldID, newID := range assetMap {
			oldPath := "/api/workspaces/" + oldSlug + "/assets/" + oldID
			if strings.Contains(current, oldPath) {
				current = strings.ReplaceAll(current, oldPath, "/api/workspaces/"+newSlug+"/assets/"+newID)
			}
		}
		return current
	default:
		return value
	}
}

func transferString(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func transferInt64(value any) int64 {
	var result int64
	_, _ = fmt.Sscan(transferString(value), &result)
	return result
}
