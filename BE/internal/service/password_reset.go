package service

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/pkg/mail"
	log "github.com/sirupsen/logrus"
	"golang.org/x/crypto/bcrypt"
)

const (
	passwordResetTTL      = time.Hour
	passwordResetCooldown = time.Minute
)

// WithPasswordReset enables the forgot/reset password flow. A nil sender means
// SMTP is not configured; the reset link is then logged when logLinks is true.
func WithPasswordReset(repo repository.PasswordResetRepo, sender mail.Sender, frontendURL string, logLinks bool) AuthServiceOption {
	return func(s *AuthService) {
		s.resetRepo = repo
		s.mailer = sender
		s.frontendURL = strings.TrimRight(frontendURL, "/")
		s.logResetLinks = logLinks
	}
}

// RequestPasswordReset never reveals whether the email belongs to an account:
// the caller returns immediately and all account-dependent work (lookup, cooldown,
// token insert, mail) runs in the background, so response time is uniform.
func (s *AuthService) RequestPasswordReset(_ context.Context, email string) error {
	if s.resetRepo == nil {
		return nil
	}
	s.runAsync(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := s.issuePasswordReset(ctx, email); err != nil {
			log.WithError(err).WithField("event", "auth.password_reset_request_failed").Error("password reset request failed")
		}
	})
	return nil
}

func (s *AuthService) issuePasswordReset(ctx context.Context, email string) error {
	user, err := s.userRepo.GetByEmail(ctx, email)
	if err != nil || user == nil {
		return err
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	// Issue enforces the per-account cooldown atomically; mail only follows a stored token.
	issued, err := s.resetRepo.Issue(ctx, &repository.PasswordResetToken{
		ID: uuid.New(), UserID: user.ID, TokenHash: hashToken(token), ExpiresAt: time.Now().Add(passwordResetTTL),
	}, passwordResetCooldown)
	if err != nil || !issued {
		return err
	}

	link := fmt.Sprintf("%s/reset-password?token=%s", s.frontendURL, url.QueryEscape(token))
	if s.mailer == nil {
		if s.logResetLinks {
			log.WithFields(log.Fields{"event": "auth.password_reset_link", "email": user.Email, "link": link}).Info("SMTP not configured; password reset link")
		} else {
			log.WithField("event", "auth.password_reset_unsent").Warn("password reset requested but SMTP is not configured")
		}
		return nil
	}
	body := "Someone requested a password reset for your Kuayle account.\r\n\r\nReset your password: " + link +
		"\r\n\r\nThis link expires in 1 hour. If you did not request it, you can ignore this email."
	if err := s.mailer.Send(user.Email, "Reset your Kuayle password", body); err != nil {
		log.WithError(err).WithField("event", "auth.password_reset_send_failed").Error("failed to send password reset email")
	}
	return nil
}

func (s *AuthService) runAsync(f func()) {
	if s.async != nil {
		s.async(f)
		return
	}
	go f()
}

func (s *AuthService) ResetPassword(ctx context.Context, token, password string) error {
	if s.resetRepo == nil {
		return ErrInvalidToken
	}
	if err := validatePasswordComplexity(password); err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	err = s.resetRepo.Reset(ctx, hashToken(token), string(hash))
	if errors.Is(err, sql.ErrNoRows) {
		return ErrInvalidToken
	}
	return err
}
