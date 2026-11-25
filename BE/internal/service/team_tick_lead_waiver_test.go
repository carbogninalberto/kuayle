package service

import (
	"encoding/json"
	"testing"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// A standing waiver (KUA-305) holds for every head, so the checks the lead
// is told of (KUA-302) show the check waived, as the tick's gate treats it,
// whatever the head.
func TestTickTheLeadHearsAStandingWaiverAsWaived(t *testing.T) {
	w := newTickWorld(t, withLead)
	w.agent = func(role string, turn int, task domain.AgentTask) tickTurn {
		if role == "reviewer" {
			return tickTurn{final: "Verdict: request_changes\nPersist the toggle."}
		}
		if role == domain.PipelineLeadRole {
			return tickTurn{final: "Waiting."}
		}
		return w.writesAndApproves(role, turn, task)
	}
	_, err := w.f.tasks.Repo.PostRoomMessage(w.f.ctx, w.f.ws, w.root, domain.TaskRoomPost{Kind: domain.RoomWaiver, Body: "The owner reviewed it by hand.", Data: domain.TaskRoomData{Check: "reviewer", Standing: true}})
	require.NoError(t, err)
	w.drain()
	w.later()
	checks := w.facts(domain.LeadFactChecks)
	require.NotEmpty(t, checks)
	var states map[string]string
	require.NoError(t, json.Unmarshal(checks[len(checks)-1].Data.Value, &states))
	require.Equal(t, "waived", states["reviewer"])
}
