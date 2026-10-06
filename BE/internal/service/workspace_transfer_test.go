package service

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/pkg/storage"
	"github.com/stretchr/testify/require"
)

func TestSanitizeExportRowRemovesSecretsAndDisablesIntegrations(t *testing.T) {
	tests := []struct {
		table   string
		row     dto.WorkspaceTransferRow
		removed string
	}{
		{"webhooks", dto.WorkspaceTransferRow{"secret": "secret", "is_active": true}, "secret"},
		{"github_installations", dto.WorkspaceTransferRow{"access_token": "token", "token_expires_at": "later"}, "access_token"},
		{"ai_settings", dto.WorkspaceTransferRow{"api_key_encrypted": "ciphertext"}, "api_key_encrypted"},
		{"assets", dto.WorkspaceTransferRow{"storage_key": "private-key"}, "storage_key"},
		{"shared_links", dto.WorkspaceTransferRow{"token": "bearer", "is_active": true}, "token"},
	}
	for _, test := range tests {
		t.Run(test.table, func(t *testing.T) {
			sanitizeExportRow(test.table, test.row)
			_, exists := test.row[test.removed]
			require.False(t, exists)
		})
	}
}

func TestRemapTransferValueRewritesIDsNestedJSONAndAssetURLs(t *testing.T) {
	oldID := uuid.New().String()
	newID := uuid.New().String()
	oldAsset := uuid.New().String()
	newAsset := uuid.New().String()
	row := dto.WorkspaceTransferRow{
		"issue_id": oldID,
		"filters":  map[string]any{"issue": oldID},
		"body":     `<img src="/api/workspaces/old/assets/` + oldAsset + `">`,
	}
	result := remapTransferValue(row, map[string]string{oldID: newID}, "old", "new", map[string]string{oldAsset: newAsset}).(dto.WorkspaceTransferRow)
	require.Equal(t, newID, result["issue_id"])
	require.Equal(t, newID, result["filters"].(map[string]any)["issue"])
	require.Contains(t, result["body"], "/api/workspaces/new/assets/"+newAsset)
}

func TestPrepareIdentityMapIncludesGitHubRepositoryReferences(t *testing.T) {
	workspaceID := uuid.New()
	ownerID := uuid.New()
	repoID := uuid.New()
	prID := uuid.New()
	branchID := uuid.New()
	commitID := uuid.New()
	scopeSettingID := uuid.New()

	data := dto.WorkspaceTransferData{
		Workspace: dto.WorkspaceTransferRow{
			"id":       workspaceID.String(),
			"owner_id": ownerID.String(),
		},
		Tables: make(map[string][]dto.WorkspaceTransferRow),
	}
	for _, spec := range repository.WorkspaceTransferTableSpecs() {
		data.Tables[spec.Name] = []dto.WorkspaceTransferRow{}
	}
	data.Tables["github_repos"] = []dto.WorkspaceTransferRow{{"id": repoID.String(), "workspace_id": workspaceID.String()}}
	data.Tables["github_pull_requests"] = []dto.WorkspaceTransferRow{{"id": prID.String(), "workspace_id": workspaceID.String(), "github_repo_id": repoID.String()}}
	data.Tables["github_branches"] = []dto.WorkspaceTransferRow{{"id": branchID.String(), "workspace_id": workspaceID.String(), "github_repo_id": repoID.String()}}
	data.Tables["github_commits"] = []dto.WorkspaceTransferRow{{"id": commitID.String(), "workspace_id": workspaceID.String(), "github_repo_id": repoID.String(), "pr_id": prID.String()}}
	data.Tables["dev_machine_scope_settings"] = []dto.WorkspaceTransferRow{{"id": scopeSettingID.String(), "workspace_id": workspaceID.String(), "github_repo_id": repoID.String()}}

	parsed := &parsedWorkspaceArchive{
		manifest: dto.WorkspaceExportManifest{SourceWorkspaceID: workspaceID.String()},
		data:     data,
	}
	mapping, _, err := prepareIdentityMap(parsed, uuid.New(), map[string]string{ownerID.String(): uuid.New().String()})
	require.NoError(t, err)
	require.NoError(t, validateArchiveReferences(parsed, mapping))

	for _, oldID := range []uuid.UUID{repoID, prID, branchID, commitID, scopeSettingID} {
		newID := mapping[oldID.String()]
		require.NotEmpty(t, newID, oldID.String())
		require.NotEqual(t, oldID.String(), newID, oldID.String())
	}
}

func TestValidateArchiveReferencesStillRejectsUnknownNonSharedLinkReferences(t *testing.T) {
	workspaceID := uuid.New()
	ownerID := uuid.New()
	issueID := uuid.New()
	missingTeamID := uuid.New()
	parsed := &parsedWorkspaceArchive{
		data: dto.WorkspaceTransferData{
			Workspace: dto.WorkspaceTransferRow{"id": workspaceID.String(), "owner_id": ownerID.String()},
			Tables: map[string][]dto.WorkspaceTransferRow{
				"issues": {{"id": issueID.String(), "workspace_id": workspaceID.String(), "team_id": missingTeamID.String()}},
			},
		},
	}

	err := validateArchiveReferences(parsed, map[string]string{
		workspaceID.String(): workspaceID.String(),
		ownerID.String():     ownerID.String(),
		issueID.String():     issueID.String(),
	})
	require.ErrorIs(t, err, ErrInvalidWorkspaceArchive)
	require.ErrorContains(t, err, "issues.team_id references data outside the archive")
}

func TestParseWorkspaceArchiveRejectsUnsafeAndDuplicateEntries(t *testing.T) {
	for name, entries := range map[string][]string{
		"unsafe":    {"manifest.json", "data.json", "../escape"},
		"duplicate": {"manifest.json", "data.json", "data.json"},
		"unknown":   {"manifest.json", "data.json", "commands.sql"},
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "archive.zip")
			file, err := os.Create(path)
			require.NoError(t, err)
			writer := zip.NewWriter(file)
			for _, entryName := range entries {
				entry, createErr := writer.Create(entryName)
				require.NoError(t, createErr)
				_, _ = entry.Write([]byte(`{}`))
			}
			require.NoError(t, writer.Close())
			require.NoError(t, file.Close())
			_, err = parseWorkspaceArchive(path)
			require.ErrorIs(t, err, ErrInvalidWorkspaceArchive)
		})
	}
}

func TestWorkspaceTransferRoundTrip(t *testing.T) {
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
	labelID := uuid.New()
	issueID := uuid.New()
	commentID := uuid.New()
	assetID := uuid.New()
	projectID := uuid.New()
	cycleID := uuid.New()
	issue2ID := uuid.New()
	groupID := uuid.New()
	githubInstallationID := uuid.New()
	githubRepoID := uuid.New()
	githubPRID := uuid.New()
	validTeamSharedLinkID := uuid.New()
	orphanedTeamSharedLinkID := uuid.New()
	orphanedTeamID := uuid.New()
	validTeamSharedLinkToken := randomTransferToken(32)
	sourceSlug := "transfer-source-" + strings.ReplaceAll(uuid.New().String(), "-", "")[:8]
	targetSlug := "transfer-target-" + strings.ReplaceAll(uuid.New().String(), "-", "")[:8]
	memoryTargetSlug := "transfer-memory-" + strings.ReplaceAll(uuid.New().String(), "-", "")[:8]
	foreignWorkspaceID := uuid.New()
	foreignSlug := "transfer-foreign-" + strings.ReplaceAll(uuid.New().String(), "-", "")[:8]

	_, err = db.ExecContext(ctx, `INSERT INTO users(id,email,name,display_name,password_hash) VALUES($1,$2,'Transfer User','Transfer User','unusable')`, userID, "transfer-"+userID.String()+"@example.test")
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id=$1`, userID) })
	_, err = db.ExecContext(ctx, `INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Transfer Source',$2,$3)`, workspaceID, sourceSlug, userID)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM workspaces WHERE slug IN ($1,$2,$3,$4)`, sourceSlug, targetSlug, memoryTargetSlug, foreignSlug)
	})
	_, err = db.ExecContext(ctx, `INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'owner')`, workspaceID, userID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Foreign Workspace',$2,$3)`, foreignWorkspaceID, foreignSlug, userID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO teams(id,workspace_id,name,key) VALUES(gen_random_uuid(),$1,'Must Not Export','NOPE')`, foreignWorkspaceID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Engineering','ENG')`, teamID, workspaceID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO team_statuses(id,team_id,name,slug,category,position,is_default) VALUES($1,$2,'Backlog','backlog','backlog',0,true)`, statusID, teamID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO labels(id,workspace_id,name,color) VALUES($1,$2,'Portable','#123456')`, labelID, workspaceID)
	require.NoError(t, err)
	description := `<p>Asset</p><img src="/api/workspaces/` + sourceSlug + `/assets/` + assetID.String() + `">`
	_, err = db.ExecContext(ctx, `INSERT INTO issues(id,workspace_id,team_id,number,identifier_text,title,description,status,priority,creator_id,status_id) VALUES($1,$2,$3,1,'ENG-1','Portable issue',$4,'backlog',0,$5,$6)`, issueID, workspaceID, teamID, description, userID, statusID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO issue_labels(issue_id,label_id) VALUES($1,$2)`, issueID, labelID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO comments(id,issue_id,user_id,body) VALUES($1,$2,$3,'Portable comment')`, commentID, issueID, userID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO team_members(team_id,user_id) VALUES($1,$2)`, teamID, userID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO projects(id,workspace_id,team_id,name,status,lead_id,start_date,target_date,sort_order) VALUES($1,$2,$3,'Portable project','in_progress',$4,'2026-01-01','2026-02-01',4.5)`, projectID, workspaceID, teamID, userID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO project_members(project_id,user_id) VALUES($1,$2)`, projectID, userID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO project_status_visibility(project_id,status_id) VALUES($1,$2)`, projectID, statusID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO cycles(id,team_id,name,number,start_date,end_date,status,description,goals,retrospective) VALUES($1,$2,'Portable cycle',1,'2026-01-01','2026-01-14','active','Cycle description','Cycle goals','Cycle retro')`, cycleID, teamID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `UPDATE issues SET project_id=$1,cycle_id=$2 WHERE id=$3`, projectID, cycleID, issueID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO issues(id,workspace_id,team_id,number,identifier_text,title,status,priority,creator_id,status_id,parent_id) VALUES($1,$2,$3,2,'ENG-2','Portable child','backlog',1,$4,$5,$6)`, issue2ID, workspaceID, teamID, userID, statusID, issueID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO issue_assignees(issue_id,user_id) VALUES($1,$2)`, issueID, userID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO issue_subscribers(issue_id,user_id) VALUES($1,$2)`, issueID, userID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO issue_relations(id,issue_id,related_issue_id,type) VALUES(gen_random_uuid(),$1,$2,'related')`, issueID, issue2ID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO comments(id,issue_id,user_id,body,parent_id,resolved_at) VALUES(gen_random_uuid(),$1,$2,'Portable reply',$3,NOW())`, issueID, userID, commentID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO issue_history(id,issue_id,user_id,field,old_value,new_value) VALUES(gen_random_uuid(),$1,$2,'priority','0','1')`, issueID, userID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO issue_templates(id,workspace_id,team_id,title,description,status,priority,assignee_id,label_ids,recurrence_rule,next_run_at,is_active,created_by) VALUES(gen_random_uuid(),$1,$2,'Portable template','Template body','backlog',2,$3,jsonb_build_array($4::text),'{"frequency":"weekly"}',NOW()+INTERVAL '1 day',true,$3)`, workspaceID, teamID, userID, labelID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO issue_groups(id,workspace_id,name,description) VALUES($1,$2,'Portable group','Group description')`, groupID, workspaceID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO issue_group_items(group_id,issue_id,position) VALUES($1,$2,3)`, groupID, issueID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO views(id,workspace_id,creator_id,name,description,filters,is_shared) VALUES(gen_random_uuid(),$1,$2,'Portable view','View description',jsonb_build_object('team',$3::text,'labels',jsonb_build_array($4::text)),true)`, workspaceID, userID, teamID, labelID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO favorites(id,workspace_id,user_id,entity_type,entity_id,position) VALUES(gen_random_uuid(),$1,$2,'project',$3,2)`, workspaceID, userID, projectID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO shared_links(id,token,workspace_id,created_by,scope,scope_id,filters,include_description,is_active) VALUES(gen_random_uuid(),$1,$2,$3,'project',$4,'{}',true,true)`, randomTransferToken(32), workspaceID, userID, projectID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO shared_links(id,token,workspace_id,created_by,scope,scope_id,filters,include_description,is_active) VALUES($1,$2,$3,$4,'team',$5,'{}',true,true)`, validTeamSharedLinkID, validTeamSharedLinkToken, workspaceID, userID, teamID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO shared_links(id,token,workspace_id,created_by,scope,scope_id,filters,include_description,is_active) VALUES($1,$2,$3,$4,'team',$5,'{}',true,true)`, orphanedTeamSharedLinkID, randomTransferToken(32), workspaceID, userID, orphanedTeamID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO notifications(id,user_id,workspace_id,issue_id,type,title,source_team_ids) VALUES(gen_random_uuid(),$1,$2,$3,'issue_updated','Portable notification',ARRAY[$4::uuid])`, userID, workspaceID, issueID, teamID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO webhooks(id,workspace_id,url,secret,events,is_active) VALUES(gen_random_uuid(),$1,'https://example.test/hook','top-secret',ARRAY['issue.created','issue.updated'],true)`, workspaceID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO github_installations(id,workspace_id,installation_id,account_login,account_type,access_token,installed_by) VALUES($1,$2,987654321,'portable-org','Organization','secret-token',$3)`, githubInstallationID, workspaceID, userID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO github_repos(id,installation_id,workspace_id,github_repo_id,full_name,default_branch,is_active) VALUES($1,$2,$3,123456789,'portable/repo','main',true)`, githubRepoID, githubInstallationID, workspaceID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO github_pull_requests(id,workspace_id,issue_id,github_repo_id,github_pr_id,number,title,state,author_login,html_url) VALUES($1,$2,$3,$4,111,7,'Portable PR','open','developer','https://github.com/portable/repo/pull/7')`, githubPRID, workspaceID, issueID, githubRepoID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO github_branches(id,workspace_id,issue_id,github_repo_id,name,html_url) VALUES(gen_random_uuid(),$1,$2,$3,'eng-1','https://github.com/portable/repo/tree/eng-1')`, workspaceID, issueID, githubRepoID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO github_commits(id,workspace_id,issue_id,github_repo_id,pr_id,sha,message,author_login,html_url,committed_at) VALUES(gen_random_uuid(),$1,$2,$3,$4,'0123456789012345678901234567890123456789','Portable commit','developer','https://github.com/portable/repo/commit/0123',NOW())`, workspaceID, issueID, githubRepoID, githubPRID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO github_auto_transitions(id,workspace_id,event,target_status,target_status_id,is_active) VALUES(gen_random_uuid(),$1,'pr_merged','done',$2,true)`, workspaceID, statusID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO ai_settings(workspace_id,provider,base_url,model,api_key_encrypted,description_expand_prompt,issue_copy_prompt) VALUES($1,'openai_compatible','https://ai.example.test','portable-model','encrypted-secret','Expand prompt','Copy prompt')`, workspaceID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO dev_machine_workspace_policies(workspace_id,enabled,allowed_providers,allowed_repositories) VALUES($1,true,'["codex"]','["portable/repo"]')`, workspaceID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO dev_machine_scope_settings(id,workspace_id,team_id,github_repo_id,base_branch) VALUES(gen_random_uuid(),$1,$2,$3,'main')`, workspaceID, teamID, githubRepoID)
	require.NoError(t, err)

	storageDir := t.TempDir()
	backend, err := storage.NewLocalBackend(storageDir, "")
	require.NoError(t, err)
	assetKey := assetID.String() + ".txt"
	assetBody := "portable asset bytes"
	_, err = backend.Put(ctx, assetKey, strings.NewReader(assetBody), "text/plain")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO assets(id,workspace_id,storage_key,filename,content_type,size,uploaded_by,team_id) VALUES($1,$2,$3,'portable.txt','text/plain',$4,$5,$6)`, assetID, workspaceID, assetKey, len(assetBody), userID, teamID)
	require.NoError(t, err)

	transferRepo := repository.NewWorkspaceTransferRepository(db)
	transferService := NewWorkspaceTransferService(transferRepo, backend)
	// The export lock and all archive reads must share one connection.
	db.SetMaxOpenConns(1)
	exportCtx, cancelExport := context.WithTimeout(ctx, 10*time.Second)
	defer cancelExport()
	archive, err := transferService.Export(exportCtx, &domain.Workspace{ID: workspaceID, Name: "Transfer Source", Slug: sourceSlug, OwnerID: userID}, userID)
	db.SetMaxOpenConns(0)
	require.NoError(t, err)
	defer os.Remove(archive.Path)
	exported, err := parseWorkspaceArchive(archive.Path)
	require.NoError(t, err)
	require.NotContains(t, exported.data.Tables["webhooks"][0], "secret")
	require.NotContains(t, exported.data.Tables["github_installations"][0], "access_token")
	require.NotContains(t, exported.data.Tables["ai_settings"][0], "api_key_encrypted")
	require.NotContains(t, exported.data.Users[0], "password_hash")
	require.Contains(t, exported.manifest.Omitted, "refresh_tokens")
	require.Len(t, exported.data.Tables["teams"], 1, "rows from another workspace must not enter the archive")
	require.Len(t, exported.data.Tables["shared_links"], 2, "only links with valid scope targets should be exported")
	require.Contains(t, exported.manifest.Warnings, "Omitted 1 shared link with a missing scope target.")
	for _, row := range exported.data.Tables["shared_links"] {
		require.NotEqual(t, orphanedTeamSharedLinkID.String(), transferString(row["id"]))
		require.NotContains(t, row, "token")
	}
	require.NoError(t, exported.zip.Close())

	result, err := transferService.Import(ctx, archive.Path, "Transfer Target", targetSlug, userID)
	require.NoError(t, err)
	require.Equal(t, targetSlug, result.Slug)

	var targetWorkspaceID uuid.UUID
	require.NoError(t, db.Get(&targetWorkspaceID, `SELECT id FROM workspaces WHERE slug=$1`, targetSlug))
	var importedDescription string
	require.NoError(t, db.Get(&importedDescription, `SELECT description FROM issues WHERE workspace_id=$1 AND identifier_text='ENG-1'`, targetWorkspaceID))
	require.Contains(t, importedDescription, "/api/workspaces/"+targetSlug+"/assets/")
	require.NotContains(t, importedDescription, assetID.String())
	var importedAssetKey string
	require.NoError(t, db.Get(&importedAssetKey, `SELECT storage_key FROM assets WHERE workspace_id=$1`, targetWorkspaceID))
	rc, err := backend.Get(ctx, importedAssetKey)
	require.NoError(t, err)
	bytes, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.NoError(t, rc.Close())
	require.Equal(t, assetBody, string(bytes))

	memoryBackend := &memoryTransferBackend{objects: make(map[string][]byte)}
	memoryResult, err := NewWorkspaceTransferService(transferRepo, memoryBackend).Import(ctx, archive.Path, "Memory Target", memoryTargetSlug, userID)
	require.NoError(t, err)
	require.Equal(t, 1, memoryResult.Counts["assets"])
	require.Len(t, memoryBackend.objects, 1)
	for _, object := range memoryBackend.objects {
		require.Equal(t, assetBody, string(object))
	}

	var counts map[string]int
	encoded, _ := json.Marshal(result.Counts)
	require.NoError(t, json.Unmarshal(encoded, &counts))
	require.Equal(t, 2, counts["issues"])
	require.Equal(t, 2, counts["comments"])
	require.Equal(t, 1, counts["assets"])
	require.Equal(t, 1, counts["webhooks"])
	require.Equal(t, 1, counts["github_commits"])
	require.Equal(t, 2, counts["shared_links"])
	var importedWebhookActive, importedDevMachinesEnabled bool
	var importedWebhookSecret string
	require.NoError(t, db.QueryRow(`SELECT is_active,secret FROM webhooks WHERE workspace_id=$1`, targetWorkspaceID).Scan(&importedWebhookActive, &importedWebhookSecret))
	require.False(t, importedWebhookActive)
	require.NotEqual(t, "top-secret", importedWebhookSecret)
	require.NoError(t, db.Get(&importedDevMachinesEnabled, `SELECT enabled FROM dev_machine_workspace_policies WHERE workspace_id=$1`, targetWorkspaceID))
	require.False(t, importedDevMachinesEnabled)
	var importedAISecret *string
	require.NoError(t, db.Get(&importedAISecret, `SELECT api_key_encrypted FROM ai_settings WHERE workspace_id=$1`, targetWorkspaceID))
	require.Nil(t, importedAISecret)

	var importedRepoID, importedInstallationID uuid.UUID
	var importedRepoActive bool
	require.NoError(t, db.QueryRow(`
		SELECT r.id, r.installation_id, r.is_active
		FROM github_repos r
		WHERE r.workspace_id=$1 AND r.full_name='portable/repo'
	`, targetWorkspaceID).Scan(&importedRepoID, &importedInstallationID, &importedRepoActive))
	require.NotEqual(t, githubRepoID, importedRepoID)
	require.False(t, importedRepoActive)

	var importedInstallationWorkspaceID uuid.UUID
	var importedAccessToken *string
	require.NoError(t, db.QueryRow(`
		SELECT workspace_id, access_token
		FROM github_installations
		WHERE id=$1
	`, importedInstallationID).Scan(&importedInstallationWorkspaceID, &importedAccessToken))
	require.Equal(t, targetWorkspaceID, importedInstallationWorkspaceID)
	require.Nil(t, importedAccessToken)

	var importedPRID, importedPRRepoID uuid.UUID
	require.NoError(t, db.QueryRow(`
		SELECT id, github_repo_id
		FROM github_pull_requests
		WHERE workspace_id=$1 AND github_pr_id=111
	`, targetWorkspaceID).Scan(&importedPRID, &importedPRRepoID))
	require.NotEqual(t, githubPRID, importedPRID)
	require.Equal(t, importedRepoID, importedPRRepoID)

	var importedBranchRepoID, importedCommitRepoID, importedCommitPRID uuid.UUID
	require.NoError(t, db.QueryRow(`
		SELECT github_repo_id
		FROM github_branches
		WHERE workspace_id=$1 AND name='eng-1'
	`, targetWorkspaceID).Scan(&importedBranchRepoID))
	require.NoError(t, db.QueryRow(`
		SELECT github_repo_id, pr_id
		FROM github_commits
		WHERE workspace_id=$1 AND sha='0123456789012345678901234567890123456789'
	`, targetWorkspaceID).Scan(&importedCommitRepoID, &importedCommitPRID))
	require.Equal(t, importedRepoID, importedBranchRepoID)
	require.Equal(t, importedRepoID, importedCommitRepoID)
	require.Equal(t, importedPRID, importedCommitPRID)

	var importedTeamID uuid.UUID
	require.NoError(t, db.Get(&importedTeamID, `SELECT id FROM teams WHERE workspace_id=$1 AND key='ENG'`, targetWorkspaceID))
	var importedValidTeamSharedLinkID, importedSharedLinkScopeID uuid.UUID
	var importedSharedLinkActive bool
	var importedSharedLinkToken string
	require.NoError(t, db.QueryRow(`
		SELECT id, scope_id, is_active, token
		FROM shared_links
		WHERE workspace_id=$1 AND scope='team'
	`, targetWorkspaceID).Scan(&importedValidTeamSharedLinkID, &importedSharedLinkScopeID, &importedSharedLinkActive, &importedSharedLinkToken))
	require.NotEqual(t, validTeamSharedLinkID, importedValidTeamSharedLinkID)
	require.Equal(t, importedTeamID, importedSharedLinkScopeID)
	require.False(t, importedSharedLinkActive)
	require.NotEmpty(t, importedSharedLinkToken)
	require.NotEqual(t, validTeamSharedLinkToken, importedSharedLinkToken)
	var importedAssetTeam, importedNotificationTeam uuid.UUID
	require.NoError(t, db.Get(&importedAssetTeam, `SELECT team_id FROM assets WHERE workspace_id=$1`, targetWorkspaceID))
	require.Equal(t, importedTeamID, importedAssetTeam)
	require.NoError(t, db.Get(&importedNotificationTeam, `SELECT source_team_ids[1] FROM notifications WHERE workspace_id=$1`, targetWorkspaceID))
	require.Equal(t, importedTeamID, importedNotificationTeam)
	privateArchive := mutateWorkspaceArchive(t, archive.Path, func(_ *dto.WorkspaceExportManifest, data *dto.WorkspaceTransferData) {
		data.Tables["teams"][0]["is_private"] = true
	})
	_, err = transferService.Preview(ctx, privateArchive, userID)
	require.ErrorIs(t, err, ErrUnsupportedArchive)
	_, err = transferService.Import(ctx, privateArchive, "Private import", "private-import-"+uuid.NewString()[:8], userID)
	require.ErrorIs(t, err, ErrUnsupportedArchive)
	var importedScopeRepoID uuid.UUID
	require.NoError(t, db.Get(&importedScopeRepoID, `SELECT github_repo_id FROM dev_machine_scope_settings WHERE workspace_id=$1 AND team_id=$2`, targetWorkspaceID, importedTeamID))
	require.Equal(t, importedRepoID, importedScopeRepoID)

	var importedTransitionStatusID, importedTeamStatusID uuid.UUID
	require.NoError(t, db.Get(&importedTransitionStatusID, `SELECT target_status_id FROM github_auto_transitions WHERE workspace_id=$1 AND event='pr_merged'`, targetWorkspaceID))
	require.NoError(t, db.Get(&importedTeamStatusID, `SELECT id FROM team_statuses WHERE team_id=$1 AND slug='backlog'`, importedTeamID))
	require.Equal(t, importedTeamStatusID, importedTransitionStatusID)

	_, err = transferService.Import(ctx, archive.Path, "Conflict", targetSlug, userID)
	require.ErrorIs(t, err, ErrWorkspaceImportSlug)

	rollbackSlug := "transfer-rollback-" + strings.ReplaceAll(uuid.New().String(), "-", "")[:8]
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM workspaces WHERE slug=$1`, rollbackSlug) })
	rollbackArchive := mutateWorkspaceArchive(t, archive.Path, func(manifest *dto.WorkspaceExportManifest, data *dto.WorkspaceTransferData) {
		duplicate := cloneTransferRow(data.Tables["teams"][0])
		duplicate["id"] = uuid.New().String()
		data.Tables["teams"] = append(data.Tables["teams"], duplicate)
		manifest.Counts["teams"]++
	})
	filesBefore := regularFileCount(t, storageDir)
	_, err = transferService.Import(ctx, rollbackArchive, "Rollback", rollbackSlug, userID)
	require.Error(t, err)
	var rollbackCount int
	require.NoError(t, db.Get(&rollbackCount, `SELECT COUNT(*) FROM workspaces WHERE slug=$1`, rollbackSlug))
	require.Zero(t, rollbackCount)
	require.Equal(t, filesBefore, regularFileCount(t, storageDir), "staged asset must be removed after transaction rollback")

	missingSlug := "transfer-missing-" + strings.ReplaceAll(uuid.New().String(), "-", "")[:8]
	missingEmail := "missing-" + uuid.New().String() + "@example.test"
	missingArchive := mutateWorkspaceArchive(t, archive.Path, func(manifest *dto.WorkspaceExportManifest, data *dto.WorkspaceTransferData) {
		missingID := uuid.New().String()
		data.Users = append(data.Users, dto.WorkspaceTransferRow{"id": missingID, "email": missingEmail, "name": "Missing", "display_name": "Missing", "avatar_url": nil})
		data.Tables["workspace_members"] = append(data.Tables["workspace_members"], dto.WorkspaceTransferRow{
			"workspace_id": manifest.SourceWorkspaceID, "user_id": missingID, "role": "member", "created_at": time.Now().UTC(),
		})
		manifest.Counts["users"]++
		manifest.Counts["workspace_members"]++
	})
	preview, err := transferService.Preview(ctx, missingArchive, userID)
	require.NoError(t, err)
	require.Equal(t, []string{missingEmail}, preview.MissingUsers)
	_, err = transferService.Import(ctx, missingArchive, "Missing", missingSlug, userID)
	require.ErrorIs(t, err, ErrWorkspaceImportUsers)

	require.NoError(t, backend.Delete(ctx, assetKey))
	_, err = transferService.Export(ctx, &domain.Workspace{ID: workspaceID, Name: "Transfer Source", Slug: sourceSlug, OwnerID: userID}, userID)
	require.Error(t, err)
}

type memoryTransferBackend struct {
	objects map[string][]byte
}

func (b *memoryTransferBackend) Put(_ context.Context, key string, reader io.Reader, _ string) (int64, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return 0, err
	}
	b.objects[key] = append([]byte(nil), data...)
	return int64(len(data)), nil
}

func (b *memoryTransferBackend) Get(_ context.Context, key string) (io.ReadCloser, error) {
	data, ok := b.objects[key]
	if !ok {
		return nil, os.ErrNotExist
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (b *memoryTransferBackend) Delete(_ context.Context, key string) error {
	delete(b.objects, key)
	return nil
}

func (b *memoryTransferBackend) URL(_ context.Context, key string) (string, error) {
	return "memory://" + key, nil
}

func mutateWorkspaceArchive(t *testing.T, sourcePath string, mutate func(*dto.WorkspaceExportManifest, *dto.WorkspaceTransferData)) string {
	t.Helper()
	parsed, err := parseWorkspaceArchive(sourcePath)
	require.NoError(t, err)
	defer parsed.zip.Close()
	mutate(&parsed.manifest, &parsed.data)
	path := filepath.Join(t.TempDir(), "mutated.kuayle.zip")
	file, err := os.Create(path)
	require.NoError(t, err)
	writer := zip.NewWriter(file)
	require.NoError(t, writeZIPJSON(writer, "manifest.json", parsed.manifest))
	require.NoError(t, writeZIPJSON(writer, "data.json", parsed.data))
	for name, entry := range parsed.entries {
		if !strings.HasPrefix(name, "assets/") {
			continue
		}
		source, openErr := entry.Open()
		require.NoError(t, openErr)
		target, createErr := writer.Create(name)
		require.NoError(t, createErr)
		_, copyErr := io.Copy(target, source)
		require.NoError(t, copyErr)
		require.NoError(t, source.Close())
	}
	require.NoError(t, writer.Close())
	require.NoError(t, file.Close())
	return path
}

func regularFileCount(t *testing.T, root string) int {
	t.Helper()
	count := 0
	require.NoError(t, filepath.WalkDir(root, func(_ string, entry os.DirEntry, err error) error {
		if err == nil && entry.Type().IsRegular() {
			count++
		}
		return err
	}))
	return count
}

func TestPrivateArchiveVisibilityMustBeExplicitBooleanFalse(t *testing.T) {
	for _, value := range []any{true, "false", nil, map[string]any{}, []any{}, json.Number("0")} {
		parsed := &parsedWorkspaceArchive{data: dto.WorkspaceTransferData{Workspace: dto.WorkspaceTransferRow{}, Tables: map[string][]dto.WorkspaceTransferRow{"teams": {{"is_private": value}}}}}
		require.ErrorIs(t, validateWorkspaceArchive(parsed), ErrUnsupportedArchive)
	}
}
