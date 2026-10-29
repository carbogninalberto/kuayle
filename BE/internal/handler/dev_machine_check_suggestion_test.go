package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/internal/config"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/service"
	cryptoutil "github.com/kuayle/kuayle-backend/pkg/crypto"
	"github.com/stretchr/testify/require"
)

type checkSuggestionStoreFake struct {
	handlerDevMachineStoreFake
	repository *domain.GitHubRepoModel
	workspace  *domain.DevMachineScopeSetting
}

func (f *checkSuggestionStoreFake) GetScopeSetting(_ context.Context, _ uuid.UUID, team, project, issue *uuid.UUID) (*domain.DevMachineScopeSetting, error) {
	if team == nil && project == nil && issue == nil {
		return f.workspace, nil
	}
	return nil, nil
}

func (f *checkSuggestionStoreFake) GetLinkedRepository(_ context.Context, workspaceID, id uuid.UUID) (*domain.GitHubRepoModel, error) {
	if f.repository != nil && f.repository.ID == id && f.repository.WorkspaceID == workspaceID {
		return f.repository, nil
	}
	return nil, nil
}

type manifestReaderFake struct {
	paths []string
	files map[string][]byte
	err   error
	refs  []string
}

func (f *manifestReaderFake) RepositoryPaths(_ context.Context, _ uuid.UUID, fullName, ref string) ([]string, error) {
	f.refs = append(f.refs, fullName+"@"+ref)
	return f.paths, f.err
}

func (f *manifestReaderFake) RepositoryFiles(_ context.Context, _ uuid.UUID, _, _ string, paths []string) (map[string][]byte, error) {
	files := map[string][]byte{}
	for _, path := range paths {
		files[path] = f.files[path]
	}
	return files, nil
}

func TestCheckSuggestionReadsTheLinkedRepository(t *testing.T) {
	workspaceID := uuid.New()
	repository := &domain.GitHubRepoModel{ID: uuid.New(), WorkspaceID: workspaceID, FullName: "acme/app", DefaultBranch: "trunk", IsActive: true}
	store := &checkSuggestionStoreFake{handlerDevMachineStoreFake: handlerDevMachineStoreFake{policy: testPolicyForHandler(workspaceID)}, repository: repository}
	machines := service.NewDevMachineService(store, agent.NewRegistry(), true, "machines.example.test", cryptoutil.DeriveKey("test"), time.Minute, service.DevMachineImages{})
	reader := &manifestReaderFake{paths: []string{"BE/go.mod", "UI/package.json", "UI/package-lock.json"}, files: map[string][]byte{"UI/package.json": []byte(`{"scripts":{"check":"svelte-check"}}`)}}
	h := NewDevMachineHandler(machines, config.DiskQuotaModeHard)
	get := func(role, query string) *httptest.ResponseRecorder {
		t.Helper()
		recorder := httptest.NewRecorder()
		ctx := devMachineHandlerContext(httptest.NewRequest(http.MethodGet, "/dev-machine-check-suggestion"+query, nil), recorder, workspaceID, uuid.New())
		ctx.Set("workspace_role", role)
		require.NoError(t, h.CheckSuggestion(ctx))
		return recorder
	}
	query := "?repository_id=" + repository.ID.String()
	require.Equal(t, http.StatusConflict, get(domain.RoleAdmin, query).Code, "unavailable without a GitHub reader")
	machines.SetRepositoryManifests(reader)

	require.Equal(t, http.StatusForbidden, get(domain.RoleMember, query).Code, "only administrators edit development defaults")
	require.Equal(t, http.StatusBadRequest, get(domain.RoleAdmin, "").Code)
	require.Equal(t, http.StatusBadRequest, get(domain.RoleAdmin, "?repository_id=x").Code)
	require.Equal(t, http.StatusBadRequest, get(domain.RoleAdmin, "?project_id=x").Code)
	require.Equal(t, http.StatusForbidden, get(domain.RoleAdmin, "?repository_id="+uuid.NewString()).Code, "a repository that is not linked to the workspace is refused")
	require.Empty(t, reader.refs)

	recorder := get(domain.RoleOwner, query)
	require.Equal(t, http.StatusOK, recorder.Code)
	var suggestion service.CheckSuggestion
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &suggestion))
	require.Equal(t, []string{"sh", "-c", "(cd BE && go build ./... && go vet ./...) && (cd UI && npm ci && npm run check)"}, suggestion.Command)
	require.Equal(t, []string{"BE/go.mod", "UI/package.json"}, suggestion.Basis)
	require.Equal(t, []string{"acme/app@trunk"}, reader.refs, "the default branch is read")

	team := "?team_id=" + uuid.NewString()
	require.Equal(t, http.StatusNotFound, get(domain.RoleAdmin, team).Code, "a team inherits no repository without a workspace default")
	store.workspace = &domain.DevMachineScopeSetting{WorkspaceID: workspaceID, GitHubRepoID: &repository.ID}
	require.Equal(t, http.StatusOK, get(domain.RoleAdmin, team).Code, "a team inherits the workspace repository")
	require.Equal(t, http.StatusBadRequest, get(domain.RoleAdmin, "?team_id=x").Code)

	reader.paths = []string{"README.md"}
	recorder = get(domain.RoleAdmin, query)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.JSONEq(t, `null`, recorder.Body.String(), "nothing recognised")

	reader.err = errors.New("GitHub API error 500: internal detail")
	recorder = get(domain.RoleAdmin, query)
	require.Equal(t, http.StatusBadGateway, recorder.Code)
	require.NotContains(t, recorder.Body.String(), "internal detail")
}
