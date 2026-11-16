package main

import (
	"context"
	"time"

	log "github.com/sirupsen/logrus"
)

// Removers of soft-deleted rows deleted before a cutoff, in batches.
type deletedIssuePurger interface {
	PurgeDeletedIssues(ctx context.Context, deletedBefore time.Time, batchSize int) (int, error)
}

type deletedWikiPagePurger interface {
	PurgeDeletedPages(ctx context.Context, deletedBefore time.Time, batchSize int) (int, error)
}

type deletedCommentPurger interface {
	PurgeDeletedComments(ctx context.Context, deletedBefore time.Time, batchSize int) (int, error)
}

const purgeBatchSize = 500

// purgeDeleted hard-deletes the issues, wiki pages and comments soft-deleted
// more than grace before now; past that they can no longer be restored. A
// failure in one does not stop the others.
func purgeDeleted(ctx context.Context, now time.Time, grace time.Duration, issues deletedIssuePurger, pages deletedWikiPagePurger, comments deletedCommentPurger) {
	cutoff := now.Add(-grace)
	if n, err := issues.PurgeDeletedIssues(ctx, cutoff, purgeBatchSize); err != nil {
		log.WithError(err).Warn("failed to purge deleted issues")
	} else if n > 0 {
		log.WithField("count", n).Info("purged deleted issues past their grace period")
	}
	if n, err := pages.PurgeDeletedPages(ctx, cutoff, purgeBatchSize); err != nil {
		log.WithError(err).Warn("failed to purge deleted wiki pages")
	} else if n > 0 {
		log.WithField("count", n).Info("purged deleted wiki pages past their grace period")
	}
	if n, err := comments.PurgeDeletedComments(ctx, cutoff, purgeBatchSize); err != nil {
		log.WithError(err).Warn("failed to purge deleted comments")
	} else if n > 0 {
		log.WithField("count", n).Info("purged deleted comments past their grace period")
	}
}

// purgeDeletedEvery runs purgeDeleted at start and then every interval.
func purgeDeletedEvery(ctx context.Context, interval, grace time.Duration, issues deletedIssuePurger, pages deletedWikiPagePurger, comments deletedCommentPurger) {
	purgeDeleted(ctx, time.Now(), grace, issues, pages, comments)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			purgeDeleted(ctx, now, grace, issues, pages, comments)
		}
	}
}
