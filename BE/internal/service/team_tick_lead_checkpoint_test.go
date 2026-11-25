package service

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// The Phase 5 checkpoint (plan, Phase 5) on the scenario world, as far as
// KUA-300 to KUA-302 reach: the lead runs the task and the owner reads.

// From a spec, the task reaches "ready for approval" while the owner only
// reads the lead's summaries: the owner posts nothing, the lead moves the
// task on each time Kuayle says the phase holds and reports to the owner at
// every turn, and Kuayle asks the reviewer and waits for the owner's
// approval. (The summaries reach the owner's notifications in P5.4; here the
// lead's posts to @human are asserted.)
func TestCheckpointFromASpecTheLeadBringsTheTaskToApproval(t *testing.T) {
	w := newTickWorld(t, withLead)
	w.github.ci("passed", "")
	phases := []string{"implement", "pr", "review", "integrate", "approval"}
	w.agent = func(role string, turn int, task domain.AgentTask) tickTurn {
		if role != domain.PipelineLeadRole {
			return w.writesAndApproves(role, turn, task)
		}
		current := slices.Index(phases, w.phase())
		report, _ := json.Marshal(map[string]string{"to": "@human", "kind": "summary", "body": fmt.Sprintf("Turn %d: the task is in %s.", turn, w.phase())})
		posts := []string{string(report)}
		if strings.Contains(task.Prompt, "Every required check up to") && current >= 0 && current < len(phases)-1 {
			move, _ := json.Marshal(map[string]any{"to": "@all", "kind": "phase", "body": "The phase holds.", "data": map[string]string{"phase": phases[current+1]}})
			posts = append(posts, string(move))
		}
		return tickTurn{final: "Reported to the owner.", posts: posts}
	}
	w.drain()

	ready := w.task()
	require.Equal(t, "approval", w.phase())
	require.Equal(t, "awaiting_review", ready.State)
	require.NotNil(t, ready.WorkflowHeldSince, "ready for approval: the owner is awaited")
	require.Contains(t, ready.Reason, "Nobody has approved merging")
	require.Equal(t, []string{"pr", "review", "integrate", "approval"}, w.leadPhases())
	require.Equal(t, 1, w.runtime.turns["reviewer"])
	var owner, reports int
	require.NoError(t, w.f.db.Get(&owner, `SELECT count(*) FROM task_messages WHERE task_id=$1 AND author_kind='human'`, w.root))
	require.NoError(t, w.f.db.Get(&reports, `SELECT count(*) FROM task_messages WHERE task_id=$1 AND author_role=$2 AND kind='summary' AND 'human'=ANY(to_roles) AND reject_reason IS NULL`, w.root, domain.PipelineLeadRole))
	require.Zero(t, owner, "the owner only read")
	require.Equal(t, w.runtime.turns[domain.PipelineLeadRole], reports, "a summary for the owner at every turn of the lead")
	require.Empty(t, w.github.merged)
}

// The lead cannot post a verdict, a fact, an ungranted waiver or a grant:
// each is refused with its reason, and none of them counts. The reviewer's
// check stays unmet without the reviewer's verdict, CI stays failed on
// GitHub's word, the waiver waives nothing (the grant it tried first was
// refused too), and a lead that moved straight to the merge merges nothing.
func TestCheckpointTheLeadCannotPostAVerdictAFactAnUngrantedWaiverOrAGrant(t *testing.T) {
	w := newTickWorld(t, withLead)
	w.github.ci("failed", "TestDarkMode failed")
	w.agent = func(role string, turn int, task domain.AgentTask) tickTurn {
		switch {
		case role == "reviewer":
			return tickTurn{hold: true}
		case role != domain.PipelineLeadRole:
			return w.writesAndApproves(role, turn, task)
		case turn == 1:
			return tickTurn{final: "Tried to ship it myself.", posts: []string{
				`{"to":"@all","kind":"verdict","body":"Looks right.","data":{"verdict":"approve","commit":"` + sha(1) + `"}}`,
				`{"to":"@all","kind":"fact","body":"CI is green.","data":{"fact":"ci_green","value":true}}`,
				`{"to":"@all","kind":"grant","body":"I may waive CI.","data":{"check":"ci"}}`,
				`{"to":"@all","kind":"waiver","body":"CI is flaky.","data":{"check":"ci"}}`,
				`{"to":"@all","kind":"phase","body":"Merge it.","data":{"phase":"merge"}}`,
			}}
		}
		return tickTurn{final: "Waiting."}
	}
	w.drain()

	refused := map[string]string{}
	for _, m := range w.room() {
		if m.AuthorRole != nil && *m.AuthorRole == domain.PipelineLeadRole && m.RejectReason != nil {
			refused[m.Kind] = *m.RejectReason
		}
	}
	require.Equal(t, map[string]string{
		domain.RoomVerdict: "the lead may not post verdict: the lead gives no verdict; ask a role that reviews with --request review",
		domain.RoomFact:    "the lead may not post fact: only Kuayle posts facts",
		domain.RoomGrant:   "the lead may not post grant: only the task's owner grants",
		domain.RoomWaiver:  "the lead waives only the checks granted to it",
	}, refused)
	require.Equal(t, []string{"merge"}, w.leadPhases(), "its move counts: moving is the lead's")

	var verdicts int
	require.NoError(t, w.f.db.Get(&verdicts, `SELECT count(*) FROM task_verdicts WHERE task_id=$1`, w.root))
	require.Zero(t, verdicts)
	checks := w.facts(domain.LeadFactChecks)
	require.NotEmpty(t, checks)
	var states map[string]string
	require.NoError(t, json.Unmarshal(checks[len(checks)-1].Data.Value, &states))
	require.Equal(t, "unmet", states["reviewer"], "no verdict but the reviewer's")
	require.Equal(t, "unmet", states["ci"], "CI failed on GitHub, whatever the lead posted, and nothing waived it")
	require.Empty(t, readTickState(w.task()).Waivers)
	require.Empty(t, w.github.merged)
	require.NotEqual(t, "completed", w.task().State)
}
