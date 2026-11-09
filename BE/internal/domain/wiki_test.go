package domain

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestWikiScope(t *testing.T) {
	team, project := uuid.New(), uuid.New()
	require.True(t, TeamWiki(team).Valid())
	require.True(t, ProjectWiki(project).Valid())
	require.False(t, TeamWiki(uuid.Nil).Valid())
	require.False(t, WikiScope{Kind: "workspace", ID: team}.Valid())

	require.Equal(t, TeamWiki(team), WikiPage{TeamID: &team}.Scope())
	require.Equal(t, ProjectWiki(project), WikiPage{ProjectID: &project}.Scope())
	require.False(t, WikiPage{}.Scope().Valid())
}

func TestWikiRegistryEntryHTML(t *testing.T) {
	at := time.Date(2026, 11, 9, 9, 5, 0, 0, time.FixedZone("CET", 3600))
	entry, err := WikiRegistryEntryHTML("  KUA-20\n implemented <script> ", at)
	require.NoError(t, err)
	require.Equal(t, "<p><strong>08:05</strong> KUA-20 implemented &lt;script&gt;</p>", entry)
	require.Equal(t, "Registry 2026-11-09", WikiRegistryTitle(at))
	_, err = WikiRegistryEntryHTML(" \n ", at)
	require.ErrorIs(t, err, ErrWikiRegistryEntry)
	_, err = WikiRegistryEntryHTML(strings.Repeat("é", WikiRegistryEntryMaxRunes+1), at)
	require.ErrorIs(t, err, ErrWikiRegistryEntry)
}
