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

// installationAccount is the account an installation belongs to, as GitHub
// reports it to the installing user.
type installationAccount struct {
	ID         int64
	Login      string
	TargetType string
}

// UserControlsInstallation exchanges the OAuth code GitHub issues during an
// app installation and reports whether that user may bind installationID: the
// installation must be listed for the user and belong either to the user's
// own account or to an organization the user is an active admin of.
//
// The installation_id in the setup redirect is otherwise unauthenticated, and
// GitHub lists an installation to every user who can reach one repository it
// covers, so being listed alone does not show control of the installation.
// The account is read from GitHub's answer for this grant and compared by
// numeric id; a failed or forbidden lookup is an error and never an approval.
func UserControlsInstallation(ctx context.Context, client *http.Client, clientID, clientSecret, code string, installationID int64) (bool, error) {
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

	account, err := listedInstallationAccount(ctx, client, grant.AccessToken, installationID)
	if err != nil || account == nil {
		return false, err
	}
	if account.ID <= 0 {
		return false, nil
	}
	switch account.TargetType {
	case "User":
		var user struct {
			ID int64 `json:"id"`
		}
		if err := getAsUser(ctx, client, grant.AccessToken, "/user", &user); err != nil {
			return false, fmt.Errorf("reading installing user: %w", err)
		}
		return user.ID == account.ID, nil
	case "Organization":
		if account.Login == "" {
			return false, nil
		}
		var membership struct {
			Role         string `json:"role"`
			State        string `json:"state"`
			Organization struct {
				ID int64 `json:"id"`
			} `json:"organization"`
		}
		if err := getAsUser(ctx, client, grant.AccessToken, "/user/memberships/orgs/"+url.PathEscape(account.Login), &membership); err != nil {
			return false, fmt.Errorf("reading organization membership: %w", err)
		}
		return membership.Organization.ID == account.ID && membership.Role == "admin" && membership.State == "active", nil
	}
	// Enterprise and unknown targets have no owner check here.
	return false, nil
}

// listedInstallationAccount returns the account of installationID when GitHub
// lists that installation for the user, and nil otherwise.
func listedInstallationAccount(ctx context.Context, client *http.Client, token string, installationID int64) (*installationAccount, error) {
	for page := 1; ; page++ {
		var result struct {
			Installations []struct {
				ID         int64  `json:"id"`
				TargetType string `json:"target_type"`
				Account    struct {
					ID    int64  `json:"id"`
					Login string `json:"login"`
					Type  string `json:"type"`
				} `json:"account"`
			} `json:"installations"`
		}
		if err := getAsUser(ctx, client, token, fmt.Sprintf("/user/installations?per_page=100&page=%d", page), &result); err != nil {
			return nil, fmt.Errorf("listing user installations: %w", err)
		}
		for _, installation := range result.Installations {
			if installation.ID != installationID {
				continue
			}
			targetType := installation.TargetType
			if targetType == "" {
				targetType = installation.Account.Type
			}
			return &installationAccount{ID: installation.Account.ID, Login: installation.Account.Login, TargetType: targetType}, nil
		}
		if len(result.Installations) < 100 {
			return nil, nil
		}
	}
}

// getAsUser performs a GET on the fixed GitHub API host with the installing
// user's OAuth token.
func getAsUser(ctx context.Context, client *http.Client, token, path string, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, githubAPIBaseURL+path, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/vnd.github+json")
	return doGitHubJSON(client, request, target)
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
