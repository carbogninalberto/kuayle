package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func (f *undoFixture) createPage(t *testing.T, parent *uuid.UUID, content string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := f.db.Exec(`INSERT INTO wiki_pages(id,workspace_id,team_id,title,content,parent_id,created_by) VALUES($1,$2,$3,'Page',$4,$5,$6)`,
		id, f.ws, f.team, content, parent, f.owner)
	require.NoError(t, err)
	return id
}

func TestDeletedWikiPageIsHiddenAndRestorableWithinGracePeriod(t *testing.T) {
	f := newUndoFixture(t)
	repo := NewWikiRepository(f.db)
	scope := domain.TeamWiki(f.team)
	top := f.createPage(t, nil, "top")
	// The deleted page mentions top, and it is published.
	page := f.createPage(t, &top, `middle <span data-type="mention" data-kind="wiki" data-id="`+top.String()+`">top</span>`)
	child := f.createPage(t, &page, "child")
	backlinks, err := repo.Backlinks(f.ctx, f.ws, scope, top, true)
	require.NoError(t, err)
	require.Len(t, backlinks.Pages, 1)
	_, err = f.db.Exec(`UPDATE wiki_pages SET publish_mode='public' WHERE id=$1`, page)
	require.NoError(t, err)

	require.NoError(t, repo.Delete(f.ctx, f.ws, scope, page))

	_, err = repo.Get(f.ctx, f.ws, scope, page)
	require.ErrorIs(t, err, ErrWikiPageNotFound)
	tree, err := repo.Tree(f.ctx, f.ws, scope)
	require.NoError(t, err)
	parents := map[uuid.UUID]*uuid.UUID{}
	for _, node := range tree {
		parents[node.ID] = node.ParentID
	}
	require.NotContains(t, parents, page, "the tree hides a deleted page")
	require.Equal(t, top, *parents[child], "children move up to the deleted page's parent")
	refs, err := repo.SearchReferences(f.ctx, f.ws, "middle", 10)
	require.NoError(t, err)
	require.Empty(t, refs)
	published, err := repo.PublishedPages(context.Background(), f.ws, page)
	require.NoError(t, err)
	require.Empty(t, published, "a public link stops showing a deleted page")
	backlinks, err = repo.Backlinks(f.ctx, f.ws, scope, top, true)
	require.NoError(t, err)
	require.Empty(t, backlinks.Pages, "a deleted page is no longer a backlink")

	restored, err := repo.Restore(f.ctx, f.ws, scope, page, time.Now().Add(-time.Hour))
	require.NoError(t, err)
	require.Equal(t, page, restored.ID)
	tree, err = repo.Tree(f.ctx, f.ws, scope)
	require.NoError(t, err)
	for _, node := range tree {
		if node.ID == page {
			require.Equal(t, top, *node.ParentID, "a restored page goes back under its parent")
		}
		if node.ID == child {
			require.Equal(t, top, *node.ParentID, "lifted children stay where they are")
		}
	}
}

func TestWikiRestoreAfterGracePeriodOrPurgeIsRefused(t *testing.T) {
	f := newUndoFixture(t)
	repo := NewWikiRepository(f.db)
	scope := domain.TeamWiki(f.team)
	expired, recent := f.createPage(t, nil, "old"), f.createPage(t, nil, "new")
	require.NoError(t, repo.Delete(f.ctx, f.ws, scope, expired))
	require.NoError(t, repo.Delete(f.ctx, f.ws, scope, recent))
	_, err := f.db.Exec(`UPDATE wiki_page_records SET deleted_at = NOW() - INTERVAL '31 days' WHERE id = $1`, expired)
	require.NoError(t, err)
	cutoff := time.Now().Add(-30 * 24 * time.Hour)

	_, err = repo.Restore(f.ctx, f.ws, scope, expired, cutoff)
	require.ErrorIs(t, err, ErrWikiRestoreExpired)

	purged, err := repo.PurgeDeletedPages(context.Background(), cutoff, 10)
	require.NoError(t, err)
	require.GreaterOrEqual(t, purged, 1)
	_, err = repo.Restore(f.ctx, f.ws, scope, expired, cutoff)
	require.ErrorIs(t, err, ErrWikiPageNotFound, "a purged page is gone")
	_, err = repo.Restore(f.ctx, f.ws, scope, recent, cutoff)
	require.NoError(t, err, "a page inside the grace period is not purged")
}

func TestDeletedRegistryPageFreesItsDay(t *testing.T) {
	f := newUndoFixture(t)
	repo := NewWikiRepository(f.db)
	scope := domain.TeamWiki(f.team)
	require.NoError(t, repo.SetRegistryEnabled(f.ctx, f.ws, scope, f.owner, true))
	day := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	first, _, err := repo.AppendRegistry(f.ctx, f.ws, scope, f.owner, day, "<p>first</p>")
	require.NoError(t, err)
	require.NoError(t, repo.Delete(f.ctx, f.ws, scope, first.ID))

	second, created, err := repo.AppendRegistry(f.ctx, f.ws, scope, f.owner, day, "<p>second</p>")
	require.NoError(t, err)
	require.True(t, created, "the registry starts a new page for the freed day")
	require.NotEqual(t, first.ID, second.ID)

	restored, err := repo.Restore(f.ctx, f.ws, scope, first.ID, time.Now().Add(-time.Hour))
	require.NoError(t, err)
	require.Nil(t, restored.RegistryDate, "a restored registry page does not take the day back from the new one")
}

func TestOutsiderCannotRestoreAPrivateTeamPage(t *testing.T) {
	f := newUndoFixture(t)
	repo := NewWikiRepository(f.db)
	scope := domain.TeamWiki(f.privateTeam)
	id := uuid.New()
	_, err := f.db.Exec(`INSERT INTO wiki_pages(id,workspace_id,team_id,title) VALUES($1,$2,$3,'Private')`, id, f.ws, f.privateTeam)
	require.NoError(t, err)
	require.NoError(t, repo.Delete(f.ctx, f.ws, scope, id))
	_, err = repo.Restore(f.outsiderCtx, f.ws, scope, id, time.Now().Add(-time.Hour))
	require.ErrorIs(t, err, ErrWikiPageNotFound)
}
