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

// githubMergeRequestState is the state both GitHub processors store for a pull
// request. The live processor and the forge adapter share it so a payload
// cannot be interpreted differently by each path.
func githubMergeRequestState(state string, draft, merged bool) string {
	if draft {
		state = "draft"
	}
	if merged {
		state = "merged"
	}
	return state
}

func (githubForge) ParseWebhook(header http.Header, body []byte) (*domain.ForgeEvent, error) {
	switch header.Get("X-GitHub-Event") {
	case "pull_request":
		var payload ghWebhookPR
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil, fmt.Errorf("unmarshaling GitHub pull request: %w", err)
		}
		pr := payload.PullRequest
		state := githubMergeRequestState(pr.State, pr.Draft, pr.Merged)
		if !pr.Merged && payload.Action == "closed" {
			state = "closed"
		} else if state == "" {
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
		var payload ghWebhookPush
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
		var payload ghWebhookCreate
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
