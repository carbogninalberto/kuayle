package service

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type webhookTestTransport struct {
	target *url.URL
	base   http.RoundTripper
}

func (t webhookTestTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	clone := r.Clone(r.Context())
	u := *r.URL
	clone.URL = &u
	clone.URL.Scheme = t.target.Scheme
	clone.URL.Host = t.target.Host
	return t.base.RoundTrip(clone)
}

// Uses real migrated PostgreSQL and the real app client against a local GitHub
// fixture. The same test runs in CI's Backend Tests database job.
func TestPullRequestCommentWebhookPostgres(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL is not set")
	}
	databaseURL, err := url.Parse(dsn)
	require.NoError(t, err)
	applicationName := "kua60-" + uuid.NewString()
	options := databaseURL.Query()
	options.Set("application_name", applicationName)
	databaseURL.RawQuery = options.Encode()
	db, err := repository.Open(databaseURL.String())
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	owner, ws, team, status, issue := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Bot test','hash')`, owner, owner.String()+"@test.invalid")
	require.NoError(t, err)
	t.Cleanup(func() { _, err := db.Exec(`DELETE FROM users WHERE id=$1`, owner); require.NoError(t, err) })
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Bot test',$2,$3)`, ws, ws.String(), owner)
	require.NoError(t, err)
	t.Cleanup(func() { _, err := db.Exec(`DELETE FROM workspaces WHERE id=$1`, ws); require.NoError(t, err) })
	_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'owner')`, ws, owner)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Bot','BOT')`, team, ws)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO team_statuses(id,team_id,name,slug,category) VALUES($1,$2,'Todo','todo','unstarted')`, status, team)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO issues(id,workspace_id,team_id,status_id,number,identifier_text,title,description,creator_id) VALUES($1,$2,$3,$4,60,'BOT-60','PRIVATE TITLE','PRIVATE DESCRIPTION',$5)`, issue, ws, team, status, owner)
	require.NoError(t, err)
	repo := repository.NewGitHubRepository(db)
	inst := &domain.GitHubInstallation{ID: uuid.New(), WorkspaceID: ws, InstallationID: time.Now().UnixNano(), AccountLogin: "owner", AccountType: "User", InstalledBy: owner}
	require.NoError(t, repo.CreateInstallation(ctx, inst))
	linked := &domain.GitHubRepoModel{ID: uuid.New(), WorkspaceID: ws, InstallationID: inst.ID, GitHubRepoID: 123, FullName: "owner/repo", DefaultBranch: "main", IsActive: true}
	require.NoError(t, repo.CreateRepo(ctx, linked))
	var mu sync.Mutex
	comments := []map[string]any{}
	posts, requests := 0, 0
	failStatus := 0
	postArrived, releasePost := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		requests++
		assert.NotEmpty(t, r.Header.Get("Authorization"))
		if r.URL.Path == fmt.Sprintf("/app/installations/%d/access_tokens", inst.InstallationID) {
			w.WriteHeader(201)
			fmt.Fprint(w, `{"token":"test-token","expires_at":"2099-01-01T00:00:00Z"}`)
			return
		}
		if failStatus != 0 {
			w.WriteHeader(failStatus)
			fmt.Fprint(w, "PRIVATE API CONTENT")
			return
		}
		if r.Method == http.MethodGet {
			assert.NoError(t, json.NewEncoder(w).Encode(comments))
			return
		}
		posts++
		if posts == 1 {
			close(postArrived)
			mu.Unlock()
			<-releasePost
			mu.Lock()
		}
		var payload map[string]string
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		assert.Contains(t, payload["body"], "[BOT-60](https://kuayle.test/"+ws.String()+"/issue/BOT-60)")
		assert.NotContains(t, payload["body"], "PRIVATE")
		comments = append(comments, map[string]any{"body": payload["body"], "user": map[string]any{"type": "Bot"}, "performed_via_github_app": map[string]any{"id": 42}})
		w.WriteHeader(201)
		fmt.Fprint(w, `{"id":1}`)
	}))
	t.Cleanup(server.Close)
	target, err := url.Parse(server.URL)
	require.NoError(t, err)
	original := http.DefaultTransport
	http.DefaultTransport = webhookTestTransport{target: target, base: original}
	t.Cleanup(func() { http.DefaultTransport = original })
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	history := new(mockIssueHistoryRepo)
	history.On("Create", mock.Anything, issue, uuid.Nil, "status", mock.Anything, mock.Anything).Return(nil).Once()
	reviewStatus := uuid.New()
	_, err = db.Exec(`INSERT INTO team_statuses(id,team_id,name,slug,category) VALUES($1,$2,'Review','in_review','started')`, reviewStatus, team)
	require.NoError(t, err)
	require.NoError(t, repo.UpsertAutoTransition(ctx, &domain.GitHubAutoTransition{ID: uuid.New(), WorkspaceID: ws, Event: "pr_opened", TargetStatus: "in_review", TargetStatusID: &reviewStatus, IsActive: true}))
	svc := NewGitHubService(repo, repository.NewIssueRepository(db), nil, nil, history, nil, nil, "https://kuayle.test", "", &GlobalGitHubAppConfig{AppID: 42, PrivateKey: string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})), WebhookSecret: "webhook-secret"})
	event := ghWebhookPR{Action: "opened"}
	event.Installation.ID = inst.InstallationID
	event.Repository.ID = 123
	event.PullRequest.ID = 456
	event.PullRequest.Number = 7
	event.PullRequest.Title = "BOT-60"
	event.PullRequest.State = "open"
	event.PullRequest.Draft = true
	event.PullRequest.User.Login = "agent[bot]"
	event.PullRequest.Head.Ref = "agent/bot-60"
	event.PullRequest.HTMLURL = "https://github.com/owner/repo/pull/7"
	payload, err := json.Marshal(event)
	require.NoError(t, err)
	mac := hmac.New(sha256.New, []byte("webhook-secret"))
	mac.Write(payload)
	signature := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	require.True(t, svc.VerifyWebhookSignature(ctx, payload, signature))
	require.False(t, svc.VerifyWebhookSignature(ctx, append(payload, ' '), signature))
	require.False(t, svc.VerifyWebhookSignature(ctx, payload, "bad"))
	// Concurrent duplicate deliveries across separate service instances share SQL locks.
	errs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		go func() { copy := *svc; errs <- copy.HandleWebhookEvent(ctx, "pull_request", payload) }()
	}
	select {
	case <-postArrived:
	case err := <-errs:
		t.Fatalf("delivery completed before comment POST: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("comment POST did not start")
	}
	// Keep the first remote POST in flight until a competing delivery is visibly
	// blocked in PostgreSQL. This proves replica serialization deterministically.
	serialized := assert.Eventually(t, func() bool {
		var blocked bool
		err := db.Get(&blocked, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND application_name=$1 AND pid<>pg_backend_pid() AND query LIKE 'SELECT pg_advisory_xact_lock(610060,%' AND wait_event_type='Lock')`, applicationName)
		return err == nil && blocked
	}, 2*time.Second, 10*time.Millisecond)
	close(releasePost)
	require.True(t, serialized)
	for i := 0; i < 4; i++ {
		require.NoError(t, <-errs)
	}
	require.Equal(t, 1, posts)
	pr, err := repo.GetPRByRepoAndNumber(ctx, linked.ID, 7)
	require.NoError(t, err)
	require.Equal(t, issue, *pr.IssueID)
	require.Equal(t, "draft", pr.State)
	var currentStatus string
	require.NoError(t, db.Get(&currentStatus, `SELECT status FROM issues WHERE id=$1`, issue))
	require.Equal(t, "in_review", currentStatus)
	// A user can move the issue after initial delivery. Comment redelivery must
	// not overwrite that decision or write another status history entry.
	_, err = db.Exec(`UPDATE issues SET status='todo', status_id=$2 WHERE id=$1`, issue, status)
	require.NoError(t, err)
	before := requests
	// No comments for ordinary updates, missing/ambiguous references, inactive repos,
	// unknown installations or private teams (even with an owner's context).
	event.Action = "edited"
	payload, _ = json.Marshal(event)
	require.NoError(t, svc.HandleWebhookEvent(ctx, "pull_request", payload))
	require.Equal(t, before, requests)
	event.Action = "opened"
	for _, title := range []string{"BOT-61", "no reference"} {
		event.PullRequest.Title = title
		event.PullRequest.Head.Ref = "agent/bot"
		payload, _ = json.Marshal(event)
		require.NoError(t, svc.HandleWebhookEvent(ctx, "pull_request", payload))
		require.Equal(t, before, requests)
	}
	_, err = db.Exec(`INSERT INTO issues(id,workspace_id,team_id,status_id,number,identifier_text,title,creator_id) VALUES($1,$2,$3,$4,61,'BOT-61','Another issue',$5)`, uuid.New(), ws, team, status, owner)
	require.NoError(t, err)
	event.PullRequest.Title = "BOT-60 BOT-61"
	payload, _ = json.Marshal(event)
	require.NoError(t, svc.HandleWebhookEvent(ctx, "pull_request", payload))
	require.Equal(t, before, requests)
	event.PullRequest.Title = "BOT-60"
	payload, _ = json.Marshal(event)
	require.NoError(t, repo.DeactivateRepo(ctx, linked.ID))
	require.NoError(t, svc.HandleWebhookEvent(ctx, "pull_request", payload))
	require.Equal(t, before, requests)
	_, err = db.Exec(`UPDATE github_repos SET is_active=true WHERE id=$1`, linked.ID)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE teams SET is_private=true WHERE id=$1`, team)
	require.NoError(t, err)
	require.NoError(t, svc.HandleWebhookEvent(domain.WithAccess(ctx, domain.Access{UserID: owner, WorkspaceID: ws}), "pull_request", payload))
	require.Equal(t, before, requests)
	_, err = db.Exec(`UPDATE teams SET is_private=false WHERE id=$1`, team)
	require.NoError(t, err)
	// API failure propagates for redelivery; existing PR linkage survives. A later
	// delivery reconciles the original comment without posting a duplicate.
	mu.Lock()
	failStatus = 403
	mu.Unlock()
	err = svc.HandleWebhookEvent(ctx, "pull_request", payload)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "PRIVATE")
	mu.Lock()
	failStatus = 0
	mu.Unlock()
	require.NoError(t, svc.HandleWebhookEvent(ctx, "pull_request", payload))
	require.Equal(t, 1, posts)
	// A queued API privacy transition must wait before the global graph barrier,
	// permitting unrelated writes (including edits to this issue) during I/O.
	privacyResult := make(chan error, 1)
	actor := domain.WithAccess(ctx, domain.Access{UserID: owner, WorkspaceID: ws})
	require.NoError(t, repo.WithPullRequestCommentLock(ctx, inst, linked, 7, issue, func(string) error {
		go func() {
			changeCtx, cancel := context.WithTimeout(actor, 5*time.Second)
			defer cancel()
			_, err := repository.NewTeamRepository(db).SetVisibility(changeCtx, team, true, false, true)
			privacyResult <- err
		}()
		require.Eventually(t, func() bool {
			var waiting bool
			e := db.Get(&waiting, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND application_name=$1 AND pid<>pg_backend_pid() AND query LIKE 'SELECT pg_advisory_xact_lock(hashtextextended%,63))' AND wait_event_type='Lock')`, applicationName)
			return e == nil && waiting
		}, 2*time.Second, 10*time.Millisecond)
		writeCtx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		_, err := repo.UpsertPullRequest(writeCtx, pr)
		require.NoError(t, err, "PR updates must not wait on GitHub I/O")
		_, err = db.ExecContext(writeCtx, `INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Unrelated','OTHER')`, uuid.New(), ws)
		require.NoError(t, err, "exclusive graph writes must not wait on GitHub I/O")
		_, err = db.ExecContext(writeCtx, `UPDATE issues SET title='edit during publication' WHERE id=$1`, issue)
		require.NoError(t, err)
		select {
		case err := <-privacyResult:
			t.Fatalf("privacy transition completed during publication: %v", err)
		default:
		}
		return nil
	}))
	require.NoError(t, <-privacyResult)
	_, err = repository.NewTeamRepository(db).SetVisibility(actor, team, false, true, false)
	require.NoError(t, err)
	require.NoError(t, db.Get(&currentStatus, `SELECT status FROM issues WHERE id=$1`, issue))
	require.Equal(t, "todo", currentStatus)
	history.AssertExpectations(t)
	// Lock cancellation does not poison future deliveries.
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	require.Error(t, repo.WithPullRequestCommentLock(cancelled, inst, linked, 7, issue, func(string) error { t.Fatal("cancelled callback"); return nil }))
	require.NoError(t, svc.HandleWebhookEvent(ctx, "pull_request", payload))
	require.Equal(t, 1, posts)
}
