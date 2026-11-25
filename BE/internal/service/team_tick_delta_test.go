package service

import (
	"encoding/json"
	"fmt"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// Delta reviews (P4.4, KUA-292): a role's later review pass is told the
// commit its last verdict named, the new head, the diff between them and its
// own earlier findings; its first pass is told none of it.

// threeRounds plays the team pipeline through three review rounds: the
// reviewer asks for changes twice (the second time in the room, its final
// message apart) and approves the third head.
func threeRounds(w *tickWorld) func(role string, turn int, task domain.AgentTask) tickTurn {
	return func(role string, turn int, task domain.AgentTask) tickTurn {
		switch {
		case role == "reviewer" && turn == 1:
			return tickTurn{final: "Verdict: request_changes\nThe toggle is not persisted across reloads."}
		case role == "reviewer" && turn == 2:
			return tickTurn{final: "Posted my review in the room.", posts: []string{
				fmt.Sprintf(`{"to":"@all","kind":"verdict","body":"The system theme is ignored.","data":{"verdict":"request_changes","commit":%q}}`, sha(2))}}
		}
		return w.writesAndApproves(role, turn, task)
	}
}

func TestTickDeltaReviewTellsALaterPassWhatItReviewed(t *testing.T) {
	w := newTickWorld(t, nil)
	w.agent = threeRounds(w)
	w.drain()
	require.Equal(t, "integrate", w.phase(), "the third head is approved")
	require.Equal(t, 3, w.member("reviewer").Turns)

	first := w.prompt("reviewer", 1)
	require.NotContains(t, first, "Your earlier review", "the first pass is a plain review")
	require.NotContains(t, first, "You last reviewed")

	second := w.prompt("reviewer", 2)
	require.Contains(t, second, fmt.Sprintf("You last reviewed commit %s and gave request_changes. The head is now %s.", sha(1), sha(2)))
	require.Contains(t, second, fmt.Sprintf("`git diff %s..%s`", sha(1), sha(2)))
	require.Contains(t, second, "The toggle is not persisted across reloads.", "its own findings")

	// The third pass starts from the second verdict, given in the room,
	// and quotes the final message of the turn that gave it.
	third := w.prompt("reviewer", 3)
	require.Contains(t, third, fmt.Sprintf("You last reviewed commit %s and gave request_changes. The head is now %s.", sha(2), sha(3)))
	require.Contains(t, third, fmt.Sprintf("`git diff %s..%s`", sha(2), sha(3)))
	require.Contains(t, third, "The system theme is ignored.")
	require.Contains(t, third, domain.LastTurnMessage("Posted my review in the room."))
}

// The Phase 4 checkpoint: a three-round review costs one cold start per
// role; every later turn carries a resume id, through the real launch path
// (relaunch), and a resumed review pass still gets its delta.
func TestTickCheckpointAThreeRoundReviewColdStartsOncePerRole(t *testing.T) {
	w := newTickWorld(t, nil)
	play := threeRounds(w)
	sessions := map[string][]string{}
	cold := map[string]int{}
	launched := map[string][]domain.DevMachineAgentRun{}
	w.agent = func(role string, turn int, task domain.AgentTask) tickTurn {
		// Each turn's harness reported a session of its own; this turn is
		// launched as Kuayle starts it on its machine.
		previous := ""
		if turn > 1 {
			previous = sessions[role][turn-2]
		}
		run := w.relaunch(task.ID, "codex", previous)
		var argv []string
		require.NoError(t, json.Unmarshal(run.CommandArgv, &argv))
		if slices.Contains(argv, "resume") {
			require.Contains(t, argv, previous, "the @%s's turn %d resumes the session of its turn before", role, turn)
		} else {
			cold[role]++
		}
		sessions[role] = append(sessions[role], uuid.NewString())
		launched[role] = append(launched[role], run)
		return play(role, turn, task)
	}
	w.drain()
	require.Equal(t, "integrate", w.phase())
	require.Equal(t, 3, len(launched["implementer"]))
	require.Equal(t, 3, len(launched["reviewer"]))
	require.Equal(t, map[string]int{"implementer": 1, "reviewer": 1}, cold, "one cold start per role")

	for turn, run := range launched["reviewer"][1:] {
		require.Contains(t, run.Prompt, "this continues your previous session")
		require.Contains(t, run.Prompt, "# Your earlier review", "a resumed pass %d still gets its delta", turn+2)
		require.NotContains(t, run.Prompt, "Previous agent summary", "the handoff does not repeat the final message the delta quotes")
	}
	require.Contains(t, launched["reviewer"][1].Prompt, "The toggle is not persisted across reloads.")
	require.Contains(t, launched["reviewer"][2].Prompt, "The system theme is ignored.")
}
