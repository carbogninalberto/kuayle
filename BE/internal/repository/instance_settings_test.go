package repository

import (
	"errors"
	"os"
	"testing"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/pkg/crypto"
	"github.com/stretchr/testify/require"
)

func TestInstanceSettingSealsPrivateValueAndRollsBack(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	db, err := sqlx.Connect("pgx", url)
	require.NoError(t, err)
	defer db.Close()
	key := uuid.NewString()
	defer db.Exec("DELETE FROM instance_settings WHERE key=$1", key)
	r := NewInstanceSettingsRepository(db, crypto.DeriveKey(uuid.NewString()))
	private, _, err := webpush.GenerateVAPIDKeys()
	require.NoError(t, err)
	ctx := t.Context()
	require.NoError(t, r.Update(ctx, key, true, func(old string) (string, error) {
		require.Empty(t, old)
		return private, nil
	}))
	var row struct {
		Value  string
		Secret bool
	}
	require.NoError(t, db.Get(&row, "SELECT value, secret FROM instance_settings WHERE key=$1", key))
	require.True(t, row.Secret)
	require.NotEmpty(t, row.Value)
	require.NotContains(t, row.Value, private)
	value, err := r.Get(ctx, key)
	require.NoError(t, err)
	require.Equal(t, private, value)
	rejected := errors.New("rejected")
	require.ErrorIs(t, r.Update(ctx, key, false, func(string) (string, error) { return "", rejected }), rejected)
	value, err = r.Get(ctx, key)
	require.NoError(t, err)
	require.Equal(t, private, value)
}
