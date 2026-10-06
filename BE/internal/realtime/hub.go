package realtime

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
	"nhooyr.io/websocket"
)

type Event struct {
	Type      string      `json:"type"`
	Timestamp time.Time   `json:"timestamp"`
	Payload   interface{} `json:"payload"`
}

type Client struct {
	conn         *websocket.Conn
	workspaceID  uuid.UUID
	userID       uuid.UUID
	send         chan []byte
	viewingIssue string // issue ID currently being viewed (empty if none)
}

// Incoming message types from clients
type IncomingMessage struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type PresencePayload struct {
	IssueID string `json:"issue_id"`
}

type CursorPayload struct {
	IssueID string  `json:"issue_id"`
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
}

type AccessCheck func(context.Context, uuid.UUID, uuid.UUID) (member bool, private bool, err error)

type Hub struct {
	access  AccessCheck
	mu      sync.RWMutex
	clients map[uuid.UUID]map[*Client]bool // workspaceID -> clients
}

func NewHub(access ...AccessCheck) *Hub {
	hub := &Hub{clients: make(map[uuid.UUID]map[*Client]bool)}
	if len(access) > 0 {
		hub.access = access[0]
	}
	return hub
}

func (h *Hub) policy(ctx context.Context, client *Client) (bool, bool) {
	if h.access == nil {
		return false, true
	}
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	member, private, err := h.access(checkCtx, client.workspaceID, client.userID)
	return err == nil && member, private
}

var privateRefresh = []byte(`{"type":"app.refresh","payload":{"resources":["workspace","teams","projects","issues","members","views","notifications","favorites","cycles"],"privacy":true}}`)

// Read policy at dequeue: authorization when enqueuing is insufficient for
// messages buffered before removal or a visibility transition.
func (h *Hub) delivery(ctx context.Context, client *Client, msg []byte) ([]byte, bool) {
	member, private := h.policy(ctx, client)
	if !member {
		return nil, false
	}
	if private {
		return privateRefresh, true
	}
	return msg, true
}

func (h *Hub) Register(conn *websocket.Conn, workspaceID, userID uuid.UUID) *Client {
	client := &Client{
		conn:        conn,
		workspaceID: workspaceID,
		userID:      userID,
		send:        make(chan []byte, 256),
	}

	h.mu.Lock()
	if h.clients[workspaceID] == nil {
		h.clients[workspaceID] = make(map[*Client]bool)
	}
	h.clients[workspaceID][client] = true
	h.mu.Unlock()

	return client
}

func (h *Hub) Unregister(client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if clients, ok := h.clients[client.workspaceID]; ok && clients[client] {
		delete(clients, client)
		close(client.send)
		if len(clients) == 0 {
			delete(h.clients, client.workspaceID)
		}
	}
}

func (h *Hub) Broadcast(workspaceID uuid.UUID, event Event) {
	event.Timestamp = time.Now()

	data, err := json.Marshal(event)
	if err != nil {
		log.WithError(err).Error("failed to marshal event")
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()
	clients := h.clients[workspaceID]

	for client := range clients {
		select {
		case client.send <- data:
		default:
			// Client buffer full, skip
		}
	}
}

func (h *Hub) BroadcastToUser(workspaceID, userID uuid.UUID, event Event) {
	event.Timestamp = time.Now()

	data, err := json.Marshal(event)
	if err != nil {
		log.WithError(err).Error("failed to marshal event")
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()
	clients := h.clients[workspaceID]

	for client := range clients {
		if client.userID == userID {
			select {
			case client.send <- data:
			default:
			}
		}
	}
}

func (h *Hub) WritePump(ctx context.Context, client *Client) {
	// Idle sockets are also revalidated; removal need not wait for a new event.
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	defer func() { _ = client.conn.Close(websocket.StatusPolicyViolation, "Connection access changed") }()
	for {
		select {
		case msg, ok := <-client.send:
			if !ok {
				return
			}
			var allowed bool
			msg, allowed = h.delivery(ctx, client, msg)
			if !allowed {
				return
			}
			err := client.conn.Write(ctx, websocket.MessageText, msg)
			if err != nil {
				return
			}
		case <-ticker.C:
			member, _ := h.policy(ctx, client)
			if !member {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

func (h *Hub) ReadPump(ctx context.Context, client *Client) {
	defer h.handlePresenceLeave(client)

	for {
		_, data, err := client.conn.Read(ctx)
		if err != nil {
			return
		}

		member, private := h.policy(ctx, client)
		if !member {
			return
		}
		if private {
			// Presence, cursor and arbitrary focus payloads cannot safely be
			// relayed across team boundaries in this increment.
			h.mu.Lock()
			client.viewingIssue = ""
			h.mu.Unlock()
			continue
		}
		var msg IncomingMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}

		switch msg.Type {
		case "presence.join":
			var p PresencePayload
			if json.Unmarshal(msg.Payload, &p) == nil && p.IssueID != "" {
				h.handlePresenceJoin(client, p.IssueID)
			}
		case "presence.leave":
			h.handlePresenceLeave(client)
		case "cursor.move":
			var p CursorPayload
			if json.Unmarshal(msg.Payload, &p) == nil && p.IssueID != "" {
				h.BroadcastExcluding(client.workspaceID, client, Event{
					Type: "cursor.move",
					Payload: map[string]interface{}{
						"issue_id": p.IssueID,
						"user_id":  client.userID.String(),
						"x":        p.X,
						"y":        p.Y,
					},
				})
			}
		case "focus.update", "focus.leave":
			// Relay focus/cursor-position events to other clients viewing the same issue
			var raw map[string]interface{}
			if json.Unmarshal(msg.Payload, &raw) == nil {
				raw["user_id"] = client.userID.String()
				h.BroadcastExcluding(client.workspaceID, client, Event{
					Type:    msg.Type,
					Payload: raw,
				})
			}
		}
	}
}

func (h *Hub) handlePresenceJoin(client *Client, issueID string) {
	// Leave previous issue if any
	h.mu.RLock()
	previous := client.viewingIssue
	h.mu.RUnlock()
	if previous != "" && previous != issueID {
		h.handlePresenceLeave(client)
	}
	h.mu.Lock()
	client.viewingIssue = issueID
	h.mu.Unlock()

	// Broadcast join to workspace (excluding sender)
	h.BroadcastExcluding(client.workspaceID, client, Event{
		Type: "presence.join",
		Payload: map[string]interface{}{
			"issue_id": issueID,
			"user_id":  client.userID.String(),
		},
	})

	// Send sync to the joining client with current viewers
	viewers := h.GetIssueViewers(client.workspaceID, issueID, client)
	viewerIDs := make([]string, len(viewers))
	for i, uid := range viewers {
		viewerIDs[i] = uid.String()
	}
	h.sendToClient(client, Event{
		Type: "presence.sync",
		Payload: map[string]interface{}{
			"issue_id": issueID,
			"users":    viewerIDs,
		},
	})
}

func (h *Hub) handlePresenceLeave(client *Client) {
	h.mu.Lock()
	issueID := client.viewingIssue
	client.viewingIssue = ""
	h.mu.Unlock()
	if issueID == "" {
		return
	}

	h.BroadcastExcluding(client.workspaceID, client, Event{
		Type: "presence.leave",
		Payload: map[string]interface{}{
			"issue_id": issueID,
			"user_id":  client.userID.String(),
		},
	})
}

func (h *Hub) BroadcastExcluding(workspaceID uuid.UUID, exclude *Client, event Event) {
	event.Timestamp = time.Now()

	data, err := json.Marshal(event)
	if err != nil {
		log.WithError(err).Error("failed to marshal event")
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()
	clients := h.clients[workspaceID]

	for client := range clients {
		if client == exclude {
			continue
		}
		select {
		case client.send <- data:
		default:
		}
	}
}

func (h *Hub) GetIssueViewers(workspaceID uuid.UUID, issueID string, exclude *Client) []uuid.UUID {
	h.mu.RLock()
	defer h.mu.RUnlock()

	var viewers []uuid.UUID
	for client := range h.clients[workspaceID] {
		if client.viewingIssue == issueID && client != exclude {
			viewers = append(viewers, client.userID)
		}
	}
	return viewers
}

func (h *Hub) sendToClient(client *Client, event Event) {
	event.Timestamp = time.Now()
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	if !h.clients[client.workspaceID][client] {
		return
	}
	select {
	case client.send <- data:
	default:
	}
}
