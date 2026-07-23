package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/realtime"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/pkg/crypto"
	gh "github.com/kuayle/kuayle-backend/pkg/github"
	log "github.com/sirupsen/logrus"
)

var issueIdentifierRegex = regexp.MustCompile(`(?i)([A-Z][A-Z0-9]+-\d+)`)

// GlobalGitHubAppConfig holds pre-configured GitHub App credentials for SaaS mode.
// When set, all workspaces share this app instead of creating per-workspace apps via manifest.
type GlobalGitHubAppConfig struct {
	AppID         int64
	PrivateKey    string // raw PEM or base64-encoded PEM
	ClientID      string
	ClientSecret  string
	WebhookSecret string
	Slug          string
}

type GitHubService struct {
	ghRepo         *repository.GitHubRepository
	issueRepo      repository.IssueRepo
	teamRepo       repository.TeamRepo
	teamStatusRepo repository.TeamStatusRepo
	historyRepo    repository.IssueHistoryRepo
	encryptionKey  []byte
	hub            *realtime.Hub
	frontendURL    string
	webhookURL     string                 // optional override (e.g. smee.io for dev)
	globalApp      *GlobalGitHubAppConfig // nil = self-hosted mode
}

// IsGlobalMode returns true when a shared GitHub App is configured via env vars.
func (s *GitHubService) IsGlobalMode() bool {
	return s.globalApp != nil
}

func NewGitHubService(
	ghRepo *repository.GitHubRepository,
	issueRepo repository.IssueRepo,
	teamRepo repository.TeamRepo,
	teamStatusRepo repository.TeamStatusRepo,
	historyRepo repository.IssueHistoryRepo,
	encryptionKey []byte,
	hub *realtime.Hub,
	frontendURL string,
	webhookURL string,
	globalApp *GlobalGitHubAppConfig,
) *GitHubService {
	return &GitHubService{
		ghRepo:         ghRepo,
		issueRepo:      issueRepo,
		teamRepo:       teamRepo,
		teamStatusRepo: teamStatusRepo,
		historyRepo:    historyRepo,
		encryptionKey:  encryptionKey,
		hub:            hub,
		frontendURL:    frontendURL,
		webhookURL:     webhookURL,
		globalApp:      globalApp,
	}
}

// getClient creates a GitHub API client. In global mode, uses shared credentials;
// otherwise looks up per-workspace app config from the DB.
func (s *GitHubService) getClient(ctx context.Context, workspaceID uuid.UUID) (*gh.Client, *domain.GitHubAppConfig, error) {
	if s.globalApp != nil {
		client, err := gh.NewClient(s.globalApp.AppID, s.globalApp.PrivateKey)
		if err != nil {
			return nil, nil, fmt.Errorf("creating global GitHub client: %w", err)
		}
		slug := s.globalApp.Slug
		cfg := &domain.GitHubAppConfig{
			AppID:   s.globalApp.AppID,
			AppSlug: &slug,
		}
		return client, cfg, nil
	}

	cfg, err := s.ghRepo.GetAppConfigByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, nil, err
	}
	if cfg == nil {
		return nil, nil, fmt.Errorf("GitHub App not configured")
	}

	// Decrypt private key
	privateKey, err := crypto.Decrypt(cfg.PrivateKey, s.encryptionKey)
	if err != nil {
		return nil, nil, fmt.Errorf("decrypting private key: %w", err)
	}

	client, err := gh.NewClient(cfg.AppID, privateKey)
	if err != nil {
		return nil, nil, fmt.Errorf("creating GitHub client: %w", err)
	}

	return client, cfg, nil
}

// getClientByInstallationID creates a client by looking up the workspace from an installation.
func (s *GitHubService) getClientByInstallationID(ctx context.Context, installationID int64) (*gh.Client, *domain.GitHubInstallation, error) {
	inst, err := s.ghRepo.GetInstallationByGitHubID(ctx, installationID)
	if err != nil || inst == nil {
		return nil, nil, fmt.Errorf("unknown installation")
	}

	client, _, err := s.getClient(ctx, inst.WorkspaceID)
	if err != nil {
		return nil, nil, err
	}

	return client, inst, nil
}

// --- Manifest Flow ---

// GetManifest returns the GitHub App manifest and the URL to submit it to.
func (s *GitHubService) GetManifest(workspaceID uuid.UUID, slug string) (map[string]any, string) {
	callbackURL := fmt.Sprintf("%s/%s/settings/github", s.frontendURL, slug)

	// Determine if the frontend URL is publicly reachable
	isLocal := strings.Contains(s.frontendURL, "localhost") || strings.Contains(s.frontendURL, "127.0.0.1")

	manifest := map[string]any{
		"name":            fmt.Sprintf("Kuayle (%s)", slug),
		"url":             s.frontendURL,
		"redirect_url":    callbackURL,
		"setup_url":       callbackURL,
		"setup_on_update": true,
		"public":          false,
		"default_permissions": map[string]string{
			"pull_requests": "write",
			"contents":      "write",
			"metadata":      "read",
			"issues":        "read",
		},
		"default_events": []string{
			"pull_request",
			"push",
			"create",
		},
	}

	// Determine webhook URL: explicit override > auto-derive from domain > placeholder
	if s.webhookURL != "" {
		manifest["hook_attributes"] = map[string]any{"url": s.webhookURL, "active": true}
	} else if !isLocal {
		webhookURL := fmt.Sprintf("%s/api/github/webhook", strings.Replace(s.frontendURL, ":5173", ":8080", 1))
		manifest["hook_attributes"] = map[string]any{"url": webhookURL, "active": true}
	} else {
		manifest["hook_attributes"] = map[string]any{"url": "https://example.com/webhook", "active": false}
	}

	submitURL := "https://github.com/settings/apps/new"
	return manifest, submitURL
}

// HandleManifestCallback exchanges the temporary code from GitHub for app credentials.
func (s *GitHubService) HandleManifestCallback(ctx context.Context, workspaceID uuid.UUID, code string) (*domain.GitHubAppConfig, error) {
	if s.globalApp != nil {
		return nil, fmt.Errorf("GitHub App is centrally managed — manifest setup is not available")
	}

	// Check if already configured
	existing, err := s.ghRepo.GetAppConfigByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return existing, nil
	}

	// Exchange code for credentials
	log.WithField("workspace_id", workspaceID).Info("Exchanging manifest code with GitHub")
	convURL := fmt.Sprintf("https://api.github.com/app-manifests/%s/conversions", code)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, convURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating conversion request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("exchanging manifest code: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		log.WithFields(log.Fields{
			"status": resp.StatusCode,
		}).Warn("GitHub manifest conversion failed")
		return nil, fmt.Errorf("GitHub API error %d: %s", resp.StatusCode, body)
	}

	var result struct {
		ID            int64  `json:"id"`
		Slug          string `json:"slug"`
		ClientID      string `json:"client_id"`
		ClientSecret  string `json:"client_secret"`
		PEM           string `json:"pem"`
		WebhookSecret string `json:"webhook_secret"`
		HTMLURL       string `json:"html_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding manifest response: %w", err)
	}

	// Encrypt secrets
	encClientSecret, err := crypto.Encrypt(result.ClientSecret, s.encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("encrypting client secret: %w", err)
	}
	encPrivateKey, err := crypto.Encrypt(result.PEM, s.encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("encrypting private key: %w", err)
	}
	encWebhookSecret, err := crypto.Encrypt(result.WebhookSecret, s.encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("encrypting webhook secret: %w", err)
	}

	appCfg := &domain.GitHubAppConfig{
		ID:            uuid.New(),
		WorkspaceID:   workspaceID,
		AppID:         result.ID,
		AppSlug:       &result.Slug,
		ClientID:      result.ClientID,
		ClientSecret:  encClientSecret,
		PrivateKey:    encPrivateKey,
		WebhookSecret: encWebhookSecret,
		HTMLURL:       &result.HTMLURL,
	}

	if err := s.ghRepo.CreateAppConfig(ctx, appCfg); err != nil {
		return nil, err
	}

	return appCfg, nil
}

// --- Installation Flow ---

// GetInstallURL returns the GitHub App installation URL using the stored app slug.
func (s *GitHubService) GetInstallURL(ctx context.Context, workspaceID uuid.UUID) (string, error) {
	if s.globalApp != nil {
		slug := s.globalApp.Slug
		if slug == "" {
			slug = "kuayle"
		}
		return fmt.Sprintf("https://github.com/apps/%s/installations/new?state=%s", slug, workspaceID.String()), nil
	}

	cfg, err := s.ghRepo.GetAppConfigByWorkspace(ctx, workspaceID)
	if err != nil || cfg == nil {
		return "", fmt.Errorf("GitHub App not configured")
	}
	slug := "kuayle"
	if cfg.AppSlug != nil {
		slug = *cfg.AppSlug
	}
	return fmt.Sprintf("https://github.com/apps/%s/installations/new?state=%s", slug, workspaceID.String()), nil
}

// HandleInstallationCallback processes the GitHub App installation callback.
func (s *GitHubService) HandleInstallationCallback(ctx context.Context, workspaceID, userID uuid.UUID, installationID int64) (*domain.GitHubInstallation, error) {
	existing, err := s.ghRepo.GetInstallationByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return existing, nil
	}

	client, _, err := s.getClient(ctx, workspaceID)
	if err != nil {
		return nil, err
	}

	ghInst, err := client.GetInstallation(installationID)
	if err != nil {
		return nil, fmt.Errorf("fetching installation details: %w", err)
	}

	inst := &domain.GitHubInstallation{
		ID:             uuid.New(),
		WorkspaceID:    workspaceID,
		InstallationID: installationID,
		AccountLogin:   ghInst.Account.Login,
		AccountType:    ghInst.Account.Type,
		InstalledBy:    userID,
	}

	if err := s.ghRepo.CreateInstallation(ctx, inst); err != nil {
		return nil, err
	}

	// Seed default auto-transition rules
	defaults := []struct {
		Event  string
		Status string
	}{
		{"branch_created", "in_progress"},
		{"pr_opened", "in_review"},
		{"pr_merged", "done"},
	}
	for _, d := range defaults {
		t := &domain.GitHubAutoTransition{
			ID:           uuid.New(),
			WorkspaceID:  workspaceID,
			Event:        d.Event,
			TargetStatus: d.Status,
			IsActive:     true,
		}
		_ = s.ghRepo.UpsertAutoTransition(ctx, t)
	}

	return inst, nil
}

// GetStatus returns the current GitHub integration status for a workspace.
func (s *GitHubService) GetStatus(ctx context.Context, workspaceID uuid.UUID) (*dto.GitHubStatusResponse, error) {
	inst, _ := s.ghRepo.GetInstallationByWorkspace(ctx, workspaceID)

	resp := &dto.GitHubStatusResponse{
		Installed: inst != nil,
		Repos:     []dto.GitHubRepoResponse{},
	}

	if s.globalApp != nil {
		resp.Configured = true
		resp.GlobalApp = true
		resp.AppSlug = s.globalApp.Slug
	} else {
		appCfg, _ := s.ghRepo.GetAppConfigByWorkspace(ctx, workspaceID)
		if appCfg != nil {
			resp.Configured = true
			if appCfg.AppSlug != nil {
				resp.AppSlug = *appCfg.AppSlug
			}
		}
	}

	if inst != nil {
		resp.Installation = &dto.GitHubInstallationResponse{
			ID:             inst.ID.String(),
			InstallationID: inst.InstallationID,
			AccountLogin:   inst.AccountLogin,
			AccountType:    inst.AccountType,
			CreatedAt:      inst.CreatedAt.Format(time.RFC3339),
		}

		repos, err := s.ghRepo.ListReposByWorkspace(ctx, workspaceID)
		if err != nil {
			return nil, err
		}
		for _, r := range repos {
			resp.Repos = append(resp.Repos, dto.GitHubRepoResponse{
				ID:            r.ID.String(),
				GitHubRepoID:  r.GitHubRepoID,
				FullName:      r.FullName,
				DefaultBranch: r.DefaultBranch,
				IsActive:      r.IsActive,
			})
		}
	}

	return resp, nil
}

// ListAvailableRepos lists repos available to the GitHub App installation.
func (s *GitHubService) ListAvailableRepos(ctx context.Context, workspaceID uuid.UUID) ([]dto.GitHubAvailableRepoResponse, error) {
	inst, err := s.ghRepo.GetInstallationByWorkspace(ctx, workspaceID)
	if err != nil || inst == nil {
		return nil, fmt.Errorf("GitHub not connected")
	}

	client, _, err := s.getClient(ctx, workspaceID)
	if err != nil {
		return nil, err
	}

	token, _, err := client.GetInstallationToken(inst.InstallationID)
	if err != nil {
		return nil, fmt.Errorf("getting installation token: %w", err)
	}

	ghRepos, err := client.ListInstallationRepos(token)
	if err != nil {
		return nil, fmt.Errorf("listing repos: %w", err)
	}

	linkedRepos, err := s.ghRepo.ListReposByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	linkedMap := make(map[int64]bool)
	for _, lr := range linkedRepos {
		linkedMap[lr.GitHubRepoID] = true
	}

	var result []dto.GitHubAvailableRepoResponse
	for _, r := range ghRepos {
		result = append(result, dto.GitHubAvailableRepoResponse{
			GitHubRepoID:  r.ID,
			FullName:      r.FullName,
			DefaultBranch: r.DefaultBranch,
			Private:       r.Private,
			Linked:        linkedMap[r.ID],
		})
	}
	return result, nil
}

// LinkRepos links GitHub repos to the workspace.
func (s *GitHubService) LinkRepos(ctx context.Context, workspaceID uuid.UUID, req dto.LinkGitHubReposRequest) error {
	inst, err := s.ghRepo.GetInstallationByWorkspace(ctx, workspaceID)
	if err != nil || inst == nil {
		return fmt.Errorf("GitHub not connected")
	}

	client, _, err := s.getClient(ctx, workspaceID)
	if err != nil {
		return err
	}

	token, _, err := client.GetInstallationToken(inst.InstallationID)
	if err != nil {
		return fmt.Errorf("getting installation token: %w", err)
	}

	ghRepos, err := client.ListInstallationRepos(token)
	if err != nil {
		return fmt.Errorf("listing repos: %w", err)
	}

	repoMap := make(map[int64]gh.Repository)
	for _, r := range ghRepos {
		repoMap[r.ID] = r
	}

	for _, id := range req.GitHubRepoIDs {
		ghRepo, ok := repoMap[id]
		if !ok {
			continue
		}
		existing, _ := s.ghRepo.GetRepoByGitHubID(ctx, workspaceID, id)
		if existing != nil {
			continue
		}
		repo := &domain.GitHubRepoModel{
			ID:             uuid.New(),
			InstallationID: inst.ID,
			WorkspaceID:    workspaceID,
			GitHubRepoID:   ghRepo.ID,
			FullName:       ghRepo.FullName,
			DefaultBranch:  ghRepo.DefaultBranch,
			IsActive:       true,
		}
		if err := s.ghRepo.CreateRepo(ctx, repo); err != nil {
			log.WithError(err).WithField("repo", ghRepo.FullName).Warn("failed to link repo")
		}
	}
	return nil
}

// UnlinkRepo removes a linked repo.
func (s *GitHubService) UnlinkRepo(ctx context.Context, repoID uuid.UUID) error {
	return s.ghRepo.DeleteRepo(ctx, repoID)
}

// Disconnect removes the GitHub installation (keeps app config).
func (s *GitHubService) Disconnect(ctx context.Context, workspaceID uuid.UUID) error {
	return s.ghRepo.DeleteInstallation(ctx, workspaceID)
}

// DeleteApp removes the entire GitHub App configuration and installation.
// In global mode, only disconnects the installation (the shared app config is not in DB).
func (s *GitHubService) DeleteApp(ctx context.Context, workspaceID uuid.UUID) error {
	_ = s.ghRepo.DeleteInstallation(ctx, workspaceID)
	if s.globalApp == nil {
		return s.ghRepo.DeleteAppConfig(ctx, workspaceID)
	}
	return nil
}

// --- Webhook Event Processing ---

// VerifyWebhookSignature verifies the GitHub webhook HMAC-SHA256 signature.
// In global mode, uses the shared webhook secret directly.
// In self-hosted mode, looks up the webhook secret from the DB using the installation ID.
func (s *GitHubService) VerifyWebhookSignature(ctx context.Context, payload []byte, signature string) bool {
	if s.globalApp != nil {
		sig := strings.TrimPrefix(signature, "sha256=")
		mac := hmac.New(sha256.New, []byte(s.globalApp.WebhookSecret))
		mac.Write(payload)
		expected := hex.EncodeToString(mac.Sum(nil))
		return hmac.Equal([]byte(sig), []byte(expected))
	}

	// Self-hosted: extract installation ID from payload to look up the webhook secret
	var partial struct {
		Installation struct {
			ID int64 `json:"id"`
		} `json:"installation"`
	}
	if err := json.Unmarshal(payload, &partial); err != nil || partial.Installation.ID == 0 {
		return false
	}

	inst, err := s.ghRepo.GetInstallationByGitHubID(ctx, partial.Installation.ID)
	if err != nil || inst == nil {
		return false
	}

	cfg, err := s.ghRepo.GetAppConfigByWorkspace(ctx, inst.WorkspaceID)
	if err != nil || cfg == nil {
		return false
	}

	secret, err := crypto.Decrypt(cfg.WebhookSecret, s.encryptionKey)
	if err != nil {
		return false
	}

	sig := strings.TrimPrefix(signature, "sha256=")
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(sig), []byte(expected))
}

// HandleWebhookEvent processes an incoming GitHub webhook event.
func (s *GitHubService) HandleWebhookEvent(ctx context.Context, eventType string, payload []byte) error {
	switch eventType {
	case "pull_request":
		return s.processPullRequestEvent(ctx, payload)
	case "push":
		return s.processPushEvent(ctx, payload)
	case "create":
		return s.processCreateEvent(ctx, payload)
	case "installation":
		return s.processInstallationEvent(ctx, payload)
	default:
		return nil
	}
}

func (s *GitHubService) broadcastAppRefresh(workspaceID uuid.UUID, resources ...string) {
	if s.hub == nil {
		return
	}
	s.hub.Broadcast(workspaceID, realtime.Event{
		Type: "app.refresh",
		Payload: map[string]any{
			"source":    "github",
			"resources": resources,
		},
	})
}

func (s *GitHubService) broadcastRealtimeEvent(workspaceID uuid.UUID, event realtime.Event) {
	if s.hub == nil {
		return
	}
	s.hub.Broadcast(workspaceID, event)
}

type ghWebhookPR struct {
	Action      string `json:"action"`
	Number      int    `json:"number"`
	PullRequest struct {
		ID      int64  `json:"id"`
		Number  int    `json:"number"`
		Title   string `json:"title"`
		State   string `json:"state"`
		Draft   bool   `json:"draft"`
		Merged  bool   `json:"merged"`
		HTMLURL string `json:"html_url"`
		Head    struct {
			Ref string `json:"ref"`
		} `json:"head"`
		Base struct {
			Ref string `json:"ref"`
		} `json:"base"`
		User struct {
			Login     string `json:"login"`
			AvatarURL string `json:"avatar_url"`
		} `json:"user"`
		Additions int        `json:"additions"`
		Deletions int        `json:"deletions"`
		MergedAt  *time.Time `json:"merged_at"`
		ClosedAt  *time.Time `json:"closed_at"`
		Body      string     `json:"body"`
	} `json:"pull_request"`
	Repository struct {
		ID int64 `json:"id"`
	} `json:"repository"`
	Installation struct {
		ID int64 `json:"id"`
	} `json:"installation"`
}

func (s *GitHubService) processPullRequestEvent(ctx context.Context, payload []byte) error {
	var event ghWebhookPR
	if err := json.Unmarshal(payload, &event); err != nil {
		return fmt.Errorf("unmarshaling PR event: %w", err)
	}

	inst, err := s.ghRepo.GetInstallationByGitHubID(ctx, event.Installation.ID)
	if err != nil || inst == nil {
		return nil
	}

	repo, err := s.ghRepo.GetRepoByGitHubID(ctx, inst.WorkspaceID, event.Repository.ID)
	if err != nil || repo == nil {
		return nil
	}

	state := event.PullRequest.State
	if event.PullRequest.Draft {
		state = "draft"
	}
	if event.PullRequest.Merged {
		state = "merged"
	}

	issue := s.resolveIssueFromRef(ctx, inst.WorkspaceID,
		event.PullRequest.Head.Ref,
		event.PullRequest.Title,
		event.PullRequest.Body,
	)

	var issueID *uuid.UUID
	if issue != nil {
		issueID = &issue.ID
	}

	avatarURL := event.PullRequest.User.AvatarURL
	headRef := event.PullRequest.Head.Ref
	baseRef := event.PullRequest.Base.Ref

	pr := &domain.GitHubPullRequest{
		ID:              uuid.New(),
		WorkspaceID:     inst.WorkspaceID,
		IssueID:         issueID,
		GitHubRepoID:    repo.ID,
		GitHubPRID:      event.PullRequest.ID,
		Number:          event.PullRequest.Number,
		Title:           event.PullRequest.Title,
		State:           state,
		AuthorLogin:     event.PullRequest.User.Login,
		AuthorAvatarURL: &avatarURL,
		HTMLURL:         event.PullRequest.HTMLURL,
		HeadBranch:      &headRef,
		BaseBranch:      &baseRef,
		Additions:       event.PullRequest.Additions,
		Deletions:       event.PullRequest.Deletions,
		MergedAt:        event.PullRequest.MergedAt,
		ClosedAt:        event.PullRequest.ClosedAt,
	}

	if err := s.ghRepo.UpsertPullRequest(ctx, pr); err != nil {
		return fmt.Errorf("upserting PR: %w", err)
	}

	if issue != nil {
		switch {
		case event.Action == "opened" || event.Action == "ready_for_review":
			s.applyAutoTransition(ctx, inst.WorkspaceID, "pr_opened", issue)
		case event.PullRequest.Merged:
			s.applyAutoTransition(ctx, inst.WorkspaceID, "pr_merged", issue)
		}

		s.broadcastRealtimeEvent(inst.WorkspaceID, realtime.Event{
			Type:    "github:pr_updated",
			Payload: map[string]any{"issue_id": issue.ID, "pr_number": pr.Number, "state": state},
		})
		s.broadcastAppRefresh(inst.WorkspaceID, "issues")
	}

	return nil
}

type ghWebhookPush struct {
	Ref     string `json:"ref"`
	Commits []struct {
		ID        string `json:"id"`
		Message   string `json:"message"`
		URL       string `json:"url"`
		Timestamp string `json:"timestamp"`
		Author    struct {
			Name     string `json:"name"`
			Username string `json:"username"`
		} `json:"author"`
	} `json:"commits"`
	Repository struct {
		ID int64 `json:"id"`
	} `json:"repository"`
	Installation struct {
		ID int64 `json:"id"`
	} `json:"installation"`
}

func (s *GitHubService) processPushEvent(ctx context.Context, payload []byte) error {
	var event ghWebhookPush
	if err := json.Unmarshal(payload, &event); err != nil {
		return fmt.Errorf("unmarshaling push event: %w", err)
	}

	inst, err := s.ghRepo.GetInstallationByGitHubID(ctx, event.Installation.ID)
	if err != nil || inst == nil {
		return nil
	}

	repo, err := s.ghRepo.GetRepoByGitHubID(ctx, inst.WorkspaceID, event.Repository.ID)
	if err != nil || repo == nil {
		return nil
	}

	updatedIssues := make(map[uuid.UUID]bool)
	for _, c := range event.Commits {
		issue := s.resolveIssueFromRef(ctx, inst.WorkspaceID, c.Message)
		var issueID *uuid.UUID
		if issue != nil {
			issueID = &issue.ID
		}

		committedAt, _ := time.Parse(time.RFC3339, c.Timestamp)
		if committedAt.IsZero() {
			committedAt = time.Now()
		}

		username := c.Author.Username
		commit := &domain.GitHubCommit{
			ID:           uuid.New(),
			WorkspaceID:  inst.WorkspaceID,
			IssueID:      issueID,
			GitHubRepoID: repo.ID,
			SHA:          c.ID,
			Message:      c.Message,
			AuthorLogin:  &username,
			HTMLURL:      c.URL,
			CommittedAt:  committedAt,
		}

		if err := s.ghRepo.UpsertCommit(ctx, commit); err != nil {
			log.WithError(err).WithField("sha", c.ID).Warn("failed to upsert commit")
			continue
		}

		if issue != nil {
			updatedIssues[issue.ID] = true
			s.broadcastRealtimeEvent(inst.WorkspaceID, realtime.Event{
				Type:    "github:commit_pushed",
				Payload: map[string]any{"issue_id": issue.ID, "sha": commit.SHA},
			})
		}
	}
	if len(updatedIssues) > 0 {
		s.broadcastAppRefresh(inst.WorkspaceID, "issues")
	}

	return nil
}

type ghWebhookCreate struct {
	RefType    string `json:"ref_type"`
	Ref        string `json:"ref"`
	Repository struct {
		ID      int64  `json:"id"`
		HTMLURL string `json:"html_url"`
	} `json:"repository"`
	Installation struct {
		ID int64 `json:"id"`
	} `json:"installation"`
}

func (s *GitHubService) processCreateEvent(ctx context.Context, payload []byte) error {
	var event ghWebhookCreate
	if err := json.Unmarshal(payload, &event); err != nil {
		return fmt.Errorf("unmarshaling create event: %w", err)
	}

	if event.RefType != "branch" {
		return nil
	}

	inst, err := s.ghRepo.GetInstallationByGitHubID(ctx, event.Installation.ID)
	if err != nil || inst == nil {
		return nil
	}

	repo, err := s.ghRepo.GetRepoByGitHubID(ctx, inst.WorkspaceID, event.Repository.ID)
	if err != nil || repo == nil {
		return nil
	}

	issue := s.resolveIssueFromRef(ctx, inst.WorkspaceID, event.Ref)
	var issueID *uuid.UUID
	if issue != nil {
		issueID = &issue.ID
	}

	branchURL := fmt.Sprintf("%s/tree/%s", event.Repository.HTMLURL, event.Ref)
	branch := &domain.GitHubBranch{
		ID:           uuid.New(),
		WorkspaceID:  inst.WorkspaceID,
		IssueID:      issueID,
		GitHubRepoID: repo.ID,
		Name:         event.Ref,
		HTMLURL:      &branchURL,
	}

	if err := s.ghRepo.UpsertBranch(ctx, branch); err != nil {
		return fmt.Errorf("upserting branch: %w", err)
	}

	if issue != nil {
		s.applyAutoTransition(ctx, inst.WorkspaceID, "branch_created", issue)
		s.broadcastRealtimeEvent(inst.WorkspaceID, realtime.Event{
			Type:    "github:branch_created",
			Payload: map[string]any{"issue_id": issue.ID, "branch": event.Ref},
		})
		s.broadcastAppRefresh(inst.WorkspaceID, "issues")
	}

	return nil
}

type ghWebhookInstallation struct {
	Action       string `json:"action"`
	Installation struct {
		ID      int64 `json:"id"`
		Account struct {
			Login string `json:"login"`
			Type  string `json:"type"`
		} `json:"account"`
	} `json:"installation"`
}

func (s *GitHubService) processInstallationEvent(ctx context.Context, payload []byte) error {
	var event ghWebhookInstallation
	if err := json.Unmarshal(payload, &event); err != nil {
		return fmt.Errorf("unmarshaling installation event: %w", err)
	}

	if event.Action == "deleted" || event.Action == "suspend" {
		inst, err := s.ghRepo.GetInstallationByGitHubID(ctx, event.Installation.ID)
		if err != nil || inst == nil {
			return nil
		}
		return s.ghRepo.DeleteInstallation(ctx, inst.WorkspaceID)
	}

	return nil
}

// --- Issue Linking ---

func (s *GitHubService) resolveIssueFromRef(ctx context.Context, workspaceID uuid.UUID, texts ...string) *domain.Issue {
	seen := make(map[string]bool)
	for _, text := range texts {
		matches := issueIdentifierRegex.FindAllString(text, -1)
		for _, m := range matches {
			if seen[m] {
				continue
			}
			seen[m] = true
			issue, err := s.issueRepo.GetByIdentifier(ctx, workspaceID, strings.ToUpper(m))
			if err == nil && issue != nil {
				return issue
			}
		}
	}
	return nil
}

// --- Auto-Transitions ---

func (s *GitHubService) applyAutoTransition(ctx context.Context, workspaceID uuid.UUID, event string, issue *domain.Issue) {
	rule, err := s.ghRepo.GetAutoTransitionByEvent(ctx, workspaceID, event)
	if err != nil || rule == nil {
		return
	}

	oldStatus := string(issue.Status)
	newStatus := rule.TargetStatus
	if oldStatus == newStatus {
		return
	}

	issue.Status = domain.IssueStatus(newStatus)
	if rule.TargetStatusID != nil {
		issue.StatusID = rule.TargetStatusID
	} else {
		// Resolve the team's custom status ID from the slug
		ts, err := s.teamStatusRepo.GetByTeamAndSlug(ctx, issue.TeamID, newStatus)
		if err == nil && ts != nil {
			issue.StatusID = &ts.ID
		}
	}

	if err := s.issueRepo.Update(ctx, issue); err != nil {
		log.WithError(err).WithField("issue_id", issue.ID).Warn("failed to auto-transition issue")
		return
	}

	_ = s.historyRepo.Create(ctx, issue.ID, uuid.Nil, "status", &oldStatus, &newStatus)

	s.broadcastRealtimeEvent(workspaceID, realtime.Event{
		Type:    "issue.updated",
		Payload: issue,
	})
	s.applyStatusAutomation(ctx, workspaceID, issue, map[uuid.UUID]bool{})
}

func (s *GitHubService) applyStatusAutomation(ctx context.Context, workspaceID uuid.UUID, issue *domain.Issue, visited map[uuid.UUID]bool) {
	if issue == nil || visited[issue.ID] || s.teamRepo == nil {
		return
	}
	visited[issue.ID] = true

	category := s.issueStatusCategory(ctx, issue)
	if category == domain.StatusCategoryCompleted {
		team, err := s.teamRepo.GetByID(ctx, issue.TeamID)
		if err == nil && team != nil && team.SubIssueAutoCloseEnabled {
			s.autoCloseSubIssues(ctx, workspaceID, issue.ID, visited)
		}
	}
	if issue.ParentID != nil {
		s.maybeAutoCloseParent(ctx, workspaceID, *issue.ParentID, visited)
	}
}

func (s *GitHubService) maybeAutoCloseParent(ctx context.Context, workspaceID, parentID uuid.UUID, visited map[uuid.UUID]bool) {
	parent, err := s.issueRepo.GetByID(ctx, parentID)
	if err != nil || parent == nil || parent.WorkspaceID != workspaceID || visited[parent.ID] || s.teamRepo == nil {
		return
	}
	team, err := s.teamRepo.GetByID(ctx, parent.TeamID)
	if err != nil || team == nil || !team.ParentAutoCloseEnabled {
		return
	}
	total, done, err := s.issueRepo.CountSubIssues(ctx, parent.ID)
	if err != nil || total == 0 || total != done {
		return
	}
	s.moveIssueToCompleted(ctx, workspaceID, parent, visited)
}

func (s *GitHubService) autoCloseSubIssues(ctx context.Context, workspaceID, parentID uuid.UUID, visited map[uuid.UUID]bool) {
	subIssues, err := s.issueRepo.ListSubIssues(ctx, parentID)
	if err != nil {
		return
	}
	for i := range subIssues {
		sub := subIssues[i]
		if sub.WorkspaceID != workspaceID || s.isTerminalStatus(ctx, &sub) {
			continue
		}
		s.moveIssueToCompleted(ctx, workspaceID, &sub, visited)
	}
}

func (s *GitHubService) moveIssueToCompleted(ctx context.Context, workspaceID uuid.UUID, issue *domain.Issue, visited map[uuid.UUID]bool) {
	if issue == nil || s.isTerminalStatus(ctx, issue) {
		return
	}
	completedStatus, err := s.completedStatusForTeam(ctx, issue.TeamID)
	if err != nil || completedStatus == nil {
		return
	}

	old := string(issue.Status)
	issue.Status = domain.IssueStatus(completedStatus.Slug)
	issue.StatusID = &completedStatus.ID
	if err := s.issueRepo.Update(ctx, issue); err != nil {
		log.WithError(err).WithField("issue_id", issue.ID).Warn("failed to auto-close issue")
		return
	}
	newVal := completedStatus.Slug
	_ = s.historyRepo.Create(ctx, issue.ID, uuid.Nil, "status", &old, &newVal)
	s.broadcastRealtimeEvent(workspaceID, realtime.Event{Type: "issue.updated", Payload: issue})
	s.applyStatusAutomation(ctx, workspaceID, issue, visited)
}

func (s *GitHubService) completedStatusForTeam(ctx context.Context, teamID uuid.UUID) (*domain.TeamStatus, error) {
	status, err := s.teamStatusRepo.GetByTeamAndSlug(ctx, teamID, string(domain.IssueStatusDone))
	if err == nil && status != nil {
		return status, nil
	}
	statuses, err := s.teamStatusRepo.ListByTeam(ctx, teamID)
	if err != nil {
		return nil, err
	}
	for i := range statuses {
		if statuses[i].Category == domain.StatusCategoryCompleted {
			return &statuses[i], nil
		}
	}
	return nil, nil
}

func (s *GitHubService) isTerminalStatus(ctx context.Context, issue *domain.Issue) bool {
	category := s.issueStatusCategory(ctx, issue)
	return category == domain.StatusCategoryCompleted || category == domain.StatusCategoryCancelled
}

func (s *GitHubService) issueStatusCategory(ctx context.Context, issue *domain.Issue) domain.StatusCategory {
	if issue.StatusID != nil {
		status, err := s.teamStatusRepo.GetByID(ctx, *issue.StatusID)
		if err == nil && status != nil {
			return status.Category
		}
	}
	switch issue.Status {
	case domain.IssueStatusDone:
		return domain.StatusCategoryCompleted
	case domain.IssueStatusCancelled:
		return domain.StatusCategoryCancelled
	case domain.IssueStatusInProgress, domain.IssueStatusInReview:
		return domain.StatusCategoryStarted
	case domain.IssueStatusTodo:
		return domain.StatusCategoryUnstarted
	default:
		return domain.StatusCategoryBacklog
	}
}

// --- Data Access ---

func (s *GitHubService) GetIssueActivity(ctx context.Context, workspaceID uuid.UUID, identifier string) (*dto.GitHubIssueActivityResponse, error) {
	issue, err := s.issueRepo.GetByIdentifier(ctx, workspaceID, identifier)
	if err != nil || issue == nil {
		return nil, fmt.Errorf("issue not found")
	}

	prs, _ := s.ghRepo.ListPRsWithRepoByIssue(ctx, issue.ID)
	branches, _ := s.ghRepo.ListBranchesWithRepoByIssue(ctx, issue.ID)
	commits, _ := s.ghRepo.ListCommitsWithRepoByIssue(ctx, issue.ID)

	resp := &dto.GitHubIssueActivityResponse{
		PullRequests: make([]dto.GitHubPullRequestResponse, 0, len(prs)),
		Branches:     make([]dto.GitHubBranchResponse, 0, len(branches)),
		Commits:      make([]dto.GitHubCommitResponse, 0, len(commits)),
	}

	for _, pr := range prs {
		avatarURL, headBranch, baseBranch := "", "", ""
		if pr.AuthorAvatarURL != nil {
			avatarURL = *pr.AuthorAvatarURL
		}
		if pr.HeadBranch != nil {
			headBranch = *pr.HeadBranch
		}
		if pr.BaseBranch != nil {
			baseBranch = *pr.BaseBranch
		}
		resp.PullRequests = append(resp.PullRequests, dto.GitHubPullRequestResponse{
			ID: pr.ID.String(), Number: pr.Number, Title: pr.Title, State: pr.State,
			AuthorLogin: pr.AuthorLogin, AuthorAvatarURL: avatarURL, HTMLURL: pr.HTMLURL,
			HeadBranch: headBranch, BaseBranch: baseBranch,
			Additions: pr.Additions, Deletions: pr.Deletions,
			RepoFullName: pr.RepoFullName, MergedAt: pr.MergedAt,
			CreatedAt: pr.CreatedAt, UpdatedAt: pr.UpdatedAt,
		})
	}

	for _, b := range branches {
		htmlURL := ""
		if b.HTMLURL != nil {
			htmlURL = *b.HTMLURL
		}
		resp.Branches = append(resp.Branches, dto.GitHubBranchResponse{
			ID: b.ID.String(), Name: b.Name, HTMLURL: htmlURL, RepoFullName: b.RepoFullName,
		})
	}

	for _, c := range commits {
		authorLogin, authorAvatar := "", ""
		if c.AuthorLogin != nil {
			authorLogin = *c.AuthorLogin
		}
		if c.AuthorAvatarURL != nil {
			authorAvatar = *c.AuthorAvatarURL
		}
		shortSHA := c.SHA
		if len(shortSHA) > 7 {
			shortSHA = shortSHA[:7]
		}
		resp.Commits = append(resp.Commits, dto.GitHubCommitResponse{
			ID: c.ID.String(), SHA: c.SHA, ShortSHA: shortSHA, Message: c.Message,
			AuthorLogin: authorLogin, AuthorAvatarURL: authorAvatar,
			HTMLURL: c.HTMLURL, RepoFullName: c.RepoFullName, CommittedAt: c.CommittedAt,
		})
	}

	return resp, nil
}

// --- Auto Transition Management ---

func (s *GitHubService) ListAutoTransitions(ctx context.Context, workspaceID uuid.UUID) ([]dto.GitHubAutoTransitionResponse, error) {
	transitions, err := s.ghRepo.ListAutoTransitions(ctx, workspaceID)
	if err != nil {
		return nil, err
	}

	var result []dto.GitHubAutoTransitionResponse
	for _, t := range transitions {
		var statusID *string
		if t.TargetStatusID != nil {
			s := t.TargetStatusID.String()
			statusID = &s
		}
		result = append(result, dto.GitHubAutoTransitionResponse{
			Event: t.Event, TargetStatus: t.TargetStatus, TargetStatusID: statusID, IsActive: t.IsActive,
		})
	}
	return result, nil
}

func (s *GitHubService) UpdateAutoTransitions(ctx context.Context, workspaceID uuid.UUID, req dto.UpdateAutoTransitionsRequest) error {
	for _, rule := range req.Transitions {
		var statusID *uuid.UUID
		if rule.TargetStatusID != nil {
			parsed, err := uuid.Parse(*rule.TargetStatusID)
			if err == nil {
				statusID = &parsed
			}
		}
		t := &domain.GitHubAutoTransition{
			ID: uuid.New(), WorkspaceID: workspaceID,
			Event: rule.Event, TargetStatus: rule.TargetStatus, TargetStatusID: statusID, IsActive: rule.IsActive,
		}
		if err := s.ghRepo.UpsertAutoTransition(ctx, t); err != nil {
			return err
		}
	}
	return nil
}
