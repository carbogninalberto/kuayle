// Package gitea implements the Gitea and Forgejo side of the forge
// abstraction. Both forges share the same API family, so one implementation is
// constructed with the provider it serves.
package gitea

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kuayle/kuayle-backend/internal/domain"
)

const (
	headerForgejoSignature = "X-Forgejo-Signature"
	headerGiteaSignature   = "X-Gitea-Signature"
	headerForgejoEvent     = "X-Forgejo-Event"
	headerGiteaEvent       = "X-Gitea-Event"
	headerForgejoDelivery  = "X-Forgejo-Delivery"
	headerGiteaDelivery    = "X-Gitea-Delivery"

	eventPullRequest = "pull_request"
	eventPush        = "push"
	eventCreate      = "create"
)

// Provider implements domain.ForgeWebhook and domain.ForgeCommenter for the
// provider it was constructed with (Gitea or Forgejo).
type Provider struct {
	provider domain.ForgeProvider
	client   *http.Client
}

func New(provider domain.ForgeProvider, client *http.Client) *Provider {
	if client == nil {
		client = http.DefaultClient
	}
	return &Provider{provider: provider, client: client}
}

func (p *Provider) Provider() domain.ForgeProvider { return p.provider }

func firstHeader(header http.Header, names ...string) string {
	for _, name := range names {
		if value := header.Get(name); value != "" {
			return value
		}
	}
	return ""
}

// VerifyWebhook checks the hex HMAC-SHA256 signature Gitea and Forgejo send.
func (p *Provider) VerifyWebhook(header http.Header, body []byte, secret string) error {
	if secret == "" {
		return errors.New("gitea webhook secret is not configured")
	}
	signature := firstHeader(header, headerForgejoSignature, headerGiteaSignature)
	if signature == "" {
		return errors.New("gitea webhook signature is missing")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(strings.ToLower(signature)), []byte(expected)) {
		return errors.New("gitea webhook signature does not match")
	}
	return nil
}

func (p *Provider) DeliveryID(header http.Header) string {
	return firstHeader(header, headerForgejoDelivery, headerGiteaDelivery)
}

type repositoryPayload struct {
	ID       int64  `json:"id"`
	FullName string `json:"full_name"`
	HTMLURL  string `json:"html_url"`
}

type pullRequestPayload struct {
	Action      string `json:"action"`
	Number      int    `json:"number"`
	PullRequest struct {
		ID       int64      `json:"id"`
		Number   int        `json:"number"`
		Title    string     `json:"title"`
		Body     string     `json:"body"`
		State    string     `json:"state"`
		Draft    bool       `json:"draft"`
		Merged   bool       `json:"merged"`
		HTMLURL  string     `json:"html_url"`
		MergedAt *time.Time `json:"merged_at"`
		ClosedAt *time.Time `json:"closed_at"`
		Head     struct {
			Ref string `json:"ref"`
		} `json:"head"`
		Base struct {
			Ref string `json:"ref"`
		} `json:"base"`
		User struct {
			Login string `json:"login"`
		} `json:"user"`
	} `json:"pull_request"`
	Repository repositoryPayload `json:"repository"`
}

type pushPayload struct {
	Ref        string            `json:"ref"`
	Repository repositoryPayload `json:"repository"`
	Pusher     struct {
		Login    string `json:"login"`
		Username string `json:"username"`
	} `json:"pusher"`
	Commits []struct {
		ID        string `json:"id"`
		Message   string `json:"message"`
		URL       string `json:"url"`
		Timestamp string `json:"timestamp"`
		Author    struct {
			Name     string `json:"name"`
			Username string `json:"username"`
			Email    string `json:"email"`
		} `json:"author"`
	} `json:"commits"`
}

type createPayload struct {
	Ref        string            `json:"ref"`
	RefType    string            `json:"ref_type"`
	Repository repositoryPayload `json:"repository"`
}

func (p *Provider) ParseWebhook(header http.Header, body []byte) (*domain.ForgeEvent, error) {
	switch firstHeader(header, headerForgejoEvent, headerGiteaEvent) {
	case eventPullRequest:
		return parsePullRequest(body)
	case eventPush:
		return parsePush(body)
	case eventCreate:
		return parseCreate(body)
	default:
		return &domain.ForgeEvent{Kind: domain.ForgeEventIgnored}, nil
	}
}

func parsePullRequest(body []byte) (*domain.ForgeEvent, error) {
	var payload pullRequestPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("unmarshaling Gitea pull request: %w", err)
	}
	pr := payload.PullRequest
	number := pr.Number
	if number <= 0 {
		number = payload.Number
	}
	if number <= 0 {
		return &domain.ForgeEvent{Kind: domain.ForgeEventIgnored}, nil
	}
	externalID := pr.ID
	if externalID <= 0 {
		externalID = int64(number)
	}
	state := "open"
	switch {
	case pr.Merged:
		state = "merged"
	case pr.State == "closed":
		state = "closed"
	case pr.Draft:
		state = "draft"
	}
	action := "updated"
	switch payload.Action {
	case "opened":
		action = "opened"
	case "reopened":
		action = "reopened"
	case "ready_for_review":
		action = "ready"
	case "closed":
		if pr.Merged {
			action = "merged"
		} else {
			action = "closed"
		}
	}
	return &domain.ForgeEvent{
		Kind: domain.ForgeEventMergeRequest,
		Repo: domain.ForgeRepoRef{
			ExternalID: payload.Repository.ID,
			FullName:   payload.Repository.FullName,
			WebURL:     payload.Repository.HTMLURL,
		},
		MergeRequest: &domain.ForgeMergeRequest{
			ExternalID:   externalID,
			Number:       number,
			Title:        pr.Title,
			Description:  pr.Body,
			SourceBranch: pr.Head.Ref,
			TargetBranch: pr.Base.Ref,
			Author:       pr.User.Login,
			WebURL:       pr.HTMLURL,
			State:        state,
			Action:       action,
			MergedAt:     pr.MergedAt,
			ClosedAt:     pr.ClosedAt,
		},
	}, nil
}

func parsePush(body []byte) (*domain.ForgeEvent, error) {
	var payload pushPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("unmarshaling Gitea push: %w", err)
	}
	event := &domain.ForgeEvent{
		Kind: domain.ForgeEventPush,
		Repo: domain.ForgeRepoRef{
			ExternalID: payload.Repository.ID,
			FullName:   payload.Repository.FullName,
			WebURL:     payload.Repository.HTMLURL,
		},
	}
	for _, commit := range payload.Commits {
		committedAt, _ := time.Parse(time.RFC3339, commit.Timestamp)
		if committedAt.IsZero() {
			committedAt = time.Now()
		}
		author := commit.Author.Username
		if author == "" {
			author = commit.Author.Name
		}
		event.Commits = append(event.Commits, domain.ForgeCommit{
			SHA: commit.ID, Message: commit.Message, URL: commit.URL,
			Author: author, CommittedAt: committedAt,
		})
	}
	return event, nil
}

func parseCreate(body []byte) (*domain.ForgeEvent, error) {
	var payload createPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("unmarshaling Gitea create: %w", err)
	}
	if payload.RefType != "branch" || payload.Ref == "" {
		return &domain.ForgeEvent{Kind: domain.ForgeEventIgnored}, nil
	}
	return &domain.ForgeEvent{
		Kind: domain.ForgeEventBranchCreated,
		Repo: domain.ForgeRepoRef{
			ExternalID: payload.Repository.ID,
			FullName:   payload.Repository.FullName,
			WebURL:     payload.Repository.HTMLURL,
		},
		Branch: &domain.ForgeBranch{
			Name:   payload.Ref,
			WebURL: strings.TrimRight(payload.Repository.HTMLURL, "/") + "/src/branch/" + url.PathEscape(payload.Ref),
		},
	}, nil
}

// CommentOnMergeRequest posts an issue comment on the pull request using the
// connection's access token. It never follows redirects.
func (p *Provider) CommentOnMergeRequest(ctx context.Context, conn *domain.ForgeConnection, repo *domain.ForgeRepo, number int, body string) error {
	if conn.AccessToken == nil || *conn.AccessToken == "" {
		return errors.New("gitea access token is not configured")
	}
	owner, name, ok := strings.Cut(repo.FullName, "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
		return errors.New("gitea repository full name is invalid")
	}
	payload, err := json.Marshal(map[string]string{"body": body})
	if err != nil {
		return err
	}
	endpoint := fmt.Sprintf("%s/api/v1/repos/%s/%s/issues/%d/comments",
		strings.TrimRight(conn.BaseURL, "/"), url.PathEscape(owner), url.PathEscape(name), number)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "token "+*conn.AccessToken)
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("gitea comment failed with status %d", resp.StatusCode)
	}
	return nil
}
