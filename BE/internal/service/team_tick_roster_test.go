package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// The roster (KUA-288, plan P4.1): a role a required check names is asked
// for its verdict when its phase needs it, and an on-call role works only
// when it is addressed by name; it costs nothing until then.

// withOnCallUIUX adds an on-call uiux role to the team pipeline, with an
// advisory verdict in the review phase: nobody requires it.
func withOnCallUIUX(p *domain.Pipeline) {
	p.Roster = append(p.Roster, domain.PipelineMember{Role: "uiux", ProfileID: p.Roster[1].ProfileID, OnCall: true})
	p.Phases[2].Checks = append(p.Phases[2].Checks, domain.PipelineCheck{ID: "uiux", Fact: domain.PipelineFactVerdict, Role: "uiux"})
}

// onCallTalk is the team when people talk to everyone: the writer commits
// once and asks everyone a question; the reviewer approves.
func (w *tickWorld) onCallTalk(role string, turn int, task domain.AgentTask) tickTurn {
	switch {
	case role == "implementer" && turn == 1:
		return tickTurn{commit: sha(1), final: "Added the toggle.", posts: []string{`{"to":"@all","kind":"ask","body":"Does anyone know the brand's dark palette?"}`}}
	case role == "implementer":
		return tickTurn{final: "Noted."}
	}
	return tickTurn{final: "Verdict: approve\nThe toggle works."}
}

// onCallMember reports whether the task has a member for a role, and how
// many turns the role took.
func (w *tickWorld) onCallMember(role string) (bool, int) {
	w.t.Helper()
	var exists bool
	require.NoError(w.t, w.f.db.Get(&exists, `SELECT EXISTS(SELECT 1 FROM task_members WHERE task_id=$1 AND role=$2)`, w.root, role))
	return exists, w.runtime.turns[role]
}

// An on-call role nobody addresses takes no turn, from the first turn to the
// merge, whatever is said to everyone: it has no member and no runs.
func TestTickAnOnCallRoleNobodyAddressedTakesNoTurn(t *testing.T) {
	w := newTickWorld(t, withOnCallUIUX)
	w.agent = w.onCallTalk
	w.drain()
	_, err := w.f.tasks.Repo.PostRoomMessage(w.f.ctx, w.f.ws, w.root, domain.TaskRoomPost{Kind: domain.RoomSay, Body: "Keep the v1 route."})
	require.NoError(t, err)
	w.drain()
	require.GreaterOrEqual(t, w.runtime.turns["reviewer"], 2, "what is said to everyone calls the roles that are not on call")
	w.github.ci("passed", "")
	w.later()
	require.Equal(t, "approval", w.phase())
	require.NoError(t, w.decide(WorkflowDecisionApprove))
	w.drain()
	require.Equal(t, "completed", w.task().State)

	member, turns := w.onCallMember("uiux")
	require.False(t, member, "an on-call role is a member only once addressed")
	require.Zero(t, turns)
	var tasks int
	require.NoError(t, w.f.db.Get(&tasks, `SELECT count(*) FROM agent_tasks WHERE review_of_task_id=$1`, w.root))
	require.Equal(t, 1, tasks, "the reviewer's task only")
}

// The first post addressed to an on-call role makes it a member and starts
// its turn, which reads that post even in a long room; it is not asked for
// a verdict nobody requires.
func TestTickTheFirstAddressStartsAnOnCallRole(t *testing.T) {
	w := newTickWorld(t, withOnCallUIUX)
	w.agent = w.onCallTalk
	w.drain()
	member, _ := w.onCallMember("uiux")
	require.False(t, member)

	// A long room: more posts than one turn reads, and more text than its
	// prompt holds.
	for i := range 240 {
		value, _ := json.Marshal(i)
		_, err := w.f.tasks.Repo.RecordRoomMessage(w.f.ctx, w.root, domain.TaskRoomPost{AuthorKind: domain.RoomAuthorKuayle, Kind: domain.RoomFact,
			Body: fmt.Sprintf("Filler fact %d. %s", i, strings.Repeat("Kuayle read the pull request again. ", 6)), Data: domain.TaskRoomData{Fact: "filler", Value: value}})
		require.NoError(t, err)
	}
	_, err := w.f.tasks.Repo.PostRoomMessage(w.f.ctx, w.f.ws, w.root, domain.TaskRoomPost{Kind: domain.RoomAsk, ToRoles: []string{"uiux"}, Body: "Is the contrast of the toggle right?"})
	require.NoError(t, err)
	w.drain()

	uiux := w.member("uiux")
	require.Equal(t, 1, uiux.Turns)
	require.NotNil(t, uiux.MemberTaskID)
	require.Equal(t, sha(1), *uiux.PinnedCommit, "it reads the newest commit Kuayle published")
	require.False(t, uiux.VerdictAsked, "an advisory verdict is not asked for")
	prompt := w.prompt("uiux", 1)
	require.Contains(t, prompt, "You are the @uiux")
	require.Contains(t, prompt, "Is the contrast of the toggle right?", "its first turn reads the post that called it")
	require.Contains(t, prompt, "Nobody asked you for a verdict")
	require.Equal(t, 1, w.runtime.turns["uiux"])
}
