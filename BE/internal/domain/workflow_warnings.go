package domain

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

// A planning profile only writes a plan: Kuayle mounts its worktree
// read-only and fails the step if it changed anything. A step that runs one
// while its name or prompt tells the agent to implement gives it two
// contradicting instructions (KUA-122), so validation warns about it. The
// rule is deliberately simple:
//
//   - the step's name is that of an implementing role (it contains
//     "implement", "build", "coder", "developer" or "engineer"), or
//   - a sentence or clause of its prompt starts with an order to change
//     code: implement, write/change the code, make/apply the changes, fix,
//     commit, push or open a pull request (after words such as "then",
//     "please", "now"). "Plan how to implement" and "do not implement"
//     start with something else, so they pass.
var (
	implementingLabel  = regexp.MustCompile(`(?i)\b(implement\w*|build\w*|coder|developer|engineer)\b`)
	promptClauseBreak  = regexp.MustCompile(`[.;:!?\n,]+`)
	implementingClause = regexp.MustCompile(`(?i)^(?:(?:and|then|now|next|first|finally|please|also|afterwards|you|must|should|to)\s+)*(implement\b|(?:write|change|edit|modify|update)\s+the\s+code\b|(?:make|apply)\s+(?:the|these|those|your)\s+changes\b|fix\b|commit\b|push\b|open\s+a\s+pull\s+request\b)`)
)

// WorkflowWarnings are problems of a graph that do not stop it from being
// saved or run. They use the shape of validation issues, so the editor can
// show them on the step.
func WorkflowWarnings(g WorkflowGraph, profiles map[uuid.UUID]AgentProfile) []WorkflowIssue {
	warnings := []WorkflowIssue{}
	for _, node := range g.Nodes {
		if node.Type != WorkflowNodeAgent || node.ProfileID == nil {
			continue
		}
		profile, ok := profiles[*node.ProfileID]
		if !ok || !profile.Plans() {
			continue
		}
		label := strings.TrimSpace(node.Label)
		warn := func(what string) {
			warnings = append(warnings, WorkflowIssue{Code: "planning_step_implements", NodeID: node.ID,
				Message: fmt.Sprintf("%s: the profile %q only plans (its worktree is read-only and the step fails if it changes code), but %s. Rename the step and ask for a plan, or choose a profile that changes code", label, profile.Name, what)})
		}
		if word := implementingLabel.FindString(label); word != "" {
			warn(fmt.Sprintf("the step's name says %q", word))
		}
		for _, clause := range promptClauseBreak.Split(node.Prompt, -1) {
			clause = strings.TrimSpace(clause)
			if implementingClause.MatchString(clause) {
				warn(fmt.Sprintf("its prompt tells it to %q", truncateUTF8(strings.Join(strings.Fields(clause), " "), 80)))
				break
			}
		}
	}
	return warnings
}
