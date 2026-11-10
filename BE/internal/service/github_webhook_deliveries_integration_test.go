package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

type flakySignals struct {
	err      error
	recorded int
}

func (f *flakySignals) Record(context.Context, uuid.UUID, string, int, domain.AgentTaskSignalInput) error {
	if f.err != nil {
		return f.err
	}
	f.recorded++
	return nil
}

func TestWebhookDeliveryIDsAreValidated(t *testing.T) {
	require.True(t, ValidWebhookDeliveryID("72d3162e-cc78-11e3-81ab-4c9367dc0958"))
	for _, id := range []string{"", " ", "a b", "a\nb", "café", string(make([]byte, 129))} {
		require.False(t, ValidWebhookDeliveryID(id), "%q", id)
	}
}

// A delivery is processed once. One whose processing failed stays
// redeliverable under the same ID, because GitHub's manual redelivery reuses
// it; a claim left behind by a request that died is taken over after its lease.
func TestWebhookDeliveryIsProcessedOnceAndStaysRedeliverableAfterFailurePostgres(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := repository.Open(dsn)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	owner, ws := uuid.New(), uuid.New()
	_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Delivery test','hash')`, owner, owner.String()+"@test.invalid")
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id=$1`, owner) })
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Delivery test',$2,$3)`, ws, ws.String(), owner)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM workspaces WHERE id=$1`, ws) })
	repo := repository.NewGitHubRepository(db)
	inst := &domain.GitHubInstallation{ID: uuid.New(), WorkspaceID: ws, InstallationID: time.Now().UnixNano(), AccountLogin: "acme", AccountType: "Organization", InstalledBy: owner}
	require.NoError(t, repo.CreateInstallation(ctx, inst))
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM github_webhook_deliveries WHERE installation_id=$1`, inst.InstallationID)
	})
	linked := &domain.GitHubRepoModel{ID: uuid.New(), WorkspaceID: ws, InstallationID: inst.ID, GitHubRepoID: time.Now().UnixNano(), FullName: "acme/app", DefaultBranch: "main", IsActive: true}
	require.NoError(t, repo.CreateRepo(ctx, linked))
	signals := &flakySignals{err: errors.New("database is restarting")}
	svc := NewGitHubService(repo, repository.NewIssueRepository(db), nil, nil, nil, nil, nil, "https://kuayle.test", "", nil)
	svc.SetTaskSignals(signals)
	payload, err := json.Marshal(map[string]any{
		"action":       "completed",
		"check_suite":  map[string]any{"id": 77, "head_sha": "abc123", "conclusion": "failure", "pull_requests": []any{map[string]any{"number": 12}}},
		"repository":   map[string]any{"id": linked.GitHubRepoID},
		"installation": map[string]any{"id": inst.InstallationID},
	})
	require.NoError(t, err)
	delivery := uuid.NewString()

	duplicate, err := svc.HandleWebhookDelivery(ctx, "check_suite", delivery, payload)
	require.ErrorIs(t, err, signals.err)
	require.False(t, duplicate)

	signals.err = nil
	duplicate, err = svc.HandleWebhookDelivery(ctx, "check_suite", delivery, payload)
	require.NoError(t, err)
	require.False(t, duplicate, "a failed delivery is processed when GitHub redelivers it")
	require.Equal(t, 1, signals.recorded)

	duplicate, err = svc.HandleWebhookDelivery(ctx, "check_suite", delivery, payload)
	require.NoError(t, err)
	require.True(t, duplicate)
	require.Equal(t, 1, signals.recorded, "a processed delivery is not processed again")

	// Markers are scoped to the installation in the signed payload.
	other := uuid.NewString()
	claimed, err := repo.ClaimWebhookDelivery(ctx, inst.InstallationID, other, time.Minute)
	require.NoError(t, err)
	require.True(t, claimed)
	claimed, err = repo.ClaimWebhookDelivery(ctx, inst.InstallationID, other, time.Minute)
	require.NoError(t, err)
	require.False(t, claimed, "a delivery being processed is not processed concurrently")
	duplicate, err = svc.HandleWebhookDelivery(ctx, "check_suite", other, payload)
	require.NoError(t, err)
	require.True(t, duplicate)

	// The request holding the claim died: after the lease it can be taken over.
	_, err = db.Exec(`UPDATE github_webhook_deliveries SET claimed_at=now()-interval '1 hour' WHERE installation_id=$1 AND delivery_id=$2`, inst.InstallationID, other)
	require.NoError(t, err)
	duplicate, err = svc.HandleWebhookDelivery(ctx, "check_suite", other, payload)
	require.NoError(t, err)
	require.False(t, duplicate)
	require.Equal(t, 2, signals.recorded)
	// A processed delivery is never taken over, however old.
	_, err = db.Exec(`UPDATE github_webhook_deliveries SET claimed_at=now()-interval '1 hour' WHERE installation_id=$1 AND delivery_id=$2`, inst.InstallationID, other)
	require.NoError(t, err)
	duplicate, err = svc.HandleWebhookDelivery(ctx, "check_suite", other, payload)
	require.NoError(t, err)
	require.True(t, duplicate)

	// Cleanup removes only markers older than the retention.
	_, err = db.Exec(`UPDATE github_webhook_deliveries SET claimed_at=now()-interval '8 days' WHERE installation_id=$1 AND delivery_id=$2`, inst.InstallationID, other)
	require.NoError(t, err)
	_, err = repo.DeleteExpiredWebhookDeliveries(ctx, GitHubWebhookDeliveryRetention)
	require.NoError(t, err)
	var remaining []string
	require.NoError(t, db.Select(&remaining, `SELECT delivery_id FROM github_webhook_deliveries WHERE installation_id=$1`, inst.InstallationID))
	require.Equal(t, []string{delivery}, remaining)
}
