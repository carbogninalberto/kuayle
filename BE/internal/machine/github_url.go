package machine

import (
	"net/url"
	"strconv"
	"strings"
)

func safeGitHubPullRequestURL(value, repositoryFullName string) (string, bool) {
	repository := strings.Split(repositoryFullName, "/")
	if len(repository) != 2 || repository[0] == "" || repository[1] == "" {
		return "", false
	}

	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || !strings.EqualFold(parsed.Hostname(), "github.com") ||
		parsed.User != nil || parsed.Port() != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", false
	}

	path := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(path) != 4 || !strings.EqualFold(path[0], repository[0]) || !strings.EqualFold(path[1], repository[1]) || path[2] != "pull" {
		return "", false
	}
	number, err := strconv.Atoi(path[3])
	if err != nil || number < 1 {
		return "", false
	}

	return parsed.String(), true
}
