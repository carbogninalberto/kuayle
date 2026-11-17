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
	"github.com/kuayle/kuayle-backend/internal/service"
	cryptoutil "github.com/kuayle/kuayle-backend/pkg/crypto"
	"github.com/stretchr/testify/require"
)

type scopeSettingStoreFake struct {
	*handlerDevMachineStoreFake
	saved []domain.DevMachineScopeSetting
}

func (f *scopeSettingStoreFake) ScopeResourceExists(context.Context, uuid.UUID, string, *uuid.UUID) (bool, error) {
	return true, nil
}

func (f *scopeSettingStoreFake) UpsertScopeSetting(_ context.Context, setting *domain.DevMachineScopeSetting) error {
	f.saved = append(f.saved, *setting)
	return nil
}

// KUA-210: the pull request template is text of at most 8,000 characters
// using only the known placeholders.
func TestUpdateScopeSettingValidatesThePullRequestTemplate(t *testing.T) {
	workspaceID, userID := uuid.New(), uuid.New()
	store := &scopeSettingStoreFake{handlerDevMachineStoreFake: &handlerDevMachineStoreFake{policy: testPolicyForHandler(workspaceID)}}
	h := NewDevMachineHandler(service.NewDevMachineService(store, agent.NewRegistry(), true, "machines.example.test", cryptoutil.DeriveKey("test"), time.Minute, service.DevMachineImages{}))
	send := func(body string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPut, "/dev-machine-scope-setting", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		ctx := devMachineHandlerContext(request, recorder, workspaceID, userID)
		ctx.Set("workspace_role", "admin")
		require.NoError(t, h.UpdateScopeSetting(ctx))
		return recorder
	}

	saved := send(`{"scope_type":"workspace","pull_request_template":"## Summary\n{{description}}\n\n{{issue}}"}`)
	require.Equal(t, http.StatusOK, saved.Code, saved.Body.String())
	var setting domain.DevMachineScopeSetting
	require.NoError(t, json.Unmarshal(saved.Body.Bytes(), &setting))
	require.Equal(t, "## Summary\n{{description}}\n\n{{issue}}", *setting.PullRequestTemplate)
	require.Len(t, store.saved, 1)

	for name, body := range map[string]string{
		"not text":            `{"scope_type":"workspace","pull_request_template":42}`,
		"too long":            `{"scope_type":"workspace","pull_request_template":"` + strings.Repeat("x", domain.PullRequestTemplateMaxRunes+1) + `"}`,
		"unknown placeholder": `{"scope_type":"workspace","pull_request_template":"{{title}}"}`,
	} {
		refused := send(body)
		require.Equal(t, http.StatusBadRequest, refused.Code, name)
		var problem dto.ErrorResponse
		require.NoError(t, json.Unmarshal(refused.Body.Bytes(), &problem))
		require.Equal(t, "BAD_REQUEST", problem.Error.Code, name)
	}
	require.Len(t, store.saved, 1, "nothing invalid is stored")
}
