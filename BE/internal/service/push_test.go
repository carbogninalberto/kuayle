package service

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/config"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/stretchr/testify/require"
)

func TestPushPreferenceAndActorGating(t *testing.T) {
	user := uuid.New()
	prefs := &domain.UserPreferences{PushNotifications: true}
	job := pushJob{userID: user, event: notificationEvent{Type: "agent_task_finished"}}
	require.True(t, job.enabled(prefs))
	prefs.NotificationEvents = domain.NotificationEvents{"task_finished": false}
	require.False(t, job.enabled(prefs))
	prefs.NotificationEvents = nil
	prefs.PushNotifications = false
	require.False(t, job.enabled(prefs))
	prefs.PushNotifications = true
	job.event.ActorID = &user
	require.False(t, job.enabled(prefs))
}

func TestPushPayload(t *testing.T) {
	conversationID, approvalID, issueID := uuid.New(), uuid.New(), uuid.New()
	event := notificationEvent{Type: notificationApprovalRequested, Title: strings.Repeat("\x00👍", 2000),
		IssueID: &issueID, IssueIdentifier: "PLAT-123", Extra: map[string]string{
			"conversation_id": conversationID.String(), "approval_id": approvalID.String(),
		}}
	payload := makePushPayload("workspace", event)
	require.Equal(t, "Approval requested", payload.Title)
	require.Equal(t, "approval_requested:"+issueID.String(), payload.Tag)
	require.Equal(t, "/workspace/assistant?conversation="+conversationID.String(), payload.Data.URL)
	encoded, err := json.Marshal(payload)
	require.NoError(t, err)
	require.Less(t, len(encoded), 3000)
	event = notificationEvent{Type: "review_needed", IssueID: &issueID, IssueIdentifier: "PLAT-123"}
	require.Equal(t, "/workspace/issue/PLAT-123", makePushPayload("workspace", event).Data.URL)
}

type pushDeliveryStore struct {
	dropped  []uuid.UUID
	recorded map[uuid.UUID]bool
}

func (*pushDeliveryStore) Register(context.Context, domain.PushSubscription, *uuid.UUID) (uuid.UUID, error) {
	return uuid.Nil, nil
}
func (*pushDeliveryStore) Delete(context.Context, uuid.UUID, uuid.UUID, *uuid.UUID) error { return nil }
func (*pushDeliveryStore) CountByUser(context.Context, uuid.UUID) (int, error)            { return 0, nil }

func (*pushDeliveryStore) ListByUser(context.Context, uuid.UUID) ([]domain.PushSubscription, error) {
	return nil, nil
}
func (s *pushDeliveryStore) RecordDelivery(_ context.Context, sub domain.PushSubscription, status int) error {
	if status == http.StatusGone || status == http.StatusNotFound {
		s.dropped = append(s.dropped, sub.ID)
	} else {
		s.recorded[sub.ID] = status >= 200 && status < 300
	}
	return nil
}

func TestPushDeliveryPrunesAndIsolatesFailures(t *testing.T) {
	failed := domain.PushSubscription{ID: uuid.New()}
	dead := domain.PushSubscription{ID: uuid.New()}
	healthy := domain.PushSubscription{ID: uuid.New()}
	store := &pushDeliveryStore{recorded: map[uuid.UUID]bool{}}
	attempts := map[uuid.UUID]int{}
	svc := &PushService{store: store, send: func(_ context.Context, _ config.VAPIDConfig, sub domain.PushSubscription, _ []byte) (int, error) {
		attempts[sub.ID]++
		switch sub.ID {
		case failed.ID:
			return 0, errors.New("provider unreachable")
		case dead.ID:
			return http.StatusGone, nil
		default:
			return http.StatusCreated, nil
		}
	}}
	for _, sub := range []domain.PushSubscription{failed, dead, healthy} {
		svc.deliver(t.Context(), config.VAPIDConfig{}, sub, []byte("{}"))
	}
	require.Equal(t, []uuid.UUID{dead.ID}, store.dropped)
	require.Equal(t, map[uuid.UUID]bool{failed.ID: false, healthy.ID: true}, store.recorded)
	require.Equal(t, map[uuid.UUID]int{failed.ID: 2, dead.ID: 1, healthy.ID: 1}, attempts)
}

func TestPublishNotificationQueuesPushWithoutWaitingForDelivery(t *testing.T) {
	events := &userEventsFake{}
	svc := &NotificationService{push: &PushService{queue: make(chan pushJob, 1)}}
	workspace, recipient := uuid.New(), uuid.New()
	own := notificationEvent{Type: "assigned", ActorID: &recipient}
	publishNotification(events, workspace, recipient, own, svc)
	require.Empty(t, svc.push.queue)

	other := notificationEvent{Type: "mentioned"}
	publishNotification(events, workspace, recipient, other, svc)
	publishNotification(events, workspace, recipient, other, svc) // Full buffer.
	require.Len(t, events.events, 3)
	job := <-svc.push.queue
	require.Equal(t, workspace, job.workspaceID)
	require.Equal(t, recipient, job.userID)
	require.Equal(t, "mentioned", job.event.Type)
}

func TestPushSubscriptionValidatesEndpointAndKeys(t *testing.T) {
	key, err := ecdh.P256().GenerateKey(rand.Reader)
	require.NoError(t, err)
	req := dto.RegisterPushSubscriptionRequest{
		WorkspaceSlug: "kuayle", Endpoint: "https://web.push.apple.com/subscription",
		P256DH: base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()),
		Auth:   base64.RawURLEncoding.EncodeToString(make([]byte, 16)),
	}
	require.True(t, validPushSubscription(req))
	for _, endpoint := range []string{"http://push.example/sub", "https://127.0.0.1/sub", "https://push.example:444/sub", "https://user@push.example/sub"} {
		invalid := req
		invalid.Endpoint = endpoint
		require.False(t, validPushSubscription(invalid), endpoint)
	}
	req.Auth = "too-short"
	require.False(t, validPushSubscription(req))
}

type userPushDevices struct {
	pushDeliveryStore
	byUser map[uuid.UUID][]domain.PushSubscription
}

func (s *userPushDevices) ListByUser(_ context.Context, user uuid.UUID) ([]domain.PushSubscription, error) {
	return s.byUser[user], nil
}

func TestPushTestUsesCallerSubscriptionsAndEffectiveKeys(t *testing.T) {
	admin, other := uuid.New(), uuid.New()
	subs := &userPushDevices{pushDeliveryStore: pushDeliveryStore{recorded: map[uuid.UUID]bool{}}, byUser: map[uuid.UUID][]domain.PushSubscription{
		admin: {{ID: uuid.New()}, {ID: uuid.New()}}, other: {{ID: uuid.New()}},
	}}
	env := config.VAPIDConfig{PublicKey: "public", Subject: "https://example.org"}
	s := NewPushService(env, nil, subs, nil, nil, nil, nil, nil)
	var sent []uuid.UUID
	s.send = func(_ context.Context, cfg config.VAPIDConfig, sub domain.PushSubscription, payload []byte) (int, error) {
		require.Equal(t, env, cfg)
		require.Contains(t, string(payload), "Kuayle")
		sent = append(sent, sub.ID)
		if sub.ID == subs.byUser[admin][0].ID {
			return 201, nil
		}
		return 410, nil
	}
	result, err := s.Test(t.Context(), admin)
	require.NoError(t, err)
	require.Equal(t, 1, result.Sent)
	require.Equal(t, 1, result.Failed)
	require.Equal(t, []uuid.UUID{subs.byUser[admin][0].ID, subs.byUser[admin][1].ID}, sent)
}
