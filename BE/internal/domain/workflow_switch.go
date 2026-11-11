package domain

import "github.com/google/uuid"

// Why a task cannot be moved to the newest version of its workflow.
const (
	// The task follows no workflow, or is a review task.
	WorkflowSwitchNoWorkflow = "no_workflow"
	// The task already runs the newest version.
	WorkflowSwitchNoNewer = "no_newer_version"
	// The workflow has ended, or the task has.
	WorkflowSwitchFinished = "finished"
	// Something is acting on the workflow: pause it first.
	WorkflowSwitchNotAtRest = "not_at_rest"
	// The step the task is on does not exist in the newest version.
	WorkflowSwitchStepRemoved = "step_removed"
	// The step exists but does another kind of work there.
	WorkflowSwitchStepChanged = "step_changed"
)

// WorkflowSwitchAction is the audit action of a task moved to another
// version of its workflow.
const WorkflowSwitchAction = "version_switched"

// WorkflowSwitch is what moving a task to the newest version of its workflow
// would do, or why it cannot be moved. Nothing is automatic: a person asks.
type WorkflowSwitch struct {
	FromVersion int `json:"from_version"`
	ToVersion   int `json:"to_version"`
	// Node and NodeLabel are the step the task continues on, as the newest
	// version names it.
	Node      string `json:"node,omitempty"`
	NodeLabel string `json:"node_label,omitempty"`
	// Refused is empty when the task can be moved.
	Refused string `json:"refused,omitempty"`
}

// WorkflowSwitchBlocked says why the task's state does not allow a move, or
// "" when it does. A task is moved only while nothing acts on its workflow:
// paused by a person (and the interrupted attempt stopped), or waiting on a
// step for CI, an approval or a merge.
func WorkflowSwitchBlocked(task AgentTask) string {
	if !task.RunsWorkflow() {
		return WorkflowSwitchNoWorkflow
	}
	switch task.State {
	case "completed", "cancelled", "failed":
		return WorkflowSwitchFinished
	}
	if task.WorkflowStatus != nil && (*task.WorkflowStatus == WorkflowStatusDone || *task.WorkflowStatus == WorkflowStatusFailed) {
		return WorkflowSwitchFinished
	}
	if task.WorkflowPausedAt != nil {
		if task.State == "cancelling" {
			return WorkflowSwitchNotAtRest
		}
		return ""
	}
	if task.WorkflowStatus != nil && *task.WorkflowStatus == WorkflowStatusWaiting {
		return ""
	}
	return WorkflowSwitchNotAtRest
}

// MatchWorkflowNode finds the step a task is on in another version of its
// workflow. It is the same step only when it kept its ID and does the same
// kind of work: the same node type, the same check, and for an agent step a
// profile with the same behaviour. Label, prompt, limits, the profile itself
// and the transitions may all differ: that is what moving is for.
func MatchWorkflowNode(id string, from, to WorkflowGraph, profiles map[uuid.UUID]AgentProfile) (WorkflowNode, string) {
	current, ok := from.Node(id)
	if !ok {
		return WorkflowNode{}, WorkflowSwitchStepRemoved
	}
	next, ok := to.Node(id)
	if !ok {
		return WorkflowNode{}, WorkflowSwitchStepRemoved
	}
	if next.Type != current.Type || next.Check != current.Check {
		return WorkflowNode{}, WorkflowSwitchStepChanged
	}
	if current.Type == WorkflowNodeAgent {
		if current.ProfileID == nil || next.ProfileID == nil {
			return WorkflowNode{}, WorkflowSwitchStepChanged
		}
		was, known := profiles[*current.ProfileID]
		now, exists := profiles[*next.ProfileID]
		if !known || !exists || was.Behaviour != now.Behaviour {
			return WorkflowNode{}, WorkflowSwitchStepChanged
		}
	}
	return next, ""
}

// CarriedWorkflowVisits keeps the visit counts of the steps the other
// version still has, so its limits count what the task already did.
func CarriedWorkflowVisits(visits map[string]int, to WorkflowGraph) map[string]int {
	carried := map[string]int{}
	for id, count := range visits {
		if _, ok := to.Node(id); ok {
			carried[id] = count
		}
	}
	return carried
}
