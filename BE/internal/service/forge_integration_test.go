package service

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/pkg/crypto"
	"github.com/stretchr/testify/require"
)

type forgeFixture struct {
	db     *sqlx.DB
	svc    *ForgeService
	owner  uuid.UUID
	ws     uuid.UUID
	team   uuid.UUID
	issue  uuid.UUID
	status map[string]uuid.UUID
}

func newForgeFixture(t *testing.T) *forgeFixture {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := repository.Open(dsn)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	owner, ws, team, issue := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Forge integration','hash')`, owner, owner.String()+"@test.invalid")
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id=$1`, owner) })
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Forge integration',$2,$3)`, ws, ws.String(), owner)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM workspaces WHERE id=$1`, ws) })
	_, err = db.Exec(`INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Engineering','ENG')`, team, ws)
	require.NoError(t, err)
	status := map[string]uuid.UUID{}
	for i, s := range []struct{ slug, category string }{
		{"backlog", "backlog"}, {"todo", "unstarted"}, {"in_progress", "started"},
		{"in_review", "started"}, {"done", "completed"}, {"cancelled", "cancelled"},
	} {
		_, err = db.Exec(`INSERT INTO team_statuses(team_id,name,slug,category,position,is_default) VALUES($1,$2,$3,$4,$5,$6)`,
			team, s.slug, s.slug, s.category, i, i == 0)
		require.NoError(t, err)
		var id uuid.UUID
		require.NoError(t, db.Get(&id, `SELECT id FROM team_statuses WHERE team_id=$1 AND slug=$2`, team, s.slug))
		status[s.slug] = id
	}
	_, err = db.Exec(`INSERT INTO issues(id,workspace_id,team_id,number,identifier_text,title,creator_id,status,status_id)
		VALUES($1,$2,$3,1,'KUA-1','Forge issue',$4,'todo',$5)`, issue, ws, team, owner, status["todo"])
	require.NoError(t, err)
	key := crypto.DeriveKey("forge-integration-test")
	svc := NewForgeService(repository.NewForgeRepository(db), repository.NewGitHubRepository(db),
		repository.NewIssueRepository(db), repository.NewTeamRepository(db),
		repository.NewTeamStatusRepository(db), repository.NewIssueHistoryRepository(db),
		key, nil, "https://kuayle.test", false)
	return &forgeFixture{db: db, svc: svc, owner: owner, ws: ws, team: team, issue: issue, status: status}
}

func (f *forgeFixture) createConnection(t *testing.T, provider domain.ForgeProvider, baseURL, secret string) *dto.ForgeConnectionResponse {
	t.Helper()
	conn, err := f.svc.CreateConnection(context.Background(), f.ws, f.owner, dto.CreateForgeConnectionRequest{
		Provider: string(provider), Name: string(provider), BaseURL: baseURL, WebhookSecret: secret,
	})
	require.NoError(t, err)
	return conn
}

func (f *forgeFixture) linkRepo(t *testing.T, connectionID string, externalID int64) {
	t.Helper()
	id, err := uuid.Parse(connectionID)
	require.NoError(t, err)
	_, err = f.svc.LinkRepo(context.Background(), f.ws, id, dto.LinkForgeRepoRequest{
		ExternalRepoID: externalID, FullName: "team/app", WebURL: "https://forge.example.com/team/app", DefaultBranch: "main",
	})
	require.NoError(t, err)
}

func (f *forgeFixture) issueStatus(t *testing.T) string {
	t.Helper()
	var status string
	require.NoError(t, f.db.Get(&status, `SELECT status FROM issues WHERE id=$1`, f.issue))
	return status
}

func gitlabMRBody(action, state, title string) []byte {
	body, _ := json.Marshal(map[string]any{
		"object_kind": "merge_request",
		"user":        map[string]any{"username": "alice"},
		"project":     map[string]any{"id": 42, "path_with_namespace": "team/app", "web_url": "https://gitlab.example.com/team/app"},
		"object_attributes": map[string]any{
			"id": 900, "iid": 7, "title": title, "description": "Closes KUA-1",
			"state": state, "action": action, "source_branch": "feat/kua-1-x",
			"target_branch": "main", "url": "https://gitlab.example.com/team/app/-/merge_requests/7",
		},
	})
	return body
}

func TestForgeGitLabMergeRequestTransitionsAndActivityPostgres(t *testing.T) {
	f := newForgeFixture(t)
	ctx := context.Background()
	conn := f.createConnection(t, domain.ForgeGitLab, "https://gitlab.example.com", "secret")
	f.linkRepo(t, conn.ID, 42)

	header := make(http.Header)
	header.Set("X-Gitlab-Event", "Merge Request Hook")
	header.Set("X-Gitlab-Token", "secret")
	header.Set("X-Gitlab-Event-UUID", uuid.NewString())
	require.NoError(t, f.svc.HandleWebhook(ctx, uuid.MustParse(conn.ID), header, gitlabMRBody("open", "opened", "KUA-1 fix")))
	require.Equal(t, "in_review", f.issueStatus(t))

	header.Set("X-Gitlab-Event-UUID", uuid.NewString())
	require.NoError(t, f.svc.HandleWebhook(ctx, uuid.MustParse(conn.ID), header, gitlabMRBody("merge", "merged", "KUA-1 fix")))
	require.Equal(t, "done", f.issueStatus(t))

	activity, err := f.svc.GetIssueActivity(ctx, f.ws, "KUA-1")
	require.NoError(t, err)
	require.Len(t, activity.MergeRequests, 1)
	require.Equal(t, "gitlab", activity.MergeRequests[0].Provider)
	require.Equal(t, 7, activity.MergeRequests[0].Number)
	require.Equal(t, "merged", activity.MergeRequests[0].State)
}

func giteaMRBody(action string, merged bool, title string) []byte {
	body, _ := json.Marshal(map[string]any{
		"action": action, "number": 7,
		"pull_request": map[string]any{
			"id": 900, "number": 7, "title": title, "body": "Closes KUA-1",
			"state": "open", "draft": false, "merged": merged,
			"html_url": "https://gitea.example.com/team/app/pulls/7",
			"head":     map[string]any{"ref": "feat/kua-1-x"},
			"base":     map[string]any{"ref": "main"},
			"user":     map[string]any{"login": "alice"},
		},
		"repository": map[string]any{"id": 42, "full_name": "team/app", "html_url": "https://gitea.example.com/team/app"},
	})
	return body
}

func giteaHeader(secret, event, delivery string, body []byte) http.Header {
	header := make(http.Header)
	header.Set("X-Forgejo-Event", event)
	header.Set("X-Forgejo-Delivery", delivery)
	header.Set("X-Forgejo-Signature", crypto.HMACSha256(body, secret))
	return header
}

func TestForgeGiteaAndForgejoMergeRequestTransitionsPostgres(t *testing.T) {
	for _, provider := range []domain.ForgeProvider{domain.ForgeGitea, domain.ForgeForgejo} {
		t.Run(string(provider), func(t *testing.T) {
			f := newForgeFixture(t)
			ctx := context.Background()
			conn := f.createConnection(t, provider, "https://forge.example.com", "secret")
			f.linkRepo(t, conn.ID, 42)
			body := giteaMRBody("opened", false, "KUA-1 fix")
			require.NoError(t, f.svc.HandleWebhook(ctx, uuid.MustParse(conn.ID), giteaHeader("secret", "pull_request", uuid.NewString(), body), body))
			require.Equal(t, "in_review", f.issueStatus(t))

			merged := giteaMRBody("closed", true, "KUA-1 fix")
			require.NoError(t, f.svc.HandleWebhook(ctx, uuid.MustParse(conn.ID), giteaHeader("secret", "pull_request", uuid.NewString(), merged), merged))
			require.Equal(t, "done", f.issueStatus(t))

			activity, err := f.svc.GetIssueActivity(ctx, f.ws, "KUA-1")
			require.NoError(t, err)
			require.Len(t, activity.MergeRequests, 1)
			require.Equal(t, string(provider), activity.MergeRequests[0].Provider)
		})
	}
}

func TestForgeWebhookRejectionsAndIgnoredRepositoriesPostgres(t *testing.T) {
	f := newForgeFixture(t)
	ctx := context.Background()
	conn := f.createConnection(t, domain.ForgeGitLab, "https://gitlab.example.com", "secret")
	f.linkRepo(t, conn.ID, 42)
	body := gitlabMRBody("open", "opened", "KUA-1 fix")

	header := make(http.Header)
	header.Set("X-Gitlab-Event", "Merge Request Hook")
	header.Set("X-Gitlab-Token", "wrong")
	header.Set("X-Gitlab-Event-UUID", uuid.NewString())
	require.ErrorIs(t, f.svc.HandleWebhook(ctx, uuid.MustParse(conn.ID), header, body), ErrForgeUnauthorized)

	var count int
	require.NoError(t, f.db.Get(&count, `SELECT count(*) FROM forge_merge_requests WHERE workspace_id=$1`, f.ws))
	require.Zero(t, count, "a rejected delivery writes nothing")

	// A valid delivery for an unlinked repository is acknowledged and ignored.
	header.Set("X-Gitlab-Token", "secret")
	unknown := gitlabMRBody("open", "opened", "KUA-1 fix")
	var payload map[string]any
	require.NoError(t, json.Unmarshal(unknown, &payload))
	payload["project"].(map[string]any)["id"] = 99
	unknown, _ = json.Marshal(payload)
	require.NoError(t, f.svc.HandleWebhook(ctx, uuid.MustParse(conn.ID), header, unknown))
	require.NoError(t, f.db.Get(&count, `SELECT count(*) FROM forge_merge_requests WHERE workspace_id=$1`, f.ws))
	require.Zero(t, count)
	require.Equal(t, "todo", f.issueStatus(t))
}

func TestForgeWebhookReplayIsProcessedOncePostgres(t *testing.T) {
	f := newForgeFixture(t)
	ctx := context.Background()
	conn := f.createConnection(t, domain.ForgeGitLab, "https://gitlab.example.com", "secret")
	f.linkRepo(t, conn.ID, 42)
	body := gitlabMRBody("open", "opened", "KUA-1 fix")
	header := make(http.Header)
	header.Set("X-Gitlab-Event", "Merge Request Hook")
	header.Set("X-Gitlab-Token", "secret")
	header.Set("X-Gitlab-Event-UUID", "delivery-1")
	require.NoError(t, f.svc.HandleWebhook(ctx, uuid.MustParse(conn.ID), header, body))
	require.Equal(t, "in_review", f.issueStatus(t))

	_, err := f.db.Exec(`UPDATE issues SET status='todo' WHERE id=$1`, f.issue)
	require.NoError(t, err)
	require.NoError(t, f.svc.HandleWebhook(ctx, uuid.MustParse(conn.ID), header, body))
	require.Equal(t, "todo", f.issueStatus(t), "a replayed delivery does not transition again")
	var count int
	require.NoError(t, f.db.Get(&count, `SELECT count(*) FROM forge_merge_requests WHERE workspace_id=$1`, f.ws))
	require.Equal(t, 1, count)
}

func TestForgeConnectionIsolationPostgres(t *testing.T) {
	f := newForgeFixture(t)
	ctx := context.Background()
	owner := uuid.New()
	_, err := f.db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Other forge','hash')`, owner, owner.String()+"@test.invalid")
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = f.db.Exec(`DELETE FROM users WHERE id=$1`, owner) })
	other := uuid.New()
	_, err = f.db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Other forge',$2,$3)`, other, other.String(), owner)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = f.db.Exec(`DELETE FROM workspaces WHERE id=$1`, other) })
	otherConn, err := f.svc.CreateConnection(ctx, other, owner, dto.CreateForgeConnectionRequest{
		Provider: "gitlab", BaseURL: "https://gitlab.example.com", WebhookSecret: "other-secret",
	})
	require.NoError(t, err)
	otherID := uuid.MustParse(otherConn.ID)
	_, err = f.svc.LinkRepo(ctx, other, otherID, dto.LinkForgeRepoRequest{ExternalRepoID: 42, FullName: "team/app", DefaultBranch: "main"})
	require.NoError(t, err)

	header := make(http.Header)
	header.Set("X-Gitlab-Event", "Merge Request Hook")
	header.Set("X-Gitlab-Token", "other-secret")
	header.Set("X-Gitlab-Event-UUID", uuid.NewString())
	require.NoError(t, f.svc.HandleWebhook(ctx, otherID, header, gitlabMRBody("open", "opened", "KUA-1 fix")))
	require.Equal(t, "todo", f.issueStatus(t), "another workspace's webhook cannot move this issue")
}
