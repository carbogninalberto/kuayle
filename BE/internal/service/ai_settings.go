package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/pkg/crypto"
	"github.com/kuayle/kuayle-backend/pkg/sanitize"
)

const DefaultDescriptionExpandPrompt = "Write TipTap-compatible HTML for a Linear-style issue description. Use only an HTML fragment that TipTap StarterKit can parse, such as <p>, <strong>, <em>, <code>, <ul>, <ol>, <li>, <blockquote>, <h2>, and <h3>. Do not return Markdown, code fences, <html>, <body>, scripts, styles, or explanations. Expand or rewrite the given text into clear, actionable issue content with context, expected behavior, acceptance criteria, and relevant edge cases. Keep it concise and practical. Preserve existing meaning and avoid inventing facts."
const DefaultIssueCopyPrompt = "Work on issue {{issue_identifier}}:\n\n{{issue_xml}}"

var (
	ErrAISettingsNotConfigured = errors.New("AI settings are not configured")
	ErrAIProviderRequestFailed = errors.New("AI provider request failed")
	ErrIssueNotFound           = errors.New("issue not found")
)

type AISettingsService struct {
	aiRepo        repository.AISettingsRepo
	workspaceRepo repository.WorkspaceRepo
	issueRepo     repository.IssueRepo
	encryptionKey []byte
	httpClient    *http.Client
}

func NewAISettingsService(aiRepo repository.AISettingsRepo, workspaceRepo repository.WorkspaceRepo, issueRepo repository.IssueRepo, encryptionKey []byte) *AISettingsService {
	return &AISettingsService{
		aiRepo:        aiRepo,
		workspaceRepo: workspaceRepo,
		issueRepo:     issueRepo,
		encryptionKey: encryptionKey,
		httpClient:    &http.Client{Timeout: 45 * time.Second},
	}
}

func (s *AISettingsService) Get(ctx context.Context, workspaceID uuid.UUID) (*domain.AISettings, error) {
	settings, err := s.aiRepo.GetByWorkspaceID(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	if settings == nil {
		return defaultAISettings(workspaceID), nil
	}
	if strings.TrimSpace(settings.Provider) == "" {
		settings.Provider = domain.AIProviderOpenAICompatible
	}
	if strings.TrimSpace(settings.DescriptionExpandPrompt) == "" {
		settings.DescriptionExpandPrompt = DefaultDescriptionExpandPrompt
	}
	if strings.TrimSpace(settings.IssueCopyPrompt) == "" {
		settings.IssueCopyPrompt = DefaultIssueCopyPrompt
	}
	return settings, nil
}

func (s *AISettingsService) Update(ctx context.Context, workspaceID, userID uuid.UUID, req dto.UpdateAISettingsRequest) (*domain.AISettings, error) {
	ws, err := s.workspaceRepo.GetByID(ctx, workspaceID)
	if err != nil || ws == nil {
		return nil, ErrWorkspaceNotFound
	}
	if ws.OwnerID != userID {
		return nil, ErrNotWorkspaceOwner
	}

	settings, err := s.Get(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	if req.Provider != nil {
		settings.Provider = strings.TrimSpace(*req.Provider)
	}
	if req.BaseURL != nil {
		settings.BaseURL = strings.TrimRight(strings.TrimSpace(*req.BaseURL), "/")
	}
	if req.Model != nil {
		settings.Model = strings.TrimSpace(*req.Model)
	}
	if req.DescriptionExpandPrompt != nil {
		prompt := strings.TrimSpace(*req.DescriptionExpandPrompt)
		if prompt == "" {
			prompt = DefaultDescriptionExpandPrompt
		}
		settings.DescriptionExpandPrompt = prompt
	}
	if req.IssueCopyPrompt != nil {
		prompt := strings.TrimSpace(*req.IssueCopyPrompt)
		if prompt == "" {
			prompt = DefaultIssueCopyPrompt
		}
		settings.IssueCopyPrompt = prompt
	}
	if req.APIKey.Set {
		if req.APIKey.Value == nil || strings.TrimSpace(*req.APIKey.Value) == "" {
			settings.APIKeyEncrypted = nil
		} else {
			encrypted, err := crypto.Encrypt(strings.TrimSpace(*req.APIKey.Value), s.encryptionKey)
			if err != nil {
				return nil, err
			}
			settings.APIKeyEncrypted = &encrypted
		}
	}

	if settings.Provider == "" {
		settings.Provider = domain.AIProviderOpenAICompatible
	}
	if settings.Provider != domain.AIProviderOpenAICompatible {
		return nil, fmt.Errorf("unsupported AI provider")
	}
	if settings.BaseURL != "" {
		if err := validateHTTPURL(settings.BaseURL); err != nil {
			return nil, err
		}
	}

	if err := s.aiRepo.Upsert(ctx, settings); err != nil {
		return nil, err
	}
	return settings, nil
}

func (s *AISettingsService) ExpandIssueDescription(ctx context.Context, workspaceID uuid.UUID, identifier string, selectedText *string) (string, error) {
	settings, err := s.Get(ctx, workspaceID)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(settings.BaseURL) == "" || strings.TrimSpace(settings.Model) == "" || settings.APIKeyEncrypted == nil || *settings.APIKeyEncrypted == "" {
		return "", ErrAISettingsNotConfigured
	}

	issue, err := s.issueRepo.GetByIdentifier(ctx, workspaceID, identifier)
	if err != nil || issue == nil {
		return "", ErrIssueNotFound
	}

	apiKey, err := crypto.Decrypt(*settings.APIKeyEncrypted, s.encryptionKey)
	if err != nil {
		return "", ErrAISettingsNotConfigured
	}

	currentDescription := ""
	if issue.Description != nil {
		currentDescription = *issue.Description
	}

	selected := ""
	if selectedText != nil {
		selected = strings.TrimSpace(*selectedText)
	}

	userPrompt := fmt.Sprintf("%s\n\nIssue title:\n%s\n\nCurrent description:\n%s", settings.DescriptionExpandPrompt, issue.Title, currentDescription)
	if selected != "" {
		userPrompt += fmt.Sprintf("\n\nSelected text to rewrite. Return only the replacement HTML for this selected text, not the full issue description:\n%s", selected)
	}
	content, err := s.complete(ctx, settings.BaseURL, settings.Model, apiKey, userPrompt)
	if err != nil {
		return "", err
	}
	content = strings.TrimSpace(stripCodeFence(content))
	if content == "" {
		return "", ErrAIProviderRequestFailed
	}
	return sanitize.SanitizeEditorContent(content), nil
}

func defaultAISettings(workspaceID uuid.UUID) *domain.AISettings {
	return &domain.AISettings{
		WorkspaceID:             workspaceID,
		Provider:                domain.AIProviderOpenAICompatible,
		DescriptionExpandPrompt: DefaultDescriptionExpandPrompt,
		IssueCopyPrompt:         DefaultIssueCopyPrompt,
	}
}

func ToAISettingsResponse(settings *domain.AISettings) dto.AISettingsResponse {
	return dto.AISettingsResponse{
		Provider:                settings.Provider,
		BaseURL:                 settings.BaseURL,
		Model:                   settings.Model,
		HasAPIKey:               settings.APIKeyEncrypted != nil && *settings.APIKeyEncrypted != "",
		DescriptionExpandPrompt: settings.DescriptionExpandPrompt,
		DefaultPrompt:           DefaultDescriptionExpandPrompt,
		IssueCopyPrompt:         settings.IssueCopyPrompt,
		DefaultIssueCopyPrompt:  DefaultIssueCopyPrompt,
		CreatedAt:               settings.CreatedAt,
		UpdatedAt:               settings.UpdatedAt,
	}
}

func validateHTTPURL(rawURL string) error {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Hostname() == "" {
		return fmt.Errorf("base URL must be a valid URL")
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("base URL must be an http or https URL")
	}
	return nil
}

type chatCompletionRequest struct {
	Model       string                  `json:"model"`
	Messages    []chatCompletionMessage `json:"messages"`
	Temperature float64                 `json:"temperature"`
}

type chatCompletionMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatCompletionResponse struct {
	Choices []struct {
		Message chatCompletionMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (s *AISettingsService) complete(ctx context.Context, baseURL, model, apiKey, prompt string) (string, error) {
	payload := chatCompletionRequest{
		Model: model,
		Messages: []chatCompletionMessage{
			{Role: "system", Content: "You produce safe TipTap/ProseMirror-compatible HTML fragments. Return only parseable HTML nodes, never Markdown fences or surrounding prose."},
			{Role: "user", Content: prompt},
		},
		Temperature: 0.2,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	endpoint := strings.TrimRight(baseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrAIProviderRequestFailed, err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var parsed chatCompletionResponse
	_ = json.Unmarshal(respBody, &parsed)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if parsed.Error != nil && parsed.Error.Message != "" {
			return "", fmt.Errorf("%w: %s", ErrAIProviderRequestFailed, parsed.Error.Message)
		}
		return "", fmt.Errorf("%w: status %d", ErrAIProviderRequestFailed, resp.StatusCode)
	}
	if len(parsed.Choices) == 0 {
		return "", ErrAIProviderRequestFailed
	}
	return parsed.Choices[0].Message.Content, nil
}

func stripCodeFence(content string) string {
	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, "```") {
		return content
	}
	lines := strings.Split(content, "\n")
	if len(lines) >= 3 {
		lines = lines[1:]
		if strings.HasPrefix(strings.TrimSpace(lines[len(lines)-1]), "```") {
			lines = lines[:len(lines)-1]
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
