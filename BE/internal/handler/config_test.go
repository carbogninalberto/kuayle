package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
)

func TestPublicConfigReportsSSOAvailability(t *testing.T) {
	for _, tc := range []struct {
		name    string
		enabled bool
		err     error
		want    bool
	}{
		{name: "enabled", enabled: true, want: true},
		{name: "disabled"},
		{name: "unavailable", err: errors.New("lookup failed")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := NewConfigHandler(true).WithPasskeys(true).
				WithSSOAvailability(func(context.Context) (bool, error) { return tc.enabled, tc.err })
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
			require.NoError(t, h.Get(echo.New().NewContext(req, rec)))
			require.Equal(t, http.StatusOK, rec.Code)
			var body map[string]bool
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			require.Equal(t, map[string]bool{
				"registration_enabled": true, "passkeys_enabled": true,
				"sso_enabled_instance": false, "sso_enabled": tc.want,
			}, body)
		})
	}
}
