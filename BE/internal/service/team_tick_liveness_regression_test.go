package service

import (
	"testing"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// Regression scenarios for the liveness seeds the v0.4.0 integration
// (KUA-309) found not live, each the seed's shape in the scenario world.

// conflicted has the base move under the head into a conflict, GitHub
// saying so, and stay there.
func (w *tickWorld) conflicted(behind int) {
	no := false
	w.github.status.Behind, w.github.status.Mergeable, w.github.status.MergeableState = behind, &no, "dirty"
	w.github.onStatus = nil
}

// Seed 4: with a lead, the stall summary went to the lead only. When only
// the writer can move the unmet check (here the branch is behind a base it
// conflicts with, so Kuayle cannot update it), the writer hears it too, lead
// or not, as for a verdict only the writer can move.
func TestTickRegressionSeed4WhatOnlyTheWriterCanMoveReachesTheWriter(t *testing.T) {
	w := newTickWorld(t, withLead)
	w.agent = func(role string, turn int, task domain.AgentTask) tickTurn {
		switch {
		case role == domain.PipelineLeadRole:
			return leadMove("integrate", "Kuayle checks the rest.")
		case role == "implementer" && turn > 1:
			return tickTurn{final: "Merging main."}
		}
		return w.writesAndApproves(role, turn, task)
	}
	w.drain()
	require.Equal(t, "integrate", w.phase())
	w.conflicted(2)
	w.github.ci("passed", "")
	w.later()
	w.later()

	require.GreaterOrEqual(t, w.runtime.turns["implementer"], 2, "the writer alone can bring the branch up to date: %s", w.task().Reason)
	require.Contains(t, w.prompt("implementer", 2), "behind main")
	var told bool
	for _, m := range w.posts(domain.RoomSummary) {
		if m.AuthorKind == domain.RoomAuthorKuayle && sliceHas(m.ToRoles, domain.PipelineLeadRole) && sliceHas(m.ToRoles, "implementer") {
			told = true
		}
	}
	require.True(t, told, "one summary to the lead and the writer")
}

func sliceHas(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
