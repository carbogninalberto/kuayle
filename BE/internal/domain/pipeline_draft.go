package domain

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// pipelineDraftWriter is the role the profile that changes code plays in a
// drafted pipeline.
const pipelineDraftWriter = "implementer"

// DraftPipeline drafts a pipeline from a graph workflow (KUA-283), for a
// person to review and save as a workflow of its own. Nothing calls it on
// its own, and the graph keeps running as it is. It drafts one only when the
// graph is a single path once the ways back are set aside:
//
//   - A transition back to a step every path to its source passes through
//     (changes requested, a CI failure, a denied approval, a retry) is
//     implied in a pipeline, where an unmet check sends the work back.
//     Transitions to an end that finishes as failed are dropped too: a
//     pipeline fails only on its budget and otherwise asks a person. So is
//     a publish step's "no changes" transition to an end, which finishes the
//     graph as it would without it.
//   - Every other step has exactly one way on. Two are a branch, which a
//     pipeline cannot draw.
//
// Each step on the path becomes a phase with the step's ID and name:
//
//   - an agent step that changes code: a phase the writer owns, with the
//     step's prompt less the graph's variables (the room carries review
//     findings and failing checks). Its profile plays the writer,
//     "implementer";
//   - an agent step that reviews: a phase the writer owns that requires a
//     verdict from the role its profile plays ("reviewer", then
//     "reviewer-2" for another profile);
//   - publishing: a phase Kuayle owns that requires pr_exists;
//   - a CI check: a phase the writer owns that requires the branch up to
//     date with its base (head_contains_base, which Kuayle brings about by
//     updating the branch, so it never strands a task) and ci_green, and
//     reports a migration number the base took (migration_collision) as an
//     advisory check, which informs and never blocks;
//   - an approval: a phase a person owns that requires approved;
//   - a merge step, or the end after a ship approval that merges on
//     approval: the last phase, guarded, the merge Kuayle carries out;
//   - a retired local check does nothing today, so it has no phase.
//
// Visit limits, timeouts and the prompts of review and approval steps are
// not carried over. The budget keeps its minutes and its policy; its
// iterations count a turn of every role (iterations × roles, at most
// WorkflowMaxIterations), as a pipeline charges each role's turn. The draft
// has no lead, so approving and waiving stay a person's.
//
// Otherwise it returns why not, on the steps concerned, as the editor shows
// a graph's problems: the graph's own (ValidateStoredWorkflowGraph), a
// branch, a step no phase stands for (a planning step, a stage assistant), a
// second profile that changes code, an approval before anything is
// published or code changed after it, and what ValidatePipeline finds in the
// draft. It performs no I/O and never changes g.
func DraftPipeline(g WorkflowGraph, profiles map[uuid.UUID]AgentProfile) (*Pipeline, []WorkflowIssue) {
	if issues := ValidateStoredWorkflowGraph(g, profiles); len(issues) > 0 {
		return nil, issues
	}
	d := pipelineDraft{g: g, profiles: profiles, nodes: map[string]WorkflowNode{}, outgoing: map[string][]WorkflowEdge{},
		phaseIDs: map[string]bool{}, checkIDs: map[string]bool{}, roles: map[uuid.UUID]string{}}
	for _, node := range g.Nodes {
		d.nodes[node.ID] = node
		d.phaseIDs[node.ID] = true
	}
	for _, edge := range g.Edges {
		d.outgoing[edge.Source] = append(d.outgoing[edge.Source], edge)
	}
	d.p = Pipeline{Kind: WorkflowKindPipeline}
	start, _ := g.Start()
	seen := map[string]bool{}
	for node := start; ; {
		if seen[node.ID] {
			d.reason("loop", node.ID, "", "%s: the path comes back to this step without going back to an earlier one", node.Label)
			break
		}
		seen[node.ID] = true
		if node.Type == WorkflowNodeEnd {
			if d.mergeOnApproval {
				d.p.Phases = append(d.p.Phases, PipelinePhase{ID: freeID(d.phaseIDs, "merge"), Label: "Merge", Owner: PipelineOwnerKuayle, Guarded: true})
			}
			break
		}
		d.step(node)
		next, ok := d.wayOn(node)
		if !ok {
			break
		}
		node = next
	}
	if len(d.reasons) > 0 {
		return nil, d.reasons
	}
	if iterations := g.Budget.MaxIterations; iterations > 0 {
		d.p.Budget.MaxTurns = min(iterations*len(d.p.Roster), WorkflowMaxIterations)
	}
	d.p.Budget.MaxMinutes, d.p.Budget.OnExhausted = g.Budget.MaxMinutes, g.Budget.OnExhausted
	if issues := ValidatePipeline(d.p, profiles); len(issues) > 0 {
		return nil, issues
	}
	return &d.p, nil
}

// pipelineDraft is DraftPipeline's state along the path.
type pipelineDraft struct {
	g        WorkflowGraph
	profiles map[uuid.UUID]AgentProfile
	nodes    map[string]WorkflowNode
	outgoing map[string][]WorkflowEdge
	p        Pipeline
	reasons  []WorkflowIssue
	// phaseIDs and checkIDs are taken; roles names each profile's role.
	phaseIDs, checkIDs map[string]bool
	roles              map[uuid.UUID]string
	writer             *WorkflowNode
	// published: a publish step came before; approval is the last
	// approval's name; mergeOnApproval: it merges on approval.
	published       bool
	approval        string
	mergeOnApproval bool
}

func (d *pipelineDraft) reason(code, node, edge, format string, args ...any) {
	d.reasons = append(d.reasons, WorkflowIssue{Code: code, NodeID: node, EdgeID: edge, Message: fmt.Sprintf(format, args...)})
}

// step adds the phase that stands for node, or says why none does.
func (d *pipelineDraft) step(node WorkflowNode) {
	if node.Supervised() {
		d.reason("stage_assistant", node.ID, "", "%s: a stage assistant chooses the next step, and a pipeline has no choice to make; remove the assistant first", node.Label)
	}
	phase := PipelinePhase{ID: node.ID, Label: node.Label, Owner: pipelineDraftWriter}
	check := func(base, fact, role string, required bool) {
		phase.Checks = append(phase.Checks, PipelineCheck{ID: freeID(d.checkIDs, base), Fact: fact, Role: role, Required: required})
	}
	switch node.Type {
	case WorkflowNodeStart:
		return
	case WorkflowNodeAgent:
		profile := d.profiles[*node.ProfileID]
		switch {
		case profile.Plans():
			d.reason("planning_step", node.ID, "", "%s: a planning step has no phase: a phase moves on when its checks hold, and no check says a plan is done", node.Label)
			return
		case profile.Reviews():
			role := d.role(profile.ID, "reviewer", false)
			check(role, PipelineFactVerdict, role, true)
		default:
			switch {
			case d.writer != nil && *d.writer.ProfileID != profile.ID:
				d.reason("second_writer", node.ID, "", "%s: one role writes the code of a pipeline, and this step runs another profile than %s", node.Label, d.writer.Label)
				return
			case d.approval != "":
				d.reason("code_after_approval", node.ID, "", "%s: this step changes the code after %s approved it; a pipeline merges only a head a person approved, so approvals come after every step that changes code", node.Label, d.approval)
				return
			}
			d.writer = &node
			d.role(profile.ID, pipelineDraftWriter, true)
			phase.Prompt = RenderWorkflowPrompt(node.Prompt, WorkflowVariables{})
		}
	case WorkflowNodeCheck:
		if node.Check != WorkflowCheckCI {
			return
		}
		// The team template's checks and IDs; the migrations check only informs.
		check("up-to-date", PipelineFactHeadContainsBase, "", true)
		check("ci", PipelineFactCIGreen, "", true)
		check("migrations", PipelineFactMigrationCollision, "", false)
	case WorkflowNodePublish:
		d.published = true
		phase.Owner = PipelineOwnerKuayle
		check("pr-exists", PipelineFactPRExists, "", true)
	case WorkflowNodeApproval:
		if !d.published {
			d.reason("early_approval", node.ID, "", "%s: nothing is published yet, and a pipeline's approval is of the published head", node.Label)
			return
		}
		d.approval, d.mergeOnApproval = node.Label, d.g.ShipApproval(node, d.profiles) == ShipMergesOnApproval
		phase.Owner = PipelineOwnerHuman
		check("owner", PipelineFactApproved, "", true)
	case WorkflowNodeMerge:
		phase.Owner, phase.Guarded = PipelineOwnerKuayle, true
	}
	d.p.Phases = append(d.p.Phases, phase)
}

// role is the role profile plays, added to the roster the first time.
func (d *pipelineDraft) role(profile uuid.UUID, base string, writer bool) string {
	if role, ok := d.roles[profile]; ok {
		return role
	}
	taken := map[string]bool{}
	for _, member := range d.p.Roster {
		taken[member.Role] = true
	}
	role := freeID(taken, base)
	d.roles[profile] = role
	d.p.Roster = append(d.p.Roster, PipelineMember{Role: role, ProfileID: profile, Writer: writer})
	return role
}

// wayOn is the step after node: the one its transitions lead to once those
// that go back, fail, or finish with no changes are set aside. It says why
// when there is none, or more than one.
func (d *pipelineDraft) wayOn(node WorkflowNode) (WorkflowNode, bool) {
	var on []WorkflowEdge
	targets := map[string]bool{}
	for _, edge := range d.outgoing[node.ID] {
		target := d.nodes[edge.Target]
		switch {
		case d.dominates(edge.Target, node.ID):
		case target.Type == WorkflowNodeEnd && target.Outcome == WorkflowEndFailed:
		case target.Type == WorkflowNodeEnd && edge.Condition == WorkflowNoChanges:
		default:
			on = append(on, edge)
			targets[edge.Target] = true
		}
	}
	switch {
	case len(targets) == 0:
		d.reason("dead_end", node.ID, "", "%s: besides going back, no transition leads on to a step or to an end that finishes as done", node.Label)
		return WorkflowNode{}, false
	case len(targets) > 1:
		ways := make([]string, 0, len(on))
		for _, edge := range on {
			when := "otherwise"
			if edge.Condition != WorkflowElse {
				when = WorkflowOutcomeText(edge.Condition)
			}
			ways = append(ways, fmt.Sprintf("%s (%s)", d.nodes[edge.Target].Label, when))
		}
		d.reason("branching", node.ID, "", "%s: it goes on to %s; a pipeline is one path, so a step has one way on besides going back", node.Label, strings.Join(ways, " or "))
		return WorkflowNode{}, false
	}
	return d.nodes[on[0].Target], true
}

// dominates reports whether every path from the start to node passes
// through by: a transition from node to by goes back.
func (d *pipelineDraft) dominates(by, node string) bool {
	start, _ := d.g.Start()
	if by == node || by == start.ID {
		return true
	}
	seen := map[string]bool{start.ID: true, by: true}
	queue := []string{start.ID}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if current == node {
			return false
		}
		for _, edge := range d.outgoing[current] {
			if !seen[edge.Target] {
				seen[edge.Target] = true
				queue = append(queue, edge.Target)
			}
		}
	}
	return true
}

// freeID is base, or base-2, base-3… when taken has it; it is taken after.
func freeID(taken map[string]bool, base string) string {
	id := base
	for i := 2; taken[id]; i++ {
		id = fmt.Sprintf("%s-%d", base, i)
	}
	taken[id] = true
	return id
}
