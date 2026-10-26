package service

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/pkg/crypto"
	log "github.com/sirupsen/logrus"
	"strings"
	"sync"
	"time"
)

type ConversationService struct {
	Repo *repository.ConversationRepository
	ai   *AISettingsService
}

func NewConversationService(repo *repository.ConversationRepository, ai *AISettingsService) *ConversationService {
	return &ConversationService{Repo: repo, ai: ai}
}
func (s *ConversationService) configuration(ctx context.Context, ws uuid.UUID) (*domain.AISettings, error) {
	settings, err := s.ai.Get(ctx, ws)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(settings.BaseURL) == "" || strings.TrimSpace(settings.Model) == "" || settings.APIKeyEncrypted == nil || *settings.APIKeyEncrypted == "" {
		return nil, ErrAISettingsNotConfigured
	}
	return settings, nil
}
func (s *ConversationService) Status(ctx context.Context, ws uuid.UUID) (map[string]any, error) {
	// Use the same role/membership check as conversation reads.
	if _, err := s.Repo.List(ctx, ws); err != nil {
		return nil, err
	}
	settings, err := s.configuration(ctx, ws)
	if errors.Is(err, ErrAISettingsNotConfigured) {
		return map[string]any{"configured": false}, nil
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{"configured": true, "provider": settings.Provider, "model": settings.Model}, nil
}
func (s *ConversationService) Send(ctx context.Context, ws, id, request uuid.UUID, content string) (*domain.ConversationMessage, error) {
	if _, err := s.Repo.History(ctx, ws, id, 0); err != nil {
		return nil, err
	}
	settings, err := s.configuration(ctx, ws)
	if err != nil {
		return nil, err
	}
	return s.Repo.Send(ctx, ws, id, request, content, settings.Provider, settings.Model)
}
func (s *ConversationService) Retry(ctx context.Context, ws, id, message, request uuid.UUID) (*domain.ConversationMessage, error) {
	if _, err := s.Repo.History(ctx, ws, id, 0); err != nil {
		return nil, err
	}
	settings, err := s.configuration(ctx, ws)
	if err != nil {
		return nil, err
	}
	return s.Repo.Retry(ctx, ws, id, message, request, settings.Provider, settings.Model)
}

// Run uses a small bounded pool in the existing API process. PostgreSQL owns
// claims and fencing, so browser reconnects and additional API processes cannot
// duplicate a generation. No new service is required.
func (s *ConversationService) Run(ctx context.Context) {
	var workers sync.WaitGroup
	for range 4 {
		workers.Go(func() {
			ticker := time.NewTicker(500 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					claimCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
					attempt, err := s.Repo.Claim(claimCtx)
					cancel()
					if err != nil {
						log.Warn("Assistant queue claim failed")
						continue
					}
					if attempt != nil {
						s.execute(ctx, *attempt)
					}
				}
			}
		})
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	defer workers.Wait()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			recoveryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := s.Repo.Recover(recoveryCtx)
			cancel()
			if err != nil {
				log.Warn("Assistant queue recovery failed")
			}
		}
	}
}
func (s *ConversationService) execute(parent context.Context, a domain.ConversationAttempt) {
	ctx, cancel := context.WithTimeout(domain.WithAccess(parent, domain.Access{WorkspaceID: a.Conversation.WorkspaceID, UserID: a.Conversation.CreatorID}), 95*time.Second)
	defer cancel()
	status := "failed"
	defer func() {
		// Persist a terminal state even during graceful shutdown; expiry recovers
		// an ungraceful restart, without silently charging for a second model call.
		finishCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer stop()
		if err := s.Repo.Finish(finishCtx, a, status); err != nil && !errors.Is(err, repository.ErrConversationConflict) && !errors.Is(err, repository.ErrConversationNotFound) {
			log.Warn("Assistant completion persistence failed")
		}
	}()
	settings, err := s.configuration(ctx, a.Conversation.WorkspaceID)
	if err != nil {
		return
	}
	key, err := crypto.Decrypt(*settings.APIKeyEncrypted, s.ai.encryptionKey)
	if err != nil {
		return
	}
	history, err := s.Repo.GenerationContext(ctx, a)
	if err != nil {
		return
	}
	messages := []chatCompletionMessage{{Role: "system", Content: "You are Kuayle's project assistant. Respond in the user's language. You currently have conversation access only: do not claim to have read repositories, created issues, run tools, or changed files. Explain uncertainty and ask for missing context. Never invent execution results."}}
	for _, m := range history {
		messages = append(messages, chatCompletionMessage{Role: m.Role, Content: m.Content})
	}
	deltas := make(chan string, 16)
	done := make(chan error, 1)
	go func() {
		done <- s.ai.streamAssistant(ctx, settings.BaseURL, a.Message.Model, key, messages, func(delta string) error {
			select {
			case deltas <- delta:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		close(deltas)
	}()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	pending := ""
	flush := func() error {
		if pending == "" {
			return s.Repo.CheckAttempt(ctx, a)
		}
		_, err := s.Repo.Append(ctx, a, pending)
		if err == nil {
			pending = ""
		}
		return err
	}
	for {
		select {
		case <-ctx.Done():
			status = "interrupted"
			return
		case delta, open := <-deltas:
			if !open {
				err := <-done
				if flush() != nil {
					return
				}
				if err == nil {
					status = "completed"
				} else {
					status = "interrupted"
				}
				return
			}
			pending += delta
			if len(pending) >= 1024 {
				if flush() != nil {
					return
				}
			}
		case <-ticker.C:
			if flush() != nil {
				return
			}
		}
	}
}
