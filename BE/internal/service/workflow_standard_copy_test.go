package service

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/kuayle/kuayle-backend/internal/domain"
)

// "Copy of Standard" adds a transition from Publish to Failed, so its failed
// ending sits one column further right than in the built-in graph: under the
// human review, where the editor's arrange puts it. Every transition then
// runs to the right; only the review's follow-up loops back.
func TestStandardCopyWorkflowRunsLeftToRight(t *testing.T) {
	g := standardCopy(uuid.New())
	nodes := map[string]domain.WorkflowNode{}
	for _, n := range g.Nodes {
		nodes[n.ID] = n
	}
	want := map[string]domain.WorkflowPosition{
		"start":     domain.WorkflowPositionAt(0, 0, false),
		"implement": domain.WorkflowPositionAt(1, 0, false),
		"validate":  domain.WorkflowPositionAt(2, 0, false),
		"publish":   domain.WorkflowPositionAt(3, 0, false),
		"review":    domain.WorkflowPositionAt(4, 0, false),
		"done":      domain.WorkflowPositionAt(5, 0, false),
		"failed":    domain.WorkflowPositionAt(4, 1, true),
	}
	require.Len(t, g.Nodes, len(want))
	for id, position := range want {
		require.Equal(t, position, nodes[id].Position, id)
	}
	require.Equal(t, domain.WorkflowPosition{X: 1488, Y: 120}, nodes["failed"].Position)
	var publishFailed bool
	for _, e := range g.Edges {
		from, to := nodes[e.Source], nodes[e.Target]
		if e.Source == "publish" && e.Target == "failed" {
			publishFailed = true
		}
		if e.Source == "review" && e.Target == "implement" {
			continue
		}
		width := 240.0
		if from.Type == domain.WorkflowNodeStart {
			width = 176
		}
		require.GreaterOrEqual(t, to.Position.X, from.Position.X+width+100, "%s runs to the right", e.ID)
	}
	require.True(t, publishFailed)
}
