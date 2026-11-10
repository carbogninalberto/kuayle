package service

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/pkg/sanitize"
	"github.com/kuayle/kuayle-backend/pkg/storage"
	"github.com/stretchr/testify/require"
)

// Markup an archive author controls. The handler sets a marker a test could
// observe; nothing here does more than that.
const importedUnsafeHTML = `<p>Kept text</p><img src=x onerror="window.importProbe=1"><a href="javascript:window.importProbe=1">link</a><script>window.importProbe=1</script>`

func requireImportedTextIsInert(t *testing.T, value string) {
	t.Helper()
	lower := strings.ToLower(value)
	require.NotContains(t, lower, "onerror")
	require.NotContains(t, lower, "javascript:")
	require.NotContains(t, lower, "<script")
}

func TestPrepareImportedRowStoresWhatTheWritePathsStore(t *testing.T) {
	tests := []struct {
		table, column string
		// The function the owning service applies before it stores the column.
		servicePath func(string) string
	}{
		{"issues", "title", sanitize.PlainText},
		{"issues", "description", sanitize.SanitizeEditorContent},
		{"comments", "body", sanitize.SanitizeHTML},
		{"projects", "name", sanitize.StripHTML},
		{"projects", "description", sanitize.SanitizeHTML},
		{"issue_templates", "description", sanitize.SanitizeEditorContent},
	}
	for _, test := range tests {
		t.Run(test.table+"."+test.column, func(t *testing.T) {
			row := dto.WorkspaceTransferRow{test.column: importedUnsafeHTML}
			prepareImportedRow(test.table, row)
			require.Equal(t, test.servicePath(importedUnsafeHTML), row[test.column])
			if test.column != "title" {
				// Titles are text-only fields: they are stored literally and escaped on output.
				requireImportedTextIsInert(t, transferString(row[test.column]))
				require.Contains(t, row[test.column], "Kept text")
			}
		})
	}

	t.Run("markdown descriptions become editor HTML as on create", func(t *testing.T) {
		row := dto.WorkspaceTransferRow{"description": "# Heading\n\n[link](javascript:window.importProbe=1)"}
		prepareImportedRow("issues", row)
		require.Equal(t, sanitize.SanitizeEditorContent("# Heading\n\n[link](javascript:window.importProbe=1)"), row["description"])
		require.NotContains(t, row["description"], `href="javascript:`)
	})

	t.Run("absent and null columns stay as they are", func(t *testing.T) {
		row := dto.WorkspaceTransferRow{"title": "Plain", "description": nil}
		prepareImportedRow("issues", row)
		require.Nil(t, row["description"])
		_, exists := row["body"]
		require.False(t, exists)
	})

	t.Run("a rich-text column that is not a string is sanitized as text", func(t *testing.T) {
		row := dto.WorkspaceTransferRow{"body": map[string]any{"html": importedUnsafeHTML}}
		prepareImportedRow("comments", row)
		body, ok := row["body"].(string)
		require.True(t, ok)
		requireImportedTextIsInert(t, body)
	})

	t.Run("description history is sanitized, other history is untouched", func(t *testing.T) {
		row := dto.WorkspaceTransferRow{"field": "description", "old_value": importedUnsafeHTML, "new_value": importedUnsafeHTML}
		prepareImportedRow("issue_history", row)
		require.Equal(t, sanitize.SanitizeHTML(importedUnsafeHTML), row["old_value"])
		requireImportedTextIsInert(t, transferString(row["new_value"]))

		status := dto.WorkspaceTransferRow{"field": "status", "old_value": "a < b", "new_value": "done"}
		prepareImportedRow("issue_history", status)
		require.Equal(t, "a < b", status["old_value"])
	})
}

func TestWorkspaceImportSanitizesRichText(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	db, err := sqlx.Connect("pgx", databaseURL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	userID := uuid.New()
	workspaceID := uuid.New()
	teamID := uuid.New()
	statusID := uuid.New()
	issueID := uuid.New()
	suffix := strings.ReplaceAll(uuid.New().String(), "-", "")[:8]
	sourceSlug := "sanitize-source-" + suffix
	targetSlug := "sanitize-target-" + suffix

	_, err = db.ExecContext(ctx, `INSERT INTO users(id,email,name,display_name,password_hash) VALUES($1,$2,'Import User','Import User','unusable')`, userID, "import-"+userID.String()+"@example.test")
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id=$1`, userID) })
	_, err = db.ExecContext(ctx, `INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Sanitize Source',$2,$3)`, workspaceID, sourceSlug, userID)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM workspaces WHERE slug IN ($1,$2)`, sourceSlug, targetSlug) })
	_, err = db.ExecContext(ctx, `INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'owner')`, workspaceID, userID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Engineering','ENG')`, teamID, workspaceID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO team_statuses(id,team_id,name,slug,category,position,is_default) VALUES($1,$2,'Backlog','backlog','backlog',0,true)`, statusID, teamID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO issues(id,workspace_id,team_id,number,identifier_text,title,description,status,priority,creator_id,status_id) VALUES($1,$2,$3,1,'ENG-1','Source issue','<p>Source</p>','backlog',0,$4,$5)`, issueID, workspaceID, teamID, userID, statusID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO comments(id,issue_id,user_id,body) VALUES(gen_random_uuid(),$1,$2,'<p>Source</p>')`, issueID, userID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO projects(id,workspace_id,team_id,name,description,status,sort_order) VALUES(gen_random_uuid(),$1,$2,'Source project','<p>Source</p>','in_progress',1)`, workspaceID, teamID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO issue_templates(id,workspace_id,team_id,title,description,status,priority,created_by) VALUES(gen_random_uuid(),$1,$2,'Source template','<p>Source</p>','backlog',0,$3)`, workspaceID, teamID, userID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO issue_history(id,issue_id,user_id,field,old_value,new_value) VALUES(gen_random_uuid(),$1,$2,'description','<p>Older</p>','<p>Source</p>')`, issueID, userID)
	require.NoError(t, err)

	backend, err := storage.NewLocalBackend(t.TempDir(), "")
	require.NoError(t, err)
	transferRepo := repository.NewWorkspaceTransferRepository(db)
	transferService := NewWorkspaceTransferService(transferRepo, backend)
	// The export lock and all archive reads must share one connection.
	db.SetMaxOpenConns(1)
	exportCtx, cancelExport := context.WithTimeout(ctx, 10*time.Second)
	defer cancelExport()
	archive, err := transferService.Export(exportCtx, &domain.Workspace{ID: workspaceID, Name: "Sanitize Source", Slug: sourceSlug, OwnerID: userID}, userID)
	db.SetMaxOpenConns(0)
	require.NoError(t, err)
	defer os.Remove(archive.Path)

	const title = `Title <img src=x onerror="window.importProbe=1"> &amp; more`
	hostile := mutateWorkspaceArchive(t, archive.Path, func(_ *dto.WorkspaceExportManifest, data *dto.WorkspaceTransferData) {
		data.Tables["issues"][0]["title"] = title
		data.Tables["issues"][0]["description"] = importedUnsafeHTML
		data.Tables["comments"][0]["body"] = importedUnsafeHTML
		data.Tables["projects"][0]["name"] = `Project<script>window.importProbe=1</script>`
		data.Tables["projects"][0]["description"] = importedUnsafeHTML
		data.Tables["issue_templates"][0]["description"] = importedUnsafeHTML
		for _, row := range data.Tables["issue_history"] {
			if transferString(row["field"]) == "description" {
				row["old_value"] = importedUnsafeHTML
				row["new_value"] = importedUnsafeHTML
			}
		}
	})
	_, err = transferService.Import(ctx, hostile, "Sanitize Target", targetSlug, userID)
	require.NoError(t, err)

	var targetWorkspaceID uuid.UUID
	require.NoError(t, db.Get(&targetWorkspaceID, `SELECT id FROM workspaces WHERE slug=$1`, targetSlug))

	var issueTitle, issueDescription, commentBody, projectName, projectDescription, templateDescription string
	require.NoError(t, db.QueryRow(`SELECT title, description FROM issues WHERE workspace_id=$1`, targetWorkspaceID).Scan(&issueTitle, &issueDescription))
	require.NoError(t, db.Get(&commentBody, `SELECT c.body FROM comments c JOIN issues i ON i.id=c.issue_id WHERE i.workspace_id=$1`, targetWorkspaceID))
	require.NoError(t, db.QueryRow(`SELECT name, description FROM projects WHERE workspace_id=$1`, targetWorkspaceID).Scan(&projectName, &projectDescription))
	require.NoError(t, db.Get(&templateDescription, `SELECT description FROM issue_templates WHERE workspace_id=$1`, targetWorkspaceID))

	// Imported content is what the normal write paths store for the same input.
	require.Equal(t, sanitize.PlainText(title), issueTitle)
	require.Equal(t, sanitize.SanitizeEditorContent(importedUnsafeHTML), issueDescription)
	require.Equal(t, sanitize.SanitizeHTML(importedUnsafeHTML), commentBody)
	require.Equal(t, "Project", projectName)
	require.Equal(t, sanitize.SanitizeHTML(importedUnsafeHTML), projectDescription)
	for _, stored := range []string{issueDescription, commentBody, projectDescription, templateDescription} {
		requireImportedTextIsInert(t, stored)
		require.Contains(t, stored, "Kept text")
	}

	var history []struct {
		OldValue *string `db:"old_value"`
		NewValue *string `db:"new_value"`
	}
	require.NoError(t, db.Select(&history, `SELECT h.old_value, h.new_value FROM issue_history h JOIN issues i ON i.id=h.issue_id WHERE i.workspace_id=$1 AND h.field='description'`, targetWorkspaceID))
	require.NotEmpty(t, history)
	for _, entry := range history {
		require.NotNil(t, entry.OldValue)
		require.NotNil(t, entry.NewValue)
		requireImportedTextIsInert(t, *entry.OldValue)
		requireImportedTextIsInert(t, *entry.NewValue)
	}
}
