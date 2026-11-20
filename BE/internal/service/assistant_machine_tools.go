package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
)

// The machine tools use the task repository and Dev Machine service to read
// usage and manage previews and app ports.

// assistantMachineTasks resolves the issue's newest coding task. The
// repository checks task ownership for preview operations.
type assistantMachineTasks interface {
	FollowUpTarget(ctx context.Context, ws, issue uuid.UUID) (*repository.FollowUpTarget, error)
	Get(ctx context.Context, ws, id uuid.UUID) (*domain.AgentTask, error)
	RequestPreview(ctx context.Context, ws, id uuid.UUID, start bool) (*domain.AgentTask, error)
}

// assistantMachinePorts is the Dev Machine service's app port API; it admits
// only the machine's owner, as the gateway does.
type assistantMachinePorts interface {
	GetForUser(ctx context.Context, ws, machine, user uuid.UUID) (*domain.DevMachine, error)
	ListPorts(ctx context.Context, ws, machine, user uuid.UUID, admin bool) (*dto.AppPortList, error)
	CreatePort(ctx context.Context, ws, machine, user uuid.UUID, number int, label *string) (*dto.AppPort, error)
	UpdatePort(ctx context.Context, ws, machine, port, user uuid.UUID, label *string) (*dto.AppPort, error)
	ReservedPort(ctx context.Context, ws, machine uuid.UUID, number int) error
	AppPortHost(machine *domain.DevMachine, port int) string
}

type assistantMachineService interface {
	assistantMachinePorts
	AgentLogTail(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, *uuid.UUID, int) (domain.MachineLogResult, error)
	GetResourceUsageForUser(ctx context.Context, ws, machine, user uuid.UUID) (*domain.DevMachine, *domain.DevMachineUsage, error)
}

// SetMachines enables the preview, port and resource usage tools.
func (t *AssistantTools) SetMachines(tasks assistantMachineTasks, machines assistantMachineService) {
	t.machineTasks, t.machines = tasks, machines
}

// SetMachines enables approving start_preview and expose_port requests.
func (s *AssistantApprovals) SetMachines(tasks assistantMachineTasks, ports assistantMachinePorts) {
	s.machineTasks, s.machines = tasks, ports
}

const getMachineUsageTool = "get_machine_usage"

var assistantMachineToolCatalog = []assistantTool{
	machineLogsTool,
	{name: "start_preview", machines: true, approval: true, description: "Ask the user to approve starting the preview of an issue's newest coding task: its configured preview command runs in the task's worktree on its machine, on its own app port. Only the task's creator can start it. After approval the user opens it from the approval card; call list_ports for the link.", parameters: `{"type":"object","properties":{"identifier":{"type":"string","maxLength":32}},"required":["identifier"],"additionalProperties":false}`},
	{name: "stop_preview", machines: true, description: "Stop the running preview of an issue's newest coding task. Only call this when the user asked to stop it.", parameters: `{"type":"object","properties":{"identifier":{"type":"string","maxLength":32}},"required":["identifier"],"additionalProperties":false}`},
	{name: "expose_port", machines: true, approval: true, description: "Ask the user to approve exposing a TCP port (1024-65535) of the machine of an issue's newest coding task on its own authenticated gateway link. A port the machine already exposes is reused. Only the machine's owner can expose ports or open the link. Only call this when the user asked to expose or open a port.", parameters: `{"type":"object","properties":{"identifier":{"type":"string","maxLength":32},"port":{"type":"integer","minimum":1024,"maximum":65535},"label":{"type":"string","maxLength":40}},"required":["identifier","port"],"additionalProperties":false}`},
	{name: "list_ports", machines: true, description: "List the exposed app ports of the machine of an issue's newest coding task, with their gateway links, whether each is listening, the ports detected as listening and the task's preview state. Links open only for the machine's owner signed in to Kuayle.", parameters: `{"type":"object","properties":{"identifier":{"type":"string","maxLength":32}},"required":["identifier"],"additionalProperties":false}`},
	{name: getMachineUsageTool, machines: true, description: "Read a dev machine's latest and peak CPU, memory and disk usage from retained samples, with its configured limits and the latest sample time. Use for questions about resource usage, for example after 'no space left on device'. Give identifier (the machine of an issue's newest coding task, for its creator or a workspace admin) or machine_id (one of your own machines), not both. Include the limits and sample time in your answer. CPU usage is percent of one vCPU (100% is 1000 millicores). Memory and disk usage are in bytes, with memory_mb and disk_gb limits in MiB and GiB.", parameters: `{"type":"object","properties":{"identifier":{"type":"string","maxLength":32},"machine_id":{"type":"string","maxLength":36}},"additionalProperties":false}`},
}

// newestMachineTask resolves a visible issue and its newest coding task.
func (t *AssistantTools) newestMachineTask(ctx context.Context, ws uuid.UUID, identifier string) (*domain.Issue, *repository.FollowUpTarget, error) {
	issue, err := t.issues.GetByIdentifier(ctx, ws, strings.ToUpper(strings.TrimSpace(identifier)))
	if err != nil || issue == nil {
		return nil, nil, toolFailure("issue not found")
	}
	target, err := t.machineTasks.FollowUpTarget(ctx, ws, issue.ID)
	if errors.Is(err, repository.ErrNoMergeTarget) {
		return nil, nil, toolFailure("this issue has no coding task")
	}
	if err != nil {
		return nil, nil, err
	}
	return issue, target, nil
}

// machineTask resolves the issue with the user's own visibility, then its
// newest coding task as the user. Missing and hidden issues look the same.
func (t *AssistantTools) machineTask(ctx context.Context, ws, user uuid.UUID, identifier string) (*domain.Issue, *domain.AgentTask, error) {
	member, err := t.members.GetMember(ctx, ws, user)
	if err != nil || member == nil || member.Role == domain.RoleGuest {
		return nil, nil, toolFailure("you no longer have access to this workspace")
	}
	if t.machineTasks == nil || t.machines == nil {
		return nil, nil, toolFailure("unknown tool")
	}
	issue, target, err := t.newestMachineTask(ctx, ws, identifier)
	if err != nil {
		return nil, nil, err
	}
	if target.CreatorID != user {
		return nil, nil, toolFailure("only the task's creator can access its machine")
	}
	task, err := t.machineTasks.Get(ctx, ws, target.TaskID)
	if errors.Is(err, repository.ErrConversationNotFound) || errors.Is(err, repository.ErrAgentTaskUnauthorized) {
		return nil, nil, toolFailure("only the task's creator can access its machine")
	}
	if err != nil {
		return nil, nil, err
	}
	return issue, task, nil
}

// taskMachine returns the task's machine when the user owns it.
func (t *AssistantTools) taskMachine(ctx context.Context, ws, user uuid.UUID, task *domain.AgentTask) (*domain.DevMachine, error) {
	if task.MachineID == nil {
		return nil, toolFailure("the issue's newest coding task has no machine yet")
	}
	machine, err := t.machines.GetForUser(ctx, ws, *task.MachineID, user)
	if err != nil {
		return nil, machineToolFailure(err)
	}
	return machine, nil
}

// machineToolFailure turns Dev Machine errors into user-safe tool failures.
func machineToolFailure(err error) error {
	switch {
	case errors.Is(err, ErrDemoMachineForbidden):
		return toolFailure("%s", demoMachineRefusal)
	case errors.Is(err, ErrMachineNotFound):
		return toolFailure("only the machine's owner can manage its ports")
	case errors.Is(err, ErrDevMachinesDisabled):
		return toolFailure("Dev Machines are not enabled for this workspace")
	case errors.Is(err, ErrPortReserved), errors.Is(err, ErrInvalidMachineInput):
		// The detail follows the sentinel: "<sentinel>: <detail>".
		message := err.Error()
		if _, detail, found := strings.Cut(message, ": "); found {
			message = detail
		}
		return toolFailure("%s", message)
	}
	return err
}

// preparePreview binds a start_preview request to the issue's newest task,
// its machine and its preview command.
func (t *AssistantTools) preparePreview(ctx context.Context, ws, user uuid.UUID, arguments string) (uuid.UUID, domain.PreviewPayload, error) {
	var input struct {
		Identifier string `json:"identifier"`
	}
	if err := decodeToolInput(arguments, &input); err != nil {
		return uuid.Nil, domain.PreviewPayload{}, err
	}
	issue, task, err := t.machineTask(ctx, ws, user, input.Identifier)
	if err != nil {
		return uuid.Nil, domain.PreviewPayload{}, err
	}
	command := previewCommand(*task)
	if len(command) == 0 {
		return uuid.Nil, domain.PreviewPayload{}, toolFailure("the task has no preview command; set one in the development settings of the issue, its project, team or workspace")
	}
	machine, err := t.taskMachine(ctx, ws, user, task)
	if err != nil {
		return uuid.Nil, domain.PreviewPayload{}, err
	}
	return issue.ID, domain.PreviewPayload{
		Identifier: issue.Identifier, Title: issue.Title, TaskID: task.ID, MachineID: machine.ID, Machine: machine.Name,
		Command: command, Port: task.PreviewPort, Audience: domain.AppPortAudienceOwner,
	}, nil
}

// preparePort binds an expose_port request to the task's machine, the port and
// its gateway host, after the same checks CreatePort makes.
func (t *AssistantTools) preparePort(ctx context.Context, ws, user uuid.UUID, arguments string) (uuid.UUID, domain.ExposePortPayload, error) {
	var input struct {
		Identifier string  `json:"identifier"`
		Port       int     `json:"port"`
		Label      *string `json:"label"`
	}
	if err := decodeToolInput(arguments, &input); err != nil {
		return uuid.Nil, domain.ExposePortPayload{}, err
	}
	issue, task, err := t.machineTask(ctx, ws, user, input.Identifier)
	if err != nil {
		return uuid.Nil, domain.ExposePortPayload{}, err
	}
	machine, err := t.taskMachine(ctx, ws, user, task)
	if err != nil {
		return uuid.Nil, domain.ExposePortPayload{}, err
	}
	if !domain.ValidAppPort(input.Port) {
		return uuid.Nil, domain.ExposePortPayload{}, toolFailure("the port must be between 1024 and 65535")
	}
	label, err := normalizePortLabel(input.Label)
	if err != nil {
		return uuid.Nil, domain.ExposePortPayload{}, machineToolFailure(err)
	}
	list, err := t.machines.ListPorts(ctx, ws, machine.ID, user, false)
	if err != nil {
		return uuid.Nil, domain.ExposePortPayload{}, machineToolFailure(err)
	}
	existing := false
	for _, port := range list.Ports {
		existing = existing || port.Port == input.Port
	}
	if !existing {
		if err = t.machines.ReservedPort(ctx, ws, machine.ID, input.Port); err != nil {
			return uuid.Nil, domain.ExposePortPayload{}, machineToolFailure(err)
		}
	}
	return issue.ID, domain.ExposePortPayload{
		Identifier: issue.Identifier, Title: issue.Title, TaskID: task.ID, MachineID: machine.ID, Machine: machine.Name,
		Port: input.Port, Label: label, Host: t.machines.AppPortHost(machine, input.Port), Existing: existing, Audience: domain.AppPortAudienceOwner,
	}, nil
}

// runMachineTool runs the machine tools that need no approval.
func (t *AssistantTools) runMachineTool(ctx context.Context, ws, user uuid.UUID, name, arguments string) (any, *uuid.UUID, error) {
	// As on the Dev Machine routes, the demo rule also covers reading ports.
	if err := t.machineAccess.Require(user); err != nil {
		return nil, nil, machineToolFailure(err)
	}
	var input struct {
		Identifier string `json:"identifier"`
	}
	if err := decodeToolInput(arguments, &input); err != nil {
		return nil, nil, err
	}
	issue, task, err := t.machineTask(ctx, ws, user, input.Identifier)
	if err != nil {
		return nil, nil, err
	}
	if name == "stop_preview" {
		stopped, err := t.machineTasks.RequestPreview(ctx, ws, task.ID, false)
		if errors.Is(err, repository.ErrConversationNotFound) || errors.Is(err, repository.ErrAgentTaskUnauthorized) {
			return nil, nil, toolFailure("only the task's creator can manage its preview and ports")
		}
		if err != nil {
			return nil, nil, err
		}
		// The machine stops the process; until then the preview is stopping.
		state := "stopping"
		if stopped.PreviewState == nil || *stopped.PreviewState == domain.AgentTaskPreviewStopped || *stopped.PreviewState == domain.AgentTaskPreviewFailed {
			state = domain.AgentTaskPreviewStopped
		}
		preview := map[string]any{"state": state}
		return map[string]any{"identifier": issue.Identifier, "task_id": task.ID, "preview": preview}, &issue.ID, nil
	}
	machine, err := t.taskMachine(ctx, ws, user, task)
	if err != nil {
		return nil, nil, err
	}
	list, err := t.machines.ListPorts(ctx, ws, machine.ID, user, false)
	if err != nil {
		return nil, nil, machineToolFailure(err)
	}
	ports := make([]map[string]any, 0, len(list.Ports))
	for _, port := range list.Ports {
		entry := map[string]any{"port": port.Port, "port_id": port.ID, "source": port.Source, "listening": port.Listening, "url": "https://" + port.Host}
		if port.Label != nil {
			entry["label"] = *port.Label
		}
		if port.AgentTaskID != nil && *port.AgentTaskID == task.ID {
			entry["preview"] = true
		}
		ports = append(ports, entry)
	}
	return map[string]any{
		"identifier": issue.Identifier, "task_id": task.ID, "machine_id": machine.ID, "machine": machine.Name,
		"ports": ports, "listening": list.Listening, "preview": task.Preview(), "audience": domain.AppPortAudienceOwner,
		"note": "Only the machine's owner can open these links, signed in to Kuayle; the chat shows an Open button for each.",
	}, &issue.ID, nil
}

func (t *AssistantTools) getMachineUsage(ctx context.Context, ws, user uuid.UUID, arguments string) (any, *uuid.UUID, error) {
	if refusal := refuseMachineAction(t.machineAccess, user, getMachineUsageTool); refusal != "" {
		return nil, nil, toolFailure("%s", refusal)
	}
	var input struct {
		Identifier string `json:"identifier"`
		MachineID  string `json:"machine_id"`
	}
	if err := decodeToolInput(arguments, &input); err != nil {
		return nil, nil, err
	}
	identifier, machineID := strings.TrimSpace(input.Identifier), strings.TrimSpace(input.MachineID)
	result := map[string]any{}
	var issueID *uuid.UUID
	var id uuid.UUID
	owner := user
	switch {
	case identifier != "" && machineID != "":
		return nil, nil, toolFailure("give machine_id or identifier, not both")
	case machineID != "":
		parsed, err := uuid.Parse(machineID)
		if err != nil {
			return nil, nil, toolFailure("machine_id is not a machine id")
		}
		id = parsed
	case identifier != "":
		issue, task, err := t.newestMachineTask(ctx, ws, identifier)
		if err != nil {
			return nil, nil, err
		}
		refusal, err := authorizeAssistantAction(ctx, t.members, t.system, ws, user, getMachineUsageTool, task.CreatorID)
		if err != nil {
			return nil, nil, err
		}
		if refusal != "" {
			return nil, nil, toolFailure("%s", refusal)
		}
		if task.TeamID != issue.TeamID {
			return nil, nil, toolFailure("the issue's newest coding task belongs to its previous team")
		}
		if task.MachineID == nil {
			return nil, nil, toolFailure("the issue's newest coding task has no machine yet")
		}
		// Task machines belong to the task creator. The admin grant is scoped
		// to this issue lookup, and direct lookup still checks the caller.
		id, owner, issueID = *task.MachineID, task.CreatorID, &issue.ID
		result["identifier"], result["task_id"] = issue.Identifier, task.TaskID
	default:
		return nil, nil, toolFailure("give machine_id or identifier")
	}
	machine, usage, err := t.machines.GetResourceUsageForUser(ctx, ws, id, owner)
	if errors.Is(err, ErrMachineNotFound) {
		return nil, nil, toolFailure("machine not found")
	}
	if err != nil {
		return nil, nil, err
	}
	result["machine_id"], result["machine"] = machine.ID, machine.Name
	result["limits"] = map[string]int{"cpu_millis": machine.CPUMillis, "memory_mb": machine.MemoryMB, "disk_gb": machine.DiskGB}
	result["sampled_at"], result["latest"], result["peak"] = nil, nil, nil
	if usage == nil {
		result["note"] = "No resource samples are available for this machine."
	} else {
		result["sampled_at"], result["latest"], result["peak"] = usage.SampledAt.UTC(), usage.Latest, usage.Peak
	}
	return result, issueID, nil
}

// startPreview queues an approved preview as the approver. The task
// repository re-checks that they created the task and that its machine and
// checkout are ready.
func (s *AssistantApprovals) startPreview(ctx context.Context, ws, conversation uuid.UUID, approval *domain.ConversationApproval) (*domain.ConversationApproval, error) {
	var payload domain.PreviewPayload
	if s.machineTasks == nil || json.Unmarshal(approval.Payload, &payload) != nil {
		return s.conversations.FinishApproval(ctx, ws, conversation, approval.ID, nil, "This approval cannot be executed")
	}
	task, err := s.machineTasks.Get(ctx, ws, payload.TaskID)
	if err == nil && (task.IssueID != approval.IssueID || task.MachineID == nil || *task.MachineID != payload.MachineID) {
		return s.conversations.FinishApproval(ctx, ws, conversation, approval.ID, nil, "The task moved to another machine after approval was requested; ask again")
	}
	if err == nil {
		task, err = s.machineTasks.RequestPreview(ctx, ws, payload.TaskID, true)
	}
	switch {
	case errors.Is(err, repository.ErrPreviewUnavailable):
		return s.conversations.FinishApproval(ctx, ws, conversation, approval.ID, nil, "The task has no preview command or no ready checkout on a running machine")
	case errors.Is(err, repository.ErrConversationNotFound), errors.Is(err, repository.ErrAgentTaskUnauthorized):
		return s.conversations.FinishApproval(ctx, ws, conversation, approval.ID, nil, "Only the task's creator can start its preview")
	case err != nil:
		// A transient failure leaves the claim in place; approving again resumes it.
		return nil, err
	}
	return s.conversations.FinishApproval(ctx, ws, conversation, approval.ID, &task.ID, "")
}

// exposePort exposes an approved port as the approver through the Dev Machine
// service, which re-checks that they own the machine and that the port is not
// one of its services. A port the machine already exposes is reused and, when
// a label was approved, renamed; a repeated approval converges on it.
func (s *AssistantApprovals) exposePort(ctx context.Context, ws, conversation uuid.UUID, approval *domain.ConversationApproval) (*domain.ConversationApproval, error) {
	var payload domain.ExposePortPayload
	if s.machines == nil || json.Unmarshal(approval.Payload, &payload) != nil {
		return s.conversations.FinishApproval(ctx, ws, conversation, approval.ID, nil, "This approval cannot be executed")
	}
	_, err := s.machines.CreatePort(ctx, ws, payload.MachineID, approval.ActorID, payload.Port, payload.Label)
	if errors.Is(err, ErrPortExists) {
		err = s.reusePort(ctx, ws, approval.ActorID, payload)
	}
	if err != nil {
		var safe *errAssistantTool
		if errors.As(machineToolFailure(err), &safe) {
			message := safe.message
			if errors.Is(err, ErrMachineNotFound) {
				message = "Only the machine's owner can expose its ports"
			}
			return s.conversations.FinishApproval(ctx, ws, conversation, approval.ID, nil, message)
		}
		// A transient failure leaves the claim in place; approving again resumes it.
		return nil, err
	}
	return s.conversations.FinishApproval(ctx, ws, conversation, approval.ID, &payload.TaskID, "")
}

func (s *AssistantApprovals) reusePort(ctx context.Context, ws, actor uuid.UUID, payload domain.ExposePortPayload) error {
	if payload.Label == nil {
		return nil
	}
	list, err := s.machines.ListPorts(ctx, ws, payload.MachineID, actor, false)
	if err != nil {
		return err
	}
	for _, port := range list.Ports {
		if port.Port == payload.Port {
			if port.Label != nil && *port.Label == *payload.Label {
				return nil
			}
			_, err = s.machines.UpdatePort(ctx, ws, payload.MachineID, port.ID, actor, payload.Label)
			return err
		}
	}
	// The port was removed meanwhile: expose it again.
	_, err = s.machines.CreatePort(ctx, ws, payload.MachineID, actor, payload.Port, payload.Label)
	return err
}
