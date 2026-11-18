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
	WorkspaceSlug(ctx context.Context, workspaceID uuid.UUID) (string, error)
	MemberRole(ctx context.Context, workspaceID, userID uuid.UUID) (string, error)
	EnsureLabel(ctx context.Context, workspaceID uuid.UUID, projectID *uuid.UUID, name string) (uuid.UUID, error)
	EnsureCycle(ctx context.Context, workspaceID, teamID uuid.UUID, sprint importer.SourceSprint) (uuid.UUID, error)
	EnsureProject(ctx context.Context, workspaceID, teamID uuid.UUID, name string) (uuid.UUID, error)
	CreateIssue(ctx context.Context, workspaceID, teamID uuid.UUID, issue *domain.Issue, createdAt, updatedAt time.Time, ref *domain.ImportExternalRef) error
	GetIssue(ctx context.Context, workspaceID, issueID uuid.UUID) (*domain.Issue, error)
	UpdateIssue(ctx context.Context, workspaceID uuid.UUID, issue *domain.Issue, updatedAt time.Time) error
	SetParent(ctx context.Context, workspaceID, issueID, parentID, actorID uuid.UUID) error
	SetLabels(ctx context.Context, workspaceID, issueID uuid.UUID, labelIDs []uuid.UUID) error
	SetAssignees(ctx context.Context, workspaceID, issueID uuid.UUID, userIDs []uuid.UUID) error
	CreateComment(ctx context.Context, workspaceID uuid.UUID, comment *domain.Comment, createdAt, updatedAt time.Time) error
	UpdateComment(ctx context.Context, workspaceID, issueID, commentID, authorID uuid.UUID, body string) (*domain.Comment, error)
	CreateRelation(ctx context.Context, workspaceID, issueID, relatedID uuid.UUID, relationType domain.IssueRelationType) (uuid.UUID, error)
	StoreAttachment(ctx context.Context, workspaceID, teamID, uploaderID uuid.UUID, filename string, content io.Reader, size int64, contentType string) (*domain.Asset, error)
	Asset(ctx context.Context, workspaceID, assetID uuid.UUID) (*domain.Asset, error)
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

// WorkspaceSlug names the workspace in imported attachment links, which use
// the same access-checked asset route as in-app uploads.
func (s *sqlImportStore) WorkspaceSlug(ctx context.Context, workspaceID uuid.UUID) (string, error) {
	workspace, err := s.workspace.GetByID(ctx, workspaceID)
	if err != nil {
		return "", err
	}
	if workspace == nil {
		return "", fmt.Errorf("workspace %s not found", workspaceID)
	}
	return workspace.Slug, nil
}

// MemberRole is the user's current role in the workspace, or "" when they are
// no longer a member.
func (s *sqlImportStore) MemberRole(ctx context.Context, workspaceID, userID uuid.UUID) (string, error) {
	member, err := s.workspace.GetMember(ctx, workspaceID, userID)
	if err != nil {
		return "", err
	}
	if member == nil {
		return "", nil
	}
	return member.Role, nil
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

// cycleStatus maps a sprint state to a cycle status. A sprint without a known
// state becomes a completed cycle so it never shows as current work.
func cycleStatus(state string) domain.CycleStatus {
	switch state {
	case importer.SprintStateActive:
		return domain.CycleStatusActive
	case importer.SprintStateFuture:
		return domain.CycleStatusUpcoming
	default:
		return domain.CycleStatusCompleted
	}
}

// EnsureCycle reuses the team's cycle with the sprint's name, or creates one
// with the sprint's state and dates.
func (s *sqlImportStore) EnsureCycle(ctx context.Context, workspaceID, teamID uuid.UUID, sprint importer.SourceSprint) (uuid.UUID, error) {
	name := sprint.Name
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
	cycle := &domain.Cycle{
		ID: uuid.New(), TeamID: teamID, Name: name, Number: number, Status: cycleStatus(sprint.State),
		StartDate: sprint.StartDate, EndDate: sprint.EndDate,
	}
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
	issue.SortOrder = -float64(number) * 1000
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

// GetIssue returns nil for an issue that was deleted, is no longer visible to
// the importer or belongs to another workspace.
func (s *sqlImportStore) GetIssue(ctx context.Context, workspaceID, issueID uuid.UUID) (*domain.Issue, error) {
	issue, err := s.issues.GetByID(ctx, issueID)
	if err != nil || issue == nil || issue.WorkspaceID != workspaceID {
		return nil, err
	}
	return issue, nil
}

// UpdateIssue writes a re-imported issue. IssueRepo.Update leaves title and
// description alone (the editor saves those separately), so the import
// writes them explicitly.
func (s *sqlImportStore) UpdateIssue(ctx context.Context, workspaceID uuid.UUID, issue *domain.Issue, updatedAt time.Time) error {
	if err := s.issues.UpdateContent(ctx, issue, true, true); err != nil {
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

// UpdateComment syncs an edited source comment onto the imported Kuayle
// comment. The repository applies access visibility from ctx; a nil result
// means the comment is no longer editable (deleted or edited in Kuayle).
func (s *sqlImportStore) UpdateComment(ctx context.Context, _ uuid.UUID, issueID, commentID, authorID uuid.UUID, body string) (*domain.Comment, error) {
	return s.comments.Update(ctx, issueID, commentID, authorID, body)
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
func (s *sqlImportStore) StoreAttachment(ctx context.Context, workspaceID, teamID, uploaderID uuid.UUID, filename string, content io.Reader, size int64, contentType string) (*domain.Asset, error) {
	filename = filepath.Base(filename)
	if s.limitBytes > 0 && size > s.limitBytes {
		return nil, fmt.Errorf("attachment %s exceeds the %d byte limit", filename, s.limitBytes)
	}
	data, err := io.ReadAll(io.LimitReader(content, s.limitBytes+1))
	if err != nil {
		return nil, err
	}
	if s.limitBytes > 0 && int64(len(data)) > s.limitBytes {
		return nil, fmt.Errorf("attachment %s exceeds the %d byte limit", filename, s.limitBytes)
	}
	ext := strings.ToLower(filepath.Ext(filename))
	detected := mimetype.Detect(data)
	resolved, ok := domain.AllowedUploadType(detected.String(), ext)
	if !ok {
		return nil, fmt.Errorf("attachment %s has an unsupported type", filename)
	}
	contentType = resolved
	if ext == "" {
		ext = detected.Extension()
	}
	key := uuid.New().String() + ext
	written, err := s.storage.Put(ctx, key, bytes.NewReader(data), contentType)
	if err != nil {
		return nil, err
	}
	asset := &domain.Asset{
		ID: uuid.New(), WorkspaceID: workspaceID, TeamID: &teamID, StorageKey: key,
		Filename: filename, ContentType: contentType, Size: written, UploadedBy: uploaderID,
	}
	if err := s.assets.Create(ctx, asset); err != nil {
		_ = s.storage.Delete(ctx, key)
		return nil, err
	}
	return asset, nil
}

// Asset reads back an attachment imported by an earlier run, so a re-run can
// relink it. The repository applies team visibility from ctx.
func (s *sqlImportStore) Asset(ctx context.Context, workspaceID, assetID uuid.UUID) (*domain.Asset, error) {
	asset, err := s.assets.GetByID(ctx, assetID)
	if err != nil {
		return nil, err
	}
	if asset == nil || asset.WorkspaceID != workspaceID {
		return nil, fmt.Errorf("attachment %s is no longer visible in this workspace", assetID)
	}
	return asset, nil
}
