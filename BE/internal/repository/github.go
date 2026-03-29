package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

type GitHubRepository struct {
	db *sqlx.DB
}

func NewGitHubRepository(db *sqlx.DB) *GitHubRepository {
	return &GitHubRepository{db: db}
}

// --- Installations ---

func (r *GitHubRepository) CreateInstallation(ctx context.Context, inst *domain.GitHubInstallation) error {
	query := `INSERT INTO github_installations (id, workspace_id, installation_id, account_login, account_type, access_token, token_expires_at, installed_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING created_at, updated_at`
	return r.db.QueryRowContext(ctx, query,
		inst.ID, inst.WorkspaceID, inst.InstallationID, inst.AccountLogin, inst.AccountType,
		inst.AccessToken, inst.TokenExpiresAt, inst.InstalledBy,
	).Scan(&inst.CreatedAt, &inst.UpdatedAt)
}

func (r *GitHubRepository) GetInstallationByWorkspace(ctx context.Context, workspaceID uuid.UUID) (*domain.GitHubInstallation, error) {
	var inst domain.GitHubInstallation
	err := r.db.GetContext(ctx, &inst, `SELECT * FROM github_installations WHERE workspace_id = $1`, workspaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &inst, err
}

func (r *GitHubRepository) GetInstallationByGitHubID(ctx context.Context, installationID int64) (*domain.GitHubInstallation, error) {
	var inst domain.GitHubInstallation
	err := r.db.GetContext(ctx, &inst, `SELECT * FROM github_installations WHERE installation_id = $1`, installationID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &inst, err
}

func (r *GitHubRepository) UpdateInstallationToken(ctx context.Context, id uuid.UUID, token string, expiresAt *time.Time) error {
	_, err := r.db.ExecContext(ctx, `UPDATE github_installations SET access_token = $1, token_expires_at = $2, updated_at = NOW() WHERE id = $3`, token, expiresAt, id)
	return err
}

func (r *GitHubRepository) DeleteInstallation(ctx context.Context, workspaceID uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM github_installations WHERE workspace_id = $1`, workspaceID)
	return err
}

// --- Repos ---

func (r *GitHubRepository) CreateRepo(ctx context.Context, repo *domain.GitHubRepoModel) error {
	query := `INSERT INTO github_repos (id, installation_id, workspace_id, github_repo_id, full_name, default_branch, is_active)
		VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING created_at`
	return r.db.QueryRowContext(ctx, query,
		repo.ID, repo.InstallationID, repo.WorkspaceID, repo.GitHubRepoID, repo.FullName, repo.DefaultBranch, repo.IsActive,
	).Scan(&repo.CreatedAt)
}

func (r *GitHubRepository) ListReposByWorkspace(ctx context.Context, workspaceID uuid.UUID) ([]domain.GitHubRepoModel, error) {
	var repos []domain.GitHubRepoModel
	err := r.db.SelectContext(ctx, &repos, `SELECT * FROM github_repos WHERE workspace_id = $1 AND is_active = true ORDER BY full_name`, workspaceID)
	return repos, err
}

func (r *GitHubRepository) GetRepoByGitHubID(ctx context.Context, workspaceID uuid.UUID, githubRepoID int64) (*domain.GitHubRepoModel, error) {
	var repo domain.GitHubRepoModel
	err := r.db.GetContext(ctx, &repo, `SELECT * FROM github_repos WHERE workspace_id = $1 AND github_repo_id = $2`, workspaceID, githubRepoID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &repo, err
}

func (r *GitHubRepository) DeactivateRepo(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `UPDATE github_repos SET is_active = false WHERE id = $1`, id)
	return err
}

func (r *GitHubRepository) DeleteRepo(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM github_repos WHERE id = $1`, id)
	return err
}

// --- Pull Requests ---

func (r *GitHubRepository) UpsertPullRequest(ctx context.Context, pr *domain.GitHubPullRequest) error {
	query := `INSERT INTO github_pull_requests (id, workspace_id, issue_id, github_repo_id, github_pr_id, number, title, state, author_login, author_avatar_url, html_url, head_branch, base_branch, additions, deletions, merged_at, closed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
		ON CONFLICT (github_repo_id, github_pr_id) DO UPDATE SET
			issue_id = COALESCE(EXCLUDED.issue_id, github_pull_requests.issue_id),
			title = EXCLUDED.title, state = EXCLUDED.state, additions = EXCLUDED.additions, deletions = EXCLUDED.deletions,
			merged_at = EXCLUDED.merged_at, closed_at = EXCLUDED.closed_at, updated_at = NOW()
		RETURNING created_at, updated_at`
	return r.db.QueryRowContext(ctx, query,
		pr.ID, pr.WorkspaceID, pr.IssueID, pr.GitHubRepoID, pr.GitHubPRID, pr.Number,
		pr.Title, pr.State, pr.AuthorLogin, pr.AuthorAvatarURL, pr.HTMLURL,
		pr.HeadBranch, pr.BaseBranch, pr.Additions, pr.Deletions, pr.MergedAt, pr.ClosedAt,
	).Scan(&pr.CreatedAt, &pr.UpdatedAt)
}

func (r *GitHubRepository) ListPRsByIssue(ctx context.Context, issueID uuid.UUID) ([]domain.GitHubPullRequest, error) {
	var prs []domain.GitHubPullRequest
	err := r.db.SelectContext(ctx, &prs, `SELECT * FROM github_pull_requests WHERE issue_id = $1 ORDER BY created_at DESC`, issueID)
	return prs, err
}

func (r *GitHubRepository) GetPRByRepoAndNumber(ctx context.Context, repoID uuid.UUID, prNumber int) (*domain.GitHubPullRequest, error) {
	var pr domain.GitHubPullRequest
	err := r.db.GetContext(ctx, &pr, `SELECT * FROM github_pull_requests WHERE github_repo_id = $1 AND number = $2`, repoID, prNumber)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &pr, err
}

// --- Branches ---

func (r *GitHubRepository) UpsertBranch(ctx context.Context, b *domain.GitHubBranch) error {
	query := `INSERT INTO github_branches (id, workspace_id, issue_id, github_repo_id, name, html_url)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (github_repo_id, name) DO UPDATE SET
			issue_id = COALESCE(EXCLUDED.issue_id, github_branches.issue_id)
		RETURNING created_at`
	return r.db.QueryRowContext(ctx, query,
		b.ID, b.WorkspaceID, b.IssueID, b.GitHubRepoID, b.Name, b.HTMLURL,
	).Scan(&b.CreatedAt)
}

func (r *GitHubRepository) ListBranchesByIssue(ctx context.Context, issueID uuid.UUID) ([]domain.GitHubBranch, error) {
	var branches []domain.GitHubBranch
	err := r.db.SelectContext(ctx, &branches, `SELECT * FROM github_branches WHERE issue_id = $1 ORDER BY created_at DESC`, issueID)
	return branches, err
}

// --- Commits ---

func (r *GitHubRepository) UpsertCommit(ctx context.Context, c *domain.GitHubCommit) error {
	query := `INSERT INTO github_commits (id, workspace_id, issue_id, github_repo_id, pr_id, sha, message, author_login, author_avatar_url, html_url, committed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (github_repo_id, sha) DO UPDATE SET
			issue_id = COALESCE(EXCLUDED.issue_id, github_commits.issue_id),
			pr_id = COALESCE(EXCLUDED.pr_id, github_commits.pr_id)
		RETURNING created_at`
	return r.db.QueryRowContext(ctx, query,
		c.ID, c.WorkspaceID, c.IssueID, c.GitHubRepoID, c.PRID, c.SHA,
		c.Message, c.AuthorLogin, c.AuthorAvatarURL, c.HTMLURL, c.CommittedAt,
	).Scan(&c.CreatedAt)
}

func (r *GitHubRepository) ListCommitsByIssue(ctx context.Context, issueID uuid.UUID) ([]domain.GitHubCommit, error) {
	var commits []domain.GitHubCommit
	err := r.db.SelectContext(ctx, &commits, `SELECT * FROM github_commits WHERE issue_id = $1 ORDER BY committed_at DESC`, issueID)
	return commits, err
}

// --- Auto Transitions ---

func (r *GitHubRepository) UpsertAutoTransition(ctx context.Context, t *domain.GitHubAutoTransition) error {
	query := `INSERT INTO github_auto_transitions (id, workspace_id, event, target_status, target_status_id, is_active)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (workspace_id, event) DO UPDATE SET
			target_status = EXCLUDED.target_status, target_status_id = EXCLUDED.target_status_id, is_active = EXCLUDED.is_active`
	_, err := r.db.ExecContext(ctx, query, t.ID, t.WorkspaceID, t.Event, t.TargetStatus, t.TargetStatusID, t.IsActive)
	return err
}

func (r *GitHubRepository) ListAutoTransitions(ctx context.Context, workspaceID uuid.UUID) ([]domain.GitHubAutoTransition, error) {
	var transitions []domain.GitHubAutoTransition
	err := r.db.SelectContext(ctx, &transitions, `SELECT * FROM github_auto_transitions WHERE workspace_id = $1`, workspaceID)
	return transitions, err
}

func (r *GitHubRepository) GetAutoTransitionByEvent(ctx context.Context, workspaceID uuid.UUID, event string) (*domain.GitHubAutoTransition, error) {
	var t domain.GitHubAutoTransition
	err := r.db.GetContext(ctx, &t, `SELECT * FROM github_auto_transitions WHERE workspace_id = $1 AND event = $2 AND is_active = true`, workspaceID, event)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &t, err
}

// --- Queries joining repos for display ---

type PRWithRepo struct {
	domain.GitHubPullRequest
	RepoFullName string `db:"repo_full_name"`
}

type BranchWithRepo struct {
	domain.GitHubBranch
	RepoFullName string `db:"repo_full_name"`
}

type CommitWithRepo struct {
	domain.GitHubCommit
	RepoFullName string `db:"repo_full_name"`
}

func (r *GitHubRepository) ListPRsWithRepoByIssue(ctx context.Context, issueID uuid.UUID) ([]PRWithRepo, error) {
	var prs []PRWithRepo
	query := `SELECT p.*, r.full_name AS repo_full_name FROM github_pull_requests p JOIN github_repos r ON r.id = p.github_repo_id WHERE p.issue_id = $1 ORDER BY p.created_at DESC`
	err := r.db.SelectContext(ctx, &prs, query, issueID)
	return prs, err
}

func (r *GitHubRepository) ListBranchesWithRepoByIssue(ctx context.Context, issueID uuid.UUID) ([]BranchWithRepo, error) {
	var branches []BranchWithRepo
	query := `SELECT b.*, r.full_name AS repo_full_name FROM github_branches b JOIN github_repos r ON r.id = b.github_repo_id WHERE b.issue_id = $1 ORDER BY b.created_at DESC`
	err := r.db.SelectContext(ctx, &branches, query, issueID)
	return branches, err
}

func (r *GitHubRepository) ListCommitsWithRepoByIssue(ctx context.Context, issueID uuid.UUID) ([]CommitWithRepo, error) {
	var commits []CommitWithRepo
	query := `SELECT c.*, r.full_name AS repo_full_name FROM github_commits c JOIN github_repos r ON r.id = c.github_repo_id WHERE c.issue_id = $1 ORDER BY c.committed_at DESC LIMIT 50`
	err := r.db.SelectContext(ctx, &commits, query, issueID)
	return commits, err
}

// --- App Config ---

func (r *GitHubRepository) CreateAppConfig(ctx context.Context, cfg *domain.GitHubAppConfig) error {
	query := `INSERT INTO github_app_configs (id, workspace_id, app_id, app_slug, client_id, client_secret, private_key, webhook_secret, html_url)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING created_at`
	return r.db.QueryRowContext(ctx, query,
		cfg.ID, cfg.WorkspaceID, cfg.AppID, cfg.AppSlug, cfg.ClientID,
		cfg.ClientSecret, cfg.PrivateKey, cfg.WebhookSecret, cfg.HTMLURL,
	).Scan(&cfg.CreatedAt)
}

func (r *GitHubRepository) GetAppConfigByWorkspace(ctx context.Context, workspaceID uuid.UUID) (*domain.GitHubAppConfig, error) {
	var cfg domain.GitHubAppConfig
	err := r.db.GetContext(ctx, &cfg, `SELECT * FROM github_app_configs WHERE workspace_id = $1`, workspaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &cfg, err
}

func (r *GitHubRepository) DeleteAppConfig(ctx context.Context, workspaceID uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM github_app_configs WHERE workspace_id = $1`, workspaceID)
	return err
}
