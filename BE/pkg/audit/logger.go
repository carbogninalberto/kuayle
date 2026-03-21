package audit

import (
	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
)

// Log records a structured audit event for sensitive operations.
func Log(action string, actorID uuid.UUID, fields map[string]interface{}) {
	entry := log.WithFields(log.Fields{
		"audit":    true,
		"action":   action,
		"actor_id": actorID.String(),
	})
	for k, v := range fields {
		entry = entry.WithField(k, v)
	}
	entry.Info("audit event")
}
