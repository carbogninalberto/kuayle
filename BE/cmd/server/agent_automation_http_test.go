package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/handler"
	mw "github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/realtime"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/service"
	jwtpkg "github.com/kuayle/kuayle-backend/pkg/jwt"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
)

func TestAgentAutomationHTTPRequiresAdminsForRulesAndOwnersForDecisions(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := sqlx.Connect("pgx", url)
	require.NoError(t, err)
	defer db.Close()
	owner, member, ws, team, issue := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{owner, member} {
		_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Automation HTTP','hash')`, id, id.String()+"@test.invalid")
		require.NoError(t, err)
		defer db.Exec(`DELETE FROM users WHERE id=$1`, id)
	}
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Automation HTTP',$2,$3)`, ws, ws.String(), owner)
	require.NoError(t, err)
	defer db.Exec(`DELETE FROM workspaces WHERE id=$1`, ws)
	for id, role := range map[uuid.UUID]string{owner: "owner", member: "member"} {
		_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,$3)`, ws, id, role)
		require.NoError(t, err)
	}
	_, err = db.Exec(`INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Automation','AUTO')`, team, ws)
	require.NoError(t, err)
	var status uuid.UUID
	require.NoError(t, db.Get(&status, `INSERT INTO team_statuses(team_id,name,slug,category) VALUES($1,'Todo','todo','unstarted') RETURNING id`, team))
	_, err = db.Exec(`INSERT INTO issues(id,workspace_id,team_id,number,identifier_text,title,creator_id,status_id) VALUES($1,$2,$3,1,'AUTO-1','Automate',$4,$5)`, issue, ws, team, owner, status)
	require.NoError(t, err)
	ctx := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: ws, UserID: owner})
	machines := repository.NewDevMachineRepository(db)
	require.NoError(t, machines.UpsertPolicy(ctx, &domain.DevMachineWorkspacePolicy{WorkspaceID: ws, Enabled: true, MaxConcurrentMachines: 10, MaxMachinesPerUser: 10, MaxDailyAgentRuns: 25, MaxDiskGB: 100, AllowedProviders: json.RawMessage(`["codex"]`), AllowedRepositories: json.RawMessage(`[]`)}))
	machineSvc := service.NewDevMachineService(machines, agent.NewRegistry(agent.NewCodexProvider("test")), true, "machines.test.invalid", make([]byte, 32), time.Minute, service.DevMachineImages{})
	taskSvc := service.NewAgentTaskService(repository.NewAgentTaskRepository(db), machineSvc, nil)
	automationRepo := repository.NewAgentAutomationRepository(db)
	automations := service.NewAgentAutomationService(automationRepo, repository.NewConversationRepository(db), taskSvc, service.NewNotificationService(repository.NewNotificationRepository(db)), realtime.NewHub())
	secret := "automation-http-jwt-secret-with-at-least-32-chars"
	middleware := testMiddleware(mw.Auth(secret, repository.NewPersonalAccessTokenRepository(db)))
	middleware.workspaceMembership = mw.WorkspaceMembership(repository.NewWorkspaceRepository(db))
	middleware.devMachineDemoGuard = mw.DevMachineDemoGuard(func(uuid.UUID) bool { return true })
	e := echo.New()
	registerRoutes(e, &appHandlers{agentTask: handler.NewAgentTaskHandler(taskSvc), agentAutomation: handler.NewAgentAutomationHandler(automations), devMachine: handler.NewDevMachineHandler(machineSvc)}, middleware)
	server := httptest.NewServer(e)
	defer server.Close()
	request := func(method, path, body string, user uuid.UUID, want int, target any) {
		t.Helper()
		token, err := jwtpkg.GenerateAccessToken(user, secret)
		require.NoError(t, err)
		req, err := http.NewRequest(method, server.URL+"/api/workspaces/"+ws.String()+path, strings.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer res.Body.Close()
		require.Equal(t, want, res.StatusCode)
		if target != nil {
			require.NoError(t, json.NewDecoder(res.Body).Decode(target))
		}
	}
	rule := fmt.Sprintf(`{"team_id":%q,"name":"Todo fixer","status_id":%q,"instructions":"Fix it","max_active":1}`, team, status)
	request("POST", "/agent-automations", rule, member, 403, nil)
	request("GET", "/agent-automations", "", member, 403, nil)
	request("POST", "/agent-automations", strings.Replace(rule, `"Fix it"`, `""`, 1), owner, 400, nil)
	request("POST", "/agent-automations", strings.Replace(rule, `"max_active":1`, `"max_active":1,"owner_id":"`+member.String()+`"`, 1), owner, 400, nil)
	var created domain.AgentAutomation
	request("POST", "/agent-automations", rule, owner, 201, &created)
	require.Equal(t, owner, created.OwnerID, "the creator owns the rule")
	var run uuid.UUID
	require.NoError(t, db.Get(&run, `INSERT INTO agent_automation_runs(automation_id,workspace_id,issue_id,status) VALUES($1,$2,$3,'pending_approval') RETURNING id`, created.ID, ws, issue))
	request("POST", "/agent-automation-runs/"+run.String()+"/approve", "", member, 404, nil)
	request("POST", "/agent-automation-runs/"+run.String()+"/dismiss", "", owner, 200, nil)
	request("PATCH", "/agent-automations/"+created.ID.String(), `{"enabled":false}`, member, 403, nil)
	var disabled domain.AgentAutomation
	request("PATCH", "/agent-automations/"+created.ID.String(), `{"enabled":false}`, owner, 200, &disabled)
	require.False(t, disabled.Enabled)
	request("DELETE", "/agent-automations/"+created.ID.String(), "", owner, 204, nil)
}
