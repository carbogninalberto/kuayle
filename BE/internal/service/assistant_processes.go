package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
)

const processIDInput = `{"type":"object","properties":{"process_id":{"type":"string","maxLength":36}},"required":["process_id"],"additionalProperties":false}`

func (t *AssistantTools) prepareAssistantProcess(ctx context.Context, a domain.ConversationAttempt, arguments string) (domain.AssistantProcessPayload, error) {
	var input struct {
		Command          string `json:"command"`
		MachineID        string `json:"machine_id"`
		WorkingDirectory string `json:"working_directory"`
		Port             int    `json:"port"`
	}
	var payload domain.AssistantProcessPayload
	if err := decodeToolInput(arguments, &input); err != nil {
		return payload, err
	}
	ws, user := a.Conversation.WorkspaceID, a.Conversation.CreatorID
	if err := t.commandUser(ctx, ws, user); err != nil {
		return payload, err
	}
	command := strings.TrimSpace(input.Command)
	if !validAssistantCommand(command) {
		return payload, toolFailure("command must be 1 to %d bytes of text", domain.CommandMaxBytes)
	}
	directory, ok := domain.CommandWorkingDirectory(input.WorkingDirectory)
	if !ok {
		return payload, toolFailure("working_directory must be a folder inside /workspace")
	}
	if !domain.ValidAppPort(input.Port) {
		return payload, toolFailure("the port must be between 1024 and 65535")
	}
	machine, _, err := t.commandMachine(ctx, ws, user, a.Conversation.ID, input.MachineID)
	if err != nil {
		return payload, err
	}
	ports, err := t.machines.ListPorts(ctx, ws, machine.ID, user, false)
	if err != nil {
		return payload, machineToolFailure(err)
	}
	for _, port := range ports.Ports {
		if port.Port == input.Port {
			return payload, machineToolFailure(ErrPortExists)
		}
	}
	if err = t.machines.ReservedPort(ctx, ws, machine.ID, input.Port); err != nil {
		return payload, machineToolFailure(err)
	}
	return domain.AssistantProcessPayload{
		MachineID: machine.ID, Machine: machine.Name, Command: command, WorkingDirectory: directory,
		Port: input.Port, TTLMinutes: int(domain.AssistantProcessTTL / time.Minute),
	}, nil
}

func (s *AssistantApprovals) startAssistantProcess(ctx context.Context, ws, conversation uuid.UUID, approval *domain.ConversationApproval) (*domain.ConversationApproval, error) {
	var payload domain.AssistantProcessPayload
	if json.Unmarshal(approval.Payload, &payload) != nil {
		return s.conversations.FinishApproval(ctx, ws, conversation, approval.ID, nil, "This approval cannot be executed")
	}
	err := s.conversations.QueueAssistantProcess(ctx, ws, conversation, approval.ID, payload)
	message := ""
	switch {
	case errors.Is(err, repository.ErrProcessQuota), errors.Is(err, repository.ErrProcessPortReserved), errors.Is(err, repository.ErrPortExists):
		message = upperFirst(err.Error())
	case err != nil:
		message = commandApprovalFailure(err)
		if message == "" {
			return nil, err
		}
	}
	return s.conversations.FinishApproval(ctx, ws, conversation, approval.ID, nil, message)
}

type AssistantProcessResult struct {
	ID         uuid.UUID  `json:"id"`
	MachineID  *uuid.UUID `json:"machine_id"`
	Port       int        `json:"port"`
	Status     string     `json:"status"`
	ExpiresAt  time.Time  `json:"expires_at"`
	PreviewURL string     `json:"preview_url,omitempty"`
}

// AssistantProcess returns the conversation owner's process and optionally
// requests a stop. The preview URL uses the normal app-port launch flow.
func (s *ConversationService) AssistantProcess(ctx context.Context, ws, conversation, id uuid.UUID, stop bool) (*AssistantProcessResult, error) {
	process, err := s.Repo.AssistantProcess(ctx, ws, conversation, id, stop)
	if err != nil {
		return nil, err
	}
	view := &AssistantProcessResult{ID: process.ID, MachineID: process.MachineID, Port: process.Port, Status: process.Status, ExpiresAt: process.ExpiresAt}
	if view.Status == "running" && s.tools != nil && s.tools.machines != nil {
		host := s.tools.machines.AppPortHost(&domain.DevMachine{RoutingKey: process.RoutingKey}, process.Port)
		view.PreviewURL = "https://" + host
	}
	return view, nil
}

func (s *ConversationService) assistantProcessTool(ctx context.Context, a domain.ConversationAttempt, name, arguments string) (any, error) {
	var input struct {
		ID uuid.UUID `json:"process_id"`
	}
	if err := decodeToolInput(arguments, &input); err != nil {
		return nil, err
	}
	if input.ID == uuid.Nil {
		return nil, toolFailure("give the process_id (the start_process approval id)")
	}
	ws, user := a.Conversation.WorkspaceID, a.Conversation.CreatorID
	ctx = domain.WithAccess(ctx, domain.Access{WorkspaceID: ws, UserID: user})
	process, err := s.AssistantProcess(ctx, ws, a.Conversation.ID, input.ID, name == "stop_process")
	if errors.Is(err, repository.ErrConversationNotFound) {
		return nil, toolFailure("no process was found in this conversation; use the start_process approval id after approval")
	}
	return process, err
}
