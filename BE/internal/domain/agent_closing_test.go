package domain

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A planning step asked to shorten a plan answers with its final message, so
// the closing sentence must ask for it and not for a single confirming line.
func TestAPlanIsAskedForAsTheFinalMessage(t *testing.T) {
	plan := ClosingPrompt([]ClosingAsk{PlanClosingAsk()})
	require.Contains(t, plan, "your plan is longer than the next agent can receive whole")
	require.Contains(t, plan, "Your final message must be the rewritten plan")
	require.Contains(t, plan, "12000")
	require.NotContains(t, plan, "Reply with one line when it is done")

	normal := ClosingPrompt([]ClosingAsk{NotebookClosingAsk("")})
	require.Contains(t, normal, "Reply with one line when it is done")
	require.NotContains(t, normal, "rewritten plan")
}
