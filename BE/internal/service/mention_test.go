package service

import (
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/pkg/sanitize"
	"github.com/stretchr/testify/require"
)

// Only people are notified of a mention: chips for issues and wiki pages
// carry an ID too, but not a user's.
func TestExtractMentionedUserIDsSkipsIssueAndWikiChips(t *testing.T) {
	user, legacy, issue, page := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	html := sanitize.SanitizeHTML(`<p>` +
		`<span data-type="mention" data-kind="user" data-id="` + user.String() + `">@Ada</span> ` +
		`<span data-id="` + legacy.String() + `" data-type="mention">@Bob</span> ` +
		`<span data-type="mention" data-kind="issue" data-id="` + issue.String() + `">KUA-1</span> ` +
		`<span data-type="mention" data-kind="wiki" data-id="` + page.String() + `">Guide</span> ` +
		`<span data-type="mention" data-kind="user" data-id="` + user.String() + `">@Ada</span></p>`)
	require.Equal(t, []uuid.UUID{user, legacy}, extractMentionedUserIDs(html))
}
