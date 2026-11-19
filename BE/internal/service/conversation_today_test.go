package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

// prefsRepoStub stands in for the preferences repository when reading the
// profile timezone.
type prefsRepoStub struct {
	prefs *domain.UserPreferences
	err   error
}

func (f *prefsRepoStub) Get(context.Context, uuid.UUID) (*domain.UserPreferences, error) {
	return f.prefs, f.err
}

func (f *prefsRepoStub) Upsert(context.Context, *domain.UserPreferences) error { return nil }

// The stored profile timezone reaches the prompt's date line; every way the
// lookup can be unavailable keeps the workspace zone instead of failing the
// reply.
func TestProfileTimezoneFailsOpenToTheWorkspaceZone(t *testing.T) {
	chat := &ConversationService{}
	ws, user := uuid.New(), uuid.New()
	require.Empty(t, chat.profileTimezone(context.Background(), ws, user), "no wired preferences repository keeps the workspace zone")

	chat.SetPreferences(&prefsRepoStub{err: errors.New("column missing")})
	require.Empty(t, chat.profileTimezone(context.Background(), ws, user), "a failed read keeps the workspace zone")

	chat.SetPreferences(&prefsRepoStub{})
	require.Empty(t, chat.profileTimezone(context.Background(), ws, user), "a missing profile keeps the workspace zone")

	chat.SetPreferences(&prefsRepoStub{prefs: &domain.UserPreferences{Timezone: "Pacific/Auckland"}})
	require.Equal(t, "Pacific/Auckland", chat.profileTimezone(context.Background(), ws, user))
}

var _ repository.UserPreferencesRepo = (*prefsRepoStub)(nil)

// The assistant's prompt pins today's date, weekday and zone: at
// 2026-11-13 22:30 UTC it is still Friday in UTC, while UTC+13 has
// crossed midnight, so "next Friday" resolves in the user's day.
func TestAssistantTodayPinsTheDateInTheUsersZone(t *testing.T) {
	now := time.Date(2026, 11, 13, 22, 30, 0, 0, time.UTC)
	require.Equal(t, "Today is Saturday, 2026-11-14 (Pacific/Auckland).", assistantToday(now, "Pacific/Auckland", time.UTC))
	require.Equal(t, "Today is Friday, 2026-11-13 (UTC).", assistantToday(now, "", time.UTC), "no profile timezone keeps the workspace's UTC day")
	berlin := time.FixedZone("Europe/Berlin", 3600)
	require.Equal(t, "Today is Friday, 2026-11-13 (Europe/Berlin).", assistantToday(now, "", berlin), "the workspace zone answers when the profile has none")
}

// The zone falls back from the user's profile timezone to the workspace's
// to UTC; an unknown name never fails a reply.
func TestAssistantTimezoneFallsBackFromProfileToWorkspaceToUTC(t *testing.T) {
	berlin := time.FixedZone("Europe/Berlin", 3600)
	require.Equal(t, "Pacific/Auckland", assistantTimezone("Pacific/Auckland", berlin).String())
	require.Equal(t, "Europe/Berlin", assistantTimezone("", berlin).String())
	require.Equal(t, "Europe/Berlin", assistantTimezone("Mars/Olympus", berlin).String(), "an unknown profile timezone falls back")
	require.Equal(t, berlin, assistantTimezone("Local", berlin), "the server host zone is not a profile timezone")
	require.Equal(t, time.UTC, assistantTimezone("", nil))
	require.Equal(t, time.UTC, assistantTimezone("Mars/Olympus", nil))
}
