package service

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

// A release is asked for from chat, belongs to no issue, and is published only
// after an admin approves, with the notes as edited on the card.
func TestCreateReleaseFromChatPublishesTheEditedNotesAfterApproval(t *testing.T) {
	f := newToolFixture(t)
	github := newReleaseFake()
	skills := repository.NewAssistantSkillRepository(f.db)
	f.service.tools.SetReleases(github, releaseRepos{}, skills)
	approvals := NewAssistantApprovals(f.repo, nil)
	approvals.SetMerging(nil, repository.NewWorkspaceRepository(f.db), nil)
	approvals.SetReleases(github)
	f.service.SetApprovals(approvals)

	ask := func() (domain.ConversationAttempt, []domain.ConversationApproval, string) {
		t.Helper()
		a := f.attempt(t, "Publish v1.3.0")
		provider, requests := scriptedProvider(t, toolCallStream("create_release", createArgs(releaseNotes)), finalAnswer)
		require.NoError(t, f.service.converse(f.ctx, a, provider.URL, "key", []chatCompletionMessage{{Role: "user", Content: "Publish v1.3.0"}}, func(string) error { return nil }))
		h, err := f.repo.History(f.ctx, f.ws, a.Conversation.ID, 0)
		require.NoError(t, err)
		history := (*requests)[1]["messages"].([]any)
		return a, h.Approvals, history[len(history)-1].(map[string]any)["content"].(string)
	}

	// A member is refused when asking, with the reason, and nothing is recorded.
	_, pending, answer := ask()
	require.Empty(t, pending)
	require.Contains(t, answer, "only workspace owners and admins can publish a release")

	_, err := f.db.Exec(`UPDATE workspace_members SET role='admin' WHERE workspace_id=$1 AND user_id=$2`, f.ws, f.user)
	require.NoError(t, err)
	a, pending, answer := ask()
	require.Contains(t, answer, "pending_user_approval")
	require.Len(t, pending, 1)
	approval := pending[0]
	require.Equal(t, "create_release", approval.Action)
	require.Equal(t, uuid.Nil, approval.IssueID, "a release belongs to no issue")
	require.Empty(t, github.created, "asking publishes nothing")

	// Notes that fail the check are refused and the approval stays pending.
	bad := "- staged on 192.168.0.4 first"
	_, err = f.service.ResolveApprovalDecision(f.ctx, f.ws, a.Conversation.ID, approval.ID, true, ModelChoice{}, &ApprovalEdit{Notes: &bad})
	var invalid *ApprovalEditError
	require.True(t, errors.As(err, &invalid))
	require.Contains(t, invalid.Message, "private address")
	h, err := f.repo.History(f.ctx, f.ws, a.Conversation.ID, 0)
	require.NoError(t, err)
	require.Equal(t, "pending", h.Approvals[0].Status)
	require.Empty(t, github.created)

	edited := "### Fixed\n- **Notes edited on the card.**"
	done, err := f.service.ResolveApprovalDecision(f.ctx, f.ws, a.Conversation.ID, approval.ID, true, ModelChoice{}, &ApprovalEdit{Notes: &edited})
	require.NoError(t, err)
	require.Equal(t, "approved", done.Status, "error: %v", done.Error)
	require.Len(t, github.created, 1)
	require.Equal(t, edited, github.created[0].Body, "the edited notes are what gets published")
	require.Equal(t, releaseSHA, github.created[0].TargetCommitish)
	var payload domain.ReleasePayload
	require.NoError(t, json.Unmarshal(done.Payload, &payload))
	require.Equal(t, edited, payload.Notes, "the approval records the notes that were published")
	require.True(t, payload.NotesEdited)

	// A repeated approval publishes nothing more.
	again, err := f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, approval.ID, true)
	require.NoError(t, err)
	require.Equal(t, "approved", again.Status)
	require.Len(t, github.created, 1)

	// An admin demoted before approving is refused by the same role check.
	delete(github.tags, "v1.3.0")
	github.releases = github.releases[1:]
	a, pending, _ = ask()
	require.Len(t, pending, 1)
	_, err = f.db.Exec(`UPDATE workspace_members SET role='member' WHERE workspace_id=$1 AND user_id=$2`, f.ws, f.user)
	require.NoError(t, err)
	refused, err := f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, pending[0].ID, true)
	require.NoError(t, err)
	require.Equal(t, "failed", refused.Status)
	require.Contains(t, *refused.Error, "Only workspace owners and admins can publish a release")
	require.Len(t, github.created, 1)

}
