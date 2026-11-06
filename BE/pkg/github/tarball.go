package github

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// codeloadHost serves the archives the tarball endpoint redirects to.
const codeloadHost = "codeload.github.com"

// RepositoryTarball streams the gzipped tarball of ref. GitHub answers with a
// redirect to a short-lived codeload URL; only that host is followed, over
// HTTPS and without the Authorization header, so the installation token is
// sent to api.github.com alone. The caller closes the body and bounds it.
func (c *Client) RepositoryTarball(ctx context.Context, token, owner, repo, ref string) (io.ReadCloser, error) {
	endpoint := fmt.Sprintf("%s/repos/%s/%s/tarball/%s", githubAPIBaseURL, url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(ref))
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "token "+token)
	request.Header.Set("Accept", "application/vnd.github+json")
	client := *c.httpClient
	// The archive may take longer than an API call; the caller's context bounds it.
	client.Timeout = 0
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("requesting repository archive: %w", err)
	}
	if response.StatusCode == http.StatusOK {
		return response.Body, nil
	}
	location := response.Header.Get("Location")
	status := response.StatusCode
	if status < 300 || status >= 400 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		response.Body.Close()
		return nil, &APIError{Status: status, Message: string(body)}
	}
	response.Body.Close()
	target, err := url.Parse(location)
	if err != nil || target.Scheme != "https" || target.Host != codeloadHost || target.User != nil {
		return nil, errors.New("GitHub redirected the repository archive to an unexpected location")
	}
	archive, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, err
	}
	response, err = client.Do(archive)
	if err != nil {
		return nil, fmt.Errorf("downloading repository archive: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		response.Body.Close()
		return nil, &APIError{Status: response.StatusCode, Message: string(body)}
	}
	return response.Body, nil
}
