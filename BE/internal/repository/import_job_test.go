package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/require"

	"github.com/kuayle/kuayle-backend/internal/domain"
)

func importTestRepository(t *testing.T) (*ImportJobRepository, *sqlx.DB, uuid.UUID, uuid.UUID) {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db := isolatedPrivacyDatabase(t, url)
	user, workspace := uuid.New(), uuid.New()
	_, err := db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Importer','x')`, user, user.String()+"@example.test")
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Imports',$2,$3)`, workspace, workspace.String(), user)
	require.NoError(t, err)
	return NewImportJobRepository(db), db, workspace, user
}

func newDraftImport(t *testing.T, repo *ImportJobRepository, workspace, user uuid.UUID) *domain.ImportJob {
	t.Helper()
	secret := "sealed-credentials"
	job := &domain.ImportJob{
		WorkspaceID: workspace, CreatedBy: &user, Source: domain.ImportSourceJira, SourceSite: "acme.atlassian.net",
		Config: json.RawMessage(`{}`), Mapping: json.RawMessage(`{}`), CredentialsEnc: &secret,
	}
	require.NoError(t, repo.Create(context.Background(), job))
	return job
}

// The repository must hand back a full page plus one row, so the service can
// show "Load more" for every page size the API accepts.
func TestImportItemsPageAtEverySizeTheAPIAccepts(t *testing.T) {
	repo, _, workspace, user := importTestRepository(t)
	ctx := context.Background()
	job := newDraftImport(t, repo, workspace, user)
	for i := 0; i < 600; i++ {
		require.NoError(t, repo.AddItem(ctx, &domain.ImportJobItem{
			JobID: job.ID, WorkspaceID: workspace, ExternalType: domain.ImportExternalIssue,
			ExternalID: fmt.Sprint(i), ExternalKey: fmt.Sprintf("PROJ-%d", i), Outcome: domain.ImportOutcomeFailed, Reason: "x",
		}))
	}
	for _, tc := range []struct{ limit, offset, want int }{
		{201, 0, 201},
		{ImportItemsPageMax + 1, 0, ImportItemsPageMax + 1},
		{9999, 0, ImportItemsPageMax + 1},
		{100, 550, 50},
		{0, 0, 1},
	} {
		items, err := repo.ListItems(ctx, workspace, job.ID, tc.limit, tc.offset)
		require.NoError(t, err)
		require.Len(t, items, tc.want, "limit %d offset %d", tc.limit, tc.offset)
		require.Equal(t, fmt.Sprintf("PROJ-%d", tc.offset), items[0].ExternalKey, "rows come back in report order")
	}
	other, err := repo.ListItems(ctx, uuid.New(), job.ID, 50, 0)
	require.NoError(t, err)
	require.Empty(t, other, "the report is workspace scoped")
}

func TestImportPreviewAndMappingFollowTheJobLifecycle(t *testing.T) {
	repo, _, workspace, user := importTestRepository(t)
	ctx := context.Background()
	job := newDraftImport(t, repo, workspace, user)

	require.NoError(t, repo.SetPreview(ctx, workspace, job.ID, json.RawMessage(`{"counts":{"issues":1}}`)))
	require.NoError(t, repo.SetPreview(ctx, workspace, job.ID, json.RawMessage(`{"counts":{"issues":2}}`)), "a previewed job can be previewed again")
	got, err := repo.Get(ctx, workspace, job.ID)
	require.NoError(t, err)
	require.Equal(t, domain.ImportStatusPreviewed, got.Status)
	require.JSONEq(t, `{"counts":{"issues":2}}`, string(got.Preview))

	require.NoError(t, repo.UpdateMapping(ctx, workspace, job.ID, json.RawMessage(`{"priorities":{"high":2}}`), nil))
	got, err = repo.Get(ctx, workspace, job.ID)
	require.NoError(t, err)
	require.JSONEq(t, `{"counts":{"issues":2}}`, string(got.Preview), "no preview argument keeps the stored preview")
	require.JSONEq(t, `{"priorities":{"high":2}}`, string(got.Mapping))

	require.NoError(t, repo.UpdateMapping(ctx, workspace, job.ID, json.RawMessage(`{}`), json.RawMessage(`{"counts":{"issues":2},"mapping_gaps":[]}`)))
	got, err = repo.Get(ctx, workspace, job.ID)
	require.NoError(t, err)
	require.JSONEq(t, `{"counts":{"issues":2},"mapping_gaps":[]}`, string(got.Preview), "the refreshed preview is saved with the mapping")

	require.ErrorIs(t, repo.UpdateMapping(ctx, uuid.New(), job.ID, json.RawMessage(`{}`), nil), ErrImportJobNotFound)
	require.NoError(t, repo.Start(ctx, workspace, job.ID, 2))
	require.ErrorIs(t, repo.SetPreview(ctx, workspace, job.ID, json.RawMessage(`{}`)), ErrImportNotEditable)
	require.ErrorIs(t, repo.UpdateMapping(ctx, workspace, job.ID, json.RawMessage(`{}`), nil), ErrImportNotEditable)
}

// Credentials never outlive a job: cancelling a draft, an idle draft and a
// stale running job all clear them in the statement that ends the job.
func TestImportCredentialsAreClearedWhenJobsEnd(t *testing.T) {
	repo, db, workspace, user := importTestRepository(t)
	ctx := context.Background()

	cancelled := newDraftImport(t, repo, workspace, user)
	require.ErrorIs(t, repo.RequestCancel(ctx, uuid.New(), cancelled.ID), ErrImportNotEditable, "another workspace cannot cancel it")
	require.NoError(t, repo.RequestCancel(ctx, workspace, cancelled.ID))
	got, err := repo.Get(ctx, workspace, cancelled.ID)
	require.NoError(t, err)
	require.Equal(t, domain.ImportStatusCancelled, got.Status)
	require.Nil(t, got.CredentialsEnc)

	idle := newDraftImport(t, repo, workspace, user)
	fresh := newDraftImport(t, repo, workspace, user)
	_, err = db.Exec(`UPDATE import_jobs SET updated_at = now() - interval '25 hours' WHERE id = $1`, idle.ID)
	require.NoError(t, err)
	expired, err := repo.ExpireIdle(ctx, time.Now().Add(-24*time.Hour))
	require.NoError(t, err)
	require.EqualValues(t, 1, expired)
	got, err = repo.Get(ctx, workspace, idle.ID)
	require.NoError(t, err)
	require.Equal(t, domain.ImportStatusCancelled, got.Status)
	require.Nil(t, got.CredentialsEnc)
	got, err = repo.Get(ctx, workspace, fresh.ID)
	require.NoError(t, err)
	require.NotNil(t, got.CredentialsEnc, "a recent draft keeps its credentials")

	require.NoError(t, repo.SetPreview(ctx, workspace, fresh.ID, json.RawMessage(`{}`)))
	require.NoError(t, repo.Start(ctx, workspace, fresh.ID, 0))
	claimed, err := repo.ClaimNext(ctx)
	require.NoError(t, err)
	require.Equal(t, fresh.ID, claimed.ID)
	_, err = db.Exec(`UPDATE import_jobs SET started_at = now() - interval '2 hours' WHERE id = $1`, fresh.ID)
	require.NoError(t, err)
	require.NoError(t, repo.Heartbeat(ctx, fresh.ID))
	stale, err := repo.FailStaleRunning(ctx, time.Now().Add(-10*time.Minute))
	require.NoError(t, err)
	require.Zero(t, stale, "a job with a recent heartbeat is not stale, however long ago it started")
	_, err = db.Exec(`UPDATE import_jobs SET updated_at = now() - interval '11 minutes' WHERE id = $1`, fresh.ID)
	require.NoError(t, err)
	stale, err = repo.FailStaleRunning(ctx, time.Now().Add(-10*time.Minute))
	require.NoError(t, err)
	require.EqualValues(t, 1, stale)
	got, err = repo.Get(ctx, workspace, fresh.ID)
	require.NoError(t, err)
	require.Equal(t, domain.ImportStatusFailed, got.Status)
	require.Nil(t, got.CredentialsEnc)
}

func TestImportRefsAreUniquePerSiteAndUpdatedInPlace(t *testing.T) {
	repo, _, workspace, user := importTestRepository(t)
	ctx := context.Background()
	job := newDraftImport(t, repo, workspace, user)
	hash := "h1"
	ref := &domain.ImportExternalRef{
		WorkspaceID: workspace, Source: domain.ImportSourceJira, SourceSite: "acme.atlassian.net",
		ExternalType: domain.ImportExternalIssue, ExternalID: "10001", ExternalKey: "PROJ-1",
		KuayleID: uuid.New(), ContentHash: &hash, LastJobID: &job.ID,
	}
	require.NoError(t, repo.UpsertRef(ctx, nil, ref))
	firstID := ref.ID

	updated := *ref
	newHash := "h2"
	updated.ContentHash = &newHash
	require.NoError(t, repo.UpsertRef(ctx, nil, &updated))
	require.Equal(t, firstID, updated.ID, "a re-run updates the existing ref")

	got, err := repo.GetRef(ctx, workspace, domain.ImportSourceJira, "acme.atlassian.net", domain.ImportExternalIssue, "10001")
	require.NoError(t, err)
	require.Equal(t, "h2", *got.ContentHash)
	require.Equal(t, "PROJ-1", got.ExternalKey)

	otherSite, err := repo.GetRef(ctx, workspace, domain.ImportSourceJira, "other.atlassian.net", domain.ImportExternalIssue, "10001")
	require.NoError(t, err)
	require.Nil(t, otherSite, "the same Jira id on another site is a different issue")
	otherWorkspace, err := repo.GetRef(ctx, uuid.New(), domain.ImportSourceJira, "acme.atlassian.net", domain.ImportExternalIssue, "10001")
	require.NoError(t, err)
	require.Nil(t, otherWorkspace)
}
