package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"golang.org/x/crypto/bcrypt"
)

const (
	// PasskeyChallengeTTL is how long a started ceremony can be completed.
	PasskeyChallengeTTL = 5 * time.Minute
	// PasskeyFreshSession is how recently the user must have signed in to add a
	// passkey without entering their password again.
	PasskeyFreshSession = 10 * time.Minute
	// MaxPasskeysPerUser bounds the credentials one account can hold.
	MaxPasskeysPerUser  = 20
	maxPasskeyNameRunes = 64
	defaultPasskeyName  = "Passkey"
)

var (
	// ErrPasskeysDisabled: the instance has no usable relying party.
	ErrPasskeysDisabled = errors.New("passkeys are not available on this instance")
	// ErrPasskeyOrigin: the browser is not on one of the relying party's origins.
	ErrPasskeyOrigin = errors.New("passkey origin is not allowed")
	// ErrPasskeyReauthRequired: the session is too old to add a passkey.
	ErrPasskeyReauthRequired = errors.New("recent sign-in required")
	// ErrPasskeyChallenge: the ceremony was not started, has expired, was
	// already completed, or belongs to someone else.
	ErrPasskeyChallenge = errors.New("passkey challenge is missing or expired")
	// ErrPasskeyVerification: the authenticator response failed verification.
	ErrPasskeyVerification = errors.New("passkey verification failed")
	// ErrPasskeyUnknownCredential: no such passkey (never registered, or removed).
	ErrPasskeyUnknownCredential = errors.New("unknown passkey")
	// ErrPasskeyCloneSuspected: the signature counter did not advance.
	ErrPasskeyCloneSuspected = errors.New("passkey signature counter regressed")
	// ErrPasskeyAlreadyRegistered: the authenticator's credential is already stored.
	ErrPasskeyAlreadyRegistered = errors.New("passkey already registered")
	ErrPasskeyLimit             = errors.New("passkey limit reached")
	ErrPasskeyNotFound          = errors.New("passkey not found")
	ErrPasskeyName              = errors.New("passkey name must be 1 to 64 characters")
	// ErrLastPasskey: removing it would leave an account without a password
	// with no way to sign in.
	ErrLastPasskey = errors.New("the last passkey of an account without a password cannot be removed")
)

// PasskeyRejection is a refused ceremony with what is known about it, for the
// audit log. It unwraps to one of the Err* values above.
type PasskeyRejection struct {
	Reason    error
	Detail    string
	UserID    uuid.UUID // uuid.Nil when unknown
	PasskeyID uuid.UUID // uuid.Nil when unknown
}

func (r *PasskeyRejection) Error() string {
	if r.Detail == "" {
		return r.Reason.Error()
	}
	return r.Reason.Error() + ": " + r.Detail
}

func (r *PasskeyRejection) Unwrap() error { return r.Reason }

// PasskeyService runs the WebAuthn ceremonies and manages stored passkeys.
type PasskeyService struct {
	auth  *AuthService
	users repository.UserRepo
	repo  repository.PasskeyRepo
	rp    *PasskeyRelyingParty
	wa    *webauthn.WebAuthn
	now   func() time.Time
}

// NewPasskeyService builds the service. A nil relying party leaves passkey
// ceremonies disabled while stored passkeys can still be listed and removed.
func NewPasskeyService(auth *AuthService, users repository.UserRepo, repo repository.PasskeyRepo, rp *PasskeyRelyingParty) (*PasskeyService, error) {
	s := &PasskeyService{auth: auth, users: users, repo: repo, now: time.Now}
	if rp == nil {
		return s, nil
	}
	wa, err := webauthn.New(&webauthn.Config{
		RPID:          rp.ID,
		RPDisplayName: rp.DisplayName,
		RPOrigins:     rp.Origins,
		// No attestation is requested and no authenticator model is trusted or
		// distrusted: any AAGUID and self ("none") attestation are accepted.
		AttestationPreference: protocol.PreferNoAttestation,
		// A passkey replaces the password, so the authenticator must verify the
		// user (biometric or PIN), not merely detect presence.
		AuthenticatorSelection: protocol.AuthenticatorSelection{UserVerification: protocol.VerificationRequired},
		// Password managers return extension outputs nobody asked for; none of
		// them is used for a decision here.
		ExtensionsUnsolicitedOutputPolicy: protocol.UnsolicitedOutputPolicyIgnore,
		Timeouts: webauthn.TimeoutsConfig{
			Login:        webauthn.TimeoutConfig{Enforce: true, Timeout: PasskeyChallengeTTL, TimeoutUVD: PasskeyChallengeTTL},
			Registration: webauthn.TimeoutConfig{Enforce: true, Timeout: PasskeyChallengeTTL, TimeoutUVD: PasskeyChallengeTTL},
		},
	})
	if err != nil {
		return nil, err
	}
	s.rp, s.wa = rp, wa
	return s, nil
}

// Enabled reports whether passkey ceremonies can run on this instance.
func (s *PasskeyService) Enabled() bool { return s != nil && s.wa != nil }

// checkOrigin refuses a ceremony early when the browser's Origin header shows
// it is on an address the relying party does not cover. The relying party is
// never derived from Host or X-Forwarded-* headers; the signed origin in the
// authenticator response is what completion verifies.
func (s *PasskeyService) checkOrigin(requestOrigin string) error {
	if !s.Enabled() {
		return ErrPasskeysDisabled
	}
	if requestOrigin != "" && !s.rp.allows(requestOrigin) {
		return &PasskeyRejection{Reason: ErrPasskeyOrigin, Detail: fmt.Sprintf("request origin %q, allowed %v", requestOrigin, s.rp.Origins)}
	}
	return nil
}

type passkeyUser struct {
	user        *domain.User
	credentials []webauthn.Credential
}

func (u *passkeyUser) WebAuthnID() []byte   { return u.user.ID[:] }
func (u *passkeyUser) WebAuthnName() string { return u.user.Email }
func (u *passkeyUser) WebAuthnDisplayName() string {
	if u.user.DisplayName != "" {
		return u.user.DisplayName
	}
	if u.user.Name != "" {
		return u.user.Name
	}
	return u.user.Email
}
func (u *passkeyUser) WebAuthnCredentials() []webauthn.Credential { return u.credentials }

func toWebAuthnCredential(p *domain.PasskeyCredential) webauthn.Credential {
	transports := make([]protocol.AuthenticatorTransport, 0, len(p.Transports))
	for _, transport := range p.Transports {
		transports = append(transports, protocol.AuthenticatorTransport(transport))
	}
	return webauthn.Credential{
		ID:                p.CredentialID,
		PublicKey:         p.PublicKey,
		AttestationType:   p.AttestationType,
		AttestationFormat: p.AttestationFormat,
		Transport:         transports,
		Flags: webauthn.CredentialFlags{
			UserPresent: true, UserVerified: p.UserVerified,
			BackupEligible: p.BackupEligible, BackupState: p.BackupState,
		},
		Authenticator: webauthn.Authenticator{AAGUID: p.AAGUID, SignCount: p.SignCount},
	}
}

// HasUsablePassword reports whether the account can sign in with a password:
// whether its stored hash is one a password can ever match. Every sign-up sets
// a password today, so this guards accounts whose hash was blanked by hand or
// by a future passwordless sign-up.
func HasUsablePassword(user *domain.User) bool {
	if user == nil {
		return false
	}
	_, err := bcrypt.Cost([]byte(user.PasswordHash))
	return err == nil
}

// BeginRegistration starts adding a passkey to the signed-in user. The session
// must be fresh (authenticatedAt within PasskeyFreshSession) or the request
// must carry the account password, so a stolen session cannot add a passkey
// on its own. It returns the options for navigator.credentials.create and the
// opaque handle the browser must present on completion.
func (s *PasskeyService) BeginRegistration(ctx context.Context, userID uuid.UUID, authenticatedAt time.Time, password, requestOrigin string) (*protocol.CredentialCreation, string, error) {
	if err := s.checkOrigin(requestOrigin); err != nil {
		return nil, "", err
	}
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, "", err
	}
	if user == nil {
		return nil, "", ErrPasskeyNotFound
	}
	fresh := !authenticatedAt.IsZero() && s.now().Sub(authenticatedAt) <= PasskeyFreshSession
	if !fresh {
		if password == "" {
			return nil, "", ErrPasskeyReauthRequired
		}
		if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
			return nil, "", ErrInvalidCredentials
		}
	}
	existing, err := s.repo.ListByUser(ctx, userID)
	if err != nil {
		return nil, "", err
	}
	if len(existing) >= MaxPasskeysPerUser {
		return nil, "", ErrPasskeyLimit
	}
	wu := &passkeyUser{user: user}
	for i := range existing {
		wu.credentials = append(wu.credentials, toWebAuthnCredential(&existing[i]))
	}
	options, session, err := s.wa.BeginRegistration(wu,
		// Discoverable, so sign-in needs no email address first.
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired),
		// The same authenticator cannot be registered twice.
		webauthn.WithExclusions(webauthn.Credentials(wu.credentials).CredentialDescriptors()),
		webauthn.WithExtensions(webauthn.WithExtensionCredProps()),
	)
	if err != nil {
		return nil, "", err
	}
	handle, err := s.saveChallenge(ctx, domain.PasskeyChallengeRegistration, &userID, session, "")
	if err != nil {
		return nil, "", err
	}
	return options, handle, nil
}

// FinishRegistration verifies the authenticator's response to the user's
// pending registration and stores the new passkey.
func (s *PasskeyService) FinishRegistration(ctx context.Context, userID uuid.UUID, handle, name string, credentialJSON []byte) (*domain.PasskeyCredential, error) {
	if !s.Enabled() {
		return nil, ErrPasskeysDisabled
	}
	name, err := normalizePasskeyName(name, true)
	if err != nil {
		return nil, err
	}
	// The challenge is consumed before anything is verified: whatever happens
	// next, this response and this challenge cannot be tried again.
	session, err := s.consumeChallenge(ctx, handle, domain.PasskeyChallengeRegistration, &userID)
	if err != nil {
		return nil, err
	}
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, ErrPasskeyNotFound
	}
	parsed, err := protocol.ParseCredentialCreationResponseBytes(credentialJSON)
	if err != nil {
		return nil, &PasskeyRejection{Reason: ErrPasskeyVerification, Detail: protocolDetail(err), UserID: userID}
	}
	if origin := parsed.Response.CollectedClientData.Origin; !s.rp.allows(origin) {
		return nil, &PasskeyRejection{Reason: ErrPasskeyOrigin, Detail: fmt.Sprintf("response origin %q, allowed %v", origin, s.rp.Origins), UserID: userID}
	}
	credential, err := s.wa.CreateCredential(&passkeyUser{user: user}, *session, parsed)
	if err != nil {
		return nil, &PasskeyRejection{Reason: ErrPasskeyVerification, Detail: protocolDetail(err), UserID: userID}
	}
	existing, err := s.repo.ListByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if len(existing) >= MaxPasskeysPerUser {
		return nil, ErrPasskeyLimit
	}
	transports := make([]string, 0, len(credential.Transport))
	for _, transport := range credential.Transport {
		transports = append(transports, string(transport))
	}
	stored := &domain.PasskeyCredential{
		ID: uuid.New(), UserID: userID, CredentialID: credential.ID, PublicKey: credential.PublicKey,
		SignCount: credential.Authenticator.SignCount, Transports: transports, AAGUID: credential.Authenticator.AAGUID,
		AttestationType: credential.AttestationType, AttestationFormat: credential.AttestationFormat,
		UserVerified: credential.Flags.UserVerified, BackupEligible: credential.Flags.BackupEligible,
		BackupState: credential.Flags.BackupState, Name: name,
	}
	if err := s.repo.Create(ctx, stored); err != nil {
		if errors.Is(err, repository.ErrPasskeyExists) {
			return nil, &PasskeyRejection{Reason: ErrPasskeyAlreadyRegistered, UserID: userID}
		}
		return nil, err
	}
	return stored, nil
}

// BeginLogin starts a sign-in with a discoverable credential: no account is
// named, the authenticator offers the passkeys it holds for this relying
// party. replacesHandle is the handle of a login this browser started before,
// which the new one replaces.
func (s *PasskeyService) BeginLogin(ctx context.Context, replacesHandle, requestOrigin string) (*protocol.CredentialAssertion, string, error) {
	if err := s.checkOrigin(requestOrigin); err != nil {
		return nil, "", err
	}
	options, session, err := s.wa.BeginDiscoverableLogin()
	if err != nil {
		return nil, "", err
	}
	handle, err := s.saveChallenge(ctx, domain.PasskeyChallengeLogin, nil, session, replacesHandle)
	if err != nil {
		return nil, "", err
	}
	return options, handle, nil
}

// FinishLogin verifies an assertion against the browser's pending login and
// opens a session through the same path as a password sign-in. The session is
// tied to the passkey: removing the passkey ends it.
func (s *PasskeyService) FinishLogin(ctx context.Context, handle string, credentialJSON []byte) (*domain.User, string, string, error) {
	if !s.Enabled() {
		return nil, "", "", ErrPasskeysDisabled
	}
	session, err := s.consumeChallenge(ctx, handle, domain.PasskeyChallengeLogin, nil)
	if err != nil {
		return nil, "", "", err
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(credentialJSON)
	if err != nil {
		return nil, "", "", &PasskeyRejection{Reason: ErrPasskeyVerification, Detail: protocolDetail(err)}
	}
	if origin := parsed.Response.CollectedClientData.Origin; !s.rp.allows(origin) {
		return nil, "", "", &PasskeyRejection{Reason: ErrPasskeyOrigin, Detail: fmt.Sprintf("response origin %q, allowed %v", origin, s.rp.Origins)}
	}
	// The credential ID alone identifies the passkey and its owner, so an
	// authenticator that returns no user handle still signs in.
	stored, err := s.repo.GetByCredentialID(ctx, parsed.RawID)
	if err != nil {
		return nil, "", "", err
	}
	if stored == nil {
		return nil, "", "", &PasskeyRejection{Reason: ErrPasskeyUnknownCredential, Detail: "credential ID is not registered"}
	}
	user, err := s.users.GetByID(ctx, stored.UserID)
	if err != nil {
		return nil, "", "", err
	}
	if user == nil {
		return nil, "", "", &PasskeyRejection{Reason: ErrPasskeyUnknownCredential, Detail: "owner no longer exists", PasskeyID: stored.ID}
	}
	rejected := func(reason error, detail string) error {
		return &PasskeyRejection{Reason: reason, Detail: detail, UserID: user.ID, PasskeyID: stored.ID}
	}
	if len(parsed.Response.UserHandle) == 0 {
		parsed.Response.UserHandle = user.ID[:]
	} else if !bytes.Equal(parsed.Response.UserHandle, user.ID[:]) {
		return nil, "", "", rejected(ErrPasskeyUnknownCredential, "user handle does not match the passkey's owner")
	}
	wu := &passkeyUser{user: user, credentials: []webauthn.Credential{toWebAuthnCredential(stored)}}
	_, verified, err := s.wa.ValidatePasskeyLogin(func(_, _ []byte) (webauthn.User, error) { return wu, nil }, *session, parsed)
	if err != nil {
		return nil, "", "", rejected(ErrPasskeyVerification, protocolDetail(err))
	}
	if verified.Authenticator.CloneWarning {
		return nil, "", "", rejected(ErrPasskeyCloneSuspected, fmt.Sprintf("stored counter %d, received %d",
			stored.SignCount, parsed.Response.AuthenticatorData.Counter))
	}
	used, err := s.repo.RecordUse(ctx, stored.ID, stored.SignCount, verified.Authenticator.SignCount, verified.Flags.BackupState)
	if err != nil {
		return nil, "", "", err
	}
	if !used {
		return nil, "", "", rejected(ErrPasskeyUnknownCredential, "passkey was removed or used concurrently during sign-in")
	}
	accessToken, refreshToken, err := s.auth.issueSession(ctx, user.ID, s.now(), &stored.ID)
	if err != nil {
		// The session references the passkey; it cannot be stored once the
		// passkey is gone.
		if exists, existsErr := s.repo.Exists(ctx, stored.ID, user.ID); existsErr == nil && !exists {
			return nil, "", "", rejected(ErrPasskeyUnknownCredential, "passkey was removed during sign-in")
		}
		return nil, "", "", err
	}
	return user, accessToken, refreshToken, nil
}

func (s *PasskeyService) List(ctx context.Context, userID uuid.UUID) ([]domain.PasskeyCredential, error) {
	return s.repo.ListByUser(ctx, userID)
}

func (s *PasskeyService) Rename(ctx context.Context, userID, id uuid.UUID, name string) (*domain.PasskeyCredential, error) {
	name, err := normalizePasskeyName(name, false)
	if err != nil {
		return nil, err
	}
	renamed, err := s.repo.Rename(ctx, id, userID, name)
	if err != nil {
		return nil, err
	}
	if renamed == nil {
		return nil, ErrPasskeyNotFound
	}
	return renamed, nil
}

// Delete removes one of the user's passkeys together with the sessions it
// minted; other sessions are untouched. An account that cannot sign in with a
// password keeps at least one passkey.
func (s *PasskeyService) Delete(ctx context.Context, userID, id uuid.UUID) error {
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return err
	}
	if user == nil {
		return ErrPasskeyNotFound
	}
	removed, err := s.repo.Delete(ctx, id, userID, !HasUsablePassword(user))
	if errors.Is(err, repository.ErrLastPasskey) {
		return ErrLastPasskey
	}
	if err != nil {
		return err
	}
	if !removed {
		return ErrPasskeyNotFound
	}
	return nil
}

// SessionPasskeyExists reports whether the passkey a session was minted with
// still belongs to the user. Access tokens outlive the stored session by up to
// their lifetime, so the authentication middleware asks on every request.
func (s *PasskeyService) SessionPasskeyExists(ctx context.Context, id, userID uuid.UUID) (bool, error) {
	return s.repo.Exists(ctx, id, userID)
}

func (s *PasskeyService) saveChallenge(ctx context.Context, kind string, userID *uuid.UUID, session *webauthn.SessionData, replacesHandle string) (string, error) {
	data, err := json.Marshal(session)
	if err != nil {
		return "", err
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	handle := base64.RawURLEncoding.EncodeToString(raw)
	replaces := ""
	if replacesHandle != "" {
		replaces = hashToken(replacesHandle)
	}
	err = s.repo.SaveChallenge(ctx, &domain.PasskeyChallenge{
		HandleHash: hashToken(handle), Kind: kind, UserID: userID, SessionData: data,
		ExpiresAt: s.now().Add(PasskeyChallengeTTL),
	}, replaces)
	if err != nil {
		return "", err
	}
	return handle, nil
}

// consumeChallenge takes the pending ceremony out of the store. It fails when
// the handle is unknown (never issued, already used, replaced), expired, of
// another kind, or bound to another user.
func (s *PasskeyService) consumeChallenge(ctx context.Context, handle, kind string, userID *uuid.UUID) (*webauthn.SessionData, error) {
	if handle == "" {
		return nil, ErrPasskeyChallenge
	}
	challenge, err := s.repo.ConsumeChallenge(ctx, hashToken(handle))
	if err != nil {
		return nil, err
	}
	if challenge == nil || challenge.Kind != kind || !challenge.ExpiresAt.After(s.now()) {
		return nil, ErrPasskeyChallenge
	}
	if (userID == nil) != (challenge.UserID == nil) || (userID != nil && *userID != *challenge.UserID) {
		return nil, ErrPasskeyChallenge
	}
	var session webauthn.SessionData
	if err := json.Unmarshal(challenge.SessionData, &session); err != nil {
		return nil, err
	}
	return &session, nil
}

func normalizePasskeyName(name string, allowDefault bool) (string, error) {
	name = strings.Join(strings.Fields(name), " ")
	if name == "" && allowDefault {
		return defaultPasskeyName, nil
	}
	if name == "" || utf8.RuneCountInString(name) > maxPasskeyNameRunes {
		return "", ErrPasskeyName
	}
	return name, nil
}

// protocolDetail renders a WebAuthn library error with its developer detail.
func protocolDetail(err error) string {
	var protocolErr *protocol.Error
	if errors.As(err, &protocolErr) {
		parts := []string{protocolErr.Type, protocolErr.Details, protocolErr.DevInfo}
		out := make([]string, 0, len(parts))
		for _, part := range parts {
			if part != "" {
				out = append(out, part)
			}
		}
		return strings.Join(out, ": ")
	}
	return err.Error()
}
