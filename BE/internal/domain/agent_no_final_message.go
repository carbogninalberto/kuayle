package domain

import "strings"

const noFinalMessageReasonPrefix = "The agent ended without a final message"

// NoFinalMessageReason is the error of a run whose harness exited normally
// although the agent never concluded: it stopped on a tool call or on an
// error, so nothing says what it did. detail is what the harness reported,
// if anything. The text is kept verbatim as the task's reason.
func NoFinalMessageReason(detail string) string {
	reason := noFinalMessageReasonPrefix + ", so there is no summary of what it did"
	if detail = strings.TrimSpace(detail); detail != "" {
		reason += ". " + detail
	}
	return reason
}

// IsNoFinalMessageReason reports whether a run error or task reason is the
// explanation above.
func IsNoFinalMessageReason(reason string) bool {
	return strings.HasPrefix(reason, noFinalMessageReasonPrefix)
}
