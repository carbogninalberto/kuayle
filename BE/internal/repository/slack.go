package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

var ErrSlackAdminRequired = errors.New("Slack settings require a workspace owner or admin")
var ErrSlackConnected = errors.New("workspace already connected to Slack")

type SlackRepository struct{ db *sqlx.DB }

func NewSlackRepository(db *sqlx.DB) *SlackRepository { return &SlackRepository{db} }

func (r *SlackRepository) BeginInstall(ctx context.Context, ws, user, state uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO slack_oauth_states(workspace_id,user_id,state,expires_at)
 VALUES($1,$2,$3,now()+interval '10 minutes') ON CONFLICT(workspace_id,user_id)
 DO UPDATE SET state=excluded.state, expires_at=excluded.expires_at`, ws, user, state)
	return err
}

func (r *SlackRepository) ConsumeState(ctx context.Context, ws, user, state uuid.UUID) error {
	return r.db.QueryRowContext(ctx, `DELETE FROM slack_oauth_states WHERE workspace_id=$1 AND user_id=$2 AND state=$3 AND expires_at>now() RETURNING state`, ws, user, state).Scan(&state)
}

const slackInstallColumns = "id, workspace_id, team_id, name, bot_token, scope"

func (r *SlackRepository) Get(ctx context.Context, ws uuid.UUID) (domain.SlackInstall, error) {
	var i domain.SlackInstall
	err := r.db.GetContext(ctx, &i, "SELECT "+slackInstallColumns+" FROM slack_installs WHERE workspace_id=$1", ws)
	return i, err
}

// The Slack round trip may outlast a membership change. Check the role again
// under a row lock before committing the installation or channel binding.
func lockSlackAdmin(ctx context.Context, tx *sql.Tx, ws, user uuid.UUID) error {
	var id uuid.UUID
	err := tx.QueryRowContext(ctx, `SELECT user_id FROM workspace_members
 WHERE workspace_id=$1 AND user_id=$2 AND role IN ('owner','admin') FOR SHARE`, ws, user).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrSlackAdminRequired
	}
	return err
}

func (r *SlackRepository) Install(ctx context.Context, i domain.SlackInstall, user uuid.UUID, slackUser string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := lockSlackAdmin(ctx, tx, i.WorkspaceID, user); err != nil {
		return err
	}
	err = tx.QueryRowContext(ctx, `INSERT INTO slack_installs(id,workspace_id,team_id,name,bot_token,scope,installer_id)
 VALUES($1,$2,$3,$4,$5,$6,$7)
 ON CONFLICT DO NOTHING RETURNING id`, i.ID, i.WorkspaceID, i.TeamID, i.Name, i.Token, i.Scope, user).Scan(&i.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrSlackConnected
	}
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO slack_identities(workspace_id,slack_user_id,user_id) VALUES($1,$2,$3)`, i.WorkspaceID, slackUser, user)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (r *SlackRepository) Remove(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, "DELETE FROM slack_installs WHERE id=$1", id)
	return err
}

func (r *SlackRepository) Bindings(ctx context.Context, ws uuid.UUID) ([]domain.SlackBinding, error) {
	items := []domain.SlackBinding{}
	err := r.db.SelectContext(ctx, &items, "SELECT team_id,project_id,channel_id FROM slack_bindings WHERE workspace_id=$1 ORDER BY team_id,project_id", ws)
	return items, err
}

// Fence against disconnect/reinstall while the service was checking the channel in Slack.
func (r *SlackRepository) SetBinding(ctx context.Context, i domain.SlackInstall, user uuid.UUID, b domain.SlackBinding) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := lockSlackAdmin(ctx, tx, i.WorkspaceID, user); err != nil {
		return err
	}
	var ws uuid.UUID
	err = tx.QueryRowContext(ctx, "SELECT workspace_id FROM slack_installs WHERE id=$1 AND workspace_id=$2 FOR SHARE", i.ID, i.WorkspaceID).Scan(&ws)
	if err != nil {
		return err
	}
	if b.ChannelID == "" {
		_, err = tx.ExecContext(ctx, `DELETE FROM slack_bindings WHERE workspace_id=$1 AND team_id IS NOT DISTINCT FROM $2 AND project_id IS NOT DISTINCT FROM $3`, ws, b.TeamID, b.ProjectID)
	} else {
		_, err = tx.ExecContext(ctx, `INSERT INTO slack_bindings(workspace_id,team_id,project_id,channel_id) VALUES($1,$2,$3,$4)
 ON CONFLICT(workspace_id,team_id,project_id) DO UPDATE SET channel_id=excluded.channel_id`, ws, b.TeamID, b.ProjectID, b.ChannelID)
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		return sql.ErrNoRows
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (r *SlackRepository) EnqueueUninstall(ctx context.Context, team, event string) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO slack_uninstall_events(event_id,install_id)
 SELECT $2,id FROM slack_installs WHERE team_id=$1 ON CONFLICT DO NOTHING`, team, event)
	return err
}

type SlackUninstall struct {
	EventID string
	Install domain.SlackInstall
}

// ClaimUninstall leases one event. A crashed worker can resume after one minute.
func (r *SlackRepository) ClaimUninstall(ctx context.Context) (SlackUninstall, error) {
	var e SlackUninstall
	err := r.db.QueryRowContext(ctx, `WITH claimed AS (
 UPDATE slack_uninstall_events SET next_attempt=now()+interval '1 minute'
 WHERE event_id=(SELECT event_id FROM slack_uninstall_events WHERE next_attempt<=now()
 ORDER BY next_attempt LIMIT 1 FOR UPDATE SKIP LOCKED) RETURNING install_id,event_id)
 SELECT c.event_id,i.id,i.bot_token FROM claimed c JOIN slack_installs i ON i.id=c.install_id`).Scan(&e.EventID, &e.Install.ID, &e.Install.Token)
	return e, err
}

func (r *SlackRepository) FinishUninstall(ctx context.Context, e SlackUninstall, revoked bool) error {
	if revoked {
		return r.Remove(ctx, e.Install.ID)
	}
	// Preserve the receipt for a stale uninstall delivered after a new installation.
	_, err := r.db.ExecContext(ctx, "UPDATE slack_uninstall_events SET next_attempt=NULL WHERE install_id=$1 AND event_id=$2", e.Install.ID, e.EventID)
	return err
}
