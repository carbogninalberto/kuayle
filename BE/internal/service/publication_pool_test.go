package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/pkg/crypto"
	"github.com/stretchr/testify/require"
)

// Guarded AI requests hold a pooled connection (the publication lock) while
// the provider responds. Slow providers must not starve ordinary requests.
func TestSlowPublicationsDoNotExhaustConnectionPool(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := sqlx.Connect("pgx", url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	owner, ws, team, status := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Pool','hash')`, owner, owner.String()+"@example.test")
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id=$1`, owner) })
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Pool',$2,$3)`, ws, ws.String(), owner)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM workspaces WHERE id=$1`, ws) })
	_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'owner')`, ws, owner)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Pool','POOL')`, team, ws)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO team_statuses(id,team_id,name,slug,category) VALUES($1,$2,'Todo','todo','unstarted')`, status, team)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO issues(workspace_id,team_id,status_id,number,identifier_text,title,creator_id) VALUES($1,$2,$3,1,'POOL-1','Pool title',$4)`, ws, team, status, owner)
	require.NoError(t, err)

	release := make(chan struct{})
	var releaseOnce sync.Once
	stop := func() { releaseOnce.Do(func() { close(release) }) }
	arrived := make(chan struct{}, 64)
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		arrived <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"<p>Expanded</p>"}}]}`))
	}))
	t.Cleanup(receiver.Close)
	t.Cleanup(stop)
	key := crypto.DeriveKey("publication-pool-test")
	encrypted, err := crypto.Encrypt("test-key", key)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO ai_settings(workspace_id,base_url,model,api_key_encrypted) VALUES($1,$2,'test-model',$3)`, ws, receiver.URL, encrypted)
	require.NoError(t, err)

	const poolSize = 12
	pool, err := sqlx.Connect("pgx", url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })
	pool.SetMaxOpenConns(poolSize)
	ai := NewAISettingsService(repository.NewAISettingsRepository(pool), repository.NewWorkspaceRepository(pool), repository.NewIssueRepository(pool), key)
	ownerCtx, cancel := context.WithTimeout(domain.WithAccess(context.Background(), domain.Access{UserID: owner, WorkspaceID: ws}), 20*time.Second)
	defer cancel()
	var requests sync.WaitGroup
	for range poolSize {
		requests.Add(1)
		go func() {
			defer requests.Done()
			_, _ = ai.ExpandIssueDescription(ownerCtx, ws, "POOL-1", nil)
		}()
	}
	for range maxConcurrentPublications {
		select {
		case <-arrived:
		case <-time.After(5 * time.Second):
			t.Fatal("provider requests did not start")
		}
	}
	select {
	case <-arrived:
		t.Fatal("more guarded publications started than the configured limit")
	case <-time.After(300 * time.Millisecond):
	}

	deadline, stopQuery := context.WithTimeout(context.Background(), time.Second)
	defer stopQuery()
	var one int
	require.NoError(t, pool.GetContext(deadline, &one, `SELECT 1`), "ordinary requests must still obtain a connection")

	stop()
	requests.Wait()
}
