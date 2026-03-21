package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/jwt"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"golang.org/x/crypto/bcrypt"
)

func bcryptHash(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	return string(hash), err
}

func TestAuthHandler_Register_ValidationError(t *testing.T) {
	e := echo.New()

	body := `{"email": "not-an-email"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	userRepo := &testUserRepo{}
	refreshRepo := &testRefreshTokenRepo{}
	authSvc := service.NewAuthService(userRepo, refreshRepo, "test-secret")
	h := NewAuthHandler(authSvc, false, middleware.NewLoginThrottle(5, 15*time.Minute))

	err := h.Register(c)

	assert.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "VALIDATION_ERROR")
}

func TestAuthHandler_Register_Success(t *testing.T) {
	e := echo.New()

	body := `{"email": "test@example.com", "password": "Password123!!", "name": "Test User"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	userRepo := &testUserRepo{}
	refreshRepo := &testRefreshTokenRepo{}
	authSvc := service.NewAuthService(userRepo, refreshRepo, "test-secret")
	h := NewAuthHandler(authSvc, false, middleware.NewLoginThrottle(5, 15*time.Minute))

	err := h.Register(c)

	assert.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Contains(t, rec.Body.String(), "test@example.com")

	// Verify cookies are set
	cookies := rec.Result().Cookies()
	var hasAccess, hasRefresh bool
	for _, cookie := range cookies {
		if cookie.Name == "access_token" && cookie.Value != "" {
			hasAccess = true
		}
		if cookie.Name == "refresh_token" && cookie.Value != "" {
			hasRefresh = true
		}
	}
	assert.True(t, hasAccess, "access_token cookie should be set")
	assert.True(t, hasRefresh, "refresh_token cookie should be set")
}

func TestAuthHandler_Register_DuplicateEmail(t *testing.T) {
	e := echo.New()

	body := `{"email": "existing@example.com", "password": "Password123!!", "name": "Test"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	userRepo := &testUserRepo{emailExists: true}
	refreshRepo := &testRefreshTokenRepo{}
	authSvc := service.NewAuthService(userRepo, refreshRepo, "test-secret")
	h := NewAuthHandler(authSvc, false, middleware.NewLoginThrottle(5, 15*time.Minute))

	err := h.Register(c)

	assert.NoError(t, err)
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "EMAIL_TAKEN")
}

func TestAuthHandler_Login_Success(t *testing.T) {
	e := echo.New()

	body := `{"email": "test@example.com", "password": "Password123!!"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	userRepo := &testUserRepo{userWithPassword: "Password123!!"}
	refreshRepo := &testRefreshTokenRepo{}
	authSvc := service.NewAuthService(userRepo, refreshRepo, "test-secret")
	h := NewAuthHandler(authSvc, false, middleware.NewLoginThrottle(5, 15*time.Minute))

	err := h.Login(c)

	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	cookies := rec.Result().Cookies()
	var hasAccess bool
	for _, cookie := range cookies {
		if cookie.Name == "access_token" && cookie.Value != "" {
			hasAccess = true
		}
	}
	assert.True(t, hasAccess)
}

func TestAuthHandler_Login_WrongPassword(t *testing.T) {
	e := echo.New()

	body := `{"email": "test@example.com", "password": "wrongpassword"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	userRepo := &testUserRepo{userWithPassword: "Password123!!"}
	refreshRepo := &testRefreshTokenRepo{}
	authSvc := service.NewAuthService(userRepo, refreshRepo, "test-secret")
	h := NewAuthHandler(authSvc, false, middleware.NewLoginThrottle(5, 15*time.Minute))

	err := h.Login(c)

	assert.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), "INVALID_CREDENTIALS")
}

func TestAuthHandler_Me_Success(t *testing.T) {
	e := echo.New()

	userID := uuid.New()
	accessToken, _ := jwt.GenerateAccessToken(userID, "test-secret")
	_ = accessToken

	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set("user_id", userID)

	userRepo := &testUserRepo{specificUserID: userID}
	refreshRepo := &testRefreshTokenRepo{}
	authSvc := service.NewAuthService(userRepo, refreshRepo, "test-secret")
	h := NewAuthHandler(authSvc, false, middleware.NewLoginThrottle(5, 15*time.Minute))

	err := h.Me(c)

	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestAuthHandler_Logout(t *testing.T) {
	e := echo.New()

	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: "refresh_token", Value: "some-token"})
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	userRepo := &testUserRepo{}
	refreshRepo := &testRefreshTokenRepo{}
	authSvc := service.NewAuthService(userRepo, refreshRepo, "test-secret")
	h := NewAuthHandler(authSvc, false, middleware.NewLoginThrottle(5, 15*time.Minute))

	err := h.Logout(c)

	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	// Verify cookies are cleared
	cookies := rec.Result().Cookies()
	for _, cookie := range cookies {
		if cookie.Name == "access_token" || cookie.Name == "refresh_token" {
			assert.True(t, cookie.MaxAge < 0, "cookie %s should be cleared", cookie.Name)
		}
	}
}

// --- Test helpers: simple in-memory repos for handler tests ---

type testUserRepo struct {
	emailExists      bool
	userWithPassword string
	specificUser     *dto.UserResponse
	specificUserID   uuid.UUID
	createdUsers     map[string]*domain.User
}

func (r *testUserRepo) Create(_ context.Context, user *domain.User) error {
	if r.createdUsers == nil {
		r.createdUsers = make(map[string]*domain.User)
	}
	r.createdUsers[user.Email] = user
	return nil
}

func (r *testUserRepo) GetByID(_ context.Context, id uuid.UUID) (*domain.User, error) {
	if r.specificUserID == id {
		return &domain.User{
			ID:          id,
			Email:       "test@example.com",
			Name:        "Test",
			DisplayName: "Test",
		}, nil
	}
	if r.createdUsers != nil {
		for _, u := range r.createdUsers {
			if u.ID == id {
				return u, nil
			}
		}
	}
	return nil, nil
}

func (r *testUserRepo) GetByEmail(_ context.Context, email string) (*domain.User, error) {
	if r.emailExists {
		return &domain.User{ID: uuid.New(), Email: email}, nil
	}
	if r.userWithPassword != "" {
		hash, _ := bcryptHash(r.userWithPassword)
		return &domain.User{
			ID:           uuid.New(),
			Email:        email,
			Name:         "Test User",
			DisplayName:  "Test User",
			PasswordHash: hash,
		}, nil
	}
	if r.createdUsers != nil {
		if u, ok := r.createdUsers[email]; ok {
			return u, nil
		}
	}
	return nil, nil
}

type testRefreshTokenRepo struct {
	tokens map[string]*repository.RefreshToken
}

func (r *testRefreshTokenRepo) Create(_ context.Context, rt *repository.RefreshToken) error {
	if r.tokens == nil {
		r.tokens = make(map[string]*repository.RefreshToken)
	}
	r.tokens[rt.TokenHash] = rt
	return nil
}

func (r *testRefreshTokenRepo) GetByHash(_ context.Context, hash string) (*repository.RefreshToken, error) {
	if r.tokens != nil {
		if rt, ok := r.tokens[hash]; ok {
			return rt, nil
		}
	}
	return nil, fmt.Errorf("not found")
}

func (r *testRefreshTokenRepo) DeleteByHash(_ context.Context, hash string) error {
	if r.tokens != nil {
		delete(r.tokens, hash)
	}
	return nil
}

func (r *testRefreshTokenRepo) DeleteByUser(_ context.Context, _ uuid.UUID) error {
	return nil
}

func (r *testRefreshTokenRepo) DeleteExpired(_ context.Context) error {
	return nil
}
