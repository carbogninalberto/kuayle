package repository

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestSlackInstallationIsolation(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := Open(dsn)
	require.NoError(t, err)
	defer db.Close()
	ctx := context.Background()
	repo := NewSlackRepository(db)
	user, ws, other := uuid.New(), uuid.New(), uuid.New()
	_, err = db.Exec("INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Slack test','hash')", user, user.String()+"@test.invalid")
	require.NoError(t, err)
	defer db.Exec("DELETE FROM users WHERE id=$1", user)
	for _, id := range []uuid.UUID{ws, other} {
		_, err = db.Exec("INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Slack test',$2,$3)", id, id.String(), user)
		require.NoError(t, err)
		defer db.Exec("DELETE FROM workspaces WHERE id=$1", id)
		_, err = db.Exec("INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'admin')", id, user)
		require.NoError(t, err)
	}
	nonce := uuid.New()
	require.NoError(t, repo.BeginInstall(ctx, ws, user, nonce))
	require.ErrorIs(t, repo.ConsumeState(ctx, other, user, nonce), sql.ErrNoRows)
	require.ErrorIs(t, repo.ConsumeState(ctx, ws, uuid.New(), nonce), sql.ErrNoRows)
	require.NoError(t, repo.ConsumeState(ctx, ws, user, nonce))
	require.ErrorIs(t, repo.ConsumeState(ctx, ws, user, nonce), sql.ErrNoRows)
	require.NoError(t, repo.BeginInstall(ctx, ws, user, nonce))
	_, err = db.Exec("UPDATE slack_oauth_states SET expires_at=now()-interval '1 second' WHERE workspace_id=$1 AND user_id=$2", ws, user)
	require.NoError(t, err)
	require.ErrorIs(t, repo.ConsumeState(ctx, ws, user, nonce), sql.ErrNoRows)
	install := domain.SlackInstall{ID: uuid.New(), WorkspaceID: ws, TeamID: uuid.NewString(), Name: "Test", Token: "ciphertext", Scope: "channels:read"}
	save := func(i domain.SlackInstall, slackUser string) error {
		return repo.Install(ctx, user, func() (domain.SlackInstall, string, error) { return i, slackUser, nil })
	}
	require.NoError(t, save(install, "U1"))
	duplicate := install
	duplicate.ID = uuid.New()
	require.ErrorIs(t, save(duplicate, "U1"), ErrSlackConnected)
	duplicate.WorkspaceID = other
	require.ErrorIs(t, save(duplicate, "U2"), ErrSlackTeamInUse)
	_, err = repo.Get(ctx, other)
	require.ErrorIs(t, err, sql.ErrNoRows)
	saved, err := repo.Get(ctx, ws)
	require.NoError(t, err)
	require.Equal(t, install, saved)
	var mapped uuid.UUID
	require.NoError(t, db.Get(&mapped, "SELECT user_id FROM slack_identities WHERE workspace_id=$1 AND slack_user_id='U1'", ws))
	require.Equal(t, user, mapped)
	team := uuid.New()
	_, err = db.Exec("INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Test','TEST')", team, other)
	require.NoError(t, err)
	require.Error(t, repo.SetBinding(ctx, install, user, domain.SlackBinding{TeamID: &team, ChannelID: "C1"}))

	local := uuid.New()
	_, err = db.Exec("INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Test','TEST')", local, ws)
	require.NoError(t, err)
	binding := domain.SlackBinding{TeamID: &local, ChannelID: "C1"}
	require.ErrorIs(t, repo.SetBinding(ctx, install, uuid.New(), binding), ErrSlackAdminRequired)
	require.NoError(t, repo.SetBinding(ctx, install, user, binding))
	binding.ChannelID = "C2"
	require.NoError(t, repo.SetBinding(ctx, install, user, binding))
	bindings, err := repo.Bindings(ctx, ws)
	require.NoError(t, err)
	require.Equal(t, []domain.SlackBinding{binding}, bindings)
	binding.ChannelID = ""
	require.NoError(t, repo.SetBinding(ctx, install, user, binding))
	bindings, err = repo.Bindings(ctx, ws)
	require.NoError(t, err)
	require.Empty(t, bindings)

	t.Run("cleanup waits for an in-flight claim from another workspace", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		started, finish := make(chan struct{}), make(chan struct{})
		pending := domain.SlackInstall{ID: uuid.New(), WorkspaceID: other, TeamID: uuid.NewString(), Token: "ciphertext"}
		installed := make(chan error, 1)
		go func() {
			installed <- repo.Install(ctx, user, func() (domain.SlackInstall, string, error) {
				close(started)
				select {
				case <-finish:
					return pending, "U2", nil
				case <-ctx.Done():
					return domain.SlackInstall{}, "", ctx.Err()
				}
			})
		}()
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		cleaned := make(chan error, 1)
		go func() {
			cleaned <- repo.UninstallIfUnclaimed(ctx, pending.TeamID, func() error {
				return errors.New("attempted to uninstall an in-flight grant")
			})
		}()
		require.Eventually(t, func() bool {
			var waiting bool
			err := db.GetContext(ctx, &waiting, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity
 WHERE wait_event='advisory' AND query LIKE '%pg_advisory_xact_lock(%slack:oauth%')`)
			return err == nil && waiting
		}, 3*time.Second, 10*time.Millisecond)
		close(finish)
		require.NoError(t, <-installed)
		require.NoError(t, <-cleaned)
	})
}
