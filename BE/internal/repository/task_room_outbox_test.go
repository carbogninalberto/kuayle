package repository

import (
	"context"
	"strings"
	"testing"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func roomFile(name, data string) domain.AgentOutboxFile {
	return domain.AgentOutboxFile{Worktree: "/workspace/repo", Name: name, Data: []byte(data)}
}

// An agent's outbox files addressed to the room are posted as the role its
// task plays; a refusal is kept for it with the reason, a file taken twice
// is posted once, and other files are left for the agent messages.
func TestOutboxFilesToTheRoomArePostedAsTheTasksRole(t *testing.T) {
	f := newRoomFixture(t)
	files := []domain.AgentOutboxFile{
		roomFile("a.json", `{"to":"@reviewer","kind":"ask","body":"Is v1 still used?"}`),
		roomFile("b.json", `{"to":"@reviewer","kind":"verdict","data":{"verdict":"approve","commit":"abc1234"}}`),
		roomFile("c.json", `{"to":"@reviewer","body":`),
		roomFile("d.json", `{"to":"ENG-12","body":"for an issue"}`),
		{Worktree: "/workspace/repo", Name: "e.json", Data: []byte(`{"to":"@all","body":"` + strings.Repeat("x", 100)), Oversize: true},
	}
	rest, recorded, err := f.repo.RecordRoomOutbox(context.Background(), f.task.ID, files)
	require.NoError(t, err)
	require.Equal(t, 4, recorded)
	require.Equal(t, []domain.AgentOutboxFile{files[3]}, rest)

	room := f.room()
	require.Len(t, room, 4)
	ask := room[0]
	require.Equal(t, "implementer", *ask.AuthorRole, "the root task plays the pipeline's writer")
	require.Equal(t, domain.RoomAuthorMember, ask.AuthorKind)
	require.Equal(t, f.task.RunID, *ask.RunID)
	require.Equal(t, []string{"reviewer"}, []string(ask.ToRoles))
	require.Nil(t, ask.RejectReason)
	require.Equal(t, "the implementer may not post verdict", *room[1].RejectReason)
	require.Contains(t, *room[2].RejectReason, "invalid JSON")
	require.Equal(t, "message file is too large", *room[3].RejectReason)

	_, again, err := f.repo.RecordRoomOutbox(context.Background(), f.task.ID, files[:1])
	require.NoError(t, err)
	require.Len(t, f.room(), 4, "the same file of the same run is posted once")
	require.Equal(t, 1, again, "a repeat counts as taken")

	// The implementer reads the refusals of its own posts on its next turn.
	prompt := domain.RoomTurnPrompt("implementer", room)
	require.Contains(t, prompt, "was refused: the implementer may not post verdict")
}

// A task that runs no pipeline has no room: its room files go on to the
// agent messages, which refuse them with the reason.
func TestRoomOutboxFilesOfATaskWithoutAPipelineAreRefused(t *testing.T) {
	f := newRoomFixture(t)
	file := roomFile("a.json", `{"to":"@reviewer","body":"hello"}`)
	rest, recorded, err := f.repo.RecordRoomOutbox(context.Background(), f.plain.ID, []domain.AgentOutboxFile{file})
	require.NoError(t, err)
	require.Zero(t, recorded)
	require.Equal(t, []domain.AgentOutboxFile{file}, rest)

	_, err = NewAgentMessageRepository(f.db).RecordTaskOutbox(context.Background(), f.plain.ID, rest)
	require.NoError(t, err)
	var row struct {
		State  string  `db:"state"`
		Reason *string `db:"reject_reason"`
		ToRef  string  `db:"to_ref"`
	}
	require.NoError(t, f.db.Get(&row, `SELECT state,reject_reason,to_ref FROM agent_messages WHERE from_task_id=$1`, f.plain.ID))
	require.Equal(t, "rejected", row.State)
	require.Equal(t, "@REVIEWER", row.ToRef)
	require.Equal(t, "the task room is only for tasks on a pipeline workflow", *row.Reason)
}
