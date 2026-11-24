package domain

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestTeamPipelineIsValidAndRoundTrips(t *testing.T) {
	implementer, reviewer, profiles := exampleProfiles()
	p := TeamPipeline(implementer, reviewer)
	require.Empty(t, ValidatePipeline(p, profiles))

	ids := []string{}
	for _, phase := range p.Phases {
		ids = append(ids, phase.ID)
	}
	require.Equal(t, []string{"implement", "pr", "review", "integrate", "approval", "merge"}, ids)
	var integrate []string
	for _, check := range p.Phases[3].Checks {
		integrate = append(integrate, check.Fact)
	}
	require.Equal(t, []string{"head_contains_base", "ci_green", "migration_collision"}, integrate, "a migration number taken on the base goes back to the writer")
	require.Equal(t, []PipelineMember{{Role: "implementer", ProfileID: implementer, Writer: true}, {Role: "reviewer", ProfileID: reviewer}}, p.Roster)
	require.Nil(t, p.Lead)
	require.True(t, p.Phases[5].Guarded)

	raw, err := json.Marshal(p)
	require.NoError(t, err)
	require.Equal(t, WorkflowKindPipeline, WorkflowKind(raw))
	parsed, err := ParsePipeline(raw)
	require.NoError(t, err)
	require.Equal(t, p, *parsed)
}

// The kind at the top of a version's JSON picks the engine: absent or
// "graph" is the graph engine, "pipeline" the pipeline. The graph parser
// never returns a pipeline, so the graph engine cannot run one.
func TestWorkflowKindPicksTheParser(t *testing.T) {
	require.Equal(t, WorkflowKindGraph, WorkflowKind([]byte(`{"nodes":[],"edges":[],"budget":{}}`)))
	require.Equal(t, WorkflowKindGraph, WorkflowKind([]byte(`{"kind":"graph","nodes":[]}`)))
	require.Equal(t, WorkflowKindGraph, WorkflowKind([]byte(`not json`)), "the graph parser reports it")
	require.Equal(t, WorkflowKindPipeline, WorkflowKind([]byte(`{"kind":"pipeline"}`)))
	require.Equal(t, "robot", WorkflowKind([]byte(`{"kind":"robot"}`)))

	graph, err := ParseWorkflowGraph([]byte(`{"kind":"graph","nodes":[],"edges":[],"budget":{}}`))
	require.NoError(t, err)
	require.Equal(t, WorkflowKindGraph, graph.Kind)

	invalid := func(err error) string {
		t.Helper()
		var refused *WorkflowInvalidError
		require.ErrorAs(t, err, &refused)
		return refused.Issues[0].Code
	}
	implementer, reviewer, _ := exampleProfiles()
	pipeline, err := json.Marshal(TeamPipeline(implementer, reviewer))
	require.NoError(t, err)
	_, err = ParseWorkflowGraph(pipeline)
	require.Equal(t, "pipeline", invalid(err))
	_, err = ParseWorkflowGraph([]byte(`{"kind":"robot","nodes":[],"edges":[],"budget":{}}`))
	require.Equal(t, "invalid_kind", invalid(err))

	_, err = ParsePipeline([]byte(`{"kind":"pipeline","phases":[],"roster":[],"sudo":true}`))
	require.Equal(t, "malformed", invalid(err), "unknown fields are refused")
	_, err = ParsePipeline([]byte(`{"kind":"graph","phases":[],"roster":[]}`))
	require.Equal(t, "malformed", invalid(err))
	_, err = ParsePipeline([]byte(`{"kind":"pipeline","phases":[{"id":"x","label":"` + strings.Repeat("a", WorkflowMaxGraphBytes) + `"}]}`))
	require.Equal(t, "too_large", invalid(err))
}

func TestPipelineValidationRejects(t *testing.T) {
	implementer, reviewer, profiles := exampleProfiles()
	phase := func(p *Pipeline, id string) *PipelinePhase {
		for i := range p.Phases {
			if p.Phases[i].ID == id {
				return &p.Phases[i]
			}
		}
		t.Fatalf("no phase %s", id)
		return nil
	}
	cases := []struct {
		name   string
		change func(p *Pipeline)
		code   string
	}{
		{"no phases", func(p *Pipeline) { p.Phases = nil }, "pipeline_size"},
		{"too many phases", func(p *Pipeline) {
			for len(p.Phases) <= WorkflowMaxNodes {
				p.Phases = append([]PipelinePhase{{ID: fmt.Sprintf("extra-%d", len(p.Phases)), Label: "Extra", Owner: "implementer"}}, p.Phases...)
			}
		}, "pipeline_size"},
		{"no roles", func(p *Pipeline) { p.Roster = nil }, "pipeline_size"},
		{"too many roles", func(p *Pipeline) {
			for i := 0; len(p.Roster) <= PipelineMaxRoster; i++ {
				p.Roster = append(p.Roster, PipelineMember{Role: "expert-" + string(rune('a'+i)), ProfileID: reviewer})
			}
		}, "pipeline_size"},
		{"a role in capitals", func(p *Pipeline) { p.Roster = append(p.Roster, PipelineMember{Role: "Security", ProfileID: reviewer}) }, "invalid_role"},
		{"a reserved role", func(p *Pipeline) { p.Roster = append(p.Roster, PipelineMember{Role: "lead", ProfileID: reviewer}) }, "invalid_role"},
		{"a role twice", func(p *Pipeline) { p.Roster = append(p.Roster, PipelineMember{Role: "reviewer", ProfileID: reviewer}) }, "duplicate_role"},
		{"a profile that does not exist", func(p *Pipeline) { p.Roster[1].ProfileID = uuid.New() }, "missing_profile"},
		{"no writer", func(p *Pipeline) { p.Roster[0].Writer = false }, "writer_count"},
		{"two writers", func(p *Pipeline) {
			p.Roster = append(p.Roster, PipelineMember{Role: "second", ProfileID: implementer, Writer: true})
		}, "writer_count"},
		{"a writer that reviews", func(p *Pipeline) { p.Roster[0].ProfileID = reviewer }, "invalid_writer"},
		{"a writer on call", func(p *Pipeline) { p.Roster[0].OnCall = true }, "invalid_writer"},
		{"a lead without a profile", func(p *Pipeline) { p.Lead = &PipelineLead{ProfileID: uuid.New()} }, "missing_profile"},
		{"a bad phase ID", func(p *Pipeline) { p.Phases[0].ID = "not ok" }, "invalid_id"},
		{"a phase ID twice", func(p *Pipeline) { p.Phases[1].ID = "implement" }, "invalid_id"},
		{"a phase without a name", func(p *Pipeline) { p.Phases[0].Label = " " }, "invalid_label"},
		{"a name on two lines", func(p *Pipeline) { p.Phases[0].Label = "Implement\nnow" }, "invalid_label"},
		{"a long prompt", func(p *Pipeline) { p.Phases[0].Prompt = strings.Repeat("a", WorkflowMaxPromptBytes+1) }, "prompt_too_long"},
		{"an unknown owner", func(p *Pipeline) { p.Phases[0].Owner = "security" }, "invalid_owner"},
		{"a guarded phase before the last", func(p *Pipeline) { phase(p, "pr").Guarded = true }, "invalid_guard"},
		{"a guarded phase a role runs", func(p *Pipeline) { phase(p, "merge").Owner = "implementer" }, "invalid_guard"},
		// The owner merges (A6): a guarded merge needs a person's approval first.
		{"a merge nobody approves", func(p *Pipeline) { phase(p, "approval").Checks = nil }, "missing_approval"},
		{"an advisory approval", func(p *Pipeline) { phase(p, "approval").Checks[0].Required = false }, "missing_approval"},
		{"an approval only at the merge", func(p *Pipeline) {
			phase(p, "merge").Checks, phase(p, "approval").Checks = phase(p, "approval").Checks, nil
		}, "missing_approval"},
		{"too many checks", func(p *Pipeline) {
			for i := 0; i <= PipelineMaxChecks; i++ {
				p.Phases[0].Checks = append(p.Phases[0].Checks, PipelineCheck{ID: "c" + string(rune('a'+i)), Fact: PipelineFactCIGreen})
			}
		}, "pipeline_size"},
		{"a check ID twice", func(p *Pipeline) {
			phase(p, "integrate").Checks[1].ID = phase(p, "integrate").Checks[0].ID
		}, "invalid_id"},
		{"a fact that is not a condition", func(p *Pipeline) { phase(p, "pr").Checks[0].Fact = "head_sha" }, "invalid_check"},
		{"an unknown fact", func(p *Pipeline) { phase(p, "pr").Checks[0].Fact = "sudo" }, "invalid_check"},
		{"a verdict without a role", func(p *Pipeline) { phase(p, "review").Checks[0].Role = "" }, "invalid_check"},
		{"a verdict from a role not on the roster", func(p *Pipeline) { phase(p, "review").Checks[0].Role = "security" }, "invalid_check"},
		{"a verdict from the writer", func(p *Pipeline) { phase(p, "review").Checks[0].Role = "implementer" }, "invalid_check"},
		{"a script check without a command", func(p *Pipeline) {
			p.Phases[0].Checks = []PipelineCheck{{ID: "tests", Fact: PipelineFactScript, Script: "  "}}
		}, "invalid_check"},
		{"a long script", func(p *Pipeline) {
			p.Phases[0].Checks = []PipelineCheck{{ID: "tests", Fact: PipelineFactScript, Script: strings.Repeat("a", WorkflowMaxPromptBytes+1)}}
		}, "invalid_check"},
		{"a script on another fact", func(p *Pipeline) { phase(p, "pr").Checks[0].Script = "make test" }, "invalid_check"},
		{"a role on another fact", func(p *Pipeline) { phase(p, "pr").Checks[0].Role = "reviewer" }, "invalid_check"},
		{"approval by a lead it does not have", func(p *Pipeline) { p.Autonomy.Approve = PipelineLeadRole }, "invalid_autonomy"},
		{"approval by someone else", func(p *Pipeline) { p.Autonomy.Approve = "reviewer" }, "invalid_autonomy"},
		{"a waiver of a check that does not exist", func(p *Pipeline) { p.Autonomy.Waive = map[string]string{"nope": PipelineOwnerHuman} }, "invalid_autonomy"},
		{"a waiver by a lead it does not have", func(p *Pipeline) { p.Autonomy.Waive = map[string]string{"ci": PipelineLeadRole} }, "invalid_autonomy"},
		{"an unknown budget end", func(p *Pipeline) { p.Budget.OnExhausted = "explode" }, "invalid_budget"},
		{"too many turns", func(p *Pipeline) { p.Budget.MaxTurns = WorkflowMaxIterations + 1 }, "invalid_budget"},
		{"negative minutes", func(p *Pipeline) { p.Budget.MaxMinutes = -1 }, "invalid_budget"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := TeamPipeline(implementer, reviewer)
			c.change(&p)
			require.Equal(t, []string{c.code}, issueCodes(ValidatePipeline(p, profiles)))
		})
	}
}

// A lead, on-call experts, advisory script checks and waivers the owner
// granted the lead are all part of a valid pipeline.
func TestPipelineWithALeadAndExpertsIsValid(t *testing.T) {
	implementer, reviewer, profiles := exampleProfiles()
	p := TeamPipeline(implementer, reviewer)
	p.Lead = &PipelineLead{ProfileID: reviewer}
	p.Roster = append(p.Roster, PipelineMember{Role: "uiux", ProfileID: reviewer, OnCall: true})
	p.Phases[0].Checks = []PipelineCheck{{ID: "local-tests", Fact: PipelineFactScript, Script: "make test"}}
	p.Phases[2].Checks = append(p.Phases[2].Checks, PipelineCheck{ID: "uiux", Fact: PipelineFactVerdict, Role: "uiux"})
	p.Autonomy = PipelineAutonomy{Approve: PipelineLeadRole, Waive: map[string]string{"ci": PipelineOwnerHuman, "uiux": PipelineLeadRole}}
	require.Empty(t, ValidatePipeline(p, profiles))
}

// Every fact with a defined "met" can be required (owner decision on
// KUA-258): the plan's conditions, not only those of the team template.
// Values such as the head or base name are reported, never required.
func TestPipelineChecksAcceptEveryConditionFact(t *testing.T) {
	implementer, reviewer, profiles := exampleProfiles()
	for _, fact := range PipelineConditionFacts {
		p := TeamPipeline(implementer, reviewer)
		p.Phases[0].Checks = []PipelineCheck{{ID: "c", Fact: fact}}
		require.Empty(t, ValidatePipeline(p, profiles), fact)
	}
	require.ElementsMatch(t, []string{"pr_exists", "pr_draft", "head_contains_base", "conflict", "migration_collision", "ci_green", "approved", "merged", "closed", "overlap"}, PipelineConditionFacts)
	for _, value := range []string{"head_sha", "base_ref", "board", "merged_elsewhere"} {
		p := TeamPipeline(implementer, reviewer)
		p.Phases[0].Checks = []PipelineCheck{{ID: "c", Fact: value}}
		require.NotEmpty(t, ValidatePipeline(p, profiles), value)
	}
}
