package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// EnsurePullRequestComment reconciles a marker belonging to this app before
// creating a comment. The caller must serialize calls for the same PR across
// processes. Scanning GitHub also recovers from a successful POST whose response
// was lost. Never retry a POST blindly.
func (c *Client) EnsurePullRequestComment(ctx context.Context, token, fullName string, number int, marker, body string) error {
	endpoint, _, _, found, err := c.findAppMarkerComment(ctx, token, fullName, number, marker)
	if err != nil {
		return err
	}
	if found {
		return nil
	}
	payload, err := json.Marshal(map[string]string{"body": marker + "\n\n" + body})
	if err != nil {
		return err
	}
	return c.commentRequest(ctx, http.MethodPost, endpoint, token, payload, http.StatusCreated, nil)
}

// UpsertPullRequestComment reconciles a marker belonging to this app, creating
// the comment when absent and updating it in place when the body changed, so a
// redelivered opened event never duplicates the comment. The caller must
// serialize calls for the same PR across processes.
func (c *Client) UpsertPullRequestComment(ctx context.Context, token, fullName string, number int, marker, body string) error {
	endpoint, id, existing, found, err := c.findAppMarkerComment(ctx, token, fullName, number, marker)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]string{"body": marker + "\n\n" + body})
	if err != nil {
		return err
	}
	if !found {
		return c.commentRequest(ctx, http.MethodPost, endpoint, token, payload, http.StatusCreated, nil)
	}
	if strings.ReplaceAll(existing, "\r\n", "\n") == marker+"\n\n"+body {
		return nil
	}
	return c.commentRequest(ctx, http.MethodPatch, fmt.Sprintf("%s/%d", endpoint, id), token, payload, http.StatusOK, nil)
}

// findAppMarkerComment scans all comment pages for a marker comment authored by
// this app's bot. It returns the comments endpoint, the found comment's ID and
// body, and whether one was found.
func (c *Client) findAppMarkerComment(ctx context.Context, token, fullName string, number int, marker string) (endpoint string, id int64, existingBody string, found bool, err error) {
	parts := strings.Split(fullName, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || number <= 0 || c.appID <= 0 || marker == "" {
		return "", 0, "", false, fmt.Errorf("invalid pull request comment target")
	}
	endpoint = fmt.Sprintf("%s/repos/%s/%s/issues/%d/comments", githubAPIBaseURL, url.PathEscape(parts[0]), url.PathEscape(parts[1]), number)
	for page := 1; ; page++ {
		var comments []struct {
			ID   int64  `json:"id"`
			Body string `json:"body"`
			User struct {
				Type string `json:"type"`
			} `json:"user"`
			App *struct {
				ID int64 `json:"id"`
			} `json:"performed_via_github_app"`
		}
		if err := c.commentRequest(ctx, http.MethodGet, fmt.Sprintf("%s?per_page=100&page=%d", endpoint, page), token, nil, http.StatusOK, &comments); err != nil {
			return "", 0, "", false, err
		}
		for _, comment := range comments {
			if comment.App != nil && comment.App.ID == c.appID && comment.User.Type == "Bot" && (strings.HasPrefix(comment.Body, marker+"\n") || strings.HasPrefix(comment.Body, marker+"\r\n")) {
				return endpoint, comment.ID, comment.Body, true, nil
			}
		}
		if len(comments) < 100 {
			break
		}
	}
	return endpoint, 0, "", false, nil
}

// Do not include response bodies or transport errors in errors: either can
// contain credentials, request details, or private content.
func (c *Client) commentRequest(ctx context.Context, method, endpoint, token string, payload []byte, expected int, result any) error {
	request, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("building GitHub comment request")
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("GitHub comment request failed")
	}
	defer response.Body.Close()
	if response.StatusCode != expected {
		return fmt.Errorf("GitHub comment API returned HTTP %d", response.StatusCode)
	}
	if result != nil {
		if err := json.NewDecoder(response.Body).Decode(result); err != nil {
			return fmt.Errorf("invalid GitHub comment response")
		}
	}
	return nil
}
