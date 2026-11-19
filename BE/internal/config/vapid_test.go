package config

import (
	"testing"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/stretchr/testify/require"
)

func TestVAPIDFromEnvironment(t *testing.T) {
	setProductionLoadEnv(t)
	t.Setenv("VAPID_PUBLIC_KEY", "")
	t.Setenv("VAPID_PRIVATE_KEY", "")
	t.Setenv("VAPID_SUBJECT", "")
	cfg, err := Load()
	require.NoError(t, err)
	require.False(t, cfg.VAPID.Enabled())

	private, public, err := webpush.GenerateVAPIDKeys()
	require.NoError(t, err)
	t.Setenv("VAPID_PUBLIC_KEY", public)
	t.Setenv("VAPID_PRIVATE_KEY", private)
	t.Setenv("VAPID_SUBJECT", "mailto:admin@example.com")
	cfg, err = Load()
	require.NoError(t, err)
	require.True(t, cfg.VAPID.Enabled())
	require.Equal(t, public, cfg.VAPID.PublicKey)

	t.Setenv("VAPID_SUBJECT", "ftp://example.com")
	_, err = Load()
	require.ErrorContains(t, err, "VAPID_SUBJECT")
	t.Setenv("VAPID_SUBJECT", "https://example.com/contact")
	t.Setenv("VAPID_PRIVATE_KEY", "")
	_, err = Load()
	require.ErrorContains(t, err, "VAPID_PRIVATE_KEY")
}
