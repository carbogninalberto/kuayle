package service

import (
	"context"
	"database/sql"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type fakeResetRepo struct {
	issued time.Time
	tokens map[string]bool // hash -> usable
	hashes []string
}

func (f *fakeResetRepo) Create(_ context.Context, t *repository.PasswordResetToken) error {
	f.issued = time.Now()
	f.tokens = map[string]bool{t.TokenHash: true}
	f.hashes = append(f.hashes, t.TokenHash)
	return nil
}

func (f *fakeResetRepo) LastIssuedAt(context.Context, uuid.UUID) (time.Time, error) {
	return f.issued, nil
}

func (f *fakeResetRepo) Reset(_ context.Context, tokenHash, _ string) error {
	if !f.tokens[tokenHash] {
		return sql.ErrNoRows
	}
	f.tokens[tokenHash] = false
	return nil
}

type fakeMailer struct{ to, body string }

func (f *fakeMailer) Send(to, _, body string) error { f.to, f.body = to, body; return nil }

func TestPasswordResetFlow(t *testing.T) {
	ctx := context.Background()
	users := new(mockUserRepo)
	user := &domain.User{ID: uuid.New(), Email: "a@example.com"}
	users.On("GetByEmail", mock.Anything, "a@example.com").Return(user, nil)
	users.On("GetByEmail", mock.Anything, "none@example.com").Return(nil, nil)
	repo := &fakeResetRepo{}
	mailer := &fakeMailer{}
	svc := NewAuthService(users, new(mockRefreshTokenRepo), "secret", WithPasswordReset(repo, mailer, "http://app.test/", false))

	svc.async = func(f func()) { f() }

	require.NoError(t, svc.RequestPasswordReset(ctx, "none@example.com"))
	assert.Empty(t, mailer.to, "unknown emails must not receive mail")
	assert.Empty(t, repo.hashes)

	require.NoError(t, svc.RequestPasswordReset(ctx, "a@example.com"))
	require.Equal(t, "a@example.com", mailer.to)
	require.NoError(t, svc.RequestPasswordReset(ctx, "a@example.com"))
	assert.Len(t, repo.hashes, 1, "second request within the cooldown mints no token")

	idx := strings.Index(mailer.body, "http://app.test/reset-password?token=")
	require.GreaterOrEqual(t, idx, 0)
	link := strings.Fields(mailer.body[idx:])[0]
	u, err := url.Parse(link)
	require.NoError(t, err)
	token := u.Query().Get("token")
	assert.NotContains(t, repo.hashes[0], token, "only the hash is stored")

	assert.ErrorIs(t, svc.ResetPassword(ctx, token, "weakpassword1"), ErrWeakPassword)
	assert.ErrorIs(t, svc.ResetPassword(ctx, "wrong", "Str0ngPassw0rd!"), ErrInvalidToken)
	require.NoError(t, svc.ResetPassword(ctx, token, "Str0ngPassw0rd!"))
	assert.ErrorIs(t, svc.ResetPassword(ctx, token, "Str0ngPassw0rd!"), ErrInvalidToken, "token is single use")
}
