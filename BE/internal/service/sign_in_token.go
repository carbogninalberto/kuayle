package service

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/repository"
	jwtpkg "github.com/kuayle/kuayle-backend/pkg/jwt"
	log "github.com/sirupsen/logrus"
)

const signInTokenBytes = 32

func (s *AuthService) SetSignInTokens(repo *repository.SignInTokenRepository) {
	s.signInTokens = repo
}

func (s *AuthService) ExchangeSignInToken(ctx context.Context, slug, token, accessCookie, refreshCookie string) (string, string, error) {
	if s.signInTokens == nil || len(token) != base64.RawURLEncoding.EncodedLen(signInTokenBytes) {
		return "", "", ErrInvalidToken
	}
	hash := hashToken(token)
	user, err := s.signInTokens.User(ctx, hash, slug)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", ErrInvalidToken
	}
	if err != nil {
		return "", "", err
	}
	existing, err := s.signInCookieUser(ctx, accessCookie, refreshCookie)
	if err != nil {
		return "", "", err
	}
	if existing != uuid.Nil && existing != user {
		return "", "", ErrInvalidToken
	}
	if existing == uuid.Nil {
		if err := s.RejectPasswordSignIn(ctx, user); err != nil {
			return "", "", err
		}
	}

	var access, refresh string
	err = s.signInTokens.Use(ctx, hash, slug, user, func(store func(context.Context, *repository.RefreshToken) error) error {
		// Preserve the user's current session and any SSO or passkey binding.
		if existing != uuid.Nil {
			return nil
		}
		var err error
		// A bearer link does not satisfy checks that require recent reauthentication.
		access, refresh, err = s.issueSessionWith(ctx, user, time.Time{}, nil, nil, nil, store)
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", ErrInvalidToken
	}
	if err != nil {
		return "", "", err
	}
	log.WithFields(log.Fields{"event": "auth.sign_in_token_used", "workspace": slug, "user_id": user, "purpose": "telegram"}).Info("sign-in link used")
	return access, refresh, nil
}

func (s *AuthService) signInCookieUser(ctx context.Context, access, refresh string) (uuid.UUID, error) {
	if claims, err := jwtpkg.ValidateAccessToken(access, s.jwtSecret); err == nil {
		user, err := s.userRepo.GetByID(ctx, claims.UserID)
		if err != nil {
			return uuid.Nil, err
		}
		if user != nil && user.SessionVersion == claims.SessionVersion {
			return user.ID, nil
		}
	}
	claims, err := jwtpkg.ValidateRefreshToken(refresh, s.jwtSecret)
	if err != nil {
		return uuid.Nil, nil
	}
	_, err = s.refreshRepo.GetByHash(ctx, hashToken(refresh))
	if errors.Is(err, sql.ErrNoRows) {
		return uuid.Nil, nil
	}
	return claims.UserID, err
}
