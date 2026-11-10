package domain

import (
	"time"

	"github.com/google/uuid"
)

// Passkey ceremony kinds.
const (
	PasskeyChallengeRegistration = "registration"
	PasskeyChallengeLogin        = "login"
)

// PasskeyCredential is a WebAuthn credential a user registered.
type PasskeyCredential struct {
	ID                uuid.UUID
	UserID            uuid.UUID
	CredentialID      []byte
	PublicKey         []byte
	SignCount         uint32
	Transports        []string
	AAGUID            []byte
	AttestationType   string
	AttestationFormat string
	UserVerified      bool
	BackupEligible    bool
	BackupState       bool
	Name              string
	CreatedAt         time.Time
	LastUsedAt        *time.Time
}

// PasskeyChallenge is the server-side state of one pending ceremony.
type PasskeyChallenge struct {
	HandleHash string
	Kind       string
	// UserID is set for registrations, which belong to a signed-in user.
	UserID      *uuid.UUID
	SessionData []byte
	ExpiresAt   time.Time
}
