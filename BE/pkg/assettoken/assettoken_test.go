package assettoken

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateValidate(t *testing.T) {
	assetID := uuid.New()
	workspaceID := uuid.New()
	issueID := uuid.New()

	token, expiresAt, err := Generate("secret-32-characters-minimum-value", assetID, workspaceID, issueID, time.Hour)
	require.NoError(t, err)
	assert.WithinDuration(t, time.Now().Add(time.Hour), expiresAt, 2*time.Second)

	claims, err := Validate(token, "secret-32-characters-minimum-value")
	require.NoError(t, err)
	assert.Equal(t, PurposePromptAsset, claims.Purpose)
	assert.Equal(t, assetID, claims.AssetID)
	assert.Equal(t, workspaceID, claims.WorkspaceID)
	assert.Equal(t, issueID, claims.IssueID)
}

func TestValidateRejectsExpiredToken(t *testing.T) {
	token, _, err := Generate("secret-32-characters-minimum-value", uuid.New(), uuid.New(), uuid.New(), -time.Hour)
	require.NoError(t, err)

	_, err = Validate(token, "secret-32-characters-minimum-value")
	assert.Error(t, err)
}

func TestValidateRejectsTamperedToken(t *testing.T) {
	token, _, err := Generate("secret-32-characters-minimum-value", uuid.New(), uuid.New(), uuid.New(), time.Hour)
	require.NoError(t, err)

	_, err = Validate(token+"x", "secret-32-characters-minimum-value")
	assert.Error(t, err)
}

func TestValidateRejectsWrongPurpose(t *testing.T) {
	claims := Claims{
		Purpose:     "other",
		AssetID:     uuid.New(),
		WorkspaceID: uuid.New(),
		IssueID:     uuid.New(),
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("secret-32-characters-minimum-value"))
	require.NoError(t, err)

	_, err = Validate(token, "secret-32-characters-minimum-value")
	assert.Error(t, err)
}
