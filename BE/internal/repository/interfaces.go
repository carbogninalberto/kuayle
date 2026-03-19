package repository

import (
	"context"

	"github.com/carbon/carbon-backend/internal/domain"
	"github.com/carbon/carbon-backend/internal/dto"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

type UserRepo interface {
	Create(ctx context.Context, user *domain.User) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error)
	GetByEmail(ctx context.Context, email string) (*domain.User, error)
}

type RefreshTokenRepo interface {
	Create(ctx context.Context, rt *RefreshToken) error
	GetByHash(ctx context.Context, hash string) (*RefreshToken, error)
	DeleteByHash(ctx context.Context, hash string) error
	DeleteByUser(ctx context.Context, userID uuid.UUID) error
	DeleteExpired(ctx context.Context) error
}

type WorkspaceRepo interface {
	Create(ctx context.Context, ws *domain.Workspace) error
	GetBySlug(ctx context.Context, slug string) (*domain.Workspace, error)
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Workspace, error)
	Update(ctx context.Context, ws *domain.Workspace) error
	ListByUser(ctx context.Context, userID uuid.UUID) ([]domain.Workspace, error)
	AddMember(ctx context.Context, member *domain.WorkspaceMember) error
	GetMember(ctx context.Context, workspaceID, userID uuid.UUID) (*domain.WorkspaceMember, error)
	ListMembers(ctx context.Context, workspaceID uuid.UUID) ([]domain.WorkspaceMember, error)
}

type TeamRepo interface {
	Create(ctx context.Context, team *domain.Team) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Team, error)
	ListByWorkspace(ctx context.Context, workspaceID uuid.UUID) ([]domain.Team, error)
	Update(ctx context.Context, team *domain.Team) error
	AddMember(ctx context.Context, member *domain.TeamMember) error
	GetMember(ctx context.Context, teamID, userID uuid.UUID) (*domain.TeamMember, error)
}

type IssueRepo interface {
	Create(ctx context.Context, tx *sqlx.Tx, issue *domain.Issue) error
	NextNumber(ctx context.Context, tx *sqlx.Tx, teamID uuid.UUID) (int, error)
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Issue, error)
	GetByIdentifier(ctx context.Context, workspaceID uuid.UUID, identifier string) (*domain.Issue, error)
	List(ctx context.Context, workspaceID uuid.UUID, params dto.IssueFilterParams) ([]domain.Issue, int, error)
	Update(ctx context.Context, issue *domain.Issue) error
	Delete(ctx context.Context, id uuid.UUID) error
	SetLabels(ctx context.Context, issueID uuid.UUID, labelIDs []uuid.UUID) error
	GetLabels(ctx context.Context, issueID uuid.UUID) ([]domain.Label, error)
	BeginTx(ctx context.Context) (*sqlx.Tx, error)
}

type LabelRepo interface {
	Create(ctx context.Context, label *domain.Label) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Label, error)
	ListByWorkspace(ctx context.Context, workspaceID uuid.UUID) ([]domain.Label, error)
	Update(ctx context.Context, label *domain.Label) error
	Delete(ctx context.Context, id uuid.UUID) error
}

type CommentRepo interface {
	Create(ctx context.Context, comment *domain.Comment) error
	ListByIssue(ctx context.Context, issueID uuid.UUID) ([]domain.Comment, error)
}

type ProjectRepo interface {
	Create(ctx context.Context, project *domain.Project) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Project, error)
	ListByWorkspace(ctx context.Context, workspaceID uuid.UUID) ([]domain.Project, error)
	Update(ctx context.Context, project *domain.Project) error
}

type NotificationRepo interface {
	Create(ctx context.Context, n *domain.Notification) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Notification, error)
	ListByUser(ctx context.Context, userID uuid.UUID, limit, offset int) ([]domain.Notification, error)
	Update(ctx context.Context, n *domain.Notification) error
	MarkAllRead(ctx context.Context, userID uuid.UUID) error
}

type IssueHistoryRepo interface {
	Create(ctx context.Context, issueID, userID uuid.UUID, field string, oldValue, newValue *string) error
	ListByIssue(ctx context.Context, issueID uuid.UUID) ([]domain.IssueHistory, error)
}

type ViewRepo interface {
	Create(ctx context.Context, view *domain.View) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.View, error)
	ListByWorkspace(ctx context.Context, workspaceID uuid.UUID, userID uuid.UUID) ([]domain.View, error)
	Update(ctx context.Context, view *domain.View) error
	Delete(ctx context.Context, id uuid.UUID) error
}

type CycleRepo interface {
	Create(ctx context.Context, cycle *domain.Cycle) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Cycle, error)
	ListByTeam(ctx context.Context, teamID uuid.UUID) ([]domain.Cycle, error)
	NextNumber(ctx context.Context, teamID uuid.UUID) (int, error)
	Update(ctx context.Context, cycle *domain.Cycle) error
}
