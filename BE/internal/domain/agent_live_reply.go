package domain

import (
	"strconv"
	"strings"
)

// A reply is mandatory. Every message a person sends to a task's agent (or
// the orchestrator sends for them) must be answered by the agent: an outbox
// file addressed to the orchestrator whose "reply_to" names the message's
// sequence number. Kuayle enforces it, not the prompt: Claude Code is not
// allowed to stop while a delivered message has no reply (its Stop hook reads
// AgentLiveUnansweredFile), and for every harness an attempt that ends
// normally with such a message is not a success. The message is then given
// once more, to the same step's next attempt.
const (
	// AgentLiveUnansweredFile is the inbox file listing the sequence numbers
	// the running attempt still owes a reply to: {"unanswered":[3,5]}.
	AgentLiveUnansweredFile = "unanswered.json"
	// AgentLiveReplyAttempts is how many attempts are given a message before
	// Kuayle stops asking; it then stays flagged as unanswered.
	AgentLiveReplyAttempts = 2
	// AgentLiveStopReminders is how often Claude Code's Stop hook refuses to
	// stop for the same unanswered message within one attempt.
	AgentLiveStopReminders = 3

	// How a person's message is sent (POST live-messages "mode").
	// AgentLiveSendInterrupt stops the working attempt and resumes its
	// session with the message; AgentLiveSendStart starts an attempt for it
	// when none runs. Empty queues it for the working attempt's inbox, or the
	// next attempt's prompt.
	AgentLiveSendInterrupt = "interrupt"
	AgentLiveSendStart     = "start"
)

const unansweredMessageReasonPrefix = "The agent ended without replying to "

// UnansweredMessageReason is the error of a run that ended normally although
// the agent never replied to the messages with these sequence numbers. The
// text is kept verbatim as the task's reason.
func UnansweredMessageReason(seqs []int64) string {
	names := make([]string, 0, len(seqs))
	for _, seq := range seqs {
		names = append(names, strconv.FormatInt(seq, 10))
	}
	noun := "message "
	if len(names) != 1 {
		noun = "messages "
	}
	return unansweredMessageReasonPrefix + noun + strings.Join(names, ", ") + " from the person who owns the task"
}

// IsUnansweredMessageReason reports whether a run error or task reason is the
// explanation above.
func IsUnansweredMessageReason(reason string) bool {
	return strings.HasPrefix(reason, unansweredMessageReasonPrefix)
}

// HarnessReadsLiveInbox reports whether Kuayle can hand the harness a message
// in the middle of a turn. Only Claude Code has such a hook; any other
// harness receives a person's message by being interrupted and resumed.
func HarnessReadsLiveInbox(provider string) bool {
	return provider == "claude-code"
}

// LiveResumed reports whether the task's current attempt was started by a
// person to deliver a message (an interruption, or a start when none ran).
func (t AgentTask) LiveResumed() bool {
	return t.LiveResumeGeneration != nil && *t.LiveResumeGeneration == t.Generation
}
