package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/stretchr/testify/require"
)

func (f *bulkProjectFixture) setParent(ctx context.Context, user uuid.UUID, ids []uuid.UUID, parent *uuid.UUID) (int, error) {
	raw := ""
	if parent != nil {
		raw = parent.String()
	}
	strs := make([]string, len(ids))
	for i, id := range ids {
		strs[i] = id.String()
	}
	return f.svc.BulkUpdate(ctx, f.workspace, user, dto.BulkUpdateIssueRequest{IssueIDs: strs, ParentID: &raw})
}

func (f *bulkProjectFixture) parentOf(t *testing.T, id uuid.UUID) *uuid.UUID {
	t.Helper()
	var parent *uuid.UUID
	require.NoError(t, f.db.Get(&parent, `SELECT parent_id FROM issues WHERE id=$1`, id))
	return parent
}

func (f *bulkProjectFixture) parentHistory(t *testing.T, ids ...uuid.UUID) int {
	t.Helper()
	n := 0
	for _, id := range ids {
		n += f.count(t, `SELECT COUNT(*) FROM issue_history WHERE issue_id=$1 AND field='parent'`, id)
	}
	return n
}

// A bulk parent change only writes the parent: an edit to another field that
// commits while the change waits for the row is kept, not reverted to a stale
// snapshot.
func TestIssueServiceBulkUpdateParentKeepsConcurrentEditsPostgres(t *testing.T) {
	f := newBulkProjectFixture(t)
	parent := f.issue(t, f.team, f.status, nil)
	first, second := f.issue(t, f.team, f.status, nil), f.issue(t, f.team, f.status, &f.projectA)

	// Another editor holds the second issue's row with uncommitted changes.
	editor, err := f.db.Beginx()
	require.NoError(t, err)
	defer editor.Rollback()
	var editorPID int
	require.NoError(t, editor.Get(&editorPID, `SELECT pg_backend_pid()`))
	_, err = editor.Exec(`UPDATE issues SET priority=4, title='Edited', project_id=$2 WHERE id=$1`, second, f.projectB)
	require.NoError(t, err)

	type result struct {
		updated int
		err     error
	}
	done := make(chan result, 1)
	go func() {
		updated, err := f.setParent(f.ctx, f.user, []uuid.UUID{first, second}, &parent)
		done <- result{updated, err}
	}()
	// Wait until the change is blocked on the editor's row lock, then commit the edit.
	require.Eventually(t, func() bool {
		return f.count(t, `SELECT COUNT(*) FROM pg_stat_activity WHERE $1 = ANY(pg_blocking_pids(pid))`, editorPID) > 0
	}, 10*time.Second, 10*time.Millisecond)
	require.NoError(t, editor.Commit())

	var res result
	select {
	case res = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("bulk parent change did not finish")
	}
	require.NoError(t, res.err)
	require.Equal(t, 2, res.updated)
	var row struct {
		Priority  int       `db:"priority"`
		Title     string    `db:"title"`
		ProjectID uuid.UUID `db:"project_id"`
		ParentID  uuid.UUID `db:"parent_id"`
	}
	require.NoError(t, f.db.Get(&row, `SELECT priority, title, project_id, parent_id FROM issues WHERE id=$1`, second))
	require.Equal(t, 4, row.Priority, "the concurrent priority change survives")
	require.Equal(t, "Edited", row.Title)
	require.Equal(t, f.projectB, row.ProjectID)
	require.Equal(t, parent, row.ParentID)
	require.Equal(t, 1, f.count(t, `SELECT COUNT(*) FROM issue_history WHERE issue_id=$1 AND field='parent' AND old_value='' AND new_value=$2 AND user_id=$3`,
		second, parent.String(), f.user))
}

// Repeated IDs change and record each issue once; issues already under the
// parent are not counted; an empty parent removes it.
func TestIssueServiceBulkUpdateParentCountsAndRemovesPostgres(t *testing.T) {
	f := newBulkProjectFixture(t)
	parent, other := f.issue(t, f.team, f.status, nil), f.issue(t, f.team, f.status, nil)
	first, second := f.issue(t, f.team, f.status, nil), f.issue(t, f.team, f.status, nil)
	f.exec(t, `UPDATE issues SET parent_id=$2 WHERE id=$1`, second, other)

	updated, err := f.setParent(f.ctx, f.user, []uuid.UUID{first, first, second, first}, &parent)
	require.NoError(t, err)
	require.Equal(t, 2, updated)
	require.Equal(t, 1, f.count(t, `SELECT COUNT(*) FROM issue_history WHERE issue_id=$1 AND field='parent' AND old_value=$2 AND new_value=$3`,
		second, other.String(), parent.String()))

	updated, err = f.setParent(f.ctx, f.user, []uuid.UUID{first, second}, &parent)
	require.NoError(t, err)
	require.Zero(t, updated, "already under the parent")
	require.Equal(t, 2, f.parentHistory(t, first, second))

	updated, err = f.setParent(f.ctx, f.user, []uuid.UUID{first, second}, nil)
	require.NoError(t, err)
	require.Equal(t, 2, updated)
	require.Nil(t, f.parentOf(t, first))
	require.Nil(t, f.parentOf(t, second))
}

// A parent below one of the selected issues would create a cycle: the whole
// batch is refused and nothing is written.
func TestIssueServiceBulkUpdateParentRefusesCyclePostgres(t *testing.T) {
	f := newBulkProjectFixture(t)
	root, free := f.issue(t, f.team, f.status, nil), f.issue(t, f.team, f.status, nil)
	child, grandchild := f.issue(t, f.team, f.status, nil), f.issue(t, f.team, f.status, nil)
	f.exec(t, `UPDATE issues SET parent_id=$2 WHERE id=$1`, child, root)
	f.exec(t, `UPDATE issues SET parent_id=$2 WHERE id=$1`, grandchild, child)

	_, err := f.setParent(f.ctx, f.user, []uuid.UUID{free, root}, &grandchild)
	require.EqualError(t, err, "parent would create a sub-issue cycle")
	require.Nil(t, f.parentOf(t, free), "nothing written")
	require.Nil(t, f.parentOf(t, root))
	require.Zero(t, f.parentHistory(t, free, root))

	_, err = f.setParent(f.ctx, f.user, []uuid.UUID{free, root}, &free)
	require.EqualError(t, err, "issue cannot be its own parent")
	require.Nil(t, f.parentOf(t, root))
}

// Two bulk changes that together would close a loop are serialized: one
// wins and the other sees its result and refuses.
func TestIssueServiceBulkUpdateParentConcurrentCyclePostgres(t *testing.T) {
	f := newBulkProjectFixture(t)
	a, b := f.issue(t, f.team, f.status, nil), f.issue(t, f.team, f.status, nil)

	// Hold both rows so the two changes start together and wait.
	editor, err := f.db.Beginx()
	require.NoError(t, err)
	defer editor.Rollback()
	var editorPID int
	require.NoError(t, editor.Get(&editorPID, `SELECT pg_backend_pid()`))
	_, err = editor.Exec(`UPDATE issues SET priority=2 WHERE id IN ($1,$2)`, a, b)
	require.NoError(t, err)

	errs := make(chan error, 2)
	go func() { _, err := f.setParent(f.ctx, f.user, []uuid.UUID{a}, &b); errs <- err }()
	go func() { _, err := f.setParent(f.ctx, f.user, []uuid.UUID{b}, &a); errs <- err }()
	// The second change may queue behind the first rather than the editor.
	require.Eventually(t, func() bool {
		return f.count(t, `SELECT COUNT(*) FROM pg_stat_activity WHERE pid <> $1 AND datname = current_database()
			AND cardinality(pg_blocking_pids(pid)) > 0`, editorPID) >= 2
	}, 10*time.Second, 10*time.Millisecond)
	require.NoError(t, editor.Commit())

	failed := 0
	for range 2 {
		select {
		case err := <-errs:
			if err != nil {
				require.EqualError(t, err, "parent would create a sub-issue cycle")
				failed++
			}
		case <-time.After(10 * time.Second):
			t.Fatal("bulk parent change did not finish")
		}
	}
	require.Equal(t, 1, failed, "exactly one change wins")
	require.False(t, f.parentOf(t, a) != nil && f.parentOf(t, b) != nil, "no loop")
	require.Equal(t, 1, f.parentHistory(t, a, b))
}

// A member outside a private team can neither select its issues nor use one
// as the parent; a public issue cannot go under a private-team parent. Each
// refusal writes nothing.
func TestIssueServiceBulkUpdateParentRespectsPrivateTeamsPostgres(t *testing.T) {
	f := newBulkProjectFixture(t)
	member := uuid.New()
	f.exec(t, `INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Member','hash')`, member, member.String()+"@example.test")
	t.Cleanup(func() { _, _ = f.db.Exec(`DELETE FROM users WHERE id=$1`, member) })
	f.exec(t, `INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'member')`, f.workspace, member)
	memberCtx := domain.WithAccess(context.Background(), domain.Access{UserID: member, WorkspaceID: f.workspace})

	parent, visible := f.issue(t, f.team, f.status, nil), f.issue(t, f.team, f.status, nil)
	hidden, privateParent := f.issue(t, f.private, f.privateStatus, nil), f.issue(t, f.private, f.privateStatus, nil)

	_, err := f.setParent(memberCtx, member, []uuid.UUID{visible, hidden}, &parent)
	require.EqualError(t, err, "issue not found")
	require.Nil(t, f.parentOf(t, visible), "nothing written")
	require.Nil(t, f.parentOf(t, hidden))

	_, err = f.setParent(memberCtx, member, []uuid.UUID{visible}, &privateParent)
	require.EqualError(t, err, "parent issue not found")
	require.Nil(t, f.parentOf(t, visible))

	// The owner sees both teams, but a public issue cannot sit under a
	// private-team parent; the private issue listed first is not written.
	_, err = f.setParent(f.ctx, f.user, []uuid.UUID{hidden, visible}, &privateParent)
	require.EqualError(t, err, "resource reference is unavailable or crosses a private-team boundary")
	require.Nil(t, f.parentOf(t, hidden), "nothing written")
	require.Nil(t, f.parentOf(t, visible))
	require.Zero(t, f.parentHistory(t, hidden, visible))

	updated, err := f.setParent(f.ctx, f.user, []uuid.UUID{hidden}, &privateParent)
	require.NoError(t, err)
	require.Equal(t, 1, updated)
}
