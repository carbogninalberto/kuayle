package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	cryptoutil "github.com/kuayle/kuayle-backend/pkg/crypto"
	"github.com/stretchr/testify/require"
)

type codexDeviceTestStore struct {
	signInStoreFake
	saved chan string
}

func (s *codexDeviceTestStore) SaveAgentSignIn(ctx context.Context, workspace, user uuid.UUID, provider, encrypted string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.rows = map[string]signInRow{
		signInKey(workspace, user, provider, domain.AgentSignInCredentials): {
			encrypted: encrypted,
			info:      domain.AgentSignIn{Provider: provider, Kind: domain.AgentSignInCredentials, UpdatedAt: time.Now()},
		},
	}
	s.saved <- encrypted
	return nil
}

func TestCodexDeviceLoginWithoutAMachine(t *testing.T) {
	workspace, user := uuid.New(), uuid.New()
	ctx := context.Background()
	store := &codexDeviceTestStore{saved: make(chan string, 1)}
	service := newTestDevMachineService(store)
	result := make(chan []byte, 1)
	service.codexDevices.start = func(ctx context.Context) (codexDeviceLogin, error) {
		return codexDeviceLogin{
			url: "https://auth.openai.com/codex/device", code: "ABCD-1234",
			wait: func() ([]byte, error) {
				select {
				case file := <-result:
					return file, nil
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			},
			close: func() {},
		}, nil
	}

	request, requestDone := context.WithCancel(ctx)
	defer requestDone()
	started, err := service.StartCodexDeviceLogin(request, workspace, user)
	require.NoError(t, err)
	requestDone() // Returning the prompt ends its HTTP request.
	require.Equal(t, "ABCD-1234", started.UserCode)
	require.Equal(t, "https://auth.openai.com/codex/device", started.VerificationURL)

	for _, stranger := range [][2]uuid.UUID{{workspace, uuid.New()}, {uuid.New(), user}} {
		_, err = service.PollCodexDeviceLogin(ctx, stranger[0], stranger[1], started.AttemptID)
		require.ErrorIs(t, err, ErrCodexDeviceAttemptNotFound)
		require.ErrorIs(t, service.CancelCodexDeviceLogin(stranger[0], stranger[1], started.AttemptID), ErrCodexDeviceAttemptNotFound)
	}
	pending, err := service.PollCodexDeviceLogin(ctx, workspace, user, started.AttemptID)
	require.NoError(t, err)
	require.Equal(t, "pending", pending.Status)
	const authFile = `{"auth_mode":"chatgpt","tokens":{"access_token":"<synthetic-code-test-data>"}}`
	result <- []byte(authFile)
	require.Eventually(t, func() bool {
		current, err := service.PollCodexDeviceLogin(ctx, workspace, user, started.AttemptID)
		return err == nil && current.Status == "connected"
	}, time.Second, time.Millisecond)
	sealed := <-store.saved
	status, err := service.PollCodexDeviceLogin(ctx, workspace, user, started.AttemptID)
	require.NoError(t, err)
	require.Equal(t, "connected", status.Status)
	require.Empty(t, store.saved, "polling again never repeats the write")
	require.NotContains(t, sealed, "<synthetic-code-test-data>")
	opened, err := cryptoutil.Decrypt(sealed, cryptoutil.DeriveKey("test"))
	require.NoError(t, err)
	require.Equal(t, authFile, opened)
	listed, err := service.ListAgentSignIns(ctx, workspace, user)
	require.NoError(t, err)
	require.NotNil(t, listed[1].Terminal)
	listed, err = service.ListAgentSignIns(ctx, workspace, uuid.New())
	require.NoError(t, err)
	require.Nil(t, listed[1].Terminal)
}

func TestCodexDeviceLoginCancelAndExpiry(t *testing.T) {
	store := &codexDeviceTestStore{saved: make(chan string, 1)}
	service := newTestDevMachineService(store)
	ctx := context.Background()
	workspace, user := uuid.New(), uuid.New()
	closed := make(chan struct{}, 2)
	service.codexDevices.start = func(ctx context.Context) (codexDeviceLogin, error) {
		return codexDeviceLogin{
			code: "ABCD-1234", url: "https://auth.openai.com/codex/device",
			wait: func() ([]byte, error) {
				<-ctx.Done()
				return []byte(`{"auth_mode":"chatgpt"}`), nil
			},
			close: func() { closed <- struct{}{} },
		}, nil
	}
	started, err := service.StartCodexDeviceLogin(ctx, workspace, user)
	require.NoError(t, err)
	replacement, err := service.StartCodexDeviceLogin(ctx, workspace, user)
	require.NoError(t, err)
	require.NotEqual(t, started.AttemptID, replacement.AttemptID)
	require.ErrorIs(t, service.CancelCodexDeviceLogin(workspace, user, started.AttemptID), ErrCodexDeviceAttemptNotFound)
	pending, err := service.PollCodexDeviceLogin(ctx, workspace, user, replacement.AttemptID)
	require.NoError(t, err)
	require.Equal(t, "pending", pending.Status)
	require.NoError(t, service.CancelCodexDeviceLogin(workspace, user, replacement.AttemptID))
	for _, cancelled := range []string{"replaced", "cancelled"} {
		select {
		case <-closed:
		case <-time.After(time.Second):
			t.Fatalf("%s login was not closed", cancelled)
		}
	}
	require.Empty(t, store.saved)

	service.codexDevices.lifetime = 150 * time.Millisecond
	started, err = service.StartCodexDeviceLogin(ctx, workspace, user)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		current, err := service.PollCodexDeviceLogin(ctx, workspace, user, started.AttemptID)
		return err == nil && current.Status == "expired"
	}, time.Second, time.Millisecond)
	require.Empty(t, store.saved)

	service.codexDevices.start = func(context.Context) (codexDeviceLogin, error) {
		return codexDeviceLogin{}, errors.New("upstream body with sensitive fixture")
	}
	_, err = service.StartCodexDeviceLogin(ctx, workspace, user)
	require.ErrorIs(t, err, ErrCodexDeviceUnavailable)
	require.NotContains(t, err.Error(), "sensitive fixture")
}
