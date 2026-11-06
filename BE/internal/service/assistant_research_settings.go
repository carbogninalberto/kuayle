package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/pkg/audit"
	"github.com/kuayle/kuayle-backend/pkg/crypto"
)

var (
	ErrAssistantResearchDenied = errors.New("assistant research settings access denied")
	ErrInvalidResearchSettings = errors.New("invalid assistant research settings")
)

type assistantResearchStore interface {
	Get(ctx context.Context, ws uuid.UUID) (*domain.AssistantResearchSettings, error)
	Save(ctx context.Context, settings *domain.AssistantResearchSettings, user uuid.UUID) error
}

// AssistantResearchService manages Settings → Assistant → Research. Members
// read it; owners and admins change it (checked again in SQL). The search API
// key is encrypted at rest and only reported as set or not.
type AssistantResearchService struct {
	store   assistantResearchStore
	members assistantMemberRepo
	key     []byte
}

func NewAssistantResearchService(store assistantResearchStore, members assistantMemberRepo, encryptionKey []byte) *AssistantResearchService {
	return &AssistantResearchService{store: store, members: members, key: encryptionKey}
}

// ResearchSettingsView is the client's view of the settings.
type ResearchSettingsView struct {
	SearchProvider      string     `json:"search_provider"`
	SearchProviders     []string   `json:"search_providers"`
	HasSearchAPIKey     bool       `json:"has_search_api_key"`
	SubagentModel       string     `json:"subagent_model"`
	SubagentMaxParallel int        `json:"subagent_max_parallel"`
	CanManage           bool       `json:"can_manage"`
	UpdatedAt           *time.Time `json:"updated_at,omitempty"`
}

// ResearchSettingsUpdate changes the fields that are set. search_api_key:
// absent keeps the key, null or "" removes it, a string replaces it.
type ResearchSettingsUpdate struct {
	SearchProvider      *string            `json:"search_provider"`
	SearchAPIKey        dto.OptionalString `json:"search_api_key"`
	SubagentModel       *string            `json:"subagent_model"`
	SubagentMaxParallel *int               `json:"subagent_max_parallel"`
}

func (s *AssistantResearchService) settings(ctx context.Context, ws uuid.UUID) (*domain.AssistantResearchSettings, error) {
	settings, err := s.store.Get(ctx, ws)
	if err != nil {
		return nil, err
	}
	if settings == nil {
		settings = &domain.AssistantResearchSettings{WorkspaceID: ws, SearchProvider: "brave", SubagentMaxParallel: subagentDefaultParallel}
	}
	return settings, nil
}

// access admits current non-guest members of the request's workspace and
// reports whether the caller is an owner or admin.
func (s *AssistantResearchService) access(ctx context.Context, ws uuid.UUID) (uuid.UUID, bool, error) {
	access, ok := domain.AccessFromContext(ctx)
	if !ok || access.WorkspaceID != ws {
		return uuid.Nil, false, ErrAssistantResearchDenied
	}
	member, err := s.members.GetMember(ctx, ws, access.UserID)
	if err != nil {
		return uuid.Nil, false, err
	}
	if member == nil || member.Role == domain.RoleGuest {
		return uuid.Nil, false, ErrAssistantResearchDenied
	}
	return access.UserID, member.Role == domain.RoleOwner || member.Role == domain.RoleAdmin, nil
}

func researchView(settings *domain.AssistantResearchSettings, manage bool) *ResearchSettingsView {
	view := &ResearchSettingsView{
		SearchProvider: settings.SearchProvider, SearchProviders: slices.Clone(WebSearchProviders),
		HasSearchAPIKey: settings.SearchAPIKeyEncrypted != nil && *settings.SearchAPIKeyEncrypted != "",
		SubagentModel:   settings.SubagentModel, SubagentMaxParallel: settings.SubagentMaxParallel, CanManage: manage,
	}
	if !settings.UpdatedAt.IsZero() {
		updated := settings.UpdatedAt
		view.UpdatedAt = &updated
	}
	return view
}

func (s *AssistantResearchService) View(ctx context.Context, ws uuid.UUID) (*ResearchSettingsView, error) {
	_, manage, err := s.access(ctx, ws)
	if err != nil {
		return nil, err
	}
	settings, err := s.settings(ctx, ws)
	if err != nil {
		return nil, err
	}
	return researchView(settings, manage), nil
}

func invalidResearch(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidResearchSettings, fmt.Sprintf(format, args...))
}

func (s *AssistantResearchService) Update(ctx context.Context, ws uuid.UUID, update ResearchSettingsUpdate) (*ResearchSettingsView, error) {
	user, manage, err := s.access(ctx, ws)
	if err != nil {
		return nil, err
	}
	if !manage {
		return nil, ErrAssistantResearchDenied
	}
	settings, err := s.settings(ctx, ws)
	if err != nil {
		return nil, err
	}
	if update.SearchProvider != nil {
		provider := strings.TrimSpace(*update.SearchProvider)
		if !slices.Contains(WebSearchProviders, provider) {
			return nil, invalidResearch("search_provider must be one of %s", strings.Join(WebSearchProviders, ", "))
		}
		settings.SearchProvider = provider
	}
	if update.SubagentModel != nil {
		model := strings.TrimSpace(*update.SubagentModel)
		if !validModelName(model) {
			return nil, invalidResearch("subagent_model must be at most 200 characters without control characters")
		}
		settings.SubagentModel = model
	}
	if update.SubagentMaxParallel != nil {
		if *update.SubagentMaxParallel < 1 || *update.SubagentMaxParallel > subagentMaxParallel {
			return nil, invalidResearch("subagent_max_parallel must be between 1 and %d", subagentMaxParallel)
		}
		settings.SubagentMaxParallel = *update.SubagentMaxParallel
	}
	keyChanged := false
	if update.SearchAPIKey.Set {
		keyChanged = true
		value := ""
		if update.SearchAPIKey.Value != nil {
			value = strings.TrimSpace(*update.SearchAPIKey.Value)
		}
		if value == "" {
			settings.SearchAPIKeyEncrypted = nil
		} else {
			if len(value) > 512 || strings.IndexFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
				return nil, invalidResearch("search_api_key must be at most 512 characters without spaces")
			}
			encrypted, err := crypto.Encrypt(value, s.key)
			if err != nil {
				return nil, err
			}
			settings.SearchAPIKeyEncrypted = &encrypted
		}
	}
	if err := s.store.Save(ctx, settings, user); err != nil {
		if errors.Is(err, repository.ErrAssistantResearchSettingsDenied) {
			return nil, ErrAssistantResearchDenied
		}
		return nil, err
	}
	audit.Log("assistant.research_settings_updated", user, map[string]interface{}{
		"workspace_id": ws.String(), "search_provider": settings.SearchProvider, "search_key_changed": keyChanged,
		"search_key_set": settings.SearchAPIKeyEncrypted != nil, "subagent_model": settings.SubagentModel, "subagent_max_parallel": settings.SubagentMaxParallel,
	})
	return researchView(settings, true), nil
}

// ResearchConfig is the settings with the key decrypted, for the tools.
func (s *AssistantResearchService) ResearchConfig(ctx context.Context, ws uuid.UUID) (ResearchConfig, error) {
	settings, err := s.settings(ctx, ws)
	if err != nil {
		return ResearchConfig{}, err
	}
	config := ResearchConfig{SearchProvider: settings.SearchProvider, SubagentModel: settings.SubagentModel, SubagentMaxParallel: settings.SubagentMaxParallel}
	if settings.SearchAPIKeyEncrypted != nil && *settings.SearchAPIKeyEncrypted != "" {
		if config.SearchAPIKey, err = crypto.Decrypt(*settings.SearchAPIKeyEncrypted, s.key); err != nil {
			// A key encrypted under another secret cannot be used; say it is not configured.
			config.SearchAPIKey = ""
		}
	}
	return config, nil
}
