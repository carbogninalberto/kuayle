package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type cutoffRecorder struct {
	cutoffs []time.Time
	err     error
}

func (r *cutoffRecorder) PurgeDeletedIssues(_ context.Context, before time.Time, batch int) (int, error) {
	r.cutoffs = append(r.cutoffs, before)
	return 0, r.err
}

func (r *cutoffRecorder) PurgeDeletedPages(_ context.Context, before time.Time, batch int) (int, error) {
	r.cutoffs = append(r.cutoffs, before)
	return 0, r.err
}

func (r *cutoffRecorder) PurgeDeletedComments(_ context.Context, before time.Time, batch int) (int, error) {
	r.cutoffs = append(r.cutoffs, before)
	return 0, r.err
}

// The purge removes only what was deleted more than the grace period ago, and
// a failing issue purge does not stop the wiki and comment purges.
func TestPurgeDeletedUsesTheGracePeriodCutoff(t *testing.T) {
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	issues := &cutoffRecorder{err: errors.New("database unavailable")}
	pages := &cutoffRecorder{}
	comments := &cutoffRecorder{}

	purgeDeleted(context.Background(), now, 72*time.Hour, issues, pages, comments)

	assert.Equal(t, []time.Time{now.Add(-72 * time.Hour)}, issues.cutoffs)
	assert.Equal(t, []time.Time{now.Add(-72 * time.Hour)}, pages.cutoffs)
	assert.Equal(t, []time.Time{now.Add(-72 * time.Hour)}, comments.cutoffs)
}
