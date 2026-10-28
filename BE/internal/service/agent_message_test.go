package service

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/realtime"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

type messageCommentStoreFake struct {
	agentMessageStore
	due []*repository.AgentMessageComment
}

func (f *messageCommentStoreFake) PostNextComment(context.Context) (*repository.AgentMessageComment, error) {
	if len(f.due) == 0 {
		return nil, nil
	}
	next := f.due[0]
	f.due = f.due[1:]
	return next, nil
}

type broadcastFake struct{ events []realtime.Event }

func (f *broadcastFake) Broadcast(_ uuid.UUID, event realtime.Event) {
	f.events = append(f.events, event)
}

func TestPostedAgentCommentsAreAnnouncedOnlyWhenPosted(t *testing.T) {
	posted := &repository.AgentMessageComment{MessageID: uuid.New(), WorkspaceID: uuid.New(), IssueID: uuid.New(), Identifier: "ENG-4", CommentID: uuid.New(), Posted: true}
	skipped := &repository.AgentMessageComment{MessageID: uuid.New(), Identifier: "ENG-5"}
	events := &broadcastFake{}
	service := &AgentMessageService{store: &messageCommentStoreFake{due: []*repository.AgentMessageComment{skipped, posted, posted}}, events: events}
	count, err := service.PostComments(context.Background(), 2)
	require.NoError(t, err)
	require.Equal(t, 1, count, "the limit bounds one pass")
	require.Len(t, events.events, 1)
	require.Equal(t, "comment.created", events.events[0].Type)
	require.Equal(t, map[string]string{"issue_id": posted.IssueID.String(), "comment_id": posted.CommentID.String(), "identifier": "ENG-4"}, events.events[0].Payload)
}
