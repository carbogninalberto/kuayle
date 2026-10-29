package repository

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestOnlyTheTaskCreatorQueuesLiveMessagesInOrder(t *testing.T) {
	f := newMessageFixture(t)
	tasks := NewAgentTaskRepository(f.db)

	first, err := tasks.QueueLiveMessage(f.ctx, f.workspace, f.sender.ID, domain.AgentLiveSenderUser, "  Also add tests\r\n")
	require.NoError(t, err)
	require.Equal(t, int64(1), *first.Seq)
	require.Equal(t, "Also add tests", first.Body)
	require.Equal(t, domain.AgentLiveQueued, first.State)
	require.Equal(t, f.user, *first.AuthorID)
	second, err := tasks.QueueLiveMessage(f.ctx, f.workspace, f.sender.ID, domain.AgentLiveSenderOrchestrator, "Use the v2 route")
	require.NoError(t, err)
	require.Equal(t, int64(2), *second.Seq)

	_, err = tasks.QueueLiveMessage(f.memberCtx, f.workspace, f.sender.ID, domain.AgentLiveSenderUser, "Steal the task")
	require.ErrorIs(t, err, ErrConversationNotFound, "another member cannot steer someone else's agent")
	otherWorkspace := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: uuid.New(), UserID: f.user})
	_, err = tasks.QueueLiveMessage(otherWorkspace, f.workspace, f.sender.ID, domain.AgentLiveSenderUser, "x")
	require.ErrorIs(t, err, ErrConversationNotFound)
	_, err = tasks.QueueLiveMessage(f.ctx, f.workspace, f.sender.ID, domain.AgentLiveSenderAgent, "forged")
	require.ErrorIs(t, err, ErrAgentLiveMessageInvalid, "nobody sends as the agent")
	_, err = tasks.QueueLiveMessage(f.ctx, f.workspace, f.sender.ID, domain.AgentLiveSenderUser, "\u0007 ")
	require.ErrorIs(t, err, ErrAgentLiveMessageInvalid)

	listed, err := tasks.LiveMessages(f.ctx, f.workspace, f.sender.ID, 50)
	require.NoError(t, err)
	require.Len(t, listed, 2)
	require.Equal(t, []string{"Also add tests", "Use the v2 route"}, []string{listed[0].Body, listed[1].Body})
	_, err = tasks.LiveMessages(f.memberCtx, f.workspace, f.sender.ID, 50)
	require.ErrorIs(t, err, ErrConversationNotFound)

	for range domain.AgentLiveMessagesPerHour - 2 {
		_, err = tasks.QueueLiveMessage(f.ctx, f.workspace, f.sender.ID, domain.AgentLiveSenderUser, "ping")
		require.NoError(t, err)
	}
	_, err = tasks.QueueLiveMessage(f.ctx, f.workspace, f.sender.ID, domain.AgentLiveSenderUser, "one too many")
	require.ErrorIs(t, err, ErrAgentLiveMessageRateLimited)

	_, err = f.db.Exec(`UPDATE agent_tasks SET state='completed' WHERE id=$1`, f.recipientTask.ID)
	require.NoError(t, err)
	_, err = tasks.QueueLiveMessage(f.ctx, f.workspace, f.recipientTask.ID, domain.AgentLiveSenderUser, "too late")
	require.ErrorIs(t, err, ErrAgentLiveMessageClosed)
}

// The running attempt receives queued messages through its inbox; what it
// received is never placed in a later prompt, and the rest is.
func TestLiveMessagesReachTheRunningAttemptOrTheNextPrompt(t *testing.T) {
	f := newMessageFixture(t)
	tasks, machines := NewAgentTaskRepository(f.db), NewDevMachineRepository(f.db)
	for _, body := range []string{"first", "second"} {
		_, err := tasks.QueueLiveMessage(f.ctx, f.workspace, f.sender.ID, domain.AgentLiveSenderUser, body)
		require.NoError(t, err)
	}

	pending, err := machines.PendingLiveMessages(f.ctx, f.sender.RunID)
	require.NoError(t, err)
	require.Len(t, pending, 2)
	none, err := machines.PendingLiveMessages(f.ctx, uuid.New())
	require.NoError(t, err)
	require.Empty(t, none, "only the task's current attempt receives its messages")
	marked, err := machines.MarkLiveMessagesDelivered(f.ctx, f.sender.RunID, []uuid.UUID{pending[0].ID})
	require.NoError(t, err)
	require.Equal(t, 1, marked)
	marked, err = machines.MarkLiveMessagesDelivered(f.ctx, uuid.New(), []uuid.UUID{pending[1].ID})
	require.NoError(t, err)
	require.Zero(t, marked, "another attempt cannot mark them")

	next := *f.sender
	next.RunID = uuid.New()
	_, err = f.db.Exec(`UPDATE agent_tasks SET run_id=$1 WHERE id=$2`, next.RunID, next.ID)
	require.NoError(t, err)
	claimed, err := tasks.ClaimLiveMessages(f.ctx, next, 10000)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.Equal(t, "second", claimed[0].Body)
	again, err := tasks.ClaimLiveMessages(f.ctx, next, 10000)
	require.NoError(t, err)
	require.Equal(t, claimed[0].ID, again[0].ID, "recreating the attempt places the same messages")
	pending, err = machines.PendingLiveMessages(f.ctx, next.RunID)
	require.NoError(t, err)
	require.Empty(t, pending, "a message placed in the prompt is not written to the inbox again")

	listed, err := tasks.LiveMessages(f.ctx, f.workspace, f.sender.ID, 50)
	require.NoError(t, err)
	require.Equal(t, domain.AgentLiveViaInbox, *listed[0].DeliveredVia)
	require.Equal(t, f.sender.RunID, *listed[0].RunID)
	require.Equal(t, domain.AgentLiveViaPrompt, *listed[1].DeliveredVia)
	require.Equal(t, next.RunID, *listed[1].RunID)

	_, err = tasks.QueueLiveMessage(f.ctx, f.workspace, f.sender.ID, domain.AgentLiveSenderUser, strings.Repeat("x", 3000))
	require.NoError(t, err)
	later := next
	later.RunID = uuid.New()
	_, err = f.db.Exec(`UPDATE agent_tasks SET run_id=$1 WHERE id=$2`, later.RunID, later.ID)
	require.NoError(t, err)
	fits, err := tasks.ClaimLiveMessages(f.ctx, later, 600)
	require.NoError(t, err)
	require.Len(t, fits, 1, "only messages that fit the prompt are claimed")
	require.Equal(t, "second", fits[0].Body, "a message claimed for an attempt that was never created is claimed again")
}

func TestAgentMessagesToTheOrchestratorAreRecordedForTheTask(t *testing.T) {
	f := newMessageFixture(t)
	recorded, err := f.messages.RecordTaskOutbox(f.ctx, f.sender.ID, []domain.AgentOutboxFile{
		outbox("1.json", `{"to":"orchestrator","kind":"question","body":"Keep the v1 route?"}`),
		outbox("2.json", `{"to":"orchestrator","body":"Tests added"}`),
		outbox("3.json", `{"to":"orchestrator","kind":"order","body":"x"}`),
		outbox("4.json", `{"to":"AUT-4","body":"Schema is merged"}`),
	})
	require.NoError(t, err)
	require.Equal(t, 4, recorded)
	again, err := f.messages.RecordTaskOutbox(f.ctx, f.sender.ID, []domain.AgentOutboxFile{outbox("1.json", `{"to":"orchestrator","kind":"question","body":"Keep the v1 route?"}`)})
	require.NoError(t, err)
	require.Zero(t, again, "a file taken twice is one message")

	listed, err := NewAgentTaskRepository(f.db).LiveMessages(f.ctx, f.workspace, f.sender.ID, 50)
	require.NoError(t, err)
	require.Len(t, listed, 3)
	byBody := map[string]domain.AgentLiveMessage{}
	for _, message := range listed {
		require.Equal(t, domain.AgentLiveFromAgent, message.Direction)
		require.Equal(t, domain.AgentLiveSenderAgent, message.Sender)
		require.Nil(t, message.Seq)
		byBody[message.Body] = message
	}
	require.Equal(t, domain.AgentLiveKindQuestion, byBody["Keep the v1 route?"].Kind)
	require.Equal(t, domain.AgentLiveReceived, byBody["Keep the v1 route?"].State)
	require.Equal(t, f.sender.RunID, *byBody["Keep the v1 route?"].RunID)
	require.Equal(t, domain.AgentLiveKindMessage, byBody["Tests added"].Kind)
	var refused []domain.AgentLiveMessage
	for _, message := range listed {
		if message.State == domain.AgentLiveRejected {
			refused = append(refused, message)
		}
	}
	require.Len(t, refused, 1)
	require.Equal(t, "unknown kind", *refused[0].RejectReason)
	require.Equal(t, map[string]string{"AUT-4": "queued"}, f.states(t), "messages to issues keep their registry")

	_, err = f.db.Exec(`UPDATE agent_tasks SET state='completed' WHERE id=$1`, f.sender.ID)
	require.NoError(t, err)
	_, err = f.messages.RecordTaskOutbox(f.ctx, f.sender.ID, []domain.AgentOutboxFile{outbox("5.json", `{"to":"orchestrator","body":"after the end"}`)})
	require.NoError(t, err)
	var state string
	require.NoError(t, f.db.Get(&state, `SELECT state FROM agent_live_messages WHERE task_id=$1 AND body='after the end'`, f.sender.ID))
	require.Equal(t, domain.AgentLiveRejected, state)

	files := []domain.AgentOutboxFile{}
	for i := range domain.AgentLiveMessagesPerHour {
		files = append(files, outbox("burst-"+strings.Repeat("x", i)+".json", `{"to":"orchestrator","body":"ping `+strings.Repeat("!", i)+`"}`))
	}
	recorded, err = f.messages.RecordTaskOutbox(f.ctx, f.sender.ID, files)
	require.NoError(t, err)
	require.Equal(t, domain.AgentLiveMessagesPerHour-4, recorded, "at most the hourly budget is recorded")
}
