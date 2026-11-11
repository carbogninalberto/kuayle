package service

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/stretchr/testify/require"
)

type preferencesRepoFake struct {
	stored  *domain.UserPreferences
	upserts int
}

func (f *preferencesRepoFake) Get(context.Context, uuid.UUID) (*domain.UserPreferences, error) {
	if f.stored == nil {
		return nil, nil
	}
	copied := *f.stored
	copied.NotificationEvents = domain.NotificationEvents{}
	for key, enabled := range f.stored.NotificationEvents {
		copied.NotificationEvents[key] = enabled
	}
	return &copied, nil
}

func (f *preferencesRepoFake) Upsert(_ context.Context, prefs *domain.UserPreferences) error {
	f.upserts++
	f.stored = prefs
	return nil
}

var allNotificationEvents = map[string]bool{
	"approval_requested": true, "task_finished": true, "review_needed": true, "assigned": true,
	"mentioned": true, "comment": true, "issue_updated": true,
}

func TestPreferencesNotificationDefaultsForUserWithoutRow(t *testing.T) {
	svc := NewPreferencesService(&preferencesRepoFake{})

	prefs, err := svc.Get(context.Background(), uuid.New())

	require.NoError(t, err)
	require.True(t, prefs.BrowserNotifications)
	require.Equal(t, "chime", prefs.NotificationSound)
	require.Equal(t, allNotificationEvents, map[string]bool(prefs.NotificationEvents))
}

// Rows written before an event existed, or holding a retired key, still read
// as the full set: missing keys are enabled and unknown ones are dropped.
func TestPreferencesNotificationEventsReadComplete(t *testing.T) {
	repo := &preferencesRepoFake{stored: &domain.UserPreferences{NotificationEvents: domain.NotificationEvents{"comment": false, "retired": false}}}
	svc := NewPreferencesService(repo)

	prefs, err := svc.Get(context.Background(), uuid.New())

	require.NoError(t, err)
	want := map[string]bool{}
	for key := range allNotificationEvents {
		want[key] = key != "comment"
	}
	require.Equal(t, want, map[string]bool(prefs.NotificationEvents))
	require.Equal(t, "chime", prefs.NotificationSound, "an empty stored sound reads as the default")
}

func TestPreferencesNotificationEventsMergePartialUpdates(t *testing.T) {
	repo := &preferencesRepoFake{}
	svc := NewPreferencesService(repo)
	ctx, user := context.Background(), uuid.New()

	_, err := svc.Update(ctx, user, dto.UpdatePreferencesRequest{NotificationEvents: &map[string]bool{"comment": false}})
	require.NoError(t, err)
	off := false
	prefs, err := svc.Update(ctx, user, dto.UpdatePreferencesRequest{NotificationEvents: &map[string]bool{"issue_updated": false, "assigned": true}, BrowserNotifications: &off})

	require.NoError(t, err)
	want := map[string]bool{}
	for key := range allNotificationEvents {
		want[key] = key != "comment" && key != "issue_updated"
	}
	require.Equal(t, want, map[string]bool(prefs.NotificationEvents), "the earlier change survives a later partial update")
	require.False(t, prefs.BrowserNotifications)
	require.Equal(t, want, map[string]bool(repo.stored.NotificationEvents))

	// Updating something else leaves the notification preferences alone.
	size := "large"
	prefs, err = svc.Update(ctx, user, dto.UpdatePreferencesRequest{FontSize: &size})
	require.NoError(t, err)
	require.Equal(t, want, map[string]bool(prefs.NotificationEvents))
	require.False(t, prefs.BrowserNotifications)
}

func TestPreferencesRejectUnknownNotificationEvent(t *testing.T) {
	repo := &preferencesRepoFake{}
	svc := NewPreferencesService(repo)

	_, err := svc.Update(context.Background(), uuid.New(), dto.UpdatePreferencesRequest{NotificationEvents: &map[string]bool{"comment": false, "deployed": true}})

	require.ErrorIs(t, err, ErrInvalidPreferences)
	require.Zero(t, repo.upserts, "nothing is stored when a key is refused")
}

func TestPreferencesNotificationSound(t *testing.T) {
	for _, sound := range []string{"off", "chime", "bell", "pop", "drop", "pulse"} {
		repo := &preferencesRepoFake{}
		prefs, err := NewPreferencesService(repo).Update(context.Background(), uuid.New(), dto.UpdatePreferencesRequest{NotificationSound: &sound})
		require.NoError(t, err, sound)
		require.Equal(t, sound, prefs.NotificationSound)
	}
	for _, sound := range []string{"", "gong", "Chime"} {
		repo := &preferencesRepoFake{}
		_, err := NewPreferencesService(repo).Update(context.Background(), uuid.New(), dto.UpdatePreferencesRequest{NotificationSound: &sound})
		require.ErrorIs(t, err, ErrInvalidPreferences, sound)
		require.Zero(t, repo.upserts)
	}
}
