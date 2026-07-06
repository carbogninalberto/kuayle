package assettoken

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const PurposePromptAsset = "prompt_asset"

type Claims struct {
	Purpose     string    `json:"purpose"`
	AssetID     uuid.UUID `json:"asset_id"`
	WorkspaceID uuid.UUID `json:"workspace_id"`
	IssueID     uuid.UUID `json:"issue_id"`
	jwt.RegisteredClaims
}

func Generate(secret string, assetID, workspaceID, issueID uuid.UUID, ttl time.Duration) (string, time.Time, error) {
	now := time.Now()
	expiresAt := now.Add(ttl)
	claims := Claims{
		Purpose:     PurposePromptAsset,
		AssetID:     assetID,
		WorkspaceID: workspaceID,
		IssueID:     issueID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(now),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(secret))
	return signed, expiresAt, err
}

func Validate(tokenString, secret string) (*Claims, error) {
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
	if claims.Purpose != PurposePromptAsset {
		return nil, fmt.Errorf("invalid token purpose")
	}
	return claims, nil
}
