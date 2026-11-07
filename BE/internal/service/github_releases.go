package service

import (
	"context"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	gh "github.com/kuayle/kuayle-backend/pkg/github"
)

// releaseGitHub is the GitHub side of the release tools. Every call uses an
// installation token scoped to the one repository.
type releaseGitHub interface {
	ListReleases(ctx context.Context, ws uuid.UUID, repository string, limit int) ([]gh.Release, error)
	GetRelease(ctx context.Context, ws uuid.UUID, repository, tag string) (*gh.Release, error)
	ResolveCommit(ctx context.Context, ws uuid.UUID, repository, ref string) (string, error)
	OnBranch(ctx context.Context, ws uuid.UUID, repository, branch, sha string) (bool, error)
	ReleaseRange(ctx context.Context, ws uuid.UUID, repository, base, head string) (*ReleaseRange, error)
	PullRequestSummary(ctx context.Context, ws uuid.UUID, repository string, number int) (*gh.PullRequest, error)
	CommitChecks(ctx context.Context, ws uuid.UUID, repository, sha string) ([]PullRequestCheck, ChecksCoverage, error)
	TagExists(ctx context.Context, ws uuid.UUID, repository, tag string) (bool, error)
	CreateRelease(ctx context.Context, ws uuid.UUID, repository string, release gh.NewRelease) (*gh.Release, error)
	WorkflowRuns(ctx context.Context, ws uuid.UUID, repository, sha string) ([]gh.WorkflowRun, error)
	WorkflowRun(ctx context.Context, ws uuid.UUID, repository string, id int64) (*gh.WorkflowRun, []gh.WorkflowJob, bool, error)
}

var _ releaseGitHub = (*GitHubService)(nil)

// ReleaseRange is what a release adds: the commits and files between the
// previous tag and the target commit.
type ReleaseRange struct {
	// Status is "ahead" when the target descends from the previous tag.
	Status           string
	Messages         []string
	TotalCommits     int
	CommitsTruncated bool
	Files            []gh.CompareFile
	// FilesTruncated means GitHub's 300-file limit was reached, so files are missing.
	FilesTruncated bool
}

// maxReleaseRangePages bounds the comparison: 100 commits per page; GitHub
// sends at most 300 files in all.
const maxReleaseRangePages = 5

func (s *GitHubService) ListReleases(ctx context.Context, ws uuid.UUID, repository string, limit int) ([]gh.Release, error) {
	client, token, owner, name, err := s.repositoryClient(ctx, ws, repository)
	if err != nil {
		return nil, err
	}
	return client.ListReleases(token, owner, name, limit)
}

func (s *GitHubService) GetRelease(ctx context.Context, ws uuid.UUID, repository, tag string) (*gh.Release, error) {
	client, token, owner, name, err := s.repositoryClient(ctx, ws, repository)
	if err != nil {
		return nil, err
	}
	return client.GetReleaseByTag(token, owner, name, tag)
}

func (s *GitHubService) ResolveCommit(ctx context.Context, ws uuid.UUID, repository, ref string) (string, error) {
	client, token, owner, name, err := s.repositoryClient(ctx, ws, repository)
	if err != nil {
		return "", err
	}
	commit, err := client.GetCommit(token, owner, name, ref)
	if err != nil {
		return "", err
	}
	return commit.SHA, nil
}

func (s *GitHubService) OnBranch(ctx context.Context, ws uuid.UUID, repository, branch, sha string) (bool, error) {
	client, token, owner, name, err := s.repositoryClient(ctx, ws, repository)
	if err != nil {
		return false, err
	}
	comparison, err := client.CompareCommits(token, owner, name, branch, sha)
	if err != nil {
		return false, err
	}
	return comparison.Status == "identical" || comparison.Status == "behind", nil
}

func (s *GitHubService) ReleaseRange(ctx context.Context, ws uuid.UUID, repository, base, head string) (*ReleaseRange, error) {
	client, token, owner, name, err := s.repositoryClient(ctx, ws, repository)
	if err != nil {
		return nil, err
	}
	out := &ReleaseRange{}
	// GitHub may send the files on the first page only or page them too; a
	// file is kept once either way.
	seen := map[string]bool{}
	for page := 1; page <= maxReleaseRangePages; page++ {
		comparison, err := client.CompareCommitsPage(token, owner, name, base, head, page)
		if err != nil {
			return nil, err
		}
		if page == 1 {
			out.Status, out.TotalCommits = comparison.Status, comparison.TotalCommits
		}
		for _, commit := range comparison.Commits {
			out.Messages = append(out.Messages, commit.Commit.Message)
		}
		added := 0
		for _, file := range comparison.Files {
			if !seen[file.Filename] {
				seen[file.Filename] = true
				out.Files = append(out.Files, file)
				added++
			}
		}
		if len(comparison.Commits) < 100 && (len(comparison.Files) < 100 || added == 0) {
			break
		}
	}
	out.CommitsTruncated = len(out.Messages) < out.TotalCommits
	out.FilesTruncated = len(out.Files) >= 300
	return out, nil
}

func (s *GitHubService) PullRequestSummary(ctx context.Context, ws uuid.UUID, repository string, number int) (*gh.PullRequest, error) {
	client, token, owner, name, err := s.repositoryClient(ctx, ws, repository)
	if err != nil {
		return nil, err
	}
	return client.GetPullRequest(token, owner, name, number)
}

func (s *GitHubService) CommitChecks(ctx context.Context, ws uuid.UUID, repository, sha string) ([]PullRequestCheck, ChecksCoverage, error) {
	client, token, owner, name, err := s.repositoryClient(ctx, ws, repository)
	if err != nil {
		return nil, ChecksCoverage{}, err
	}
	return commitChecks(client, token, owner, name, sha)
}

func (s *GitHubService) TagExists(ctx context.Context, ws uuid.UUID, repository, tag string) (bool, error) {
	client, token, owner, name, err := s.repositoryClient(ctx, ws, repository)
	if err != nil {
		return false, err
	}
	return client.TagExists(token, owner, name, tag)
}

func (s *GitHubService) CreateRelease(ctx context.Context, ws uuid.UUID, repository string, release gh.NewRelease) (*gh.Release, error) {
	if err := s.requireAbility(ctx, ws, domain.GitHubBotReleases); err != nil {
		return nil, err
	}
	client, token, owner, name, err := s.repositoryClient(ctx, ws, repository)
	if err != nil {
		return nil, err
	}
	return client.CreateRelease(token, owner, name, release)
}

func (s *GitHubService) WorkflowRuns(ctx context.Context, ws uuid.UUID, repository, sha string) ([]gh.WorkflowRun, error) {
	client, token, owner, name, err := s.repositoryClient(ctx, ws, repository)
	if err != nil {
		return nil, err
	}
	return client.ListWorkflowRuns(token, owner, name, sha)
}

func (s *GitHubService) WorkflowRun(ctx context.Context, ws uuid.UUID, repository string, id int64) (*gh.WorkflowRun, []gh.WorkflowJob, bool, error) {
	client, token, owner, name, err := s.repositoryClient(ctx, ws, repository)
	if err != nil {
		return nil, nil, false, err
	}
	run, err := client.GetWorkflowRun(token, owner, name, id)
	if err != nil {
		return nil, nil, false, err
	}
	jobs, truncated, err := client.ListWorkflowJobs(token, owner, name, id)
	if err != nil {
		return nil, nil, false, err
	}
	return run, jobs, truncated, nil
}
