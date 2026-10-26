package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const githubOAuthTokenURL = "https://github.com/login/oauth/access_token"

// UserCanAccessInstallation exchanges the OAuth code GitHub issues during an
// app installation and reports whether that user can access installationID.
// The installation_id in the setup redirect is otherwise unauthenticated.
func UserCanAccessInstallation(ctx context.Context, client *http.Client, clientID, clientSecret, code string, installationID int64) (bool, error) {
	if client == nil {
		client = http.DefaultClient
	}
	form := url.Values{"client_id": {clientID}, "client_secret": {clientSecret}, "code": {code}}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, githubOAuthTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return false, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var grant struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := doGitHubJSON(client, request, &grant); err != nil {
		return false, fmt.Errorf("exchanging installation authorization: %w", err)
	}
	if grant.AccessToken == "" {
		return false, fmt.Errorf("installation authorization was rejected: %s", grant.Error)
	}

	for page := 1; ; page++ {
		endpoint := fmt.Sprintf("%s/user/installations?per_page=100&page=%d", githubAPIBaseURL, page)
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return false, err
		}
		request.Header.Set("Authorization", "Bearer "+grant.AccessToken)
		request.Header.Set("Accept", "application/vnd.github+json")
		var result struct {
			Installations []struct {
				ID int64 `json:"id"`
			} `json:"installations"`
		}
		if err := doGitHubJSON(client, request, &result); err != nil {
			return false, fmt.Errorf("listing user installations: %w", err)
		}
		for _, installation := range result.Installations {
			if installation.ID == installationID {
				return true, nil
			}
		}
		if len(result.Installations) < 100 {
			return false, nil
		}
	}
}

func doGitHubJSON(client *http.Client, request *http.Request, target any) error {
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("GitHub API error %d", response.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(target)
}
