package domain

import (
	"strconv"
	"strings"
)

// A reply is mandatory, and the work is never lost over a missing one. Every
// message a person sends to a task's agent (or the orchestrator sends for
// them) must be answered by the agent: an outbox file addressed to the
// orchestrator whose "reply_to" names the message's sequence number. Kuayle
// asks for it, not only the prompt: Claude Code is not allowed to stop while
// a delivered message has no reply (its Stop hook reads
// AgentLiveUnansweredFile), and an attempt that ends well with such a message
// is asked for the reply in its closing continuation (ReplyClosingAsk), the
// one extra turn an attempt gets for whatever is missing. An attempt never fails
// for a missing reply: the message is flagged as unanswered and given once
// more to the same step's next attempt, if there is one.
const (
	// AgentLiveUnansweredFile is the inbox file listing the sequence numbers
	// the running attempt still owes a reply to: {"unanswered":[3,5]}.
	AgentLiveUnansweredFile = "unanswered.json"
	// AgentLiveReplyAttempts is how many attempts that ran to an end are
	// given a message before Kuayle stops asking; it then stays flagged as
	// unanswered. An attempt that was interrupted or paused does not count.
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

// UnansweredMessageNote is what an attempt's result says when it ended
// although the agent never replied to the messages with these sequence
// numbers. It is a note, never the reason an attempt fails.
func UnansweredMessageNote(seqs []int64) string {
	names := make([]string, 0, len(seqs))
	for _, seq := range seqs {
		names = append(names, strconv.FormatInt(seq, 10))
	}
	noun := "message "
	if len(names) != 1 {
		noun = "messages "
	}
	return "The agent did not reply to " + noun + strings.Join(names, ", ") + " from the person who owns the task. Kuayle asked again; the work is kept and the message is shown as unanswered."
}

// ReplyClosingAsk is what the closing continuation asks of an attempt that
// ended well without replying to the messages with these sequence numbers.
func ReplyClosingAsk(seqs []int64) ClosingAsk {
	names := make([]string, 0, len(seqs))
	for _, seq := range seqs {
		names = append(names, strconv.FormatInt(seq, 10))
	}
	noun := "message "
	if len(names) != 1 {
		noun = "messages "
	}
	return ClosingAsk{
		Missing: "you did not reply to " + noun + strings.Join(names, ", ") + " from the person who owns this task (you received them during this session)",
		Do:      "For each of them write one file `.kuayle/outbox/<name>.json` at this worktree's root containing {\"to\":\"orchestrator\",\"reply_to\":<message number>,\"body\":\"...\"}, saying what you understood and what you did about it.",
	}
}

// HarnessReadsLiveInbox reports whether Kuayle can hand the harness a message
// in the middle of a turn. Only Claude Code has such a hook; any other
// harness receives a person's message by being interrupted and resumed.
func HarnessReadsLiveInbox(provider string) bool {
	return provider == "claude-code"
}

// LiveInterrupting reports whether the task's working attempt is being
// stopped to deliver a message, after which it continues by itself.
func (t AgentTask) LiveInterrupting() bool {
	return t.State == "cancelling" && t.LiveResumeGeneration != nil && *t.LiveResumeGeneration == t.Generation+1
}

// LiveResumed reports whether the task's current attempt was started by a
// person to deliver a message (an interruption, or a start when none ran).
func (t AgentTask) LiveResumed() bool {
	return t.LiveResumeGeneration != nil && *t.LiveResumeGeneration == t.Generation
}
