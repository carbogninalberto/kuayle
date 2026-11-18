package service

import (
	"context"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/pkg/sanitize"
	log "github.com/sirupsen/logrus"
)

var commentIdentifierPattern = regexp.MustCompile(`^[A-Z][A-Z0-9]+-[0-9]+$`)

// blockLevelTagPattern separates block-level tags before stripping, so
// `<p>a</p><p>b</p>` flattens to `a b` instead of `ab`.
var blockLevelTagPattern = regexp.MustCompile(`(?i)<\s*/?\s*(p|br|li|ul|ol|h[1-6]|div|blockquote|pre|tr|td|th|hr)\b[^>]*>`)

// linkbackEscaper runs after html.EscapeString: it breaks @-mentions, turns
// backticks into entities so no fence survives, and flattens line breaks.
var linkbackEscaper = strings.NewReplacer("@", "@\u200b", "`", "&#96;", "\r", " ", "\n", " ")

const (
	issueLinkbackStart = "<!-- kuayle:issue-linkback:start -->"
	issueLinkbackEnd   = "<!-- kuayle:issue-linkback:end -->"

	maxLinkbackTitleRunes       = 200
	maxLinkbackDescriptionRunes = 280
)

func pullRequestIssueMarker(workspaceID uuid.UUID) string {
	return "<!-- kuayle:pr-issue:" + workspaceID.String() + " -->"
}

func issueLink(frontendURL, slug, identifier string) (string, error) {
	base, err := url.Parse(frontendURL)
	if err != nil || base.Host == "" || (base.Scheme != "https" && base.Scheme != "http") || base.User != nil || base.RawQuery != "" || base.Fragment != "" || slug == "" || !commentIdentifierPattern.MatchString(identifier) {
		return "", fmt.Errorf("invalid issue link configuration")
	}
	// Escape path segments, including markdown delimiters which PathEscape leaves intact.
	link := strings.TrimRight(base.String(), "/") + "/" + url.PathEscape(slug) + "/issue/" + identifier
	return strings.NewReplacer("(", "%28", ")", "%29", "<", "%3C", ">", "%3E").Replace(link), nil
}

// issueCommentBody renders the Linear-style collapsible linkback block for a
// linked issue. The block has no blank lines: CommonMark then treats the whole
// <details> element as one raw HTML block, so Markdown, code fences and
// @-mentions in the issue content are never interpreted by the host.
func issueCommentBody(frontendURL, slug string, issue *domain.Issue) (string, error) {
	if issue == nil {
		return "", fmt.Errorf("invalid issue link configuration")
	}
	link, err := issueLink(frontendURL, slug, issue.Identifier)
	if err != nil {
		return "", err
	}
	// The link is already percent-escaped; escaping again is defensive.
	href := html.EscapeString(link)
	summary := html.EscapeString(issue.Identifier) + " " + linkbackText(issue.Title, maxLinkbackTitleRunes)
	description := linkbackText(flattenIssueDescription(issue.Description), maxLinkbackDescriptionRunes)
	if description == "" {
		description = "No description provided."
	}
	lines := []string{
		issueLinkbackStart,
		"<details>",
		"<summary><a href=\"" + href + "\" rel=\"nofollow noreferrer noopener\" target=\"_blank\">" + summary + "</a></summary>",
		"<p>" + description + "</p>",
		"</details>",
		"<p><a href=\"" + href + "\" rel=\"nofollow noreferrer noopener\" target=\"_blank\">Review in Kuayle</a></p>",
		issueLinkbackEnd,
	}
	return strings.Join(lines, "\n"), nil
}

// flattenIssueDescription converts stored editor HTML (or Markdown) into a
// single line of plain text.
func flattenIssueDescription(description *string) string {
	if description == nil {
		return ""
	}
	flat := blockLevelTagPattern.ReplaceAllString(*description, " ")
	flat = sanitize.StripHTML(flat)
	flat = sanitize.PlainText(flat)
	return strings.Join(strings.Fields(flat), " ")
}

// linkbackText truncates to max runes (appending an ellipsis when cut), then
// HTML-escapes and neutralises mentions, fences and line breaks so untrusted
// content cannot inject markup into or out of the block.
func linkbackText(s string, max int) string {
	runes := []rune(strings.TrimSpace(s))
	if max > 0 && len(runes) > max {
		runes = runes[:max]
		s = strings.TrimSpace(string(runes)) + "…"
	} else {
		s = string(runes)
	}
	return linkbackEscaper.Replace(html.EscapeString(s))
}

func (s *GitHubService) commentOnPullRequest(ctx context.Context, inst *domain.GitHubInstallation, repo *domain.GitHubRepoModel, pr *domain.GitHubPullRequest, issue *domain.Issue) (result error) {
	defer func() {
		if result != nil {
			log.WithFields(log.Fields{"workspace_id": inst.WorkspaceID, "repo_id": repo.ID, "pr_number": pr.Number}).WithError(result).Warn("GitHub issue comment failed; redeliver the opened event after recovery")
		}
	}()
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	bot, err := s.botSettings(ctx, inst.WorkspaceID)
	if err != nil {
		return fmt.Errorf("reading GitHub bot settings")
	}
	if !bot.Enabled(domain.GitHubBotLinkIssues) {
		return nil
	}
	// Resolve credentials before borrowing the publication transaction connection.
	client, _, err := s.getClient(ctx, inst.WorkspaceID)
	if err != nil {
		return fmt.Errorf("configuring GitHub comment client")
	}
	token, _, err := client.GetInstallationTokenContext(ctx, inst.InstallationID)
	if err != nil {
		return fmt.Errorf("authenticating GitHub comment client")
	}
	return s.ghRepo.WithPullRequestCommentLock(ctx, inst, repo, pr.Number, issue.ID, func(slug string) error {
		body, err := issueCommentBody(s.frontendURL, slug, issue)
		if err != nil {
			return err
		}
		marker := pullRequestIssueMarker(inst.WorkspaceID)
		return client.UpsertPullRequestComment(ctx, token, repo.FullName, pr.Number, marker, bot.Signature(s.frontendURL)+body)
	})
}
