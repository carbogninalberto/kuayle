package service

import (
	"strings"
	"testing"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// The lead asked to split the issue, as a turn's outbox post does.
const leadSplitPost = `{"to":"@human","kind":"request","body":"Two parts","data":{"action":"split","issues":[{"title":"Parser","description":"Write the parser"},{"title":"Docs","description":"Document the parser"}]}}`

// splitWorld is a pipeline task with a lead that asks for a split when the
// owner asks it; otherwise it takes the task to the merge, as a lead moves
// the phases (KUA-301) and wakes for each role's turn (KUA-302).
func splitWorld(t *testing.T) *tickWorld {
	t.Helper()
	w := newTickWorld(t, withLead)
	w.agent = func(role string, turn int, task domain.AgentTask) tickTurn {
		if prompts := w.runtime.prompts[role]; role == domain.PipelineLeadRole && len(prompts) > 0 && strings.Contains(prompts[len(prompts)-1], "one issue or two") {
			return tickTurn{final: "Asked for a split.", posts: []string{leadSplitPost}}
		}
		if role == domain.PipelineLeadRole {
			return leadMove("merge", "Everything else is Kuayle's to check.")
		}
		return w.writesAndApproves(role, turn, task)
	}
	w.exec(`INSERT INTO team_statuses(team_id,name,slug,category) SELECT team_id,'Backlog','backlog','backlog' FROM issues WHERE id=$1 ON CONFLICT DO NOTHING`, w.f.issue)
	w.drain()
	return w
}

func (w *tickWorld) callLead() {
	w.t.Helper()
	_, err := w.f.tasks.Repo.PostRoomMessage(w.f.ctx, w.f.ws, w.root, domain.TaskRoomPost{Kind: domain.RoomSay, ToRoles: []string{domain.PipelineLeadRole}, Body: "Is this one issue or two?"})
	require.NoError(w.t, err)
	w.drain()
	var requests int
	require.NoError(w.t, w.f.db.Get(&requests, `SELECT count(*) FROM task_messages WHERE task_id=$1 AND author_role='lead' AND kind='request' AND data->>'action'='split' AND reject_reason IS NULL`, w.root))
	require.Equal(w.t, 1, requests, "the room took the lead's request")
}

func (w *tickWorld) subIssues() []string {
	w.t.Helper()
	var titles []string
	require.NoError(w.t, w.f.db.Select(&titles, `SELECT title FROM issues WHERE parent_id=$1 ORDER BY number`, w.f.issue))
	return titles
}

// With the owner's split grant, the tick creates the sub-issues the lead
// asked for without the approval step (KUA-313).
func TestTickAGrantedLeadSplitsWithoutAsking(t *testing.T) {
	w := splitWorld(t)
	_, err := w.f.tasks.Repo.PostRoomMessage(w.f.ctx, w.f.ws, w.root, domain.TaskRoomPost{Kind: domain.RoomGrant, Data: domain.TaskRoomData{Role: domain.PipelineLeadRole, Action: domain.RoomRequestSplit}})
	require.NoError(t, err)
	w.callLead()
	require.Equal(t, []string{"Parser", "Docs"}, w.subIssues(), "at the pass the lead's turn wakes, without waiting for a poll")
	created := w.facts("split_approved")
	require.Len(t, created, 1)
	require.Contains(t, created[0].Body, "Created sub-issues")
}

// Without it, the request waits for the owner's approval.
func TestTickALeadSplitWithoutAGrantWaits(t *testing.T) {
	w := splitWorld(t)
	_, err := w.f.tasks.Repo.PostRoomMessage(w.f.ctx, w.f.ws, w.root, domain.TaskRoomPost{Kind: domain.RoomSay, Body: "You may split the issue without asking me."})
	require.NoError(t, err)
	w.callLead()
	w.later()
	require.Empty(t, w.subIssues())
	require.Empty(t, w.facts("split_approved"))
}
