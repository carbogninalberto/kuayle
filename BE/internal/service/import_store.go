package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/gabriel-vasile/mimetype"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/importer"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/pkg/storage"
)

// importStore is the persistence surface the import worker needs. Keeping it
// as one interface (rather than spreading repository calls through the worker)
// lets the service tests run with a fake store and no database.
type importStore interface {
	Teams(ctx context.Context, workspaceID uuid.UUID) ([]domain.Team, error)
	Statuses(ctx context.Context, workspaceID, teamID uuid.UUID) ([]domain.TeamStatus, error)
	Members(ctx context.Context, workspaceID uuid.UUID) ([]importer.Member, error)
	EnsureLabel(ctx context.Context, workspaceID uuid.UUID, projectID *uuid.UUID, name string) (uuid.UUID, error)
	EnsureCycle(ctx context.Context, workspaceID, teamID uuid.UUID, name string) (uuid.UUID, error)
	EnsureProject(ctx context.Context, workspaceID, teamID uuid.UUID, name string) (uuid.UUID, error)
	CreateIssue(ctx context.Context, workspaceID, teamID uuid.UUID, issue *domain.Issue, createdAt, updatedAt time.Time, ref *domain.ImportExternalRef) error
	GetIssue(ctx context.Context, workspaceID, issueID uuid.UUID) (*domain.Issue, error)
	UpdateIssue(ctx context.Context, workspaceID uuid.UUID, issue *domain.Issue, updatedAt time.Time) error
	SetParent(ctx context.Context, workspaceID, issueID, parentID, actorID uuid.UUID) error
	SetLabels(ctx context.Context, workspaceID, issueID uuid.UUID, labelIDs []uuid.UUID) error
	SetAssignees(ctx context.Context, workspaceID, issueID uuid.UUID, userIDs []uuid.UUID) error
	CreateComment(ctx context.Context, workspaceID uuid.UUID, comment *domain.Comment, createdAt, updatedAt time.Time) error
	CreateRelation(ctx context.Context, workspaceID, issueID, relatedID uuid.UUID, relationType domain.IssueRelationType) (uuid.UUID, error)
	StoreAttachment(ctx context.Context, workspaceID, teamID, uploaderID uuid.UUID, filename string, content io.Reader, size int64, contentType string) (*domain.Asset, string, error)
	AssetURL(ctx context.Context, workspaceID, assetID uuid.UUID) (filename, url string, err error)
}

type sqlImportStore struct {
	teams      repository.TeamRepo
	statuses   repository.TeamStatusRepo
	workspace  repository.WorkspaceRepo
	labels     repository.LabelRepo
	cycles     repository.CycleRepo
	projects   repository.ProjectRepo
	issues     repository.IssueRepo
	comments   repository.CommentRepo
	relations  repository.IssueRelationRepo
	assets     repository.AssetRepo
	imports    repository.ImportRepo
	storage    storage.Backend
	limitBytes int64
}

var _ importStore = (*sqlImportStore)(nil)

func NewSQLImportStore(
	teams repository.TeamRepo,
	statuses repository.TeamStatusRepo,
	workspace repository.WorkspaceRepo,
	labels repository.LabelRepo,
	cycles repository.CycleRepo,
	projects repository.ProjectRepo,
	issues repository.IssueRepo,
	comments repository.CommentRepo,
	relations repository.IssueRelationRepo,
	assets repository.AssetRepo,
	imports repository.ImportRepo,
	store storage.Backend,
	attachmentLimit int64,
) *sqlImportStore {
	if attachmentLimit <= 0 {
		attachmentLimit = domain.MaxUploadSize
	}
	return &sqlImportStore{
		teams: teams, statuses: statuses, workspace: workspace, labels: labels, cycles: cycles,
		projects: projects, issues: issues, comments: comments, relations: relations, assets: assets,
		imports: imports, storage: store, limitBytes: attachmentLimit,
	}
}

func (s *sqlImportStore) Teams(ctx context.Context, workspaceID uuid.UUID) ([]domain.Team, error) {
	return s.teams.ListByWorkspace(ctx, workspaceID)
}

func (s *sqlImportStore) Statuses(ctx context.Context, workspaceID, teamID uuid.UUID) ([]domain.TeamStatus, error) {
	return s.statuses.ListByTeam(ctx, teamID)
}

func (s *sqlImportStore) Members(ctx context.Context, workspaceID uuid.UUID) ([]importer.Member, error) {
	rows, err := s.workspace.ListMembersWithUsers(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	members := make([]importer.Member, 0, len(rows))
	for _, row := range rows {
		members = append(members, importer.Member{UserID: row.UserID, Email: row.Email, DisplayName: row.DisplayName})
	}
	return members, nil
}

func (s *sqlImportStore) EnsureLabel(ctx context.Context, workspaceID uuid.UUID, projectID *uuid.UUID, name string) (uuid.UUID, error) {
	exists, err := s.labels.ExistsByName(ctx, workspaceID, projectID, name, uuid.Nil)
	if err != nil {
		return uuid.Nil, err
	}
	if exists {
		labels, err := s.labels.ListByWorkspace(ctx, workspaceID, projectID)
		if err == nil {
			for _, label := range labels {
				if strings.EqualFold(label.Name, name) {
					return label.ID, nil
				}
			}
		}
	}
	label := &domain.Label{ID: uuid.New(), WorkspaceID: workspaceID, Name: name, Color: "#6b7280", ProjectID: projectID}
	if err := s.labels.Create(ctx, label); err != nil {
		if errors.Is(err, repository.ErrLabelNameTaken) {
			labels, listErr := s.labels.ListByWorkspace(ctx, workspaceID, projectID)
			if listErr == nil {
				for _, existing := range labels {
					if strings.EqualFold(existing.Name, name) {
						return existing.ID, nil
					}
				}
			}
		}
		return uuid.Nil, err
	}
	return label.ID, nil
}

func (s *sqlImportStore) EnsureCycle(ctx context.Context, workspaceID, teamID uuid.UUID, name string) (uuid.UUID, error) {
	exists, err := s.cycles.ExistsByName(ctx, teamID, name)
	if err != nil {
		return uuid.Nil, err
	}
	if exists {
		cycles, err := s.cycles.ListByTeam(ctx, teamID)
		if err == nil {
			for _, cycle := range cycles {
				if strings.EqualFold(cycle.Name, name) {
					return cycle.ID, nil
				}
			}
		}
	}
	number, err := s.cycles.NextNumber(ctx, teamID)
	if err != nil {
		return uuid.Nil, err
	}
	cycle := &domain.Cycle{ID: uuid.New(), TeamID: teamID, Name: name, Number: number, Status: domain.CycleStatusCompleted}
	if err := s.cycles.Create(ctx, cycle); err != nil {
		return uuid.Nil, err
	}
	return cycle.ID, nil
}

func (s *sqlImportStore) EnsureProject(ctx context.Context, workspaceID, teamID uuid.UUID, name string) (uuid.UUID, error) {
	projects, err := s.projects.ListByWorkspace(ctx, workspaceID)
	if err != nil {
		return uuid.Nil, err
	}
	for _, project := range projects {
		if strings.EqualFold(project.Name, name) {
			return project.ID, nil
		}
	}
	project := &domain.Project{ID: uuid.New(), WorkspaceID: workspaceID, TeamID: &teamID, Name: name, Status: domain.ProjectStatusPlanned}
	if err := s.projects.Create(ctx, project); err != nil {
		return uuid.Nil, err
	}
	return project.ID, nil
}

func (s *sqlImportStore) CreateIssue(ctx context.Context, workspaceID, teamID uuid.UUID, issue *domain.Issue, createdAt, updatedAt time.Time, ref *domain.ImportExternalRef) error {
	tx, err := s.issues.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	number, err := s.issues.NextNumber(ctx, tx, teamID)
	if err != nil {
		return err
	}
	key := ""
	if keyed, ok := s.issues.(interface {
		TeamKey(context.Context, *sqlx.Tx, uuid.UUID) (string, error)
	}); ok {
		key, err = keyed.TeamKey(ctx, tx, teamID)
		if err != nil {
			return err
		}
	}
	issue.ID = uuid.New()
	issue.WorkspaceID = workspaceID
	issue.TeamID = teamID
	issue.Number = number
	issue.Identifier = fmt.Sprintf("%s-%d", key, number)
	if err := s.issues.Create(ctx, tx, issue); err != nil {
		return err
	}
	if !createdAt.IsZero() {
		if err := s.imports.SetIssueTimestamps(ctx, tx, workspaceID, issue.ID, createdAt, updatedAt); err != nil {
			return err
		}
	}
	// Write the external ref in the same transaction as the issue so a
	// committed issue always has its idempotency mapping.
	if ref != nil {
		ref.KuayleID = issue.ID
		if err := s.imports.UpsertRef(ctx, tx, ref); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *sqlImportStore) GetIssue(ctx context.Context, workspaceID, issueID uuid.UUID) (*domain.Issue, error) {
	return s.issues.GetByID(ctx, issueID)
}

func (s *sqlImportStore) UpdateIssue(ctx context.Context, workspaceID uuid.UUID, issue *domain.Issue, updatedAt time.Time) error {
	if err := s.issues.Update(ctx, issue); err != nil {
		return err
	}
	if !updatedAt.IsZero() {
		return s.imports.SetIssueTimestamps(ctx, nil, workspaceID, issue.ID, issue.CreatedAt, updatedAt)
	}
	return nil
}

func (s *sqlImportStore) SetParent(ctx context.Context, workspaceID, issueID, parentID, actorID uuid.UUID) error {
	_, err := s.issues.BulkSetParent(ctx, workspaceID, []uuid.UUID{issueID}, &parentID, actorID)
	return err
}

func (s *sqlImportStore) SetLabels(ctx context.Context, workspaceID, issueID uuid.UUID, labelIDs []uuid.UUID) error {
	return s.issues.SetLabels(ctx, issueID, labelIDs)
}

func (s *sqlImportStore) SetAssignees(ctx context.Context, workspaceID, issueID uuid.UUID, userIDs []uuid.UUID) error {
	return s.issues.SetAssignees(ctx, issueID, userIDs)
}

func (s *sqlImportStore) CreateComment(ctx context.Context, workspaceID uuid.UUID, comment *domain.Comment, createdAt, updatedAt time.Time) error {
	comment.ID = uuid.New()
	if err := s.comments.Create(ctx, comment); err != nil {
		return err
	}
	if !createdAt.IsZero() {
		return s.imports.SetCommentTimestamps(ctx, nil, workspaceID, comment.ID, createdAt, updatedAt)
	}
	return nil
}

func (s *sqlImportStore) CreateRelation(ctx context.Context, workspaceID, issueID, relatedID uuid.UUID, relationType domain.IssueRelationType) (uuid.UUID, error) {
	relation := &domain.IssueRelation{ID: uuid.New(), IssueID: issueID, RelatedIssueID: relatedID, Type: relationType}
	if err := s.relations.Create(ctx, relation); err != nil {
		return uuid.Nil, err
	}
	return relation.ID, nil
}

// StoreAttachment detects the MIME type and validates the type/extension pair
// through the same rules as the upload handler, then stores through the asset
// pipeline with the target team so team privacy is preserved.
func (s *sqlImportStore) StoreAttachment(ctx context.Context, workspaceID, teamID, uploaderID uuid.UUID, filename string, content io.Reader, size int64, contentType string) (*domain.Asset, string, error) {
	if s.limitBytes > 0 && size > s.limitBytes {
		return nil, "", fmt.Errorf("attachment %s exceeds the %d byte limit", filename, s.limitBytes)
	}
	data, err := io.ReadAll(io.LimitReader(content, s.limitBytes+1))
	if err != nil {
		return nil, "", err
	}
	if s.limitBytes > 0 && int64(len(data)) > s.limitBytes {
		return nil, "", fmt.Errorf("attachment %s exceeds the %d byte limit", filename, s.limitBytes)
	}
	ext := strings.ToLower(filepath.Ext(filename))
	if detected := mimetype.Detect(data); detected != nil {
		resolved, ok := domain.AllowedUploadType(detected.String(), ext)
		if !ok {
			return nil, "", fmt.Errorf("attachment %s has an unsupported type", filename)
		}
		contentType = resolved
	}
	key := uuid.New().String() + ext
	written, err := s.storage.Put(ctx, key, bytes.NewReader(data), contentType)
	if err != nil {
		return nil, "", err
	}
	asset := &domain.Asset{
		ID: uuid.New(), WorkspaceID: workspaceID, TeamID: &teamID, StorageKey: key,
		Filename: filename, ContentType: contentType, Size: written, UploadedBy: uploaderID,
	}
	if err := s.assets.Create(ctx, asset); err != nil {
		_ = s.storage.Delete(ctx, key)
		return nil, "", err
	}
	url, err := s.storage.URL(ctx, key)
	if err != nil {
		url = ""
	}
	return asset, url, nil
}

// AssetURL rebuilds a stored attachment's link from its asset id. Existing
// refs carry only the Kuayle asset id, so re-runs read the filename and URL
// back from the asset and its storage key.
func (s *sqlImportStore) AssetURL(ctx context.Context, workspaceID, assetID uuid.UUID) (string, string, error) {
	asset, err := s.assets.GetByID(ctx, assetID)
	if err != nil {
		return "", "", err
	}
	if asset == nil || asset.WorkspaceID != workspaceID {
		return "", "", fmt.Errorf("attachment %s is not visible in this workspace", assetID)
	}
	url, err := s.storage.URL(ctx, asset.StorageKey)
	if err != nil {
		return asset.Filename, "", nil
	}
	return asset.Filename, url, nil
}
