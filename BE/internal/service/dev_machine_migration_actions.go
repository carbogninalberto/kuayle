package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	cryptoutil "github.com/kuayle/kuayle-backend/pkg/crypto"
)

// Named migration actions (KUA-181) are configured through the Dev Machine
// service: the settings handlers validate the request, the service encrypts
// the connection secret with the machine service's key and the store, which
// re-checks the caller's authority (the machine's creator or a workspace
// owner or admin) in its transactions, persists the action. Neither the raw
// nor the encrypted secret ever appears in an error, a log entry or a
// response.

// ErrInvalidMigrationAction marks a migration action request the service
// refused; its message names the field, never the secret.
var ErrInvalidMigrationAction = errors.New("invalid migration action")

// MigrationActionStore persists a machine's named migration actions; the
// concrete AssistantCommandRepository implements it.
type MigrationActionStore interface {
	ListMachineMigrationActions(ctx context.Context, ws, machine, user uuid.UUID) ([]domain.MigrationAction, error)
	CreateMachineMigrationAction(ctx context.Context, ws, machine, user uuid.UUID, action *domain.MigrationAction) (*domain.MigrationAction, error)
	UpdateMachineMigrationAction(ctx context.Context, ws, machine, actionID, user uuid.UUID, action *domain.MigrationAction, newSecret bool) (*domain.MigrationAction, error)
	DeleteMachineMigrationAction(ctx context.Context, ws, machine, actionID, user uuid.UUID) error
}

// SetMigrationActions wires the store that persists named migration actions
// (KUA-181); the concrete AssistantCommandRepository implements it.
func (s *DevMachineService) SetMigrationActions(store MigrationActionStore) {
	s.migrationActions = store
}

// ListMigrationActions lists one machine's migration actions for the
// machine's creator or a workspace owner or admin; the store enforces it.
func (s *DevMachineService) ListMigrationActions(ctx context.Context, workspaceID, machineID, userID uuid.UUID) ([]domain.MigrationAction, error) {
	return s.migrationActions.ListMachineMigrationActions(ctx, workspaceID, machineID, userID)
}

// validateMigrationAction checks the configured fields and reports the
// secret to store; an empty one keeps the stored secret on update. The
// secret itself never becomes part of an error.
func validateMigrationAction(input *dto.DevMachineMigrationActionInput, secretRequired bool) (string, error) {
	if !domain.ValidMigrationActionName(input.Name) {
		return "", fmt.Errorf("%w: invalid action name", ErrInvalidMigrationAction)
	}
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	if input.Description == "" || utf8.RuneCountInString(input.Description) > domain.MigrationDescriptionMaxRunes {
		return "", fmt.Errorf("%w: description is required and at most %d characters", ErrInvalidMigrationAction, domain.MigrationDescriptionMaxRunes)
	}
	if !domain.ValidMigrationCommand(input.Command) {
		return "", fmt.Errorf("%w: invalid action command", ErrInvalidMigrationAction)
	}
	input.Command = strings.TrimSpace(input.Command)
	switch {
	case input.Secret == "":
		if secretRequired {
			return "", fmt.Errorf("%w: secret is required", ErrInvalidMigrationAction)
		}
	case len(input.Secret) > domain.MigrationSecretMaxBytes || !utf8.ValidString(input.Secret) || strings.ContainsRune(input.Secret, 0):
		return "", fmt.Errorf("%w: invalid secret", ErrInvalidMigrationAction)
	case strings.Contains(input.Command, input.Secret) || strings.Contains(input.Description, input.Secret):
		// The command and description are shown to the model, in approvals
		// and in the machine's list; the secret reaches the run only as
		// $KUAYLE_MIGRATION_SECRET.
		return "", fmt.Errorf("%w: the command and description must not contain the secret; use $KUAYLE_MIGRATION_SECRET", ErrInvalidMigrationAction)
	}
	return input.Secret, nil
}

// CreateMigrationAction stores a new named migration action. The secret is
// encrypted with the machine service's key and never leaves the server.
func (s *DevMachineService) CreateMigrationAction(ctx context.Context, workspaceID, machineID, userID uuid.UUID, input dto.DevMachineMigrationActionInput) (*domain.MigrationAction, error) {
	secret, err := validateMigrationAction(&input, true)
	if err != nil {
		return nil, err
	}
	encrypted, err := cryptoutil.Encrypt(secret, s.encryptionKey)
	if err != nil {
		return nil, err
	}
	action := &domain.MigrationAction{
		WorkspaceID: workspaceID, MachineID: machineID, Name: input.Name,
		Description: input.Description, Command: input.Command, EncryptedSecret: encrypted,
	}
	return s.migrationActions.CreateMachineMigrationAction(ctx, workspaceID, machineID, userID, action)
}

// UpdateMigrationAction changes one action's name, description and command;
// an omitted secret keeps the stored one.
func (s *DevMachineService) UpdateMigrationAction(ctx context.Context, workspaceID, machineID, actionID, userID uuid.UUID, input dto.DevMachineMigrationActionInput) (*domain.MigrationAction, error) {
	secret, err := validateMigrationAction(&input, false)
	if err != nil {
		return nil, err
	}
	action := &domain.MigrationAction{
		WorkspaceID: workspaceID, MachineID: machineID, Name: input.Name,
		Description: input.Description, Command: input.Command,
	}
	if secret != "" {
		encrypted, err := cryptoutil.Encrypt(secret, s.encryptionKey)
		if err != nil {
			return nil, err
		}
		action.EncryptedSecret = encrypted
	}
	return s.migrationActions.UpdateMachineMigrationAction(ctx, workspaceID, machineID, actionID, userID, action, secret != "")
}

// DeleteMigrationAction removes one action; the store re-checks the
// caller's authority.
func (s *DevMachineService) DeleteMigrationAction(ctx context.Context, workspaceID, machineID, actionID, userID uuid.UUID) error {
	return s.migrationActions.DeleteMachineMigrationAction(ctx, workspaceID, machineID, actionID, userID)
}
