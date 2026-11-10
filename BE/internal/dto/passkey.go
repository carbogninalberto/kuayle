package dto

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// BeginPasskeyRegistrationRequest carries the account password when the
// session is not fresh enough to add a passkey on its own.
type BeginPasskeyRegistrationRequest struct {
	Password string `json:"password" validate:"omitempty,max=128"`
}

// FinishPasskeyRegistrationRequest carries the browser's
// PublicKeyCredential (attestation) as JSON and a name for the passkey.
type FinishPasskeyRegistrationRequest struct {
	Name       string          `json:"name" validate:"omitempty,max=200"`
	Credential json.RawMessage `json:"credential" validate:"required"`
}

// FinishPasskeyLoginRequest carries the browser's PublicKeyCredential (assertion) as JSON.
type FinishPasskeyLoginRequest struct {
	Credential json.RawMessage `json:"credential" validate:"required"`
}

type RenamePasskeyRequest struct {
	Name string `json:"name" validate:"required,max=200"`
}

// PasskeyResponse never includes the public key or the credential ID.
type PasskeyResponse struct {
	ID         uuid.UUID  `json:"id"`
	Name       string     `json:"name"`
	Transports []string   `json:"transports"`
	AAGUID     string     `json:"aaguid"`
	BackedUp   bool       `json:"backed_up"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	// Current marks the passkey the calling session signed in with.
	Current bool `json:"current"`
}
