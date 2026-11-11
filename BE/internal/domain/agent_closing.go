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
}

// ClosingPrompt is the prompt of the closing continuation for asks, or ""
// when there is nothing to ask.
func ClosingPrompt(asks []ClosingAsk) string {
	const only = " Do nothing else: do not change any other file, do not run tests and do not repeat your summary. Reply with one line when it is done."
	switch len(asks) {
	case 0:
		return ""
	case 1:
		return "One thing is missing before this step can close: " + strings.TrimSpace(asks[0].Missing) + ". " + strings.TrimSpace(asks[0].Do) + only
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d things are missing before this step can close:", len(asks))
	for i, ask := range asks {
		fmt.Fprintf(&b, "\n%d. %s. %s", i+1, strings.TrimSpace(ask.Missing), strings.TrimSpace(ask.Do))
	}
	return b.String() + "\n" + strings.TrimSpace(only)
}

// Placeholders in the command line that continues an attempt's session
// (dev_machine_agent_runs.closing_continuation). The manager replaces them
// with the session the attempt ran in and with ClosingPrompt's text.
const (
	ClosingSessionPlaceholder = "kuayle-closing-session"
	ClosingPromptPlaceholder  = "KUAYLE_CLOSING_PROMPT"
)
