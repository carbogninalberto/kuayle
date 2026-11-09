package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWikiSectionEdits(t *testing.T) {
	page := `<h1>Runbook</h1><p>intro</p><h2>Deploy</h2><p>old steps</p><h3>Notes</h3><p>n</p><h2>Rollback</h2><p>r</p>`
	require.Equal(t, []string{"Runbook", "Deploy", "Notes", "Rollback"}, wikiHeadings(page))

	replaced, ok := replaceWikiSection(page, " deploy ", "<p>new steps</p>")
	require.True(t, ok)
	require.Equal(t, `<h1>Runbook</h1><p>intro</p><h2>Deploy</h2><p>new steps</p><h2>Rollback</h2><p>r</p>`, replaced, "a section ends at the next heading of its level, so subsections go too")

	inserted, ok := insertAfterWikiHeading(page, "Rollback", "<p>first</p>")
	require.True(t, ok)
	require.Equal(t, `<h1>Runbook</h1><p>intro</p><h2>Deploy</h2><p>old steps</p><h3>Notes</h3><p>n</p><h2>Rollback</h2><p>first</p><p>r</p>`, inserted)

	last, ok := replaceWikiSection(page, "Rollback", "<p>gone</p>")
	require.True(t, ok)
	require.Equal(t, `<h1>Runbook</h1><p>intro</p><h2>Deploy</h2><p>old steps</p><h3>Notes</h3><p>n</p><h2>Rollback</h2><p>gone</p>`, last)

	_, ok = replaceWikiSection(page, "Missing", "x")
	require.False(t, ok)
}
