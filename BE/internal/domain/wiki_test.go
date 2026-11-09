package domain

import (
	"testing"

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
