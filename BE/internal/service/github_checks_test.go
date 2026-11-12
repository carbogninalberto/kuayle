package service

import (
	"testing"
	"time"

	gh "github.com/kuayle/kuayle-backend/pkg/github"
	"github.com/stretchr/testify/require"
)

// A re-run has the same check name; only its newest attempt is the result, so
// an earlier failed attempt never fails a commit whose re-run passed.
func TestAReRunReplacesTheEarlierCheckRunOfTheSameName(t *testing.T) {
	start := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	failedAt, passedAt := start.Add(time.Minute), start.Add(2*time.Minute)
	runs := []gh.CheckRun{
		{ID: 1, Name: "Tests", Conclusion: "failure", StartedAt: &start, CompletedAt: &failedAt},
		{ID: 2, Name: "Tests", Conclusion: "success", StartedAt: &start, CompletedAt: &passedAt},
		{ID: 3, Name: "Lint", Conclusion: "success", StartedAt: &start, CompletedAt: &failedAt},
	}
	latest := latestCheckRuns(runs)
	require.Len(t, latest, 2)
	require.Equal(t, "Tests", latest[0].Name)
	require.Equal(t, "success", latest[0].Conclusion)
	require.EqualValues(t, 2, latest[0].ID)
	require.Equal(t, "Lint", latest[1].Name)
}
