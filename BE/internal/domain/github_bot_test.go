package domain

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// GitHub comments and descriptions start with the configured name and
// picture; defaults leave them unchanged, and an uploaded picture is linked
// only from a public https address.
func TestGitHubBotSignatureUsesTheConfiguredIdentity(t *testing.T) {
	str := func(v string) *string { return &v }
	ws := uuid.New()
	require.Empty(t, (&GitHubBotSettings{WorkspaceID: ws}).Signature("https://kuayle.test"))
	require.Empty(t, (*GitHubBotSettings)(nil).Signature("https://kuayle.test"))
	named := &GitHubBotSettings{WorkspaceID: ws, DisplayName: str("Acme Reviewer")}
	require.Equal(t, "**Acme Reviewer**\n\n", named.Signature("https://kuayle.test"))
	linked := &GitHubBotSettings{WorkspaceID: ws, AvatarURL: str("https://img.test/bot.png")}
	require.Equal(t, "<img src=\"https://img.test/bot.png\" alt=\"\" width=\"20\" height=\"20\" align=\"top\"> **Kuayle**\n\n", linked.Signature("https://kuayle.test"))
	uploaded := &GitHubBotSettings{WorkspaceID: ws, AvatarFile: str(uuid.NewString() + ".png")}
	require.Contains(t, uploaded.Signature("https://kuayle.test/"), `<img src="https://kuayle.test/api/github-bot/`+ws.String()+`/avatar/`)
	require.Equal(t, "**Kuayle**\n\n", uploaded.Signature("http://localhost:5173"))
	edited := &GitHubBotSettings{WorkspaceID: ws, DisplayName: str("@team *ping*")}
	require.Equal(t, "**@​team ping**\n\n", edited.Signature(""))
	unsafe := &GitHubBotSettings{WorkspaceID: ws, AvatarURL: str("https://img.test/a\".png")}
	require.Equal(t, "**Kuayle**\n\n", unsafe.Signature(""))
}

// The pull request webhook looks for issue identifiers in the description.
// The signature Kuayle put there is not part of what the author wrote, so a
// bot called "OPS-12 Bot" or a picture named img-204.png must not count.
func TestGitHubBotSignatureIsNotPartOfTheDescription(t *testing.T) {
	str := func(v string) *string { return &v }
	ws := uuid.New()
	named := &GitHubBotSettings{WorkspaceID: ws, DisplayName: str("OPS-12 Bot")}
	require.Equal(t, "Closes KUA-60", named.WithoutSignature("", named.Signature("")+"Closes KUA-60"))
	linked := &GitHubBotSettings{WorkspaceID: ws, AvatarURL: str("https://img.test/img-204.png")}
	body := linked.Signature("") + "Closes KUA-60"
	require.Equal(t, "Closes KUA-60", linked.WithoutSignature("", body))
	// GitHub may hand the description back with CRLF line endings.
	require.Equal(t, "Closes KUA-60", linked.WithoutSignature("", strings.ReplaceAll(body, "\n", "\r\n")))
	// Only a leading signature is removed, and nothing without one.
	require.Equal(t, "Closes KUA-60\n\n**OPS-12 Bot**\n\n", named.WithoutSignature("", "Closes KUA-60\n\n**OPS-12 Bot**\n\n"))
	require.Equal(t, "**Other**\n\nKUA-60", named.WithoutSignature("", "**Other**\n\nKUA-60"))
	require.Equal(t, "KUA-60", (*GitHubBotSettings)(nil).WithoutSignature("", "KUA-60"))
	require.Equal(t, "KUA-60", (&GitHubBotSettings{WorkspaceID: ws}).WithoutSignature("", "KUA-60"))
}
