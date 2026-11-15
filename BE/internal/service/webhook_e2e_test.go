package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/realtime"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

// duringRequest runs call as an HTTP handler does: with the member's access,
// in a context that is canceled as soon as the call returns.
func duringRequest[T any](access domain.Access, call func(context.Context) (T, error)) (T, error) {
	ctx, cancel := context.WithCancel(domain.WithAccess(context.Background(), access))
	defer cancel()
	return call(ctx)
}

// signedPayload checks a delivery's event and signature, computing the
// signature independently, and returns the envelope's payload.
func signedPayload(t *testing.T, got receivedDelivery, event, secret string) json.RawMessage {
	t.Helper()
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(got.body)
	require.Equal(t, "sha256="+hex.EncodeToString(mac.Sum(nil)), got.signature)
	require.Equal(t, event, got.event)
	var envelope deliveryEnvelope
	require.NoError(t, json.Unmarshal(got.body, &envelope))
	require.Equal(t, event, envelope.Event)
	return envelope.Payload
}

type e2eIssuePayload struct {
	Issue struct {
		ID         uuid.UUID `json:"id"`
		Identifier string    `json:"identifier"`
		Title      string    `json:"title"`
	} `json:"issue"`
	ActorID *uuid.UUID `json:"actor_id"`
}

// KUA-109: issue and comment writes made through the services, wired as
// cmd/server/main.go wires them, reach a subscribed receiver once each, signed,
// after the request that made them has ended. Once the workspace has a private
// team nothing is delivered, and the write still succeeds.
func TestWebhookIssueAndCommentWritesReachReceiverEndToEnd(t *testing.T) {
	f := newWebhookFixture(t)
	ws, owner, team := f.workspace()
	const secret = "end-to-end-receiver-secret"
	f.receiver(ws, "receiver", secret, true, WebhookEventIssueCreated, WebhookEventIssueUpdated, WebhookEventCommentCreated, WebhookEventCommentUpdated)
	status := uuid.New()
	_, err := f.db.Exec(`INSERT INTO team_statuses(id,team_id,name,slug,category) VALUES($1,$2,'Todo','todo','unstarted')`, status, team)
	require.NoError(t, err)
	statusID := status.String()

	workspaceRepo := repository.NewWorkspaceRepository(f.db)
	hub := realtime.NewHub(workspaceRepo.RealtimeAccess)
	notifSvc := NewNotificationService(repository.NewNotificationRepository(f.db))
	issueRepo := repository.NewIssueRepository(f.db)
	issues := NewIssueService(issueRepo, repository.NewTeamRepository(f.db), repository.NewTeamStatusRepository(f.db),
		repository.NewIssueHistoryRepository(f.db), hub, notifSvc, repository.NewProjectRepository(f.db))
	issues.SetWebhooks(f.svc)
	comments := NewCommentService(repository.NewCommentRepository(f.db), issueRepo, hub, notifSvc)
	comments.SetWebhooks(f.svc)
	member := domain.Access{UserID: owner, WorkspaceID: ws}

	created, err := duringRequest(member, func(ctx context.Context) (*domain.Issue, error) {
		return issues.Create(ctx, ws, owner, dto.CreateIssueRequest{Title: "Ship webhooks", TeamID: team.String(), StatusID: &statusID})
	})
	require.NoError(t, err)
	var createdPayload e2eIssuePayload
	require.NoError(t, json.Unmarshal(signedPayload(t, f.next(), WebhookEventIssueCreated, secret), &createdPayload))
	require.Equal(t, created.ID, createdPayload.Issue.ID)
	require.Equal(t, created.Identifier, createdPayload.Issue.Identifier)
	require.Equal(t, "Ship webhooks", createdPayload.Issue.Title)
	require.NotNil(t, createdPayload.ActorID)
	require.Equal(t, owner, *createdPayload.ActorID)
	f.none()

	title := "Ship signed webhooks"
	_, err = duringRequest(member, func(ctx context.Context) (*domain.Issue, error) {
		return issues.Update(ctx, ws, owner, created.Identifier, dto.UpdateIssueRequest{Title: &title})
	})
	require.NoError(t, err)
	var updatedPayload e2eIssuePayload
	require.NoError(t, json.Unmarshal(signedPayload(t, f.next(), WebhookEventIssueUpdated, secret), &updatedPayload))
	require.Equal(t, created.Identifier, updatedPayload.Issue.Identifier)
	require.Equal(t, title, updatedPayload.Issue.Title)
	require.NotNil(t, updatedPayload.ActorID)
	require.Equal(t, owner, *updatedPayload.ActorID)
	f.none()

	comment, err := duringRequest(member, func(ctx context.Context) (*domain.Comment, error) {
		return comments.Create(ctx, created.ID, owner, dto.CreateCommentRequest{Body: "Receiver verified"})
	})
	require.NoError(t, err)
	var commentPayload struct {
		Comment struct {
			ID   uuid.UUID `json:"id"`
			Body string    `json:"body"`
		} `json:"comment"`
		Issue   dto.WebhookIssueRef `json:"issue"`
		ActorID *uuid.UUID          `json:"actor_id"`
	}
	require.NoError(t, json.Unmarshal(signedPayload(t, f.next(), WebhookEventCommentCreated, secret), &commentPayload))
	require.Equal(t, created.Identifier, commentPayload.Issue.Identifier)
	require.Equal(t, comment.ID, commentPayload.Comment.ID)
	require.Equal(t, comment.Body, commentPayload.Comment.Body)
	require.Contains(t, commentPayload.Comment.Body, "Receiver verified")
	require.NotNil(t, commentPayload.ActorID)
	require.Equal(t, owner, *commentPayload.ActorID)
	f.none()

	// KUA-196: resolving the comment is an edit, sent as comment.updated.
	resolved, err := duringRequest(member, func(ctx context.Context) (*domain.Comment, error) {
		return comments.Resolve(ctx, comment.ID, owner)
	})
	require.NoError(t, err)
	require.NotNil(t, resolved.ResolvedAt)
	var updatedComment struct {
		Comment struct {
			ID         uuid.UUID  `json:"id"`
			ResolvedAt *time.Time `json:"resolved_at"`
		} `json:"comment"`
		Issue   dto.WebhookIssueRef `json:"issue"`
		ActorID *uuid.UUID          `json:"actor_id"`
	}
	require.NoError(t, json.Unmarshal(signedPayload(t, f.next(), WebhookEventCommentUpdated, secret), &updatedComment))
	require.Equal(t, comment.ID, updatedComment.Comment.ID)
	require.NotNil(t, updatedComment.Comment.ResolvedAt)
	require.Equal(t, created.Identifier, updatedComment.Issue.Identifier)
	require.Equal(t, owner, *updatedComment.ActorID)
	f.none()

	// A private team anywhere in the workspace stops all deliveries.
	privateTeam := uuid.New()
	_, err = f.db.Exec(`INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Security','SEC')`, privateTeam, ws)
	require.NoError(t, err)
	_, err = f.db.Exec(`UPDATE teams SET is_private=TRUE WHERE id=$1`, privateTeam)
	require.NoError(t, err)
	hidden := "Kept from receivers"
	updated, err := duringRequest(member, func(ctx context.Context) (*domain.Issue, error) {
		return issues.Update(ctx, ws, owner, created.Identifier, dto.UpdateIssueRequest{Title: &hidden})
	})
	require.NoError(t, err, "a skipped delivery does not fail the write")
	require.Equal(t, hidden, updated.Title)
	var stored string
	require.NoError(t, f.db.Get(&stored, `SELECT title FROM issues WHERE id=$1`, created.ID))
	require.Equal(t, hidden, stored)
	f.none()
}
