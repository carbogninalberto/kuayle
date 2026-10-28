package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/service"
	cryptoutil "github.com/kuayle/kuayle-backend/pkg/crypto"
	"github.com/stretchr/testify/require"
)

// signInHandlerStore keeps tokens per workspace and user.
type signInHandlerStore struct {
	repository.DevMachineStore
	tokens map[string]string
}

func (f *signInHandlerStore) key(ws, user uuid.UUID) string { return ws.String() + "/" + user.String() }
func (f *signInHandlerStore) ListAgentSignIns(_ context.Context, ws, user uuid.UUID) ([]domain.AgentSignIn, error) {
	if _, ok := f.tokens[f.key(ws, user)]; !ok {
		return nil, nil
	}
	expires := time.Now().Add(time.Hour)
	return []domain.AgentSignIn{{Provider: "claude-code", Kind: domain.AgentSignInToken, UpdatedAt: time.Now(), ExpiresAt: &expires}}, nil
}
func (f *signInHandlerStore) SaveAgentSignInToken(_ context.Context, ws, user uuid.UUID, _, encrypted string, _ time.Time) error {
	if f.tokens == nil {
		f.tokens = map[string]string{}
	}
	f.tokens[f.key(ws, user)] = encrypted
	return nil
}
func (f *signInHandlerStore) DeleteAgentSignIns(_ context.Context, ws, user uuid.UUID, _, _ string) (int64, error) {
	delete(f.tokens, f.key(ws, user))
	return 1, nil
}

func sendSignIn(t *testing.T, h *DevMachineHandler, method, provider, query, body, role string, ws, user uuid.UUID) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, "/agent-sign-ins"+query, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	ctx := devMachineHandlerContext(request, recorder, ws, user)
	ctx.Set("workspace_role", role)
	if provider != "" {
		ctx.SetParamNames("provider")
		ctx.SetParamValues(provider)
	}
	switch method {
	case http.MethodGet:
		require.NoError(t, h.ListAgentSignIns(ctx))
	case http.MethodPut:
		require.NoError(t, h.SaveAgentSignInToken(ctx))
	case http.MethodDelete:
		require.NoError(t, h.DeleteAgentSignIn(ctx))
	}
	return recorder
}

func TestAgentSignInsBelongToTheCallerAndNeverExposeTheToken(t *testing.T) {
	ws, member, admin := uuid.New(), uuid.New(), uuid.New()
	store := &signInHandlerStore{}
	h := NewDevMachineHandler(service.NewDevMachineService(store, agent.NewRegistry(), true, "machines.example.test", cryptoutil.DeriveKey("test"), time.Minute, service.DevMachineImages{}))
	const token = "sk-ant-oat01-handler-fixture-token-0001"

	saved := sendSignIn(t, h, http.MethodPut, "claude-code", "", `{"token":"`+token+`"}`, domain.RoleMember, ws, member)
	require.Equal(t, http.StatusOK, saved.Code, saved.Body.String())
	require.NotContains(t, saved.Body.String(), token)
	require.NotContains(t, saved.Body.String(), store.tokens[store.key(ws, member)])
	var status dto.AgentSignInResponse
	require.NoError(t, json.Unmarshal(saved.Body.Bytes(), &status))
	require.NotNil(t, status.Token)

	listed := sendSignIn(t, h, http.MethodGet, "", "", "", domain.RoleMember, ws, member)
	require.Equal(t, http.StatusOK, listed.Code)
	require.NotContains(t, listed.Body.String(), token)
	var signIns []dto.AgentSignInResponse
	require.NoError(t, json.Unmarshal(listed.Body.Bytes(), &signIns))
	require.NotNil(t, signIns[0].Token)

	// An admin sees and removes only their own sign-ins, never the member's.
	adminView := sendSignIn(t, h, http.MethodGet, "", "", "", domain.RoleAdmin, ws, admin)
	require.NoError(t, json.Unmarshal(adminView.Body.Bytes(), &signIns))
	require.Nil(t, signIns[0].Token)
	require.Equal(t, http.StatusNoContent, sendSignIn(t, h, http.MethodDelete, "claude-code", "", "", domain.RoleAdmin, ws, admin).Code)
	require.Contains(t, store.tokens, store.key(ws, member))

	require.Equal(t, http.StatusBadRequest, sendSignIn(t, h, http.MethodPut, "claude-code", "", `{"token":"has spaces in it and is long"}`, domain.RoleMember, ws, member).Code)
	require.Equal(t, http.StatusBadRequest, sendSignIn(t, h, http.MethodPut, "codex", "", `{"token":"`+token+`"}`, domain.RoleMember, ws, member).Code)
	require.Equal(t, http.StatusBadRequest, sendSignIn(t, h, http.MethodDelete, "claude-code", "?kind=password", "", domain.RoleMember, ws, member).Code)
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		require.Equal(t, http.StatusForbidden, sendSignIn(t, h, method, "claude-code", "", `{"token":"`+token+`"}`, domain.RoleGuest, ws, uuid.New()).Code, "guests run no agents: "+method)
	}

	require.Equal(t, http.StatusNoContent, sendSignIn(t, h, http.MethodDelete, "claude-code", "?kind=token", "", domain.RoleMember, ws, member).Code)
	require.NotContains(t, store.tokens, store.key(ws, member))
}
