package service

import (
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/stretchr/testify/require"
)

// Every issue a create, sub-issue create or duplicate writes is announced
// once, after it is stored, for the user who created it.
func TestIssueWebhooksCreatePathsSendOneCreatedEachPostgres(t *testing.T) {
	f := newBulkProjectFixture(t)
	sent := &webhookRecorder{}
	f.svc.SetWebhooks(sent)
	status := f.status.String()

	parent, err := f.svc.Create(f.ctx, f.workspace, f.user, dto.CreateIssueRequest{Title: "Parent", TeamID: f.team.String(), StatusID: &status})
	require.NoError(t, err)
	_, err = f.svc.CreateSubIssue(f.ctx, f.workspace, f.user, parent.Identifier, dto.CreateSubIssueRequest{Title: "Child", StatusID: &status})
	require.NoError(t, err)
	_, err = f.svc.Duplicate(f.ctx, f.workspace, f.user, parent.Identifier, true)
	require.NoError(t, err)

	require.Equal(t, []string{WebhookEventIssueCreated, WebhookEventIssueCreated, WebhookEventIssueCreated, WebhookEventIssueCreated}, sent.events())
	seen := map[uuid.UUID]bool{}
	for _, created := range sent.sent {
		require.Equal(t, f.workspace, created.workspace)
		require.Equal(t, f.user, created.actor)
		require.False(t, seen[created.issue.ID], "each issue is announced once")
		seen[created.issue.ID] = true
		require.Equal(t, 1, f.count(t, `SELECT COUNT(*) FROM issues WHERE id=$1`, created.issue.ID), "announced after it is stored")
	}
	require.Equal(t, parent.ID, sent.sent[0].issue.ID)
}

// A bulk update announces only the issues whose written fields changed, with
// their stored state; a bulk delete announces each deleted issue as it was.
func TestIssueWebhooksBulkWritesSendChangedAndDeletedIssuesPostgres(t *testing.T) {
	f := newBulkProjectFixture(t)
	sent := &webhookRecorder{}
	f.svc.SetWebhooks(sent)
	first, second, already := f.issue(t, f.team, f.status, nil), f.issue(t, f.team, f.status, nil), f.issue(t, f.team, f.status, nil)
	f.exec(t, `UPDATE issues SET priority=3 WHERE id=$1`, already)
	priority := 3

	n, err := f.svc.BulkUpdate(f.ctx, f.workspace, f.user, dto.BulkUpdateIssueRequest{
		IssueIDs: []string{first.String(), second.String(), already.String(), first.String()}, Priority: &priority})
	require.NoError(t, err)
	require.Equal(t, 3, n, "the count still reports every written issue")
	require.Equal(t, []string{WebhookEventIssueUpdated, WebhookEventIssueUpdated}, sent.events())
	updated := map[uuid.UUID]domain.Issue{}
	for _, event := range sent.sent {
		require.Equal(t, f.user, event.actor)
		updated[event.issue.ID] = event.issue
	}
	require.ElementsMatch(t, []uuid.UUID{first, second}, []uuid.UUID{sent.sent[0].issue.ID, sent.sent[1].issue.ID})
	require.Equal(t, domain.IssuePriority(3), updated[first].Priority)

	// Repeating the same change writes the rows but changes nothing.
	sent.sent = nil
	_, err = f.svc.BulkUpdate(f.ctx, f.workspace, f.user, dto.BulkUpdateIssueRequest{IssueIDs: []string{first.String()}, Priority: &priority})
	require.NoError(t, err)
	require.Empty(t, sent.sent)

	var identifier string
	require.NoError(t, f.db.Get(&identifier, `SELECT identifier_text FROM issues WHERE id=$1`, second))
	deleted, err := f.svc.BulkDelete(f.ctx, f.workspace, f.user, true, dto.BulkDeleteIssueRequest{IssueIDs: []string{first.String(), second.String()}})
	require.NoError(t, err)
	require.Equal(t, 2, deleted)
	require.Equal(t, []string{WebhookEventIssueDeleted, WebhookEventIssueDeleted}, sent.events())
	identifiers := []string{sent.sent[0].issue.Identifier, sent.sent[1].issue.Identifier}
	require.Contains(t, identifiers, identifier, "the issue as it was before deletion")
	require.Zero(t, f.count(t, `SELECT COUNT(*) FROM issues WHERE id=ANY(ARRAY[$1,$2]::uuid[])`, first, second))
}
