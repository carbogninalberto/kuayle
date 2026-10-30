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

const passwordResetTTL = time.Hour

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

// RequestPasswordReset never reveals whether the email belongs to an account.
func (s *AuthService) RequestPasswordReset(ctx context.Context, email string) error {
	if s.resetRepo == nil {
		return nil
	}
	user, err := s.userRepo.GetByEmail(ctx, email)
	if err != nil {
		return err
	}
	if user == nil {
		return nil
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	if err := s.resetRepo.Create(ctx, &repository.PasswordResetToken{
		ID: uuid.New(), UserID: user.ID, TokenHash: hashToken(token), ExpiresAt: time.Now().Add(passwordResetTTL),
	}); err != nil {
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
