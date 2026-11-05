package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func sampleReleaseInput() releaseNotesInput {
	return releaseNotesInput{
		Repository: "acme/app", PreviousTag: "v1.2.0", Tag: "v1.3.0", TargetCommit: "0123456789abcdef0123456789abcdef01234567", Branch: "main",
		Pulls: []releasePull{
			{Number: 12, Title: "feat(assistant): release tools from chat", URL: "https://github.com/acme/app/pull/12"},
			{Number: 13, Title: "fix(ui): keep focus in the dialog", URL: "https://github.com/acme/app/pull/13"},
			{Number: 14, Title: "KUA-90: Faster image builds", URL: "https://github.com/acme/app/pull/14", Labels: []string{"ci"}},
			{Number: 15, Title: "Crash on empty title", URL: "https://github.com/acme/app/pull/15", Labels: []string{"bug"}},
		},
		Migrations:   []string{"BE/migrations/000111_release_approvals.up.sql"},
		ChecksPassed: 12,
	}
}

func TestDefaultReleaseStyleRendersTheWorkspaceSectionsInOrder(t *testing.T) {
	sections := parseReleaseSections(defaultReleaseNotesStyle)
	require.Equal(t, []string{"Features", "Improvements", "Fixed", "Upgrade", "Validation", "PR links", "Full Changelog"}, sections)
	notes := renderReleaseNotes(sections, sampleReleaseInput())
	order := []string{"### Features", "### Improvements", "### Fixed", "### Upgrade", "### Validation", "PRs: ", "**Full Changelog**: "}
	last := -1
	for _, heading := range order {
		at := strings.Index(notes, heading)
		require.Greater(t, at, last, "%s comes after the previous section in:\n%s", heading, notes)
		last = at
	}
	require.Contains(t, notes, "- **Release tools from chat** (#12)", "the conventional prefix is dropped")
	require.Contains(t, notes, "### Fixed\n- **Keep focus in the dialog** (#13)\n- **Crash on empty title** (#15)", "fixes come from the prefix or the bug label")
	require.Contains(t, notes, "### Improvements\n- **KUA-90: Faster image builds** (#14)")
	require.Contains(t, notes, "000111_release_approvals")
	require.Contains(t, notes, "Back up the database")
	require.Contains(t, notes, "All 12 checks passed on main at 0123456")
	require.Contains(t, notes, "PRs: https://github.com/acme/app/pull/12, https://github.com/acme/app/pull/13, https://github.com/acme/app/pull/14, https://github.com/acme/app/pull/15")
	require.True(t, strings.HasSuffix(notes, "**Full Changelog**: https://github.com/acme/app/compare/v1.2.0...v1.3.0\n"))
	require.NoError(t, checkReleaseNotes(notes), "a rendered draft passes the notes check")
}

func TestEditedReleaseStyleChangesSectionsAndOrder(t *testing.T) {
	style := "Write short notes.\n\nSections: Fixed, Highlights, Features, Full Changelog\n"
	sections := parseReleaseSections(style)
	require.Equal(t, []string{"Fixed", "Highlights", "Features", "Full Changelog"}, sections)
	input := sampleReleaseInput()
	input.Migrations = nil
	notes := renderReleaseNotes(sections, input)
	require.Less(t, strings.Index(notes, "### Fixed"), strings.Index(notes, "### Features"))
	require.NotContains(t, notes, "### Upgrade", "a section the style leaves out is not rendered")
	require.NotContains(t, notes, "PRs: ")
	require.NotContains(t, notes, "### Highlights", "an unknown section is left for the assistant to write")
	require.Equal(t, []string{"Highlights"}, unrenderedSections(sections))

	// No Sections line: the default order applies.
	require.Equal(t, parseReleaseSections(defaultReleaseNotesStyle), parseReleaseSections("Just prose about tone."))
}

func TestFirstReleaseLinksTheCommitsAndEmptySectionsAreOmitted(t *testing.T) {
	input := sampleReleaseInput()
	input.PreviousTag, input.Pulls, input.Migrations = "", nil, nil
	notes := renderReleaseNotes(parseReleaseSections(defaultReleaseNotesStyle), input)
	require.NotContains(t, notes, "### Features")
	require.NotContains(t, notes, "### Upgrade")
	require.Contains(t, notes, "### Validation")
	require.Contains(t, notes, "**Full Changelog**: https://github.com/acme/app/commits/v1.3.0")
}

func TestPullRequestNumbersAreReadFromMergeAndSquashCommits(t *testing.T) {
	require.Equal(t, []int{113, 7, 119}, pullNumbersFromMessages([]string{
		"Merge pull request #113 from owner/branch\n\nTitle",
		"fix(ui): keep focus (#7)\n\nbody mentions #99",
		"bump(version): v0.2.11",
		"Merge pull request #119 from owner/other",
		"Merge pull request #113 from owner/branch",
	}))
}

func TestNextReleaseTagBumpsThePatchVersion(t *testing.T) {
	require.Equal(t, "v0.2.12", nextReleaseTag("v0.2.11"))
	require.Equal(t, "1.4.1", nextReleaseTag("1.4.0"))
	require.Equal(t, "", nextReleaseTag("nightly"))
	require.Equal(t, "", nextReleaseTag(""))
}
