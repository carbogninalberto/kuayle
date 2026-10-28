package machine

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	log "github.com/sirupsen/logrus"
)

const (
	portScanInterval = 15 * time.Second
	// The developer container is scanned at most this often per batch.
	portScanBatch       = 200
	portScanParallelism = 4
	portScanTimeout     = 10 * time.Second
	// Agent requests are untrusted input: bounded in count and size.
	maxPortRequestsPerScan = 20
	maxPortRequestBytes    = 1024
	maxListeningPorts      = 64
	// Task previews use the preferred port when free, otherwise the first
	// free port in this range.
	previewPortFirst = 4100
	previewPortLast  = 4999
	// An unchanged state is still rewritten this often, since the developer
	// container's in-memory home is lost on restart.
	portStateRefresh = 2 * time.Minute
)

// PortRequest is one request file an agent left in the developer container:
// its file name and at most maxPortRequestBytes of its content.
type PortRequest struct {
	Name string
	Body []byte
}

// PortScan is the developer container's listening TCP ports, the agent
// requests taken (and removed) from it, and the ports published by containers
// of the machine's Docker daemon (listening in the docker service container).
type PortScan struct {
	Listening []int
	Requests  []PortRequest
	Docker    []int
}

// parsePortScan reads the scan script's output: /proc/net/tcp{,6} lines in
// LISTEN state (0A) and "R <name> <base64>" request lines. Anything else, and
// any request that does not decode, is ignored.
func parsePortScan(output string) PortScan {
	var scan PortScan
	seen := map[int]bool{}
	for _, line := range strings.Split(output, "\n") {
		if rest, ok := strings.CutPrefix(line, "R "); ok {
			if len(scan.Requests) >= maxPortRequestsPerScan {
				continue
			}
			name, encoded, _ := strings.Cut(rest, " ")
			body, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
			if err != nil || len(body) > maxPortRequestBytes || name == "" {
				continue
			}
			scan.Requests = append(scan.Requests, PortRequest{Name: name, Body: body})
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 || fields[3] != "0A" || !strings.HasSuffix(fields[0], ":") {
			continue
		}
		address, hexPort, ok := strings.Cut(fields[1], ":")
		if !ok || len(hexPort) != 4 || loopbackAddress(address) {
			continue
		}
		port, err := strconv.ParseUint(hexPort, 16, 16)
		if err != nil || port == 0 || seen[int(port)] || len(scan.Listening) >= maxListeningPorts {
			continue
		}
		seen[int(port)] = true
		scan.Listening = append(scan.Listening, int(port))
	}
	slices.Sort(scan.Listening)
	return scan
}

// loopbackAddress reports whether a /proc/net/tcp{,6} local address (hex,
// little-endian words) is loopback: 127.0.0.0/8, ::1 or ::ffff:127.x. The
// gateway reaches the container over its network address, never loopback.
func loopbackAddress(address string) bool {
	address = strings.ToUpper(address)
	switch len(address) {
	case 8:
		return strings.HasSuffix(address, "7F")
	case 32:
		return address == "00000000000000000000000001000000" ||
			(strings.HasPrefix(address, "0000000000000000FFFF0000") && strings.HasSuffix(address, "7F"))
	}
	return false
}

// portAction is a validated agent request.
type portAction struct {
	expose bool
	port   int
	label  *string
	// worktree is the task worktree the agent ran in, if it named one.
	worktree string
}

// parsePortRequest validates an agent request file: expose-<port>.json with
// {"port","label"?,"cwd"?}, or unexpose-<port>.
func parsePortRequest(request PortRequest) (portAction, error) {
	var action portAction
	digits, expose := strings.CutPrefix(request.Name, "expose-")
	if expose {
		var ok bool
		if digits, ok = strings.CutSuffix(digits, ".json"); !ok {
			return action, errors.New("invalid request name")
		}
	} else if digits, expose = strings.CutPrefix(request.Name, "unexpose-"); expose {
		expose = false
	} else {
		return action, errors.New("invalid request name")
	}
	port, ok := domain.ParseAppPortNumber(digits)
	if !ok {
		return action, errors.New("invalid request: ports are 1024-65535")
	}
	action.port = port
	if !strings.HasPrefix(request.Name, "expose-") {
		return action, nil
	}
	action.expose = true
	if len(request.Body) > maxPortRequestBytes {
		return action, errors.New("request is too large")
	}
	var body struct {
		Port  int     `json:"port"`
		Label *string `json:"label"`
		Cwd   string  `json:"cwd"`
	}
	decoder := json.NewDecoder(bytes.NewReader(request.Body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&body) != nil || decoder.Decode(new(any)) != io.EOF {
		return action, errors.New("invalid JSON request")
	}
	if body.Port != port {
		return action, fmt.Errorf("the request's port %d does not match its file name", body.Port)
	}
	if body.Label != nil && *body.Label != "" {
		if !domain.ValidPortLabel(*body.Label) {
			return action, errors.New("a label has at most 40 letters, digits, spaces, dots, dashes or underscores")
		}
		action.label = body.Label
	}
	action.worktree = taskWorktree(body.Cwd)
	return action, nil
}

// taskWorktree returns the /workspace/tasks/<name> directory containing path,
// or "" when path is not inside a task worktree.
func taskWorktree(path string) string {
	rest, ok := strings.CutPrefix(path, "/workspace/tasks/")
	if !ok || len(path) > 4096 {
		return ""
	}
	name, _, _ := strings.Cut(rest, "/")
	worktree := "/workspace/tasks/" + name
	if !validTerminalWorkingDirectory(worktree) {
		return ""
	}
	return worktree
}

// agentPortState is written to ~/.kuayle/ports/state.json in the developer
// container for kuayle-ports. Reserved lists the machine's own service ports,
// which kuayle-ports does not offer; Refused lists this scan's refused requests.
// Docker lists the listening ports published by the machine's Docker daemon.
type agentPortState struct {
	Ports     []agentPort   `json:"ports"`
	Listening []int         `json:"listening"`
	Docker    []int         `json:"docker"`
	Reserved  []int         `json:"reserved"`
	Refused   []portRefusal `json:"refused"`
	UpdatedAt *time.Time    `json:"updated_at"`
}

type agentPort struct {
	Port   int    `json:"port"`
	Label  string `json:"label,omitempty"`
	URL    string `json:"url,omitempty"`
	Source string `json:"source"`
}

type portRefusal struct {
	Port   int    `json:"port"`
	Reason string `json:"reason"`
}

type publishedPortState struct {
	digest  [32]byte
	written time.Time
}

// SetMachineDomain sets the domain of app port URLs given to agents.
func (m *Manager) SetMachineDomain(machineDomain string) {
	m.machineDomain = strings.Trim(strings.ToLower(machineDomain), ".")
}

func (m *Manager) appPortURL(machine *domain.DevMachine, port int) string {
	if m.machineDomain == "" {
		return ""
	}
	return "https://" + machine.RoutingKey + "-" + strconv.Itoa(port) + "." + m.machineDomain
}

// developerRunning reports whether the machine's developer container serves.
func developerRunning(services []domain.DevMachineService) bool {
	for _, service := range services {
		if isDeveloperService(service.ServiceType) && service.AgentRunID == nil && service.Status == "running" && service.ContainerID != nil {
			return true
		}
	}
	return false
}

func servicePortReason(services []domain.DevMachineService, port int) string {
	for _, service := range services {
		if service.InternalPort == port {
			return fmt.Sprintf("port %d belongs to the machine's %s service", port, service.ServiceType)
		}
	}
	return ""
}

func servicePorts(services []domain.DevMachineService) []int {
	ports := []int{}
	for _, service := range services {
		if service.InternalPort > 0 && !slices.Contains(ports, service.InternalPort) {
			ports = append(ports, service.InternalPort)
		}
	}
	slices.Sort(ports)
	return ports
}

// schedulePortScan starts one background scan of running machines unless the
// previous one is still running.
func (m *Manager) schedulePortScan(ctx context.Context) {
	if !m.portScanning.CompareAndSwap(false, true) {
		return
	}
	m.operations.Add(1)
	go func() {
		defer m.operations.Done()
		defer m.portScanning.Store(false)
		if err := m.scanPorts(ctx); err != nil && ctx.Err() == nil {
			log.WithError(err).Warn("dev machine port scan failed")
		}
	}()
}

func (m *Manager) scanPorts(ctx context.Context) error {
	machines, err := m.store.ListPortScanMachines(ctx, portScanBatch)
	if err != nil {
		return err
	}
	slots := make(chan struct{}, portScanParallelism)
	var wait sync.WaitGroup
	for i := range machines {
		machine := machines[i]
		slots <- struct{}{}
		wait.Add(1)
		go func() {
			defer func() { <-slots; wait.Done() }()
			scanCtx, cancel := context.WithTimeout(ctx, portScanTimeout)
			defer cancel()
			services, err := m.store.ListServices(scanCtx, machine.WorkspaceID, machine.ID)
			if err == nil {
				err = m.syncMachinePorts(scanCtx, &machine, services)
			}
			if err != nil && scanCtx.Err() == nil {
				log.WithFields(log.Fields{"machine_id": machine.ID, "event_type": "machine.port_scan"}).WithError(err).Debug("port scan skipped")
			}
		}()
	}
	wait.Wait()
	return nil
}

// syncMachinePorts scans the developer container, applies the agent requests
// it found, records the listening ports and publishes the result to agents.
// It also collects the agents' outbox messages.
func (m *Manager) syncMachinePorts(ctx context.Context, machine *domain.DevMachine, services []domain.DevMachineService) error {
	_, err := m.syncPorts(ctx, machine, services)
	m.collectOutbox(ctx, machine, services)
	return err
}

func (m *Manager) syncPorts(ctx context.Context, machine *domain.DevMachine, services []domain.DevMachineService) ([]int, error) {
	if !developerRunning(services) {
		return nil, nil
	}
	scan, err := m.runtime.ScanPorts(ctx, services)
	if err != nil {
		return nil, err
	}
	listening := make([]int, 0, len(scan.Listening)+len(scan.Docker))
	docker := make([]int, 0, len(scan.Docker))
	for _, port := range scan.Docker {
		if servicePortReason(services, port) == "" && !slices.Contains(docker, port) {
			docker = append(docker, port)
		}
	}
	for _, port := range append(append([]int{}, scan.Listening...), docker...) {
		if servicePortReason(services, port) == "" && !slices.Contains(listening, port) {
			listening = append(listening, port)
		}
	}
	slices.Sort(listening)
	slices.Sort(docker)
	if err = m.store.SetListeningPorts(ctx, machine.ID, listening, docker); err != nil {
		return nil, err
	}
	refused := []portRefusal{}
	for i, request := range scan.Requests {
		if i >= maxPortRequestsPerScan {
			break
		}
		if reason := m.applyPortRequest(ctx, machine, services, request); reason != "" {
			action, _ := parsePortRequest(request)
			port := action.port
			if port == 0 {
				port = requestPort(request.Name)
			}
			refused = append(refused, portRefusal{Port: port, Reason: reason})
		}
	}
	ports, err := m.store.MachinePorts(ctx, machine.ID)
	if err != nil {
		return nil, err
	}
	state := agentPortState{Ports: make([]agentPort, 0, len(ports)), Listening: listening, Docker: docker, Reserved: servicePorts(services), Refused: refused}
	for _, port := range ports {
		item := agentPort{Port: port.Port, URL: m.appPortURL(machine, port.Port), Source: port.Source}
		if port.Label != nil {
			item.Label = *port.Label
		}
		state.Ports = append(state.Ports, item)
	}
	if err = m.publishPortState(ctx, machine.ID, services, state, len(scan.Requests) > 0); err != nil {
		return nil, err
	}
	return listening, nil
}

// requestPort extracts the port named by a request file, for refusals.
func requestPort(name string) int {
	name = strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(name, "un"), "expose-"), ".json")
	port, _ := strconv.Atoi(name)
	if port < 0 || port > 65535 {
		return 0
	}
	return port
}

// applyPortRequest applies one agent request and returns why it was refused,
// or "". Agent ports belong to the machine creator: only they are admitted.
func (m *Manager) applyPortRequest(ctx context.Context, machine *domain.DevMachine, services []domain.DevMachineService, request PortRequest) string {
	action, err := parsePortRequest(request)
	if err != nil {
		return err.Error()
	}
	if !action.expose {
		removed, err := m.store.DeleteAgentPort(ctx, machine.ID, action.port)
		if err != nil {
			return "the port could not be removed"
		}
		if !removed {
			return "only ports exposed by agents can be removed here; the owner removes others from the machine page"
		}
		return ""
	}
	if reason := servicePortReason(services, action.port); reason != "" {
		return reason
	}
	if machine.CreatedByUserID == nil {
		return "the machine has no owner"
	}
	var task *uuid.UUID
	if action.worktree != "" {
		if task, err = m.store.TaskForWorktree(ctx, machine.ID, action.worktree); err != nil {
			task = nil
		}
	}
	port := &domain.DevMachinePort{WorkspaceID: machine.WorkspaceID, MachineID: machine.ID, Port: action.port, Label: action.label,
		Source: domain.DevMachinePortSourceAgent, AgentTaskID: task, CreatedByUserID: *machine.CreatedByUserID}
	switch err = m.store.CreatePort(ctx, port); {
	case err == nil, errors.Is(err, repository.ErrPortExists):
		return ""
	default:
		log.WithFields(log.Fields{"machine_id": machine.ID, "event_type": "machine.port_request"}).WithError(err).Warn("agent port request failed")
		return "the port could not be exposed"
	}
}

// publishPortState writes the state for kuayle-ports when it changed, after
// requests, or when the last write is old.
func (m *Manager) publishPortState(ctx context.Context, machineID uuid.UUID, services []domain.DevMachineService, state agentPortState, force bool) error {
	state.UpdatedAt = nil
	content, err := json.Marshal(state)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(content)
	now := time.Now().UTC()
	m.portStateMu.Lock()
	if m.portStates == nil {
		m.portStates = map[uuid.UUID]publishedPortState{}
	}
	previous, ok := m.portStates[machineID]
	m.portStateMu.Unlock()
	if !force && ok && previous.digest == digest && now.Sub(previous.written) < portStateRefresh {
		return nil
	}
	state.UpdatedAt = &now
	payload, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if err = m.runtime.WritePortState(ctx, services, payload); err != nil {
		return err
	}
	m.portStateMu.Lock()
	m.portStates[machineID] = publishedPortState{digest: digest, written: now}
	m.portStateMu.Unlock()
	return nil
}

// choosePreviewPort returns the task's own port, else its preferred port when
// free, else the first free port of the preview range. Free means not used by
// a service, not listening and not already exposed.
func choosePreviewPort(preferred *int, own *domain.DevMachinePort, services []domain.DevMachineService, listening []int, ports []domain.DevMachinePort) (int, bool) {
	if own != nil {
		return own.Port, true
	}
	free := func(port int) bool {
		if !domain.ValidAppPort(port) || servicePortReason(services, port) != "" || slices.Contains(listening, port) {
			return false
		}
		for _, exposed := range ports {
			if exposed.Port == port {
				return false
			}
		}
		return true
	}
	if preferred != nil && free(*preferred) {
		return *preferred, true
	}
	for port := previewPortFirst; port <= previewPortLast; port++ {
		if free(port) {
			return port, true
		}
	}
	return 0, false
}

// startPreview runs a task's preview command in its worktree on the developer
// container, on its own port and app port. Other tasks' previews keep running.
// Failures are recorded on the task; only infrastructure errors are retried.
func (m *Manager) startPreview(ctx context.Context, machine *domain.DevMachine, op *domain.DevMachineOperation) error {
	if op.AgentTaskID == nil {
		return nil
	}
	target, err := m.store.PreviewTarget(ctx, *op.AgentTaskID)
	if err != nil || target == nil {
		return err
	}
	task := target.Task
	if task.PreviewState == nil || *task.PreviewState != domain.AgentTaskPreviewStarting {
		return nil
	}
	fail := func(message string) error {
		return m.store.SetTaskPreview(ctx, task.ID, domain.AgentTaskPreviewFailed, &message, nil)
	}
	if machine.Status != domain.DevMachineStatusRunning || machine.DesiredStatus != domain.DevMachineStatusRunning || task.MachineID == nil || *task.MachineID != machine.ID {
		return fail("The machine is not running")
	}
	argv := target.PreviewCommand()
	if len(argv) == 0 || domain.ValidateTestCommand(argv) != nil {
		return fail("The preview command is invalid")
	}
	checkout, err := m.store.GetCheckoutInternal(ctx, task.CheckoutID)
	if err != nil {
		return err
	}
	if checkout == nil || checkout.MachineID != machine.ID || checkout.Status != "ready" {
		return fail("The task's worktree is not ready on this machine")
	}
	services, err := m.store.ListServices(ctx, machine.WorkspaceID, machine.ID)
	if err != nil {
		return err
	}
	if !developerRunning(services) {
		return fail("The machine's developer container is not running")
	}
	listening, err := m.syncPorts(ctx, machine, services)
	if err != nil {
		return err
	}
	ports, err := m.store.MachinePorts(ctx, machine.ID)
	if err != nil {
		return err
	}
	number, ok := choosePreviewPort(task.PreviewPort, target.Port, services, listening, ports)
	if !ok {
		return fail("No free port is available for the preview")
	}
	port := target.Port
	if port == nil {
		port = &domain.DevMachinePort{WorkspaceID: machine.WorkspaceID, MachineID: machine.ID, Port: number, Label: domain.PortLabel(target.Identifier),
			Source: domain.DevMachinePortSourceTask, AgentTaskID: &task.ID, CreatedByUserID: task.CreatorID}
		switch err = m.store.CreatePort(ctx, port); {
		case errors.Is(err, repository.ErrPortExists):
			return fail(fmt.Sprintf("Port %d was exposed meanwhile; start the preview again", number))
		case err != nil:
			return err
		}
	}
	if err = m.runtime.StartPreview(ctx, machine, services, checkout, task.ID, argv, number); err != nil {
		if stopErr := m.store.StopTaskPreview(ctx, task.ID); stopErr != nil {
			return stopErr
		}
		return fail("The preview command could not be started: " + safeError(err))
	}
	if err = m.store.SetTaskPreview(ctx, task.ID, domain.AgentTaskPreviewRunning, nil, port); err != nil {
		return err
	}
	// Agents see the new URL at once; the port is reported listening by the
	// next scan, when the application has started.
	if _, err = m.syncPorts(ctx, machine, services); err != nil {
		log.WithFields(log.Fields{"machine_id": machine.ID, "event_type": "machine.port_scan"}).WithError(err).Debug("post-preview port scan failed")
	}
	return nil
}

// stopPreview kills one task's preview process group, removes its app port and
// records the preview as stopped. Other tasks' previews keep running.
func (m *Manager) stopPreview(ctx context.Context, machine *domain.DevMachine, op *domain.DevMachineOperation) error {
	if op.AgentTaskID == nil {
		return nil
	}
	var services []domain.DevMachineService
	if machine.Status == domain.DevMachineStatusRunning {
		var err error
		if services, err = m.store.ListServices(ctx, machine.WorkspaceID, machine.ID); err != nil {
			return err
		}
		if developerRunning(services) {
			if err = m.runtime.StopPreview(ctx, services, *op.AgentTaskID); err != nil {
				return err
			}
		}
	}
	if err := m.store.StopTaskPreview(ctx, *op.AgentTaskID); err != nil {
		return err
	}
	if len(services) > 0 {
		if _, err := m.syncPorts(ctx, machine, services); err != nil {
			log.WithFields(log.Fields{"machine_id": machine.ID, "event_type": "machine.port_scan"}).WithError(err).Debug("post-preview port scan failed")
		}
	}
	return nil
}

// stalePreview records a start that the machine outlived.
func (m *Manager) stalePreview(ctx context.Context, op *domain.DevMachineOperation) error {
	if op.AgentTaskID == nil {
		return nil
	}
	target, err := m.store.PreviewTarget(ctx, *op.AgentTaskID)
	if err != nil || target == nil || target.Task.PreviewState == nil || *target.Task.PreviewState != domain.AgentTaskPreviewStarting {
		return err
	}
	message := "The machine restarted before the preview started; start it again"
	return m.store.SetTaskPreview(ctx, target.Task.ID, domain.AgentTaskPreviewFailed, &message, nil)
}

// previewsEnded records that the machine's preview processes ended with its
// containers. Owner and agent ports are kept.
func (m *Manager) previewsEnded(ctx context.Context, machine *domain.DevMachine) {
	if err := m.store.StopMachinePreviews(ctx, machine.ID); err != nil {
		log.WithFields(log.Fields{"machine_id": machine.ID, "event_type": "machine.preview_stop"}).WithError(err).Warn("failed to record stopped previews")
	}
	m.portStateMu.Lock()
	delete(m.portStates, machine.ID)
	m.portStateMu.Unlock()
}
