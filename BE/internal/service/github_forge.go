package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/kuayle/kuayle-backend/internal/domain"
)

// githubForge adapts GitHub's webhook payloads to the normalized forge event.
// GitHub's own webhook flow keeps its dedicated processor because it carries
// behaviour (heads, task reviews, bot, check suites, mentions) the generic
// forge service does not implement.
type githubForge struct{}

var _ domain.ForgeWebhook = githubForge{}

func (githubForge) Provider() domain.ForgeProvider { return domain.ForgeGitHub }

func (githubForge) VerifyWebhook(header http.Header, body []byte, secret string) error {
	if secret == "" {
		return fmt.Errorf("github webhook secret is not configured")
	}
	if !verifyGitHubWebhookSignature(secret, body, header.Get("X-Hub-Signature-256")) {
		return fmt.Errorf("github webhook signature does not match")
	}
	return nil
}

func (githubForge) DeliveryID(header http.Header) string {
	return header.Get("X-GitHub-Delivery")
}

func verifyGitHubWebhookSignature(secret string, payload []byte, signature string) bool {
	sig := strings.TrimPrefix(signature, "sha256=")
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(sig), []byte(expected))
}

type githubForgePRPayload struct {
	Action      string `json:"action"`
	Number      int    `json:"number"`
	PullRequest struct {
		ID      int64  `json:"id"`
		Number  int    `json:"number"`
		Title   string `json:"title"`
		State   string `json:"state"`
		Draft   bool   `json:"draft"`
		Merged  bool   `json:"merged"`
		HTMLURL string `json:"html_url"`
		Body    string `json:"body"`
		Head    struct {
			Ref string `json:"ref"`
		} `json:"head"`
		Base struct {
			Ref string `json:"ref"`
		} `json:"base"`
		User struct {
			Login string `json:"login"`
		} `json:"user"`
		MergedAt *time.Time `json:"merged_at"`
		ClosedAt *time.Time `json:"closed_at"`
	} `json:"pull_request"`
	Repository struct {
		ID       int64  `json:"id"`
		FullName string `json:"full_name"`
		HTMLURL  string `json:"html_url"`
	} `json:"repository"`
}

type githubForgePushPayload struct {
	Ref     string `json:"ref"`
	Commits []struct {
		ID        string `json:"id"`
		Message   string `json:"message"`
		URL       string `json:"url"`
		Timestamp string `json:"timestamp"`
		Author    struct {
			Username string `json:"username"`
			Name     string `json:"name"`
		} `json:"author"`
	} `json:"commits"`
	Repository struct {
		ID       int64  `json:"id"`
		FullName string `json:"full_name"`
		HTMLURL  string `json:"html_url"`
	} `json:"repository"`
}

type githubForgeCreatePayload struct {
	RefType    string `json:"ref_type"`
	Ref        string `json:"ref"`
	Repository struct {
		ID       int64  `json:"id"`
		FullName string `json:"full_name"`
		HTMLURL  string `json:"html_url"`
	} `json:"repository"`
}

func (githubForge) ParseWebhook(header http.Header, body []byte) (*domain.ForgeEvent, error) {
	switch header.Get("X-GitHub-Event") {
	case "pull_request":
		var payload githubForgePRPayload
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil, fmt.Errorf("unmarshaling GitHub pull request: %w", err)
		}
		pr := payload.PullRequest
		state := pr.State
		switch {
		case pr.Merged:
			state = "merged"
		case payload.Action == "closed":
			state = "closed"
		case pr.Draft:
			state = "draft"
		case state == "":
			state = "open"
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
		number := pr.Number
		if number == 0 {
			number = payload.Number
		}
		return &domain.ForgeEvent{
			Kind: domain.ForgeEventMergeRequest,
			Repo: domain.ForgeRepoRef{
				ExternalID: payload.Repository.ID,
				FullName:   payload.Repository.FullName,
				WebURL:     payload.Repository.HTMLURL,
			},
			MergeRequest: &domain.ForgeMergeRequest{
				ExternalID:   pr.ID,
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
	case "push":
		var payload githubForgePushPayload
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil, fmt.Errorf("unmarshaling GitHub push: %w", err)
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
	case "create":
		var payload githubForgeCreatePayload
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil, fmt.Errorf("unmarshaling GitHub create: %w", err)
		}
		if payload.RefType != "branch" {
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
				WebURL: strings.TrimRight(payload.Repository.HTMLURL, "/") + "/tree/" + payload.Ref,
			},
		}, nil
	default:
		return &domain.ForgeEvent{Kind: domain.ForgeEventIgnored}, nil
	}
}
