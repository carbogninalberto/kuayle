package domain

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProfileFilesLayOutInstructionsAndSkillsPerHarness(t *testing.T) {
	bundle := AgentProfileBundle{Name: "Security reviewer", Role: AgentRoleSecurityReviewer, Instructions: "  Check auth.  ",
		Skills: []AgentProfileSkill{
			{Name: "Review SQL", Description: "Scope: every query\nby workspace", Body: "# SQL\n"},
			{Name: "review-sql", Description: "Second one", Body: "Other"},
		}}
	files := ProfileFiles("claude-code", bundle)
	require.Len(t, files, 3)
	require.Equal(t, "/home/kuayle/.claude/CLAUDE.md", files[0].Path)
	require.Contains(t, files[0].Content, "# Agent profile: Security reviewer (security reviewer)")
	require.Contains(t, files[0].Content, "Check auth.\n")
	require.Equal(t, "/home/kuayle/.claude/skills/review-sql/SKILL.md", files[1].Path)
	require.Equal(t, "---\nname: review-sql\ndescription: \"Scope: every query by workspace\"\n---\n\n# SQL\n", files[1].Content)
	require.Equal(t, "/home/kuayle/.claude/skills/review-sql-2/SKILL.md", files[2].Path)
	for _, f := range files {
		require.True(t, ValidProfileFilePath(f.Path), f.Path)
	}

	require.Equal(t, "/home/kuayle/.codex/AGENTS.md", ProfileFiles("codex", bundle)[0].Path)
	require.Equal(t, "/home/kuayle/.config/opencode/skill/review-sql/SKILL.md", ProfileFiles("opencode", bundle)[1].Path)
	require.Nil(t, ProfileFiles("goose", bundle))
	require.False(t, ProfileFilesSupported("goose"))
	require.Empty(t, ProfileFiles("codex", AgentProfileBundle{Instructions: "  "}))
}

func TestSkillSlug(t *testing.T) {
	require.Equal(t, "write-db-migrations", SkillSlug("  Write DB migrations! "))
	require.Equal(t, "skill", SkillSlug("日本語"))
	long := SkillSlug(strings.Repeat("ab ", 40))
	require.LessOrEqual(t, len(long), 64)
	require.Equal(t, long, SkillSlug(long))
}

func TestValidProfileFilePathRefusesAnythingElse(t *testing.T) {
	for _, p := range []string{
		"/home/kuayle/.bashrc",
		"/home/kuayle/.claude/skills/../../.bashrc",
		"/home/kuayle/.claude/skills/Bad Name/SKILL.md",
		"/home/kuayle/.claude/skills/ok/other.md",
		"/home/kuayle/.claude/skills/ok/nested/SKILL.md",
		"/etc/passwd",
		"relative/CLAUDE.md",
	} {
		require.False(t, ValidProfileFilePath(p), p)
	}
}
