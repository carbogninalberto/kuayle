package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/kuayle/kuayle-backend/pkg/crypto"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

func TestSharingPrivacyPostgres(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := sqlx.Connect("pgx", url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	owner, member, ws, team, public := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, user := range []uuid.UUID{owner, member} {
		_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Share privacy','hash')`, user, user.String()+"@example.test")
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id=$1`, user) })
	}
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Share privacy',$2,$3)`, ws, ws.String(), owner)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM workspaces WHERE id=$1`, ws) })
	for user, role := range map[uuid.UUID]string{owner: "owner", member: "member"} {
		_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,$3)`, ws, user, role)
		require.NoError(t, err)
	}
	for i, id := range []uuid.UUID{team, public} {
		key := []string{"PRIVATE", "PUBLIC"}[i]
		_, err = db.Exec(`INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,$3,$3)`, id, ws, key)
		require.NoError(t, err)
		status := uuid.New()
		_, err = db.Exec(`INSERT INTO team_statuses(id,team_id,name,slug,category) VALUES($1,$2,$3,'todo','unstarted')`, status, id, key+" status")
		require.NoError(t, err)
		_, err = db.Exec(`INSERT INTO issues(workspace_id,team_id,number,identifier_text,title,creator_id,status_id) VALUES($1,$2,1,$3,$4,$5,$6)`, ws, id, key+"-1", key+" title", owner, status)
		require.NoError(t, err)
	}
	ownerCtx := domain.WithAccess(context.Background(), domain.Access{UserID: owner, WorkspaceID: ws})
	memberCtx := domain.WithAccess(context.Background(), domain.Access{UserID: member, WorkspaceID: ws})
	links := repository.NewSharedLinkRepository(db)
	views := repository.NewViewRepository(db)
	favorites := repository.NewFavoriteRepository(db)
	svc := NewSharedLinkService(links, repository.NewWorkspaceRepository(db), repository.NewTeamRepository(db), repository.NewProjectRepository(db), views, repository.NewIssueRepository(db), repository.NewUserRepository(db), repository.NewTeamStatusRepository(db), "sharing-test-secret")
	view := &domain.View{ID: uuid.New(), WorkspaceID: ws, CreatorID: owner, Name: "Private scope description", Filters: json.RawMessage(`{}`), IsShared: true}
	require.NoError(t, views.Create(ownerCtx, view))
	makeLink := func(scope string, id *uuid.UUID, filters string) *domain.SharedLink {
		t.Helper()
		var scopeID *string
		if id != nil {
			s := id.String()
			scopeID = &s
		}
		link, err := svc.Create(ownerCtx, ws, owner, dto.CreateSharedLinkRequest{Scope: scope, ScopeID: scopeID, Filters: json.RawMessage(filters)})
		require.NoError(t, err)
		return link
	}
	teamLink := makeLink("team", &team, `{}`)
	publicLink := makeLink("team", &public, `{}`)
	workspaceLink := makeLink("workspace", nil, `{}`)
	filteredLink := makeLink("workspace", nil, `{"search":"private stored text"}`)
	viewLink := makeLink("view", &view.ID, `{}`)
	for _, link := range []*domain.SharedLink{teamLink, publicLink, workspaceLink, filteredLink, viewLink} {
		_, err = svc.GetPublicMeta(context.Background(), link.Token)
		require.NoError(t, err)
	}
	favorite := &domain.Favorite{ID: uuid.New(), WorkspaceID: ws, UserID: member, EntityType: "team", EntityID: team}
	require.NoError(t, favorites.Create(memberCtx, favorite))
	// Exercise real outbound requests before privacy, then prove no private
	// payload reaches the receiver after transition (including admin callers).
	requests := make(chan string, 8)
	releases := make(chan chan struct{}, 8)
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requests <- string(body)
		release := make(chan struct{})
		releases <- release
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"<p>Expanded</p>"}}]}`))
	}))
	defer receiver.Close()
	assertResponsive := func() {
		t.Helper()
		release := <-releases
		defer close(release)
		deadline, cancel := context.WithTimeout(ownerCtx, 750*time.Millisecond)
		defer cancel()
		_, e := db.ExecContext(deadline, `UPDATE issues SET updated_at=NOW() WHERE workspace_id=$1`, ws)
		require.NoError(t, e, "ordinary bulk edit must finish while the provider is waiting")
		_, e = db.ExecContext(deadline, `UPDATE teams SET is_private=TRUE WHERE id=$1`, team)
		require.ErrorContains(t, e, "Publication in progress", "raw SQL must fail closed without waiting in reverse lock order")
	}
	key := crypto.DeriveKey("external-privacy-test")
	encrypted, err := crypto.Encrypt("test-key", key)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO ai_settings(workspace_id,base_url,model,api_key_encrypted) VALUES($1,$2,'test-model',$3)`, ws, receiver.URL, encrypted)
	require.NoError(t, err)
	aiRepo := repository.NewAISettingsRepository(db)
	ai := NewAISettingsService(aiRepo, repository.NewWorkspaceRepository(db), repository.NewIssueRepository(db), key)
	aiResult := make(chan error, 1)
	go func() {
		_, e := ai.ExpandIssueDescription(ownerCtx, ws, "PUBLIC-1", nil)
		aiResult <- e
	}()
	select {
	case body := <-requests:
		require.Contains(t, body, "PUBLIC title")
	case <-time.After(3 * time.Second):
		t.Fatal("public AI request was not delivered")
	}
	assertResponsive()
	require.NoError(t, <-aiResult)
	hookKey := crypto.DeriveKey("external-privacy-test:webhook")
	hookSecret, err := crypto.Encrypt("test-hook-key", hookKey)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO webhooks(workspace_id,url,secret,events) VALUES($1,$2,$3,ARRAY['issue.updated'])`, ws, receiver.URL, hookSecret)
	require.NoError(t, err)
	hookRepo := repository.NewWebhookRepository(db)
	hooks := NewWebhookService(hookRepo, "external-privacy-test")
	hooks.Dispatch(ownerCtx, ws, "issue.updated", map[string]string{"title": "PUBLIC title"})
	select {
	case body := <-requests:
		require.Contains(t, body, "PUBLIC title")
		assertResponsive()
	case <-time.After(3 * time.Second):
		t.Fatal("public webhook was not delivered")
	}
	// Transition must reject even stopped or unscoped machine artifacts; their
	// historical prompts/images cannot safely be assigned to a team.
	for _, artifact := range []struct{ table, insert string }{
		{"dev_machine_scope_settings", `INSERT INTO dev_machine_scope_settings(workspace_id) VALUES($1)`},
		{"dev_machine_environments", `INSERT INTO dev_machine_environments(workspace_id,name,image_ref) VALUES($1,'test','test:image')`},
		{"dev_machines", `INSERT INTO dev_machines(workspace_id,routing_key,name,machine_size,cpu_millis,memory_mb,disk_gb,max_runtime_minutes,status,expires_at) VALUES($1,'` + strings.ReplaceAll(uuid.NewString(), "-", "") + `','test','small',1000,1024,20,60,'stopped',NOW()+INTERVAL '1 hour')`},
	} {
		_, err = db.Exec(artifact.insert, ws)
		require.NoError(t, err)
		_, err = db.Exec(`UPDATE teams SET is_private=TRUE WHERE id=$1`, team)
		require.ErrorContains(t, err, "Remove development machines")
		_, err = db.Exec(`DELETE FROM `+artifact.table+` WHERE workspace_id=$1`, ws)
		require.NoError(t, err)
	}
	_, err = db.Exec(`INSERT INTO dev_machine_workspace_policies(workspace_id,enabled) VALUES($1,TRUE)`, ws)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO github_auto_transitions(workspace_id,event,target_status,target_status_id) SELECT $1,'pr_merged','done',id FROM team_statuses WHERE team_id=$2`, ws, team)
	require.NoError(t, err)
	transferRepo := repository.NewWorkspaceTransferRepository(db)
	guard, err := transferRepo.BeginPublicExport(ownerCtx, ws)
	require.NoError(t, err)
	var transitionAvailable bool
	require.NoError(t, db.QueryRow(`SELECT pg_try_advisory_xact_lock(hashtextextended($1::text,63))`, ws).Scan(&transitionAvailable))
	require.False(t, transitionAvailable, "an export must block concurrent privacy transitions")
	require.NoError(t, guard.Rollback())
	_, err = db.Exec(`UPDATE teams SET is_private=TRUE WHERE id=$1`, team)
	require.NoError(t, err)
	for _, identifier := range []string{"PRIVATE-1", "PUBLIC-1"} {
		_, err = ai.ExpandIssueDescription(ownerCtx, ws, identifier, nil)
		require.ErrorIs(t, err, repository.ErrPrivateWorkspaceOperation)
	}
	hooks.Dispatch(ownerCtx, ws, "issue.updated", map[string]string{"title": "PRIVATE title"})
	_, err = hookRepo.BeginPublicOperation(ownerCtx, ws)
	require.ErrorIs(t, err, repository.ErrPrivateWorkspaceOperation)
	select {
	case body := <-requests:
		t.Fatalf("privacy workspace emitted external content: %s", body)
	case <-time.After(200 * time.Millisecond):
	}
	machines := repository.NewDevMachineRepository(db)
	policy, err := machines.GetPolicy(ownerCtx, ws)
	require.NoError(t, err)
	require.False(t, policy.Enabled)
	_, err = db.Exec(`UPDATE dev_machine_workspace_policies SET enabled=TRUE WHERE workspace_id=$1`, ws)
	require.ErrorContains(t, err, "Development machines are unavailable")
	_, err = db.Exec(`INSERT INTO dev_machine_scope_settings(workspace_id) VALUES($1)`, ws)
	require.ErrorContains(t, err, "Development machines are unavailable")
	_, err = db.Exec(`INSERT INTO dev_machine_environments(workspace_id,name,image_ref) VALUES($1,'test','test:image')`, ws)
	require.ErrorContains(t, err, "Development machines are unavailable")
	var privateIssue uuid.UUID
	require.NoError(t, db.Get(&privateIssue, `SELECT id FROM issues WHERE team_id=$1`, team))
	development, err := machines.GetIssueDevelopmentContext(ownerCtx, ws, privateIssue)
	require.NoError(t, err)
	require.Nil(t, development)
	var transitions int
	require.NoError(t, db.Get(&transitions, `SELECT COUNT(*) FROM github_auto_transitions WHERE workspace_id=$1`, ws))
	require.Zero(t, transitions)
	_, err = db.Exec(`INSERT INTO github_auto_transitions(workspace_id,event,target_status,target_status_id) SELECT $1,'pr_merged','done',id FROM team_statuses WHERE team_id=$2`, ws, team)
	require.ErrorContains(t, err, "public-team status")
	github := &GitHubService{issueRepo: repository.NewIssueRepository(db), ghRepo: repository.NewGitHubRepository(db)}
	_, err = github.GetIssueActivity(ownerCtx, ws, "PRIVATE-1")
	require.Error(t, err)
	_, err = github.GetIssueActivity(ownerCtx, ws, "PUBLIC-1")
	require.NoError(t, err)
	// Tokens, metadata, authenticated management lists and writes all deny hidden scopes.
	for _, link := range []*domain.SharedLink{teamLink, filteredLink, viewLink} {
		_, err = svc.GetPublicMeta(ownerCtx, link.Token)
		require.Error(t, err)
		_, err = svc.ListPublicIssues(ownerCtx, link.Token, dto.IssueFilterParams{})
		require.Error(t, err)
		hidden, e := links.GetByID(memberCtx, link.ID)
		require.NoError(t, e)
		require.Nil(t, hidden)
		require.ErrorIs(t, links.Update(ownerCtx, link), sql.ErrNoRows)
	}
	available, err := links.ListByWorkspace(memberCtx, ws)
	require.NoError(t, err)
	require.Len(t, available, 2)
	for _, link := range []*domain.SharedLink{publicLink, workspaceLink} {
		result, e := svc.ListPublicIssues(ownerCtx, link.Token, dto.IssueFilterParams{})
		require.NoError(t, e)
		require.Equal(t, 1, result.TotalCount)
		require.Len(t, result.Data, 1)
		require.Equal(t, "PUBLIC title", result.Data[0].Title)
		meta, e := svc.GetPublicMeta(ownerCtx, link.Token)
		require.NoError(t, e)
		require.NotContains(t, meta.ScopeName, "PRIVATE")
	}
	// No privilege to publish private content, even for administrators.
	scopeID := team.String()
	_, err = svc.Create(ownerCtx, ws, owner, dto.CreateSharedLinkRequest{Scope: "team", ScopeID: &scopeID})
	require.Error(t, err)
	_, err = svc.Create(ownerCtx, ws, owner, dto.CreateSharedLinkRequest{Scope: "workspace", Filters: json.RawMessage(`{"team":"private"}`)})
	require.ErrorIs(t, err, repository.ErrPrivateWorkspaceOperation, "the sticky restriction must be reported, not a raw no-rows error")
	got, err := views.GetByID(ownerCtx, view.ID)
	require.NoError(t, err)
	require.Nil(t, got)
	list, err := views.ListByWorkspace(ownerCtx, ws, owner)
	require.NoError(t, err)
	require.Empty(t, list)
	require.ErrorIs(t, views.Update(ownerCtx, view), sql.ErrNoRows)
	view.ID = uuid.New()
	require.ErrorIs(t, views.Create(ownerCtx, view), repository.ErrPrivateWorkspaceOperation)
	favs, err := favorites.ListByUser(memberCtx, ws, member)
	require.NoError(t, err)
	require.Empty(t, favs)
	favorite.ID = uuid.New()
	require.ErrorIs(t, favorites.Create(memberCtx, favorite), sql.ErrNoRows)
	_, err = db.Exec(`INSERT INTO team_members(team_id,user_id) VALUES($1,$2)`, team, member)
	require.NoError(t, err)
	favs, err = favorites.ListByUser(memberCtx, ws, member)
	require.NoError(t, err)
	require.Len(t, favs, 1)
	// Caller identity, rather than a supplied user ID, owns favorites.
	favs, err = favorites.ListByUser(ownerCtx, ws, member)
	require.NoError(t, err)
	require.Empty(t, favs)
	_, err = db.Exec(`DELETE FROM team_members WHERE team_id=$1 AND user_id=$2`, team, member)
	require.NoError(t, err)
	favs, err = favorites.ListByUser(memberCtx, ws, member)
	require.NoError(t, err)
	require.Empty(t, favs)
	_, err = db.Exec(`UPDATE teams SET is_private=FALSE WHERE id=$1`, team)
	require.NoError(t, err)
	_, err = svc.GetPublicMeta(ownerCtx, filteredLink.Token)
	require.Error(t, err, "unattributable metadata must stay disabled after declassification")
	archive, err := NewWorkspaceTransferService(transferRepo, nil).Export(ownerCtx, &domain.Workspace{ID: ws}, owner)
	require.ErrorIs(t, err, repository.ErrPrivateWorkspaceTransfer)
	require.Nil(t, archive, "export must stop before raw table or asset reads, even after declassification")
}
