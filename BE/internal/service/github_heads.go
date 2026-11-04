package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	gh "github.com/kuayle/kuayle-backend/pkg/github"
)

const maxHeadChangeCommits = 20

type pullRequestHeadStore interface {
	RecordPullRequestHead(ctx context.Context, ws uuid.UUID, repository string, number int, sha string) error
}

// SetPullRequestHeads lets webhooks and merge checks record the head GitHub
// reports for agent task pull requests.
func (s *GitHubService) SetPullRequestHeads(heads pullRequestHeadStore) {
	s.heads = heads
}

// InspectPullRequestHead reads the pull request's head from GitHub right now,
// records it, and when it is not the published commit describes the commits
// others added: their authors and whether the tree changed.
func (s *GitHubService) InspectPullRequestHead(ctx context.Context, ws uuid.UUID, repository string, number int, published string) (*domain.PullRequestHead, error) {
	read := s.readPullRequestHead
	if read == nil {
		read = s.githubPullRequestHead
	}
	head, err := read(ctx, ws, repository, number)
	if err != nil {
		return nil, err
	}
	if s.heads != nil {
		if err = s.heads.RecordPullRequestHead(ctx, ws, repository, number, head); err != nil {
			return nil, err
		}
	}
	result := &domain.PullRequestHead{SHA: head}
	if head == published {
		return result, nil
	}
	compare := s.compareCommits
	if compare == nil {
		compare = s.githubCompareCommits
	}
	if result.Change, err = compare(ctx, ws, repository, published, head); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *GitHubService) repositoryClient(ctx context.Context, ws uuid.UUID, repository string) (*gh.Client, string, string, string, error) {
	owner, name, ok := strings.Cut(repository, "/")
	if !ok {
		return nil, "", "", "", fmt.Errorf("invalid repository %q", repository)
	}
	inst, err := s.ghRepo.GetInstallationByWorkspace(ctx, ws)
	if err != nil || inst == nil {
		return nil, "", "", "", fmt.Errorf("GitHub not connected")
	}
	client, _, err := s.getClient(ctx, ws)
	if err != nil {
		return nil, "", "", "", err
	}
	token, _, err := client.GetRepositoryInstallationToken(inst.InstallationID, name)
	if err != nil {
		return nil, "", "", "", err
	}
	return client, token, owner, name, nil
}

func (s *GitHubService) githubPullRequestHead(ctx context.Context, ws uuid.UUID, repository string, number int) (string, error) {
	client, token, owner, name, err := s.repositoryClient(ctx, ws, repository)
	if err != nil {
		return "", err
	}
	pull, err := client.GetPullRequest(token, owner, name, number)
	if err != nil {
		return "", err
	}
	if pull.Head.SHA == "" {
		return "", fmt.Errorf("GitHub returned no head commit")
	}
	return pull.Head.SHA, nil
}

func (s *GitHubService) githubCompareCommits(ctx context.Context, ws uuid.UUID, repository, published, head string) (*domain.PullRequestHeadChange, error) {
	client, token, owner, name, err := s.repositoryClient(ctx, ws, repository)
	if err != nil {
		return nil, err
	}
	comparison, err := client.CompareCommits(token, owner, name, published, head)
	if err != nil {
		return nil, err
	}
	// Trees come from the two commits themselves: the compare result is one
	// page, so its last commit is not necessarily the head.
	publishedCommit, err := client.GetCommit(token, owner, name, published)
	if err != nil {
		return nil, err
	}
	headCommit, err := client.GetCommit(token, owner, name, head)
	if err != nil {
		return nil, err
	}
	treeChanged := publishedCommit.Commit.Tree.SHA == "" || publishedCommit.Commit.Tree.SHA != headCommit.Commit.Tree.SHA
	return headChange(comparison, treeChanged), nil
}

// headChange summarizes commits added after publication. When every added
// commit has several parents the change is a "merge" (normally the base branch
// merged in; the branch is not verified, so the tree decides whether it is
// harmless). A history that no longer contains the published commit is
// "rewritten". An empty comparison cannot say what changed, so the tree is
// reported as changed.
func headChange(comparison *gh.Comparison, treeChanged bool) *domain.PullRequestHeadChange {
	commits := comparison.Commits
	change := &domain.PullRequestHeadChange{Kind: "merge", TreeChanged: treeChanged, Commits: []domain.PullRequestHeadCommit{}}
	if len(commits) == 0 {
		change.Kind, change.TreeChanged = "pushed", true
	}
	if comparison.TotalCommits > len(commits) {
		change.Truncated = true
	}
	if comparison.Status != "ahead" {
		change.Kind = "rewritten"
	}
	for i, commit := range commits {
		merge := len(commit.Parents) > 1
		if !merge && change.Kind == "merge" {
			change.Kind = "pushed"
		}
		if i >= maxHeadChangeCommits {
			change.Truncated = true
			continue
		}
		author := commit.Commit.Author.Name
		if commit.Author != nil && commit.Author.Login != "" {
			author = commit.Author.Login
		}
		subject, _, _ := strings.Cut(commit.Commit.Message, "\n")
		change.Commits = append(change.Commits, domain.PullRequestHeadCommit{
			SHA: commit.SHA, Author: truncateRunes(author, 80), Subject: truncateRunes(subject, 200), Merge: merge,
		})
	}
	return change
}
