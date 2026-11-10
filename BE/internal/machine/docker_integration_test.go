package machine

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/containerd/errdefs"
	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

func TestDockerRuntimeLifecycle(t *testing.T) {
	if os.Getenv("KUAYLE_DOCKER_INTEGRATION") != "1" {
		t.Skip("set KUAYLE_DOCKER_INTEGRATION=1 to run Docker runtime tests")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	routingKey := strings.ReplaceAll(uuid.NewString(), "-", "")[:20]
	gatewayName := "kuayle-integration-gateway-" + routingKey
	runtime, err := NewDockerRuntime(DockerConfig{
		GatewayContainerName: gatewayName,
		IngestURL:            "https://example.invalid/api/dev-machine-ingest",
		EgressDenylist:       "169.254.169.254,metadata.google.internal",
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = runtime.client.Close() })
	require.NoError(t, runtime.Ping(ctx), "Docker data root must support XFS project quotas")

	gateway, err := runtime.client.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config: &container.Config{
			Image:      "kuayle/dev-machine-ide:0.1.0",
			Entrypoint: []string{"/bin/sleep"},
			Cmd:        []string{"180"},
		},
		Name: gatewayName,
	})
	require.NoError(t, err)
	_, err = runtime.client.ContainerStart(ctx, gateway.ID, client.ContainerStartOptions{})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = runtime.client.ContainerRemove(context.Background(), gateway.ID, client.ContainerRemoveOptions{Force: true})
	})

	machine := &domain.DevMachine{
		ID: uuid.New(), WorkspaceID: uuid.New(), RoutingKey: routingKey,
		Status:    domain.DevMachineStatusQueued,
		CPUMillis: 4000, MemoryMB: 4096, DiskGB: 20, PidsLimit: 1024,
		BaseBranch: "main", WorkingBranch: "kuayle/integration-test",
	}
	services := []domain.DevMachineService{
		integrationService(machine, "collector", "collector", "kuayle/dev-machine-collector:0.1.0", 8091),
		integrationService(machine, "egress", "egress", "kuayle/dev-machine-egress:0.1.0", 3128),
		integrationService(machine, "ide", "ide", "kuayle/dev-machine-ide:0.1.0", 8080),
		integrationService(machine, "terminal", "terminal", "kuayle/dev-machine-ide:0.1.0", 7681),
		integrationService(machine, "browser", "browser", "kuayle/dev-machine-browser:0.1.0", 3000),
	}
	secrets := map[string]map[string]string{
		"collector": {"KUAYLE_MACHINE_TOKEN": "integration-test-token", "KUAYLE_BROWSER_CDP_TOKEN": "integration-cdp-token"},
		"browser":   {"KUAYLE_BROWSER_CDP_TOKEN": "integration-cdp-token"},
	}

	networkName, volumeName, containers, err := runtime.Spawn(ctx, machine, services, secrets)
	require.NoError(t, err)
	volumeInspection, err := runtime.client.VolumeInspect(ctx, volumeName, client.VolumeInspectOptions{})
	require.NoError(t, err)
	require.Equal(t, "21474836480", volumeInspection.Volume.Options["size"])
	require.Equal(t, "21474836480", volumeInspection.Volume.Labels[workspaceQuotaLabel])
	machine.Status = domain.DevMachineStatusRunning
	require.Equal(t, containers["ide"], containers["terminal"])
	machine.DockerNetworkName = &networkName
	machine.WorkspaceVolumeName = &volumeName
	for index := range services {
		containerID := containers[services[index].ServiceKey]
		services[index].ContainerID = &containerID
		services[index].Status = "running"
	}
	cleaned := false
	t.Cleanup(func() {
		if !cleaned {
			_ = runtime.Teardown(context.Background(), machine, services)
		}
	})

	privateNetwork, err := runtime.client.NetworkInspect(ctx, networkName, client.NetworkInspectOptions{})
	require.NoError(t, err)
	require.True(t, privateNetwork.Network.Internal)
	require.Contains(t, privateNetwork.Network.Containers, gateway.ID)

	for _, service := range services {
		requireContainerRunning(t, ctx, runtime, service.ServiceKey, *service.ContainerID)
		inspection, err := runtime.client.ContainerInspect(ctx, *service.ContainerID, client.ContainerInspectOptions{})
		require.NoError(t, err)
		require.Equal(t, "1000:1000", inspection.Container.Config.User)
		require.True(t, inspection.Container.HostConfig.ReadonlyRootfs)
		require.False(t, inspection.Container.HostConfig.Privileged)
		require.Contains(t, inspection.Container.HostConfig.CapDrop, "ALL")
		require.Contains(t, inspection.Container.HostConfig.SecurityOpt, "no-new-privileges=true")
		require.Empty(t, inspection.Container.HostConfig.PortBindings)
		require.NotZero(t, inspection.Container.HostConfig.Memory)
		require.NotZero(t, inspection.Container.HostConfig.NanoCPUs)
		require.NotNil(t, inspection.Container.HostConfig.PidsLimit)
		require.Contains(t, inspection.Container.HostConfig.Tmpfs, "/run/kuayle-secrets")
		if service.ServiceType == "egress" {
			require.Contains(t, inspection.Container.NetworkSettings.Networks, egressNetworkName)
		}
	}
	for _, service := range services {
		if service.ServiceType == "ide" {
			_, err := runtime.execOutput(ctx, *service.ContainerID, []string{"/bin/sh", "-c", "touch /workspace/.kuayle-write-test && rm /workspace/.kuayle-write-test"})
			require.NoError(t, err)
			output, err := runtime.execOutput(ctx, *service.ContainerID, []string{"/bin/sh", "-c", "printf '#!/bin/sh\\necho home-exec-ok\\n' > /home/kuayle/.kuayle-exec-test && chmod +x /home/kuayle/.kuayle-exec-test && /home/kuayle/.kuayle-exec-test; status=$?; rm -f /home/kuayle/.kuayle-exec-test; exit $status"})
			require.NoError(t, err)
			require.Contains(t, output, "home-exec-ok")
			break
		}
	}
	terminalSession := &domain.DevMachineTerminalSession{RuntimeSessionName: "term-" + routingKey}
	_, err = runtime.execOutput(ctx, developerContainerID(services), []string{"tmux", "new-session", "-d", "-s", terminalSession.RuntimeSessionName, "sleep", "120"})
	require.NoError(t, err)
	require.NoError(t, runtime.TerminateTerminal(ctx, machine, services, terminalSession))
	require.NoError(t, runtime.TerminateTerminal(ctx, machine, services, terminalSession), "repeated terminal close must be idempotent")
	_, err = runtime.execOutput(ctx, developerContainerID(services), []string{"tmux", "has-session", "-t", terminalSession.RuntimeSessionName})
	require.Error(t, err)

	pausedTerminal := &domain.DevMachineTerminalSession{RuntimeSessionName: "term-paused-" + routingKey}
	_, err = runtime.execOutput(ctx, developerContainerID(services), []string{"tmux", "new-session", "-d", "-s", pausedTerminal.RuntimeSessionName, "sleep", "120"})
	require.NoError(t, err)

	require.NoError(t, runtime.Pause(ctx, machine, services))
	machine.Status = domain.DevMachineStatusPaused
	for _, service := range services {
		inspection, err := runtime.client.ContainerInspect(ctx, *service.ContainerID, client.ContainerInspectOptions{})
		require.NoError(t, err)
		require.True(t, inspection.Container.State.Paused)
	}
	require.NoError(t, runtime.TerminateTerminal(ctx, machine, services, pausedTerminal))
	pausedInspection, err := runtime.client.ContainerInspect(ctx, developerContainerID(services), client.ContainerInspectOptions{})
	require.NoError(t, err)
	require.True(t, pausedInspection.Container.State.Paused, "terminal close must restore the paused state")
	require.NoError(t, runtime.Start(ctx, machine, services, secrets))
	machine.Status = domain.DevMachineStatusRunning
	_, err = runtime.execOutput(ctx, developerContainerID(services), []string{"tmux", "has-session", "-t", pausedTerminal.RuntimeSessionName})
	require.Error(t, err)
	for _, service := range services {
		requireContainerRunning(t, ctx, runtime, service.ServiceKey, *service.ContainerID)
	}

	require.NoError(t, runtime.Stop(ctx, machine, services))
	machine.Status = domain.DevMachineStatusStopped
	require.NoError(t, runtime.Start(ctx, machine, services, secrets))
	machine.Status = domain.DevMachineStatusRunning
	for _, service := range services {
		requireContainerRunning(t, ctx, runtime, service.ServiceKey, *service.ContainerID)
	}

	requirePreviewRunsOutsideTheDeveloperContainer(t, ctx, runtime, machine, services, gateway.ID)

	command, err := json.Marshal([]string{"git", "config", "--global", "--get", "credential.helper"})
	require.NoError(t, err)
	run := &domain.DevMachineAgentRun{
		ID: uuid.New(), Mode: "task", CommandArgv: command,
	}
	provider := &domain.DevMachineAgentProvider{ImageRef: "kuayle/dev-machine-agent-claude:0.1.0"}
	execution, err := runtime.RunAgent(ctx, machine, run, provider, nil, map[string]string{
		"GITHUB_TOKEN": "integration-github-token",
	}, nil)
	require.NoError(t, err)
	require.Zero(t, execution.ExitCode)
	require.Contains(t, execution.Stdout, "/home/kuayle/.kuayle-git-credential")
	retriedExecution, err := runtime.RunAgent(ctx, machine, run, provider, nil, nil, nil)
	require.NoError(t, err)
	require.Equal(t, execution.ContainerID, retriedExecution.ContainerID)
	require.Equal(t, execution.Stdout, retriedExecution.Stdout)
	customCommand, err := json.Marshal([]string{"/usr/bin/env"})
	require.NoError(t, err)
	customExecution, err := runtime.RunAgent(ctx, machine, &domain.DevMachineAgentRun{
		ID: uuid.New(), Mode: "task", CommandArgv: customCommand,
	}, &domain.DevMachineAgentProvider{ImageRef: "kuayle/dev-machine-ide:0.1.0", IsCustom: true}, nil, map[string]string{
		"TEST_SECRET": "custom-secret-value", "GITHUB_TOKEN": "custom-github-token",
	}, nil)
	require.NoError(t, err)
	require.Zero(t, customExecution.ExitCode)
	require.Contains(t, customExecution.Stdout, "TEST_SECRET=custom-secret-value")
	// KUA-113: a stub agent in the machine prints what Claude Code printed in
	// production at its 5-hour limit and exits 1. The limit and its reset time
	// are recognised from the container's real output, so the task pauses.
	limitScript := `cat <<'STREAM'
{"type":"system","subtype":"init","session_id":"ae361499-0008-48b7-b8a7-d9df1193e4d3"}
{"type":"rate_limit_event","rate_limit_info":{"status":"rejected","resetsAt":1794283800,"rateLimitType":"five_hour","overageStatus":"rejected"}}
{"type":"assistant","message":{"content":[{"type":"text","text":"You've hit your 5-hour limit · resets 4:10am (UTC)"}]},"error":"rate_limit","api_error_status":429,"api_error":"usage_limit_reached"}
{"type":"result","subtype":"success","is_error":true,"result":"You've hit your 5-hour limit · resets 4:10am (UTC)"}
STREAM
exit 1`
	limitCommand, err := json.Marshal([]string{"/bin/sh", "-c", limitScript})
	require.NoError(t, err)
	limitedExecution, err := runtime.RunAgent(ctx, machine, &domain.DevMachineAgentRun{
		ID: uuid.New(), Mode: "task", CommandArgv: limitCommand,
	}, &domain.DevMachineAgentProvider{ImageRef: "kuayle/dev-machine-ide:0.1.0", IsCustom: true}, nil, nil, nil)
	require.NoError(t, err)
	require.Equal(t, 1, limitedExecution.ExitCode)
	usageLimit, limited := domain.DetectUsageLimit(limitedExecution.Stdout, limitedExecution.Stderr, "", time.Now())
	require.True(t, limited, "stdout: %s", limitedExecution.Stdout)
	require.Equal(t, "You've hit your 5-hour limit · resets 4:10am (UTC)", usageLimit.Message)
	require.NotNil(t, usageLimit.ResetsAt)
	require.True(t, usageLimit.ResetsAt.Equal(time.Unix(1794283800, 0)))
	pausedError, ok := domain.ParseUsageLimitError(usageLimit.ErrorMessage())
	require.True(t, ok)
	require.True(t, pausedError.ResetsAt.Equal(*usageLimit.ResetsAt))
	// The same stub through the real manager on this live machine: the run it
	// records is failed with the usage-limit error the task worker pauses on,
	// and the run log keeps the provider's message.
	managedMachine := *machine
	managedMachine.DesiredStatus, managedMachine.Generation = domain.DevMachineStatusRunning, 1
	limitedRunID, limitedTaskID := uuid.New(), uuid.New()
	managerStore := &managerStoreFake{
		machine:  &managedMachine,
		services: services,
		agentRun: &domain.DevMachineAgentRun{
			ID: limitedRunID, MachineID: machine.ID, WorkspaceID: machine.WorkspaceID, TaskID: &limitedTaskID,
			ProviderID: "opencode", Mode: "task", Status: domain.DevMachineAgentRunStatusQueued, MaxRuntimeSeconds: 60, CommandArgv: limitCommand,
		},
		provider: &domain.DevMachineAgentProvider{MachineID: machine.ID, ProviderID: "opencode", ImageRef: "kuayle/dev-machine-ide:0.1.0", Enabled: true, IsCustom: true},
	}
	manager := NewManager(managerStore, runtime, agent.NewRegistry(agent.NewOpenCodeProvider("kuayle/dev-machine-ide:0.1.0")), nil, make([]byte, 32), make([]byte, 32), "test")
	require.NoError(t, manager.runAgent(ctx, &managedMachine, &domain.DevMachineOperation{
		MachineID: machine.ID, WorkspaceID: machine.WorkspaceID, AgentRunID: &limitedRunID, Action: domain.DevMachineOpRunAgent, Generation: 1,
	}))
	require.NotNil(t, managerStore.completedRun)
	require.Equal(t, domain.DevMachineAgentRunStatusFailed, managerStore.completedRun.Status)
	require.NotNil(t, managerStore.completedRun.ErrorMessage)
	managedLimit, ok := domain.ParseUsageLimitError(*managerStore.completedRun.ErrorMessage)
	require.True(t, ok, *managerStore.completedRun.ErrorMessage)
	require.Equal(t, "You've hit your 5-hour limit · resets 4:10am (UTC)", managedLimit.Message)
	require.True(t, managedLimit.ResetsAt.Equal(time.Unix(1794283800, 0)))
	var managedLog strings.Builder
	for _, chunk := range managerStore.outputChunks {
		managedLog.WriteString(chunk.Content)
	}
	require.Contains(t, managedLog.String(), "You've hit your 5-hour limit")
	// A live message written before the attempt starts is readable at
	// /run/kuayle/inbox, which the agent cannot write.
	inboxCommand, err := json.Marshal([]string{"/bin/sh", "-c", `cat /run/kuayle/inbox/1.json; echo; touch /run/kuayle/inbox/2.json 2>/dev/null && echo inbox-writable || echo inbox-read-only`})
	require.NoError(t, err)
	inboxRun := &domain.DevMachineAgentRun{ID: uuid.New(), MachineID: machine.ID, WorkspaceID: machine.WorkspaceID, Mode: "task", CommandArgv: inboxCommand}
	require.NoError(t, runtime.prepareLiveInbox(ctx, machine, inboxRun, provider.ImageRef))
	seq := int64(1)
	inboxFile, err := domain.AgentLiveMessage{Seq: &seq, Sender: domain.AgentLiveSenderUser, Body: "Also add tests", CreatedAt: time.Now()}.InboxFile()
	require.NoError(t, err)
	require.NoError(t, runtime.WriteLiveInbox(ctx, machine, inboxRun, []domain.AgentLiveInboxFile{inboxFile}))
	inboxExecution, err := runtime.RunAgent(ctx, machine, inboxRun, provider, nil, nil, nil)
	require.NoError(t, err)
	require.Contains(t, inboxExecution.Stdout, "Also add tests")
	require.Contains(t, inboxExecution.Stdout, "inbox-read-only")
	require.NoError(t, runtime.CloseLiveInbox(ctx, machine, inboxRun))
	require.NoError(t, runtime.CancelAgent(ctx, inboxRun))
	_, err = runtime.client.VolumeInspect(ctx, liveInboxName(machine, inboxRun), client.VolumeInspectOptions{})
	require.True(t, errdefs.IsNotFound(err), "the run's inbox is removed with it")
	services = append(services, domain.DevMachineService{
		ServiceType: "agent", ServiceKey: "agent", ContainerID: &execution.ContainerID,
	}, domain.DevMachineService{
		ServiceType: "agent", ServiceKey: "custom-agent", ContainerID: &customExecution.ContainerID,
	})
	require.NoError(t, runtime.Stop(ctx, machine, services))
	machine.Status = domain.DevMachineStatusStopped
	require.NoError(t, runtime.Start(ctx, machine, services, secrets))
	machine.Status = domain.DevMachineStatusRunning
	for _, service := range services[:len(services)-2] {
		requireContainerRunning(t, ctx, runtime, service.ServiceKey, *service.ContainerID)
	}
	agentInspection, err := runtime.client.ContainerInspect(ctx, execution.ContainerID, client.ContainerInspectOptions{})
	require.NoError(t, err)
	require.False(t, agentInspection.Container.State.Running)
	customAgentInspection, err := runtime.client.ContainerInspect(ctx, customExecution.ContainerID, client.ContainerInspectOptions{})
	require.NoError(t, err)
	require.False(t, customAgentInspection.Container.State.Running)

	require.NoError(t, runtime.Teardown(ctx, machine, services))
	cleaned = true
}

func integrationService(machine *domain.DevMachine, serviceType, key, image string, port int) domain.DevMachineService {
	return domain.DevMachineService{
		ID: uuid.New(), MachineID: machine.ID, ServiceType: serviceType, ServiceKey: key,
		ContainerName: "kuayle-" + machine.RoutingKey + "-" + key, ImageRef: image,
		InternalHost: machine.RoutingKey + "-" + key, InternalPort: port,
	}
}

func requireContainerRunning(t *testing.T, ctx context.Context, runtime *DockerRuntime, serviceKey, containerID string) {
	t.Helper()
	var inspection container.InspectResponse
	var inspectErr error
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		raw, rawErr := runtime.client.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
		inspection, inspectErr = raw.Container, rawErr
		if inspectErr == nil && inspection.State.Running {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	logs, err := runtime.client.ContainerLogs(ctx, containerID, client.ContainerLogsOptions{ShowStdout: true, ShowStderr: true})
	if err != nil {
		t.Fatalf("%s container is not running: inspect=%v logs=%v", serviceKey, inspectErr, err)
	}
	defer logs.Close()
	output, _ := io.ReadAll(logs)
	t.Fatalf("%s container is not running (status=%s exit=%d error=%q path=%q args=%q): %s", serviceKey, inspection.State.Status, inspection.State.ExitCode, inspection.State.Error, inspection.Path, inspection.Args, output)
}

// requirePreviewRunsOutsideTheDeveloperContainer starts a task preview on a
// machine that allows sudo and whose developer container holds a
// developer-scoped secret and the owner's agent sign-ins. The preview command
// stands for code an agent wrote: it reports what it can reach. It must run
// in its own container, see neither the secret nor the sign-ins, be unable to
// use sudo, and be reachable from the gateway by the preview port's name
// while the developer container serves nothing on that port.
func requirePreviewRunsOutsideTheDeveloperContainer(t *testing.T, ctx context.Context, runtime *DockerRuntime, machine *domain.DevMachine, services []domain.DevMachineService, gatewayID string) {
	t.Helper()
	developer := developerContainerID(services)
	require.NoError(t, runtime.copySecrets(ctx, developer, map[string]string{"DEV_ONLY_FIXTURE": "developer-only-fixture"}))
	require.NoError(t, runtime.SyncAgentSignIns(ctx, services, map[string]string{"claude-code.json": "sign-in-fixture"}))
	_, err := runtime.execOutput(ctx, developer, []string{"/bin/sh", "-c", `test -s /run/kuayle-secrets/DEV_ONLY_FIXTURE && test -s "${HOME:-/home/kuayle}/.kuayle/agent-sign-ins/claude-code.json" && mkdir -p /workspace/tasks/preview-fixture`})
	require.NoError(t, err, "the developer container holds the secret and the sign-ins")

	// The policy allows sudo on this machine; only the developer container
	// would be relaxed by it.
	sudoMachine := *machine
	sudoMachine.SudoEnabled = true
	const port = 4100
	task := uuid.New()
	checkout := &domain.DevMachineCheckout{WorkspacePath: "/workspace/tasks/preview-fixture"}
	argv := []string{"/bin/sh", "-c", `{
  if [ -e /run/kuayle-secrets/DEV_ONLY_FIXTURE ] || [ -n "$(ls -A /run/kuayle-secrets 2>/dev/null)" ]; then echo scoped=visible; else echo scoped=absent; fi
  if [ -e "$HOME/.kuayle/agent-sign-ins" ] || [ -e "$HOME/.kuayle" ]; then echo sign-ins=visible; else echo sign-ins=absent; fi
  if sudo -n true 2>/dev/null; then echo sudo=works; else echo sudo=refused; fi
  if touch /usr/local/bin/kuayle-preview-probe 2>/dev/null; then echo rootfs=writable; else echo rootfs=read-only; fi
  echo "uid=$(id -u)"
  echo "port=$PORT host=$HOST"
  env | cut -d= -f1 | sort | tr '\n' ' '
} > report.txt
exec python3 -m http.server "$PORT" --bind "$HOST"`}
	require.NoError(t, runtime.StartPreview(ctx, &sudoMachine, services, checkout, task, argv, port, nil))
	name := previewContainerName(machine, task)
	inspection, err := runtime.client.ContainerInspect(ctx, name, client.ContainerInspectOptions{})
	require.NoError(t, err)
	require.NotEqual(t, developer, inspection.Container.ID, "the preview has its own container")
	require.True(t, inspection.Container.State.Running)
	require.True(t, inspection.Container.HostConfig.ReadonlyRootfs)
	require.False(t, inspection.Container.HostConfig.Privileged)
	require.Equal(t, []string{"ALL"}, []string(inspection.Container.HostConfig.CapDrop))
	require.Contains(t, inspection.Container.HostConfig.SecurityOpt, "no-new-privileges=true")
	require.NotZero(t, inspection.Container.HostConfig.Memory)
	require.NotNil(t, inspection.Container.HostConfig.PidsLimit)
	require.Len(t, inspection.Container.Mounts, 1, "only the workspace volume")
	require.Equal(t, "/workspace", inspection.Container.Mounts[0].Destination)

	// The gateway reaches the preview by the name the port router uses.
	host := domain.PreviewPortHost(machine.RoutingKey, port)
	var report string
	require.Eventually(t, func() bool {
		report, err = runtime.execOutput(ctx, gatewayID, []string{"curl", "-fsS", "--noproxy", "*", "--max-time", "5", "http://" + host + ":4100/report.txt"})
		return err == nil
	}, 30*time.Second, 500*time.Millisecond, "the preview answers on the machine network")
	for _, line := range []string{"scoped=absent", "sign-ins=absent", "sudo=refused", "rootfs=read-only", "uid=1000", "port=4100 host=0.0.0.0"} {
		require.Contains(t, report, line)
	}
	for _, forbidden := range []string{"DEV_ONLY_FIXTURE", "developer-only-fixture", "sign-in-fixture", "TOKEN", "API_KEY", "KUAYLE_SECRET", "KUAYLE_MACHINE", "DOCKER_"} {
		require.NotContains(t, report, forbidden)
	}
	_, err = runtime.execOutput(ctx, gatewayID, []string{"curl", "-fsS", "--noproxy", "*", "--max-time", "3", "http://" + machine.RoutingKey + "-ide:4100/report.txt"})
	require.Error(t, err, "the developer container serves nothing on the preview port")
	scan, err := runtime.ScanPorts(ctx, machine, services)
	require.NoError(t, err)
	require.Equal(t, []int{port}, scan.Preview)
	require.NotContains(t, scan.Listening, port)

	// Stopping the preview (the task finished or was cancelled) and stopping
	// the machine both remove the container.
	require.NoError(t, runtime.StopPreview(ctx, machine, services, task))
	_, err = runtime.client.ContainerInspect(ctx, name, client.ContainerInspectOptions{})
	require.True(t, errdefs.IsNotFound(err), "a stopped preview leaves no container")
	require.NoError(t, runtime.StartPreview(ctx, &sudoMachine, services, checkout, task, argv, port, nil))
	require.NoError(t, runtime.RemovePreviews(ctx, machine))
	_, err = runtime.client.ContainerInspect(ctx, name, client.ContainerInspectOptions{})
	require.True(t, errdefs.IsNotFound(err), "a stopping machine takes its previews with it")
	requireContainerRunning(t, ctx, runtime, "ide", developer)
}
