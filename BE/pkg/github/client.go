package github

import (
	"bytes"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	githubAPIBaseURL = "https://api.github.com"
)

// Client communicates with the GitHub API using GitHub App authentication.
type Client struct {
	appID      int64
	privateKey *rsa.PrivateKey
	httpClient *http.Client

	// Token cache
	mu         sync.Mutex
	tokenCache map[int64]cachedToken
}

// GetRepositoryInstallationToken creates a short-lived installation token
// restricted to one repository and the permissions required by Dev Machines.
func (c *Client) GetRepositoryInstallationToken(installationID int64, repository string) (string, time.Time, error) {
	appJWT, err := c.generateAppJWT()
	if err != nil {
		return "", time.Time{}, fmt.Errorf("generating app JWT: %w", err)
	}
	body, err := json.Marshal(map[string]any{
		"repositories": []string{repository},
		"permissions": map[string]string{
			"contents":      "write",
			"pull_requests": "write",
			"metadata":      "read",
		},
	})
	if err != nil {
		return "", time.Time{}, err
	}
	endpoint := fmt.Sprintf("%s/app/installations/%d/access_tokens", githubAPIBaseURL, installationID)
	request, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", time.Time{}, err
	}
	request.Header.Set("Authorization", "Bearer "+appJWT)
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("Content-Type", "application/json")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("requesting repository installation token: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		responseBody, _ := io.ReadAll(response.Body)
		return "", time.Time{}, fmt.Errorf("GitHub API error %d: %s", response.StatusCode, responseBody)
	}
	var result struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return "", time.Time{}, err
	}
	return result.Token, result.ExpiresAt, nil
}

// CreatePullRequest opens a pull request using a repository-scoped installation token.
func (c *Client) CreatePullRequest(token, owner, repository, title, head, base, body string) (*PullRequest, error) {
	payload, err := json.Marshal(map[string]any{
		"title": title, "head": head, "base": base, "body": body,
	})
	if err != nil {
		return nil, err
	}
	endpoint := fmt.Sprintf("%s/repos/%s/%s/pulls", githubAPIBaseURL, owner, repository)
	request, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("Content-Type", "application/json")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		responseBody, _ := io.ReadAll(response.Body)
		return nil, fmt.Errorf("GitHub API error %d: %s", response.StatusCode, responseBody)
	}
	var pullRequest PullRequest
	if err := json.NewDecoder(response.Body).Decode(&pullRequest); err != nil {
		return nil, err
	}
	return &pullRequest, nil
}

type cachedToken struct {
	Token     string
	ExpiresAt time.Time
}

// Repository represents a GitHub repository.
type Repository struct {
	ID            int64  `json:"id"`
	FullName      string `json:"full_name"`
	DefaultBranch string `json:"default_branch"`
	Private       bool   `json:"private"`
}

// PullRequest represents a GitHub pull request.
type PullRequest struct {
	ID        int64      `json:"id"`
	Number    int        `json:"number"`
	Title     string     `json:"title"`
	State     string     `json:"state"`
	Draft     bool       `json:"draft"`
	Merged    bool       `json:"merged"`
	HTMLURL   string     `json:"html_url"`
	Head      PRBranch   `json:"head"`
	Base      PRBranch   `json:"base"`
	User      GitHubUser `json:"user"`
	Additions int        `json:"additions"`
	Deletions int        `json:"deletions"`
	MergedAt  *time.Time `json:"merged_at"`
	ClosedAt  *time.Time `json:"closed_at"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

type PRBranch struct {
	Ref string `json:"ref"`
}

type GitHubUser struct {
	Login     string `json:"login"`
	AvatarURL string `json:"avatar_url"`
}

// Commit represents a GitHub commit.
type Commit struct {
	SHA     string      `json:"sha"`
	HTMLURL string      `json:"html_url"`
	Commit  CommitData  `json:"commit"`
	Author  *GitHubUser `json:"author"`
}

type CommitData struct {
	Message string       `json:"message"`
	Author  CommitAuthor `json:"author"`
}

type CommitAuthor struct {
	Name string    `json:"name"`
	Date time.Time `json:"date"`
}

// NewClient creates a GitHub App API client.
// privateKeyPEM can be raw PEM or base64-encoded PEM.
func NewClient(appID int64, privateKeyPEM string) (*Client, error) {
	keyBytes := []byte(privateKeyPEM)
	// Try base64 decode if it doesn't look like PEM
	if len(keyBytes) > 0 && keyBytes[0] != '-' {
		decoded, err := base64.StdEncoding.DecodeString(privateKeyPEM)
		if err != nil {
			return nil, fmt.Errorf("failed to base64 decode private key: %w", err)
		}
		keyBytes = decoded
	}

	block, _ := pem.Decode(keyBytes)
	if block == nil {
		return nil, fmt.Errorf("failed to parse PEM block")
	}

	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		// Try PKCS8
		parsed, err2 := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err2 != nil {
			return nil, fmt.Errorf("failed to parse private key: %w (also tried PKCS8: %v)", err, err2)
		}
		var ok bool
		key, ok = parsed.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("PKCS8 key is not RSA")
		}
	}

	return &Client{
		appID:      appID,
		privateKey: key,
		httpClient: &http.Client{Timeout: 15 * time.Second},
		tokenCache: make(map[int64]cachedToken),
	}, nil
}

// generateAppJWT creates a short-lived JWT for authenticating as the GitHub App.
func (c *Client) generateAppJWT() (string, error) {
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Issuer:    fmt.Sprintf("%d", c.appID),
		IssuedAt:  jwt.NewNumericDate(now.Add(-60 * time.Second)),
		ExpiresAt: jwt.NewNumericDate(now.Add(10 * time.Minute)),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	return token.SignedString(c.privateKey)
}

// GetInstallationToken retrieves (or returns cached) an installation access token.
func (c *Client) GetInstallationToken(installationID int64) (string, time.Time, error) {
	c.mu.Lock()
	if cached, ok := c.tokenCache[installationID]; ok && time.Now().Before(cached.ExpiresAt.Add(-5*time.Minute)) {
		c.mu.Unlock()
		return cached.Token, cached.ExpiresAt, nil
	}
	c.mu.Unlock()

	appJWT, err := c.generateAppJWT()
	if err != nil {
		return "", time.Time{}, fmt.Errorf("generating app JWT: %w", err)
	}

	url := fmt.Sprintf("%s/app/installations/%d/access_tokens", githubAPIBaseURL, installationID)
	req, _ := http.NewRequest("POST", url, nil)
	req.Header.Set("Authorization", "Bearer "+appJWT)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("requesting installation token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return "", time.Time{}, fmt.Errorf("GitHub API error %d: %s", resp.StatusCode, body)
	}

	var result struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", time.Time{}, fmt.Errorf("decoding token response: %w", err)
	}

	c.mu.Lock()
	c.tokenCache[installationID] = cachedToken{Token: result.Token, ExpiresAt: result.ExpiresAt}
	c.mu.Unlock()

	return result.Token, result.ExpiresAt, nil
}

// ListInstallationRepos lists repositories accessible to an installation.
func (c *Client) ListInstallationRepos(token string) ([]Repository, error) {
	var allRepos []Repository
	page := 1
	for {
		url := fmt.Sprintf("%s/installation/repositories?per_page=100&page=%d", githubAPIBaseURL, page)
		req, _ := http.NewRequest("GET", url, nil)
		req.Header.Set("Authorization", "token "+token)
		req.Header.Set("Accept", "application/vnd.github+json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("GitHub API error %d: %s", resp.StatusCode, body)
		}

		var result struct {
			Repositories []Repository `json:"repositories"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, err
		}
		allRepos = append(allRepos, result.Repositories...)
		if len(result.Repositories) < 100 {
			break
		}
		page++
	}
	return allRepos, nil
}

// GetInstallation fetches installation details by ID.
func (c *Client) GetInstallation(installationID int64) (*Installation, error) {
	appJWT, err := c.generateAppJWT()
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/app/installations/%d", githubAPIBaseURL, installationID)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+appJWT)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API error %d: %s", resp.StatusCode, body)
	}

	var inst Installation
	if err := json.NewDecoder(resp.Body).Decode(&inst); err != nil {
		return nil, err
	}
	return &inst, nil
}

type Installation struct {
	ID      int64 `json:"id"`
	Account struct {
		Login string `json:"login"`
		Type  string `json:"type"`
	} `json:"account"`
}

// doGet is a helper for authenticated GET requests.
func (c *Client) doGet(token, url string, result interface{}) error {
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "token "+token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("GitHub API error %d: %s", resp.StatusCode, body)
	}

	return json.NewDecoder(resp.Body).Decode(result)
}

// GetPullRequest fetches a single PR.
func (c *Client) GetPullRequest(token, owner, repo string, number int) (*PullRequest, error) {
	var pr PullRequest
	url := fmt.Sprintf("%s/repos/%s/%s/pulls/%d", githubAPIBaseURL, owner, repo, number)
	if err := c.doGet(token, url, &pr); err != nil {
		return nil, err
	}
	return &pr, nil
}

// ListPRCommits lists commits on a PR.
func (c *Client) ListPRCommits(token, owner, repo string, number int) ([]Commit, error) {
	var commits []Commit
	url := fmt.Sprintf("%s/repos/%s/%s/pulls/%d/commits?per_page=100", githubAPIBaseURL, owner, repo, number)
	if err := c.doGet(token, url, &commits); err != nil {
		return nil, err
	}
	return commits, nil
}
