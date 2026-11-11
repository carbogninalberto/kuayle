package machine

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/containerd/errdefs"
	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
	log "github.com/sirupsen/logrus"
)

// The live channel lets the task's owner (or the orchestrator acting for
// them) reach a running attempt. While the attempt runs, the manager writes
// each queued message as <seq>.json into a per-run Docker volume that the
// agent container mounts read-only at /run/kuayle/inbox, outside the
// worktree, so the agent cannot write or forge messages there. The manager
// fills the volume through a writer container that is created with the
// volume read-write and never started. In the same loop the manager collects
// the outbox, so agent messages (including those to the orchestrator) are
// recorded within seconds instead of only when the attempt ends.

// liveChannelInterval is how often a running attempt's inbox and outbox are
// synchronized.
const liveChannelInterval = 5 * time.Second

const liveInboxService = "agent-inbox"

// errLiveInboxUnavailable reports an attempt without an inbox (yet).
var errLiveInboxUnavailable = errors.New("live inbox is unavailable")

// liveInboxRuntime is implemented by runtimes that give attempts an inbox.
type liveInboxRuntime interface {
	WriteLiveInbox(ctx context.Context, machine *domain.DevMachine, run *domain.DevMachineAgentRun, files []domain.AgentLiveInboxFile) error
	CloseLiveInbox(ctx context.Context, machine *domain.DevMachine, run *domain.DevMachineAgentRun) error
}

// liveReplyGrace is how long before the harness exited an inbox message may
// have been written and still not been read: it is queued again instead.
const liveReplyGrace = 20 * time.Second

// liveReplyTimeout bounds the one continuation that asks a finished attempt
// for the replies it owes. The run is still recorded as running meanwhile,
// and a run past its time limit is killed and marked as timed out, so the
// continuation also ends liveReplyMargin before the attempt's own limit
// (room to collect, settle and record the result), and is skipped when less
// than liveReplyMinimum would be left for it.
const (
	liveReplyTimeout = 3 * time.Minute
	liveReplyMargin  = time.Minute
	liveReplyMinimum = 30 * time.Second
)

// liveReplyStore knows which of an attempt's messages still owe a reply.
type liveReplyStore interface {
	UnansweredLiveMessages(ctx context.Context, run uuid.UUID) ([]int64, error)
	SettleLiveMessages(ctx context.Context, run uuid.UUID, unread time.Time) ([]int64, error)
}

// agentContinuationRuntime is implemented by runtimes that can run a
// finished attempt's harness once more on its session.
type agentContinuationRuntime interface {
	ContinueAgent(ctx context.Context, machine *domain.DevMachine, run *domain.DevMachineAgentRun, provider *domain.DevMachineAgentProvider, checkout *domain.DevMachineCheckout, secrets map[string]string, continuation []string, observer *AgentObserver) (*AgentExecution, error)
}

type liveInboxStore interface {
	PendingLiveMessages(ctx context.Context, run uuid.UUID) ([]domain.AgentLiveMessage, error)
	MarkLiveMessagesDelivered(ctx context.Context, run uuid.UUID, ids []uuid.UUID) (int, error)
}

// startLiveChannel synchronizes the run's inbox and the machine's outboxes
// until ctx ends or the returned stop is called; stop waits for the loop.
func (m *Manager) startLiveChannel(ctx context.Context, machine *domain.DevMachine, run *domain.DevMachineAgentRun) func() {
	interval := m.liveInterval
	if interval <= 0 {
		interval = liveChannelInterval
	}
	loopCtx, cancel := context.WithCancel(ctx)
	var done sync.WaitGroup
	done.Add(1)
	go func() {
		defer done.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		// What the inbox last said the attempt still owes a reply to.
		owed := ""
		for {
			select {
			case <-loopCtx.Done():
				return
			case <-ticker.C:
			}
			tickCtx, cancelTick := context.WithTimeout(loopCtx, 15*time.Second)
			if services, err := m.store.ListServices(tickCtx, machine.WorkspaceID, machine.ID); err == nil {
				m.collectOutbox(tickCtx, machine, services)
			}
			if err := m.deliverLiveInbox(tickCtx, machine, run); err != nil && !errors.Is(err, errLiveInboxUnavailable) && tickCtx.Err() == nil {
				log.WithFields(log.Fields{"machine_id": machine.ID, "agent_run_id": run.ID, "event_type": "agent_live_messages.deliver"}).WithError(err).Warn("live messages could not be delivered")
			}
			if written, err := m.writeUnanswered(tickCtx, machine, run, owed); err == nil {
				owed = written
			} else if !errors.Is(err, errLiveInboxUnavailable) && tickCtx.Err() == nil {
				log.WithFields(log.Fields{"machine_id": machine.ID, "agent_run_id": run.ID, "event_type": "agent_live_messages.unanswered"}).WithError(err).Warn("unanswered live messages could not be listed")
			}
			cancelTick()
		}
	}()
	return func() {
		cancel()
		done.Wait()
	}
}

// deliverLiveInbox writes the run's queued messages to its inbox, then marks
// them delivered. A failed mark leaves them queued; rewriting the same
// <seq>.json is harmless because the reader tracks sequence numbers.
func (m *Manager) deliverLiveInbox(ctx context.Context, machine *domain.DevMachine, run *domain.DevMachineAgentRun) error {
	runtime, ok := m.runtime.(liveInboxRuntime)
	store, recording := m.store.(liveInboxStore)
	if !ok || !recording {
		return nil
	}
	messages, err := store.PendingLiveMessages(ctx, run.ID)
	if err != nil || len(messages) == 0 {
		return err
	}
	files := make([]domain.AgentLiveInboxFile, 0, len(messages))
	ids := make([]uuid.UUID, 0, len(messages))
	for _, message := range messages {
		file, err := message.InboxFile()
		if err != nil {
			return err
		}
		files = append(files, file)
		ids = append(ids, message.ID)
	}
	if err = runtime.WriteLiveInbox(ctx, machine, run, files); err != nil {
		return err
	}
	delivered, err := store.MarkLiveMessagesDelivered(ctx, run.ID, ids)
	if delivered > 0 {
		_ = m.createEvent(ctx, machine, &run.ID, "agent", "agent_live_messages.delivered", map[string]any{"count": delivered})
	}
	return err
}

// writeUnanswered keeps the inbox's list of the messages the attempt still
// owes a reply to (domain.AgentLiveUnansweredFile) current: Claude Code's
// Stop hook reads it and does not let the agent stop while it names one. The
// outbox was collected just before, so a reply written meanwhile counts. It
// returns what the file now says; last is what it said before.
func (m *Manager) writeUnanswered(ctx context.Context, machine *domain.DevMachine, run *domain.DevMachineAgentRun, last string) (string, error) {
	runtime, ok := m.runtime.(liveInboxRuntime)
	store, recording := m.store.(liveReplyStore)
	if !ok || !recording {
		return last, nil
	}
	seqs, err := store.UnansweredLiveMessages(ctx, run.ID)
	if err != nil {
		return last, err
	}
	if seqs == nil {
		seqs = []int64{}
	}
	data, err := json.Marshal(map[string][]int64{"unanswered": seqs})
	if err != nil {
		return last, err
	}
	// An attempt that never owed a reply gets no file.
	if string(data) == last || (last == "" && len(seqs) == 0) {
		return last, nil
	}
	if err = runtime.WriteLiveInbox(ctx, machine, run, []domain.AgentLiveInboxFile{{Name: domain.AgentLiveUnansweredFile, Data: data}}); err != nil {
		return last, err
	}
	return string(data), nil
}

// liveCollectFailedNote is what an attempt's result says when Kuayle could
// not read the agent's last messages: a reply may exist that nobody saw.
const liveCollectFailedNote = "Kuayle could not collect the agent's messages when the attempt ended, so a reply the agent wrote may be missing. This is a problem on Kuayle's side, not the agent's."

// liveReplyRequest is what settleReplies needs to ask a finished attempt for
// its replies.
type liveReplyRequest struct {
	provider *domain.DevMachineAgentProvider
	checkout *domain.DevMachineCheckout
	secrets  *domain.AgentRunSecretSnapshot
	// signIn is the agent's own sign-in now in force, if it uses one.
	signIn string
	// exited is when the harness exited; finished whether it ended well, so
	// that only a missing reply stands between the attempt and its success.
	exited   time.Time
	finished bool
	// deadline is the attempt's own time limit; without one nothing is asked.
	deadline time.Time
}

// liveReplySettlement is what is known about an ended attempt's replies.
type liveReplySettlement struct {
	// unanswered are the messages it received and never replied to.
	unanswered []int64
	// unknown: the agent's last messages could not be collected, so whether
	// it replied is not known and nothing is said to be unanswered.
	unknown bool
	// notes are for the attempt's result.
	notes []string
}

// settleReplies closes the ended attempt's side of the live channel. It
// collects the agent's last messages and, only when that worked, decides
// which messages the attempt received and never replied to: a failed
// collection means a reply may exist unseen, so nothing is concluded and the
// result says so. An attempt that ended well still owing a reply is
// continued once, on the same session, to write it (requestReply); whatever
// is unanswered after that is reported as a note. A missing reply never
// changes how the attempt ended.
func (m *Manager) settleReplies(ctx context.Context, machine *domain.DevMachine, run *domain.DevMachineAgentRun, request liveReplyRequest) liveReplySettlement {
	store, ok := m.store.(liveReplyStore)
	if !ok {
		return liveReplySettlement{}
	}
	collect := func() error {
		collectCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer cancel()
		services, err := m.store.ListServices(collectCtx, machine.WorkspaceID, machine.ID)
		if err == nil {
			err = m.collectOutboxChecked(collectCtx, machine, services)
		}
		if err != nil {
			log.WithFields(log.Fields{"machine_id": machine.ID, "agent_run_id": run.ID, "event_type": "agent_live_messages.collect"}).WithError(err).Warn("the agent's last messages could not be collected")
			_ = m.createEvent(collectCtx, machine, &run.ID, "agent", "agent_live_messages.collect_failed", map[string]any{"error": safeError(err)})
		}
		return err
	}
	// Replies may exist that nobody saw: nothing is concluded.
	unknown := liveReplySettlement{unknown: true, notes: []string{liveCollectFailedNote}}
	collected := collect() == nil
	// A message that reached the inbox just before the harness exited is
	// queued again whether or not the outbox could be read.
	settleCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	unanswered, err := store.SettleLiveMessages(settleCtx, run.ID, request.exited.Add(-liveReplyGrace))
	if err != nil {
		log.WithFields(log.Fields{"agent_run_id": run.ID, "event_type": "agent_live_messages.settle"}).WithError(err).Warn("live messages could not be settled")
		if collected {
			return liveReplySettlement{}
		}
		return unknown
	}
	if !collected {
		// Only an attempt that owes a reply has one that could be missing.
		if len(unanswered) == 0 {
			return liveReplySettlement{}
		}
		return unknown
	}
	if len(unanswered) == 0 || !request.finished {
		return liveReplySettlement{unanswered: unanswered}
	}
	if m.requestReply(ctx, machine, run, request, unanswered) {
		if collect() != nil {
			return unknown
		}
		againCtx, cancelAgain := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer cancelAgain()
		if unanswered, err = store.UnansweredLiveMessages(againCtx, run.ID); err != nil {
			log.WithFields(log.Fields{"agent_run_id": run.ID, "event_type": "agent_live_messages.settle"}).WithError(err).Warn("live messages could not be settled")
			return liveReplySettlement{}
		}
	}
	settled := liveReplySettlement{unanswered: unanswered}
	if len(unanswered) > 0 {
		settled.notes = []string{domain.UnansweredMessageNote(unanswered)}
	}
	return settled
}

// requestReply continues a finished attempt once, on its own native session,
// with a prompt that asks only for the replies it owes, and reports whether
// it did. It is bounded by liveReplyTimeout and by what is left of the
// attempt's time limit, and its output is discarded: the
// attempt's result, summary and logs are those of its work. Only a harness
// that can continue a session gets it (domain.HarnessContinuesSession), and
// only an attempt nobody cancelled.
func (m *Manager) requestReply(ctx context.Context, machine *domain.DevMachine, run *domain.DevMachineAgentRun, request liveReplyRequest, unanswered []int64) bool {
	runtime, ok := m.runtime.(agentContinuationRuntime)
	if !ok || run.TaskID == nil || !domain.HarnessContinuesSession(run.ProviderID) || request.secrets == nil {
		return false
	}
	current, err := m.store.GetAgentRunInternal(ctx, run.ID)
	if err != nil || current == nil || terminalAgentStatus(current.Status) || current.NativeSessionID == nil {
		return false
	}
	var argv []string
	if json.Unmarshal(run.CommandArgv, &argv) != nil {
		return false
	}
	continuation, ok := agent.ReplyContinuation(run.ProviderID, argv, *current.NativeSessionID, domain.AgentLiveReplyPrompt(unanswered))
	if !ok {
		return false
	}
	environment := make(map[string]string, len(request.secrets.Environment))
	for name, value := range request.secrets.Environment {
		environment[name] = value
	}
	name := signInSecretNames[run.ProviderID]
	if request.signIn != "" && name != "" {
		// The sign-in the attempt may have refreshed, not the one it started with.
		environment[name] = request.signIn
	}
	// Never past the attempt's own time limit: it must not turn an attempt
	// that ended well into one that timed out.
	end := request.deadline.Add(-liveReplyMargin)
	if latest := time.Now().Add(liveReplyTimeout); latest.Before(end) {
		end = latest
	}
	if request.deadline.IsZero() || time.Until(end) < liveReplyMinimum {
		return false
	}
	replyCtx, cancel := context.WithDeadline(context.WithoutCancel(ctx), end)
	defer cancel()
	discarded := newAgentOutputBuffer(request.secrets.Redactions, func(string, int64, string, string, bool) error { return nil })
	observer := &AgentObserver{Authorize: func() error { return m.store.AuthorizeAgentRun(replyCtx, run.ID) }, Output: discarded.WriteDocker}
	_ = m.createEvent(replyCtx, machine, &run.ID, "agent", "agent_live_messages.reply_requested", map[string]any{"messages": unanswered})
	if _, err = runtime.ContinueAgent(replyCtx, machine, run, request.provider, request.checkout, environment, continuation, observer); err != nil {
		log.WithFields(log.Fields{"agent_run_id": run.ID, "event_type": "agent_live_messages.reply_request"}).WithError(err).Warn("the agent could not be asked again for its reply")
	}
	if environment[name] != "" {
		returnCtx, cancelReturn := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		if err = m.returnSignIn(returnCtx, machine, run, environment[name]); err != nil {
			log.WithField("agent_run_id", run.ID).WithError(err).Warn("refreshed agent sign-in could not be kept")
		}
		cancelReturn()
	}
	return true
}

// closeLiveInbox removes the run's inbox writer once the attempt ended.
func (m *Manager) closeLiveInbox(ctx context.Context, machine *domain.DevMachine, run *domain.DevMachineAgentRun) {
	runtime, ok := m.runtime.(liveInboxRuntime)
	if !ok {
		return
	}
	if err := runtime.CloseLiveInbox(ctx, machine, run); err != nil && ctx.Err() == nil {
		log.WithFields(log.Fields{"machine_id": machine.ID, "agent_run_id": run.ID}).WithError(err).Warn("live inbox could not be closed")
	}
}

func liveInboxName(machine *domain.DevMachine, run *domain.DevMachineAgentRun) string {
	return "kuayle-" + machine.RoutingKey + "-inbox-" + run.ID.String()
}

func liveInboxLabels(machine *domain.DevMachine, run *domain.DevMachineAgentRun) map[string]string {
	labels := machineLabels(machine)
	labels["com.kuayle.service"] = liveInboxService
	labels["com.kuayle.agent-run-id"] = run.ID.String()
	return labels
}

func ownedLiveInbox(labels map[string]string, machine *domain.DevMachine, run *domain.DevMachineAgentRun) bool {
	return ownedByMachine(labels, machine.ID) && labels["com.kuayle.service"] == liveInboxService && labels["com.kuayle.agent-run-id"] == run.ID.String()
}

// liveInboxMount is the agent container's read-only view of its inbox.
func liveInboxMount(machine *domain.DevMachine, run *domain.DevMachineAgentRun) mount.Mount {
	return mount.Mount{
		Type: mount.TypeVolume, Source: liveInboxName(machine, run), Target: domain.AgentLiveInboxPath, ReadOnly: true,
		VolumeOptions: &mount.VolumeOptions{NoCopy: true},
	}
}

// prepareLiveInbox creates the run's inbox volume and its writer container
// (created, never started: Docker copies into a stopped container's
// volumes). An existing writer must belong to the run.
func (r *DockerRuntime) prepareLiveInbox(ctx context.Context, machine *domain.DevMachine, run *domain.DevMachineAgentRun, imageRef string) error {
	name, labels := liveInboxName(machine, run), liveInboxLabels(machine, run)
	created, err := r.client.VolumeCreate(ctx, client.VolumeCreateOptions{Name: name, Driver: "local", Labels: labels})
	if err != nil {
		return fmt.Errorf("create live inbox volume: %w", err)
	}
	if !ownedLiveInbox(created.Volume.Labels, machine, run) {
		return fmt.Errorf("live inbox volume %s ownership does not match the run", name)
	}
	if inspection, err := r.client.ContainerInspect(ctx, name, client.ContainerInspectOptions{}); err == nil {
		if !ownedLiveInbox(inspection.Container.Config.Labels, machine, run) {
			return fmt.Errorf("live inbox writer %s ownership does not match the run", name)
		}
		return nil
	} else if !errdefs.IsNotFound(err) {
		return err
	}
	_, err = r.client.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config: &container.Config{Image: imageRef, Entrypoint: []string{"/bin/true"}, Labels: labels, NetworkDisabled: true},
		HostConfig: &container.HostConfig{
			NetworkMode: "none", ReadonlyRootfs: true, CapDrop: []string{"ALL"}, SecurityOpt: []string{"no-new-privileges=true"},
			LogConfig: boundedLogConfig(),
			Mounts:    []mount.Mount{{Type: mount.TypeVolume, Source: name, Target: "/inbox", VolumeOptions: &mount.VolumeOptions{NoCopy: true}}},
		},
		Name: name,
	})
	if err != nil {
		_, _ = r.client.VolumeRemove(context.WithoutCancel(ctx), name, client.VolumeRemoveOptions{})
		return fmt.Errorf("create live inbox writer: %w", err)
	}
	return nil
}

// WriteLiveInbox copies inbox files into the run's inbox as root-owned,
// world-readable regular files.
func (r *DockerRuntime) WriteLiveInbox(ctx context.Context, machine *domain.DevMachine, run *domain.DevMachineAgentRun, files []domain.AgentLiveInboxFile) error {
	if len(files) == 0 {
		return nil
	}
	name := liveInboxName(machine, run)
	inspection, err := r.client.ContainerInspect(ctx, name, client.ContainerInspectOptions{})
	if errdefs.IsNotFound(err) {
		return errLiveInboxUnavailable
	}
	if err != nil {
		return err
	}
	if !ownedLiveInbox(inspection.Container.Config.Labels, machine, run) {
		return fmt.Errorf("live inbox writer %s ownership does not match the run", name)
	}
	archive, err := liveInboxArchive(files, time.Now())
	if err != nil {
		return err
	}
	_, err = r.client.CopyToContainer(ctx, inspection.Container.ID, client.CopyToContainerOptions{DestinationPath: "/inbox", Content: bytes.NewReader(archive)})
	return err
}

func liveInboxArchive(files []domain.AgentLiveInboxFile, now time.Time) ([]byte, error) {
	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	for _, file := range files {
		if err := writer.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: file.Name, Mode: 0o644, Size: int64(len(file.Data)), ModTime: now, Format: tar.FormatPAX}); err != nil {
			return nil, err
		}
		if _, err := writer.Write(file.Data); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// CloseLiveInbox removes the run's writer and, once no container uses it,
// the inbox volume; an ended agent container keeps it until it is removed
// with the run (CancelAgent) or the machine (Teardown).
func (r *DockerRuntime) CloseLiveInbox(ctx context.Context, machine *domain.DevMachine, run *domain.DevMachineAgentRun) error {
	name := liveInboxName(machine, run)
	inspection, err := r.client.ContainerInspect(ctx, name, client.ContainerInspectOptions{})
	if err == nil {
		if !ownedLiveInbox(inspection.Container.Config.Labels, machine, run) {
			return fmt.Errorf("live inbox writer %s ownership does not match the run", name)
		}
		if _, err = r.client.ContainerRemove(ctx, inspection.Container.ID, client.ContainerRemoveOptions{Force: true}); err != nil && !errdefs.IsNotFound(err) {
			return err
		}
	} else if !errdefs.IsNotFound(err) {
		return err
	}
	if _, err = r.client.VolumeRemove(ctx, name, client.VolumeRemoveOptions{}); err != nil && !errdefs.IsNotFound(err) && !errdefs.IsConflict(err) {
		return err
	}
	return nil
}

// removeLiveInboxVolumes removes the inbox volumes matching filter, once the
// containers using them are gone. Only volumes of the machine are removed.
func (r *DockerRuntime) removeLiveInboxVolumes(ctx context.Context, filter string, machineID uuid.UUID) error {
	volumes, err := r.client.VolumeList(ctx, client.VolumeListOptions{Filters: make(client.Filters).Add("label", filter).Add("label", "com.kuayle.service="+liveInboxService)})
	if err != nil {
		return err
	}
	var failures []error
	for _, item := range volumes.Items {
		if !ownedByMachine(item.Labels, machineID) || item.Labels["com.kuayle.service"] != liveInboxService {
			failures = append(failures, fmt.Errorf("volume %s ownership does not match", item.Name))
			continue
		}
		if _, err := r.client.VolumeRemove(ctx, item.Name, client.VolumeRemoveOptions{Force: true}); err != nil && !errdefs.IsNotFound(err) {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

var (
	_ liveInboxRuntime = (*DockerRuntime)(nil)
	_ liveInboxStore   = (*repository.DevMachineRepository)(nil)
	_ liveReplyStore   = (*repository.DevMachineRepository)(nil)
)
