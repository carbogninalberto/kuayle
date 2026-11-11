package machine

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/internal/domain"
	log "github.com/sirupsen/logrus"
)

// The closing continuation asks an attempt that concluded with something
// still missing for exactly those things, in its own session, once.
//
//	asked := m.askToClose(ctx, attempt, []domain.ClosingAsk{...})
//
// It is one place on purpose: every reason to ask an agent for something
// before its step closes (a notebook update, a reply to a message from the
// task's owner) adds a domain.ClosingAsk to the list instead of a
// continuation of its own, so an attempt is continued at most once whatever
// is missing. settleClosing builds that list when a run ends: it decides
// what is missing before the call, and looks again after it; the
// continuation never decides the attempt's outcome.
//
// What it guarantees:
//   - same session: the attempt's own harness session is continued (Claude
//     Code with --resume, Codex with "exec resume"), from the command line
//     stored when the run was created. A harness without one is not asked;
//   - once: closing_asked_at is taken before the container starts, so a
//     manager that restarts does not ask again;
//   - bounded: closingTimeout, never past closingMargin before the
//     attempt's own time limit (the run is still recorded as running, and a
//     run past its limit is killed and marked as timed out), skipped when
//     less than closingMinimum would be left; one short prompt
//     (domain.ClosingPrompt);
//   - contained: it is a second container of the same run, named
//     "...-agent-<run>-closing", under the run's sandbox, mounts and
//     authorization, without the live inbox. Its output is not the
//     attempt's: summary and logs stay those of the work;
//   - cleaned up: that container is removed by its exact name on every
//     path out, and one found left over by a crash is removed, not re-run.
//     The attempt's own container is never touched from here.

// closingTimeout bounds the closing continuation. It also ends
// closingMargin before the attempt's own time limit, which leaves room to
// collect the outbox and record the result, and it is not started with less
// than closingMinimum left: it must never turn an attempt that ended well
// into one that timed out.
const (
	closingTimeout = 5 * time.Minute
	closingMargin  = time.Minute
	closingMinimum = 30 * time.Second
)

// closingSuffix names the continuation's container after the run's own.
const closingSuffix = "-closing"

// closingAttempt is the finished attempt a closing continuation continues.
type closingAttempt struct {
	machine  *domain.DevMachine
	run      *domain.DevMachineAgentRun
	provider *domain.DevMachineAgentProvider
	checkout *domain.DevMachineCheckout
	secrets  *domain.AgentRunSecretSnapshot
	// deadline is the attempt's own time limit; without one nothing is asked.
	deadline time.Time
}

// closingEnd is when a continuation started now must end, and whether there
// is enough time left to start one.
func closingEnd(deadline time.Time) (time.Time, bool) {
	end := deadline.Add(-closingMargin)
	if latest := time.Now().Add(closingTimeout); latest.Before(end) {
		end = latest
	}
	return end, !deadline.IsZero() && time.Until(end) >= closingMinimum
}

// closingState is what the end of a run knows when it settles what is
// missing: the machine's services and how collecting the outbox went, when
// the harness exited, how the attempt ended (concluded: it did not fail;
// succeeded: it ended well), and whether it left the worktree unchanged.
type closingState struct {
	services     []domain.DevMachineService
	collectErr   error
	exited       time.Time
	concluded    bool
	succeeded    bool
	unchangedRun func() bool
}

// closingSettlement is what was still missing when the attempt closed.
type closingSettlement struct {
	replies              liveReplySettlement
	notebookNotCollected bool
}

// settleClosing is the one place that decides what an ended attempt is
// asked for. After the outbox was collected it lists what is missing (a
// reply to each delivered message of the task's owner, for an attempt that
// ended well; a notebook update, for one that concluded), asks for all of it
// in one closing continuation, collects the outbox again and looks again.
// Nothing is asked for what Kuayle itself failed to collect, and the
// attempt's outcome is never changed here: what is still missing is
// reported, for notes on the run.
func (m *Manager) settleClosing(ctx context.Context, attempt closingAttempt, state closingState) closingSettlement {
	run := attempt.run
	// Replies are judged only on an outbox that was really read.
	collected := state.collectErr == nil && developerRunning(state.services)
	settled := closingSettlement{replies: m.settleReplies(ctx, run, state.exited, collected)}
	var asks []domain.ClosingAsk
	replyAsked := state.succeeded && len(settled.replies.unanswered) > 0
	if replyAsked {
		asks = append(asks, domain.ReplyClosingAsk(settled.replies.unanswered))
	}
	notebook, notCollected := m.notebookAsk(ctx, run, state.collectErr, state.concluded, state.unchangedRun)
	settled.notebookNotCollected = notCollected
	if notebook != nil {
		asks = append(asks, *notebook)
	}
	if len(asks) > 0 && m.askToClose(ctx, attempt, asks) {
		collectCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		err := m.collectOutbox(collectCtx, attempt.machine, state.services)
		cancel()
		if notebook != nil {
			settled.notebookNotCollected = err != nil
		}
		settled.replies = m.repliesAfterClosing(ctx, run, err == nil)
	}
	switch {
	case settled.replies.unknown:
		settled.replies.notes = []string{liveCollectFailedNote}
		_ = m.createEvent(ctx, attempt.machine, &run.ID, "agent", "agent_live_messages.collect_failed", map[string]any{})
	case state.succeeded && len(settled.replies.unanswered) > 0:
		settled.replies.notes = []string{domain.UnansweredMessageNote(settled.replies.unanswered)}
	}
	return settled
}

// closingStore keeps the continuation to once per attempt and records it.
type closingStore interface {
	MarkClosingAsked(ctx context.Context, runID uuid.UUID) (bool, error)
	RecordClosingSeconds(ctx context.Context, runID uuid.UUID, seconds int) error
}

// closingRuntime removes the continuation's container by its exact name.
type closingRuntime interface {
	RemoveClosingContainer(ctx context.Context, machine *domain.DevMachine, run *domain.DevMachineAgentRun, suffix string) error
}

// askToClose continues the attempt's own session once to ask for asks, and
// reports whether the continuation ran. It returns false, having asked
// nothing, when there is nothing to ask, the harness cannot continue a
// session, the session is unknown, or the attempt was already asked.
func (m *Manager) askToClose(ctx context.Context, attempt closingAttempt, asks []domain.ClosingAsk) bool {
	run := attempt.run
	store, ok := m.store.(closingStore)
	prompt := domain.ClosingPrompt(asks)
	if !ok || prompt == "" || run.Mode == "interactive" || len(run.ClosingContinuation) == 0 || attempt.secrets == nil {
		return false
	}
	current, err := m.store.GetAgentRunInternal(ctx, run.ID)
	if err != nil || current == nil || current.NativeSessionID == nil || !agent.ValidSessionID(*current.NativeSessionID) || current.Status == domain.DevMachineAgentRunStatusCancelled {
		return false
	}
	var argv []string
	if json.Unmarshal(run.ClosingContinuation, &argv) != nil {
		return false
	}
	session, prompted := false, false
	for i, argument := range argv {
		switch argument {
		case domain.ClosingSessionPlaceholder:
			argv[i], session = *current.NativeSessionID, true
		case domain.ClosingPromptPlaceholder:
			argv[i], prompted = prompt, true
		}
	}
	command, err := json.Marshal(argv)
	if err != nil || !session || !prompted {
		return false
	}
	// The sign-in the attempt may have refreshed is read again, so the
	// continuation never presents a rotated credential.
	environment := make(map[string]string, len(attempt.secrets.Environment))
	for name, value := range attempt.secrets.Environment {
		environment[name] = value
	}
	used := environment[signInSecretNames[run.ProviderID]]
	if used != "" {
		if err := m.agentSignInSecrets(ctx, run, environment, map[string]string{}); err != nil {
			return false
		}
		used = environment[signInSecretNames[run.ProviderID]]
	}
	// Whatever happens next, no continuation container outlives this call.
	remove := func() {
		runtime, ok := m.runtime.(closingRuntime)
		if !ok {
			return
		}
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if err := runtime.RemoveClosingContainer(cleanupCtx, attempt.machine, run, closingSuffix); err != nil {
			log.WithFields(log.Fields{"machine_id": attempt.machine.ID, "agent_run_id": run.ID}).WithError(err).Warn("the closing continuation's container could not be removed")
		}
	}
	// Never past the attempt's own time limit, and not at all with too
	// little of it left: decided before the attempt is marked as asked.
	end, enough := closingEnd(attempt.deadline)
	if !enough {
		return false
	}
	// Once per attempt: the mark is taken before the container starts. A
	// mark already taken means a continuation ran, or was running when the
	// manager stopped: what it left is removed, and it is not run again.
	if first, err := store.MarkClosingAsked(ctx, run.ID); err != nil || !first {
		remove()
		return false
	}
	defer remove()
	continuation := *run
	continuation.CommandArgv, continuation.ContainerSuffix = command, closingSuffix
	askCtx, cancel := context.WithDeadline(ctx, end)
	observer := &AgentObserver{
		Authorize: func() error { return m.store.AuthorizeAgentRun(askCtx, run.ID) },
		Output:    func(string, []byte) error { return nil },
	}
	started := time.Now()
	_, runErr := m.runtime.RunAgent(askCtx, attempt.machine, &continuation, attempt.provider, attempt.checkout, environment, observer)
	seconds := int(time.Since(started).Round(time.Second) / time.Second)
	timedOut := errors.Is(askCtx.Err(), context.DeadlineExceeded)
	cancel()
	if runErr != nil {
		log.WithFields(log.Fields{"machine_id": attempt.machine.ID, "agent_run_id": run.ID, "timed_out": timedOut}).WithError(runErr).Warn("the closing continuation did not finish")
	}
	if used != "" {
		returnCtx, cancelReturn := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		if err := m.returnSignIn(returnCtx, attempt.machine, run, used); err != nil {
			log.WithField("agent_run_id", run.ID).WithError(err).Warn("refreshed agent sign-in could not be kept")
		}
		cancelReturn()
	}
	// That it ran, for how long and how it ended stay on the run and in the
	// machine's events; its token usage is not the attempt's.
	if err := store.RecordClosingSeconds(context.WithoutCancel(ctx), run.ID, seconds); err != nil {
		log.WithField("agent_run_id", run.ID).WithError(err).Warn("the closing continuation's duration could not be recorded")
	}
	_ = m.createEvent(ctx, attempt.machine, &run.ID, "agent", "agent_run.closing_continuation", map[string]any{
		"provider_id": run.ProviderID, "asks": len(asks), "seconds": seconds, "finished": runErr == nil, "timed_out": timedOut})
	return true
}
