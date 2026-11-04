package repository

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	headOld = "1111111111111111111111111111111111111111"
	headNew = "2222222222222222222222222222222222222222"
)

// A pull_request webhook delivered late must not replace a newer head, and
// pull requests Kuayle did not publish are not recorded.
func TestRecordPullRequestHeadIgnoresOlderObservations(t *testing.T) {
	db, repo, ctx, task, claimed := publishingTask(t)
	ws := task.WorkspaceID
	read := func() string {
		var sha string
		err := db.Get(&sha, `SELECT head_sha FROM pull_request_heads WHERE workspace_id=$1 AND repository_full_name='fixture/repo' AND pull_request_number=7`, ws)
		if err != nil {
			return ""
		}
		return sha
	}

	require.NoError(t, repo.RecordPullRequestHead(ctx, ws, "fixture/repo", 7, headNew, time.Now()))
	require.Empty(t, read(), "not a pull request Kuayle published")

	_, err := db.Exec(`INSERT INTO dev_machine_git_refs(workspace_id,machine_id,ref_type,repository_full_name,pull_request_number,source) VALUES($1,$2,'pull_request','fixture/repo',7,'published')`, ws, *claimed.MachineID)
	require.NoError(t, err)

	now := time.Now()
	require.NoError(t, repo.RecordPullRequestHead(ctx, ws, "fixture/repo", 7, headNew, now))
	require.Equal(t, headNew, read())
	require.NoError(t, repo.RecordPullRequestHead(ctx, ws, "fixture/repo", 7, headOld, now.Add(-time.Minute)))
	require.Equal(t, headNew, read(), "an older synchronize event does not overwrite a newer head")
	require.NoError(t, repo.RecordPullRequestHead(ctx, ws, "fixture/repo", 7, headOld, now.Add(time.Minute)))
	require.Equal(t, headOld, read(), "a newer observation, such as a force-push, replaces it")
}
