package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/handler"
	mw "github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/service"
	jwtpkg "github.com/kuayle/kuayle-backend/pkg/jwt"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
)

// filesSnapshot serves one archive as the commit GitHub resolves any ref to.
type filesSnapshot struct {
	archive []byte
	commit  string
	calls   atomic.Int32
}

func (f *filesSnapshot) RepositorySnapshot(context.Context, uuid.UUID, string, string) (*service.RepositorySnapshot, error) {
	f.calls.Add(1)
	return &service.RepositorySnapshot{Commit: f.commit, Archive: io.NopCloser(bytes.NewReader(f.archive))}, nil
}

func filesArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		require.NoError(t, tw.WriteHeader(&tar.Header{Name: "acme-app-sha/" + name, Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(body))}))
		_, err := tw.Write([]byte(body))
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	return buf.Bytes()
}

// A task's files are its creator's: both routes are session-only, behind
// the demo guard, never cached, and closed to every other person, in or out
// of the workspace. What they serve is what the published head tracks.
func TestTaskFilesHTTP(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := sqlx.Connect("pgx", url)
	require.NoError(t, err)
	defer db.Close()
	creator, owner, member, outsider := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	ws, otherWS, team, issue, machine := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{creator, owner, member, outsider} {
		_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Files HTTP','hash')`, id, id.String()+"@test.invalid")
		require.NoError(t, err)
		defer db.Exec(`DELETE FROM users WHERE id=$1`, id)
	}
	for id, head := range map[uuid.UUID]uuid.UUID{ws: owner, otherWS: outsider} {
		_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Files HTTP',$2,$3)`, id, id.String(), head)
		require.NoError(t, err)
		defer db.Exec(`DELETE FROM workspaces WHERE id=$1`, id)
	}
	for _, m := range []struct {
		ws, user uuid.UUID
		role     string
	}{{ws, owner, "owner"}, {ws, creator, "member"}, {ws, member, "member"}, {otherWS, outsider, "owner"}, {otherWS, creator, "member"}} {
		_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,$3)`, m.ws, m.user, m.role)
		require.NoError(t, err)
	}
	_, err = db.Exec(`INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Files HTTP','FIL')`, team, ws)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO team_statuses(team_id,name,slug,category) VALUES($1,'Todo','todo','unstarted')`, team)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO issues(id,workspace_id,team_id,number,identifier_text,title,creator_id,status_id) VALUES($1,$2,$3,1,'FIL-1','Files',$4,(SELECT id FROM team_statuses WHERE team_id=$3 LIMIT 1))`, issue, ws, team, creator)
	require.NoError(t, err)
	ctx := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: ws, UserID: creator})
	chat, err := repository.NewConversationRepository(db).OpenIssue(ctx, ws, issue)
	require.NoError(t, err)
	machines := repository.NewDevMachineRepository(db)
	taskRepo := repository.NewAgentTaskRepository(db)
	task, err := taskRepo.Create(ctx, ws, chat.ID, domain.AgentTaskRequest{RequestID: uuid.New(), Prompt: "Files", Provider: "codex"})
	require.NoError(t, err)
	// The task's attempt published its branch: the head is that commit.
	head := strings.Repeat("a", 40)
	_, err = db.Exec(`INSERT INTO dev_machines(id,workspace_id,created_by_user_id,routing_key,name,status,desired_status,generation,repo_url,repo_provider,repo_owner,repo_name,base_branch,working_branch,machine_size,cpu_millis,memory_mb,disk_gb)
 VALUES($1,$2,$3,$4,'files-http','running','running',1,'','github','','','','','small',1000,2048,20)`, machine, ws, creator, strings.ReplaceAll(machine.String(), "-", "")[:16])
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO dev_machine_agent_runs(id,workspace_id,machine_id,task_id,requested_by_user_id,provider_id,mode,prompt,command_argv,max_runtime_seconds,status,completed_at,changed_files)
 VALUES($1,$2,$3,$4,$5,'codex','autonomous','Files','["codex"]',60,'succeeded',now(),'["src/app.go"]')`, task.RunID, ws, machine, task.ID, creator)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO dev_machine_git_refs(workspace_id,machine_id,agent_run_id,issue_id,ref_type,repository_full_name,ref_name,commit_sha,pull_request_number,url,source)
 VALUES($1,$2,$3,$4,'commit','acme/app','kuayle/fil-1',$5,NULL,NULL,'published'),($1,$2,$3,$4,'pull_request','acme/app',NULL,NULL,7,'https://github.com/acme/app/pull/7','published')`, ws, machine, task.RunID, issue, head)
	require.NoError(t, err)

	tokens := repository.NewPersonalAccessTokenRepository(db)
	secret := "files-http-jwt-secret-with-at-least-32-characters"
	middleware := testMiddleware(mw.Auth(secret, tokens))
	middleware.workspaceMembership = mw.WorkspaceMembership(repository.NewWorkspaceRepository(db), mw.SSONotEnforced)
	var demoDenied atomic.Bool
	middleware.devMachineDemoGuard = mw.DevMachineDemoGuard(func(uuid.UUID) bool { return !demoDenied.Load() })
	machineSvc := service.NewDevMachineService(machines, agent.NewRegistry(agent.NewCodexProvider("test")), true, "machines.test.invalid", make([]byte, 32), time.Minute, service.DevMachineImages{})
	snapshots := &filesSnapshot{commit: head, archive: filesArchive(t, map[string]string{
		".gitignore": ".env\n",
		".env":       "LOCAL_ONLY=1\n",
		"src/app.go": "package app\n\nfunc App() {}\n",
		"logo.png":   "PNG\x00\x01",
	})}
	checkouts := service.NewRepositoryCheckouts(snapshots)
	defer checkouts.Close()
	tasks := service.NewAgentTaskService(taskRepo, machineSvc, nil)
	tasks.SetTaskFiles(checkouts, machines)
	e := echo.New()
	registerRoutes(e, &appHandlers{agentTask: handler.NewAgentTaskHandler(tasks)}, middleware)
	server := httptest.NewServer(e)
	defer server.Close()
	tokenFor := func(id uuid.UUID) string {
		raw, err := jwtpkg.GenerateAccessToken(id, secret)
		require.NoError(t, err)
		return raw
	}
	type errorBody struct {
		Error struct{ Code, Message string }
	}
	get := func(path, auth string, status int, target any) http.Header {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, server.URL+"/api/workspaces/"+path, nil)
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer "+auth)
		res, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer res.Body.Close()
		require.Equal(t, status, res.StatusCode, path)
		if target != nil {
			require.NoError(t, json.NewDecoder(res.Body).Decode(target))
		}
		return res.Header
	}
	root := ws.String() + "/agent-tasks/" + task.ID.String() + "/files"
	content := root + "/content?path=src/app.go&ref=" + head
	token := tokenFor(creator)

	var listed domain.TaskFiles
	headers := get(root, token, 200, &listed)
	require.Equal(t, "private, no-store", headers.Get("Cache-Control"))
	require.Equal(t, head, listed.Commit)
	require.Equal(t, []domain.TaskFile{{Path: ".gitignore"}, {Path: "logo.png"}, {Path: "src/app.go", Changed: true}}, listed.Files, "the ignored .env is not listed")
	var page domain.TaskFileContent
	headers = get(content+"&start=3&end=3", token, 200, &page)
	require.Equal(t, "private, no-store", headers.Get("Cache-Control"))
	require.Equal(t, domain.TaskFileContent{Path: "src/app.go", Commit: head, Content: "func App() {}", Start: 3, End: 3, TotalLines: 3}, page)
	get(root+"/content?path=logo.png", token, 200, &page)
	require.True(t, page.Binary)
	require.Empty(t, page.Content)

	var refusal errorBody
	for _, path := range []string{".env", "../../etc/passwd", "src/../.env", "%2E%2E/.env", ".git/config"} {
		headers = get(root+"/content?path="+path, token, 404, &refusal)
		require.Equal(t, "FILE_NOT_FOUND", refusal.Error.Code, path)
		require.Equal(t, "private, no-store", headers.Get("Cache-Control"))
	}
	get(root+"/content", token, 400, nil)
	get(root+"/content?path=src/app.go&start=x", token, 400, nil)
	get(root+"?ref=main", token, 400, &refusal)
	require.Equal(t, "INVALID_REF", refusal.Error.Code)
	get(root+"?ref="+strings.Repeat("b", 40), token, 400, &refusal)
	require.Equal(t, "INVALID_REF", refusal.Error.Code, "a commit of no attempt of the task")

	// Anyone else gets nothing: the workspace's owner, another member, a
	// person outside it, and the creator or another owner through another
	// workspace.
	for _, other := range []struct {
		path, auth string
		status     int
	}{
		{root, tokenFor(owner), 404},
		{root, tokenFor(member), 404},
		{root, tokenFor(outsider), 403},
		{strings.Replace(root, ws.String(), otherWS.String(), 1), token, 404},
		{strings.Replace(root, ws.String(), otherWS.String(), 1), tokenFor(outsider), 404},
	} {
		get(other.path, other.auth, other.status, nil)
		get(other.path+"/content?path=src/app.go", other.auth, other.status, nil)
	}
	// Tokens never reach them, whatever their scopes.
	pat, hash, prefix, err := domain.GenerateToken()
	require.NoError(t, err)
	require.NoError(t, tokens.Create(context.Background(), &domain.PersonalAccessToken{ID: uuid.New(), UserID: creator, Name: "Files", Scopes: []string{domain.PermAgentTasksRead, "dev_machine:manage"}, TokenHash: hash, TokenPrefix: prefix}))
	get(root, pat, 403, nil)
	get(content, pat, 403, nil)
	// The demo guard covers them like every task route.
	demoDenied.Store(true)
	get(root, token, 403, nil)
	get(content, token, 403, nil)
	demoDenied.Store(false)
	require.Equal(t, int32(1), snapshots.calls.Load(), "one download, shared by the creator's reads; none for anyone else")
}
