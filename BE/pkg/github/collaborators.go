package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// PermissionNone is GitHub's answer for a user without access to a repository.
const PermissionNone = "none"

// GetCollaboratorPermission returns the permission GitHub reports for a user
// on a repository: admin, write, read or none (maintain is reported as write
// and triage as read). An unknown user has none. The endpoint needs only the
// App's Metadata: read permission, which every GitHub App has.
//
// Errors carry the HTTP status only: response bodies and transport errors can
// contain request details.
func (c *Client) GetCollaboratorPermission(ctx context.Context, token, owner, repo, username string) (string, error) {
	if owner == "" || repo == "" || username == "" {
		return "", fmt.Errorf("invalid collaborator permission target")
	}
	endpoint := fmt.Sprintf("%s/repos/%s/%s/collaborators/%s/permission", githubAPIBaseURL, url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(username))
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("building GitHub permission request")
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("GitHub permission request failed")
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return PermissionNone, nil
	}
	if response.StatusCode != http.StatusOK {
		return "", &APIError{Status: response.StatusCode}
	}
	var result struct {
		Permission string `json:"permission"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&result); err != nil || result.Permission == "" {
		return "", fmt.Errorf("invalid GitHub permission response")
	}
	return result.Permission, nil
}
