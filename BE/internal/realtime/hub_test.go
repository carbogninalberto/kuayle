package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"nhooyr.io/websocket"
)

func TestQueuedWebSocketDeliveryRechecksPrivacyAndMembership(t *testing.T) {
	var private, removed atomic.Bool
	hub := NewHub(func(context.Context, uuid.UUID, uuid.UUID) (bool, bool, error) {
		return !removed.Load(), private.Load(), nil
	})
	workspace, user := uuid.New(), uuid.New()
	ready := make(chan *Client, 1)
	start := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		client := hub.Register(conn, workspace, user)
		defer hub.Unregister(client)
		ready <- client
		<-start
		go hub.WritePump(r.Context(), client)
		hub.ReadPump(r.Context(), client)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	socket, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	require.NoError(t, err)
	defer func() { _ = socket.CloseNow() }()
	<-ready
	hub.Broadcast(workspace, Event{Type: "issue.updated", Payload: map[string]string{"title": "PRIVATE TITLE", "id": "PRIVATE ID"}})
	private.Store(true)
	close(start)
	_, message, err := socket.Read(ctx)
	require.NoError(t, err)
	require.NotContains(t, string(message), "PRIVATE")
	var event Event
	require.NoError(t, json.Unmarshal(message, &event))
	require.Equal(t, "app.refresh", event.Type)
	removed.Store(true)
	hub.Broadcast(workspace, Event{Type: "issue.updated", Payload: "must not arrive"})
	_, _, err = socket.Read(ctx)
	require.Error(t, err)
	require.Equal(t, websocket.StatusPolicyViolation, websocket.CloseStatus(err))
}

func TestDeliveryPolicyPreservesPublicEventsAndFailsClosed(t *testing.T) {
	workspace, user := uuid.New(), uuid.New()
	client := &Client{workspaceID: workspace, userID: user}
	raw := []byte(`{"type":"issue.updated","payload":{"title":"Public"}}`)
	for _, tc := range []struct {
		name               string
		check              AccessCheck
		allowed, unchanged bool
	}{
		{"public member", func(context.Context, uuid.UUID, uuid.UUID) (bool, bool, error) { return true, false, nil }, true, true},
		{"private member", func(context.Context, uuid.UUID, uuid.UUID) (bool, bool, error) { return true, true, nil }, true, false},
		{"removed member", func(context.Context, uuid.UUID, uuid.UUID) (bool, bool, error) { return false, false, nil }, false, false},
		{"database error", func(context.Context, uuid.UUID, uuid.UUID) (bool, bool, error) {
			return true, false, errors.New("offline")
		}, false, false},
		{"missing authorizer", nil, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, allowed := NewHub(tc.check).delivery(context.Background(), client, raw)
			require.Equal(t, tc.allowed, allowed)
			if tc.unchanged {
				require.Equal(t, raw, data)
			} else {
				require.NotContains(t, string(data), "Public")
			}
		})
	}
}

func TestPrivateWorkspaceDoesNotRelayIncomingPresence(t *testing.T) {
	hub := NewHub(func(context.Context, uuid.UUID, uuid.UUID) (bool, bool, error) { return true, true, nil })
	workspace := uuid.New()
	ready := make(chan *Client, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		client := hub.Register(conn, workspace, uuid.New())
		defer hub.Unregister(client)
		ready <- client
		go hub.WritePump(r.Context(), client)
		hub.ReadPump(r.Context(), client)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sender, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	require.NoError(t, err)
	defer func() { _ = sender.CloseNow() }()
	<-ready
	receiver, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	require.NoError(t, err)
	defer func() { _ = receiver.CloseNow() }()
	<-ready
	for _, kind := range []string{"presence.join", "cursor.move", "focus.update"} {
		payload, _ := json.Marshal(map[string]any{"type": kind, "payload": map[string]string{"issue_id": "private-issue", "secret": "private-content"}})
		require.NoError(t, sender.Write(ctx, websocket.MessageText, payload))
	}
	readCtx, readCancel := context.WithTimeout(ctx, 150*time.Millisecond)
	defer readCancel()
	_, _, err = receiver.Read(readCtx)
	require.Error(t, err, "no relayed presence or focus payload should arrive")
	require.Empty(t, hub.GetIssueViewers(workspace, "private-issue", nil))
}

func TestConcurrentBroadcastAndUnregister(t *testing.T) {
	hub := NewHub()
	workspace := uuid.New()
	var workers sync.WaitGroup
	for i := 0; i < 30; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			c := hub.Register(nil, workspace, uuid.New())
			hub.Broadcast(workspace, Event{Type: "test"})
			hub.BroadcastToUser(workspace, c.userID, Event{Type: "test"})
			hub.BroadcastExcluding(workspace, c, Event{Type: "test"})
			hub.Unregister(c)
			hub.sendToClient(c, Event{Type: "test"})
			hub.Unregister(c)
		}()
	}
	workers.Wait()
}
