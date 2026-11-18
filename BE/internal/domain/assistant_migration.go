package domain

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Named migration actions (KUA-181) let the assistant run a database
// migration against a real database by name only. A workspace owner, admin or
// the machine's creator configures an action on a Dev Machine with a command
// that references the connection secret as $KUAYLE_MIGRATION_SECRET; Kuayle
// stores the secret encrypted, injects it into the run container at run time
// and redacts it from the stored output, so neither the model nor the
// transcript ever sees it.

const (
	// MigrationSecretEnv is the only form the action's command can reference
	// the secret in; the value is injected at run time and never stored or
	// reported.
	MigrationSecretEnv = "KUAYLE_MIGRATION_SECRET"
	// MigrationActionNameMaxBytes bounds an action's name (also a schema
	// CHECK).
	MigrationActionNameMaxBytes = 100
	// MigrationDescriptionMaxRunes bounds an action's description (also a
	// schema CHECK).
	MigrationDescriptionMaxRunes = 500
	// MigrationSecretMaxBytes bounds the configured secret before encryption.
	MigrationSecretMaxBytes = 8192
)

// MigrationAction is one named, preconfigured migration action of a machine.
// EncryptedSecret never leaves the server: it is excluded from every JSON
// response and only decrypted when the run container is created.
type MigrationAction struct {
	ID                   uuid.UUID  `json:"id" db:"id"`
	WorkspaceID          uuid.UUID  `json:"workspace_id" db:"workspace_id"`
	MachineID            uuid.UUID  `json:"machine_id" db:"machine_id"`
	Name                 string     `json:"name" db:"name"`
	Description          string     `json:"description" db:"description"`
	Command              string     `json:"command" db:"command"`
	EncryptedSecret      string     `json:"-" db:"encrypted_secret"`
	EncryptionKeyVersion int        `json:"-" db:"encryption_key_version"`
	CreatedByUserID      *uuid.UUID `json:"created_by_user_id" db:"created_by_user_id"`
	CreatedAt            time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at" db:"updated_at"`
}

// MigrationRunPayload is the approval of run_migration: the action's name,
// its admin-configured command and the machine it runs on. The command
// reaches the secret only as $KUAYLE_MIGRATION_SECRET, so the payload never
// carries a credential.
type MigrationRunPayload struct {
	CommandMachine
	Action         string `json:"action"`
	Description    string `json:"description,omitempty"`
	Command        string `json:"command"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

// ValidMigrationActionName reports whether name is a usable action name: one
// line of 1 to MigrationActionNameMaxBytes bytes, not blank and trimmed.
func ValidMigrationActionName(name string) bool {
	name = strings.TrimSpace(name)
	return name != "" && len(name) <= MigrationActionNameMaxBytes && utf8.ValidString(name) &&
		!strings.ContainsAny(name, "\r\n\x00")
}

// ValidMigrationCommand reports whether command is a usable action command:
// nonblank, no NUL and within CommandMaxBytes. That it does not contain the
// secret itself is checked where the secret is supplied (the service).
func ValidMigrationCommand(command string) bool {
	command = strings.TrimSpace(command)
	return command != "" && len(command) <= CommandMaxBytes && utf8.ValidString(command) &&
		!strings.ContainsRune(command, 0)
}
