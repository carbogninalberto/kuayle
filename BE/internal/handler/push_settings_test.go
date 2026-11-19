package handler

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/config"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
)

type pushSettingMemory struct {
	value  string
	secret bool
}

func (m *pushSettingMemory) Get(context.Context, string) (string, error) { return m.value, nil }
func (m *pushSettingMemory) Update(_ context.Context, _ string, secret bool, f func(string) (string, error)) error {
	v, err := f(m.value)
	if err == nil {
		m.value = v
		m.secret = secret
	}
	return err
}

type emptyPushDevices struct {
	*repository.PushSubscriptionRepository
}

func (emptyPushDevices) CountByUser(context.Context, uuid.UUID) (int, error) { return 0, nil }

func (emptyPushDevices) ListByUser(context.Context, uuid.UUID) ([]domain.PushSubscription, error) {
	return nil, nil
}

func TestPushSettingsLiveAndEnvironmentPrecedence(t *testing.T) {
	records := &pushSettingMemory{}
	admin := uuid.New()
	newHandler := func(env config.VAPIDConfig) *PushHandler {
		s := service.NewPushService(env, records, emptyPushDevices{}, nil, nil, nil, nil, nil)
		return NewPushHandler(s, func(id uuid.UUID) bool { return id == admin })
	}
	h := newHandler(config.VAPIDConfig{})
	var secrets []string
	call := func(action echo.HandlerFunc, method, body string, code int) *httptest.ResponseRecorder {
		t.Helper()
		rec := callBackup(t, action, method, "/", body, admin)
		require.Equal(t, code, rec.Code, rec.Body.String())
		require.NotContains(t, rec.Body.String(), "private")
		for _, secret := range secrets {
			require.NotContains(t, rec.Body.String(), secret)
		}
		return rec
	}
	status := call(h.Settings, "GET", "", 200)
	require.Contains(t, status.Body.String(), `"source":"off"`)
	require.Contains(t, status.Body.String(), `"subscribed_devices":0`)
	call(h.UpdateSettings, "PUT", `{"subject":"https://example.org","enabled":true}`, 400)
	generated := call(h.GenerateKeys, "POST", `{}`, 200)
	var saved struct {
		PublicKey  string `json:"public_key"`
		PrivateKey string `json:"private_key"`
	}
	require.NoError(t, json.Unmarshal([]byte(records.value), &saved))
	require.True(t, records.secret)
	require.NotEmpty(t, saved.PrivateKey)
	secrets = append(secrets, saved.PrivateKey)
	require.NotContains(t, generated.Body.String(), saved.PrivateKey)
	require.NotEmpty(t, saved.PublicKey)
	call(h.GenerateKeys, "POST", `{}`, 409)
	call(h.UpdateSettings, "PUT", `{"subject":"person@example.org","enabled":true}`, 400)
	call(h.UpdateSettings, "PUT", `{"subject":"https://","enabled":true}`, 400)
	call(h.UpdateSettings, "PUT", `{"subject":"mailto:admin@example.org","enabled":true}`, 200)
	key := call(h.PublicKey, "GET", "", 200)
	require.JSONEq(t, `{"public_key":"`+saved.PublicKey+`"}`, key.Body.String())
	require.Contains(t, key.Header().Get("Cache-Control"), "no-store")
	require.Contains(t, call(h.Settings, "GET", "", 200).Body.String(), `"source":"app"`)
	require.JSONEq(t, `{"sent":0,"failed":0}`, call(h.Test, "POST", "", 200).Body.String())

	private, public, err := webpush.GenerateVAPIDKeys()
	require.NoError(t, err)
	secrets = append(secrets, private)
	env := newHandler(config.VAPIDConfig{PublicKey: public, PrivateKey: private, Subject: "https://example.org"})
	got := call(env.Settings, "GET", "", 200).Body.String()
	require.Contains(t, got, `"source":"environment"`)
	require.Contains(t, got, `"read_only":true`)
	require.Contains(t, got, public)
	call(env.GenerateKeys, "POST", `{"confirm":true}`, 409)
	call(env.UpdateSettings, "PUT", `{"subject":"https://example.org","enabled":false}`, 409)
	require.JSONEq(t, `{"public_key":"`+public+`"}`, call(env.PublicKey, "GET", "", 200).Body.String())

	rotation := call(h.GenerateKeys, "POST", `{"confirm":true}`, 200)
	require.Contains(t, rotation.Body.String(), `"requires_reenrollment":true`)
	require.NotContains(t, rotation.Body.String(), saved.PublicKey)
	call(h.UpdateSettings, "PUT", `{"subject":"https://example.org","enabled":false}`, 200)
	require.JSONEq(t, `{"public_key":""}`, call(h.PublicKey, "GET", "", 200).Body.String())
	call(h.Test, "POST", "", 503)
}
