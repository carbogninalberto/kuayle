package repository

import (
	"testing"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// A later suite of the same commit replaces an earlier runner failure; a
// later real failure fails the change.
func TestWorkflowCIKeepsTheLatestResultPerCommit(t *testing.T) {
	runner := string(domain.CIFailureRunner)
	test := string(domain.CIFailureTest)
	failed, infrastructure, passed := classifyWorkflowCISignals([]domain.AgentTaskSignal{
		{Name: "GitHub Actions", Outcome: "failure", FailureClass: &runner, FailureEvidence: "Could not resolve 'archive.ubuntu.com'"},
		{Name: "GitHub Actions", Outcome: "success"},
	})
	require.Empty(t, failed)
	require.Empty(t, infrastructure, "the re-run's pass decides")
	require.Len(t, passed, 1)

	failed, infrastructure, passed = classifyWorkflowCISignals([]domain.AgentTaskSignal{
		{Name: "GitHub Actions", Outcome: "failure", FailureClass: &runner},
		{Name: "GitHub Actions", Outcome: "failure", FailureClass: &test, Summary: "TestX failed"},
	})
	require.Len(t, failed, 1)
	require.Empty(t, infrastructure)
	require.Empty(t, passed)

	failed, infrastructure, passed = classifyWorkflowCISignals([]domain.AgentTaskSignal{
		{Name: "GitHub Actions", Outcome: "failure", FailureClass: &runner},
	})
	require.Empty(t, failed)
	require.Len(t, infrastructure, 1)
	require.Empty(t, passed)
}
