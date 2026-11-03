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
	commits, err := client.CompareCommits(token, owner, name, published, head)
	if err != nil {
		return nil, err
	}
	publishedCommit, err := client.GetCommit(token, owner, name, published)
	if err != nil {
		return nil, err
	}
	headTree := publishedCommit.Commit.Tree.SHA
	if len(commits) > 0 {
		headTree = commits[len(commits)-1].Commit.Tree.SHA
	}
	return headChange(commits, publishedCommit.Commit.Tree.SHA != headTree), nil
}

// headChange summarizes commits added after publication. Merge commits are
// merges of another branch (normally the base) into the task branch; when every
// added commit is one, the change is labelled a base merge.
func headChange(commits []gh.Commit, treeChanged bool) *domain.PullRequestHeadChange {
	change := &domain.PullRequestHeadChange{Kind: "base_merge", TreeChanged: treeChanged, Commits: []domain.PullRequestHeadCommit{}}
	if len(commits) == 0 {
		change.Kind = "pushed"
	}
	for i, commit := range commits {
		merge := len(commit.Parents) > 1
		if !merge {
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
