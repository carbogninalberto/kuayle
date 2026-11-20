package main

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/kuayle/kuayle-backend/internal/realtime"
	log "github.com/sirupsen/logrus"
)

// Each HTTP replica wakes its own clients when a worker commits a
// notice. Notification rows remain readable after downtime or a missed wake.
func listenWorkerNotifications(ctx context.Context, databaseURL string, hub *realtime.Hub) {
	for ctx.Err() == nil {
		err := receiveWorkerNotifications(ctx, databaseURL, hub)
		if ctx.Err() != nil {
			return
		}
		// Connection errors can include deployment-private connection details.
		if err != nil {
			log.Warn("Worker notification listener disconnected")
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

func receiveWorkerNotifications(ctx context.Context, databaseURL string, hub *realtime.Hub) error {
	conn, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer conn.Close(context.Background())
	if _, err = conn.Exec(ctx, `LISTEN agent_automation_approval`); err != nil {
		return err
	}
	if _, err = conn.Exec(ctx, `LISTEN host_disk_low`); err != nil {
		return err
	}
	for {
		notice, err := conn.WaitForNotification(ctx)
		if err != nil {
			return err
		}
		workspace, owner, ok := strings.Cut(notice.Payload, "/")
		ws, e1 := uuid.Parse(workspace)
		user, e2 := uuid.Parse(owner)
		if ok && e1 == nil && e2 == nil {
			hub.BroadcastToUser(ws, user, realtime.Event{Type: "notification.created", Payload: map[string]string{"type": notice.Channel}})
		}
	}
}
