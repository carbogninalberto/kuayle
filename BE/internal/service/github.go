package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/realtime"
	"github.com/kuayle/kuayle-backend/internal/repository"
	gh "github.com/kuayle/kuayle-backend/pkg/github"
	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
)

var issueIdentifierRegex = regexp.MustCompile(`([A-Z][A-Z0-9]+-\d+)`)

type GitHubService struct {
	ghRepo         *repository.GitHubRepository
	issueRepo      repository.IssueRepo
	teamStatusRepo repository.TeamStatusRepo
	historyRepo    repository.IssueHistoryRepo
	ghClient       *gh.Client
	encryptionKey  []byte
	hub            *realtime.Hub
	webhookSecret  string
	clientID       string
}

func NewGitHubService(
	ghRepo *repository.GitHubRepository,
	issueRepo repository.IssueRepo,
	teamStatusRepo repository.TeamStatusRepo,
	historyRepo repository.IssueHistoryRepo,
	ghClient *gh.Client,
	encryptionKey []byte,
	hub *realtime.Hub,
	webhookSecret string,
	clientID string,
) *GitHubService {
	return &GitHubService{
		ghRepo:         ghRepo,
		issueRepo:      issueRepo,
		teamStatusRepo: teamStatusRepo,
		historyRepo:    historyRepo,
		ghClient:       ghClient,
		encryptionKey:  encryptionKey,
		hub:            hub,
		webhookSecret:  webhookSecret,
		clientID:       clientID,
	}
}

// --- Installation Flow ---

// GetInstallURL returns the GitHub App installation URL.
func (s *GitHubService) GetInstallURL(workspaceID uuid.UUID) string {
	return fmt.Sprintf("https://github.com/apps/kuayle/installations/new?state=%s", workspaceID.String())
}

// HandleInstallationCallback processes the GitHub App installation callback.
func (s *GitHubService) HandleInstallationCallback(ctx context.Context, workspaceID, userID uuid.UUID, installationID int64) (*domain.GitHubInstallation, error) {
	// Check if already installed
	existing, err := s.ghRepo.GetInstallationByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return existing, nil
	}

	// Fetch installation details from GitHub
	ghInst, err := s.ghClient.GetInstallation(installationID)
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
	inst, err := s.ghRepo.GetInstallationByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}

	resp := &dto.GitHubStatusResponse{
		Installed: inst != nil,
		Repos:     []dto.GitHubRepoResponse{},
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

	token, _, err := s.ghClient.GetInstallationToken(inst.InstallationID)
	if err != nil {
		return nil, fmt.Errorf("getting installation token: %w", err)
	}

	ghRepos, err := s.ghClient.ListInstallationRepos(token)
	if err != nil {
		return nil, fmt.Errorf("listing repos: %w", err)
	}

	// Get already linked repos
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

	token, _, err := s.ghClient.GetInstallationToken(inst.InstallationID)
	if err != nil {
		return fmt.Errorf("getting installation token: %w", err)
	}

	ghRepos, err := s.ghClient.ListInstallationRepos(token)
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

// Disconnect removes the GitHub integration for a workspace.
func (s *GitHubService) Disconnect(ctx context.Context, workspaceID uuid.UUID) error {
	return s.ghRepo.DeleteInstallation(ctx, workspaceID)
}

// --- Webhook Event Processing ---

// VerifyWebhookSignature verifies the GitHub webhook HMAC-SHA256 signature.
func (s *GitHubService) VerifyWebhookSignature(payload []byte, signature string) bool {
	if s.webhookSecret == "" {
		return true // No secret configured, skip verification
	}
	sig := strings.TrimPrefix(signature, "sha256=")
	mac := hmac.New(sha256.New, []byte(s.webhookSecret))
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
		return nil // Ignore unhandled events
	}
}

type ghWebhookPR struct {
	Action       string `json:"action"`
	Number       int    `json:"number"`
	PullRequest  struct {
		ID        int64      `json:"id"`
		Number    int        `json:"number"`
		Title     string     `json:"title"`
		State     string     `json:"state"`
		Draft     bool       `json:"draft"`
		Merged    bool       `json:"merged"`
		HTMLURL   string     `json:"html_url"`
		Head      struct{ Ref string `json:"ref"` } `json:"head"`
		Base      struct{ Ref string `json:"ref"` } `json:"base"`
		User      struct {
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
		return nil // Unknown installation, skip
	}

	repo, err := s.ghRepo.GetRepoByGitHubID(ctx, inst.WorkspaceID, event.Repository.ID)
	if err != nil || repo == nil {
		return nil // Repo not linked, skip
	}

	// Determine PR state
	state := event.PullRequest.State
	if event.PullRequest.Draft {
		state = "draft"
	}
	if event.PullRequest.Merged {
		state = "merged"
	}

	// Resolve issue from branch name, PR title, or PR body
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

	// Apply auto-transitions
	if issue != nil {
		switch {
		case event.Action == "opened" || event.Action == "ready_for_review":
			s.applyAutoTransition(ctx, inst.WorkspaceID, "pr_opened", issue)
		case event.PullRequest.Merged:
			s.applyAutoTransition(ctx, inst.WorkspaceID, "pr_merged", issue)
		}
	}

	// Broadcast realtime update
	if issue != nil {
		s.hub.Broadcast(inst.WorkspaceID, realtime.Event{
			Type:    "github:pr_updated",
			Payload: map[string]any{"issue_id": issue.ID, "pr_number": pr.Number, "state": state},
		})
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
		}
	}

	return nil
}

type ghWebhookCreate struct {
	RefType string `json:"ref_type"`
	Ref     string `json:"ref"`
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
		s.hub.Broadcast(inst.WorkspaceID, realtime.Event{
			Type:    "github:branch_created",
			Payload: map[string]any{"issue_id": issue.ID, "branch": event.Ref},
		})
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

// resolveIssueFromRef extracts issue identifiers from text and returns the first matching issue.
func (s *GitHubService) resolveIssueFromRef(ctx context.Context, workspaceID uuid.UUID, texts ...string) *domain.Issue {
	seen := make(map[string]bool)
	for _, text := range texts {
		matches := issueIdentifierRegex.FindAllString(text, -1)
		for _, m := range matches {
			if seen[m] {
				continue
			}
			seen[m] = true
			issue, err := s.issueRepo.GetByIdentifier(ctx, workspaceID, m)
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

	// Don't transition if already in the target status
	if oldStatus == newStatus {
		return
	}

	issue.Status = domain.IssueStatus(newStatus)
	if rule.TargetStatusID != nil {
		issue.StatusID = rule.TargetStatusID
	}

	if err := s.issueRepo.Update(ctx, issue); err != nil {
		log.WithError(err).WithField("issue_id", issue.ID).Warn("failed to auto-transition issue")
		return
	}

	// Record history
	_ = s.historyRepo.Create(ctx, issue.ID, uuid.Nil, "status", &oldStatus, &newStatus)

	// Broadcast
	s.hub.Broadcast(workspaceID, realtime.Event{
		Type:    "issue.updated",
		Payload: issue,
	})
}

// --- Data Access ---

// GetIssueActivity returns GitHub PRs, branches, and commits linked to an issue.
func (s *GitHubService) GetIssueActivity(ctx context.Context, workspaceID uuid.UUID, identifier string) (*dto.GitHubIssueActivityResponse, error) {
	issue, err := s.issueRepo.GetByIdentifier(ctx, workspaceID, identifier)
	if err != nil || issue == nil {
		return nil, fmt.Errorf("issue not found")
	}

	prs, err := s.ghRepo.ListPRsWithRepoByIssue(ctx, issue.ID)
	if err != nil {
		return nil, err
	}

	branches, err := s.ghRepo.ListBranchesWithRepoByIssue(ctx, issue.ID)
	if err != nil {
		return nil, err
	}

	commits, err := s.ghRepo.ListCommitsWithRepoByIssue(ctx, issue.ID)
	if err != nil {
		return nil, err
	}

	resp := &dto.GitHubIssueActivityResponse{
		PullRequests: make([]dto.GitHubPullRequestResponse, 0, len(prs)),
		Branches:     make([]dto.GitHubBranchResponse, 0, len(branches)),
		Commits:      make([]dto.GitHubCommitResponse, 0, len(commits)),
	}

	for _, pr := range prs {
		avatarURL := ""
		if pr.AuthorAvatarURL != nil {
			avatarURL = *pr.AuthorAvatarURL
		}
		headBranch := ""
		if pr.HeadBranch != nil {
			headBranch = *pr.HeadBranch
		}
		baseBranch := ""
		if pr.BaseBranch != nil {
			baseBranch = *pr.BaseBranch
		}
		resp.PullRequests = append(resp.PullRequests, dto.GitHubPullRequestResponse{
			ID:              pr.ID.String(),
			Number:          pr.Number,
			Title:           pr.Title,
			State:           pr.State,
			AuthorLogin:     pr.AuthorLogin,
			AuthorAvatarURL: avatarURL,
			HTMLURL:         pr.HTMLURL,
			HeadBranch:      headBranch,
			BaseBranch:      baseBranch,
			Additions:       pr.Additions,
			Deletions:       pr.Deletions,
			RepoFullName:    pr.RepoFullName,
			MergedAt:        pr.MergedAt,
			CreatedAt:       pr.CreatedAt,
			UpdatedAt:       pr.UpdatedAt,
		})
	}

	for _, b := range branches {
		htmlURL := ""
		if b.HTMLURL != nil {
			htmlURL = *b.HTMLURL
		}
		resp.Branches = append(resp.Branches, dto.GitHubBranchResponse{
			ID:           b.ID.String(),
			Name:         b.Name,
			HTMLURL:      htmlURL,
			RepoFullName: b.RepoFullName,
		})
	}

	for _, c := range commits {
		authorLogin := ""
		if c.AuthorLogin != nil {
			authorLogin = *c.AuthorLogin
		}
		authorAvatar := ""
		if c.AuthorAvatarURL != nil {
			authorAvatar = *c.AuthorAvatarURL
		}
		shortSHA := c.SHA
		if len(shortSHA) > 7 {
			shortSHA = shortSHA[:7]
		}
		resp.Commits = append(resp.Commits, dto.GitHubCommitResponse{
			ID:              c.ID.String(),
			SHA:             c.SHA,
			ShortSHA:        shortSHA,
			Message:         c.Message,
			AuthorLogin:     authorLogin,
			AuthorAvatarURL: authorAvatar,
			HTMLURL:         c.HTMLURL,
			RepoFullName:    c.RepoFullName,
			CommittedAt:     c.CommittedAt,
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
			Event:          t.Event,
			TargetStatus:   t.TargetStatus,
			TargetStatusID: statusID,
			IsActive:       t.IsActive,
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
			ID:             uuid.New(),
			WorkspaceID:    workspaceID,
			Event:          rule.Event,
			TargetStatus:   rule.TargetStatus,
			TargetStatusID: statusID,
			IsActive:       rule.IsActive,
		}
		if err := s.ghRepo.UpsertAutoTransition(ctx, t); err != nil {
			return err
		}
	}
	return nil
}

