package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
)

func TestMachineErrorMapsCheckoutEligibilityConflict(t *testing.T) {
	e := echo.New()
	recorder := httptest.NewRecorder()
	ctx := e.NewContext(httptest.NewRequest(http.MethodPost, "/checkouts", nil), recorder)

	err := machineError(ctx, fmt.Errorf("%w: issue has no development repository", service.ErrCheckoutNotEligible))

	require.NoError(t, err)
	require.Equal(t, http.StatusConflict, recorder.Code)
	var response dto.ErrorResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, "CHECKOUT_NOT_ELIGIBLE", response.Error.Code)
	require.Contains(t, response.Error.Message, "no development repository")
}

func TestAgentRunTraceRejectsInvalidRunID(t *testing.T) {
	e := echo.New()
	recorder := httptest.NewRecorder()
	ctx := e.NewContext(httptest.NewRequest(http.MethodGet, "/agent-runs/not-a-uuid/trace", nil), recorder)
	ctx.SetPath("/agent-runs/:agentRunId/trace")
	ctx.SetParamNames("agentRunId")
	ctx.SetParamValues("not-a-uuid")

	h := &DevMachineHandler{service: nil}
	err := h.AgentRunTrace(ctx)

	require.NoError(t, err)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	var response dto.ErrorResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, "BAD_REQUEST", response.Error.Code)
}

func TestAgentRunTraceDefaultsQueryParams(t *testing.T) {
	e := echo.New()
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/agent-runs/00000000-0000-0000-0000-000000000001/trace", nil)
	ctx := e.NewContext(req, recorder)
	ctx.SetPath("/agent-runs/:agentRunId/trace")
	ctx.SetParamNames("agentRunId")
	ctx.SetParamValues("00000000-0000-0000-0000-000000000001")

	var params dto.TraceListParams
	require.NoError(t, ctx.Bind(&params))
	params.Defaults()
	require.Equal(t, 200, params.EventsLimit)
	require.Equal(t, 500, params.LogsLimit)
	require.Equal(t, int64(0), params.EventsAfterID)
	require.Equal(t, int64(0), params.LogsAfterID)
}
