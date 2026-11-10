package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/realtime"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
)

// agentStoreStub refuses or accepts every assignment change.
type agentStoreStub struct {
	setErr error
	sets   [][]uuid.UUID
	agent  domain.IssueAgentAssignee
}

func (s *agentStoreStub) Identities(context.Context, uuid.UUID) ([]domain.AgentIdentity, error) {
	return []domain.AgentIdentity{{ID: s.agent.ID, Name: s.agent.Name, TeamIDs: []uuid.UUID{}}}, nil
}
func (s *agentStoreStub) UpdateIdentity(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, *string, *[]uuid.UUID) error {
	return s.setErr
}
func (s *agentStoreStub) ForIssues(_ context.Context, ids []uuid.UUID) (map[uuid.UUID][]domain.IssueAgentAssignee, error) {
	if len(s.sets) == 0 {
		return map[uuid.UUID][]domain.IssueAgentAssignee{}, nil
	}
	return map[uuid.UUID][]domain.IssueAgentAssignee{ids[0]: {s.agent}}, nil
}
func (s *agentStoreStub) Set(_ context.Context, _, _, _ uuid.UUID, profiles []uuid.UUID) ([]repository.AgentAssignment, []repository.AgentAssignment, error) {
	if s.setErr != nil {
		return nil, nil, s.setErr
	}
	s.sets = append(s.sets, profiles)
	return nil, nil, nil
}
func (s *agentStoreStub) Assignable(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (*domain.AgentProfile, error) {
	return nil, repository.ErrAgentNotAssignable
}
func (s *agentStoreStub) Pending(context.Context, int) ([]repository.AgentAssignment, error) {
	return nil, nil
}
func (s *agentStoreStub) Finish(context.Context, repository.AgentAssignment, *uuid.UUID, string) (bool, error) {
	return true, nil
}
func (s *agentStoreStub) ExpirePending(context.Context) ([]repository.AgentAssignment, error) {
	return nil, nil
}
func (s *agentStoreStub) MentionedAgents(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, []uuid.UUID) ([]domain.AgentProfile, error) {
	return nil, nil
}
func (s *agentStoreStub) OpenAgentTask(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (*domain.AgentTask, error) {
	return nil, nil
}
func (s *agentStoreStub) AgentNames(context.Context, uuid.UUID, []uuid.UUID) (map[uuid.UUID]string, error) {
	return nil, nil
}

func agentAssigneeFixture(store *agentStoreStub) (*AgentAssigneeHandler, *domain.Workspace, uuid.UUID) {
	issueRepo := newTestIssueRepo()
	ws := &domain.Workspace{ID: uuid.New(), Name: "Test", Slug: "test"}
	issueRepo.issues["ENG-1"] = &domain.Issue{ID: uuid.New(), WorkspaceID: ws.ID, Identifier: "ENG-1", Title: "Flaky import", TeamID: uuid.New()}
	hub := realtime.NewHub()
	issues := service.NewIssueService(issueRepo, newTestTeamRepo(), &testTeamStatusRepo{}, &testHistoryRepo{}, hub, service.NewNotificationService(&testNotifRepo{}))
	return NewAgentAssigneeHandler(service.NewAgentAssigneeService(store, nil, nil, nil, nil, hub), issues), ws, uuid.New()
}

func setIssueAgents(h *AgentAssigneeHandler, ws *domain.Workspace, user uuid.UUID, identifier, body string) (int, string) {
	c, rec := setupIssueContext(echo.New(), http.MethodPut, "/api/workspaces/test/issues/"+identifier+"/agents", body)
	c.Set("workspace", ws)
	c.SetPathValues(echo.PathValues{{Name: "identifier", Value: identifier}})
	c.SetRequest(c.Request().WithContext(domain.WithAccess(c.Request().Context(), domain.Access{WorkspaceID: ws.ID, UserID: user})))
	_ = h.SetIssueAgents(c)
	return rec.Code, rec.Body.String()
}

func TestSettingIssueAgentsMapsRefusalsToClientErrors(t *testing.T) {
	agent := uuid.New()
	body := `{"agent_ids":["` + agent.String() + `"]}`
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{repository.ErrAgentAssignForbidden, http.StatusForbidden, "AGENT_ASSIGN_FORBIDDEN"},
		{repository.ErrAgentNotAssignable, http.StatusForbidden, "AGENT_NOT_ASSIGNABLE"},
		{repository.ErrAgentIssueNotFound, http.StatusNotFound, "NOT_FOUND"},
	} {
		h, ws, user := agentAssigneeFixture(&agentStoreStub{setErr: tc.err})
		status, response := setIssueAgents(h, ws, user, "ENG-1", body)
		assert.Equal(t, tc.status, status, tc.code)
		assert.Contains(t, response, tc.code)
	}

	store := &agentStoreStub{agent: domain.IssueAgentAssignee{ID: agent, Name: "Fixer"}}
	h, ws, user := agentAssigneeFixture(store)
	status, _ := setIssueAgents(h, ws, user, "ENG-1", `{"agent_ids":["not-an-id"]}`)
	assert.Equal(t, http.StatusBadRequest, status)
	status, _ = setIssueAgents(h, ws, user, "ENG-404", body)
	assert.Equal(t, http.StatusNotFound, status)
	assert.Empty(t, store.sets, "nothing is written for a bad ID or an unknown issue")

	status, response := setIssueAgents(h, ws, user, "ENG-1", body)
	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, [][]uuid.UUID{{agent}}, store.sets)
	assert.Contains(t, response, `"name":"Fixer"`)
}

func TestAgentListIsEmptyForGuests(t *testing.T) {
	h, ws, _ := agentAssigneeFixture(&agentStoreStub{agent: domain.IssueAgentAssignee{ID: uuid.New(), Name: "Fixer"}})
	for role, want := range map[string]string{domain.RoleGuest: `"agents":[]`, domain.RoleMember: `"name":"Fixer"`} {
		c, rec := setupIssueContext(echo.New(), http.MethodGet, "/api/workspaces/test/agents", "")
		c.Set("workspace", ws)
		c.Set("workspace_role", role)
		assert.NoError(t, h.List(c))
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), want, role)
	}
}
