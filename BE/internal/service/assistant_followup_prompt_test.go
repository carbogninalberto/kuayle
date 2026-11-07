package service

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func followUpReview(findings string) *domain.AgentReview {
	verdict := "request_changes"
	return &domain.AgentReview{PullRequestNumber: 146, CommitSHA: "d5e11dd0123456789abcdef0123456789abcdef0", Verdict: &verdict, Findings: &findings}
}

// A follow-up after an agent review carries the review's findings in full,
// fenced as untrusted text, and replaces an earlier follow-up section.
func TestChatFollowUpPromptCarriesTheReviewFindings(t *testing.T) {
	earlier := chatFollowUpPrompt("Implement workflows", "Fix the CI wait", nil)
	require.NotContains(t, earlier, "agent review")

	prompt := chatFollowUpPrompt(earlier, "Fix the three listed findings", followUpReview("- **major:** CI wait\n~~~\n- **nit:** Standard graph lacks the draft step"))
	require.True(t, strings.HasPrefix(prompt, "Implement workflows"+followUpMarker))
	require.Equal(t, 1, strings.Count(prompt, followUpMarker), "the earlier follow-up is replaced")
	require.NotContains(t, prompt, "Fix the CI wait")
	require.Contains(t, prompt, "Fix the three listed findings")
	require.Contains(t, prompt, "pull request #146 at commit d5e11dd0 (verdict request_changes)")
	require.Contains(t, prompt, "- **nit:** Standard graph lacks the draft step")
	require.Equal(t, 2, strings.Count(prompt, "~~~"), "the findings cannot close their fence")
}

// Within the task prompt limit the review findings and the approved
// instructions keep priority over the original task text.
func TestChatFollowUpPromptStaysWithinTheTaskPromptLimit(t *testing.T) {
	findings := strings.Repeat("é", (domain.MaxAgentReviewFindings-len("END-OF-FINDINGS"))/2) + "END-OF-FINDINGS"
	prompt := chatFollowUpPrompt(strings.Repeat("issue text ", 4000), strings.Repeat("ü", 16000), followUpReview(findings))
	require.LessOrEqual(t, len(prompt), maxTaskPromptBytes)
	require.True(t, utf8.ValidString(prompt))
	require.Contains(t, prompt, "END-OF-FINDINGS")
	require.True(t, strings.HasPrefix(prompt, "issue text "), "some of the original task stays")
	require.Contains(t, prompt, strings.Repeat("ü", 1000))
}
