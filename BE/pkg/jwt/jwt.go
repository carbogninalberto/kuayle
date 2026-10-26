package jwt

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const (
	accessTokenTTL  = 15 * time.Minute
	refreshTokenTTL = 7 * 24 * time.Hour

	TokenTypeAccess  = "access"
	TokenTypeRefresh = "refresh"
)

type Claims struct {
	UserID uuid.UUID `json:"user_id"`
	// Type separates access and refresh tokens, which share a signing key.
	// Tokens issued before it existed have no type.
	Type string `json:"typ,omitempty"`
	jwt.RegisteredClaims
}

func GenerateAccessToken(userID uuid.UUID, secret string) (string, error) {
	claims := Claims{
		UserID: userID,
		Type:   TokenTypeAccess,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(accessTokenTTL)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

func GenerateRefreshToken(userID uuid.UUID, secret string) (string, time.Time, error) {
	exp := time.Now().Add(refreshTokenTTL)
	claims := Claims{
		UserID: userID,
		Type:   TokenTypeRefresh,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(exp),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(secret))
	return signed, exp, err
}

// ValidateAccessToken accepts only access tokens. Untyped tokens predate the
// type claim; they are accepted only with an access-token lifetime, so a
// legacy refresh token cannot authenticate API requests.
func ValidateAccessToken(tokenString string, secret string) (*Claims, error) {
	claims, err := ValidateToken(tokenString, secret)
	if err != nil {
		return nil, err
	}
	switch claims.Type {
	case TokenTypeAccess:
		return claims, nil
	case "":
		if claims.IssuedAt != nil && claims.ExpiresAt != nil && claims.ExpiresAt.Sub(claims.IssuedAt.Time) <= accessTokenTTL {
			return claims, nil
		}
	}
	return nil, fmt.Errorf("invalid token type")
}

// ValidateRefreshToken rejects access tokens. Untyped legacy refresh tokens
// remain valid because refresh also requires the token's stored hash.
func ValidateRefreshToken(tokenString string, secret string) (*Claims, error) {
	claims, err := ValidateToken(tokenString, secret)
	if err != nil {
		return nil, err
	}
	if claims.Type != TokenTypeRefresh && claims.Type != "" {
		return nil, fmt.Errorf("invalid token type")
	}
	return claims, nil
}

func ValidateToken(tokenString string, secret string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}

	return claims, nil
}
