package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/carbon/carbon-backend/internal/domain"
	"github.com/carbon/carbon-backend/internal/dto"
	"github.com/carbon/carbon-backend/internal/repository"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"golang.org/x/crypto/bcrypt"
)

// --- Mocks ---

type mockUserRepo struct {
	mock.Mock
}

func (m *mockUserRepo) Create(ctx context.Context, user *domain.User) error {
	args := m.Called(ctx, user)
	return args.Error(0)
}

func (m *mockUserRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.User), args.Error(1)
}

func (m *mockUserRepo) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	args := m.Called(ctx, email)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.User), args.Error(1)
}

type mockRefreshTokenRepo struct {
	mock.Mock
}

func (m *mockRefreshTokenRepo) Create(ctx context.Context, rt *repository.RefreshToken) error {
	args := m.Called(ctx, rt)
	return args.Error(0)
}

func (m *mockRefreshTokenRepo) GetByHash(ctx context.Context, hash string) (*repository.RefreshToken, error) {
	args := m.Called(ctx, hash)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*repository.RefreshToken), args.Error(1)
}

func (m *mockRefreshTokenRepo) DeleteByHash(ctx context.Context, hash string) error {
	args := m.Called(ctx, hash)
	return args.Error(0)
}

func (m *mockRefreshTokenRepo) DeleteByUser(ctx context.Context, userID uuid.UUID) error {
	args := m.Called(ctx, userID)
	return args.Error(0)
}

func (m *mockRefreshTokenRepo) DeleteExpired(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

// --- Tests ---

func TestRegister_Happy(t *testing.T) {
	userRepo := new(mockUserRepo)
	refreshRepo := new(mockRefreshTokenRepo)
	svc := NewAuthService(userRepo, refreshRepo, "test-secret")

	ctx := context.Background()
	req := dto.RegisterRequest{
		Email:    "test@example.com",
		Password: "password123",
		Name:     "Test User",
	}

	userRepo.On("GetByEmail", ctx, "test@example.com").Return(nil, nil)
	userRepo.On("Create", ctx, mock.AnythingOfType("*domain.User")).Return(nil)
	refreshRepo.On("Create", ctx, mock.AnythingOfType("*repository.RefreshToken")).Return(nil)

	user, accessToken, refreshToken, err := svc.Register(ctx, req)

	assert.NoError(t, err)
	assert.NotNil(t, user)
	assert.Equal(t, "test@example.com", user.Email)
	assert.Equal(t, "Test User", user.Name)
	assert.NotEmpty(t, accessToken)
	assert.NotEmpty(t, refreshToken)

	userRepo.AssertExpectations(t)
	refreshRepo.AssertExpectations(t)
}

func TestRegister_DuplicateEmail(t *testing.T) {
	userRepo := new(mockUserRepo)
	refreshRepo := new(mockRefreshTokenRepo)
	svc := NewAuthService(userRepo, refreshRepo, "test-secret")

	ctx := context.Background()
	req := dto.RegisterRequest{
		Email:    "existing@example.com",
		Password: "password123",
		Name:     "Test User",
	}

	existingUser := &domain.User{
		ID:    uuid.New(),
		Email: "existing@example.com",
	}
	userRepo.On("GetByEmail", ctx, "existing@example.com").Return(existingUser, nil)

	user, _, _, err := svc.Register(ctx, req)

	assert.ErrorIs(t, err, ErrEmailTaken)
	assert.Nil(t, user)
}

func TestRegister_DuplicateEmail_DBConstraint(t *testing.T) {
	userRepo := new(mockUserRepo)
	refreshRepo := new(mockRefreshTokenRepo)
	svc := NewAuthService(userRepo, refreshRepo, "test-secret")

	ctx := context.Background()
	req := dto.RegisterRequest{
		Email:    "race@example.com",
		Password: "password123",
		Name:     "Test User",
	}

	userRepo.On("GetByEmail", ctx, "race@example.com").Return(nil, nil)
	userRepo.On("Create", ctx, mock.AnythingOfType("*domain.User")).Return(repository.ErrDuplicateEmail)

	user, _, _, err := svc.Register(ctx, req)

	assert.ErrorIs(t, err, ErrEmailTaken)
	assert.Nil(t, user)
}

func TestLogin_Happy(t *testing.T) {
	userRepo := new(mockUserRepo)
	refreshRepo := new(mockRefreshTokenRepo)
	svc := NewAuthService(userRepo, refreshRepo, "test-secret")

	ctx := context.Background()

	// bcrypt hash of "password123"
	existingUser := &domain.User{
		ID:    uuid.New(),
		Email: "test@example.com",
		Name:  "Test User",
	}
	// We need a real bcrypt hash
	hash, _ := bcryptGenerateHelper("password123")
	existingUser.PasswordHash = hash

	userRepo.On("GetByEmail", ctx, "test@example.com").Return(existingUser, nil)
	refreshRepo.On("Create", ctx, mock.AnythingOfType("*repository.RefreshToken")).Return(nil)

	req := dto.LoginRequest{
		Email:    "test@example.com",
		Password: "password123",
	}

	user, accessToken, refreshToken, err := svc.Login(ctx, req)

	assert.NoError(t, err)
	assert.NotNil(t, user)
	assert.Equal(t, existingUser.ID, user.ID)
	assert.NotEmpty(t, accessToken)
	assert.NotEmpty(t, refreshToken)
}

func TestLogin_WrongPassword(t *testing.T) {
	userRepo := new(mockUserRepo)
	refreshRepo := new(mockRefreshTokenRepo)
	svc := NewAuthService(userRepo, refreshRepo, "test-secret")

	ctx := context.Background()
	hash, _ := bcryptGenerateHelper("password123")
	existingUser := &domain.User{
		ID:           uuid.New(),
		Email:        "test@example.com",
		PasswordHash: hash,
	}

	userRepo.On("GetByEmail", ctx, "test@example.com").Return(existingUser, nil)

	req := dto.LoginRequest{
		Email:    "test@example.com",
		Password: "wrongpassword",
	}

	user, _, _, err := svc.Login(ctx, req)

	assert.ErrorIs(t, err, ErrInvalidCredentials)
	assert.Nil(t, user)
}

func TestLogin_NonexistentUser(t *testing.T) {
	userRepo := new(mockUserRepo)
	refreshRepo := new(mockRefreshTokenRepo)
	svc := NewAuthService(userRepo, refreshRepo, "test-secret")

	ctx := context.Background()
	userRepo.On("GetByEmail", ctx, "nobody@example.com").Return(nil, nil)

	req := dto.LoginRequest{
		Email:    "nobody@example.com",
		Password: "password123",
	}

	user, _, _, err := svc.Login(ctx, req)

	assert.ErrorIs(t, err, ErrInvalidCredentials)
	assert.Nil(t, user)
}

func TestRefreshTokens_Happy(t *testing.T) {
	userRepo := new(mockUserRepo)
	refreshRepo := new(mockRefreshTokenRepo)
	svc := NewAuthService(userRepo, refreshRepo, "test-secret")

	ctx := context.Background()

	// Generate a real refresh token to test with
	userID := uuid.New()
	jwtpkg := svc.jwtSecret
	_ = jwtpkg // We need to generate a real token

	// We'll use the Login path to get a valid refresh token first
	hash, _ := bcryptGenerateHelper("password123")
	existingUser := &domain.User{
		ID:           userID,
		Email:        "test@example.com",
		PasswordHash: hash,
	}
	userRepo.On("GetByEmail", ctx, "test@example.com").Return(existingUser, nil)
	refreshRepo.On("Create", ctx, mock.AnythingOfType("*repository.RefreshToken")).Return(nil)

	_, _, refreshToken, _ := svc.Login(ctx, dto.LoginRequest{
		Email:    "test@example.com",
		Password: "password123",
	})

	// Now test refresh
	tokenHash := hashToken(refreshToken)
	rt := &repository.RefreshToken{
		ID:        uuid.New(),
		UserID:    userID,
		TokenHash: tokenHash,
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
	}
	refreshRepo.On("GetByHash", ctx, tokenHash).Return(rt, nil)
	refreshRepo.On("DeleteByHash", ctx, tokenHash).Return(nil)

	newAccess, newRefresh, err := svc.RefreshTokens(ctx, refreshToken)

	assert.NoError(t, err)
	assert.NotEmpty(t, newAccess)
	assert.NotEmpty(t, newRefresh)
}

func TestRefreshTokens_InvalidToken(t *testing.T) {
	userRepo := new(mockUserRepo)
	refreshRepo := new(mockRefreshTokenRepo)
	svc := NewAuthService(userRepo, refreshRepo, "test-secret")

	ctx := context.Background()

	_, _, err := svc.RefreshTokens(ctx, "invalid-token")

	assert.ErrorIs(t, err, ErrInvalidToken)
}

func TestLogout(t *testing.T) {
	userRepo := new(mockUserRepo)
	refreshRepo := new(mockRefreshTokenRepo)
	svc := NewAuthService(userRepo, refreshRepo, "test-secret")

	ctx := context.Background()
	tokenHash := hashToken("some-refresh-token")
	refreshRepo.On("DeleteByHash", ctx, tokenHash).Return(nil)

	err := svc.Logout(ctx, "some-refresh-token")

	assert.NoError(t, err)
	refreshRepo.AssertExpectations(t)
}

func TestGetUserByID(t *testing.T) {
	userRepo := new(mockUserRepo)
	refreshRepo := new(mockRefreshTokenRepo)
	svc := NewAuthService(userRepo, refreshRepo, "test-secret")

	ctx := context.Background()
	userID := uuid.New()
	expectedUser := &domain.User{ID: userID, Email: "test@example.com"}
	userRepo.On("GetByID", ctx, userID).Return(expectedUser, nil)

	user, err := svc.GetUserByID(ctx, userID)

	assert.NoError(t, err)
	assert.Equal(t, expectedUser, user)
}

func TestGetUserByID_NotFound(t *testing.T) {
	userRepo := new(mockUserRepo)
	refreshRepo := new(mockRefreshTokenRepo)
	svc := NewAuthService(userRepo, refreshRepo, "test-secret")

	ctx := context.Background()
	userID := uuid.New()
	userRepo.On("GetByID", ctx, userID).Return(nil, errors.New("not found"))

	user, err := svc.GetUserByID(ctx, userID)

	assert.Error(t, err)
	assert.Nil(t, user)
}

// Helper to generate bcrypt hash for tests
func bcryptGenerateHelper(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	return string(hash), err
}
