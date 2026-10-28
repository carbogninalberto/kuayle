package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	cryptoutil "github.com/kuayle/kuayle-backend/pkg/crypto"
	"github.com/stretchr/testify/require"
)

// signInStoreFake keeps sign-ins per workspace, user, agent and kind.
type signInStoreFake struct {
	repository.DevMachineStore
	rows map[string]signInRow
}

type signInRow struct {
	encrypted string
	info      domain.AgentSignIn
}

func signInKey(ws, user uuid.UUID, provider, kind string) string {
	return ws.String() + "/" + user.String() + "/" + provider + "/" + kind
}

func (f *signInStoreFake) ListAgentSignIns(_ context.Context, ws, user uuid.UUID) ([]domain.AgentSignIn, error) {
	signIns := []domain.AgentSignIn{}
	for key, row := range f.rows {
		if strings.HasPrefix(key, ws.String()+"/"+user.String()+"/") {
			signIns = append(signIns, row.info)
		}
	}
	return signIns, nil
}

func (f *signInStoreFake) GetAgentSignInToken(_ context.Context, ws, user uuid.UUID, provider string) (string, error) {
	return f.rows[signInKey(ws, user, provider, domain.AgentSignInToken)].encrypted, nil
}

func (f *signInStoreFake) SaveAgentSignInToken(_ context.Context, ws, user uuid.UUID, provider, encrypted string, expires time.Time) error {
	if f.rows == nil {
		f.rows = map[string]signInRow{}
	}
	now := time.Now()
	f.rows[signInKey(ws, user, provider, domain.AgentSignInToken)] = signInRow{encrypted, domain.AgentSignIn{Provider: provider, Kind: domain.AgentSignInToken, CreatedAt: now, UpdatedAt: now, ExpiresAt: &expires}}
	return nil
}

func (f *signInStoreFake) DeleteAgentSignIns(_ context.Context, ws, user uuid.UUID, provider, kind string) (int64, error) {
	var removed int64
	for _, candidate := range []string{domain.AgentSignInToken, domain.AgentSignInCredentials} {
		key := signInKey(ws, user, provider, candidate)
		if _, ok := f.rows[key]; ok && (kind == "" || kind == candidate) {
			delete(f.rows, key)
			removed++
		}
	}
	return removed, nil
}

const tokenFixture = "sk-ant-oat01-Fixture_Token-Value-0001"

func TestClaudeCodeTokenIsEncryptedAndNeverReturned(t *testing.T) {
	ws, user := uuid.New(), uuid.New()
	store := &signInStoreFake{}
	svc := newTestDevMachineService(store)

	saved, err := svc.SaveAgentSignInToken(context.Background(), ws, user, "claude-code", "  "+tokenFixture+"\n")
	require.NoError(t, err, "surrounding whitespace from a paste is trimmed")
	require.NotNil(t, saved.Token)
	require.WithinDuration(t, time.Now().Add(365*24*time.Hour), *saved.Token.ExpiresAt, time.Minute, "setup-token tokens last a year")
	row := store.rows[signInKey(ws, user, "claude-code", domain.AgentSignInToken)]
	require.NotContains(t, row.encrypted, tokenFixture)
	decrypted, err := cryptoutil.Decrypt(row.encrypted, cryptoutil.DeriveKey("test"))
	require.NoError(t, err)
	require.Equal(t, tokenFixture, decrypted)
	has, err := svc.HasAgentSignInToken(context.Background(), ws, user, "claude-code")
	require.NoError(t, err)
	require.True(t, has)

	listed, err := svc.ListAgentSignIns(context.Background(), ws, user)
	require.NoError(t, err)
	require.Len(t, listed, 2, "every agent with a sign-in is listed")
	require.Equal(t, "claude-code", listed[0].Provider)
	require.True(t, listed[0].SupportsToken)
	require.NotNil(t, listed[0].Token)
	require.Nil(t, listed[0].Terminal)
	require.Equal(t, "codex", listed[1].Provider)
	require.False(t, listed[1].SupportsToken)
	for _, value := range []any{saved, listed} {
		encoded, err := json.Marshal(value)
		require.NoError(t, err)
		require.NotContains(t, string(encoded), tokenFixture)
		require.NotContains(t, string(encoded), "encrypted")
	}

	other, err := svc.ListAgentSignIns(context.Background(), ws, uuid.New())
	require.NoError(t, err)
	require.Nil(t, other[0].Token, "another user's token is not theirs")

	require.NoError(t, svc.DeleteAgentSignIn(context.Background(), ws, user, "claude-code", domain.AgentSignInToken))
	has, err = svc.HasAgentSignInToken(context.Background(), ws, user, "claude-code")
	require.NoError(t, err)
	require.False(t, has)
}

func TestClaudeCodeTokenRejectsInvalidValues(t *testing.T) {
	svc := newTestDevMachineService(&signInStoreFake{})
	for name, value := range map[string]string{
		"empty":            "   ",
		"inner whitespace": "sk-ant-oat01-abc def-0000000000",
		"control":          "sk-ant-oat01-abc\x00def-000000000",
		"non-ascii":        "sk-ant-oat01-abcdé-00000000000",
		"too short":        "sk-ant",
		"too long":         "sk-ant-oat01-" + strings.Repeat("a", 1024),
		"multiline":        "sk-ant-oat01-aaaaaaaaaaaaaaa\nsecond-line-xxxxxxxx",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := svc.SaveAgentSignInToken(context.Background(), uuid.New(), uuid.New(), "claude-code", value)
			require.ErrorIs(t, err, ErrInvalidMachineInput)
		})
	}
	_, err := svc.SaveAgentSignInToken(context.Background(), uuid.New(), uuid.New(), "codex", tokenFixture)
	require.ErrorIs(t, err, ErrInvalidMachineInput, "Codex has no token sign-in")
	require.ErrorIs(t, svc.DeleteAgentSignIn(context.Background(), uuid.New(), uuid.New(), "goose", ""), ErrInvalidMachineInput)
	require.ErrorIs(t, svc.DeleteAgentSignIn(context.Background(), uuid.New(), uuid.New(), "codex", "password"), ErrInvalidMachineInput)
}

// Continuing after sign-in skips the terminal capture only for the creator of
// a task stopped for sign-in who has saved a token for its agent.
func TestContinueAfterSignInUsesOnlyTheCreatorsOwnToken(t *testing.T) {
	ws, creator := uuid.New(), uuid.New()
	store := &signInStoreFake{}
	machines := newTestDevMachineService(store)
	svc := &AgentTaskService{machines: machines}
	task := &domain.AgentTask{WorkspaceID: ws, CreatorID: creator, Provider: "claude-code", State: "failed", SignInRequired: true}
	as := func(user uuid.UUID) context.Context {
		return domain.WithAccess(context.Background(), domain.Access{WorkspaceID: ws, UserID: user})
	}

	ok, err := svc.creatorSavedToken(as(creator), ws, task)
	require.NoError(t, err)
	require.False(t, ok, "no token saved yet: capture from the terminal")
	_, err = machines.SaveAgentSignInToken(context.Background(), ws, creator, "claude-code", tokenFixture)
	require.NoError(t, err)
	ok, err = svc.creatorSavedToken(as(creator), ws, task)
	require.NoError(t, err)
	require.True(t, ok)

	other := uuid.New()
	_, err = machines.SaveAgentSignInToken(context.Background(), ws, other, "claude-code", tokenFixture)
	require.NoError(t, err)
	ok, err = svc.creatorSavedToken(as(other), ws, task)
	require.NoError(t, err)
	require.False(t, ok, "another user's token never continues someone else's task")
	ok, err = svc.creatorSavedToken(context.Background(), ws, task)
	require.NoError(t, err)
	require.False(t, ok, "no caller")

	for name, change := range map[string]func(*domain.AgentTask){
		"not stopped for sign-in": func(task *domain.AgentTask) { task.SignInRequired = false },
		"still running":           func(task *domain.AgentTask) { task.State = "running" },
		"codex":                   func(task *domain.AgentTask) { task.Provider = "codex" },
	} {
		copy := *task
		change(&copy)
		ok, err = svc.creatorSavedToken(as(creator), ws, &copy)
		require.NoError(t, err)
		require.False(t, ok, name)
	}
}
