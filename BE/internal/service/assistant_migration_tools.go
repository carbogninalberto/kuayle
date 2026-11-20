package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
)

// The migration tool (run_migration) requests a named database migration
// action a workspace admin configured on the user's machine: the model only
// names the action (and the machine when several have one of that name), and
// nothing runs until the user approves. The command and its connection secret
// are admin-configured; the secret is injected at run time and never reaches
// the model, a payload, a result or a log line.

// noMigrationAction is what the assistant can do when no action is configured.
const noMigrationAction = "No migration action is configured on your machines. Ask the user to have a workspace admin or the machine's creator configure one in the machine's settings; they choose the command and store the database connection secret, so it is requested only by name."

// assistantMigrationStore resolves the named migration actions configured on
// the user's machines. It never hands out an action's encrypted secret.
type assistantMigrationStore interface {
	MigrationActionsForUser(ctx context.Context, ws, user uuid.UUID) ([]repository.UserMigrationAction, error)
	MigrationAction(ctx context.Context, ws, user, machine uuid.UUID, name string) (*domain.MigrationAction, error)
}

// SetMigrations enables run_migration.
func (t *AssistantTools) SetMigrations(store assistantMigrationStore) {
	t.migrations = store
}

// migrationActionList names every configured action with its machine and
// description, for a refusal or the prompt.
func migrationActionList(actions []repository.UserMigrationAction) string {
	parts := make([]string, len(actions))
	for i, action := range actions {
		parts[i] = fmt.Sprintf("%s on machine %q — %s", action.Name, action.MachineName, action.Description)
	}
	return strings.Join(parts, "; ")
}

// refuseMigrationName refuses an unknown action name by listing every
// configured action so the model can retry with a valid one; with none
// configured it says how one is set up.
func refuseMigrationName(actions []repository.UserMigrationAction, name string) error {
	if len(actions) == 0 {
		return toolFailure("%s", noMigrationAction)
	}
	return toolFailure("No migration action named %q is configured. Configured on your machines: %s", name, migrationActionList(actions))
}

// prepareMigration binds run_migration to the named action and the machine it
// is configured on, as shown on the approval card. The payload carries the
// admin-configured command, never the connection secret.
func (t *AssistantTools) prepareMigration(ctx context.Context, a domain.ConversationAttempt, arguments string) (domain.MigrationRunPayload, error) {
	ws, user := a.Conversation.WorkspaceID, a.Conversation.CreatorID
	var input struct {
		Action    string `json:"action"`
		MachineID string `json:"machine_id"`
	}
	if err := decodeToolInput(arguments, &input); err != nil {
		return domain.MigrationRunPayload{}, err
	}
	if err := t.commandUser(ctx, ws, user); err != nil {
		return domain.MigrationRunPayload{}, err
	}
	name := strings.TrimSpace(input.Action)
	if !domain.ValidMigrationActionName(name) {
		return domain.MigrationRunPayload{}, toolFailure("action must be 1 to 100 characters of text")
	}
	var action *domain.MigrationAction
	var machine *domain.DevMachine
	if text := strings.TrimSpace(input.MachineID); text != "" {
		id, err := uuid.Parse(text)
		if err != nil {
			return domain.MigrationRunPayload{}, toolFailure("machine_id is not a machine id")
		}
		if machine, err = t.commands.UsableMachine(ctx, ws, user, id); err != nil {
			if errors.Is(err, repository.ErrCommandMachineUnavailable) {
				return domain.MigrationRunPayload{}, toolFailure("%s", upperFirst(notMachineOwner))
			}
			return domain.MigrationRunPayload{}, err
		}
		if action, err = t.migrations.MigrationAction(ctx, ws, user, id, name); err != nil {
			if errors.Is(err, repository.ErrMigrationActionNotFound) {
				actions, listErr := t.migrations.MigrationActionsForUser(ctx, ws, user)
				if listErr != nil {
					return domain.MigrationRunPayload{}, listErr
				}
				return domain.MigrationRunPayload{}, refuseMigrationName(actions, name)
			}
			return domain.MigrationRunPayload{}, err
		}
	} else {
		actions, err := t.migrations.MigrationActionsForUser(ctx, ws, user)
		if err != nil {
			return domain.MigrationRunPayload{}, err
		}
		var matches []repository.UserMigrationAction
		for _, candidate := range actions {
			if candidate.Name == name {
				matches = append(matches, candidate)
			}
		}
		switch len(matches) {
		case 0:
			return domain.MigrationRunPayload{}, refuseMigrationName(actions, name)
		case 1:
			// The action's machine may have stopped since it was listed.
			if machine, err = t.commands.UsableMachine(ctx, ws, user, matches[0].MachineID); err != nil {
				if errors.Is(err, repository.ErrCommandMachineUnavailable) {
					return domain.MigrationRunPayload{}, toolFailure("The machine %s is not running; ask the user to start it, then ask again.", matches[0].MachineName)
				}
				return domain.MigrationRunPayload{}, err
			}
			action = &matches[0].MigrationAction
		default:
			return domain.MigrationRunPayload{}, toolFailure("The migration action %q is configured on several machines; give machine_id of one: %s", name, migrationActionList(matches))
		}
	}
	return domain.MigrationRunPayload{CommandMachine: domain.CommandMachineOf(machine, false), Action: name, Description: action.Description,
		Command: action.Command, TimeoutSeconds: domain.DefaultCommandTimeoutSeconds}, nil
}

// migrationPrompt tells the model when to request a named migration action,
// or "" when run_migration is not offered or no action is configured.
func (t *AssistantTools) migrationPrompt(ctx context.Context, ws, user uuid.UUID) string {
	if t == nil || t.commands == nil || t.migrations == nil {
		return ""
	}
	disabled, err := t.disabledTools(ctx, ws)
	if err != nil || disabled["run_migration"] {
		return ""
	}
	actions, err := t.migrations.MigrationActionsForUser(ctx, ws, user)
	if err != nil || len(actions) == 0 {
		return ""
	}
	prompt := " The user's machines have named database migration actions a workspace admin configured; request one with run_migration(action) — or run_migration(action, machine_id) when the model must pick the machine — which runs the admin-configured command with the connection secret injected at run time (you never see it), only after the user approves."
	listed := actions
	if len(listed) > 8 {
		listed = listed[:8]
	}
	parts := make([]string, len(listed))
	for i, action := range listed {
		parts[i] = fmt.Sprintf("%s on machine %q (%s): %s", action.Name, action.MachineName, action.MachineStatus, truncateRunes(action.Description, 200))
	}
	return prompt + " Available migrations: " + strings.Join(parts, "; ") + "."
}

// runMigration queues the approved migration action's command, as the
// approver; the repository re-checks that they own the running machine and
// may use it, and an approval queues at most one run. The card shows the
// result.
func (s *AssistantApprovals) runMigration(ctx context.Context, ws, conversation uuid.UUID, approval *domain.ConversationApproval) (*domain.ConversationApproval, error) {
	var payload domain.MigrationRunPayload
	if s.commands == nil || json.Unmarshal(approval.Payload, &payload) != nil {
		return s.conversations.FinishApproval(ctx, ws, conversation, approval.ID, nil, "This approval cannot be executed")
	}
	_, err := s.commands.QueueRun(ctx, ws, approval.ActorID, repository.CommandRunRequest{MachineID: payload.MachineID, Command: payload.Command, Write: false, Migration: true, ActionName: payload.Action,
		WorkingDirectory: "/workspace", Reused: false, ConversationID: &conversation, ToolCallID: &approval.ToolCallID, ApprovalID: &approval.ID})
	if err != nil {
		if message := commandApprovalFailure(err); message != "" {
			return s.conversations.FinishApproval(ctx, ws, conversation, approval.ID, nil, message)
		}
		// A transient failure leaves the claim in place; approving again resumes it.
		return nil, err
	}
	return s.conversations.FinishApproval(ctx, ws, conversation, approval.ID, nil, "")
}
