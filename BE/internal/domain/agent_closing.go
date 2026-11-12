package domain

import (
	"fmt"
	"strings"
)

// The closing continuation: an attempt that concluded with something still
// missing (today: a valid update of the task notebook) is asked for it once
// more, in its own session, with one short prompt that lists what is missing
// and asks for nothing else. It is bounded, happens once per attempt, and
// never decides the attempt's outcome: what is still missing afterwards is
// handled by whoever asked.

// ClosingAsk is one thing the closing continuation asks an agent for.
type ClosingAsk struct {
	// Missing says what is missing, as a clause addressed to the agent
	// ("you did not write ...").
	Missing string
	// Do says exactly what to do about it, in one or two sentences.
	Do string
	// Reply says the agent answers this ask with its final message (a
	// rewritten plan), not with a file.
	Reply bool
}

// ClosingPrompt is the prompt of the closing continuation for asks, or ""
// when there is nothing to ask.
func ClosingPrompt(asks []ClosingAsk) string {
	const only = " Do nothing else: do not change any other file, do not run tests and do not repeat your summary. Reply with one line when it is done."
	const replyOnly = " Do nothing else: do not change any file in the worktree and do not run tests. Your final message must be the rewritten plan, whole, and nothing else."
	closing := only
	for _, ask := range asks {
		if ask.Reply {
			closing = replyOnly
			break
		}
	}
	switch len(asks) {
	case 0:
		return ""
	case 1:
		return "One thing is missing before this step can close: " + strings.TrimSpace(asks[0].Missing) + ". " + strings.TrimSpace(asks[0].Do) + closing
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d things are missing before this step can close:", len(asks))
	for i, ask := range asks {
		fmt.Fprintf(&b, "\n%d. %s. %s", i+1, strings.TrimSpace(ask.Missing), strings.TrimSpace(ask.Do))
	}
	return b.String() + "\n" + strings.TrimSpace(closing)
}

// PlanMaxCharacters is the plan length a planning step is asked to stay
// under; a plan over agent.MaxSummaryBytes cannot reach the next agent whole.
const PlanMaxCharacters = 12000

// PlanClosingAsk asks a planning step to rewrite a plan that is too long to
// hand on whole.
func PlanClosingAsk() ClosingAsk {
	return ClosingAsk{
		Missing: "your plan is longer than the next agent can receive whole (it was cut at 16 KB)",
		Do:      fmt.Sprintf("Rewrite it under %d characters: keep what to change and where, in which order, which tests to add or run, and your decisions; move inventories, long lists and other detail into your notebook update (plan.steps, findings), or leave them for the next agent to work out", PlanMaxCharacters),
		Reply:   true,
	}
}

// PlanShortenedNote is the risk note on a run whose plan was shortened by
// the closing continuation.
const PlanShortenedNote = "The planning step's first plan was too long to hand on whole; Kuayle asked it once to shorten it, and the shortened plan is the one handed on."

// Placeholders in the command line that continues an attempt's session
// (dev_machine_agent_runs.closing_continuation). The manager replaces them
// with the session the attempt ran in and with ClosingPrompt's text.
const (
	ClosingSessionPlaceholder = "kuayle-closing-session"
	ClosingPromptPlaceholder  = "KUAYLE_CLOSING_PROMPT"
)
