package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	log "github.com/sirupsen/logrus"
)

type githubBotReader interface {
	Get(ctx context.Context, ws uuid.UUID) (*domain.GitHubBotSettings, error)
}

// SetBotSettings lets the workspace's bot identity sign comments and its
// ability toggles gate what the bot does on GitHub.
func (s *GitHubService) SetBotSettings(settings githubBotReader) {
	s.bot = settings
}

// botSettings returns the workspace's bot settings, or the defaults when
// none are wired.
func (s *GitHubService) botSettings(ctx context.Context, ws uuid.UUID) (*domain.GitHubBotSettings, error) {
	if s.bot == nil {
		return &domain.GitHubBotSettings{WorkspaceID: ws, Abilities: map[string]bool{}}, nil
	}
	return s.bot.Get(ctx, ws)
}

// requireAbility refuses a GitHub action the workspace turned off.
func (s *GitHubService) requireAbility(ctx context.Context, ws uuid.UUID, ability domain.GitHubBotAbility) error {
	settings, err := s.botSettings(ctx, ws)
	if err != nil {
		return err
	}
	if !settings.Enabled(ability) {
		return &GitHubBotAbilityDisabledError{Ability: ability}
	}
	return nil
}

// AppSlug is the GitHub App's slug for the workspace, which is the handle that
// mentions the bot on GitHub; empty when unknown.
func (s *GitHubService) AppSlug(ctx context.Context, ws uuid.UUID) string {
	if s.globalApp != nil {
		return s.globalApp.Slug
	}
	cfg, err := s.ghRepo.GetAppConfigByWorkspace(ctx, ws)
	if err != nil || cfg == nil || cfg.AppSlug == nil {
		return ""
	}
	return *cfg.AppSlug
}

// MentionReview is a trusted request on GitHub for the bot to review a pull request.
type MentionReview struct {
	WorkspaceID uuid.UUID
	Repository  string
	Number      int
	CommentID   int64
	Author      string
	Focus       string
	Harness     string
	Model       string
}

type mentionReviewer interface {
	ReviewMention(ctx context.Context, request MentionReview) error
}

// SetMentionReviews lets a mention of the bot on a pull request start a review.
func (s *GitHubService) SetMentionReviews(reviews mentionReviewer) {
	s.mentionReviews = reviews
}

// reviewWord asks for a review in a comment that mentions the bot.
var reviewWord = regexp.MustCompile(`(?i)\breview\b`)

// reviewMention reports whether body mentions the app (as @slug or
// @slug[bot]) and asks for a review, and returns the rest of the comment as
// the review's focus.
func reviewMention(body, slug string) (string, bool) {
	if slug == "" {
		return "", false
	}
	mention := regexp.MustCompile(`(?i)(^|[^A-Za-z0-9_/-])@` + regexp.QuoteMeta(slug) + `(\[bot\])?($|[^A-Za-z0-9_\[-])`)
	if !mention.MatchString(body) || !reviewWord.MatchString(body) {
		return "", false
	}
	focus := strings.TrimSpace(mention.ReplaceAllString(body, "${1}${3}"))
	return truncateRunes(focus, 3500), true
}

type ghWebhookIssueComment struct {
	Action string `json:"action"`
	Issue  struct {
		Number      int              `json:"number"`
		PullRequest *json.RawMessage `json:"pull_request"`
	} `json:"issue"`
	Comment struct {
		ID                int64  `json:"id"`
		Body              string `json:"body"`
		AuthorAssociation string `json:"author_association"`
		User              struct {
			Login string `json:"login"`
			Type  string `json:"type"`
		} `json:"user"`
	} `json:"comment"`
	Repository struct {
		ID int64 `json:"id"`
	} `json:"repository"`
	Installation struct {
		ID int64 `json:"id"`
	} `json:"installation"`
}

// processIssueCommentEvent starts a review when a trusted commenter mentions
// the bot on a pull request of a linked repository and asks for one, and the
// workspace turned the mention trigger on. Edits, bots and comments on issues
// are ignored. The review itself re-checks the task and its creator's
// permissions; see GitHubMentionReviews.
func (s *GitHubService) processIssueCommentEvent(ctx context.Context, payload []byte) error {
	var event ghWebhookIssueComment
	if err := json.Unmarshal(payload, &event); err != nil {
		return fmt.Errorf("unmarshaling issue comment event: %w", err)
	}
	if s.mentionReviews == nil || event.Action != "created" || event.Issue.PullRequest == nil || event.Issue.Number <= 0 ||
		event.Comment.ID <= 0 || strings.EqualFold(event.Comment.User.Type, "Bot") {
		return nil
	}
	inst, repo, err := s.linkedRepository(ctx, event.Installation.ID, event.Repository.ID)
	if err != nil || repo == nil {
		return err
	}
	settings, err := s.botSettings(ctx, inst.WorkspaceID)
	if err != nil {
		return err
	}
	if !settings.Enabled(domain.GitHubBotMentionReviews) {
		return nil
	}
	focus, ok := reviewMention(event.Comment.Body, s.AppSlug(ctx, inst.WorkspaceID))
	if !ok {
		return nil
	}
	// Like review feedback, only people with write association can start work.
	switch event.Comment.AuthorAssociation {
	case "OWNER", "MEMBER", "COLLABORATOR":
	default:
		log.WithFields(log.Fields{"workspace_id": inst.WorkspaceID, "repo_id": repo.ID, "pr_number": event.Issue.Number}).Info("Ignored a review mention from a commenter without write association")
		return nil
	}
	return s.mentionReviews.ReviewMention(ctx, MentionReview{
		WorkspaceID: inst.WorkspaceID, Repository: repo.FullName, Number: event.Issue.Number, CommentID: event.Comment.ID,
		Author: truncateRunes(event.Comment.User.Login, 100), Focus: focus, Harness: settings.MentionHarness, Model: settings.MentionModel,
	})
}

// ignoreDisabledAbility drops the refusal of an ability for automatic
// webhook reactions, which simply do not happen; other errors remain.
func ignoreDisabledAbility(err error) error {
	var disabled *GitHubBotAbilityDisabledError
	if errors.As(err, &disabled) {
		return nil
	}
	return err
}

// disabledAbilityMessage is the refusal shown on an approval whose GitHub
// action the workspace turned off; ok is false for any other error.
func disabledAbilityMessage(err error) (string, bool) {
	var disabled *GitHubBotAbilityDisabledError
	if errors.As(err, &disabled) {
		return disabled.Error(), true
	}
	return "", false
}
