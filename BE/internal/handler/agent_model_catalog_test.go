package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/stretchr/testify/require"
)

func getModelCatalog(t *testing.T, h *DevMachineHandler, query string, workspaceID uuid.UUID) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	ctx := devMachineHandlerContext(httptest.NewRequest(http.MethodGet, "/agent-model-catalog"+query, nil), recorder, workspaceID, uuid.New())
	ctx.Set("workspace_role", domain.RoleMember)
	require.NoError(t, h.ModelCatalog(ctx))
	return recorder
}

// A member reads the models of the workspace's model servers for a coding
// agent. The answer names models only: no key, URL or server.
func TestModelCatalogListsWorkspaceModelsWithoutServerDetails(t *testing.T) {
	workspaceID := uuid.New()
	base := "https://models.example.test/v1"
	// Stands in for the stored key: it must not appear in any answer.
	sealed := "fixture-sealed-value"
	store := &handlerDevMachineStoreFake{modelServers: []domain.AgentModelServer{{
		ID: uuid.New(), WorkspaceID: workspaceID, Name: "Gateway", OpenAIBaseURL: &base, EncryptedAPIKey: sealed,
		Models: json.RawMessage(`["gpt-5.5","gpt-5.5-mini"]`), DefaultModel: "gpt-5.5", Thinking: "high", IsDefault: true,
	}}}
	h := modelServerHandler(store)

	listed := getModelCatalog(t, h, "?harness=codex", workspaceID)
	require.Equal(t, http.StatusOK, listed.Code, listed.Body.String())
	var catalog dto.AgentModelCatalogResponse
	require.NoError(t, json.Unmarshal(listed.Body.Bytes(), &catalog))
	require.Equal(t, "codex", catalog.Harness)
	require.Equal(t, "workspace", catalog.Source)
	require.Equal(t, []dto.AgentCatalogModel{{ID: "gpt-5.5", Name: "gpt-5.5", Efforts: []string{}}, {ID: "gpt-5.5-mini", Name: "gpt-5.5-mini", Efforts: []string{}}}, catalog.Models)
	require.NotContains(t, listed.Body.String(), sealed)
	require.NotContains(t, listed.Body.String(), "models.example.test")
	require.NotContains(t, listed.Body.String(), "Gateway")

	// The server speaks OpenAI's protocol only: nothing is known for Claude Code.
	for _, query := range []string{"?harness=claude-code", "?harness=qwen-code", "?harness=https://attacker.example.test/models", ""} {
		empty := getModelCatalog(t, h, query, workspaceID)
		require.Equal(t, http.StatusOK, empty.Code, query)
		var body struct {
			Models      []dto.AgentCatalogModel `json:"models"`
			Unavailable bool                    `json:"unavailable"`
		}
		require.NoError(t, json.Unmarshal(empty.Body.Bytes(), &body))
		require.NotNil(t, body.Models, "models is always a list")
		require.Empty(t, body.Models)
		require.False(t, body.Unavailable)
	}
}
