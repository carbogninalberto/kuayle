package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	cryptoutil "github.com/kuayle/kuayle-backend/pkg/crypto"
)

// modelNamePattern matches the database constraint on model names.
var modelNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/@+-]{0,127}$`)

const maxDiscoveredModels = 500

// SetModelDiscovery lets operators discover models on a private network. Admins
// choose the base URL, so only public addresses are dialled by default.
func (s *DevMachineService) SetModelDiscovery(allowPrivateNetwork bool) {
	s.modelClient = newAIProviderClient(allowPrivateNetwork)
}

func (s *DevMachineService) modelHTTPClient() *http.Client {
	if s.modelClient == nil {
		s.modelClient = newAIProviderClient(false)
	}
	return s.modelClient
}

func modelServerResponse(server domain.AgentModelServer) dto.ModelServerResponse {
	harnesses := []string{}
	for _, provider := range []string{"codex", "claude-code"} {
		if server.Serves(provider) {
			harnesses = append(harnesses, provider)
		}
	}
	models := server.ModelList()
	if models == nil {
		models = []string{}
	}
	return dto.ModelServerResponse{
		ID: server.ID, Name: server.Name, OpenAIBaseURL: server.OpenAIBaseURL, AnthropicBaseURL: server.AnthropicBaseURL,
		Models: models, DefaultModel: server.DefaultModel, FastModel: server.FastModel, Thinking: server.Thinking,
		IsDefault: server.IsDefault, Harnesses: harnesses, ThinkingLevels: append([]string(nil), modelThinkingLevels...),
		UpdatedAt: server.UpdatedAt,
	}
}

// ListModelServers reports configuration only; keys are never returned.
func (s *DevMachineService) ListModelServers(ctx context.Context, workspaceID uuid.UUID) ([]dto.ModelServerResponse, error) {
	servers, err := s.store.ListModelServers(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	responses := make([]dto.ModelServerResponse, 0, len(servers))
	for _, server := range servers {
		responses = append(responses, modelServerResponse(server))
	}
	return responses, nil
}

// SaveModelServer creates a server (id nil) or updates one. The first server
// of a workspace becomes its default.
func (s *DevMachineService) SaveModelServer(ctx context.Context, workspaceID, userID uuid.UUID, id *uuid.UUID, request dto.SaveModelServerRequest) (*dto.ModelServerResponse, error) {
	server := &domain.AgentModelServer{WorkspaceID: workspaceID}
	if id != nil {
		existing, err := s.store.GetModelServer(ctx, workspaceID, id)
		if err != nil {
			return nil, err
		}
		if existing == nil {
			return nil, ErrModelServerNotFound
		}
		server = existing
	}
	name := strings.TrimSpace(request.Name)
	if name == "" || utf8.RuneCountInString(name) > 64 {
		return nil, fmt.Errorf("%w: name must be 1-64 characters", ErrInvalidMachineInput)
	}
	openai, err := normalizeModelBaseURL(request.OpenAIBaseURL)
	if err != nil {
		return nil, err
	}
	anthropic, err := normalizeModelBaseURL(request.AnthropicBaseURL)
	if err != nil {
		return nil, err
	}
	if openai == nil && anthropic == nil {
		return nil, fmt.Errorf("%w: add an OpenAI-compatible or Anthropic-compatible base URL", ErrInvalidMachineInput)
	}
	models, err := cleanModelList(request.Models)
	if err != nil {
		return nil, err
	}
	for _, model := range []string{request.DefaultModel, request.FastModel} {
		if model == "" {
			continue
		}
		if !modelNamePattern.MatchString(model) {
			return nil, fmt.Errorf("%w: invalid model name %q", ErrInvalidMachineInput, model)
		}
		if len(models) > 0 && !containsString(models, model) {
			return nil, fmt.Errorf("%w: model %q is not offered by this server", ErrInvalidMachineInput, model)
		}
	}
	if request.DefaultModel == "" {
		return nil, fmt.Errorf("%w: choose a default model", ErrInvalidMachineInput)
	}
	if !containsString(modelThinkingLevels, request.Thinking) {
		return nil, fmt.Errorf("%w: unsupported thinking level", ErrInvalidMachineInput)
	}
	if request.APIKey == nil {
		if server.EncryptedAPIKey == "" {
			return nil, fmt.Errorf("%w: api_key is required for a new server", ErrInvalidMachineInput)
		}
	} else {
		if !validModelAPIKey(*request.APIKey) {
			return nil, fmt.Errorf("%w: api_key must be 1-512 printable characters without whitespace", ErrInvalidMachineInput)
		}
		encrypted, encryptErr := cryptoutil.Encrypt(*request.APIKey, s.encryptionKey)
		if encryptErr != nil {
			return nil, fmt.Errorf("encrypt model server API key: %w", encryptErr)
		}
		server.EncryptedAPIKey, server.EncryptionKeyVersion = encrypted, 1
	}
	existing, err := s.store.ListModelServers(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	encodedModels, _ := json.Marshal(models)
	server.Name, server.OpenAIBaseURL, server.AnthropicBaseURL = name, openai, anthropic
	server.Models, server.DefaultModel, server.FastModel, server.Thinking = encodedModels, request.DefaultModel, request.FastModel, request.Thinking
	server.IsDefault = request.IsDefault || len(existing) == 0 || (len(existing) == 1 && existing[0].ID == server.ID)
	server.UpdatedBy = &userID
	if err = s.store.SaveModelServer(ctx, server); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrModelServerNotFound
		}
		if isUniqueViolation(err) {
			return nil, fmt.Errorf("%w: a model server with this name already exists", ErrInvalidMachineInput)
		}
		return nil, err
	}
	response := modelServerResponse(*server)
	return &response, nil
}

func (s *DevMachineService) DeleteModelServer(ctx context.Context, workspaceID, id uuid.UUID) error {
	if err := s.store.DeleteModelServer(ctx, workspaceID, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrModelServerNotFound
		}
		return err
	}
	return nil
}

// DiscoverModels asks a server which models it offers, through its OpenAI
// (`GET /models`) or Anthropic (`GET /v1/models`) listing.
func (s *DevMachineService) DiscoverModels(ctx context.Context, workspaceID uuid.UUID, request dto.DiscoverModelsRequest) (*dto.DiscoverModelsResponse, error) {
	openai, err := normalizeModelBaseURL(request.OpenAIBaseURL)
	if err != nil {
		return nil, err
	}
	anthropic, err := normalizeModelBaseURL(request.AnthropicBaseURL)
	if err != nil {
		return nil, err
	}
	key := ""
	if request.APIKey != nil {
		key = *request.APIKey
	} else if request.ServerID != nil {
		server, err := s.store.GetModelServer(ctx, workspaceID, request.ServerID)
		if err != nil {
			return nil, err
		}
		if server == nil {
			return nil, ErrModelServerNotFound
		}
		// A stored key is only sent to the URLs saved with it.
		if !sameURL(openai, server.OpenAIBaseURL) || !sameURL(anthropic, server.AnthropicBaseURL) {
			return nil, fmt.Errorf("%w: enter the API key again to use different URLs", ErrInvalidMachineInput)
		}
		if key, err = cryptoutil.Decrypt(server.EncryptedAPIKey, s.encryptionKey); err != nil {
			return nil, err
		}
	}
	if !validModelAPIKey(key) {
		return nil, fmt.Errorf("%w: an API key is required to list models", ErrInvalidMachineInput)
	}
	var models []string
	switch {
	case openai != nil:
		models, err = s.listModels(ctx, strings.TrimSuffix(*openai, "/")+"/models", map[string]string{"Authorization": "Bearer " + key})
	case anthropic != nil:
		models, err = s.listModels(ctx, strings.TrimSuffix(*anthropic, "/")+"/v1/models?limit=1000", map[string]string{"x-api-key": key, "anthropic-version": "2023-06-01"})
	default:
		return nil, fmt.Errorf("%w: add a base URL to list models", ErrInvalidMachineInput)
	}
	if err != nil {
		return nil, err
	}
	return &dto.DiscoverModelsResponse{Models: models}, nil
}

func (s *DevMachineService) listModels(ctx context.Context, url string, headers map[string]string) ([]string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid base URL", ErrInvalidMachineInput)
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response, err := s.modelHTTPClient().Do(request)
	if err != nil {
		return nil, fmt.Errorf("%w: the server could not be reached", ErrModelDiscoveryFailed)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: the server answered %d", ErrModelDiscoveryFailed, response.StatusCode)
	}
	var listing struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&listing); err != nil {
		return nil, fmt.Errorf("%w: the server did not return a model list", ErrModelDiscoveryFailed)
	}
	models := []string{}
	for _, item := range listing.Data {
		if modelNamePattern.MatchString(item.ID) && !containsString(models, item.ID) {
			models = append(models, item.ID)
		}
		if len(models) == maxDiscoveredModels {
			break
		}
	}
	sort.Strings(models)
	return models, nil
}

var (
	// ErrModelDiscoveryFailed means the server's model list could not be read.
	ErrModelDiscoveryFailed = errors.New("model discovery failed")
	ErrModelServerNotFound  = errors.New("model server not found")
)

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func normalizeModelBaseURL(value *string) (*string, error) {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil, nil
	}
	trimmed := strings.TrimSuffix(strings.TrimSpace(*value), "/")
	if len(trimmed) > 512 || strings.ContainsAny(trimmed, " \t\r\n\"'") {
		return nil, fmt.Errorf("%w: invalid base URL", ErrInvalidMachineInput)
	}
	if err := validateHTTPURL(trimmed); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidMachineInput, err.Error())
	}
	return &trimmed, nil
}

func cleanModelList(models []string) ([]string, error) {
	if len(models) > maxDiscoveredModels {
		return nil, fmt.Errorf("%w: at most %d models", ErrInvalidMachineInput, maxDiscoveredModels)
	}
	clean := []string{}
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" || containsString(clean, model) {
			continue
		}
		if !modelNamePattern.MatchString(model) {
			return nil, fmt.Errorf("%w: invalid model name %q", ErrInvalidMachineInput, model)
		}
		clean = append(clean, model)
	}
	return clean, nil
}

func sameURL(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
