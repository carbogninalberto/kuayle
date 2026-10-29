package machine

import (
	"archive/tar"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

func liveInboxFixture() (*domain.DevMachine, *domain.DevMachineAgentRun) {
	machine := &domain.DevMachine{ID: uuid.New(), WorkspaceID: uuid.New(), RoutingKey: "0123456789abcdef0123", MemoryMB: 4096, PidsLimit: 512}
	return machine, &domain.DevMachineAgentRun{ID: uuid.New(), MachineID: machine.ID, WorkspaceID: machine.WorkspaceID}
}

// The agent sees its inbox read-only, at a path outside every worktree, from
// a volume of its own run.
func TestLiveInboxIsMountedReadOnlyOutsideTheWorktree(t *testing.T) {
	machine, run := liveInboxFixture()
	inbox := liveInboxMount(machine, run)
	require.Equal(t, mount.TypeVolume, inbox.Type)
	require.True(t, inbox.ReadOnly)
	require.Equal(t, "/run/kuayle/inbox", inbox.Target)
	require.Equal(t, "kuayle-"+machine.RoutingKey+"-inbox-"+run.ID.String(), inbox.Source)
	for _, existing := range (&DockerRuntime{}).secureHostConfig(machine, "agent", "network", "workspace", false).Mounts {
		require.False(t, strings.HasPrefix(inbox.Target, existing.Target+"/"), "the inbox is not inside %s", existing.Target)
		require.NotEqual(t, existing.Source, inbox.Source)
	}
	_, other := liveInboxFixture()
	require.NotEqual(t, inbox.Source, liveInboxMount(machine, other).Source, "each run has its own inbox")
}

type dockerAPIFake struct {
	t        *testing.T
	mu       sync.Mutex
	labels   map[string]string
	writer   bool
	created  *container.CreateRequest
	started  []string
	archives [][]byte
	removed  []string
	volumes  []string
	inUse    bool
}

func (f *dockerAPIFake) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	path := r.URL.Path
	switch {
	case r.Method == http.MethodPost && strings.HasSuffix(path, "/volumes/create"):
		var body map[string]any
		require.NoError(f.t, json.NewDecoder(r.Body).Decode(&body))
		_ = json.NewEncoder(w).Encode(map[string]any{"Name": body["Name"], "Labels": f.labels})
	case r.Method == http.MethodGet && strings.HasSuffix(path, "/json") && strings.Contains(path, "/containers/"):
		if !f.writer {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"No such container"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"Id": "writer-id", "Config": map[string]any{"Labels": f.labels}, "State": map[string]any{}})
	case r.Method == http.MethodPost && strings.HasSuffix(path, "/containers/create"):
		var body container.CreateRequest
		require.NoError(f.t, json.NewDecoder(r.Body).Decode(&body))
		f.created, f.writer = &body, true
		_ = json.NewEncoder(w).Encode(map[string]any{"Id": "writer-id"})
	case r.Method == http.MethodPost && strings.HasSuffix(path, "/start"):
		f.started = append(f.started, path)
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodPut && strings.HasSuffix(path, "/archive"):
		require.Equal(f.t, "/inbox", r.URL.Query().Get("path"))
		data, err := io.ReadAll(r.Body)
		require.NoError(f.t, err)
		f.archives = append(f.archives, data)
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodDelete && strings.Contains(path, "/volumes/"):
		if f.inUse {
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"message":"volume is in use"}`))
			return
		}
		f.volumes = append(f.volumes, path[strings.LastIndex(path, "/")+1:])
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodDelete && strings.Contains(path, "/containers/"):
		f.removed = append(f.removed, path[strings.LastIndex(path, "/")+1:])
		f.writer = false
		w.WriteHeader(http.StatusNoContent)
	default:
		http.NotFound(w, r)
	}
}

func (f *dockerAPIFake) runtime(t *testing.T) *DockerRuntime {
	server := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(server.Close)
	docker, err := client.New(client.WithHost(server.URL), client.WithAPIVersion("1.52"))
	require.NoError(t, err)
	t.Cleanup(func() { docker.Close() })
	return &DockerRuntime{client: docker}
}

// The writer holds the inbox read-write but is never started, has no network
// and cannot gain privileges; files arrive root-owned and world-readable.
func TestLiveInboxWriterIsNeverStartedAndWritesRootOwnedFiles(t *testing.T) {
	machine, run := liveInboxFixture()
	api := &dockerAPIFake{t: t, labels: liveInboxLabels(machine, run)}
	runtime := api.runtime(t)

	require.ErrorIs(t, runtime.WriteLiveInbox(context.Background(), machine, run, []domain.AgentLiveInboxFile{{Name: "1.json", Data: []byte(`{}`)}}), errLiveInboxUnavailable)
	require.NoError(t, runtime.prepareLiveInbox(context.Background(), machine, run, "kuayle/agent:test"))
	require.NotNil(t, api.created)
	require.Equal(t, container.NetworkMode("none"), api.created.HostConfig.NetworkMode)
	require.True(t, api.created.HostConfig.ReadonlyRootfs)
	require.Equal(t, []string{"ALL"}, api.created.HostConfig.CapDrop)
	require.Len(t, api.created.HostConfig.Mounts, 1)
	require.Equal(t, "/inbox", api.created.HostConfig.Mounts[0].Target)
	require.False(t, api.created.HostConfig.Mounts[0].ReadOnly)
	require.Equal(t, liveInboxName(machine, run), api.created.HostConfig.Mounts[0].Source)
	require.NoError(t, runtime.prepareLiveInbox(context.Background(), machine, run, "kuayle/agent:test"), "preparing again keeps the writer")

	require.NoError(t, runtime.WriteLiveInbox(context.Background(), machine, run, []domain.AgentLiveInboxFile{{Name: "1.json", Data: []byte(`{"seq":1}`)}, {Name: "2.json", Data: []byte(`{"seq":2}`)}}))
	require.Empty(t, api.started, "the writer is never started")
	require.Len(t, api.archives, 1)
	reader := tar.NewReader(strings.NewReader(string(api.archives[0])))
	names := []string{}
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		require.Equal(t, byte(tar.TypeReg), header.Typeflag)
		require.Equal(t, int64(0o644), header.Mode)
		require.Zero(t, header.Uid)
		require.Zero(t, header.Gid)
		names = append(names, header.Name)
	}
	require.Equal(t, []string{"1.json", "2.json"}, names)

	// A volume still used by the ended agent container stays until the run
	// or the machine is removed.
	api.inUse = true
	require.NoError(t, runtime.CloseLiveInbox(context.Background(), machine, run))
	require.Equal(t, []string{"writer-id"}, api.removed)
	api.inUse = false
	require.NoError(t, runtime.CloseLiveInbox(context.Background(), machine, run))
	require.Equal(t, []string{liveInboxName(machine, run)}, api.volumes)
}

func TestLiveInboxRefusesAWriterOfAnotherRun(t *testing.T) {
	machine, run := liveInboxFixture()
	_, other := liveInboxFixture()
	api := &dockerAPIFake{t: t, labels: liveInboxLabels(machine, other), writer: true}
	runtime := api.runtime(t)
	require.Error(t, runtime.WriteLiveInbox(context.Background(), machine, run, []domain.AgentLiveInboxFile{{Name: "1.json", Data: []byte(`{}`)}}))
	require.Error(t, runtime.CloseLiveInbox(context.Background(), machine, run))
	require.Empty(t, api.archives)
	require.Empty(t, api.removed)
}

type liveStoreFake struct {
	*managerStoreFake
	mu        sync.Mutex
	pending   []domain.AgentLiveMessage
	delivered []uuid.UUID
	recorded  []domain.AgentOutboxFile
	ticks     int
}

func (f *liveStoreFake) ListServices(context.Context, uuid.UUID, uuid.UUID) ([]domain.DevMachineService, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ticks++
	return append([]domain.DevMachineService(nil), f.services...), nil
}

func (f *liveStoreFake) CreateEvent(_ context.Context, event *domain.DevMachineEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, *event)
	return nil
}

func (f *liveStoreFake) RecordAgentOutbox(_ context.Context, _ uuid.UUID, files []domain.AgentOutboxFile) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recorded = append(f.recorded, files...)
	return len(files), nil
}

func (f *liveStoreFake) PendingLiveMessages(_ context.Context, run uuid.UUID) ([]domain.AgentLiveMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if run != f.agentRun.ID {
		return nil, nil
	}
	return append([]domain.AgentLiveMessage(nil), f.pending...), nil
}

func (f *liveStoreFake) MarkLiveMessagesDelivered(_ context.Context, _ uuid.UUID, ids []uuid.UUID) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.delivered = append(f.delivered, ids...)
	kept := f.pending[:0]
	for _, message := range f.pending {
		if !containsID(ids, message.ID) {
			kept = append(kept, message)
		}
	}
	f.pending = kept
	return len(ids), nil
}

func containsID(ids []uuid.UUID, id uuid.UUID) bool {
	for _, candidate := range ids {
		if candidate == id {
			return true
		}
	}
	return false
}

type liveRuntimeFake struct {
	*runtimeFake
	mu       sync.Mutex
	outboxes [][]domain.AgentOutboxFile
	written  []domain.AgentLiveInboxFile
	closed   int
}

func (f *liveRuntimeFake) TakeOutbox(context.Context, []domain.DevMachineService) ([]domain.AgentOutboxFile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.outboxes) == 0 {
		return nil, nil
	}
	files := f.outboxes[0]
	f.outboxes = f.outboxes[1:]
	return files, nil
}

func (f *liveRuntimeFake) PrepareOutbox(context.Context, []domain.DevMachineService, *domain.DevMachineCheckout) error {
	return nil
}

func (f *liveRuntimeFake) WriteLiveInbox(_ context.Context, _ *domain.DevMachine, _ *domain.DevMachineAgentRun, files []domain.AgentLiveInboxFile) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.written = append(f.written, files...)
	return nil
}

func (f *liveRuntimeFake) CloseLiveInbox(context.Context, *domain.DevMachine, *domain.DevMachineAgentRun) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed++
	return nil
}

// While an attempt runs, queued messages reach its inbox once and the outbox
// is collected repeatedly, not only when the attempt ends; the inbox writer
// is closed afterwards.
func TestRunningAttemptsReceiveLiveMessagesAndSendOutboxWhileRunning(t *testing.T) {
	machineID, workspaceID, runID := uuid.New(), uuid.New(), uuid.New()
	developer := "developer"
	seq := int64(1)
	message := domain.AgentLiveMessage{ID: uuid.New(), Seq: &seq, Sender: domain.AgentLiveSenderUser, Kind: domain.AgentLiveKindMessage, Body: "Also add tests", CreatedAt: time.Now()}
	store := &liveStoreFake{managerStoreFake: &managerStoreFake{
		machine: &domain.DevMachine{
			ID: machineID, WorkspaceID: workspaceID, RoutingKey: "0123456789abcdef0123",
			Status: domain.DevMachineStatusRunning, DesiredStatus: domain.DevMachineStatusRunning, Generation: 1,
		},
		services: []domain.DevMachineService{{ID: uuid.New(), ServiceType: "terminal", ServiceKey: "terminal", Status: "running", ContainerID: &developer}},
		agentRun: &domain.DevMachineAgentRun{ID: runID, MachineID: machineID, WorkspaceID: workspaceID, ProviderID: "opencode", Mode: "autonomous", Status: domain.DevMachineAgentRunStatusQueued, MaxRuntimeSeconds: 30},
		provider: &domain.DevMachineAgentProvider{MachineID: machineID, ProviderID: "opencode", ImageRef: "kuayle/opencode:test", Enabled: true},
	}, pending: []domain.AgentLiveMessage{message}}
	runtime := &liveRuntimeFake{runtimeFake: &runtimeFake{agentExecution: &AgentExecution{ContainerID: "agent", ExitCode: 0}}, outboxes: [][]domain.AgentOutboxFile{
		{{Worktree: "/workspace/tasks/eng-1", Name: "1.json", Data: []byte(`{"to":"orchestrator","body":"on it"}`)}},
		{{Worktree: "/workspace/tasks/eng-1", Name: "2.json", Data: []byte(`{"to":"orchestrator","kind":"question","body":"v1?"}`)}},
	}}
	runtime.onAgentOutput = func(AgentOutput) error {
		require.Eventually(t, func() bool {
			runtime.mu.Lock()
			defer runtime.mu.Unlock()
			store.mu.Lock()
			defer store.mu.Unlock()
			return len(runtime.written) == 1 && len(store.recorded) == 2 && store.ticks >= 3
		}, 5*time.Second, 5*time.Millisecond)
		return nil
	}
	manager := NewManager(store, runtime, agent.NewRegistry(agent.NewOpenCodeProvider("kuayle/opencode:test")), nil, make([]byte, 32), make([]byte, 32), "test")
	manager.liveInterval = 10 * time.Millisecond

	require.NoError(t, manager.runAgent(context.Background(), store.machine, &domain.DevMachineOperation{
		MachineID: machineID, WorkspaceID: workspaceID, AgentRunID: &runID, Action: domain.DevMachineOpRunAgent, Generation: 1,
	}))
	require.Len(t, runtime.written, 1, "a delivered message is written once")
	require.Equal(t, "1.json", runtime.written[0].Name)
	require.Contains(t, string(runtime.written[0].Data), "Also add tests")
	require.Equal(t, []uuid.UUID{message.ID}, store.delivered)
	require.Equal(t, 1, runtime.closed)
	eventTypes := []string{}
	for _, event := range store.events {
		eventTypes = append(eventTypes, event.EventType)
	}
	require.Contains(t, eventTypes, "agent_live_messages.delivered")
	require.Contains(t, eventTypes, "agent_messages.recorded")
	ticks := store.ticks
	time.Sleep(50 * time.Millisecond)
	store.mu.Lock()
	defer store.mu.Unlock()
	require.LessOrEqual(t, store.ticks, ticks+1, "the loop stops with the attempt")
}
