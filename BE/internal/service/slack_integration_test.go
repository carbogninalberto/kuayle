package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/config"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/pkg/crypto"
	"github.com/stretchr/testify/require"
)

func TestSlackOAuthAndUninstallPostgres(t *testing.T) {
	f := newForgeFixture(t)
	_, err := f.db.Exec("INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'admin')", f.ws, f.owner)
	require.NoError(t, err)
	var removed, unavailable atomic.Bool
	var exchanges atomic.Int32
	teamID := uuid.NewString()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/oauth.v2.access":
			exchanges.Add(1)
			fmt.Fprintf(w, `{"ok":true,"team":{"id":%q,"name":"Test"},"authed_user":{"id":"U1"},"token_type":"bot","access_token":"fixture-bot","scope":"channels:read,groups:read"}`, teamID)
		case "/api/users.conversations":
			if r.Header.Get("Authorization") != "Bearer fixture-bot" {
				t.Error("incorrect bot authorization")
			}
			fmt.Fprint(w, `{"ok":true,"channels":[{"id":"C1","name":"design"}]}`)
		case "/api/apps.uninstall":
			if unavailable.Load() {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			if removed.Load() {
				_, _ = fmt.Fprint(w, `{"ok":false,"error":"invalid_auth"}`)
				return
			}
			fmt.Fprint(w, `{"ok":true}`)
		case "/api/auth.test":
			switch {
			case unavailable.Load():
				w.WriteHeader(http.StatusServiceUnavailable)
			case removed.Load():
				fmt.Fprint(w, `{"ok":false,"error":"token_revoked"}`)
			default:
				fmt.Fprint(w, `{"ok":true}`)
			}
		default:
			t.Errorf("unexpected Slack method %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	target, err := url.Parse(server.URL)
	require.NoError(t, err)
	original := http.DefaultTransport
	http.DefaultTransport = webhookTestTransport{target: target, base: original}
	defer func() { http.DefaultTransport = original }()
	ctx := context.Background()
	repo := repository.NewSlackRepository(f.db)
	app := config.SlackConfig{ClientID: "fixture", ClientSecret: "fixture", SigningSecret: "fixture"}
	svc := NewSlackService(repo, app, crypto.DeriveKey("slack test"), "https://kuayle.test")
	installURL, err := svc.Start(ctx, f.ws, f.owner)
	require.NoError(t, err)
	u, err := url.Parse(installURL)
	require.NoError(t, err)
	state := uuid.MustParse(u.Query().Get("state"))
	require.ErrorIs(t, svc.Connect(ctx, f.ws, uuid.New(), state, "code"), ErrSlackState)
	require.Zero(t, exchanges.Load())
	require.NoError(t, svc.Connect(ctx, f.ws, f.owner, state, "code"))
	require.ErrorIs(t, svc.Connect(ctx, f.ws, f.owner, state, "code"), ErrSlackState)
	require.EqualValues(t, 1, exchanges.Load())
	install, err := repo.Get(ctx, f.ws)
	require.NoError(t, err)
	require.NotEqual(t, "fixture-bot", install.Token)
	status, err := svc.Status(ctx, f.ws)
	require.NoError(t, err)
	body, err := json.Marshal(status)
	require.NoError(t, err)
	require.NotContains(t, string(body), install.Token)
	require.NotContains(t, string(body), "fixture-bot")
	require.ErrorIs(t, svc.SetBinding(ctx, f.ws, f.owner, domain.SlackBinding{TeamID: &f.team, ChannelID: "unknown"}), ErrSlackInput)
	require.NoError(t, svc.SetBinding(ctx, f.ws, f.owner, domain.SlackBinding{TeamID: &f.team, ChannelID: "C1"}))
	deliver := func(id string) {
		payload := []byte(fmt.Sprintf(`{"type":"event_callback","event_id":%q,"team_id":%q,"event":{"type":"app_uninstalled"}}`, id, teamID))
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		sig := "v0=" + crypto.HMACSha256(append([]byte("v0:"+ts+":"), payload...), app.SigningSecret)
		_, err := svc.Event(ctx, ts, sig, payload)
		require.NoError(t, err)
	}
	staleID := uuid.NewString()
	deliver(staleID)
	deliver(staleID)
	require.NoError(t, svc.processUninstall(ctx))
	deliver(staleID)
	require.ErrorIs(t, svc.processUninstall(ctx), sql.ErrNoRows)
	_, err = repo.Get(ctx, f.ws)
	require.NoError(t, err, "a stale uninstall must preserve the live bot")
	eventID := uuid.NewString()
	deliver(eventID)
	unavailable.Store(true)
	require.Error(t, svc.processUninstall(ctx))
	_, err = repo.Get(ctx, f.ws)
	require.NoError(t, err)
	_, err = f.db.Exec("UPDATE slack_uninstall_events SET next_attempt=now() WHERE event_id=$1", eventID)
	require.NoError(t, err)
	unavailable.Store(false)
	removed.Store(true)
	require.NoError(t, svc.processUninstall(ctx))
	_, err = repo.Get(ctx, f.ws)
	require.ErrorIs(t, err, sql.ErrNoRows)
	bindings, err := repo.Bindings(ctx, f.ws)
	require.NoError(t, err)
	require.Empty(t, bindings)

	for _, alreadyRemoved := range []bool{false, true} {
		removed.Store(alreadyRemoved)
		installURL, err = svc.Start(ctx, f.ws, f.owner)
		require.NoError(t, err)
		u, err = url.Parse(installURL)
		require.NoError(t, err)
		require.NoError(t, svc.Connect(ctx, f.ws, f.owner, uuid.MustParse(u.Query().Get("state")), "new-code"))
		unavailable.Store(true)
		require.Error(t, svc.Disconnect(ctx, f.ws))
		_, err = repo.Get(ctx, f.ws)
		require.NoError(t, err, "a failed uninstall must remain retryable")
		unavailable.Store(false)
		require.NoError(t, svc.Disconnect(ctx, f.ws))
		_, err = repo.Get(ctx, f.ws)
		require.ErrorIs(t, err, sql.ErrNoRows)
	}
}

func TestSlackUnusableInstallation(t *testing.T) {
	f := newForgeFixture(t)
	ctx := t.Context()
	_, err := f.db.Exec("INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'admin')", f.ws, f.owner)
	require.NoError(t, err)
	repo := repository.NewSlackRepository(f.db)
	app := config.SlackConfig{ClientID: "fixture", ClientSecret: "fixture", SigningSecret: "fixture"}
	key := crypto.DeriveKey("slack test")
	validToken, err := crypto.Encrypt("fixture-bot", key)
	require.NoError(t, err)
	for _, tc := range []struct {
		name       string
		app        config.SlackConfig
		ciphertext string
	}{
		{"missing settings", config.SlackConfig{}, validToken},
		{"unreadable token", app, "corrupted"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			install := domain.SlackInstall{ID: uuid.New(), WorkspaceID: f.ws, TeamID: uuid.NewString(), Token: tc.ciphertext}
			require.NoError(t, repo.Install(ctx, install, f.owner, "U1"))
			t.Cleanup(func() { require.NoError(t, repo.Remove(ctx, install.ID)) })
			svc := NewSlackService(repo, tc.app, key, "")
			if tc.ciphertext != validToken {
				eventID := uuid.NewString()
				require.NoError(t, repo.EnqueueUninstall(ctx, install.TeamID, eventID))
				require.NoError(t, svc.processUninstall(ctx))
				var finished bool
				require.NoError(t, f.db.Get(&finished, "SELECT next_attempt IS NULL FROM slack_uninstall_events WHERE event_id=$1", eventID))
				require.True(t, finished)
				_, err := repo.Get(ctx, f.ws)
				require.NoError(t, err, "unverified uninstall must preserve the installation")
			}
			require.NoError(t, svc.Disconnect(ctx, f.ws))
			require.NoError(t, svc.Disconnect(ctx, f.ws))
			_, err := NewSlackService(repo, app, key, "").Start(ctx, f.ws, f.owner)
			require.NoError(t, err, "admin can reinstall once configuration is available")
		})
	}
}
