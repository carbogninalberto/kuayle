package domain

import (
	"encoding/json"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Pipelines (KUA-252, experimental). A workflow version whose JSON says
// "kind": "pipeline" is an ordered list of phases worked by a roster of
// roles, instead of a graph of steps: each phase names what must be true to
// move on (checks over facts Kuayle reads), and the roles decide how. Absent
// or "graph" is the graph engine of workflow.go. ParseWorkflowGraph refuses a
// pipeline, so the graph engine can never run one; the tick
// (service/team_tick.go) runs pipeline tasks. This file holds the definition
// and its validation, and no I/O.

// Workflow kinds: the top-level "kind" of a version's JSON.
const (
	WorkflowKindGraph    = "graph"
	WorkflowKindPipeline = "pipeline"
)

// Who owns a phase besides a role of the roster, and who may approve or
// waive besides a person: the lead, when the pipeline has one.
const (
	PipelineOwnerKuayle = "kuayle"
	PipelineOwnerHuman  = "human"
	PipelineLeadRole    = "lead"
)

// Facts a check can require. The list is closed: Kuayle reads each one
// without a model (service/task_facts.go). Every fact with a defined "met"
// can be required: yes for pr_exists, head_contains_base, ci_green,
// approved and merged; no for pr_draft, conflict, migration_collision and
// closed; no task ahead in the merge order changing the same files for
// overlap; the role's approval of the head for verdict; exit 0 for script.
// Values (head_sha, base_ref, the board) are reported, never required.
// Removing a name would invalidate saved versions. script is the only fact
// that carries content the author wrote.
const (
	PipelineFactPRExists           = "pr_exists"
	PipelineFactPRDraft            = "pr_draft"
	PipelineFactHeadContainsBase   = "head_contains_base"
	PipelineFactConflict           = "conflict"
	PipelineFactMigrationCollision = "migration_collision"
	PipelineFactCIGreen            = "ci_green"
	PipelineFactVerdict            = "verdict"
	PipelineFactApproved           = "approved"
	PipelineFactMerged             = "merged"
	PipelineFactClosed             = "closed"
	PipelineFactOverlap            = "overlap"
	PipelineFactScript             = "script"
)

// PipelineConditionFacts are the facts a check requires without naming a
// role or carrying a command.
var PipelineConditionFacts = []string{
	PipelineFactPRExists, PipelineFactPRDraft, PipelineFactHeadContainsBase, PipelineFactConflict, PipelineFactMigrationCollision,
	PipelineFactCIGreen, PipelineFactApproved, PipelineFactMerged, PipelineFactClosed, PipelineFactOverlap,
}

// Pipeline bounds; the others are the graph's (WorkflowMaxNodes phases,
// WorkflowMaxLabelRunes, WorkflowMaxPromptBytes, WorkflowMaxGraphBytes).
const (
	PipelineMaxRoster = 10
	PipelineMaxChecks = 20
)

// Roles are addressed by name (@reviewer), so they are short, lowercase and
// never one of the names the pipeline itself uses.
var pipelineRolePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)

var pipelineReservedRoles = []string{PipelineOwnerKuayle, PipelineOwnerHuman, PipelineLeadRole, "all"}

// Pipeline is the JSON document a pipeline version stores.
type Pipeline struct {
	Kind     string           `json:"kind"`
	Phases   []PipelinePhase  `json:"phases"`
	Roster   []PipelineMember `json:"roster"`
	Lead     *PipelineLead    `json:"lead,omitempty"`
	Autonomy PipelineAutonomy `json:"autonomy,omitzero"`
	Budget   PipelineBudget   `json:"budget,omitzero"`
}

// PipelinePhase is one phase. Owner is a role of the roster, "kuayle" or
// "human". A guarded phase (the last one, run by Kuayle: the merge) acts
// only once every required check of the phases before it is met or waived.
type PipelinePhase struct {
	ID      string          `json:"id"`
	Label   string          `json:"label"`
	Owner   string          `json:"owner"`
	Prompt  string          `json:"prompt,omitempty"`
	Checks  []PipelineCheck `json:"checks,omitempty"`
	Guarded bool            `json:"guarded,omitempty"`
}

// PipelineCheck requires a fact. A verdict names the role that gives it; a
// script check names the command that must exit 0. Required checks block
// the guarded phase; advisory ones inform.
type PipelineCheck struct {
	ID       string `json:"id"`
	Fact     string `json:"fact"`
	Role     string `json:"role,omitempty"`
	Script   string `json:"script,omitempty"`
	Required bool   `json:"required,omitempty"`
}

// PipelineMember is a role of the roster and the profile that plays it.
// Exactly one role writes code; an on-call role works only when addressed.
type PipelineMember struct {
	Role      string    `json:"role"`
	ProfileID uuid.UUID `json:"profile_id"`
	Writer    bool      `json:"writer,omitempty"`
	OnCall    bool      `json:"on_call,omitempty"`
}

type PipelineLead struct {
	ProfileID uuid.UUID `json:"profile_id"`
}

// PipelineAutonomy says who approves and who may waive each check, by check
// ID: "human" (the default) or "lead".
type PipelineAutonomy struct {
	Approve string            `json:"approve,omitempty"`
	Waive   map[string]string `json:"waive,omitempty"`
}

// PipelineBudget bounds a task's whole run. Zero means no limit.
type PipelineBudget struct {
	MaxTurns    int    `json:"max_turns,omitempty"`
	MaxMinutes  int    `json:"max_minutes,omitempty"`
	OnExhausted string `json:"on_exhausted,omitempty"`
}

// Workflow is the budget as a graph workflow's, whose limits and policy
// the pipeline's tick applies the same way: its turns are the iterations.
func (b PipelineBudget) Workflow() WorkflowBudget {
	return WorkflowBudget{MaxIterations: b.MaxTurns, MaxMinutes: b.MaxMinutes, OnExhausted: b.OnExhausted}
}

// WorkflowKind reads the kind of a version's JSON: "graph" when it names
// none, or when it cannot be read (the graph parser then says why).
func WorkflowKind(raw []byte) string {
	var head struct {
		Kind string `json:"kind"`
	}
	if json.Unmarshal(raw, &head) != nil || head.Kind == "" {
		return WorkflowKindGraph
	}
	return head.Kind
}

// ParsePipeline decodes a pipeline strictly: unknown fields are refused.
func ParsePipeline(raw []byte) (*Pipeline, error) {
	if len(raw) > WorkflowMaxGraphBytes {
		return nil, &WorkflowInvalidError{[]WorkflowIssue{{Code: "too_large", Message: "the workflow is too large"}}}
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	var p Pipeline
	if err := decoder.Decode(&p); err != nil || p.Kind != WorkflowKindPipeline {
		return nil, &WorkflowInvalidError{[]WorkflowIssue{{Code: "malformed", Message: "the pipeline is not valid JSON of the expected shape"}}}
	}
	return &p, nil
}

// ValidatePipeline checks a pipeline against the profiles that exist (and
// are not archived) in its workspace, and returns every issue it finds. An
// issue about a phase carries the phase's ID as its node.
func ValidatePipeline(p Pipeline, profiles map[uuid.UUID]AgentProfile) []WorkflowIssue {
	issues := []WorkflowIssue{}
	add := func(code, phase, format string, args ...any) {
		issues = append(issues, WorkflowIssue{Code: code, NodeID: phase, Message: fmt.Sprintf(format, args...)})
	}
	if len(p.Phases) == 0 || len(p.Phases) > WorkflowMaxNodes || len(p.Roster) == 0 || len(p.Roster) > PipelineMaxRoster {
		add("pipeline_size", "", "a pipeline has 1 to %d phases and 1 to %d roles", WorkflowMaxNodes, PipelineMaxRoster)
		return issues
	}
	roles := map[string]PipelineMember{}
	writers := 0
	for _, member := range p.Roster {
		if !pipelineRolePattern.MatchString(member.Role) || slices.Contains(pipelineReservedRoles, member.Role) {
			add("invalid_role", "", "roles use 1 to 32 lowercase letters, digits, '-' or '_', and are not kuayle, human, lead or all")
			continue
		}
		if _, dup := roles[member.Role]; dup {
			add("duplicate_role", "", "two roles are called %q", member.Role)
			continue
		}
		roles[member.Role] = member
		profile, ok := profiles[member.ProfileID]
		if !ok {
			add("missing_profile", "", "%s: its agent profile does not exist or was archived", member.Role)
		}
		if member.Writer {
			writers++
			if member.OnCall {
				add("invalid_writer", "", "%s: the role that writes code is never only on call", member.Role)
			} else if ok && profile.Behaviour != AgentBehaviourChangesCode {
				add("invalid_writer", "", "%s: the role that writes code needs a profile that changes code", member.Role)
			}
		}
	}
	if writers != 1 {
		add("writer_count", "", "exactly one role writes code (this pipeline has %d)", writers)
	}
	if p.Lead != nil {
		if _, ok := profiles[p.Lead.ProfileID]; !ok {
			add("missing_profile", "", "the lead's agent profile does not exist or was archived")
		}
	}

	phases, checks := map[string]bool{}, map[string]bool{}
	for i, phase := range p.Phases {
		if !workflowIDPattern.MatchString(phase.ID) || phases[phase.ID] {
			add("invalid_id", "", "phase IDs are unique and use 1 to 64 letters, digits, '-' or '_'")
			continue
		}
		phases[phase.ID] = true
		label := strings.TrimSpace(phase.Label)
		if label == "" || utf8.RuneCountInString(label) > WorkflowMaxLabelRunes || strings.ContainsAny(label, "\r\n") {
			add("invalid_label", phase.ID, "every phase needs a one-line name of at most %d characters", WorkflowMaxLabelRunes)
		}
		if len(phase.Prompt) > WorkflowMaxPromptBytes || !utf8.ValidString(phase.Prompt) {
			add("prompt_too_long", phase.ID, "%s: the prompt is at most %d bytes", label, WorkflowMaxPromptBytes)
		}
		if _, role := roles[phase.Owner]; !role && phase.Owner != PipelineOwnerKuayle && phase.Owner != PipelineOwnerHuman {
			add("invalid_owner", phase.ID, "%s: the owner is a role of the roster, kuayle or human", label)
		}
		if phase.Guarded && (i != len(p.Phases)-1 || phase.Owner != PipelineOwnerKuayle) {
			add("invalid_guard", phase.ID, "%s: only the last phase, run by kuayle, is guarded", label)
		}
		if len(phase.Checks) > PipelineMaxChecks {
			add("pipeline_size", phase.ID, "%s: a phase has at most %d checks", label, PipelineMaxChecks)
			continue
		}
		for _, check := range phase.Checks {
			if !workflowIDPattern.MatchString(check.ID) || checks[check.ID] {
				add("invalid_id", phase.ID, "%s: check IDs are unique and use 1 to 64 letters, digits, '-' or '_'", label)
				continue
			}
			checks[check.ID] = true
			if problem := pipelineCheckProblem(check, roles); problem != "" {
				add("invalid_check", phase.ID, "%s: %s", label, problem)
			}
		}
	}

	// The owner merges (A6): a guarded merge needs a person's (or a granted
	// lead's) approval in a phase before it, as a required check.
	if last := len(p.Phases) - 1; p.Phases[last].Guarded && !slices.ContainsFunc(p.Phases[:last], func(phase PipelinePhase) bool {
		return slices.ContainsFunc(phase.Checks, func(c PipelineCheck) bool { return c.Fact == PipelineFactApproved && c.Required })
	}) {
		add("missing_approval", p.Phases[last].ID, "%s: a guarded merge needs a required approved check in a phase before it", p.Phases[last].Label)
	}

	decides := func(who string) bool {
		return who == "" || who == PipelineOwnerHuman || (who == PipelineLeadRole && p.Lead != nil)
	}
	if !decides(p.Autonomy.Approve) {
		add("invalid_autonomy", "", "approval comes from a person, or from the lead when the pipeline has one")
	}
	for _, id := range slices.Sorted(maps.Keys(p.Autonomy.Waive)) {
		if !checks[id] || !decides(p.Autonomy.Waive[id]) {
			add("invalid_autonomy", "", "%q: a waiver names a check of the pipeline and comes from a person, or from the lead when there is one", id)
		}
	}
	switch p.Budget.OnExhausted {
	case "", WorkflowOnLimitFail, WorkflowOnLimitAsk:
	default:
		add("invalid_budget", "", "when a limit is reached the task either stops as failed or asks a person")
	}
	if p.Budget.MaxTurns < 0 || p.Budget.MaxTurns > WorkflowMaxIterations || p.Budget.MaxMinutes < 0 || p.Budget.MaxMinutes > WorkflowMaxMinutes {
		add("invalid_budget", "", "the budget allows at most %d turns and %d minutes", WorkflowMaxIterations, WorkflowMaxMinutes)
	}
	return issues
}

// pipelineCheckProblem says what is wrong with a check, or "".
func pipelineCheckProblem(check PipelineCheck, roles map[string]PipelineMember) string {
	switch check.Fact {
	case PipelineFactVerdict:
		if member, ok := roles[check.Role]; !ok || member.Writer {
			return "a verdict comes from a role of the roster that does not write the code"
		}
	case PipelineFactScript:
		if strings.TrimSpace(check.Script) == "" || len(check.Script) > WorkflowMaxPromptBytes || !utf8.ValidString(check.Script) || strings.ContainsRune(check.Script, 0) {
			return fmt.Sprintf("a script check runs a command of at most %d bytes", WorkflowMaxPromptBytes)
		}
	default:
		if !slices.Contains(PipelineConditionFacts, check.Fact) {
			return fmt.Sprintf("a check cannot require %q", check.Fact)
		}
	}
	if (check.Role != "" && check.Fact != PipelineFactVerdict) || (check.Script != "" && check.Fact != PipelineFactScript) {
		return "only a verdict names a role, and only a script check carries a command"
	}
	return ""
}

// TeamPipeline is the "team" template: the implementer writes, Kuayle opens
// the pull request, the reviewer gives a verdict, the branch is up to date
// with green CI and takes no migration number its base took, a person
// approves and Kuayle merges. No lead: Kuayle moves
// to the first phase with an unmet required check.
func TeamPipeline(implementer, reviewer uuid.UUID) Pipeline {
	required := func(id, fact string) PipelineCheck { return PipelineCheck{ID: id, Fact: fact, Required: true} }
	return Pipeline{
		Kind: WorkflowKindPipeline,
		Phases: []PipelinePhase{
			{ID: "implement", Label: "Implement", Owner: "implementer"},
			{ID: "pr", Label: "Pull request", Owner: PipelineOwnerKuayle, Checks: []PipelineCheck{required("pr-exists", PipelineFactPRExists)}},
			{ID: "review", Label: "Review", Owner: "implementer", Checks: []PipelineCheck{{ID: "reviewer", Fact: PipelineFactVerdict, Role: "reviewer", Required: true}}},
			{ID: "integrate", Label: "CI checks", Owner: "implementer", Checks: []PipelineCheck{required("up-to-date", PipelineFactHeadContainsBase), required("ci", PipelineFactCIGreen),
				{ID: "migrations", Fact: PipelineFactMigrationCollision, Required: true}}},
			{ID: "approval", Label: "Approval", Owner: PipelineOwnerHuman, Checks: []PipelineCheck{required("owner", PipelineFactApproved)}},
			{ID: "merge", Label: "Merge", Owner: PipelineOwnerKuayle, Guarded: true},
		},
		Roster:   []PipelineMember{{Role: "implementer", ProfileID: implementer, Writer: true}, {Role: "reviewer", ProfileID: reviewer}},
		Autonomy: PipelineAutonomy{Approve: PipelineOwnerHuman},
		Budget:   PipelineBudget{MaxTurns: 40, MaxMinutes: 2880, OnExhausted: WorkflowOnLimitAsk},
	}
}
