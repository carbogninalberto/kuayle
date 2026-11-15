// Package gitlab implements the GitLab side of the forge abstraction: webhook
// verification and parsing plus merge request comments for gitlab.com and
// self-managed instances.
package gitlab

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/kuayle/kuayle-backend/internal/domain"
)

const (
	HeaderEvent       = "X-Gitlab-Event"
	HeaderToken       = "X-Gitlab-Token"
	HeaderEventID     = "X-Gitlab-Event-UUID"
	HeaderIdempotency = "Idempotency-Key"

	mergeRequestHook = "Merge Request Hook"
	pushHook         = "Push Hook"
)

// Provider implements domain.ForgeWebhook and domain.ForgeCommenter.
type Provider struct {
	client *http.Client
}

func New(client *http.Client) *Provider {
	if client == nil {
		client = http.DefaultClient
	}
	return &Provider{client: client}
}

func (p *Provider) Provider() domain.ForgeProvider { return domain.ForgeGitLab }

// VerifyWebhook compares the shared secret token GitLab sends in
// X-Gitlab-Token. An empty configured secret never verifies.
func (p *Provider) VerifyWebhook(header http.Header, _ []byte, secret string) error {
	token := header.Get(HeaderToken)
	if secret == "" || token == "" {
		return errors.New("gitlab webhook token is missing")
	}
	if subtle.ConstantTimeCompare([]byte(token), []byte(secret)) != 1 {
		return errors.New("gitlab webhook token does not match")
	}
	return nil
}

func (p *Provider) DeliveryID(header http.Header) string {
	if id := header.Get(HeaderEventID); id != "" {
		return id
	}
	return header.Get(HeaderIdempotency)
}

type mergeRequestPayload struct {
	ObjectKind string `json:"object_kind"`
	User       struct {
		Username string `json:"username"`
		Name     string `json:"name"`
	} `json:"user"`
	Project struct {
		ID                int64  `json:"id"`
		PathWithNamespace string `json:"path_with_namespace"`
		WebURL            string `json:"web_url"`
	} `json:"project"`
	ObjectAttributes struct {
		ID             int64      `json:"id"`
		IID            int        `json:"iid"`
		Title          string     `json:"title"`
		Description    string     `json:"description"`
		State          string     `json:"state"`
		Action         string     `json:"action"`
		SourceBranch   string     `json:"source_branch"`
		TargetBranch   string     `json:"target_branch"`
		URL            string     `json:"url"`
		Draft          bool       `json:"draft"`
		WorkInProgress bool       `json:"work_in_progress"`
		MergedAt       *time.Time `json:"merged_at"`
		ClosedAt       *time.Time `json:"closed_at"`
		Changes        struct {
			Draft struct {
				Previous bool `json:"previous"`
				Current  bool `json:"current"`
			} `json:"draft"`
		} `json:"changes"`
	} `json:"object_attributes"`
}

type pushPayload struct {
	ObjectKind   string `json:"object_kind"`
	Before       string `json:"before"`
	Ref          string `json:"ref"`
	UserName     string `json:"user_name"`
	UserUsername string `json:"user_username"`
	Project      struct {
		ID                int64  `json:"id"`
		PathWithNamespace string `json:"path_with_namespace"`
		WebURL            string `json:"web_url"`
	} `json:"project"`
	Commits []struct {
		ID        string `json:"id"`
		Message   string `json:"message"`
		URL       string `json:"url"`
		Timestamp string `json:"timestamp"`
		Author    struct {
			Name  string `json:"name"`
			Email string `json:"email"`
		} `json:"author"`
	} `json:"commits"`
}

func (p *Provider) ParseWebhook(header http.Header, body []byte) (*domain.ForgeEvent, error) {
	switch header.Get(HeaderEvent) {
	case mergeRequestHook:
		return parseMergeRequest(body)
	case pushHook:
		return parsePush(body)
	default:
		return &domain.ForgeEvent{Kind: domain.ForgeEventIgnored}, nil
	}
}

func parseMergeRequest(body []byte) (*domain.ForgeEvent, error) {
	var payload mergeRequestPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("unmarshaling GitLab merge request: %w", err)
	}
	attrs := payload.ObjectAttributes
	if attrs.IID <= 0 {
		return &domain.ForgeEvent{Kind: domain.ForgeEventIgnored}, nil
	}
	externalID := attrs.ID
	if externalID <= 0 {
		externalID = int64(attrs.IID)
	}
	state := attrs.State
	switch {
	case attrs.Action == "merge" || state == "merged" || attrs.MergedAt != nil:
		state = "merged"
	case state == "closed":
		state = "closed"
	case attrs.Draft || attrs.WorkInProgress:
		state = "draft"
	default:
		state = "open"
	}
	action := "updated"
	switch attrs.Action {
	case "open":
		action = "opened"
	case "reopen":
		action = "reopened"
	case "close":
		if state == "merged" {
			action = "merged"
		} else {
			action = "closed"
		}
	case "merge":
		action = "merged"
	case "update":
		if attrs.Changes.Draft.Previous && !attrs.Changes.Draft.Current {
			action = "ready"
		}
	}
	return &domain.ForgeEvent{
		Kind: domain.ForgeEventMergeRequest,
		Repo: domain.ForgeRepoRef{
			ExternalID: payload.Project.ID,
			FullName:   payload.Project.PathWithNamespace,
			WebURL:     payload.Project.WebURL,
		},
		MergeRequest: &domain.ForgeMergeRequest{
			ExternalID:   externalID,
			Number:       attrs.IID,
			Title:        attrs.Title,
			Description:  attrs.Description,
			SourceBranch: attrs.SourceBranch,
			TargetBranch: attrs.TargetBranch,
			Author:       payload.User.Username,
			WebURL:       attrs.URL,
			State:        state,
			Action:       action,
			MergedAt:     attrs.MergedAt,
			ClosedAt:     attrs.ClosedAt,
		},
	}, nil
}

func parsePush(body []byte) (*domain.ForgeEvent, error) {
	var payload pushPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("unmarshaling GitLab push: %w", err)
	}
	event := &domain.ForgeEvent{
		Kind: domain.ForgeEventPush,
		Repo: domain.ForgeRepoRef{
			ExternalID: payload.Project.ID,
			FullName:   payload.Project.PathWithNamespace,
			WebURL:     payload.Project.WebURL,
		},
	}
	for _, commit := range payload.Commits {
		committedAt, _ := time.Parse(time.RFC3339, commit.Timestamp)
		if committedAt.IsZero() {
			committedAt = time.Now()
		}
		author := commit.Author.Name
		if author == "" {
			author = payload.UserUsername
		}
		event.Commits = append(event.Commits, domain.ForgeCommit{
			SHA: commit.ID, Message: commit.Message, URL: commit.URL,
			Author: author, CommittedAt: committedAt,
		})
	}
	// GitLab has no branch-created event; a push whose previous tip is all
	// zeroes is the creation of its branch.
	if isZeroSHA(payload.Before) && strings.HasPrefix(payload.Ref, "refs/heads/") {
		name := strings.TrimPrefix(payload.Ref, "refs/heads/")
		event.Branch = &domain.ForgeBranch{Name: name, WebURL: branchURL(payload.Project.WebURL, name)}
	}
	return event, nil
}

func isZeroSHA(sha string) bool {
	if sha == "" {
		return false
	}
	for _, c := range sha {
		if c != '0' {
			return false
		}
	}
	return true
}

func branchURL(base, branch string) string {
	return strings.TrimRight(base, "/") + "/-/tree/" + url.PathEscape(branch)
}

// CommentOnMergeRequest posts a note on a merge request with the connection's
// access token. It never follows redirects.
func (p *Provider) CommentOnMergeRequest(ctx context.Context, conn *domain.ForgeConnection, repo *domain.ForgeRepo, number int, body string) error {
	if conn.AccessToken == nil || *conn.AccessToken == "" {
		return errors.New("gitlab access token is not configured")
	}
	payload, err := json.Marshal(map[string]string{"body": body})
	if err != nil {
		return err
	}
	endpoint := fmt.Sprintf("%s/api/v4/projects/%s/merge_requests/%d/notes",
		strings.TrimRight(conn.BaseURL, "/"), url.PathEscape(strconv.FormatInt(repo.ExternalRepoID, 10)), number)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("PRIVATE-TOKEN", *conn.AccessToken)
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("gitlab comment failed with status %d", resp.StatusCode)
	}
	return nil
}
