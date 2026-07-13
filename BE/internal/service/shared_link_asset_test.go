package service

import (
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/pkg/assettoken"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSignPublicAssetURLsRewritesImagesAndAttachments(t *testing.T) {
	workspaceID := uuid.New()
	issueID := uuid.New()
	imageID := uuid.New()
	attachmentID := uuid.New()
	secret := "secret-32-characters-minimum-value:prompt-assets"
	description := `<p><img src="/api/workspaces/acme/assets/` + imageID.String() + `"></p>` +
		`<p><a data-type="attachment" href="/api/workspaces/acme/assets/` + attachmentID.String() + `?download=1">plan.pdf</a></p>`

	got := signPublicAssetURLs(description, secret, workspaceID, issueID)

	assert.NotContains(t, got, "/api/workspaces/")
	assert.Contains(t, got, "?download=1")
	tokens := regexp.MustCompile(`/api/public/assets/([^?"']+)`).FindAllStringSubmatch(got, -1)
	require.Len(t, tokens, 2)
	seen := make(map[uuid.UUID]bool)
	for _, tokenMatch := range tokens {
		claims, err := assettoken.Validate(strings.TrimSpace(tokenMatch[1]), secret)
		require.NoError(t, err)
		assert.Equal(t, workspaceID, claims.WorkspaceID)
		assert.Equal(t, issueID, claims.IssueID)
		seen[claims.AssetID] = true
	}
	assert.True(t, seen[imageID])
	assert.True(t, seen[attachmentID])
}

func TestSignPublicAssetURLsLeavesExternalURLsUnchanged(t *testing.T) {
	description := `<p><img src="https://example.com/image.png"><a href="https://example.com/file.pdf">file</a></p>`

	got := signPublicAssetURLs(description, "secret", uuid.New(), uuid.New())

	assert.Equal(t, description, got)
}
