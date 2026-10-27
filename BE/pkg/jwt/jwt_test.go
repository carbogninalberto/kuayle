package jwt

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Refresh tokens are stored by hash under a unique constraint, so two tokens
// issued to one user in the same second (two tabs, a refresh) must differ.
func TestRefreshTokensIssuedInTheSameSecondAreDistinct(t *testing.T) {
	user := uuid.New()
	first, _, err := GenerateRefreshToken(user, "test-secret-with-enough-length-1234")
	require.NoError(t, err)
	second, _, err := GenerateRefreshToken(user, "test-secret-with-enough-length-1234")
	require.NoError(t, err)
	require.NotEqual(t, first, second)
	claims, err := ValidateToken(second, "test-secret-with-enough-length-1234")
	require.NoError(t, err)
	require.Equal(t, user, claims.UserID)
	require.NotEmpty(t, claims.ID)
}
