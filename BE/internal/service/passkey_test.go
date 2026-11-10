package service

import (
	"bytes"
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/passkeytest"
	"github.com/kuayle/kuayle-backend/internal/repository"
	jwtpkg "github.com/kuayle/kuayle-backend/pkg/jwt"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

const (
	passkeyTestOrigin   = "https://kuayle.example.test"
	passkeyTestSecret   = "passkey-test-secret-with-enough-length"
	passkeyTestPassword = "Password123!!"
)

// --- In-memory stores with the semantics the SQL repositories provide ---

type memUsers struct {
	mu    sync.Mutex
	users map[uuid.UUID]*domain.User
}

func (m *memUsers) Create(_ context.Context, user *domain.User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.users[user.ID] = user
	return nil
}
func (m *memUsers) GetByID(_ context.Context, id uuid.UUID) (*domain.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.users[id], nil
}
func (m *memUsers) GetByEmail(_ context.Context, email string) (*domain.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, user := range m.users {
		if user.Email == email {
			return user, nil
		}
	}
	return nil, nil
}
func (m *memUsers) Update(context.Context, *domain.User) error { return nil }

type memRefreshTokens struct {
	mu     sync.Mutex
	tokens map[string]*repository.RefreshToken
}

func (m *memRefreshTokens) Create(_ context.Context, rt *repository.RefreshToken) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tokens[rt.TokenHash] = rt
	return nil
}
func (m *memRefreshTokens) GetByHash(_ context.Context, hash string) (*repository.RefreshToken, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if rt, ok := m.tokens[hash]; ok {
		return rt, nil
	}
	return nil, ErrInvalidToken
}
func (m *memRefreshTokens) DeleteByHash(_ context.Context, hash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.tokens, hash)
	return nil
}
func (m *memRefreshTokens) DeleteByUser(context.Context, uuid.UUID) error { return nil }
func (m *memRefreshTokens) DeleteExpired(context.Context) error           { return nil }

type memPasskeys struct {
	mu          sync.Mutex
	credentials map[uuid.UUID]*domain.PasskeyCredential
	challenges  map[string]*domain.PasskeyChallenge
	refresh     *memRefreshTokens // sessions cascade with their passkey
}

func (m *memPasskeys) Create(_ context.Context, p *domain.PasskeyCredential) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, existing := range m.credentials {
		if bytes.Equal(existing.CredentialID, p.CredentialID) {
			return repository.ErrPasskeyExists
		}
	}
	p.CreatedAt = time.Now()
	copy := *p
	m.credentials[p.ID] = &copy
	return nil
}
func (m *memPasskeys) ListByUser(_ context.Context, userID uuid.UUID) ([]domain.PasskeyCredential, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []domain.PasskeyCredential
	for _, credential := range m.credentials {
		if credential.UserID == userID {
			out = append(out, *credential)
		}
	}
	return out, nil
}
func (m *memPasskeys) GetByCredentialID(_ context.Context, credentialID []byte) (*domain.PasskeyCredential, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, credential := range m.credentials {
		if bytes.Equal(credential.CredentialID, credentialID) {
			copy := *credential
			return &copy, nil
		}
	}
	return nil, nil
}
func (m *memPasskeys) Exists(_ context.Context, id, userID uuid.UUID) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	credential, ok := m.credentials[id]
	return ok && credential.UserID == userID, nil
}
func (m *memPasskeys) Rename(_ context.Context, id, userID uuid.UUID, name string) (*domain.PasskeyCredential, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	credential, ok := m.credentials[id]
	if !ok || credential.UserID != userID {
		return nil, nil
	}
	credential.Name = name
	copy := *credential
	return &copy, nil
}
func (m *memPasskeys) Delete(_ context.Context, id, userID uuid.UUID, keepOne bool) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	credential, ok := m.credentials[id]
	if !ok || credential.UserID != userID {
		return false, nil
	}
	if keepOne {
		others := 0
		for _, other := range m.credentials {
			if other.UserID == userID && other.ID != id {
				others++
			}
		}
		if others == 0 {
			return false, repository.ErrLastPasskey
		}
	}
	delete(m.credentials, id)
	m.refresh.mu.Lock()
	for hash, rt := range m.refresh.tokens {
		if rt.PasskeyCredentialID != nil && *rt.PasskeyCredentialID == id {
			delete(m.refresh.tokens, hash)
		}
	}
	m.refresh.mu.Unlock()
	return true, nil
}
func (m *memPasskeys) RecordUse(_ context.Context, id uuid.UUID, previous, count uint32, backupState bool) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	credential, ok := m.credentials[id]
	if !ok || credential.SignCount != previous {
		return false, nil
	}
	now := time.Now()
	credential.SignCount, credential.BackupState, credential.LastUsedAt = count, backupState, &now
	return true, nil
}
func (m *memPasskeys) SaveChallenge(_ context.Context, ch *domain.PasskeyChallenge, replacesHash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.challenges, replacesHash)
	if ch.UserID != nil {
		for hash, existing := range m.challenges {
			if existing.UserID != nil && *existing.UserID == *ch.UserID && existing.Kind == ch.Kind {
				delete(m.challenges, hash)
			}
		}
	}
	copy := *ch
	m.challenges[ch.HandleHash] = &copy
	return nil
}
func (m *memPasskeys) ConsumeChallenge(_ context.Context, handleHash string) (*domain.PasskeyChallenge, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	challenge := m.challenges[handleHash]
	delete(m.challenges, handleHash)
	return challenge, nil
}

// --- Fixture ---

type passkeyFixture struct {
	t       *testing.T
	svc     *PasskeyService
	auth    *AuthService
	users   *memUsers
	refresh *memRefreshTokens
	store   *memPasskeys
	clock   time.Time
}

func newPasskeyFixture(t *testing.T) *passkeyFixture {
	t.Helper()
	refresh := &memRefreshTokens{tokens: map[string]*repository.RefreshToken{}}
	f := &passkeyFixture{
		t: t, refresh: refresh, clock: time.Now(),
		users: &memUsers{users: map[uuid.UUID]*domain.User{}},
		store: &memPasskeys{credentials: map[uuid.UUID]*domain.PasskeyCredential{}, challenges: map[string]*domain.PasskeyChallenge{}, refresh: refresh},
	}
	f.auth = NewAuthService(f.users, refresh, passkeyTestSecret)
	rp, err := NewPasskeyRelyingParty("", "", "Kuayle Test", passkeyTestOrigin)
	require.NoError(t, err)
	f.svc, err = NewPasskeyService(f.auth, f.users, f.store, rp)
	require.NoError(t, err)
	f.svc.now = func() time.Time { return f.clock }
	return f
}

func (f *passkeyFixture) user(email string) *domain.User {
	f.t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(passkeyTestPassword), bcrypt.MinCost)
	require.NoError(f.t, err)
	user := &domain.User{ID: uuid.New(), Email: email, Name: "Ada", DisplayName: "Ada", PasswordHash: string(hash)}
	require.NoError(f.t, f.users.Create(context.Background(), user))
	return user
}

// register adds a passkey for user from a freshly signed-in session.
func (f *passkeyFixture) register(user *domain.User, authenticator *passkeytest.Authenticator, name string) *domain.PasskeyCredential {
	f.t.Helper()
	ctx := context.Background()
	options, handle, err := f.svc.BeginRegistration(ctx, user.ID, f.clock, "", passkeyTestOrigin)
	require.NoError(f.t, err)
	response, err := authenticator.Create(mustJSON(f.t, options))
	require.NoError(f.t, err)
	passkey, err := f.svc.FinishRegistration(ctx, user.ID, handle, name, response)
	require.NoError(f.t, err)
	return passkey
}

// login runs a whole sign-in and returns the service's result.
func (f *passkeyFixture) login(authenticator *passkeytest.Authenticator) (*domain.User, string, string, error) {
	f.t.Helper()
	ctx := context.Background()
	options, handle, err := f.svc.BeginLogin(ctx, "", passkeyTestOrigin)
	require.NoError(f.t, err)
	response, err := authenticator.Get(mustJSON(f.t, options))
	require.NoError(f.t, err)
	return f.svc.FinishLogin(ctx, handle, response)
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	require.NoError(t, err)
	return data
}

// --- Tests ---

func TestPasskeyRegisterThenSignInWithoutPassword(t *testing.T) {
	f := newPasskeyFixture(t)
	user := f.user("ada@example.test")
	authenticator := passkeytest.New(passkeyTestOrigin)

	passkey := f.register(user, authenticator, "  Laptop   Touch ID ")
	require.Equal(t, "Laptop Touch ID", passkey.Name)
	require.Equal(t, authenticator.CredentialID, passkey.CredentialID)
	require.Equal(t, authenticator.AAGUID[:], passkey.AAGUID)
	require.Equal(t, []string{"internal"}, passkey.Transports)
	require.True(t, passkey.UserVerified)

	signedIn, access, refresh, err := f.login(authenticator)
	require.NoError(t, err)
	require.Equal(t, user.ID, signedIn.ID)

	// The session records the passkey and the sign-in time, like any session
	// it goes through the refresh-token store.
	claims, err := jwtpkg.ValidateAccessToken(access, passkeyTestSecret)
	require.NoError(t, err)
	require.Equal(t, user.ID, claims.UserID)
	require.Equal(t, passkey.ID.String(), claims.PasskeyID)
	require.Equal(t, f.clock.Unix(), claims.AuthTime)
	stored, err := f.refresh.GetByHash(context.Background(), hashToken(refresh))
	require.NoError(t, err)
	require.Equal(t, passkey.ID, *stored.PasskeyCredentialID)

	// Rotation keeps both: refreshing is not a new sign-in.
	f.clock = f.clock.Add(time.Hour)
	access, rotated, err := f.auth.RefreshTokens(context.Background(), refresh)
	require.NoError(t, err)
	claims, err = jwtpkg.ValidateAccessToken(access, passkeyTestSecret)
	require.NoError(t, err)
	require.Equal(t, passkey.ID.String(), claims.PasskeyID)
	require.Equal(t, f.clock.Add(-time.Hour).Unix(), claims.AuthTime)
	_, err = f.refresh.GetByHash(context.Background(), hashToken(refresh))
	require.Error(t, err, "the old refresh token is gone")
	rotatedRow, err := f.refresh.GetByHash(context.Background(), hashToken(rotated))
	require.NoError(t, err)
	require.Equal(t, passkey.ID, *rotatedRow.PasskeyCredentialID)

	credentials, err := f.svc.List(context.Background(), user.ID)
	require.NoError(t, err)
	require.Len(t, credentials, 1)
	require.NotNil(t, credentials[0].LastUsedAt)
	require.EqualValues(t, 1, credentials[0].SignCount)
}

// Password sign-in and its refresh rotation carry the sign-in time and no passkey.
func TestPasswordSessionsRecordSignInTimeAndNoPasskey(t *testing.T) {
	f := newPasskeyFixture(t)
	f.user("ada@example.test")
	before := time.Now().Add(-time.Second)
	_, access, refresh, err := f.auth.Login(context.Background(), dto.LoginRequest{Email: "ada@example.test", Password: passkeyTestPassword})
	require.NoError(t, err)
	claims, err := jwtpkg.ValidateAccessToken(access, passkeyTestSecret)
	require.NoError(t, err)
	require.Empty(t, claims.PasskeyID)
	require.GreaterOrEqual(t, claims.AuthTime, before.Unix())

	access, _, err = f.auth.RefreshTokens(context.Background(), refresh)
	require.NoError(t, err)
	rotated, err := jwtpkg.ValidateAccessToken(access, passkeyTestSecret)
	require.NoError(t, err)
	require.Equal(t, claims.AuthTime, rotated.AuthTime)
	require.Empty(t, rotated.PasskeyID)
}

func TestPasskeyRegistrationNeedsFreshSessionOrPassword(t *testing.T) {
	f := newPasskeyFixture(t)
	user := f.user("ada@example.test")
	ctx := context.Background()
	stale := f.clock.Add(-PasskeyFreshSession - time.Minute)

	for name, authenticatedAt := range map[string]time.Time{"stale session": stale, "session without sign-in time": {}} {
		_, _, err := f.svc.BeginRegistration(ctx, user.ID, authenticatedAt, "", passkeyTestOrigin)
		require.ErrorIs(t, err, ErrPasskeyReauthRequired, name)
		_, _, err = f.svc.BeginRegistration(ctx, user.ID, authenticatedAt, "Wrong-password-123", passkeyTestOrigin)
		require.ErrorIs(t, err, ErrInvalidCredentials, name)
	}
	require.Empty(t, f.store.challenges, "a refused request leaves no challenge behind")

	_, handle, err := f.svc.BeginRegistration(ctx, user.ID, stale, passkeyTestPassword, passkeyTestOrigin)
	require.NoError(t, err)
	require.NotEmpty(t, handle)

	_, _, err = f.svc.BeginRegistration(ctx, user.ID, f.clock.Add(-PasskeyFreshSession+time.Minute), "", passkeyTestOrigin)
	require.NoError(t, err, "a fresh session needs no password")
}

func TestPasskeyChallengesAreSingleUseBoundAndExpire(t *testing.T) {
	ctx := context.Background()

	t.Run("a completed registration cannot be replayed", func(t *testing.T) {
		f := newPasskeyFixture(t)
		user := f.user("ada@example.test")
		authenticator := passkeytest.New(passkeyTestOrigin)
		options, handle, err := f.svc.BeginRegistration(ctx, user.ID, f.clock, "", passkeyTestOrigin)
		require.NoError(t, err)
		response, err := authenticator.Create(mustJSON(t, options))
		require.NoError(t, err)
		_, err = f.svc.FinishRegistration(ctx, user.ID, handle, "Key", response)
		require.NoError(t, err)
		_, err = f.svc.FinishRegistration(ctx, user.ID, handle, "Key", response)
		require.ErrorIs(t, err, ErrPasskeyChallenge)
		require.Len(t, f.store.credentials, 1)
	})

	t.Run("a completed assertion cannot be replayed", func(t *testing.T) {
		f := newPasskeyFixture(t)
		authenticator := passkeytest.New(passkeyTestOrigin)
		f.register(f.user("ada@example.test"), authenticator, "Key")
		options, handle, err := f.svc.BeginLogin(ctx, "", passkeyTestOrigin)
		require.NoError(t, err)
		response, err := authenticator.Get(mustJSON(t, options))
		require.NoError(t, err)
		_, _, _, err = f.svc.FinishLogin(ctx, handle, response)
		require.NoError(t, err)
		_, _, _, err = f.svc.FinishLogin(ctx, handle, response)
		require.ErrorIs(t, err, ErrPasskeyChallenge)

		// Nor against a challenge of another attempt: the signature covers the first one.
		_, other, err := f.svc.BeginLogin(ctx, "", passkeyTestOrigin)
		require.NoError(t, err)
		_, _, _, err = f.svc.FinishLogin(ctx, other, response)
		require.ErrorIs(t, err, ErrPasskeyVerification)
		require.Len(t, f.refresh.tokens, 1, "only the first completion opened a session")
	})

	t.Run("a failed attempt burns its challenge", func(t *testing.T) {
		f := newPasskeyFixture(t)
		authenticator := passkeytest.New(passkeyTestOrigin)
		f.register(f.user("ada@example.test"), authenticator, "Key")
		options, handle, err := f.svc.BeginLogin(ctx, "", passkeyTestOrigin)
		require.NoError(t, err)
		_, _, _, err = f.svc.FinishLogin(ctx, handle, []byte(`{"not":"a credential"}`))
		require.ErrorIs(t, err, ErrPasskeyVerification)
		response, err := authenticator.Get(mustJSON(t, options))
		require.NoError(t, err)
		_, _, _, err = f.svc.FinishLogin(ctx, handle, response)
		require.ErrorIs(t, err, ErrPasskeyChallenge)
		require.Empty(t, f.store.challenges)
	})

	t.Run("challenges expire", func(t *testing.T) {
		f := newPasskeyFixture(t)
		user := f.user("ada@example.test")
		authenticator := passkeytest.New(passkeyTestOrigin)
		options, handle, err := f.svc.BeginRegistration(ctx, user.ID, f.clock, "", passkeyTestOrigin)
		require.NoError(t, err)
		response, err := authenticator.Create(mustJSON(t, options))
		require.NoError(t, err)
		f.clock = f.clock.Add(PasskeyChallengeTTL + time.Second)
		_, err = f.svc.FinishRegistration(ctx, user.ID, handle, "Key", response)
		require.ErrorIs(t, err, ErrPasskeyChallenge)
		require.Empty(t, f.store.credentials)
	})

	t.Run("a registration belongs to the user who started it", func(t *testing.T) {
		f := newPasskeyFixture(t)
		ada, mallory := f.user("ada@example.test"), f.user("mallory@example.test")
		authenticator := passkeytest.New(passkeyTestOrigin)
		options, handle, err := f.svc.BeginRegistration(ctx, ada.ID, f.clock, "", passkeyTestOrigin)
		require.NoError(t, err)
		response, err := authenticator.Create(mustJSON(t, options))
		require.NoError(t, err)
		_, err = f.svc.FinishRegistration(ctx, mallory.ID, handle, "Key", response)
		require.ErrorIs(t, err, ErrPasskeyChallenge)
		_, err = f.svc.FinishRegistration(ctx, ada.ID, handle, "Key", response)
		require.ErrorIs(t, err, ErrPasskeyChallenge, "the stolen attempt consumed the challenge")
		require.Empty(t, f.store.credentials)
	})

	t.Run("a new attempt discards the previous one", func(t *testing.T) {
		f := newPasskeyFixture(t)
		user := f.user("ada@example.test")
		authenticator := passkeytest.New(passkeyTestOrigin)
		options, first, err := f.svc.BeginRegistration(ctx, user.ID, f.clock, "", passkeyTestOrigin)
		require.NoError(t, err)
		response, err := authenticator.Create(mustJSON(t, options))
		require.NoError(t, err)
		_, _, err = f.svc.BeginRegistration(ctx, user.ID, f.clock, "", passkeyTestOrigin)
		require.NoError(t, err)
		require.Len(t, f.store.challenges, 1)
		_, err = f.svc.FinishRegistration(ctx, user.ID, first, "Key", response)
		require.ErrorIs(t, err, ErrPasskeyChallenge)

		_, firstLogin, err := f.svc.BeginLogin(ctx, "", passkeyTestOrigin)
		require.NoError(t, err)
		_, _, err = f.svc.BeginLogin(ctx, firstLogin, passkeyTestOrigin)
		require.NoError(t, err)
		require.NotContains(t, f.store.challenges, hashToken(firstLogin))
	})

	t.Run("a login challenge cannot complete a registration and vice versa", func(t *testing.T) {
		f := newPasskeyFixture(t)
		user := f.user("ada@example.test")
		authenticator := passkeytest.New(passkeyTestOrigin)
		f.register(user, authenticator, "Key")
		loginOptions, loginHandle, err := f.svc.BeginLogin(ctx, "", passkeyTestOrigin)
		require.NoError(t, err)
		created, err := passkeytest.New(passkeyTestOrigin).Create(mustJSON(t, map[string]any{"publicKey": map[string]any{
			"challenge": loginOptions.Response.Challenge.String(), "rp": map[string]string{"id": "kuayle.example.test"},
			"user": map[string]string{"id": "AAAA"},
		}}))
		require.NoError(t, err)
		_, err = f.svc.FinishRegistration(ctx, user.ID, loginHandle, "Key", created)
		require.ErrorIs(t, err, ErrPasskeyChallenge)

		options, registrationHandle, err := f.svc.BeginRegistration(ctx, user.ID, f.clock, "", passkeyTestOrigin)
		require.NoError(t, err)
		assertion, err := authenticator.Get(mustJSON(t, map[string]any{"publicKey": map[string]any{
			"challenge": options.Response.Challenge.String(), "rpId": "kuayle.example.test",
		}}))
		require.NoError(t, err)
		_, _, _, err = f.svc.FinishLogin(ctx, registrationHandle, assertion)
		require.ErrorIs(t, err, ErrPasskeyChallenge)
	})

	t.Run("no handle, no ceremony", func(t *testing.T) {
		f := newPasskeyFixture(t)
		_, _, _, err := f.svc.FinishLogin(ctx, "", []byte(`{}`))
		require.ErrorIs(t, err, ErrPasskeyChallenge)
	})
}

func TestPasskeySignCounterRegressionIsRejected(t *testing.T) {
	f := newPasskeyFixture(t)
	user := f.user("ada@example.test")
	authenticator := passkeytest.New(passkeyTestOrigin)
	passkey := f.register(user, authenticator, "Security key")
	clone := authenticator.Clone() // same key, counter still at 0

	for i := 0; i < 2; i++ {
		_, _, _, err := f.login(authenticator)
		require.NoError(t, err)
	}
	require.EqualValues(t, 2, f.store.credentials[passkey.ID].SignCount)
	sessions := len(f.refresh.tokens)

	// The clone signs with counter 1 and then 2: neither is above the stored 2.
	for i := 0; i < 2; i++ {
		_, _, _, err := f.login(clone)
		require.ErrorIs(t, err, ErrPasskeyCloneSuspected)
		var rejection *PasskeyRejection
		require.ErrorAs(t, err, &rejection)
		require.Equal(t, user.ID, rejection.UserID)
		require.Equal(t, passkey.ID, rejection.PasskeyID)
	}
	require.EqualValues(t, 2, f.store.credentials[passkey.ID].SignCount, "a rejected assertion does not move the counter")
	require.Len(t, f.refresh.tokens, sessions, "and opens no session")

	_, _, _, err := f.login(authenticator)
	require.NoError(t, err, "the genuine authenticator keeps working")
}

// Synced passkeys never count; that is not a regression.
func TestSyncedPasskeyWithoutCounterSignsInRepeatedly(t *testing.T) {
	f := newPasskeyFixture(t)
	authenticator := passkeytest.New(passkeyTestOrigin)
	authenticator.Synced = true
	passkey := f.register(f.user("ada@example.test"), authenticator, "Phone")
	require.True(t, passkey.BackupEligible)
	for i := 0; i < 3; i++ {
		_, _, _, err := f.login(authenticator)
		require.NoError(t, err)
	}
	require.Zero(t, f.store.credentials[passkey.ID].SignCount)
}

func TestPasskeyUnknownOrRemovedCredentialIsRejected(t *testing.T) {
	ctx := context.Background()
	f := newPasskeyFixture(t)
	user := f.user("ada@example.test")

	// A credential the server never saw.
	stranger := passkeytest.New(passkeyTestOrigin)
	_, err := stranger.Create(mustJSON(t, map[string]any{"publicKey": map[string]any{
		"challenge": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", "rp": map[string]string{"id": "kuayle.example.test"},
		"user": map[string]string{"id": "AAAA"},
	}}))
	require.NoError(t, err)
	_, _, _, err = f.login(stranger)
	require.ErrorIs(t, err, ErrPasskeyUnknownCredential)

	// A passkey removed by another session while this one is mid-ceremony.
	authenticator := passkeytest.New(passkeyTestOrigin)
	passkey := f.register(user, authenticator, "Key")
	options, handle, err := f.svc.BeginLogin(ctx, "", passkeyTestOrigin)
	require.NoError(t, err)
	response, err := authenticator.Get(mustJSON(t, options))
	require.NoError(t, err)
	require.NoError(t, f.svc.Delete(ctx, user.ID, passkey.ID))
	_, _, _, err = f.svc.FinishLogin(ctx, handle, response)
	require.ErrorIs(t, err, ErrPasskeyUnknownCredential)
	require.Empty(t, f.refresh.tokens)
}

func TestPasskeySignInWithoutUserHandle(t *testing.T) {
	f := newPasskeyFixture(t)
	ada, grace := f.user("ada@example.test"), f.user("grace@example.test")
	authenticator := passkeytest.New(passkeyTestOrigin)
	f.register(ada, authenticator, "Key")

	authenticator.OmitUserHandle = true
	signedIn, _, _, err := f.login(authenticator)
	require.NoError(t, err)
	require.Equal(t, ada.ID, signedIn.ID, "the credential ID alone identifies the account")

	// A user handle naming someone else never selects that account.
	authenticator.OmitUserHandle = false
	authenticator.UserHandle = grace.ID[:]
	_, _, _, err = f.login(authenticator)
	require.ErrorIs(t, err, ErrPasskeyUnknownCredential)
}

func TestPasskeyAttestationFromUnknownAuthenticators(t *testing.T) {
	for _, attestation := range []string{"none", "packed"} {
		t.Run(attestation, func(t *testing.T) {
			f := newPasskeyFixture(t)
			authenticator := passkeytest.New(passkeyTestOrigin) // random AAGUID, in no metadata service
			authenticator.Attestation = attestation
			passkey := f.register(f.user("ada@example.test"), authenticator, "Key")
			require.Equal(t, authenticator.AAGUID[:], passkey.AAGUID)
			require.Equal(t, attestation, passkey.AttestationFormat)
			_, _, _, err := f.login(authenticator)
			require.NoError(t, err)
		})
	}

	t.Run("an all-zero AAGUID", func(t *testing.T) {
		f := newPasskeyFixture(t)
		authenticator := passkeytest.New(passkeyTestOrigin)
		authenticator.AAGUID = [16]byte{}
		f.register(f.user("ada@example.test"), authenticator, "Key")
		_, _, _, err := f.login(authenticator)
		require.NoError(t, err)
	})
}

func TestPasskeyRequiresUserVerification(t *testing.T) {
	ctx := context.Background()
	f := newPasskeyFixture(t)
	user := f.user("ada@example.test")

	touchOnly := passkeytest.New(passkeyTestOrigin)
	touchOnly.UserVerified = false
	options, handle, err := f.svc.BeginRegistration(ctx, user.ID, f.clock, "", passkeyTestOrigin)
	require.NoError(t, err)
	require.Equal(t, "required", string(options.Response.AuthenticatorSelection.UserVerification))
	require.Equal(t, "required", string(options.Response.AuthenticatorSelection.ResidentKey), "passkeys are discoverable")
	response, err := touchOnly.Create(mustJSON(t, options))
	require.NoError(t, err)
	_, err = f.svc.FinishRegistration(ctx, user.ID, handle, "Key", response)
	require.ErrorIs(t, err, ErrPasskeyVerification)

	authenticator := passkeytest.New(passkeyTestOrigin)
	f.register(user, authenticator, "Key")
	authenticator.UserVerified = false
	_, _, _, err = f.login(authenticator)
	require.ErrorIs(t, err, ErrPasskeyVerification)
}

func TestPasskeyOriginAndRelyingPartyMismatch(t *testing.T) {
	ctx := context.Background()
	f := newPasskeyFixture(t)
	user := f.user("ada@example.test")

	// The browser says where it is: a ceremony from an address the relying
	// party does not cover is refused before it starts.
	_, _, err := f.svc.BeginLogin(ctx, "", "https://internal-name.example.test")
	require.ErrorIs(t, err, ErrPasskeyOrigin)
	_, _, err = f.svc.BeginRegistration(ctx, user.ID, f.clock, "", "http://kuayle.example.test")
	require.ErrorIs(t, err, ErrPasskeyOrigin)
	require.Empty(t, f.store.challenges)
	// Requests without an Origin header (same-origin GET-like clients, tests) start normally.
	_, _, err = f.svc.BeginLogin(ctx, "", "")
	require.NoError(t, err)

	// What is verified is the origin the authenticator response was made for.
	phishing := passkeytest.New("https://kuayle.example.test.evil.test")
	options, handle, err := f.svc.BeginRegistration(ctx, user.ID, f.clock, "", passkeyTestOrigin)
	require.NoError(t, err)
	response, err := phishing.Create(mustJSON(t, options))
	require.NoError(t, err)
	_, err = f.svc.FinishRegistration(ctx, user.ID, handle, "Key", response)
	require.ErrorIs(t, err, ErrPasskeyOrigin)

	authenticator := passkeytest.New(passkeyTestOrigin)
	f.register(user, authenticator, "Key")
	authenticator.Origin = "https://kuayle.example.test.evil.test"
	_, _, _, err = f.login(authenticator)
	require.ErrorIs(t, err, ErrPasskeyOrigin)

	// Right origin, credential scoped to another relying party.
	authenticator.Origin = passkeyTestOrigin
	authenticator.RPID = "other.example.test"
	_, _, _, err = f.login(authenticator)
	require.ErrorIs(t, err, ErrPasskeyVerification)
}

func TestPasskeyCannotBeRegisteredTwice(t *testing.T) {
	ctx := context.Background()
	f := newPasskeyFixture(t)
	ada, grace := f.user("ada@example.test"), f.user("grace@example.test")
	authenticator := passkeytest.New(passkeyTestOrigin)
	f.register(ada, authenticator, "Key")

	options, _, err := f.svc.BeginRegistration(ctx, ada.ID, f.clock, "", passkeyTestOrigin)
	require.NoError(t, err)
	require.Len(t, options.Response.CredentialExcludeList, 1, "the browser is told which authenticators are already registered")
	require.Equal(t, authenticator.CredentialID, []byte(options.Response.CredentialExcludeList[0].CredentialID))

	// The same credential ID offered for another account is refused by the store.
	stored := f.store.credentials
	for _, credential := range stored {
		duplicate := *credential
		duplicate.ID, duplicate.UserID = uuid.New(), grace.ID
		require.ErrorIs(t, f.store.Create(ctx, &duplicate), repository.ErrPasskeyExists)
	}
}

func TestPasskeyRenameAndDelete(t *testing.T) {
	ctx := context.Background()
	f := newPasskeyFixture(t)
	ada, grace := f.user("ada@example.test"), f.user("grace@example.test")
	first, second := passkeytest.New(passkeyTestOrigin), passkeytest.New(passkeyTestOrigin)
	laptop, phone := f.register(ada, first, "Laptop"), f.register(ada, second, "Phone")

	renamed, err := f.svc.Rename(ctx, ada.ID, laptop.ID, " Work laptop ")
	require.NoError(t, err)
	require.Equal(t, "Work laptop", renamed.Name)
	_, err = f.svc.Rename(ctx, ada.ID, laptop.ID, "   ")
	require.ErrorIs(t, err, ErrPasskeyName)
	_, err = f.svc.Rename(ctx, ada.ID, laptop.ID, string(bytes.Repeat([]byte("x"), 65)))
	require.ErrorIs(t, err, ErrPasskeyName)
	_, err = f.svc.Rename(ctx, grace.ID, laptop.ID, "Mine now")
	require.ErrorIs(t, err, ErrPasskeyNotFound, "another user's passkey does not exist for the caller")
	require.ErrorIs(t, f.svc.Delete(ctx, grace.ID, laptop.ID), ErrPasskeyNotFound)

	// Three sessions: one per passkey and one by password.
	_, _, laptopRefresh, err := f.login(first)
	require.NoError(t, err)
	_, _, phoneRefresh, err := f.login(second)
	require.NoError(t, err)
	_, _, passwordRefresh, err := f.auth.Login(ctx, dto.LoginRequest{Email: ada.Email, Password: passkeyTestPassword})
	require.NoError(t, err)

	require.NoError(t, f.svc.Delete(ctx, ada.ID, laptop.ID))
	_, _, err = f.auth.RefreshTokens(ctx, laptopRefresh)
	require.ErrorIs(t, err, ErrInvalidToken, "the removed passkey's session is over")
	_, _, err = f.auth.RefreshTokens(ctx, phoneRefresh)
	require.NoError(t, err, "the other passkey's session continues")
	_, _, err = f.auth.RefreshTokens(ctx, passwordRefresh)
	require.NoError(t, err, "and so does the password session")
	exists, err := f.svc.SessionPasskeyExists(ctx, laptop.ID, ada.ID)
	require.NoError(t, err)
	require.False(t, exists)
	exists, err = f.svc.SessionPasskeyExists(ctx, phone.ID, grace.ID)
	require.NoError(t, err)
	require.False(t, exists, "a passkey only vouches for its owner")

	_, _, _, err = f.login(first)
	require.ErrorIs(t, err, ErrPasskeyUnknownCredential)

	// With a password the last passkey can go.
	require.NoError(t, f.svc.Delete(ctx, ada.ID, phone.ID))
	require.Empty(t, f.store.credentials)
}

// An account whose password cannot be used must keep one passkey, or nobody
// could sign in to it again.
func TestPasswordlessAccountKeepsItsLastPasskey(t *testing.T) {
	ctx := context.Background()
	f := newPasskeyFixture(t)
	user := f.user("imported@example.test")
	first, second := passkeytest.New(passkeyTestOrigin), passkeytest.New(passkeyTestOrigin)
	laptop, phone := f.register(user, first, "Laptop"), f.register(user, second, "Phone")
	user.PasswordHash = "!imported"
	require.False(t, HasUsablePassword(user))

	require.NoError(t, f.svc.Delete(ctx, user.ID, laptop.ID), "one of two can go")
	require.ErrorIs(t, f.svc.Delete(ctx, user.ID, phone.ID), ErrLastPasskey)
	require.Len(t, f.store.credentials, 1)

	// Without a password, adding a passkey from an old session is impossible:
	// the user signs in again with the passkey they have.
	stale := f.clock.Add(-time.Hour)
	_, _, err := f.svc.BeginRegistration(ctx, user.ID, stale, "anything-At-all-1", passkeyTestOrigin)
	require.ErrorIs(t, err, ErrInvalidCredentials)
}

func TestPasskeyLimitPerAccount(t *testing.T) {
	f := newPasskeyFixture(t)
	user := f.user("ada@example.test")
	for i := 0; i < MaxPasskeysPerUser; i++ {
		f.register(user, passkeytest.New(passkeyTestOrigin), "Key")
	}
	_, _, err := f.svc.BeginRegistration(context.Background(), user.ID, f.clock, "", passkeyTestOrigin)
	require.ErrorIs(t, err, ErrPasskeyLimit)
}

func TestPasskeysDisabledWithoutRelyingParty(t *testing.T) {
	ctx := context.Background()
	f := newPasskeyFixture(t)
	user := f.user("ada@example.test")
	authenticator := passkeytest.New(passkeyTestOrigin)
	passkey := f.register(user, authenticator, "Key")

	disabled, err := NewPasskeyService(f.auth, f.users, f.store, nil)
	require.NoError(t, err)
	require.False(t, disabled.Enabled())
	_, _, err = disabled.BeginLogin(ctx, "", "")
	require.ErrorIs(t, err, ErrPasskeysDisabled)
	_, _, _, err = disabled.FinishLogin(ctx, "handle", []byte(`{}`))
	require.ErrorIs(t, err, ErrPasskeysDisabled)
	_, _, err = disabled.BeginRegistration(ctx, user.ID, time.Now(), "", "")
	require.ErrorIs(t, err, ErrPasskeysDisabled)
	_, err = disabled.FinishRegistration(ctx, user.ID, "handle", "Key", []byte(`{}`))
	require.ErrorIs(t, err, ErrPasskeysDisabled)

	// Stored passkeys can still be seen and removed.
	listed, err := disabled.List(ctx, user.ID)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.NoError(t, disabled.Delete(ctx, user.ID, passkey.ID))
}

func TestPasskeyRelyingPartyConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		rpID, origins, frontend string
		wantID                  string
		wantOrigins             []string
		wantErr                 string
	}{
		{name: "defaults to the frontend URL", frontend: "https://app.example.com/", wantID: "app.example.com", wantOrigins: []string{"https://app.example.com"}},
		{name: "local development over http", frontend: "http://localhost:5173", wantID: "localhost", wantOrigins: []string{"http://localhost:5173"}},
		{name: "localhost subdomain", frontend: "http://kuayle.localhost:8080", wantID: "kuayle.localhost", wantOrigins: []string{"http://kuayle.localhost:8080"}},
		{name: "default ports are dropped and case folded", frontend: "HTTPS://App.Example.com:443", wantID: "app.example.com", wantOrigins: []string{"https://app.example.com"}},
		{name: "explicit parent domain and several origins", rpID: "example.com", origins: "https://app.example.com, https://kuayle.example.com:8443", frontend: "http://ignored.test",
			wantID: "example.com", wantOrigins: []string{"https://app.example.com", "https://kuayle.example.com:8443"}},
		{name: "plain http outside localhost", frontend: "http://kuayle.internal:8080", wantErr: "plain http outside localhost"},
		{name: "plain http on a LAN address", frontend: "http://192.168.1.20:5173", wantErr: "plain http outside localhost"},
		{name: "one plain http origin among several", origins: "https://app.example.com,http://app.example.com", wantErr: "plain http outside localhost"},
		{name: "an IP address cannot be a relying party", frontend: "https://203.0.113.7", wantErr: "IP address"},
		{name: "origin outside the relying party", rpID: "example.com", origins: "https://app.example.org", wantErr: "outside the relying party"},
		{name: "suffix that is not a parent domain", rpID: "ample.com", origins: "https://example.com", wantErr: "outside the relying party"},
		{name: "a public suffix as relying party", rpID: "co.uk", origins: "https://app.co.uk", wantErr: "public suffix"},
		{name: "origin with a path", origins: "https://app.example.com/kuayle", wantErr: "scheme://host"},
		{name: "not a URL", origins: "app.example.com", wantErr: "absolute URL"},
		{name: "another scheme", origins: "ftp://app.example.com", wantErr: "https"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rp, err := NewPasskeyRelyingParty(tc.rpID, tc.origins, "", tc.frontend)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantID, rp.ID)
			require.Equal(t, tc.wantOrigins, rp.Origins)
			require.Equal(t, "Kuayle", rp.DisplayName)
			_, err = NewPasskeyService(nil, nil, nil, rp)
			require.NoError(t, err, "the WebAuthn library accepts the same configuration")
		})
	}
}
