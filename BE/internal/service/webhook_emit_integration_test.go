package service

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/realtime"
	"github.com/kuayle/kuayle-backend/internal/repository"
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
	require.Contains(t, identifiers, identifier, "the row as it was when deleted")
	require.Zero(t, f.count(t, `SELECT COUNT(*) FROM issues WHERE id=ANY(ARRAY[$1,$2]::uuid[])`, first, second))
}

// eventFrom returns the recorded events of one kind for one issue.
func eventFrom(sent *webhookRecorder, event string, issueID uuid.UUID) []emittedWebhook {
	var found []emittedWebhook
	for _, got := range sent.sent {
		if got.event == event && got.issue.ID == issueID {
			found = append(found, got)
		}
	}
	return found
}

func microsecond(t time.Time) time.Time {
	return t.UTC().Truncate(time.Microsecond)
}

// The lifecycle trigger writes started_at, completed_at, cancelled_at and
// triaged_at, so the announced issue carries them instead of the caller's stale
// copy: a move to Done is announced completed, and a triage is announced
// triaged.
func TestIssueWebhooksCarryLifecycleTimestampsPostgres(t *testing.T) {
	f := newBulkProjectFixture(t)
	sent := &webhookRecorder{}
	f.svc.SetWebhooks(sent)

	done := uuid.New()
	f.exec(t, `INSERT INTO team_statuses(id,team_id,name,slug,category) VALUES($1,$2,'Done','done','completed')`, done, f.team)
	doneID := done.String()

	var identifier string
	stored := struct {
		StartedAt   *time.Time `db:"started_at"`
		CompletedAt *time.Time `db:"completed_at"`
	}{}
	moving := f.issue(t, f.team, f.status, nil)
	require.NoError(t, f.db.Get(&identifier, `SELECT identifier_text FROM issues WHERE id=$1`, moving))

	_, err := f.svc.Update(f.ctx, f.workspace, f.user, identifier, dto.UpdateIssueRequest{StatusID: &doneID})
	require.NoError(t, err)
	require.Len(t, sent.sent, 1)
	updated := sent.sent[0]
	require.Equal(t, WebhookEventIssueUpdated, updated.event)
	require.NotNil(t, updated.issue.CompletedAt)
	require.NotNil(t, updated.issue.StartedAt)
	require.NoError(t, f.db.Get(&stored, `SELECT started_at, completed_at FROM issues WHERE id=$1`, moving))
	require.NotNil(t, stored.CompletedAt)
	require.NotNil(t, stored.StartedAt)
	require.Equal(t, microsecond(*stored.CompletedAt), microsecond(*updated.issue.CompletedAt))
	require.Equal(t, microsecond(*stored.StartedAt), microsecond(*updated.issue.StartedAt))

	// An issue created directly in Done is announced completed.
	created, err := f.svc.Create(f.ctx, f.workspace, f.user, dto.CreateIssueRequest{Title: "Already done", TeamID: f.team.String(), StatusID: &doneID})
	require.NoError(t, err)
	createdEvents := eventFrom(sent, WebhookEventIssueCreated, created.ID)
	require.Len(t, createdEvents, 1)
	require.NotNil(t, createdEvents[0].issue.CompletedAt)
	// The fixture inserts issues by hand; keep its numbering after the created one.
	f.issueNumber = created.Number

	// Triaging an issue is announced with its triage timestamp.
	accepted := f.issue(t, f.team, f.status, nil)
	f.exec(t, `UPDATE issues SET triaged=FALSE, triaged_at=NULL WHERE id=$1`, accepted)
	require.NoError(t, f.db.Get(&identifier, `SELECT identifier_text FROM issues WHERE id=$1`, accepted))
	_, err = f.svc.Triage(f.ctx, f.workspace, f.user, identifier, true)
	require.NoError(t, err)
	triagedEvents := eventFrom(sent, WebhookEventIssueTriaged, accepted)
	require.Len(t, triagedEvents, 1)
	require.NotNil(t, triagedEvents[0].issue.TriagedAt)

	// Declining an untriaged issue is one issue.triaged, cancelled.
	cancelled := uuid.New()
	f.exec(t, `INSERT INTO team_statuses(id,team_id,name,slug,category) VALUES($1,$2,'Cancelled','cancelled','cancelled')`, cancelled, f.team)
	declined := f.issue(t, f.team, f.status, nil)
	f.exec(t, `UPDATE issues SET triaged=FALSE, triaged_at=NULL WHERE id=$1`, declined)
	require.NoError(t, f.db.Get(&identifier, `SELECT identifier_text FROM issues WHERE id=$1`, declined))
	_, err = f.svc.Triage(f.ctx, f.workspace, f.user, identifier, false)
	require.NoError(t, err)
	declinedEvents := eventFrom(sent, WebhookEventIssueTriaged, declined)
	require.Len(t, declinedEvents, 1)
	require.Equal(t, domain.IssueStatusCancelled, declinedEvents[0].issue.Status)
	require.NotNil(t, declinedEvents[0].issue.CancelledAt)
	require.NotNil(t, declinedEvents[0].issue.TriagedAt)
	require.Empty(t, eventFrom(sent, WebhookEventIssueUpdated, declined), "the decline's move to cancelled rides on issue.triaged")
}

// SetAssignees writes assignee_id and updated_at in its own transaction after
// the content write, so the announced issue carries the stored updated_at.
func TestIssueWebhooksUpdateWithAssigneesCarriesStoredUpdatedAtPostgres(t *testing.T) {
	f := newBulkProjectFixture(t)
	sent := &webhookRecorder{}
	f.svc.SetWebhooks(sent)
	issue := f.issue(t, f.team, f.status, nil)
	var identifier string
	require.NoError(t, f.db.Get(&identifier, `SELECT identifier_text FROM issues WHERE id=$1`, issue))

	title := "Renamed"
	updated, err := f.svc.Update(f.ctx, f.workspace, f.user, identifier, dto.UpdateIssueRequest{
		Title: &title, AssigneeIDs: []string{f.user.String()}})
	require.NoError(t, err)

	require.Len(t, sent.sent, 1)
	got := sent.sent[0]
	require.Equal(t, WebhookEventIssueUpdated, got.event)
	require.Equal(t, issue, got.issue.ID)
	require.NotNil(t, got.issue.AssigneeID)
	require.Equal(t, f.user, *got.issue.AssigneeID, "the member assignee is stored")
	var stored time.Time
	require.NoError(t, f.db.Get(&stored, `SELECT updated_at FROM issues WHERE id=$1`, issue))
	require.Equal(t, microsecond(stored), microsecond(got.issue.UpdatedAt), "the announced issue carries the stored updated_at")
	require.Equal(t, microsecond(stored), microsecond(updated.UpdatedAt), "the returned issue carries the stored updated_at")
}

// Completing a cycle with carry-over announces the moved issues once, with the
// next cycle, after the move is committed.
func TestCycleWebhooksCarryOverSendsMovedIssuesPostgres(t *testing.T) {
	f := newBulkProjectFixture(t)
	active, upcoming := uuid.New(), uuid.New()
	f.exec(t, `INSERT INTO cycles(id,team_id,name,number,status) VALUES($1,$2,'Active',1,'active')`, active, f.team)
	f.exec(t, `INSERT INTO cycles(id,team_id,name,number,status) VALUES($1,$2,'Upcoming',2,'upcoming')`, upcoming, f.team)
	unfinished, finished := f.issue(t, f.team, f.status, nil), f.issue(t, f.team, f.status, nil)
	f.exec(t, `UPDATE issues SET cycle_id=$1 WHERE id=$2`, active, unfinished)
	f.exec(t, `UPDATE issues SET cycle_id=$1 WHERE id=$2`, active, finished)
	f.exec(t, `UPDATE issues SET status='done' WHERE id=$1`, finished)

	recorder := &webhookRecorder{}
	svc := NewCycleService(repository.NewCycleRepository(f.db), repository.NewTeamRepository(f.db), realtime.NewHub(), nil)
	svc.SetWebhooks(recorder)

	cycle, carried, err := svc.Complete(f.ctx, active, dto.CompleteCycleRequest{CarryOver: true})
	require.NoError(t, err)
	require.Equal(t, domain.CycleStatusCompleted, cycle.Status)
	require.Equal(t, 1, carried)
	require.Equal(t, 1, recorder.batches, "one listing for the whole carry-over")
	require.Len(t, recorder.sent, 1)
	got := recorder.sent[0]
	require.Equal(t, WebhookEventIssueUpdated, got.event)
	require.Equal(t, unfinished, got.issue.ID)
	require.NotNil(t, got.issue.CycleID)
	require.Equal(t, upcoming, *got.issue.CycleID)
	require.Equal(t, f.user, got.actor)
	require.Equal(t, f.workspace, got.workspace)
}
