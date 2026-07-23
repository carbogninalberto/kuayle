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

type Hub struct {
	mu      sync.RWMutex
	clients map[uuid.UUID]map[*Client]bool // workspaceID -> clients
}

func NewHub() *Hub {
	return &Hub{
		clients: make(map[uuid.UUID]map[*Client]bool),
	}
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
	if clients, ok := h.clients[client.workspaceID]; ok {
		delete(clients, client)
		if len(clients) == 0 {
			delete(h.clients, client.workspaceID)
		}
	}
	h.mu.Unlock()
	close(client.send)
}

func (h *Hub) Broadcast(workspaceID uuid.UUID, event Event) {
	event.Timestamp = time.Now()

	data, err := json.Marshal(event)
	if err != nil {
		log.WithError(err).Error("failed to marshal event")
		return
	}

	h.mu.RLock()
	clients := h.clients[workspaceID]
	h.mu.RUnlock()

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
	clients := h.clients[workspaceID]
	h.mu.RUnlock()

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
	for {
		select {
		case msg, ok := <-client.send:
			if !ok {
				return
			}
			err := client.conn.Write(ctx, websocket.MessageText, msg)
			if err != nil {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

func (h *Hub) ReadPump(ctx context.Context, client *Client) {
	defer func() {
		if client.viewingIssue != "" {
			h.handlePresenceLeave(client)
		}
	}()

	for {
		_, data, err := client.conn.Read(ctx)
		if err != nil {
			return
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
	if client.viewingIssue != "" && client.viewingIssue != issueID {
		h.handlePresenceLeave(client)
	}

	client.viewingIssue = issueID

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
	issueID := client.viewingIssue
	if issueID == "" {
		return
	}
	client.viewingIssue = ""

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
	clients := h.clients[workspaceID]
	h.mu.RUnlock()

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
	select {
	case client.send <- data:
	default:
	}
}
