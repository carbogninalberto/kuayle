package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/realtime"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/pkg/crypto"
	forgepkg "github.com/kuayle/kuayle-backend/pkg/forge"
	"github.com/kuayle/kuayle-backend/pkg/forge/gitea"
	"github.com/kuayle/kuayle-backend/pkg/forge/gitlab"
	"github.com/kuayle/kuayle-backend/pkg/validate"
	log "github.com/sirupsen/logrus"
)

var (
	ErrForgeUnauthorized       = errors.New("forge webhook could not be verified")
	ErrForgeDeliveryID         = errors.New("forge webhook delivery ID is invalid")
	ErrForgeConnectionNotFound = errors.New("forge connection not found")
	ErrForgeRepoNotFound       = errors.New("linked forge repository not found")
)

// ForgeService configures GitLab, Gitea and Forgejo connections and processes
// their webhooks through the shared linking and automation rules. GitHub keeps
// its dedicated service; both use the same forgeLinker.
type ForgeService struct {
	repo           *repository.ForgeRepository
	rules          *repository.GitHubRepository
	issueRepo      repository.IssueRepo
	teamRepo       repository.TeamRepo
	teamStatusRepo repository.TeamStatusRepo
	historyRepo    repository.IssueHistoryRepo
	encryptionKey  []byte
	hub            *realtime.Hub
	frontendURL    string
	allowPrivate   bool
	webhooks       map[domain.ForgeProvider]domain.ForgeWebhook
	commenters     map[domain.ForgeProvider]domain.ForgeCommenter
}

func NewForgeService(
	repo *repository.ForgeRepository,
	rules *repository.GitHubRepository,
	issueRepo repository.IssueRepo,
	teamRepo repository.TeamRepo,
	teamStatusRepo repository.TeamStatusRepo,
	historyRepo repository.IssueHistoryRepo,
	encryptionKey []byte,
	hub *realtime.Hub,
	frontendURL string,
	allowPrivateNetwork bool,
) *ForgeService {
	client := forgepkg.NewHTTPClient(allowPrivateNetwork)
	gitlabProvider := gitlab.New(client)
	giteaProvider := gitea.New(domain.ForgeGitea, client)
	forgejoProvider := gitea.New(domain.ForgeForgejo, client)
	return &ForgeService{
		repo: repo, rules: rules, issueRepo: issueRepo, teamRepo: teamRepo,
		teamStatusRepo: teamStatusRepo, historyRepo: historyRepo,
		encryptionKey: encryptionKey, hub: hub, frontendURL: frontendURL,
		allowPrivate: allowPrivateNetwork,
		webhooks: map[domain.ForgeProvider]domain.ForgeWebhook{
			domain.ForgeGitLab:  gitlabProvider,
			domain.ForgeGitea:   giteaProvider,
			domain.ForgeForgejo: forgejoProvider,
		},
		commenters: map[domain.ForgeProvider]domain.ForgeCommenter{
			domain.ForgeGitLab:  gitlabProvider,
			domain.ForgeGitea:   giteaProvider,
			domain.ForgeForgejo: forgejoProvider,
		},
	}
}

func (s *ForgeService) linker() *forgeLinker {
	return &forgeLinker{
		issueRepo:      s.issueRepo,
		teamRepo:       s.teamRepo,
		teamStatusRepo: s.teamStatusRepo,
		historyRepo:    s.historyRepo,
		rules:          s.rules,
		broadcast:      s.broadcast,
	}
}

func (s *ForgeService) broadcast(workspaceID uuid.UUID, event realtime.Event) {
	if s.hub == nil {
		return
	}
	s.hub.Broadcast(workspaceID, event)
}

func (s *ForgeService) broadcastAppRefresh(workspaceID uuid.UUID, resources ...string) {
	s.broadcast(workspaceID, realtime.Event{
		Type: "app.refresh",
		Payload: map[string]any{
			"source":    "forge",
			"resources": resources,
		},
	})
}

func (s *ForgeService) webhookURL(id uuid.UUID) string {
	base := strings.TrimRight(s.frontendURL, "/")
	base = strings.Replace(base, ":5173", ":8080", 1)
	return base + "/api/forges/webhook/" + id.String()
}

func (s *ForgeService) normalizeBaseURL(raw string) (string, error) {
	parsed, err := validate.OperatorBaseURL(raw)
	if err != nil {
		return "", err
	}
	if parsed.Scheme != "https" && !s.allowPrivate {
		return "", errors.New("must use https; set FORGE_ALLOW_PRIVATE_NETWORK for a private http instance")
	}
	return parsed.String(), nil
}

func validForgeProvider(provider string) bool {
	switch domain.ForgeProvider(provider) {
	case domain.ForgeGitLab, domain.ForgeGitea, domain.ForgeForgejo:
		return true
	default:
		return false
	}
}

func (s *ForgeService) connectionResponse(conn *domain.ForgeConnection) dto.ForgeConnectionResponse {
	return dto.ForgeConnectionResponse{
		ID:               conn.ID.String(),
		Provider:         string(conn.Provider),
		Name:             conn.Name,
		BaseURL:          conn.BaseURL,
		HasAccessToken:   conn.AccessToken != nil && *conn.AccessToken != "",
		HasWebhookSecret: conn.WebhookSecret != "",
		WebhookURL:       s.webhookURL(conn.ID),
		CreatedAt:        conn.CreatedAt,
		UpdatedAt:        conn.UpdatedAt,
	}
}

func (s *ForgeService) repoResponse(repo *domain.ForgeRepo) dto.ForgeRepoResponse {
	return dto.ForgeRepoResponse{
		ID:             repo.ID.String(),
		ConnectionID:   repo.ConnectionID.String(),
		ExternalRepoID: repo.ExternalRepoID,
		FullName:       repo.FullName,
		WebURL:         repo.WebURL,
		DefaultBranch:  repo.DefaultBranch,
		IsActive:       repo.IsActive,
	}
}

// --- Configuration ---

func (s *ForgeService) CreateConnection(ctx context.Context, workspaceID, userID uuid.UUID, req dto.CreateForgeConnectionRequest) (*dto.ForgeConnectionResponse, error) {
	if !validForgeProvider(req.Provider) {
		return nil, errors.New("provider must be gitlab, gitea or forgejo")
	}
	baseURL, err := s.normalizeBaseURL(req.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("base URL %w", err)
	}
	if req.WebhookSecret == "" {
		return nil, errors.New("webhook secret is required")
	}
	encSecret, err := crypto.Encrypt(req.WebhookSecret, s.encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("encrypting webhook secret: %w", err)
	}
	var encToken *string
	if req.AccessToken != "" {
		token, err := crypto.Encrypt(req.AccessToken, s.encryptionKey)
		if err != nil {
			return nil, fmt.Errorf("encrypting access token: %w", err)
		}
		encToken = &token
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = req.Provider
	}
	conn := &domain.ForgeConnection{
		ID:            uuid.New(),
		WorkspaceID:   workspaceID,
		Provider:      domain.ForgeProvider(req.Provider),
		Name:          name,
		BaseURL:       baseURL,
		WebhookSecret: encSecret,
		AccessToken:   encToken,
		CreatedBy:     userID,
	}
	if err := s.repo.CreateConnection(ctx, conn); err != nil {
		return nil, err
	}
	// Forges share GitHub's automation rules. Insert only the defaults that
	// are missing so an admin's edited rules survive a second connection.
	if err := s.rules.SeedAutoTransitionDefaults(ctx, workspaceID); err != nil {
		log.WithError(err).WithField("workspace_id", workspaceID).Warn("failed to seed forge automation defaults")
	}
	resp := s.connectionResponse(conn)
	return &resp, nil
}

func (s *ForgeService) ListConnections(ctx context.Context, workspaceID uuid.UUID) ([]dto.ForgeConnectionResponse, error) {
	conns, err := s.repo.ListConnections(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	result := make([]dto.ForgeConnectionResponse, 0, len(conns))
	for i := range conns {
		result = append(result, s.connectionResponse(&conns[i]))
	}
	return result, nil
}

func (s *ForgeService) UpdateConnection(ctx context.Context, workspaceID, id uuid.UUID, req dto.UpdateForgeConnectionRequest) (*dto.ForgeConnectionResponse, error) {
	conn, err := s.repo.GetConnectionByWorkspace(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	if conn == nil {
		return nil, ErrForgeConnectionNotFound
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			name = string(conn.Provider)
		}
		conn.Name = name
	}
	if req.BaseURL != nil {
		baseURL, err := s.normalizeBaseURL(*req.BaseURL)
		if err != nil {
			return nil, fmt.Errorf("base URL %w", err)
		}
		conn.BaseURL = baseURL
	}
	if req.WebhookSecret != nil {
		if *req.WebhookSecret == "" {
			return nil, errors.New("webhook secret is required")
		}
		secret, err := crypto.Encrypt(*req.WebhookSecret, s.encryptionKey)
		if err != nil {
			return nil, fmt.Errorf("encrypting webhook secret: %w", err)
		}
		conn.WebhookSecret = secret
	}
	if req.AccessToken != nil {
		if *req.AccessToken == "" {
			conn.AccessToken = nil
		} else {
			token, err := crypto.Encrypt(*req.AccessToken, s.encryptionKey)
			if err != nil {
				return nil, fmt.Errorf("encrypting access token: %w", err)
			}
			conn.AccessToken = &token
		}
	}
	if err := s.repo.UpdateConnection(ctx, conn); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrForgeConnectionNotFound
		}
		return nil, err
	}
	resp := s.connectionResponse(conn)
	return &resp, nil
}

func (s *ForgeService) DeleteConnection(ctx context.Context, workspaceID, id uuid.UUID) error {
	if err := s.repo.DeleteConnection(ctx, workspaceID, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrForgeConnectionNotFound
		}
		return err
	}
	return nil
}

func (s *ForgeService) ListRepos(ctx context.Context, workspaceID, connectionID uuid.UUID) ([]dto.ForgeRepoResponse, error) {
	conn, err := s.repo.GetConnectionByWorkspace(ctx, workspaceID, connectionID)
	if err != nil {
		return nil, err
	}
	if conn == nil {
		return nil, ErrForgeConnectionNotFound
	}
	repos, err := s.repo.ListReposByConnection(ctx, workspaceID, connectionID)
	if err != nil {
		return nil, err
	}
	result := make([]dto.ForgeRepoResponse, 0, len(repos))
	for i := range repos {
		result = append(result, s.repoResponse(&repos[i]))
	}
	return result, nil
}

func (s *ForgeService) LinkRepo(ctx context.Context, workspaceID, connectionID uuid.UUID, req dto.LinkForgeRepoRequest) (*dto.ForgeRepoResponse, error) {
	conn, err := s.repo.GetConnectionByWorkspace(ctx, workspaceID, connectionID)
	if err != nil {
		return nil, err
	}
	if conn == nil {
		return nil, ErrForgeConnectionNotFound
	}
	if req.ExternalRepoID <= 0 {
		return nil, errors.New("external_repo_id must be positive")
	}
	fullName := strings.TrimSpace(req.FullName)
	if fullName == "" || strings.ContainsAny(fullName, " \t\r\n") {
		return nil, errors.New("full_name is invalid")
	}
	defaultBranch := strings.TrimSpace(req.DefaultBranch)
	if defaultBranch == "" {
		defaultBranch = "main"
	}
	repo := &domain.ForgeRepo{
		ID:             uuid.New(),
		ConnectionID:   conn.ID,
		WorkspaceID:    workspaceID,
		ExternalRepoID: req.ExternalRepoID,
		FullName:       fullName,
		WebURL:         strings.TrimSpace(req.WebURL),
		DefaultBranch:  defaultBranch,
		IsActive:       true,
	}
	if err := s.repo.LinkRepo(ctx, repo); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrForgeConnectionNotFound
		}
		return nil, err
	}
	resp := s.repoResponse(repo)
	return &resp, nil
}

func (s *ForgeService) UnlinkRepo(ctx context.Context, workspaceID, connectionID, repoID uuid.UUID) error {
	if err := s.repo.DeleteRepo(ctx, workspaceID, connectionID, repoID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrForgeRepoNotFound
		}
		return err
	}
	return nil
}

// --- Webhooks ---

// HandleWebhook verifies a delivery with the connection's secret, processes
// it at most once and ignores repositories that are not linked and active.
func (s *ForgeService) HandleWebhook(ctx context.Context, connectionID uuid.UUID, header http.Header, body []byte) error {
	conn, err := s.repo.GetConnection(ctx, connectionID)
	if err != nil {
		return err
	}
	if conn == nil {
		return ErrForgeUnauthorized
	}
	webhook := s.webhooks[conn.Provider]
	if webhook == nil {
		return ErrForgeUnauthorized
	}
	secret, err := crypto.Decrypt(conn.WebhookSecret, s.encryptionKey)
	if err != nil {
		return ErrForgeUnauthorized
	}
	if err := webhook.VerifyWebhook(header, body, secret); err != nil {
		return ErrForgeUnauthorized
	}
	deliveryID := webhook.DeliveryID(header)
	if !ValidWebhookDeliveryID(deliveryID) {
		return ErrForgeDeliveryID
	}

	claimed, err := s.repo.ClaimWebhookDelivery(ctx, conn.ID, deliveryID, webhookDeliveryLease)
	if err != nil {
		return fmt.Errorf("recording forge webhook delivery: %w", err)
	}
	fields := log.Fields{"connection_id": conn.ID, "delivery_id": deliveryID, "provider": conn.Provider}
	if !claimed {
		log.WithFields(fields).Info("Ignored a forge webhook delivery that was already received")
		return nil
	}
	processed := false
	defer func() {
		if processed {
			return
		}
		settle, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if releaseErr := s.repo.ReleaseWebhookDelivery(settle, conn.ID, deliveryID); releaseErr != nil {
			log.WithFields(fields).WithError(releaseErr).Warn("Failed forge webhook delivery stays claimed; it can be redelivered once the claim expires")
		}
	}()

	event, err := webhook.ParseWebhook(header, body)
	if err != nil {
		return err
	}
	if event == nil || event.Kind == domain.ForgeEventIgnored {
		processed = true
		s.completeDelivery(ctx, conn.ID, deliveryID, fields)
		return nil
	}
	repo, err := s.repo.GetRepoByExternalID(ctx, conn.ID, event.Repo.ExternalID)
	if err != nil {
		return err
	}
	if repo == nil || !repo.IsActive {
		processed = true
		s.completeDelivery(ctx, conn.ID, deliveryID, fields)
		return nil
	}
	if err := s.processEvent(ctx, conn, repo, event); err != nil {
		return err
	}
	processed = true
	s.completeDelivery(ctx, conn.ID, deliveryID, fields)
	return nil
}

func (s *ForgeService) completeDelivery(ctx context.Context, connectionID uuid.UUID, deliveryID string, fields log.Fields) {
	settle, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := s.repo.CompleteWebhookDelivery(settle, connectionID, deliveryID); err != nil {
		log.WithFields(fields).WithError(err).Warn("Forge webhook delivery was processed but not marked; a replay is accepted once the claim expires")
	}
}

func (s *ForgeService) processEvent(ctx context.Context, conn *domain.ForgeConnection, repo *domain.ForgeRepo, event *domain.ForgeEvent) error {
	switch event.Kind {
	case domain.ForgeEventMergeRequest:
		if event.MergeRequest == nil {
			return nil
		}
		return s.processMergeRequest(ctx, conn, repo, event.MergeRequest)
	case domain.ForgeEventPush:
		if err := s.processCommits(ctx, conn, repo, event.Commits); err != nil {
			return err
		}
		if event.Branch != nil {
			return s.processBranch(ctx, conn, repo, event.Branch)
		}
		return nil
	case domain.ForgeEventBranchCreated:
		if event.Branch == nil {
			return nil
		}
		return s.processBranch(ctx, conn, repo, event.Branch)
	default:
		return nil
	}
}

func (s *ForgeService) processMergeRequest(ctx context.Context, conn *domain.ForgeConnection, repo *domain.ForgeRepo, mr *domain.ForgeMergeRequest) error {
	issue, err := s.linker().resolvePullRequestIssue(ctx, conn.WorkspaceID, mr.SourceBranch, mr.Title, mr.Description)
	if err != nil {
		return err
	}
	var issueID *uuid.UUID
	if issue != nil {
		issueID = &issue.ID
	}
	state := mr.State
	switch state {
	case "open", "draft", "closed", "merged":
	default:
		state = "open"
	}
	source, target := mr.SourceBranch, mr.TargetBranch
	model := &domain.ForgeMergeRequestModel{
		ID:           uuid.New(),
		WorkspaceID:  conn.WorkspaceID,
		IssueID:      issueID,
		ForgeRepoID:  repo.ID,
		ExternalID:   mr.ExternalID,
		Number:       mr.Number,
		Title:        mr.Title,
		State:        state,
		AuthorLogin:  mr.Author,
		WebURL:       mr.WebURL,
		SourceBranch: &source,
		TargetBranch: &target,
		MergedAt:     mr.MergedAt,
		ClosedAt:     mr.ClosedAt,
	}
	inserted, err := s.repo.UpsertMergeRequest(ctx, model)
	if err != nil {
		return fmt.Errorf("upserting forge merge request: %w", err)
	}
	if issue == nil {
		return nil
	}
	if transition := mergeRequestTransition(mr.Action, inserted, state == "merged"); transition != "" {
		s.linker().applyAutoTransition(ctx, conn.WorkspaceID, transition, issue)
	}
	s.broadcast(conn.WorkspaceID, realtime.Event{
		Type: "forge:mr_updated",
		Payload: map[string]any{
			"issue_id": issue.ID, "provider": string(conn.Provider),
			"number": model.Number, "state": state,
		},
	})
	s.broadcastAppRefresh(conn.WorkspaceID, "issues")
	if mr.Action == "opened" {
		s.commentOnMergeRequest(ctx, conn, repo, model, issue)
	}
	return nil
}

func (s *ForgeService) commentOnMergeRequest(ctx context.Context, conn *domain.ForgeConnection, repo *domain.ForgeRepo, mr *domain.ForgeMergeRequestModel, issue *domain.Issue) {
	if conn.AccessToken == nil || *conn.AccessToken == "" {
		return
	}
	commenter := s.commenters[conn.Provider]
	if commenter == nil {
		return
	}
	token, err := crypto.Decrypt(*conn.AccessToken, s.encryptionKey)
	if err != nil {
		log.WithFields(log.Fields{"workspace_id": conn.WorkspaceID, "repo_id": repo.ID}).WithError(err).Warn("Could not decrypt forge access token; the merge request comment is skipped")
		return
	}
	slug, err := s.repo.WorkspaceSlug(ctx, conn.WorkspaceID)
	if err != nil {
		log.WithFields(log.Fields{"workspace_id": conn.WorkspaceID, "repo_id": repo.ID}).WithError(err).Warn("Could not resolve workspace slug; the merge request comment is skipped")
		return
	}
	body, err := issueCommentBody(s.frontendURL, slug, issue.Identifier)
	if err != nil {
		log.WithFields(log.Fields{"workspace_id": conn.WorkspaceID, "repo_id": repo.ID}).WithError(err).Warn("Could not build the merge request comment")
		return
	}
	c := *conn
	c.AccessToken = &token
	commentCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	if err := commenter.CommentOnMergeRequest(commentCtx, &c, repo, mr.Number, body); err != nil {
		log.WithFields(log.Fields{"workspace_id": conn.WorkspaceID, "repo_id": repo.ID, "mr_number": mr.Number}).WithError(err).Warn("Forge merge request comment failed; redeliver the opened event after recovery")
	}
}

func (s *ForgeService) processCommits(ctx context.Context, conn *domain.ForgeConnection, repo *domain.ForgeRepo, commits []domain.ForgeCommit) error {
	updatedIssues := make(map[uuid.UUID]bool)
	for _, c := range commits {
		if c.SHA == "" {
			continue
		}
		issue := s.linker().resolveIssueFromRef(ctx, conn.WorkspaceID, c.Message)
		var issueID *uuid.UUID
		if issue != nil {
			issueID = &issue.ID
		}
		author := c.Author
		committedAt := c.CommittedAt
		if committedAt.IsZero() {
			committedAt = time.Now()
		}
		commit := &domain.ForgeCommitModel{
			ID:          uuid.New(),
			WorkspaceID: conn.WorkspaceID,
			IssueID:     issueID,
			ForgeRepoID: repo.ID,
			SHA:         c.SHA,
			Message:     c.Message,
			AuthorLogin: &author,
			WebURL:      c.URL,
			CommittedAt: committedAt,
		}
		if err := s.repo.UpsertCommit(ctx, commit); err != nil {
			log.WithError(err).WithField("sha", c.SHA).Warn("failed to upsert forge commit")
			continue
		}
		if issue != nil {
			updatedIssues[issue.ID] = true
			s.broadcast(conn.WorkspaceID, realtime.Event{
				Type: "forge:commit_pushed",
				Payload: map[string]any{
					"issue_id": issue.ID, "provider": string(conn.Provider), "sha": commit.SHA,
				},
			})
		}
	}
	if len(updatedIssues) > 0 {
		s.broadcastAppRefresh(conn.WorkspaceID, "issues")
	}
	return nil
}

func (s *ForgeService) processBranch(ctx context.Context, conn *domain.ForgeConnection, repo *domain.ForgeRepo, branch *domain.ForgeBranch) error {
	if branch.Name == "" {
		return nil
	}
	issue := s.linker().resolveIssueFromRef(ctx, conn.WorkspaceID, branch.Name)
	var issueID *uuid.UUID
	if issue != nil {
		issueID = &issue.ID
	}
	webURL := branch.WebURL
	model := &domain.ForgeBranchModel{
		ID:          uuid.New(),
		WorkspaceID: conn.WorkspaceID,
		IssueID:     issueID,
		ForgeRepoID: repo.ID,
		Name:        branch.Name,
		WebURL:      &webURL,
	}
	if err := s.repo.UpsertBranch(ctx, model); err != nil {
		return fmt.Errorf("upserting forge branch: %w", err)
	}
	if issue != nil {
		s.linker().applyAutoTransition(ctx, conn.WorkspaceID, "branch_created", issue)
		s.broadcast(conn.WorkspaceID, realtime.Event{
			Type: "forge:branch_created",
			Payload: map[string]any{
				"issue_id": issue.ID, "provider": string(conn.Provider), "branch": branch.Name,
			},
		})
		s.broadcastAppRefresh(conn.WorkspaceID, "issues")
	}
	return nil
}

// --- Issue activity ---

func (s *ForgeService) GetIssueActivity(ctx context.Context, workspaceID uuid.UUID, identifier string) (*dto.ForgeIssueActivityResponse, error) {
	issue, err := s.issueRepo.GetByIdentifier(domain.PublicContext(ctx), workspaceID, identifier)
	if err != nil || issue == nil {
		return nil, fmt.Errorf("issue not found")
	}
	mrs, err := s.repo.ListMergeRequestsByIssue(ctx, issue.ID)
	if err != nil {
		return nil, err
	}
	branches, err := s.repo.ListBranchesByIssue(ctx, issue.ID)
	if err != nil {
		return nil, err
	}
	commits, err := s.repo.ListCommitsByIssue(ctx, issue.ID)
	if err != nil {
		return nil, err
	}
	resp := &dto.ForgeIssueActivityResponse{
		MergeRequests: make([]dto.ForgeMergeRequestResponse, 0, len(mrs)),
		Branches:      make([]dto.ForgeBranchResponse, 0, len(branches)),
		Commits:       make([]dto.ForgeCommitResponse, 0, len(commits)),
	}
	for _, mr := range mrs {
		source, target := "", ""
		if mr.SourceBranch != nil {
			source = *mr.SourceBranch
		}
		if mr.TargetBranch != nil {
			target = *mr.TargetBranch
		}
		resp.MergeRequests = append(resp.MergeRequests, dto.ForgeMergeRequestResponse{
			ID: mr.ID.String(), Provider: mr.Provider, Number: mr.Number, Title: mr.Title,
			State: mr.State, AuthorLogin: mr.AuthorLogin, WebURL: mr.WebURL,
			SourceBranch: source, TargetBranch: target, RepoFullName: mr.RepoFullName,
			MergedAt: mr.MergedAt, CreatedAt: mr.CreatedAt, UpdatedAt: mr.UpdatedAt,
		})
	}
	for _, b := range branches {
		webURL := ""
		if b.WebURL != nil {
			webURL = *b.WebURL
		}
		resp.Branches = append(resp.Branches, dto.ForgeBranchResponse{
			ID: b.ID.String(), Provider: b.Provider, Name: b.Name, WebURL: webURL, RepoFullName: b.RepoFullName,
		})
	}
	for _, c := range commits {
		author := ""
		if c.AuthorLogin != nil {
			author = *c.AuthorLogin
		}
		shortSHA := c.SHA
		if len(shortSHA) > 7 {
			shortSHA = shortSHA[:7]
		}
		resp.Commits = append(resp.Commits, dto.ForgeCommitResponse{
			ID: c.ID.String(), Provider: c.Provider, SHA: c.SHA, ShortSHA: shortSHA,
			Message: c.Message, AuthorLogin: author, WebURL: c.WebURL,
			RepoFullName: c.RepoFullName, CommittedAt: c.CommittedAt,
		})
	}
	return resp, nil
}

// RunWebhookDeliveryCleanup removes expired forge delivery markers every
// interval, like GitHub's cleanup.
func (s *ForgeService) RunWebhookDeliveryCleanup(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := s.repo.DeleteExpiredWebhookDeliveries(ctx, GitHubWebhookDeliveryRetention); err != nil {
				log.WithError(err).Warn("Failed to clean up forge webhook delivery markers")
			}
		}
	}
}
