package service

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/stretchr/testify/require"
)

// A profile timezone is trimmed and validated on write: only a known IANA
// name is stored, an empty value clears the zone, and an invalid name is
// refused without touching what is stored.
func TestPreferencesTimezoneWriteValidation(t *testing.T) {
	repo := &preferencesRepoFake{}
	svc := NewPreferencesService(repo)
	ctx, user := context.Background(), uuid.New()

	timezone := "  Pacific/Auckland  "
	prefs, err := svc.Update(ctx, user, dto.UpdatePreferencesRequest{Timezone: &timezone})
	require.NoError(t, err)
	require.Equal(t, "Pacific/Auckland", prefs.Timezone, "the stored zone is trimmed")
	require.Equal(t, "Pacific/Auckland", repo.stored.Timezone)

	empty := ""
	prefs, err = svc.Update(ctx, user, dto.UpdatePreferencesRequest{Timezone: &empty})
	require.NoError(t, err)
	require.Empty(t, prefs.Timezone, "an empty value clears the stored zone")
	require.Empty(t, repo.stored.Timezone)

	invalid := "Mars/Olympus"
	_, err = svc.Update(ctx, user, dto.UpdatePreferencesRequest{Timezone: &invalid})
	require.ErrorIs(t, err, ErrInvalidPreferences)
	require.Equal(t, 2, repo.upserts, "an invalid zone is refused without storing")
	require.Empty(t, repo.stored.Timezone, "the refused value never reaches storage")

	// "Local" loads but names the server's zone, not the user's, so it is
	// refused like any other non-IANA value.
	local := " Local "
	_, err = svc.Update(ctx, user, dto.UpdatePreferencesRequest{Timezone: &local})
	require.ErrorIs(t, err, ErrInvalidPreferences)
	require.Equal(t, 2, repo.upserts, "\"Local\" is refused without storing")
	require.Empty(t, repo.stored.Timezone)
}
