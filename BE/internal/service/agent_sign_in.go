package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	cryptoutil "github.com/kuayle/kuayle-backend/pkg/crypto"
)

// ErrSignInNotFound means the user has not finished signing in the agent in
// their machine's terminal yet.
var ErrSignInNotFound = errors.New("no agent sign-in was found in the machine's terminal")

var agentDisplayNames = map[string]string{"claude-code": "Claude Code", "codex": "Codex", "opencode": "OpenCode"}

// signInWait bounds how long continuing after sign-in waits for the manager
// to read the sign-in from the terminal.
const signInWait = 25 * time.Second

// ContinueAfterSignIn starts a new attempt once the task's creator has signed
// in: with a token they saved (nothing to capture), or by storing the sign-in
// they completed in their machine's terminal. requestID makes a double click
// converge on one capture and one retry.
func (s *AgentTaskService) ContinueAfterSignIn(ctx context.Context, ws, id, requestID uuid.UUID) (*domain.AgentTask, error) {
	if requestID != uuid.Nil {
		task, err := s.Repo.Get(ctx, ws, id)
		if err != nil {
			return nil, err
		}
		if ok, err := s.creatorSavedToken(ctx, ws, task); err != nil {
			return nil, err
		} else if ok {
			return s.retryAfterSignIn(ctx, ws, id, requestID, task)
		}
	}
	op, err := s.Repo.RequestSignInCapture(ctx, ws, id, requestID)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(signInWait)
	for op.Status != domain.DevMachineOpStatusCompleted {
		if op.Status == domain.DevMachineOpStatusFailed || op.Status == domain.DevMachineOpStatusCancelled {
			return nil, ErrSignInNotFound
		}
		if time.Now().After(deadline) {
			return nil, context.DeadlineExceeded
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
		current, err := s.machines.store.GetOperationByIdempotency(ctx, ws, op.MachineID, op.IdempotencyKey)
		if err != nil {
			return nil, err
		}
		if current != nil {
			op = current
		}
	}
	task, err := s.Repo.Get(ctx, ws, id)
	if err != nil {
		return nil, err
	}
	return s.retryAfterSignIn(ctx, ws, id, requestID, task)
}

// creatorSavedToken reports whether the caller created this task stopped for
// sign-in and has since saved a token for its agent.
func (s *AgentTaskService) creatorSavedToken(ctx context.Context, ws uuid.UUID, task *domain.AgentTask) (bool, error) {
	access, ok := domain.AccessFromContext(ctx)
	if !ok || s.machines == nil || task.CreatorID != access.UserID || task.State != "failed" || !task.SignInRequired {
		return false, nil
	}
	return s.machines.HasAgentSignInToken(ctx, ws, access.UserID, task.Provider)
}

func (s *AgentTaskService) retryAfterSignIn(ctx context.Context, ws, id, requestID uuid.UUID, task *domain.AgentTask) (*domain.AgentTask, error) {
	// The new attempt keeps the task's own instructions and test override.
	var command []string
	if task.TestCommand != nil {
		_ = json.Unmarshal(*task.TestCommand, &command)
	}
	return s.Retry(ctx, ws, id, domain.AgentTaskRetryRequest{RequestID: requestID, Generation: task.Generation, Prompt: task.Prompt, TestCommand: command})
}

// signInAgents are the agents with their own sign-in, in display order.
var signInAgents = []string{"claude-code", "codex"}

// tokenAgents accept a pasted token instead of a captured credentials file.
var tokenAgents = map[string]bool{"claude-code": true}

// signInTokenLifetime is how long a `claude setup-token` token lasts. Kuayle
// counts it from when the token was saved, so its estimate is never later
// than the real expiry.
const signInTokenLifetime = 365 * 24 * time.Hour

const (
	minSignInTokenLength = 20
	maxSignInTokenLength = 1024
)

// validSignInToken accepts one line of printable ASCII without whitespace:
// Claude Code tokens look like sk-ant-oat01-..., but the prefix is not
// required in case Anthropic changes it.
func validSignInToken(token string) bool {
	if len(token) < minSignInTokenLength || len(token) > maxSignInTokenLength {
		return false
	}
	for _, character := range []byte(token) {
		if character <= ' ' || character > '~' {
			return false
		}
	}
	return true
}

func knownSignInAgent(provider string) bool {
	for _, agent := range signInAgents {
		if agent == provider {
			return true
		}
	}
	return false
}

// ListAgentSignIns reports the user's own sign-in for each agent, without
// the sign-ins themselves.
func (s *DevMachineService) ListAgentSignIns(ctx context.Context, workspaceID, userID uuid.UUID) ([]dto.AgentSignInResponse, error) {
	stored, err := s.store.ListAgentSignIns(ctx, workspaceID, userID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	responses := make([]dto.AgentSignInResponse, 0, len(signInAgents))
	for _, provider := range signInAgents {
		response := dto.AgentSignInResponse{Provider: provider, SupportsToken: tokenAgents[provider]}
		for _, signIn := range stored {
			if signIn.Provider != provider {
				continue
			}
			switch signIn.Kind {
			case domain.AgentSignInToken:
				response.Token = &dto.AgentSignInTokenStatus{SavedAt: signIn.UpdatedAt, ExpiresAt: signIn.ExpiresAt, Expired: signIn.ExpiresAt != nil && !signIn.ExpiresAt.After(now)}
			case domain.AgentSignInCredentials:
				response.Terminal = &dto.AgentSignInTerminalInfo{SavedAt: signIn.UpdatedAt}
			}
		}
		responses = append(responses, response)
	}
	return responses, nil
}

// SaveAgentSignInToken encrypts and stores the user's token for an agent
// that accepts one, replacing an earlier token. Later runs of that user
// prefer it to a sign-in captured from the terminal.
func (s *DevMachineService) SaveAgentSignInToken(ctx context.Context, workspaceID, userID uuid.UUID, provider, token string) (*dto.AgentSignInResponse, error) {
	if !tokenAgents[provider] {
		return nil, fmt.Errorf("%w: %s has no token sign-in", ErrInvalidMachineInput, provider)
	}
	token = strings.TrimSpace(token)
	if !validSignInToken(token) {
		return nil, fmt.Errorf("%w: the token must be %d-%d printable characters on one line without spaces", ErrInvalidMachineInput, minSignInTokenLength, maxSignInTokenLength)
	}
	encrypted, err := cryptoutil.Encrypt(token, s.encryptionKey)
	if err != nil {
		return nil, err
	}
	if err = s.store.SaveAgentSignInToken(ctx, workspaceID, userID, provider, encrypted, time.Now().Add(signInTokenLifetime)); err != nil {
		return nil, err
	}
	signIns, err := s.ListAgentSignIns(ctx, workspaceID, userID)
	if err != nil {
		return nil, err
	}
	for index := range signIns {
		if signIns[index].Provider == provider {
			return &signIns[index], nil
		}
	}
	return nil, errors.New("saved sign-in not found")
}

// DeleteAgentSignIn removes the user's own sign-in for an agent: the token,
// the terminal sign-in, or both when kind is "".
func (s *DevMachineService) DeleteAgentSignIn(ctx context.Context, workspaceID, userID uuid.UUID, provider, kind string) error {
	if !knownSignInAgent(provider) {
		return fmt.Errorf("%w: unknown agent %q", ErrInvalidMachineInput, provider)
	}
	if kind != "" && kind != domain.AgentSignInToken && kind != domain.AgentSignInCredentials {
		return fmt.Errorf("%w: kind must be token or credentials", ErrInvalidMachineInput)
	}
	_, err := s.store.DeleteAgentSignIns(ctx, workspaceID, userID, provider, kind)
	return err
}

// HasAgentSignInToken reports whether the user has an unexpired token for
// the agent.
func (s *DevMachineService) HasAgentSignInToken(ctx context.Context, workspaceID, userID uuid.UUID, provider string) (bool, error) {
	if !tokenAgents[provider] {
		return false, nil
	}
	encrypted, err := s.store.GetAgentSignInToken(ctx, workspaceID, userID, provider)
	return encrypted != "", err
}
