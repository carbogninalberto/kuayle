package service

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	log "github.com/sirupsen/logrus"
)

// mentionReviewFocusPrefix starts the focus of reviews requested by a mention
// on GitHub, so the posted findings say where they were requested.
const mentionReviewFocusPrefix = "Requested on GitHub by @"

// mentionRequestNamespace derives a review's request ID from the GitHub
// comment, so a redelivered webhook converges on the same review.
var mentionRequestNamespace = uuid.MustParse("6f1c8a6e-3d4b-4a77-9a55-1f0b2c9e7d41")

type mentionReviewTargets interface {
	PullRequestReviewTarget(ctx context.Context, ws uuid.UUID, repository string, number int) (*repository.MergeTarget, error)
}

type mentionReviewConversations interface {
	OpenIssue(ctx context.Context, ws, issue uuid.UUID) (*domain.Conversation, error)
}

type mentionReviewTasks interface {
	Options(ctx context.Context, ws, conversation uuid.UUID) (*AgentTaskOptions, error)
	RequestReview(ctx context.Context, ws, conversation, reviewed uuid.UUID, input domain.AgentReviewRequest) (*domain.AgentTask, *domain.AgentReview, error)
}

// GitHubMentionReviews starts the review a trusted GitHub commenter asked the
// bot for. It reviews the pull request a Kuayle task published and awaits
// review on, exactly like an approved request_agent_review from chat, except
// that no approval card is shown: turning the ability on is the approval.
// The review acts for the task's creator under their current permissions
// (workspace membership, issue visibility, the coding agent policy), runs on
// the configured harness and model or chat's defaults, and posts its findings
// on the pull request.
type GitHubMentionReviews struct {
	targets       mentionReviewTargets
	conversations mentionReviewConversations
	tasks         mentionReviewTasks
}

func NewGitHubMentionReviews(targets mentionReviewTargets, conversations mentionReviewConversations, tasks mentionReviewTasks) *GitHubMentionReviews {
	return &GitHubMentionReviews{targets: targets, conversations: conversations, tasks: tasks}
}

// ReviewMention queues the review. Situations a retry cannot fix (no task
// awaits review on the pull request, the creator lost access, the harness is
// not allowed) are logged and dropped, so GitHub does not see a failed
// delivery; transient errors are returned.
func (m *GitHubMentionReviews) ReviewMention(ctx context.Context, request MentionReview) error {
	fields := log.Fields{"workspace_id": request.WorkspaceID, "repository": request.Repository, "pr_number": request.Number, "comment_id": request.CommentID}
	target, err := m.targets.PullRequestReviewTarget(ctx, request.WorkspaceID, request.Repository, request.Number)
	if errors.Is(err, repository.ErrNoMergeTarget) {
		log.WithFields(fields).Info("Ignored a review mention: no Kuayle task awaits review on this pull request")
		return nil
	}
	if err != nil {
		return err
	}
	actor := domain.WithAccess(ctx, domain.Access{WorkspaceID: request.WorkspaceID, UserID: target.CreatorID})
	review, err := m.queue(actor, request, target)
	if err != nil {
		if message, permanent := delegationFailure(err); permanent || errors.Is(err, repository.ErrNoReviewTarget) || errors.Is(err, repository.ErrReviewTargetChanged) {
			if message == "" {
				message = err.Error()
			}
			log.WithFields(fields).WithField("reason", message).Info("Ignored a review mention")
			return nil
		}
		return err
	}
	log.WithFields(fields).WithField("review_task_id", review.ID).Info("Started an agent review requested by mention")
	return nil
}

func (m *GitHubMentionReviews) queue(ctx context.Context, request MentionReview, target *repository.MergeTarget) (*domain.AgentTask, error) {
	chat, err := m.conversations.OpenIssue(ctx, request.WorkspaceID, target.IssueID)
	if err != nil {
		return nil, err
	}
	options, err := m.tasks.Options(ctx, request.WorkspaceID, chat.ID)
	if err != nil {
		return nil, err
	}
	// Like request_agent_review in chat: the task's own harness unless one is
	// configured for mentions, and the model chat would choose for it.
	harness := request.Harness
	if harness == "" {
		harness = target.Provider
	}
	if !providerListed(options, harness) {
		return nil, ErrProviderNotAllowed
	}
	focus := mentionReviewFocusPrefix + request.Author
	if request.Focus != "" {
		focus += ":\n" + request.Focus
	}
	input := domain.AgentReviewRequest{
		RequestID:         uuid.NewSHA1(mentionRequestNamespace, []byte(request.WorkspaceID.String()+":"+strconv.FormatInt(request.CommentID, 10))),
		Provider:          harness,
		Focus:             truncateRunes(strings.TrimSpace(focus), 4000),
		PostToPullRequest: true,
		ExpectedCommit:    target.Commit,
	}
	input.Model, input.Thinking, input.ModelServerID = chatModel(options, harness, request.Model, nil)
	task, _, err := m.tasks.RequestReview(ctx, request.WorkspaceID, chat.ID, target.TaskID, input)
	return task, err
}
