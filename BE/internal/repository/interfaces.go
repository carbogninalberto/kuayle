package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
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
	Delete(ctx context.Context, id uuid.UUID) error
	ListByUser(ctx context.Context, userID uuid.UUID) ([]domain.Workspace, error)
	AddMember(ctx context.Context, member *domain.WorkspaceMember) error
	GetMember(ctx context.Context, workspaceID, userID uuid.UUID) (*domain.WorkspaceMember, error)
	ListMembers(ctx context.Context, workspaceID uuid.UUID) ([]domain.WorkspaceMember, error)
	ListMembersWithUsers(ctx context.Context, workspaceID uuid.UUID) ([]domain.WorkspaceMemberWithUser, error)
	UpdateMemberRole(ctx context.Context, workspaceID, userID uuid.UUID, role string) error
	RemoveMember(ctx context.Context, workspaceID, userID uuid.UUID) error
	CountMembersByRole(ctx context.Context, workspaceID uuid.UUID, role string) (int, error)
}

type TeamRepo interface {
	Create(ctx context.Context, team *domain.Team) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Team, error)
	ListByWorkspace(ctx context.Context, workspaceID uuid.UUID) ([]domain.Team, error)
	Update(ctx context.Context, team *domain.Team) error
	Delete(ctx context.Context, id uuid.UUID) error
	AddMember(ctx context.Context, member *domain.TeamMember) error
	GetMember(ctx context.Context, teamID, userID uuid.UUID) (*domain.TeamMember, error)
	ListMembers(ctx context.Context, teamID uuid.UUID) ([]domain.TeamMember, error)
	RemoveMember(ctx context.Context, teamID, userID uuid.UUID) error
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
	GetLabelsForIssues(ctx context.Context, issueIDs []uuid.UUID) (map[uuid.UUID][]domain.Label, error)
	SetAssignees(ctx context.Context, issueID uuid.UUID, userIDs []uuid.UUID) error
	GetAssignees(ctx context.Context, issueID uuid.UUID) ([]uuid.UUID, error)
	GetAssigneesForIssues(ctx context.Context, issueIDs []uuid.UUID) (map[uuid.UUID][]uuid.UUID, error)
	ListSubIssues(ctx context.Context, parentID uuid.UUID) ([]domain.Issue, error)
	CountSubIssues(ctx context.Context, parentID uuid.UUID) (int, int, error)
	BulkUpdate(ctx context.Context, workspaceID uuid.UUID, issueIDs []uuid.UUID, status *string, priority *int, assigneeID *uuid.UUID, statusID *uuid.UUID) (int, error)
	BulkDelete(ctx context.Context, workspaceID uuid.UUID, issueIDs []uuid.UUID) (int, error)
	BeginTx(ctx context.Context) (*sqlx.Tx, error)
}

type AssetRepo interface {
	Create(ctx context.Context, asset *domain.Asset) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Asset, error)
}

type LabelRepo interface {
	Create(ctx context.Context, label *domain.Label) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Label, error)
	ListByWorkspace(ctx context.Context, workspaceID uuid.UUID) ([]domain.Label, error)
	Update(ctx context.Context, label *domain.Label) error
	Delete(ctx context.Context, id uuid.UUID) error
	ExistsByName(ctx context.Context, workspaceID uuid.UUID, name string) (bool, error)
}

type CommentRepo interface {
	Create(ctx context.Context, comment *domain.Comment) error
	ListByIssue(ctx context.Context, issueID uuid.UUID) ([]domain.Comment, error)
	ListReplies(ctx context.Context, parentID uuid.UUID) ([]domain.Comment, error)
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Comment, error)
	Resolve(ctx context.Context, id uuid.UUID) error
	Reopen(ctx context.Context, id uuid.UUID) error
}

type ProjectRepo interface {
	Create(ctx context.Context, project *domain.Project) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Project, error)
	ListByWorkspace(ctx context.Context, workspaceID uuid.UUID) ([]domain.Project, error)
	ListByTeam(ctx context.Context, teamID uuid.UUID) ([]domain.Project, error)
	Update(ctx context.Context, project *domain.Project) error
	Delete(ctx context.Context, id uuid.UUID) error
	IssueStats(ctx context.Context, projectID uuid.UUID) (total int, completed int, cancelled int, err error)
}

type NotificationRepo interface {
	Create(ctx context.Context, n *domain.Notification) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Notification, error)
	ListByUser(ctx context.Context, userID uuid.UUID, limit, offset int) ([]domain.Notification, error)
	ListSnoozed(ctx context.Context, userID uuid.UUID) ([]domain.Notification, error)
	ListArchived(ctx context.Context, userID uuid.UUID, limit int) ([]domain.Notification, error)
	Update(ctx context.Context, n *domain.Notification) error
	MarkAllRead(ctx context.Context, userID uuid.UUID) error
	UnreadCount(ctx context.Context, userID uuid.UUID) (int, error)
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
	Delete(ctx context.Context, id uuid.UUID) error
	IssueStats(ctx context.Context, cycleID uuid.UUID) (total int, completed int, cancelled int, err error)
	BurndownData(ctx context.Context, cycleID uuid.UUID, startDate, endDate time.Time) ([]dto.BurndownPoint, error)
	ExistsByName(ctx context.Context, teamID uuid.UUID, name string) (bool, error)
	HasOverlap(ctx context.Context, teamID uuid.UUID, startDate, endDate time.Time, excludeID *uuid.UUID) (bool, error)
	GetNextUpcoming(ctx context.Context, teamID uuid.UUID) (*domain.Cycle, error)
	CarryOverIssues(ctx context.Context, fromCycleID, toCycleID uuid.UUID) (int, error)
	VelocityData(ctx context.Context, teamID uuid.UUID, limit int) ([]dto.VelocityPoint, error)
}

type IssueRelationRepo interface {
	Create(ctx context.Context, rel *domain.IssueRelation) error
	ListByIssue(ctx context.Context, issueID uuid.UUID) ([]domain.IssueRelation, error)
	Delete(ctx context.Context, id uuid.UUID) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.IssueRelation, error)
	DeleteByIssues(ctx context.Context, issueID, relatedIssueID uuid.UUID, relType domain.IssueRelationType) error
}

type TeamStatusRepo interface {
	Create(ctx context.Context, status *domain.TeamStatus) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.TeamStatus, error)
	GetByIDs(ctx context.Context, ids []uuid.UUID) ([]domain.TeamStatus, error)
	GetByTeamAndSlug(ctx context.Context, teamID uuid.UUID, slug string) (*domain.TeamStatus, error)
	ListByTeam(ctx context.Context, teamID uuid.UUID) ([]domain.TeamStatus, error)
	Update(ctx context.Context, status *domain.TeamStatus) error
	Delete(ctx context.Context, id uuid.UUID) error
	NextPosition(ctx context.Context, teamID uuid.UUID) (int, error)
}

type FavoriteRepo interface {
	Create(ctx context.Context, fav *domain.Favorite) error
	ListByUser(ctx context.Context, workspaceID, userID uuid.UUID) ([]domain.Favorite, error)
	Delete(ctx context.Context, workspaceID, userID uuid.UUID, entityType string, entityID uuid.UUID) error
	DeleteByID(ctx context.Context, id uuid.UUID) error
}

type UserPreferencesRepo interface {
	Get(ctx context.Context, userID uuid.UUID) (*domain.UserPreferences, error)
	Upsert(ctx context.Context, prefs *domain.UserPreferences) error
}

type ProjectStatusVisibilityRepo interface {
	SetVisibleStatuses(ctx context.Context, projectID uuid.UUID, statusIDs []uuid.UUID) error
	ListVisibleStatuses(ctx context.Context, projectID uuid.UUID) ([]uuid.UUID, error)
	ListProjectsForStatus(ctx context.Context, statusID uuid.UUID) ([]uuid.UUID, error)
	ListProjectIDsByStatuses(ctx context.Context, statusIDs []uuid.UUID) (map[uuid.UUID][]uuid.UUID, error)
}

type SharedLinkRepo interface {
	Create(ctx context.Context, link *domain.SharedLink) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.SharedLink, error)
	GetByToken(ctx context.Context, token string) (*domain.SharedLink, error)
	ListByWorkspace(ctx context.Context, workspaceID uuid.UUID) ([]domain.SharedLink, error)
	Update(ctx context.Context, link *domain.SharedLink) error
	Delete(ctx context.Context, id uuid.UUID) error
}

type GitHubRepo interface {
	CreateInstallation(ctx context.Context, inst *domain.GitHubInstallation) error
	GetInstallationByWorkspace(ctx context.Context, workspaceID uuid.UUID) (*domain.GitHubInstallation, error)
	GetInstallationByGitHubID(ctx context.Context, installationID int64) (*domain.GitHubInstallation, error)
	UpdateInstallationToken(ctx context.Context, id uuid.UUID, token string, expiresAt *time.Time) error
	DeleteInstallation(ctx context.Context, workspaceID uuid.UUID) error

	CreateRepo(ctx context.Context, repo *domain.GitHubRepoModel) error
	ListReposByWorkspace(ctx context.Context, workspaceID uuid.UUID) ([]domain.GitHubRepoModel, error)
	GetRepoByGitHubID(ctx context.Context, workspaceID uuid.UUID, githubRepoID int64) (*domain.GitHubRepoModel, error)
	DeleteRepo(ctx context.Context, id uuid.UUID) error

	UpsertPullRequest(ctx context.Context, pr *domain.GitHubPullRequest) error
	ListPRsWithRepoByIssue(ctx context.Context, issueID uuid.UUID) ([]PRWithRepo, error)

	UpsertBranch(ctx context.Context, b *domain.GitHubBranch) error
	ListBranchesWithRepoByIssue(ctx context.Context, issueID uuid.UUID) ([]BranchWithRepo, error)

	UpsertCommit(ctx context.Context, c *domain.GitHubCommit) error
	ListCommitsWithRepoByIssue(ctx context.Context, issueID uuid.UUID) ([]CommitWithRepo, error)

	UpsertAutoTransition(ctx context.Context, t *domain.GitHubAutoTransition) error
	ListAutoTransitions(ctx context.Context, workspaceID uuid.UUID) ([]domain.GitHubAutoTransition, error)
	GetAutoTransitionByEvent(ctx context.Context, workspaceID uuid.UUID, event string) (*domain.GitHubAutoTransition, error)
}

type IssueTemplateRepo interface {
	Create(ctx context.Context, tmpl *domain.IssueTemplate) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.IssueTemplate, error)
	ListByWorkspace(ctx context.Context, workspaceID uuid.UUID) ([]domain.IssueTemplate, error)
	Update(ctx context.Context, tmpl *domain.IssueTemplate) error
	Delete(ctx context.Context, id uuid.UUID) error
	ListDueForRecurrence(ctx context.Context) ([]domain.IssueTemplate, error)
}
