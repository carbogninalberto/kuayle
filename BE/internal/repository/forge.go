package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// ErrForgeConnectionExists reports a duplicate (workspace, provider, base URL)
// connection. Callers map it to a conflict instead of echoing a driver error.
var ErrForgeConnectionExists = errors.New("a connection for this provider and base URL already exists")

func forgeConnectionWriteError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "forge_connections_workspace_id_provider_base_url_key" {
		return ErrForgeConnectionExists
	}
	return err
}

// ForgeRepository stores admin-configured forge connections and the merge
// requests, branches and commits their webhooks report. Every read and write
// is scoped by workspace where the caller has one.
type ForgeRepository struct {
	db *sqlx.DB
}

func NewForgeRepository(db *sqlx.DB) *ForgeRepository {
	return &ForgeRepository{db: db}
}

// --- Connections ---

func (r *ForgeRepository) CreateConnection(ctx context.Context, conn *domain.ForgeConnection) error {
	query := `INSERT INTO forge_connections (id, workspace_id, provider, name, base_url, webhook_secret, access_token, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING created_at, updated_at`
	err := r.db.QueryRowContext(ctx, query,
		conn.ID, conn.WorkspaceID, conn.Provider, conn.Name, conn.BaseURL,
		conn.WebhookSecret, conn.AccessToken, conn.CreatedBy,
	).Scan(&conn.CreatedAt, &conn.UpdatedAt)
	return forgeConnectionWriteError(err)
}

// GetConnection returns a connection by ID without workspace scoping. It is
// only used by the public webhook path, which proves ownership with the
// connection's secret before doing anything else.
func (r *ForgeRepository) GetConnection(ctx context.Context, id uuid.UUID) (*domain.ForgeConnection, error) {
	var conn domain.ForgeConnection
	err := r.db.GetContext(ctx, &conn, `SELECT * FROM forge_connections WHERE id = $1`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &conn, err
}

func (r *ForgeRepository) GetConnectionByWorkspace(ctx context.Context, workspaceID, id uuid.UUID) (*domain.ForgeConnection, error) {
	var conn domain.ForgeConnection
	err := r.db.GetContext(ctx, &conn, `SELECT * FROM forge_connections WHERE workspace_id = $1 AND id = $2`, workspaceID, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &conn, err
}

func (r *ForgeRepository) ListConnections(ctx context.Context, workspaceID uuid.UUID) ([]domain.ForgeConnection, error) {
	var conns []domain.ForgeConnection
	err := r.db.SelectContext(ctx, &conns, `SELECT * FROM forge_connections WHERE workspace_id = $1 ORDER BY created_at, id`, workspaceID)
	return conns, err
}

func (r *ForgeRepository) UpdateConnection(ctx context.Context, conn *domain.ForgeConnection) error {
	result, err := r.db.ExecContext(ctx, `UPDATE forge_connections SET provider=$1, name=$2, base_url=$3, webhook_secret=$4, access_token=$5, updated_at=NOW()
		WHERE workspace_id=$6 AND id=$7`, conn.Provider, conn.Name, conn.BaseURL, conn.WebhookSecret, conn.AccessToken, conn.WorkspaceID, conn.ID)
	if err != nil {
		return forgeConnectionWriteError(err)
	}
	if changed, err := result.RowsAffected(); err != nil {
		return err
	} else if changed == 0 {
		return sql.ErrNoRows
	}
	return r.db.GetContext(ctx, conn, `SELECT updated_at FROM forge_connections WHERE id=$1`, conn.ID)
}

func (r *ForgeRepository) DeleteConnection(ctx context.Context, workspaceID, id uuid.UUID) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM forge_connections WHERE workspace_id = $1 AND id = $2`, workspaceID, id)
	if err != nil {
		return err
	}
	if deleted, err := result.RowsAffected(); err != nil {
		return err
	} else if deleted == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// WorkspaceSlug resolves the slug used in the issue link posted as a comment.
func (r *ForgeRepository) WorkspaceSlug(ctx context.Context, workspaceID uuid.UUID) (string, error) {
	var slug string
	err := r.db.GetContext(ctx, &slug, `SELECT slug FROM workspaces WHERE id = $1`, workspaceID)
	return slug, err
}

// --- Repositories ---

func (r *ForgeRepository) LinkRepo(ctx context.Context, repo *domain.ForgeRepo) error {
	query := `INSERT INTO forge_repos (id, connection_id, workspace_id, external_repo_id, full_name, web_url, default_branch, is_active)
		SELECT $1, $2, $3, $4, $5, $6, $7, $8
		WHERE EXISTS (SELECT 1 FROM forge_connections c WHERE c.id = $2 AND c.workspace_id = $3)
		ON CONFLICT (connection_id, external_repo_id) DO UPDATE SET
			full_name = EXCLUDED.full_name, web_url = EXCLUDED.web_url,
			default_branch = EXCLUDED.default_branch, is_active = EXCLUDED.is_active
		RETURNING id, created_at`
	return r.db.QueryRowContext(ctx, query,
		repo.ID, repo.ConnectionID, repo.WorkspaceID, repo.ExternalRepoID,
		repo.FullName, repo.WebURL, repo.DefaultBranch, repo.IsActive,
	).Scan(&repo.ID, &repo.CreatedAt)
}

func (r *ForgeRepository) ListReposByConnection(ctx context.Context, workspaceID, connectionID uuid.UUID) ([]domain.ForgeRepo, error) {
	var repos []domain.ForgeRepo
	err := r.db.SelectContext(ctx, &repos, `SELECT * FROM forge_repos WHERE workspace_id = $1 AND connection_id = $2 ORDER BY full_name`, workspaceID, connectionID)
	return repos, err
}

func (r *ForgeRepository) ListReposByWorkspace(ctx context.Context, workspaceID uuid.UUID) ([]domain.ForgeRepo, error) {
	var repos []domain.ForgeRepo
	err := r.db.SelectContext(ctx, &repos, `SELECT * FROM forge_repos WHERE workspace_id = $1 AND is_active = true ORDER BY full_name`, workspaceID)
	return repos, err
}

func (r *ForgeRepository) GetRepoByExternalID(ctx context.Context, connectionID uuid.UUID, externalRepoID int64) (*domain.ForgeRepo, error) {
	var repo domain.ForgeRepo
	err := r.db.GetContext(ctx, &repo, `SELECT * FROM forge_repos WHERE connection_id = $1 AND external_repo_id = $2`, connectionID, externalRepoID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &repo, err
}

func (r *ForgeRepository) GetRepoByID(ctx context.Context, workspaceID, repoID uuid.UUID) (*domain.ForgeRepo, error) {
	var repo domain.ForgeRepo
	err := r.db.GetContext(ctx, &repo, `SELECT * FROM forge_repos WHERE workspace_id = $1 AND id = $2`, workspaceID, repoID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &repo, err
}

func (r *ForgeRepository) DeleteRepo(ctx context.Context, workspaceID, connectionID, repoID uuid.UUID) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM forge_repos WHERE workspace_id = $1 AND connection_id = $2 AND id = $3`, workspaceID, connectionID, repoID)
	if err != nil {
		return err
	}
	if deleted, err := result.RowsAffected(); err != nil {
		return err
	} else if deleted == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// --- Merge requests ---

func (r *ForgeRepository) UpsertMergeRequest(ctx context.Context, mr *domain.ForgeMergeRequestModel) (bool, error) {
	query := `INSERT INTO forge_merge_requests (id, workspace_id, issue_id, forge_repo_id, external_id, number, title, state, author_login, author_avatar_url, web_url, source_branch, target_branch, additions, deletions, merged_at, closed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
		ON CONFLICT (forge_repo_id, external_id) DO UPDATE SET
			issue_id = COALESCE(EXCLUDED.issue_id, forge_merge_requests.issue_id),
			title = EXCLUDED.title, state = EXCLUDED.state, additions = EXCLUDED.additions, deletions = EXCLUDED.deletions,
			merged_at = EXCLUDED.merged_at, closed_at = EXCLUDED.closed_at, updated_at = NOW()
		RETURNING created_at, updated_at, (xmax = 0)`
	var inserted bool
	err := r.db.QueryRowContext(ctx, query,
		mr.ID, mr.WorkspaceID, mr.IssueID, mr.ForgeRepoID, mr.ExternalID, mr.Number,
		mr.Title, mr.State, mr.AuthorLogin, mr.AuthorAvatarURL, mr.WebURL,
		mr.SourceBranch, mr.TargetBranch, mr.Additions, mr.Deletions, mr.MergedAt, mr.ClosedAt,
	).Scan(&mr.CreatedAt, &mr.UpdatedAt, &inserted)
	return inserted, err
}

func (r *ForgeRepository) ListMergeRequestsByIssue(ctx context.Context, issueID uuid.UUID) ([]ForgeMergeRequestWithRepo, error) {
	var mrs []ForgeMergeRequestWithRepo
	query := `SELECT m.*, r.full_name AS repo_full_name, c.provider AS provider
		FROM forge_merge_requests m
		JOIN forge_repos r ON r.id = m.forge_repo_id
		JOIN forge_connections c ON c.id = r.connection_id
		WHERE m.issue_id = $1 AND ` + issueVisible(domain.PublicContext(ctx), "m.issue_id") + ` AND ` + workspaceVisible(ctx, "m.workspace_id") + ` ORDER BY m.created_at DESC`
	err := r.db.SelectContext(ctx, &mrs, query, issueID)
	return mrs, err
}

// --- Branches ---

func (r *ForgeRepository) UpsertBranch(ctx context.Context, b *domain.ForgeBranchModel) error {
	query := `INSERT INTO forge_branches (id, workspace_id, issue_id, forge_repo_id, name, web_url)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (forge_repo_id, name) DO UPDATE SET
			issue_id = COALESCE(EXCLUDED.issue_id, forge_branches.issue_id),
			web_url = COALESCE(EXCLUDED.web_url, forge_branches.web_url)
		RETURNING created_at`
	return r.db.QueryRowContext(ctx, query,
		b.ID, b.WorkspaceID, b.IssueID, b.ForgeRepoID, b.Name, b.WebURL,
	).Scan(&b.CreatedAt)
}

func (r *ForgeRepository) ListBranchesByIssue(ctx context.Context, issueID uuid.UUID) ([]ForgeBranchWithRepo, error) {
	var branches []ForgeBranchWithRepo
	query := `SELECT b.*, r.full_name AS repo_full_name, c.provider AS provider
		FROM forge_branches b
		JOIN forge_repos r ON r.id = b.forge_repo_id
		JOIN forge_connections c ON c.id = r.connection_id
		WHERE b.issue_id = $1 AND ` + issueVisible(domain.PublicContext(ctx), "b.issue_id") + ` AND ` + workspaceVisible(ctx, "b.workspace_id") + ` ORDER BY b.created_at DESC`
	err := r.db.SelectContext(ctx, &branches, query, issueID)
	return branches, err
}

// --- Commits ---

func (r *ForgeRepository) UpsertCommit(ctx context.Context, c *domain.ForgeCommitModel) error {
	query := `INSERT INTO forge_commits (id, workspace_id, issue_id, forge_repo_id, sha, message, author_login, author_avatar_url, web_url, committed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (forge_repo_id, sha) DO UPDATE SET
			issue_id = COALESCE(EXCLUDED.issue_id, forge_commits.issue_id)
		RETURNING created_at`
	return r.db.QueryRowContext(ctx, query,
		c.ID, c.WorkspaceID, c.IssueID, c.ForgeRepoID, c.SHA,
		c.Message, c.AuthorLogin, c.AuthorAvatarURL, c.WebURL, c.CommittedAt,
	).Scan(&c.CreatedAt)
}

func (r *ForgeRepository) ListCommitsByIssue(ctx context.Context, issueID uuid.UUID) ([]ForgeCommitWithRepo, error) {
	var commits []ForgeCommitWithRepo
	query := `SELECT c.*, r.full_name AS repo_full_name, c2.provider AS provider
		FROM forge_commits c
		JOIN forge_repos r ON r.id = c.forge_repo_id
		JOIN forge_connections c2 ON c2.id = r.connection_id
		WHERE c.issue_id = $1 AND ` + issueVisible(domain.PublicContext(ctx), "c.issue_id") + ` AND ` + workspaceVisible(ctx, "c.workspace_id") + ` ORDER BY c.committed_at DESC LIMIT 50`
	err := r.db.SelectContext(ctx, &commits, query, issueID)
	return commits, err
}

type ForgeMergeRequestWithRepo struct {
	domain.ForgeMergeRequestModel
	RepoFullName string `db:"repo_full_name"`
	Provider     string `db:"provider"`
}

type ForgeBranchWithRepo struct {
	domain.ForgeBranchModel
	RepoFullName string `db:"repo_full_name"`
	Provider     string `db:"provider"`
}

type ForgeCommitWithRepo struct {
	domain.ForgeCommitModel
	RepoFullName string `db:"repo_full_name"`
	Provider     string `db:"provider"`
}

// --- Webhook deliveries ---

// ClaimWebhookDelivery records a forge webhook delivery and reports whether
// the caller may process it. A processed delivery is refused; an unfinished
// claim older than lease (the server stopped midway) may be taken over.
func (r *ForgeRepository) ClaimWebhookDelivery(ctx context.Context, connectionID uuid.UUID, deliveryID string, lease time.Duration) (bool, error) {
	var claimed bool
	err := r.db.QueryRowContext(ctx, `INSERT INTO forge_webhook_deliveries(connection_id, delivery_id) VALUES($1,$2)
 ON CONFLICT (connection_id, delivery_id) DO UPDATE SET claimed_at=now()
 WHERE forge_webhook_deliveries.processed_at IS NULL AND forge_webhook_deliveries.claimed_at < now() - make_interval(secs => $3)
 RETURNING true`, connectionID, deliveryID, lease.Seconds()).Scan(&claimed)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return claimed, err
}

func (r *ForgeRepository) CompleteWebhookDelivery(ctx context.Context, connectionID uuid.UUID, deliveryID string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE forge_webhook_deliveries SET processed_at=now() WHERE connection_id=$1 AND delivery_id=$2`, connectionID, deliveryID)
	return err
}

func (r *ForgeRepository) ReleaseWebhookDelivery(ctx context.Context, connectionID uuid.UUID, deliveryID string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM forge_webhook_deliveries WHERE connection_id=$1 AND delivery_id=$2 AND processed_at IS NULL`, connectionID, deliveryID)
	return err
}

func (r *ForgeRepository) DeleteExpiredWebhookDeliveries(ctx context.Context, retention time.Duration) (int64, error) {
	result, err := r.db.ExecContext(ctx, `DELETE FROM forge_webhook_deliveries WHERE claimed_at < now() - make_interval(secs => $1)`, retention.Seconds())
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
