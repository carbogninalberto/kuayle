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
	if result.Change, err = compare(ctx, ws, repository, number, published, head); err != nil {
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

// maxCompareFilePages bounds file listing; GitHub caps a comparison at 300 files.
const maxCompareFilePages = 3

func (s *GitHubService) githubCompareCommits(ctx context.Context, ws uuid.UUID, repository string, number int, published, head string) (*domain.PullRequestHeadChange, error) {
	client, token, owner, name, err := s.repositoryClient(ctx, ws, repository)
	if err != nil {
		return nil, err
	}
	pull, err := client.GetPullRequest(token, owner, name, number)
	if err != nil {
		return nil, err
	}
	base := pull.Base.Ref
	comparison, err := client.CompareCommits(token, owner, name, published, head)
	if err != nil {
		return nil, err
	}
	headCommit, err := client.GetCommit(token, owner, name, head)
	if err != nil {
		return nil, err
	}
	inBase := func(sha string) (bool, error) {
		c, err := client.CompareCommits(token, owner, name, base, sha)
		if err != nil {
			return false, err
		}
		return c.Status == "behind" || c.Status == "identical", nil
	}
	// The PR's own files: what the branch changes relative to the base branch,
	// before and after. Unequal or unreadable file sets count as a change.
	prFiles := func(sha string) (map[string]string, bool) {
		files := map[string]string{}
		for page := 1; page <= maxCompareFilePages; page++ {
			c, err := client.CompareCommitsPage(token, owner, name, base, sha, page)
			if err != nil {
				return nil, false
			}
			for _, f := range c.Files {
				files[f.Filename] = f.SHA
			}
			if len(c.Files) < 100 {
				return files, len(files) < 300
			}
		}
		return nil, false
	}
	before, okBefore := prFiles(published)
	after, okAfter := prFiles(head)
	filesChanged := !okBefore || !okAfter || len(before) != len(after)
	if !filesChanged {
		for f, sha := range before {
			if after[f] != sha {
				filesChanged = true
				break
			}
		}
	}
	return headChange(comparison, headCommit, published, filesChanged, inBase)
}

// headChange summarizes the commits added after publication. Only commits on
// the head's first-parent chain back to the published commit are listed: the
// commits a merge brings in from the base branch are reachable from head too,
// but they are not what was added to the task. The change is a "merge" when
// every such commit merges in only commits already on the base branch, "pushed"
// when someone committed work on the branch, and "rewritten" when the history no
// longer contains the published commit. filesChanged says whether the PR's own
// changed files differ from what was published.
func headChange(comparison *gh.Comparison, head *gh.Commit, published string, filesChanged bool, inBase func(sha string) (bool, error)) (*domain.PullRequestHeadChange, error) {
	bySHA := make(map[string]gh.Commit, len(comparison.Commits)+1)
	for _, c := range comparison.Commits {
		bySHA[c.SHA] = c
	}
	bySHA[head.SHA] = *head
	var chain []gh.Commit
	for cur, ok := bySHA[head.SHA], true; ok && cur.SHA != published; {
		chain = append(chain, cur)
		if len(cur.Parents) == 0 {
			break
		}
		cur, ok = bySHA[cur.Parents[0].SHA]
	}
	change := &domain.PullRequestHeadChange{Kind: "merge", TreeChanged: filesChanged, Commits: []domain.PullRequestHeadCommit{}}
	if comparison.Status != "ahead" {
		change.Kind = "rewritten"
	}
	change.TotalCommits = len(chain)
	change.Truncated = len(chain) > maxHeadChangeCommits
	for i := len(chain) - 1; i >= 0; i-- {
		commit := chain[i]
		merge := len(commit.Parents) > 1
		if change.Kind == "merge" && !merge {
			change.Kind = "pushed"
		}
		for _, parent := range commit.Parents[min(1, len(commit.Parents)):] {
			if change.Kind != "merge" {
				break
			}
			reachable, err := inBase(parent.SHA)
			if err != nil {
				return nil, err
			}
			if !reachable {
				change.Kind = "pushed"
			}
		}
		if len(change.Commits) >= maxHeadChangeCommits {
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
	return change, nil
}
