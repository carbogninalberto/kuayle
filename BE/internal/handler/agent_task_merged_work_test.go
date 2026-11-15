package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
)

// KUA-193: a retry of merged work answers 409 MERGED_WORK naming the pull
// request, so the UI can ask and send the request again confirmed.
func TestMergedWorkIsAConflictNamingThePullRequest(t *testing.T) {
	recorder := httptest.NewRecorder()
	c := echo.New().NewContext(httptest.NewRequest(http.MethodPost, "/agent-tasks/x/retry", nil), recorder)
	pr := domain.MergedPullRequest{Number: 12, URL: "https://github.com/acme/app/pull/12", Repository: "acme/app"}
	require.NoError(t, agentTaskError(c, fmt.Errorf("retry: %w", &domain.MergedWorkError{PullRequest: pr})))
	require.Equal(t, http.StatusConflict, recorder.Code)
	var body struct {
		Error struct {
			Code        string                   `json:"code"`
			Message     string                   `json:"message"`
			PullRequest domain.MergedPullRequest `json:"pull_request"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	require.Equal(t, "MERGED_WORK", body.Error.Code)
	require.Equal(t, "This issue's work was merged in #12. Retry anyway?", body.Error.Message)
	require.Equal(t, pr, body.Error.PullRequest)
}

// The confirmation is part of the retry and live-message requests the
// handler decodes, which refuse unknown fields.
func TestRetryAndLiveRequestsCarryTheMergedWorkConfirmation(t *testing.T) {
	id := uuid.New()
	request := httptest.NewRequest(http.MethodPost, "/agent-tasks/x/retry", strings.NewReader(`{"request_id":"`+id.String()+`","generation":2,"prompt":"Again","confirm_merged":true}`))
	c := echo.New().NewContext(request, httptest.NewRecorder())
	var input domain.AgentTaskRetryRequest
	require.NoError(t, bindTask(c, &input))
	require.True(t, input.ConfirmMerged)
	require.Equal(t, id, input.RequestID)

	request = httptest.NewRequest(http.MethodPost, "/agent-tasks/x/live-messages", strings.NewReader(`{"body":"Try again","mode":"start","confirm_merged":true}`))
	c = echo.New().NewContext(request, httptest.NewRecorder())
	var live liveMessageRequest
	require.NoError(t, bindTask(c, &live))
	require.Equal(t, liveMessageRequest{Body: "Try again", Mode: "start", ConfirmMerged: true}, live)
}
