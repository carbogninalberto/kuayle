package realtime

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

type Event struct {
	Type      string      `json:"type"`
	Timestamp time.Time   `json:"timestamp"`
	Payload   interface{} `json:"payload"`
}

type Client struct {
	conn        *websocket.Conn
	workspaceID uuid.UUID
	userID      uuid.UUID
	send        chan []byte
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
			err := wsjson.Write(ctx, client.conn, json.RawMessage(msg))
			if err != nil {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

func (h *Hub) ReadPump(ctx context.Context, client *Client) {
	for {
		_, _, err := client.conn.Read(ctx)
		if err != nil {
			return
		}
		// We don't process incoming messages for now (client -> server)
	}
}
