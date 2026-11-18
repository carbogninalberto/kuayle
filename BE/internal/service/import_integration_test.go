package service

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/require"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/importer"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/pkg/storage"
)

// contentSource serves real attachment bytes so the store's type detection
// and storage run as in production.
type contentSource struct {
	fakeSource
	content map[string]string
}

func (s *contentSource) OpenAttachment(_ context.Context, attachment importer.SourceAttachment) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(s.content[attachment.ExternalID])), nil
}

type importDBFixture struct {
	db                     *sqlx.DB
	svc                    *ImportService
	repo                   *repository.ImportJobRepository
	source                 *contentSource
	ws, admin, jane        uuid.UUID
	slug                   string
	eng, sec               uuid.UUID
	engStarted, engDefault uuid.UUID
}

func newImportDBFixture(t *testing.T) *importDBFixture {
	t.Helper()
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	db, err := sqlx.Connect("pgx", databaseURL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	f := &importDBFixture{
		db: db, ws: uuid.New(), admin: uuid.New(), jane: uuid.New(), eng: uuid.New(), sec: uuid.New(),
		engStarted: uuid.New(), engDefault: uuid.New(),
	}
	f.slug = "import-" + strings.ReplaceAll(f.ws.String(), "-", "")[:12]
	exec := func(query string, args ...any) {
		t.Helper()
		_, err := db.Exec(query, args...)
		require.NoError(t, err, query)
	}
	exec(`INSERT INTO users(id,email,name,display_name,password_hash) VALUES($1,$2,'Admin','Admin','x')`, f.admin, "admin-"+f.admin.String()+"@example.test")
	exec(`INSERT INTO users(id,email,name,display_name,password_hash) VALUES($1,$2,'Jane','Jane','x')`, f.jane, "jane-"+f.jane.String()+"@example.test")
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM workspaces WHERE id=$1`, f.ws)
		_, _ = db.Exec(`DELETE FROM users WHERE id IN ($1,$2)`, f.admin, f.jane)
	})
	exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Import',$2,$3)`, f.ws, f.slug, f.admin)
	exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'owner'),($1,$3,'member')`, f.ws, f.admin, f.jane)
	exec(`INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Engineering','ENG')`, f.eng, f.ws)
	exec(`INSERT INTO teams(id,workspace_id,name,key,is_private) VALUES($1,$2,'Security','SEC',TRUE)`, f.sec, f.ws)
	exec(`INSERT INTO team_members(team_id,user_id) VALUES($1,$2),($1,$3),($4,$2)`, f.eng, f.admin, f.jane, f.sec)
	exec(`INSERT INTO team_statuses(id,team_id,name,slug,category,position,is_default) VALUES
		($1,$3,'Todo','todo','unstarted',0,true),($2,$3,'In Progress','in-progress','started',1,false),
		(gen_random_uuid(),$4,'Todo','todo','unstarted',0,true)`, f.engDefault, f.engStarted, f.eng, f.sec)

	backend, err := storage.NewLocalBackend(t.TempDir(), "")
	require.NoError(t, err)
	f.repo = repository.NewImportJobRepository(db)
	store := NewSQLImportStore(repository.NewTeamRepository(db), repository.NewTeamStatusRepository(db),
		repository.NewWorkspaceRepository(db), repository.NewLabelRepository(db), repository.NewCycleRepository(db),
		repository.NewProjectRepository(db), repository.NewIssueRepository(db), repository.NewCommentRepository(db),
		repository.NewIssueRelationRepository(db), repository.NewAssetRepository(db), f.repo, backend, domain.MaxUploadSize)
	f.source = &contentSource{fakeSource: fakeSource{attachErr: map[string]error{}}, content: map[string]string{
		"a1": "%PDF-1.4\n1 0 obj\n<<>>\nendobj\ntrailer\n<<>>\n%%EOF\n",
		"a2": "plain security notes\n",
	}}
	factory := func(context.Context, json.RawMessage, json.RawMessage) (importer.Source, error) { return f.source, nil }
	f.svc = NewImportService(f.repo, store, factory, cryptoKeyForTest(), domain.MaxUploadSize)
	return f
}

func (f *importDBFixture) issues(created time.Time) []importer.SourceIssue {
	sprintStart := created.Add(24 * time.Hour)
	janeEmail := "jane-" + f.jane.String() + "@example.test"
	return []importer.SourceIssue{
		{
			ExternalID: "10001", ExternalKey: "PROJ-1", URL: "https://acme.atlassian.net/browse/PROJ-1",
			Title: "Parent", Description: "> Imported from Jira [PROJ-1](https://acme.atlassian.net/browse/PROJ-1)\n\n**Spec** first",
			Status: "In Progress", Priority: "High", ProjectExternalID: "10", CreatedAt: created, UpdatedAt: created,
			Reporter: &importer.SourceUser{ExternalID: "acc-jane", Email: janeEmail, DisplayName: "Jane"},
			Assignee: &importer.SourceUser{ExternalID: "acc-jane", Email: janeEmail, DisplayName: "Jane"},
			Labels:   []string{"backend"},
			Sprints:  []importer.SourceSprint{{Name: "Sprint 7", State: importer.SprintStateActive, StartDate: &sprintStart}},
			Comments: []importer.SourceComment{{
				ExternalID: "c1", Author: importer.SourceUser{ExternalID: "acc-ghost", DisplayName: "Ghost"},
				Body: "first **comment**", CreatedAt: created, UpdatedAt: created,
			}},
			Attachments:     []importer.SourceAttachment{{ExternalID: "a1", Filename: "spec.pdf", Size: 60}},
			Relations:       []importer.SourceRelation{{ExternalID: "l1:out", SourceExternalID: "10001", TargetExternalID: "10002", Type: domain.RelationBlocking}},
			HistoryMarkdown: "## History\n- 2024-01-02 Jane changed status: To Do -> In Progress",
		},
		{
			ExternalID: "10002", ExternalKey: "PROJ-2", Title: "Sub-task", Status: "To Do", ProjectExternalID: "10",
			ParentExternalID: "10001", CreatedAt: created, UpdatedAt: created,
		},
		{
			ExternalID: "20001", ExternalKey: "SECR-1", Title: "Private finding", Status: "Open", ProjectExternalID: "20",
			CreatedAt: created, UpdatedAt: created,
			Attachments: []importer.SourceAttachment{{ExternalID: "a2", Filename: "notes.txt", Size: 21}},
		},
	}
}

// run creates a job as the admin, maps, previews, starts and runs it.
func (f *importDBFixture) run(t *testing.T) *domain.ImportJob {
	t.Helper()
	ctx := domain.WithAccess(context.Background(), domain.Access{UserID: f.admin, WorkspaceID: f.ws})
	job, err := f.svc.Create(ctx, f.ws, f.admin, ImportCreateInput{
		Source: domain.ImportSourceJira, Config: json.RawMessage(`{"cloud":true}`),
		Credentials: json.RawMessage(`{"base_url":"https://acme.atlassian.net","email":"admin@example.test","token":"secret"}`),
	})
	require.NoError(t, err)
	f.source.preview = &importer.SourcePreview{
		Counts:   map[string]int{"issues": len(f.source.issues)},
		Statuses: []string{"In Progress", "Open", "To Do"},
		Projects: []importer.SourceProject{{ExternalID: "10", Name: "Project", Key: "PROJ"}, {ExternalID: "20", Name: "Secret", Key: "SECR"}},
	}
	_, err = f.svc.Preview(ctx, f.ws, job.ID)
	require.NoError(t, err)
	mapping := importer.NewMapping()
	mapping.Projects["10"] = importer.ProjectTarget{TeamID: f.eng}
	mapping.Projects["20"] = importer.ProjectTarget{TeamID: f.sec}
	mapping.Statuses[f.eng.String()+":in progress"] = importer.StatusTarget{TeamID: f.eng, StatusID: f.engStarted}
	mapping.Priorities["high"] = 2
	raw, err := json.Marshal(mapping)
	require.NoError(t, err)
	require.NoError(t, f.svc.UpdateMapping(ctx, f.ws, job.ID, raw))
	require.NoError(t, f.svc.Start(ctx, f.ws, job.ID))

	claimed, err := f.repo.ClaimNext(context.Background())
	require.NoError(t, err)
	require.NotNil(t, claimed)
	require.Equal(t, job.ID, claimed.ID)
	f.svc.process(context.Background(), claimed)

	finished, err := f.repo.Get(context.Background(), f.ws, job.ID)
	require.NoError(t, err)
	items, err := f.repo.ListItems(context.Background(), f.ws, job.ID, 100, 0)
	require.NoError(t, err)
	require.Equal(t, domain.ImportStatusCompleted, finished.Status, "job error %v, items %+v", finished.Error, items)
	require.Nil(t, finished.CredentialsEnc, "credentials are wiped when the job ends")
	require.Empty(t, items, "no item should fail or be skipped")
	return finished
}

type importedIssueRow struct {
	ID          uuid.UUID  `db:"id"`
	TeamID      uuid.UUID  `db:"team_id"`
	Identifier  string     `db:"identifier_text"`
	Title       string     `db:"title"`
	Description *string    `db:"description"`
	StatusID    *uuid.UUID `db:"status_id"`
	Priority    int        `db:"priority"`
	Triaged     bool       `db:"triaged"`
	CreatorID   uuid.UUID  `db:"creator_id"`
	AssigneeID  *uuid.UUID `db:"assignee_id"`
	ParentID    *uuid.UUID `db:"parent_id"`
	CycleID     *uuid.UUID `db:"cycle_id"`
	SortOrder   float64    `db:"sort_order"`
	CreatedAt   time.Time  `db:"created_at"`
}

func (f *importDBFixture) issueByKey(t *testing.T, key string) importedIssueRow {
	t.Helper()
	var row importedIssueRow
	require.NoError(t, f.db.Get(&row, `SELECT i.id, i.team_id, i.identifier_text, i.title, i.description, i.status_id, i.priority,
		i.triaged, i.creator_id, i.assignee_id, i.parent_id, i.cycle_id, i.sort_order, i.created_at
		FROM issues i JOIN import_external_refs r ON r.kuayle_id = i.id
		WHERE r.workspace_id = $1 AND r.external_type = 'issue' AND r.external_key = $2`, f.ws, key))
	return row
}

// The whole Jira path against a real database: numbering, private team,
// statuses, people, sprint, parent, link, comments, attachments and history,
// then a re-run that skips and an edit that updates in place.
func TestJiraImportWritesThroughTheRealStore(t *testing.T) {
	f := newImportDBFixture(t)
	created := time.Date(2024, 1, 2, 10, 0, 0, 0, time.UTC)
	f.source.issues = f.issues(created)
	job := f.run(t)
	require.Equal(t, 3, job.CreatedCount)
	require.Equal(t, 3, job.Processed)

	parent := f.issueByKey(t, "PROJ-1")
	child := f.issueByKey(t, "PROJ-2")
	private := f.issueByKey(t, "SECR-1")
	require.Equal(t, "ENG-1", parent.Identifier, "numbering is Kuayle's own")
	require.Equal(t, "ENG-2", child.Identifier)
	require.Equal(t, "SEC-1", private.Identifier)
	require.Equal(t, f.sec, private.TeamID, "the private team receives its project's issues")
	require.Equal(t, float64(-1000), parent.SortOrder)
	require.True(t, parent.Triaged)
	require.True(t, created.Equal(parent.CreatedAt), "the source creation date is kept")
	require.Equal(t, f.engStarted, *parent.StatusID)
	require.Equal(t, f.engDefault, *child.StatusID, "an unmapped status uses the team default")
	require.Equal(t, 2, parent.Priority)
	require.Equal(t, f.jane, parent.CreatorID, "a matched reporter keeps authorship")
	require.Equal(t, f.jane, *parent.AssigneeID)
	require.Nil(t, child.AssigneeID)
	require.Equal(t, parent.ID, *child.ParentID, "the sub-task is a sub-issue")

	var cycle struct {
		Name      string     `db:"name"`
		Status    string     `db:"status"`
		StartDate *time.Time `db:"start_date"`
	}
	require.NoError(t, f.db.Get(&cycle, `SELECT name, status, start_date FROM cycles WHERE id = $1`, *parent.CycleID))
	require.Equal(t, "Sprint 7", cycle.Name)
	require.Equal(t, string(domain.CycleStatusActive), cycle.Status)
	require.NotNil(t, cycle.StartDate)

	var label string
	require.NoError(t, f.db.Get(&label, `SELECT l.name FROM issue_labels il JOIN labels l ON l.id = il.label_id WHERE il.issue_id = $1`, parent.ID))
	require.Equal(t, "backend", label)
	var relations int
	require.NoError(t, f.db.Get(&relations, `SELECT count(*) FROM issue_relations WHERE issue_id = $1 AND related_issue_id = $2 AND type = 'blocking'`, parent.ID, child.ID))
	require.Equal(t, 1, relations)

	var parentAsset, privateAsset domain.Asset
	require.NoError(t, f.db.Get(&parentAsset, `SELECT * FROM assets WHERE workspace_id = $1 AND filename = 'spec.pdf'`, f.ws))
	require.NoError(t, f.db.Get(&privateAsset, `SELECT * FROM assets WHERE workspace_id = $1 AND filename = 'notes.txt'`, f.ws))
	require.Equal(t, "application/pdf", parentAsset.ContentType)
	require.Equal(t, f.sec, *privateAsset.TeamID, "a private issue's file stays in the private team")
	description := *parent.Description
	require.Contains(t, description, `<strong>Spec</strong> first`)
	require.Contains(t, description, `href="https://acme.atlassian.net/browse/PROJ-1"`)
	require.Contains(t, description, `href="/api/workspaces/`+f.slug+`/assets/`+parentAsset.ID.String()+`?download=1"`)
	require.Contains(t, description, `data-type="attachment"`)

	type commentRow struct {
		UserID    uuid.UUID `db:"user_id"`
		Body      string    `db:"body"`
		CreatedAt time.Time `db:"created_at"`
	}
	var comments []commentRow
	require.NoError(t, f.db.Select(&comments, `SELECT user_id, body, created_at FROM comments WHERE issue_id = $1 ORDER BY body`, parent.ID))
	require.Len(t, comments, 2, "the comment and the history summary")
	require.Contains(t, comments[0].Body, "<h2>History</h2>")
	require.Contains(t, comments[1].Body, "<em>Originally by Ghost</em>")
	require.Contains(t, comments[1].Body, "<strong>comment</strong>")
	require.Equal(t, f.admin, comments[1].UserID, "an unmatched author is attributed to the importer")
	require.True(t, created.Equal(comments[1].CreatedAt))

	// Re-running the same source creates nothing and skips every issue.
	again := f.run(t)
	require.Zero(t, again.CreatedCount)
	require.Zero(t, again.UpdatedCount)
	require.Equal(t, 3, again.SkippedCount)
	var issues, assets, allComments int
	require.NoError(t, f.db.Get(&issues, `SELECT count(*) FROM issues WHERE workspace_id = $1`, f.ws))
	require.NoError(t, f.db.Get(&assets, `SELECT count(*) FROM assets WHERE workspace_id = $1`, f.ws))
	require.NoError(t, f.db.Get(&allComments, `SELECT count(*) FROM comments c JOIN issues i ON i.id = c.issue_id WHERE i.workspace_id = $1`, f.ws))
	require.Equal(t, 3, issues)
	require.Equal(t, 2, assets)
	require.Equal(t, 2, allComments)

	// An edit in Jira updates the issue and the comment in place.
	edited := f.issues(created)
	edited[0].Title = "Parent, renamed"
	edited[0].Description = "Changed **text**"
	edited[0].UpdatedAt = created.Add(time.Hour)
	edited[0].Comments[0].Body = "first comment, edited"
	edited[0].Comments[0].UpdatedAt = created.Add(time.Hour)
	f.source.issues = edited
	updated := f.run(t)
	require.Equal(t, 1, updated.UpdatedCount)
	require.Equal(t, 2, updated.SkippedCount)
	after := f.issueByKey(t, "PROJ-1")
	require.Equal(t, "Parent, renamed", after.Title)
	require.Contains(t, *after.Description, "Changed <strong>text</strong>")
	require.Contains(t, *after.Description, parentAsset.ID.String(), "the attachment link survives the update")
	require.NoError(t, f.db.Select(&comments, `SELECT user_id, body, created_at FROM comments WHERE issue_id = $1 ORDER BY body`, parent.ID))
	require.Len(t, comments, 2)
	require.Contains(t, comments[1].Body, "first comment, edited")
	require.NoError(t, f.db.Get(&assets, `SELECT count(*) FROM assets WHERE workspace_id = $1`, f.ws))
	require.Equal(t, 2, assets)
}
