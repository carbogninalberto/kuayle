package service

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/kuayle/kuayle-backend/internal/domain"
)

var commentIdentifierPattern = regexp.MustCompile(`^[A-Z][A-Z0-9]+-[0-9]+$`)

func issueCommentBody(frontendURL, slug, identifier string) (string, error) {
	base, err := url.Parse(frontendURL)
	if err != nil || base.Host == "" || (base.Scheme != "https" && base.Scheme != "http") || base.User != nil || base.RawQuery != "" || base.Fragment != "" || slug == "" || !commentIdentifierPattern.MatchString(identifier) {
		return "", fmt.Errorf("invalid issue link configuration")
	}
	// Escape path segments, including markdown delimiters which PathEscape leaves intact.
	link := strings.TrimRight(base.String(), "/") + "/" + url.PathEscape(slug) + "/issue/" + identifier
	link = strings.NewReplacer("(", "%28", ")", "%29", "<", "%3C", ">", "%3E").Replace(link)
	return fmt.Sprintf("Linked Kuayle issue: [%s](%s)", identifier, link), nil
}

func (s *GitHubService) commentOnPullRequest(ctx context.Context, inst *domain.GitHubInstallation, repo *domain.GitHubRepoModel, pr *domain.GitHubPullRequest, issue *domain.Issue) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return s.ghRepo.WithPullRequestCommentLock(ctx, inst, repo, pr.Number, issue.ID, func(slug string) error {
		body, err := issueCommentBody(s.frontendURL, slug, issue.Identifier)
		if err != nil {
			return err
		}
		client, _, err := s.getClient(ctx, inst.WorkspaceID)
		if err != nil {
			return fmt.Errorf("configuring GitHub comment client")
		}
		token, _, err := client.GetInstallationToken(inst.InstallationID)
		if err != nil {
			return fmt.Errorf("authenticating GitHub comment client")
		}
		marker := "<!-- kuayle:pr-issue:" + inst.WorkspaceID.String() + " -->"
		return client.EnsurePullRequestComment(ctx, token, repo.FullName, pr.Number, marker, body)
	})
}
