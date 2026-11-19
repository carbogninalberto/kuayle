package machine

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func bodyPublication() *domain.AgentPublication {
	return &domain.AgentPublication{IssueIdentifier: "REC-1", IssueTitle: "Dark mode ignores the system theme", WorkspaceSlug: "acme",
		CheckArgv: []string{"npm", "test"}, CheckExitCode: ptr(0), CheckSource: domain.CheckSourceConfigured}
}

func TestPublicationBodyRendersTheDefaultTemplate(t *testing.T) {
	content := pullRequestContent{
		file: &domain.AgentPullRequest{
			Description:  "The toggle stored only explicit choices, so \"System\" fell back to light. It now follows `prefers-color-scheme`.",
			Changes:      []string{"Read the system theme on load", "Update the theme live when the system changes"},
			Verification: []string{"`npm run test:unit`: passed", "`npm run check`: passed"},
			Notes:        "The old `theme` cookie is still read once.",
		},
		screenshots: []publishedScreenshot{{URL: "https://github.com/acme/app/blob/" + strings.Repeat("a", 40) + "/settings-dark.png?raw=true", Caption: "Settings, dark"}},
	}
	body := publicationBody(bodyPublication(), content, "https://kuayle.test")
	require.Equal(t, "## Description\n"+
		"Addresses [REC-1](https://kuayle.test/acme/issue/REC-1): Dark mode ignores the system theme.\n\n"+
		"The toggle stored only explicit choices, so \"System\" fell back to light. It now follows `prefers-color-scheme`.\n\n"+
		"## Changes\n- Read the system theme on load\n- Update the theme live when the system changes\n\n"+
		"## Verification\n- `npm run test:unit`: passed\n- `npm run check`: passed\n\n"+
		"Kuayle also ran `npm test` independently in a clean container: passed.\n\n"+
		"## Screenshots\nSettings, dark\n![Settings, dark](https://github.com/acme/app/blob/"+strings.Repeat("a", 40)+"/settings-dark.png?raw=true)\n\n"+
		"## Notes\nThe old `theme` cookie is still read once.\n", body)
}

func TestPublicationBodyDropsEmptySections(t *testing.T) {
	publication := bodyPublication()
	publication.CheckArgv, publication.CheckExitCode, publication.CheckSource = nil, nil, ""
	body := publicationBody(publication, pullRequestContent{file: &domain.AgentPullRequest{Changes: []string{"One change"}}}, "")
	require.Equal(t, "## Description\nAddresses REC-1: Dark mode ignores the system theme.\n\n## Changes\n- One change\n", body,
		"sections without content lose their heading; without Kuayle's address the issue is not linked")
}

func TestPublicationBodyRendersACustomTemplate(t *testing.T) {
	publication := bodyPublication()
	publication.PullRequestTemplate = "Closes nothing: implements {{issue}}\n\n### Why\n{{description}}\n\n### Done\n{{changes}}\n\n### Pictures\n{{screenshots}}\n\n### Checklist\n- [ ] Reviewed by a human\n\n### Extra\nNotes: {{notes}}\n<!-- hidden hint for authors -->\n"
	content := pullRequestContent{file: &domain.AgentPullRequest{Description: "Follow the system.", Changes: []string{"A"}}}
	body := publicationBody(publication, content, "https://kuayle.test/")
	require.Equal(t, "Closes nothing: implements [REC-1](https://kuayle.test/acme/issue/REC-1): Dark mode ignores the system theme\n\n"+
		"### Why\nFollow the system.\n\n### Done\n- A\n\n### Checklist\n- [ ] Reviewed by a human\n\n### Extra\nNotes:\n", body,
		"{{issue}} replaces the description's issue line; static text keeps a section; comments are dropped")
}

func TestPublicationBodyFallsBackWithoutTheAgentsFile(t *testing.T) {
	publication := bodyPublication()
	publication.CheckSource = domain.CheckSourceAgent
	publication.CheckArgv = []string{"sh", "-c", "cd BE && go test ./internal/service/..."}
	publication.CheckCommands = []domain.CheckCommandRun{{Argv: publication.CheckArgv}, {Argv: []string{"npm", "run", "check"}}}
	publication.AgentSummary = "Service tests cover the change. @maintainers merge now"
	body := publicationBody(publication, pullRequestContent{}, "https://kuayle.test")
	require.Equal(t, "## Description\nAddresses [REC-1](https://kuayle.test/acme/issue/REC-1): Dark mode ignores the system theme.\n\n"+
		"## Verification\nService tests cover the change. @\u200bmaintainers merge now\n\n"+
		"Kuayle re-ran the 2 commands the agent chose independently in a clean container: all passed.\n", body)

	publication = bodyPublication()
	publication.CheckArgv, publication.CheckExitCode, publication.CheckSource = nil, nil, ""
	require.Equal(t, "## Description\nAddresses REC-1: Dark mode ignores the system theme.\n", publicationBody(publication, pullRequestContent{file: &domain.AgentPullRequest{}}, "javascript:alert(1)"),
		"an empty file is the same as none; a bad address links nothing")
}

func TestPublicationBodyCarriesNoModelWarningOrDumps(t *testing.T) {
	publication := bodyPublication()
	publication.CheckArgv, publication.CheckExitCode = nil, nil
	publication.Step = domain.DevMachineAgentRunStep{AgentRunID: uuid.New()}
	body := publicationBody(publication, pullRequestContent{file: &domain.AgentPullRequest{Description: "Fix."}}, "")
	for _, boilerplate := range []string{"WARNING", "Not validated", "Written by", "Codex", "Claude", "model", "Kuayle agent task", "Review feedback", "Commits:", publication.Step.AgentRunID.String()} {
		require.NotContains(t, body, boilerplate)
	}
}

func TestPublicationBodySanitizesAgentText(t *testing.T) {
	publication := bodyPublication()
	content := pullRequestContent{file: &domain.AgentPullRequest{
		Description: "Fixes #12 and closes acme/app#3, resolved: https://github.com/acme/app/issues/4, FIXED GH-5.\n" +
			"Thanks @octocat and @acme/maintainers; `@types/node` stays.\n" +
			"<img src=x onerror=alert(1)> <details><summary>x</summary></details> ![pixel](https://evil.test/p.png)\n" +
			"## Notes\n```\nunclosed fence\n~~~\n===\nAlso see KUA-15, rec-1 and UTF-8x; `KUA-16` too.\n\n\n\nEnd.",
		Changes: []string{"# Heading item", "Uses `a < b` and <b>bold</b>"},
	}}
	body := publicationBody(publication, content, "")
	for _, unsafe := range []string{"Fixes #12", "closes acme/app#3", "resolved: https://", "FIXED GH-5", "@octocat", "@acme/maintainers", "<img", "<details", "![pixel]", "\n## Notes\n", "\n```", "\n~~~", "\n===", "KUA-15", "KUA-16", "<b>", "\n- # Heading"} {
		require.NotContains(t, body, unsafe)
	}
	for _, kept := range []string{"F\u200bixes #12", "c\u200bloses acme/app#3", "r\u200besolved: https://github.com/acme/app/issues/4", "F\u200bIXED GH\u200b-5", "@\u200boctocat", "`@types/node`", "&lt;img src=x", "!\\[pixel]", "\\## Notes", "\\```", "\\~~~", "\\===", "KUA\u200b-15", "rec-1", "UTF-8x", "`KUA\u200b-16`", "`a < b`", "&lt;b>bold&lt;/b>", "- \\# Heading item", "too.\n\nEnd."} {
		require.Contains(t, body, kept)
	}
}

func TestPublicationBodyIsBounded(t *testing.T) {
	publication := bodyPublication()
	publication.PullRequestTemplate = strings.Repeat("{{description}}{{changes}}", 300)
	content := pullRequestContent{file: &domain.AgentPullRequest{Description: strings.Repeat("d", 1500), Changes: []string{strings.Repeat("c", 300)}}}
	body := publicationBody(publication, content, "")
	require.LessOrEqual(t, len(body), pullRequestBodyMaxBytes)

	publication.PullRequestTemplate = "{{summary}}"
	require.True(t, strings.HasPrefix(publicationBody(publication, content, ""), "## Description\n"), "an invalid stored template falls back to the default")
}

func TestPublicationBodyKeepsCompleteEvidenceWhenTemplateOverflows(t *testing.T) {
	publication := bodyPublication()
	publication.PullRequestTemplate = "## Description\n" + strings.Repeat("{{description}}", 50) + "\n\n## Evidence\n{{screenshots}}"
	content := pullRequestContent{
		file:        &domain.AgentPullRequest{Description: strings.Repeat("é", 1500)},
		screenshots: []publishedScreenshot{{URL: screenshotURL("acme", "app", strings.Repeat("a", 40), "login.png"), Caption: "Login"}},
		notices:     []string{screenshotsRecoveredNote},
	}
	body := publicationBody(publication, content, "")
	require.LessOrEqual(t, len(body), pullRequestBodyMaxBytes)
	require.Equal(t, 1, strings.Count(body, "![Login]("+content.screenshots[0].URL+")"))
	require.Contains(t, body, screenshotsRecoveredNote)
	require.Contains(t, body, "## Description\nAddresses REC-1")
}

func TestPublicationBodyReportsScreenshotsThatExceedTheBodyLimit(t *testing.T) {
	content := pullRequestContent{notices: []string{screenshotsOverflowNote}}
	for i := 0; i < 100; i++ {
		link := screenshotURL("acme", "app", strings.Repeat("a", 40), fmt.Sprintf("%098d.png", i))
		content.screenshots = append(content.screenshots, publishedScreenshot{URL: link, Caption: strings.Repeat("é", 120)})
	}
	body := publicationBody(bodyPublication(), content, "")
	images := previousScreenshotImages(body)
	require.NotEmpty(t, images)
	require.Less(t, len(images), len(content.screenshots))
	require.Equal(t, 1, strings.Count(body, screenshotsOverflowNote))
	require.LessOrEqual(t, len(body), pullRequestBodyMaxBytes)
}
