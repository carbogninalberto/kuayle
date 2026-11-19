package domain

import (
	"fmt"
	"strings"
)

// StageInstructions describes only the transitions leaving this stage in the pinned version.
func (graph WorkflowGraph) StageInstructions(node WorkflowNode) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Oversee the %s stage %q (%s).\n", node.Type, node.Label, node.ID)
	fmt.Fprintf(&b, "Stage settings: check=%q, terminal outcome=%q, instructions=%s.\n", node.Check, node.Outcome, node.Prompt)
	b.WriteString("Work on this stage in the task worktree. Update the task notebook with your work and handoff. At CI, approval, merge and end stages keep the worktree unchanged when continuing the built-in stage.\n")
	b.WriteString("Outgoing transitions (condition, edge ID, destination):\n")
	var branches []WorkflowEdge
	for _, edge := range graph.Edges {
		if edge.Source != node.ID {
			continue
		}
		branches = append(branches, edge)
	}
	// Keep even the maximum graph within the instruction budget. Edge IDs
	// stay whole. Crowded stages show a shorter destination label.
	for _, edge := range branches {
		to, _ := graph.Node(edge.Target)
		label := truncateUTF8(to.Label, max(16, 7500/len(branches)))
		mode := "built-in"
		if graph.assistantChoice(edge) {
			mode = "selectable"
		}
		fmt.Fprintf(&b, "- [%s] %s %s -> %s\n", mode, edge.Condition, edge.ID, label)
	}
	if len(branches) == 0 {
		b.WriteString("(none; finish the stage)\n")
	}
	b.WriteString("End your final message with KUAYLE_STAGE_CONTINUE to run the built-in stage, or KUAYLE_STAGE_ROUTE <edge ID> to take a selectable transition. Kuayle evaluates all other conditions itself.")
	return b.String()
}

func (node WorkflowNode) StageReadsWorktree() bool {
	return node.Supervised() && (node.Type == WorkflowNodeApproval || node.Type == WorkflowNodeMerge || node.Type == WorkflowNodeEnd || node.Check == WorkflowCheckCI)
}

// A supervisor may send work back on an existing recovery branch. Other
// discretionary exits are explicitly configured by the workflow author.
func (graph WorkflowGraph) assistantChoice(edge WorkflowEdge) bool {
	if edge.Condition == WorkflowAssistant {
		return true
	}
	switch edge.Condition {
	case WorkflowChecksFailed, WorkflowPublishFailed, WorkflowApprovalDenied:
		target, ok := graph.Node(edge.Target)
		return ok && target.Type == WorkflowNodeAgent
	}
	return false
}
