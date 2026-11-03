package service

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// An upgrade belongs to no issue and runs only after its administrator
// approves; a refused or failed start is recorded on the approval.
func TestUpgradeInstanceNeedsApprovalAndRecordsTheOutcome(t *testing.T) {
	f := newToolFixture(t)
	updater := &updaterFake{configured: true}
	admin := f.user
	system := NewAssistantSystem(updater, func(id uuid.UUID) bool { return id == admin }, "0.2.8")
	system.releases = func(context.Context) ([]instanceRelease, error) { return releaseList, nil }
	f.service.tools.SetSystem(system)
	approvals := NewAssistantApprovals(f.repo, nil)
	approvals.SetSystem(system)
	f.service.SetApprovals(approvals)

	ask := func() (domain.ConversationAttempt, domain.ConversationApproval) {
		t.Helper()
		a := f.attempt(t, "Upgrade Kuayle")
		provider, _ := scriptedProvider(t, toolCallStream("upgrade_instance", `{}`), finalAnswer)
		require.NoError(t, f.service.converse(f.ctx, a, provider.URL, "key", []chatCompletionMessage{{Role: "user", Content: "Upgrade Kuayle"}}, func(string) error { return nil }))
		h, err := f.repo.History(f.ctx, f.ws, a.Conversation.ID, 0)
		require.NoError(t, err)
		require.Len(t, h.Approvals, 1)
		return a, h.Approvals[0]
	}

	a, approval := ask()
	require.Equal(t, "upgrade_instance", approval.Action)
	require.Equal(t, uuid.Nil, approval.IssueID)
	var payload domain.UpgradePayload
	require.NoError(t, json.Unmarshal(approval.Payload, &payload))
	require.Equal(t, "0.2.8", payload.CurrentVersion)
	require.Equal(t, "0.3.0", payload.LatestVersion)
	require.Zero(t, updater.started, "asking runs nothing")

	// Denying never touches the updater.
	denied, err := f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, approval.ID, false)
	require.NoError(t, err)
	require.Equal(t, "denied", denied.Status)
	require.Zero(t, updater.started)

	// An updater that cannot start the run leaves a clear reason on the approval.
	updater.startCode = http.StatusConflict
	a, approval = ask()
	failed, err := f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, approval.ID, true)
	require.NoError(t, err)
	require.Equal(t, "failed", failed.Status)
	require.Contains(t, *failed.Error, "already running")

	updater.startCode = 0
	a, approval = ask()
	approved, err := f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, approval.ID, true)
	require.NoError(t, err)
	require.Equal(t, "approved", approved.Status)
	again, err := f.service.ResolveApproval(f.ctx, f.ws, a.Conversation.ID, approval.ID, true)
	require.NoError(t, err)
	require.Equal(t, "approved", again.Status)
	require.Equal(t, 2, updater.started, "one refused start and one approved start; a repeated approval starts nothing")

	// A user who is not an administrator is refused when asking.
	admin = uuid.New()
	a = f.attempt(t, "Upgrade Kuayle")
	provider, requests := scriptedProvider(t, toolCallStream("upgrade_instance", `{}`), finalAnswer)
	require.NoError(t, f.service.converse(f.ctx, a, provider.URL, "key", []chatCompletionMessage{{Role: "user", Content: "Upgrade Kuayle"}}, func(string) error { return nil }))
	history := (*requests)[1]["messages"].([]any)
	require.Contains(t, history[len(history)-1].(map[string]any)["content"], "only an instance administrator can upgrade")
	h, err := f.repo.History(f.ctx, f.ws, a.Conversation.ID, 0)
	require.NoError(t, err)
	require.Empty(t, h.Approvals)
}
