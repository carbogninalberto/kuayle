package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
)

type mergeMethodFake struct {
	set     []string
	setErr  error
	methods map[string]string
}

func (f *mergeMethodFake) MergeMethodSettings(context.Context, uuid.UUID) (map[string]string, error) {
	return f.methods, nil
}
func (f *mergeMethodFake) SetMergeMethod(_ context.Context, _ uuid.UUID, repository, method string) error {
	f.set = append(f.set, repository+"="+method)
	return f.setErr
}

func mergeMethodRequest(h echo.HandlerFunc, method, body string) *httptest.ResponseRecorder {
	e := echo.New()
	request := httptest.NewRequest(method, "/", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	c := e.NewContext(request, recorder)
	c.Set("workspace", &domain.Workspace{ID: uuid.New()})
	_ = h(c)
	return recorder
}

func TestMergeMethodSettingsAreValidatedAndScopedToLinkedRepositories(t *testing.T) {
	fake := &mergeMethodFake{methods: map[string]string{"acme/app": "rebase"}}
	h := NewMergeMethodHandler(fake)

	response := mergeMethodRequest(h.List, http.MethodGet, "")
	require.Equal(t, http.StatusOK, response.Code)
	require.Contains(t, response.Body.String(), `"acme/app":"rebase"`)

	for _, method := range []string{"squash", "merge", "rebase", ""} {
		response = mergeMethodRequest(h.Update, http.MethodPut, `{"repository":"acme/app","merge_method":"`+method+`"}`)
		require.Equal(t, http.StatusOK, response.Code, method)
	}
	require.Equal(t, []string{"acme/app=squash", "acme/app=merge", "acme/app=rebase", "acme/app="}, fake.set)

	fake.set = nil
	for _, body := range []string{`{"repository":"acme/app","merge_method":"fast-forward"}`, `{"merge_method":"squash"}`, `not json`} {
		response = mergeMethodRequest(h.Update, http.MethodPut, body)
		require.Equal(t, http.StatusBadRequest, response.Code, body)
	}
	require.Empty(t, fake.set, "an invalid request stores nothing")

	fake.setErr = service.ErrRepositoryNotLinked
	response = mergeMethodRequest(h.Update, http.MethodPut, `{"repository":"other/repo","merge_method":"squash"}`)
	require.Equal(t, http.StatusNotFound, response.Code)
}
