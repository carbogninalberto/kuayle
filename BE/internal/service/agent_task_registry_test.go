package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

type recordedRegistry struct {
	calls []string
	err   error
}

func (r *recordedRegistry) AppendIssueRegistry(_ context.Context, _, issue, user uuid.UUID, text string) error {
	r.calls = append(r.calls, issue.String()+" "+user.String()+" "+text)
	return r.err
}

// A published pull request is logged in the registry of the task's issue as
// the task's creator; a registry that is off or failing never affects the task.
func TestPublishedPullRequestsAreNotedInTheRegistry(t *testing.T) {
	task := domain.AgentTask{ID: uuid.New(), WorkspaceID: uuid.New(), IssueID: uuid.New(), CreatorID: uuid.New()}
	(&AgentTaskService{}).notePublished(context.Background(), task) // not wired: nothing to do

	registry := &recordedRegistry{}
	svc := &AgentTaskService{}
	svc.SetRegistry(registry)
	svc.notePublished(context.Background(), task)
	require.Equal(t, []string{task.IssueID.String() + " " + task.CreatorID.String() + " pull request published"}, registry.calls)

	for _, err := range []error{ErrWikiRegistryDisabled, errors.New("database down")} {
		registry.err = err
		require.NotPanics(t, func() { svc.notePublished(context.Background(), task) })
	}
}
