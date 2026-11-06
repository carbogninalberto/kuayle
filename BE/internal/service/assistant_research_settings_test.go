package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/pkg/crypto"
	"github.com/stretchr/testify/require"
)

type researchStoreFake struct {
	settings *domain.AssistantResearchSettings
	saves    int
}

func (f *researchStoreFake) Get(context.Context, uuid.UUID) (*domain.AssistantResearchSettings, error) {
	if f.settings == nil {
		return nil, nil
	}
	copy := *f.settings
	return &copy, nil
}

func (f *researchStoreFake) Save(_ context.Context, settings *domain.AssistantResearchSettings, user uuid.UUID) error {
	f.saves++
	copy := *settings
	copy.UpdatedBy = &user
	f.settings = &copy
	return nil
}

func researchService() (*AssistantResearchService, *researchStoreFake, uuid.UUID, map[string]uuid.UUID) {
	store := &researchStoreFake{}
	ws := uuid.New()
	users := map[string]uuid.UUID{domain.RoleOwner: uuid.New(), domain.RoleAdmin: uuid.New(), domain.RoleMember: uuid.New(), domain.RoleGuest: uuid.New(), "outsider": uuid.New()}
	members := prMembers{}
	for role, id := range users {
		if role != "outsider" {
			members[id] = role
		}
	}
	return NewAssistantResearchService(store, members, crypto.DeriveKey("test:assistant-research")), store, ws, users
}

func as(ws, user uuid.UUID) context.Context {
	return domain.WithAccess(context.Background(), domain.Access{WorkspaceID: ws, UserID: user})
}

func optional(value string) dto.OptionalString { return dto.OptionalString{Value: &value, Set: true} }

func TestResearchSettingsKeepTheKeyEncryptedAndNeverShowIt(t *testing.T) {
	svc, store, ws, users := researchService()
	view, err := svc.View(as(ws, users[domain.RoleMember]), ws)
	require.NoError(t, err)
	require.Equal(t, "brave", view.SearchProvider)
	require.False(t, view.HasSearchAPIKey)
	require.Equal(t, subagentDefaultParallel, view.SubagentMaxParallel)
	require.False(t, view.CanManage)
	require.Equal(t, WebSearchProviders, view.SearchProviders)

	provider, model, parallel := "tavily", "gpt-small", 4
	view, err = svc.Update(as(ws, users[domain.RoleAdmin]), ws, ResearchSettingsUpdate{SearchProvider: &provider, SearchAPIKey: optional("  tvly-secret  "), SubagentModel: &model, SubagentMaxParallel: &parallel})
	require.NoError(t, err)
	require.True(t, view.HasSearchAPIKey)
	require.True(t, view.CanManage)
	data, _ := json.Marshal(view)
	require.NotContains(t, string(data), "tvly-secret", "the key is never returned")
	require.NotContains(t, *store.settings.SearchAPIKeyEncrypted, "tvly-secret", "the key is stored encrypted")

	config, err := svc.ResearchConfig(context.Background(), ws)
	require.NoError(t, err)
	require.Equal(t, ResearchConfig{SearchProvider: "tavily", SearchAPIKey: "tvly-secret", SubagentModel: "gpt-small", SubagentMaxParallel: 4}, config)

	// Leaving the key out keeps it; null removes it.
	other := "brave"
	_, err = svc.Update(as(ws, users[domain.RoleOwner]), ws, ResearchSettingsUpdate{SearchProvider: &other})
	require.NoError(t, err)
	config, _ = svc.ResearchConfig(context.Background(), ws)
	require.Equal(t, "tvly-secret", config.SearchAPIKey)
	view, err = svc.Update(as(ws, users[domain.RoleOwner]), ws, ResearchSettingsUpdate{SearchAPIKey: dto.OptionalString{Set: true}})
	require.NoError(t, err)
	require.False(t, view.HasSearchAPIKey)
	config, _ = svc.ResearchConfig(context.Background(), ws)
	require.Empty(t, config.SearchAPIKey)
}

func TestResearchSettingsAreReadByMembersAndChangedByAdmins(t *testing.T) {
	svc, store, ws, users := researchService()
	for _, role := range []string{domain.RoleGuest, "outsider"} {
		_, err := svc.View(as(ws, users[role]), ws)
		require.ErrorIs(t, err, ErrAssistantResearchDenied, role)
	}
	_, err := svc.View(as(uuid.New(), users[domain.RoleOwner]), ws)
	require.ErrorIs(t, err, ErrAssistantResearchDenied, "the access must be for this workspace")
	for _, role := range []string{domain.RoleMember, domain.RoleGuest, "outsider"} {
		_, err := svc.Update(as(ws, users[role]), ws, ResearchSettingsUpdate{SearchAPIKey: optional("x")})
		require.ErrorIs(t, err, ErrAssistantResearchDenied, role)
	}
	require.Zero(t, store.saves, "refused updates write nothing")
}

func TestResearchSettingsAreValidated(t *testing.T) {
	svc, store, ws, users := researchService()
	ctx := as(ws, users[domain.RoleOwner])
	bad := "google"
	zero, nine := 0, 9
	control := "model\x00"
	for name, update := range map[string]ResearchSettingsUpdate{
		"provider":    {SearchProvider: &bad},
		"parallel 0":  {SubagentMaxParallel: &zero},
		"parallel 9":  {SubagentMaxParallel: &nine},
		"model":       {SubagentModel: &control},
		"key spaces":  {SearchAPIKey: optional("two words")},
		"key too big": {SearchAPIKey: optional(string(make([]byte, 600)))},
	} {
		_, err := svc.Update(ctx, ws, update)
		require.ErrorIs(t, err, ErrInvalidResearchSettings, name)
	}
	require.Zero(t, store.saves)
}

var _ assistantResearchStore = (*repository.AssistantResearchSettingsRepository)(nil)
