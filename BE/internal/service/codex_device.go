package service

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/codexlogin"
	"github.com/kuayle/kuayle-backend/internal/dto"
	cryptoutil "github.com/kuayle/kuayle-backend/pkg/crypto"
)

var (
	ErrCodexDeviceUnavailable     = errors.New("Codex device login is unavailable. Check that device login is enabled in ChatGPT, or use a machine terminal")
	ErrCodexDeviceAttemptNotFound = errors.New("Codex device login attempt not found")
	ErrCodexDeviceBusy            = errors.New("Codex device login is busy. Try again in a moment")
)

const (
	codexDeviceLifetime = 15 * time.Minute
	codexDeviceStartup  = 15 * time.Second
	codexDeviceHistory  = 2 * time.Minute
	codexDeviceLimit    = 64

	codexDevicePending   = "pending"
	codexDeviceConnected = "connected"
	codexDeviceExpired   = "expired"
	codexDeviceFailed    = "failed"
)

type codexDeviceLogin struct {
	url, code string
	wait      func() ([]byte, error)
	close     func()
}

type codexDeviceOwner struct {
	workspace, user uuid.UUID
}

type codexDeviceAttempt struct {
	mu     sync.Mutex
	forget *time.Timer // protected by the registry mutex
	prompt dto.CodexDeviceStartResponse
	cancel context.CancelFunc
	status string
	sealed string
}

type codexDeviceLogins struct {
	mu       sync.Mutex
	attempts map[codexDeviceOwner]*codexDeviceAttempt
	lifetime time.Duration
	active   chan struct{}
	start    func(context.Context) (codexDeviceLogin, error)
}

func newCodexDeviceLogins() *codexDeviceLogins {
	return &codexDeviceLogins{
		attempts: make(map[codexDeviceOwner]*codexDeviceAttempt),
		lifetime: codexDeviceLifetime,
		active:   make(chan struct{}, codexDeviceLimit),
		start: func(ctx context.Context) (codexDeviceLogin, error) {
			login, err := codexlogin.Start(ctx)
			if err != nil {
				return codexDeviceLogin{}, err
			}
			return codexDeviceLogin{
				url: login.VerificationURL, code: login.UserCode, wait: login.Wait, close: login.Close,
			}, nil
		},
	}
}

func (a *codexDeviceAttempt) finish(status string) {
	a.status = status
	a.sealed = ""
	a.prompt.UserCode = ""
	a.cancel()
}

func (a *codexDeviceAttempt) expire() {
	if a.status == codexDevicePending && !time.Now().Before(a.prompt.ExpiresAt) {
		a.finish(codexDeviceExpired)
	}
}

func (d *codexDeviceLogins) lookup(owner codexDeviceOwner, id uuid.UUID) (*codexDeviceAttempt, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	attempt := d.attempts[owner]
	// AttemptID is immutable after registration.
	if attempt == nil || attempt.prompt.AttemptID != id {
		return nil, ErrCodexDeviceAttemptNotFound
	}
	return attempt, nil
}

func (s *DevMachineService) StartCodexDeviceLogin(ctx context.Context, workspace, user uuid.UUID) (dto.CodexDeviceStartResponse, error) {
	if err := s.RequireMachineUse(user); err != nil {
		return dto.CodexDeviceStartResponse{}, err
	}
	if !s.enabled || len(s.encryptionKey) == 0 {
		return dto.CodexDeviceStartResponse{}, ErrCodexDeviceUnavailable
	}

	devices := s.codexDevices
	owner := codexDeviceOwner{workspace: workspace, user: user}
	devices.mu.Lock()
	select {
	case devices.active <- struct{}{}:
	default:
		devices.mu.Unlock()
		return dto.CodexDeviceStartResponse{}, ErrCodexDeviceBusy
	}
	previous := devices.attempts[owner]
	expires := time.Now().Add(devices.lifetime)
	// The helper remains alive while the user switches tabs to authorize.
	lifetime, cancel := context.WithDeadline(context.Background(), expires)
	attempt := &codexDeviceAttempt{
		prompt: dto.CodexDeviceStartResponse{AttemptID: uuid.New(), ExpiresAt: expires},
		cancel: cancel, status: codexDevicePending,
	}
	devices.attempts[owner] = attempt
	if previous != nil {
		previous.forget.Stop()
	}
	attempt.forget = time.AfterFunc(time.Until(expires)+codexDeviceHistory, func() {
		devices.mu.Lock()
		defer devices.mu.Unlock()
		if devices.attempts[owner] == attempt {
			delete(devices.attempts, owner)
		}
	})
	devices.mu.Unlock()
	if previous != nil {
		previous.mu.Lock()
		if previous.status == codexDevicePending {
			previous.finish(codexDeviceExpired)
		}
		previous.mu.Unlock()
	}

	stopRequestCancel := context.AfterFunc(ctx, cancel)
	startupTimer := time.AfterFunc(codexDeviceStartup, cancel)
	login, err := devices.start(lifetime)
	stopRequestCancel()
	startupTimer.Stop()
	attempt.mu.Lock()
	if err != nil || lifetime.Err() != nil || attempt.status != codexDevicePending {
		attempt.finish(codexDeviceFailed)
		attempt.mu.Unlock()
		if login.close != nil {
			login.close()
		}
		<-devices.active
		return dto.CodexDeviceStartResponse{}, ErrCodexDeviceUnavailable
	}

	attempt.prompt.VerificationURL = login.url
	attempt.prompt.UserCode = login.code
	prompt := attempt.prompt
	attempt.mu.Unlock()

	go func() {
		defer func() { <-devices.active }()
		defer login.close()
		auth, err := login.wait()
		attempt.mu.Lock()
		defer attempt.mu.Unlock()
		attempt.expire()
		if attempt.status != codexDevicePending {
			return
		}
		if err == nil && len(auth) != 0 {
			attempt.sealed, err = cryptoutil.Encrypt(string(auth), s.encryptionKey)
			if err == nil {
				return
			}
		}
		attempt.finish(codexDeviceFailed)
	}()

	return prompt, nil
}

// PollCodexDeviceLogin saves a completed login in the authenticated poll
// request. Removing workspace access while the helper is running prevents that
// user from completing the save. Repeated polls return the same final status.
func (s *DevMachineService) PollCodexDeviceLogin(ctx context.Context, workspace, user, id uuid.UUID) (dto.CodexDevicePollResponse, error) {
	if err := s.RequireMachineUse(user); err != nil {
		return dto.CodexDevicePollResponse{}, err
	}
	attempt, err := s.codexDevices.lookup(codexDeviceOwner{workspace: workspace, user: user}, id)
	if err != nil {
		return dto.CodexDevicePollResponse{}, err
	}
	attempt.mu.Lock()
	defer attempt.mu.Unlock()
	attempt.expire()
	if attempt.status == codexDevicePending && attempt.sealed != "" {
		if err := s.store.SaveAgentSignIn(ctx, workspace, user, "codex", attempt.sealed); err != nil {
			return dto.CodexDevicePollResponse{}, err
		}
		attempt.finish(codexDeviceConnected)
	}
	return dto.CodexDevicePollResponse{Status: attempt.status}, nil
}

func (s *DevMachineService) CancelCodexDeviceLogin(workspace, user, id uuid.UUID) error {
	attempt, err := s.codexDevices.lookup(codexDeviceOwner{workspace: workspace, user: user}, id)
	if err != nil {
		return err
	}
	attempt.mu.Lock()
	defer attempt.mu.Unlock()
	if attempt.status == codexDevicePending {
		attempt.finish(codexDeviceExpired)
	}
	return nil
}
