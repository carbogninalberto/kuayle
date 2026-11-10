package domain

import (
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
