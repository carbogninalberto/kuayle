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
	Update(ctx context.Context, user *domain.User) error
}

type AISettingsRepo interface {
	BeginPublicOperation(ctx context.Context, workspaceID uuid.UUID) (*sqlx.Tx, error)
	GetByWorkspaceID(ctx context.Context, workspaceID uuid.UUID) (*domain.AISettings, error)
	Upsert(ctx context.Context, settings *domain.AISettings) error
}

type RefreshTokenRepo interface {
	Create(ctx context.Context, rt *RefreshToken) error
	GetByHash(ctx context.Context, hash string) (*RefreshToken, error)
	DeleteByHash(ctx context.Context, hash string) error
	DeleteByUser(ctx context.Context, userID uuid.UUID) error
	DeleteExpired(ctx context.Context) error
}

// SSORepo stores workspace OIDC configuration, flow state and external
// identities. ProvisionIdentity runs the whole just-in-time sign-in in one
// transaction.
type SSORepo interface {
	GetConfig(ctx context.Context, workspaceID uuid.UUID) (*domain.SSOConfig, error)
	SaveConfig(ctx context.Context, config *domain.SSOConfig) error
	DeleteConfig(ctx context.Context, workspaceID uuid.UUID) error

	CreateLoginState(ctx context.Context, state *domain.SSOLoginState) error
	ConsumeLoginState(ctx context.Context, stateHash string) (*domain.SSOLoginState, error)
	DeleteExpiredLoginStates(ctx context.Context) error

	GetIdentity(ctx context.Context, workspaceID uuid.UUID, issuer, subject string) (*domain.UserIdentity, error)
	TouchIdentity(ctx context.Context, id uuid.UUID) error
	ProvisionIdentity(ctx context.Context, params SSOProvisionParams) (*domain.User, error)
	LinkIdentity(ctx context.Context, userID, workspaceID uuid.UUID, provider, issuer, subject, email string) (*domain.UserIdentity, error)
	HasIdentity(ctx context.Context, userID, workspaceID uuid.UUID) (bool, error)
	UserWorkspaceRoles(ctx context.Context, userID uuid.UUID) ([]domain.SSOWorkspaceRole, error)
}

// SSOProvisionParams is the input of a just-in-time SSO sign-in. PasswordHash
// is a random unusable hash for a newly created account.
type SSOProvisionParams struct {
	WorkspaceID  uuid.UUID
	Issuer       string
	Subject      string
	Provider     string
	Email        string
	Name         string
	DefaultRole  string
	PasswordHash string
}

type PersonalAccessTokenRepo interface {
	Create(ctx context.Context, token *domain.PersonalAccessToken) error
	GetByHash(ctx context.Context, hash string) (*domain.PersonalAccessToken, error)
	ListByUser(ctx context.Context, userID uuid.UUID) ([]domain.PersonalAccessToken, error)
	Revoke(ctx context.Context, id, userID uuid.UUID) error
	UpdateLastUsed(ctx context.Context, id uuid.UUID) error
}

type WorkspaceRepo interface {
	Create(ctx context.Context, ws *domain.Workspace) error
	CreateWithMemberAndLabels(ctx context.Context, ws *domain.Workspace, member *domain.WorkspaceMember, labels []domain.Label) error
	GetBySlug(ctx context.Context, slug string) (*domain.Workspace, error)
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Workspace, error)
	Update(ctx context.Context, ws *domain.Workspace) error
	// Delete removes the workspace and returns the image icons its teams had at
	// deletion, so the caller can remove their files.
	Delete(ctx context.Context, id uuid.UUID) ([]domain.TeamIconImage, error)
	ListByUser(ctx context.Context, userID uuid.UUID) ([]domain.Workspace, error)
	AddMember(ctx context.Context, member *domain.WorkspaceMember) error
	GetMember(ctx context.Context, workspaceID, userID uuid.UUID) (*domain.WorkspaceMember, error)
	ListMembers(ctx context.Context, workspaceID uuid.UUID) ([]domain.WorkspaceMember, error)
	ListMembersWithUsers(ctx context.Context, workspaceID uuid.UUID) ([]domain.WorkspaceMemberWithUser, error)
	UpdateMemberRole(ctx context.Context, workspaceID, userID uuid.UUID, role string) error
	RemoveMember(ctx context.Context, workspaceID, userID uuid.UUID) error
	CountMembersByRole(ctx context.Context, workspaceID uuid.UUID, role string) (int, error)
}

type WorkspaceInviteLinkRepo interface {
	Create(ctx context.Context, link *domain.WorkspaceInviteLink) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.WorkspaceInviteLink, error)
	GetByTokenHash(ctx context.Context, hash string) (*domain.WorkspaceInviteLink, error)
	ListByWorkspace(ctx context.Context, workspaceID uuid.UUID) ([]domain.WorkspaceInviteLink, error)
	Revoke(ctx context.Context, id uuid.UUID) error
	Join(ctx context.Context, id, userID uuid.UUID, newUser *domain.User) (string, error)
}

type TeamRepo interface {
	Create(ctx context.Context, team *domain.Team) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Team, error)
	ListByWorkspace(ctx context.Context, workspaceID uuid.UUID) ([]domain.Team, error)
	Update(ctx context.Context, team *domain.Team) error
	// SetIcon writes only the icon and returns the team with ReplacedIcon set.
	// It returns nil when the team does not exist or is not visible.
	SetIcon(ctx context.Context, id uuid.UUID, icon string) (*domain.Team, error)
	// Delete removes the team and returns the icon it had at deletion.
	Delete(ctx context.Context, id uuid.UUID) (*string, error)
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
	UpdateContent(ctx context.Context, issue *domain.Issue, titleChanged, descriptionChanged bool) error
	// UpdateUntriaged is Update for a triage decision: it writes only while the
	// stored issue is still untriaged and returns ErrIssueAlreadyTriaged when a
	// concurrent request triaged it first.
	UpdateUntriaged(ctx context.Context, issue *domain.Issue) error
	Delete(ctx context.Context, id uuid.UUID) error
	SetLabels(ctx context.Context, issueID uuid.UUID, labelIDs []uuid.UUID) error
	ValidateLabelScope(ctx context.Context, projectID *uuid.UUID, labelIDs []uuid.UUID) error
	GetLabels(ctx context.Context, issueID uuid.UUID) ([]domain.Label, error)
	GetLabelsForIssues(ctx context.Context, issueIDs []uuid.UUID) (map[uuid.UUID][]domain.Label, error)
	SetAssignees(ctx context.Context, issueID uuid.UUID, userIDs []uuid.UUID) error
	GetAssignees(ctx context.Context, issueID uuid.UUID) ([]uuid.UUID, error)
	GetAssigneesForIssues(ctx context.Context, issueIDs []uuid.UUID) (map[uuid.UUID][]uuid.UUID, error)
	ListSubIssues(ctx context.Context, parentID uuid.UUID) ([]domain.Issue, error)
	CountSubIssues(ctx context.Context, parentID uuid.UUID) (int, int, error)
	CountSubIssuesForIssues(ctx context.Context, issueIDs []uuid.UUID) (map[uuid.UUID]domain.SubIssueCount, error)
	WouldCreateCycle(ctx context.Context, issueID, parentID uuid.UUID) (bool, error)
	CycleIsActive(ctx context.Context, cycleID uuid.UUID) (bool, error)
	BulkUpdate(ctx context.Context, workspaceID uuid.UUID, issueIDs []uuid.UUID, status *string, priority *int, assigneeID *uuid.UUID, statusID *uuid.UUID, cycleID *uuid.UUID, cycleSet bool) (int, error)
	// BulkUpdateChanged also returns the issues whose written fields changed.
	BulkUpdateChanged(ctx context.Context, workspaceID uuid.UUID, issueIDs []uuid.UUID, status *string, priority *int, assigneeID *uuid.UUID, statusID *uuid.UUID, cycleID *uuid.UUID, cycleSet bool) (int, []domain.Issue, error)
	BulkDelete(ctx context.Context, workspaceID uuid.UUID, issueIDs []uuid.UUID) (int, error)
	// BulkDeleteReturning returns the deleted issues, as they were when deleted.
	BulkDeleteReturning(ctx context.Context, workspaceID uuid.UUID, issueIDs []uuid.UUID) ([]domain.Issue, error)
	BulkSetProject(ctx context.Context, workspaceID uuid.UUID, issueIDs []uuid.UUID, projectID *uuid.UUID, actorID uuid.UUID) ([]domain.Issue, error)
	BulkSetParent(ctx context.Context, workspaceID uuid.UUID, issueIDs []uuid.UUID, parentID *uuid.UUID, actorID uuid.UUID) ([]domain.Issue, error)
	BeginTx(ctx context.Context) (*sqlx.Tx, error)
	ListByIDs(ctx context.Context, workspaceID uuid.UUID, ids []uuid.UUID) ([]domain.Issue, error)
	ApplyUndo(ctx context.Context, workspaceID uuid.UUID, items []domain.IssueUndo) ([]domain.Issue, error)
	GetDeletedByIdentifier(ctx context.Context, workspaceID uuid.UUID, identifier string) (*domain.DeletedIssue, error)
	ListDeletedByIDs(ctx context.Context, workspaceID uuid.UUID, ids []uuid.UUID) ([]domain.DeletedIssue, error)
	Restore(ctx context.Context, workspaceID uuid.UUID, ids []uuid.UUID, deletedSince time.Time) ([]domain.Issue, error)
}

type AssetRepo interface {
	Create(ctx context.Context, asset *domain.Asset) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Asset, error)
}

type LabelRepo interface {
	Create(ctx context.Context, label *domain.Label) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Label, error)
	ListByWorkspace(ctx context.Context, workspaceID uuid.UUID, projectID *uuid.UUID) ([]domain.Label, error)
	Update(ctx context.Context, label *domain.Label) error
	Delete(ctx context.Context, id uuid.UUID) error
	ExistsByName(ctx context.Context, workspaceID uuid.UUID, projectID *uuid.UUID, name string, excludeID uuid.UUID) (bool, error)
}

type CommentRepo interface {
	Create(ctx context.Context, comment *domain.Comment) error
	ListByIssue(ctx context.Context, issueID uuid.UUID) ([]domain.Comment, error)
	ListReplies(ctx context.Context, parentID uuid.UUID) ([]domain.Comment, error)
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Comment, error)
	// Resolve and Reopen return the changed comment, or nil when its
	// resolution was already as asked or it is not visible.
	Resolve(ctx context.Context, id uuid.UUID) (*domain.Comment, error)
	Reopen(ctx context.Context, id uuid.UUID) (*domain.Comment, error)
	// Update changes the body of authorID's comment on issueID; nil when
	// nothing changed or no such live comment is visible.
	Update(ctx context.Context, issueID, id, authorID uuid.UUID, body string) (*domain.Comment, error)
	// SoftDelete deletes a live comment on issueID and returns it as it was
	// before deletion, or nil when no such live comment is visible.
	SoftDelete(ctx context.Context, issueID, id, actorID uuid.UUID) (*domain.Comment, error)
	// Restore brings back a comment on issueID deleted at or after
	// deletedSince (ErrRestoreExpired otherwise).
	Restore(ctx context.Context, issueID, id uuid.UUID, deletedSince time.Time) (*domain.Comment, error)
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
	CreateOrRefresh(ctx context.Context, n *domain.Notification, window time.Duration) error
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
	// CarryOverIssues moves the cycle's unfinished issues to toCycleID and
	// returns them as they were after the move.
	CarryOverIssues(ctx context.Context, fromCycleID, toCycleID uuid.UUID) ([]domain.Issue, error)
	VelocityData(ctx context.Context, teamID uuid.UUID, limit int) ([]dto.VelocityPoint, error)
}

type IssueRelationRepo interface {
	Create(ctx context.Context, rel *domain.IssueRelation) error
	ListByIssue(ctx context.Context, issueID uuid.UUID) ([]domain.IssueRelation, error)
	ListByIssues(ctx context.Context, issueIDs []uuid.UUID) ([]domain.IssueRelation, error)
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
	ActivateImportedInstallation(ctx context.Context, id uuid.UUID, installationID int64, accountLogin, accountType string, installedBy uuid.UUID) error
	DeleteInstallation(ctx context.Context, workspaceID uuid.UUID) error

	CreateRepo(ctx context.Context, repo *domain.GitHubRepoModel) error
	ListReposByWorkspace(ctx context.Context, workspaceID uuid.UUID) ([]domain.GitHubRepoModel, error)
	GetRepoByGitHubID(ctx context.Context, workspaceID uuid.UUID, githubRepoID int64) (*domain.GitHubRepoModel, error)
	DeleteRepo(ctx context.Context, workspaceID, id uuid.UUID) error

	UpsertPullRequest(ctx context.Context, pr *domain.GitHubPullRequest) (bool, error)
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

type PasswordResetRepo interface {
	Issue(ctx context.Context, t *PasswordResetToken, cooldown time.Duration) (bool, error)
	Reset(ctx context.Context, tokenHash, passwordHash string) error
}

// WikiRepo stores team and project wiki pages. Every method applies the
// caller's current visibility of the scope from the request context.
type WikiRepo interface {
	RequireScope(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope) error
	List(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope) ([]domain.WikiPage, error)
	Get(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, pageID uuid.UUID) (*domain.WikiPage, error)
	Create(ctx context.Context, page *domain.WikiPage) error
	Update(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, page *domain.WikiPage, expectedVersion int) error
	Delete(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, pageID uuid.UUID) error
	ListRevisions(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, pageID uuid.UUID) ([]domain.WikiRevision, error)
	RegistryEnabled(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope) (bool, error)
	SetRegistryEnabled(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, userID uuid.UUID, enabled bool) error
	AppendRegistry(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, userID uuid.UUID, day time.Time, entry string) (*domain.WikiPage, bool, error)
	AppendIssueRegistry(ctx context.Context, workspaceID, issueID, userID uuid.UUID, now time.Time, text string) (*domain.WikiPage, bool, error)
	RegistryDays(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, before *time.Time, limit int) ([]domain.WikiPage, error)
	Settings(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope) (domain.WikiSettings, error)
	SetOverviewSchedule(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, userID uuid.UUID, schedule domain.WikiOverviewSchedule, nextRun *time.Time) error
	RegistryRange(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, start, end time.Time) ([]domain.WikiPage, error)
	CreateOverview(ctx context.Context, overview *domain.WikiOverview) error
	ListOverviews(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, before *time.Time, limit int) ([]domain.WikiOverview, error)
	DueOverviewSchedules(ctx context.Context, now time.Time, limit int) ([]domain.DueWikiOverview, error)
	ClaimOverviewRun(ctx context.Context, settingsID uuid.UUID, slot, next time.Time) (bool, error)
	RecordOverviewRun(ctx context.Context, settingsID uuid.UUID, at time.Time, runErr *string) error
	SearchReferences(ctx context.Context, workspaceID uuid.UUID, query string, limit int) ([]domain.WikiReference, error)
	ResolveReferences(ctx context.Context, workspaceID uuid.UUID, pageIDs []uuid.UUID) ([]domain.WikiReference, error)
	Backlinks(ctx context.Context, workspaceID uuid.UUID, scope domain.WikiScope, pageID uuid.UUID, withIssues bool) (*domain.WikiBacklinks, error)
	IssueReferences(ctx context.Context, workspaceID, issueID uuid.UUID) ([]uuid.UUID, error)
	AttachIssueReference(ctx context.Context, workspaceID, issueID, pageID, userID uuid.UUID) error
	DetachIssueReference(ctx context.Context, workspaceID, issueID, pageID uuid.UUID) error
}

var _ WikiRepo = (*WikiRepository)(nil)

// PasskeyRepo stores WebAuthn credentials and the pending ceremonies' state.
type PasskeyRepo interface {
	Create(ctx context.Context, p *domain.PasskeyCredential) error
	ListByUser(ctx context.Context, userID uuid.UUID) ([]domain.PasskeyCredential, error)
	GetByCredentialID(ctx context.Context, credentialID []byte) (*domain.PasskeyCredential, error)
	Exists(ctx context.Context, id, userID uuid.UUID) (bool, error)
	Rename(ctx context.Context, id, userID uuid.UUID, name string) (*domain.PasskeyCredential, error)
	Delete(ctx context.Context, id, userID uuid.UUID, keepOne bool) (bool, error)
	RecordUse(ctx context.Context, id uuid.UUID, previousCount, signCount uint32, backupState bool) (bool, error)
	SaveChallenge(ctx context.Context, ch *domain.PasskeyChallenge, replacesHash string) error
	ConsumeChallenge(ctx context.Context, handleHash string) (*domain.PasskeyChallenge, error)
}

// CLILinkRepo stores revocable connection links for kuayle_cli.
type CLILinkRepo interface {
	Create(ctx context.Context, link *domain.CLIConnectionLink) error
	GetByHash(ctx context.Context, hash string) (*domain.CLIConnectionLink, error)
	ListActiveByUser(ctx context.Context, userID uuid.UUID) ([]domain.CLIConnectionLink, error)
	Revoke(ctx context.Context, id, userID uuid.UUID) error
	Redeem(ctx context.Context, codeHash string, build func(link *domain.CLIConnectionLink) (*domain.PersonalAccessToken, error)) (*domain.PersonalAccessToken, error)
}

// BackupRepo stores the instance backup settings, lease and run history.
type BackupRepo interface {
	GetSettings(ctx context.Context) (*domain.BackupSettings, error)
	UpdateSettings(ctx context.Context, settings *domain.BackupSettings) error
	ClaimSchedule(ctx context.Context, expected *time.Time, next time.Time) (bool, error)
	AcquireLease(ctx context.Context, token uuid.UUID, ttl time.Duration) (bool, error)
	RenewLease(ctx context.Context, token uuid.UUID, ttl time.Duration) (bool, error)
	ReleaseLease(ctx context.Context, token uuid.UUID) error
	CreateRun(ctx context.Context, run *domain.BackupRun) error
	UpdateRun(ctx context.Context, run *domain.BackupRun) error
	ListRuns(ctx context.Context, limit, offset int) ([]domain.BackupRun, error)
	ListRetainable(ctx context.Context) ([]domain.BackupRun, error)
	GetRun(ctx context.Context, id uuid.UUID) (*domain.BackupRun, error)
	MarkDeleted(ctx context.Context, id uuid.UUID, at time.Time) error
	MarkStaleRunning(ctx context.Context, before time.Time) (int64, error)
	NotificationTarget(ctx context.Context, userID uuid.UUID) (workspaceID, teamID uuid.UUID, ok bool, err error)
}

var _ BackupRepo = (*BackupRepository)(nil)
