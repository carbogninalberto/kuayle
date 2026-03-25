package service

import (
	"context"
	"fmt"
	"regexp"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/realtime"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/pkg/sanitize"
	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
)

// mentionSpanRegex matches <span ...> tags that contain data-type="mention" and captures data-id UUID.
// Uses two patterns to handle either attribute order.
var mentionSpanRegex1 = regexp.MustCompile(`<span[^>]*data-type="mention"[^>]*data-id="([0-9a-f-]{36})"[^>]*>`)
var mentionSpanRegex2 = regexp.MustCompile(`<span[^>]*data-id="([0-9a-f-]{36})"[^>]*data-type="mention"[^>]*>`)

// extractMentionedUserIDs parses mention spans from sanitized HTML and returns unique user UUIDs.
func extractMentionedUserIDs(html string) []uuid.UUID {
	seen := make(map[uuid.UUID]bool)
	var result []uuid.UUID
	for _, re := range []*regexp.Regexp{mentionSpanRegex1, mentionSpanRegex2} {
		matches := re.FindAllStringSubmatch(html, -1)
		for _, m := range matches {
			if uid, err := uuid.Parse(m[1]); err == nil && !seen[uid] {
				seen[uid] = true
				result = append(result, uid)
			}
		}
	}
	return result
}

type CommentService struct {
	commentRepo repository.CommentRepo
	issueRepo   repository.IssueRepo
	hub         *realtime.Hub
	notifSvc    *NotificationService
}

func NewCommentService(commentRepo repository.CommentRepo, issueRepo repository.IssueRepo, hub *realtime.Hub, notifSvc *NotificationService) *CommentService {
	return &CommentService{commentRepo: commentRepo, issueRepo: issueRepo, hub: hub, notifSvc: notifSvc}
}

func (s *CommentService) Create(ctx context.Context, issueID, userID uuid.UUID, req dto.CreateCommentRequest) (*domain.Comment, error) {
	// Sanitize HTML in comment body
	req.Body = sanitize.SanitizeHTML(req.Body)

	comment := &domain.Comment{
		ID:      uuid.New(),
		IssueID: issueID,
		UserID:  userID,
		Body:    req.Body,
	}
	if req.ParentID != nil {
		pid, err := uuid.Parse(*req.ParentID)
		if err != nil {
			return nil, err
		}
		comment.ParentID = &pid
	}
	if err := s.commentRepo.Create(ctx, comment); err != nil {
		return nil, err
	}

	// Broadcast and notify
	issue, _ := s.issueRepo.GetByID(ctx, issueID)
	if issue != nil {
		s.hub.Broadcast(issue.WorkspaceID, realtime.Event{
			Type: "comment.created",
			Payload: map[string]string{
				"issue_id":   issueID.String(),
				"comment_id": comment.ID.String(),
				"identifier": issue.Identifier,
			},
		})

		// Notify issue creator + assignees (except the commenter)
		recipients := make(map[uuid.UUID]bool)
		if issue.CreatorID != userID {
			recipients[issue.CreatorID] = true
		}
		if issue.AssigneeID != nil && *issue.AssigneeID != userID {
			recipients[*issue.AssigneeID] = true
		}
		assignees, _ := s.issueRepo.GetAssignees(ctx, issueID)
		for _, uid := range assignees {
			if uid != userID {
				recipients[uid] = true
			}
		}

		title := fmt.Sprintf("New comment on %s: %s", issue.Identifier, issue.Title)
		for uid := range recipients {
			if err := s.notifSvc.Create(ctx, uid, issue.WorkspaceID, &issue.ID, "commented", title); err != nil {
				log.WithError(err).Warn("failed to create comment notification")
				continue
			}
			s.hub.BroadcastToUser(issue.WorkspaceID, uid, realtime.Event{
				Type:    "notification.created",
				Payload: map[string]string{"type": "commented"},
			})
		}

		// Notify @mentioned users (skip commenter and already-notified recipients)
		mentionedIDs := extractMentionedUserIDs(comment.Body)
		mentionTitle := fmt.Sprintf("You were mentioned in %s: %s", issue.Identifier, issue.Title)
		for _, uid := range mentionedIDs {
			if uid == userID || recipients[uid] {
				continue
			}
			if err := s.notifSvc.Create(ctx, uid, issue.WorkspaceID, &issue.ID, "mentioned", mentionTitle); err != nil {
				log.WithError(err).Warn("failed to create mention notification")
				continue
			}
			s.hub.BroadcastToUser(issue.WorkspaceID, uid, realtime.Event{
				Type:    "notification.created",
				Payload: map[string]string{"type": "mentioned"},
			})
		}
	}

	return comment, nil
}

func (s *CommentService) ListByIssue(ctx context.Context, issueID uuid.UUID) ([]domain.Comment, error) {
	return s.commentRepo.ListByIssue(ctx, issueID)
}

func (s *CommentService) ListReplies(ctx context.Context, parentID uuid.UUID) ([]domain.Comment, error) {
	return s.commentRepo.ListReplies(ctx, parentID)
}

func (s *CommentService) GetByID(ctx context.Context, id uuid.UUID) (*domain.Comment, error) {
	return s.commentRepo.GetByID(ctx, id)
}

func (s *CommentService) Resolve(ctx context.Context, id uuid.UUID) error {
	return s.commentRepo.Resolve(ctx, id)
}

func (s *CommentService) Reopen(ctx context.Context, id uuid.UUID) error {
	return s.commentRepo.Reopen(ctx, id)
}
