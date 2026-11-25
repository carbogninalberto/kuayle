package service

import (
	"testing"

	"github.com/kuayle/kuayle-backend/internal/domain"
	gh "github.com/kuayle/kuayle-backend/pkg/github"
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

// Seeds 40 and 47: someone else pushed the head. The script check ran on
// the commit Kuayle published, and the reviewer reviewed that one; neither
// can read the pushed head, which nothing changes by itself. Only the
// writer can move it (its turn publishes a head of its own), so the tick
// sends it there rather than polling for the merge wait's two hours.
func TestTickRegressionSeed40AHeadKuayleDidNotPublishReachesTheWriter(t *testing.T) {
	w := newTickWorld(t, func(p *domain.Pipeline) {
		withLead(p)
		p.Phases[3].Checks = append(p.Phases[3].Checks, domain.PipelineCheck{ID: "tests", Fact: domain.PipelineFactScript, Script: "make test", Required: true})
	})
	w.agent = func(role string, turn int, task domain.AgentTask) tickTurn {
		if role == domain.PipelineLeadRole {
			return leadMove("integrate", "Kuayle checks the rest.")
		}
		return w.writesAndApproves(role, turn, task)
	}
	w.drain()
	require.Equal(t, 1, w.runtime.turns["implementer"])
	// Someone pushes while the check runs on the published commit.
	w.github.parents, w.github.onBase, w.github.changes = map[string][]string{}, map[string]bool{}, map[string][]gh.CompareFile{}
	w.head(sha(0xf00))
	w.runCheck(0, "ok\n")
	w.github.ci("passed", "")
	for range 3 {
		w.later()
	}
	require.GreaterOrEqual(t, w.runtime.turns["implementer"], 2, "the writer publishes a head the checks can read: %s", w.task().Reason)
}

func sliceHas(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
