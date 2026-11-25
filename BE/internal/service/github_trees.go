package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	gh "github.com/kuayle/kuayle-backend/pkg/github"
)

// repositoryTrees reads git trees of a workspace repository through GitHub's
// trees API, so a listing never downloads an archive (KUA-319).
type repositoryTrees interface {
	// RepositoryCommitTree returns the root tree SHA of a commit.
	RepositoryCommitTree(ctx context.Context, ws uuid.UUID, repository, commit string) (string, error)
	// RepositoryTree reads one tree by SHA: its entries, or with recursive
	// every path below it.
	RepositoryTree(ctx context.Context, ws uuid.UUID, repository, tree string, recursive bool) (*gh.TreeListing, error)
}

// readTokenTTL bounds how long one repository's read-only installation token
// is reused by tree reads, in memory only, so expanding folders one after the
// other does not mint a token each time. GitHub's own lifetime is an hour.
const readTokenTTL = 5 * time.Minute

type readToken struct {
	value   string
	expires time.Time
}

// GitHubTrees reads trees with the workspace's GitHub App installation, with
// a token that can only read the repository's contents.
type GitHubTrees struct {
	github *GitHubService
	now    func() time.Time

	mu     sync.Mutex
	tokens map[string]readToken
}

func NewGitHubTrees(github *GitHubService) *GitHubTrees {
	return &GitHubTrees{github: github, now: time.Now, tokens: map[string]readToken{}}
}

var _ repositoryTrees = (*GitHubTrees)(nil)

func (t *GitHubTrees) access(ctx context.Context, ws uuid.UUID, repository string) (*gh.Client, string, string, string, error) {
	owner, name, ok := strings.Cut(repository, "/")
	if !ok || owner == "" || name == "" {
		return nil, "", "", "", fmt.Errorf("invalid repository %q", repository)
	}
	client, _, err := t.github.getClient(ctx, ws)
	if err != nil {
		return nil, "", "", "", err
	}
	key := ws.String() + "/" + repository
	t.mu.Lock()
	cached, ok := t.tokens[key]
	t.mu.Unlock()
	if ok && t.now().Before(cached.expires) {
		return client, owner, name, cached.value, nil
	}
	inst, err := t.github.ghRepo.GetInstallationByWorkspace(ctx, ws)
	if err != nil || inst == nil {
		return nil, "", "", "", fmt.Errorf("GitHub not connected")
	}
	token, expires, err := client.GetReadOnlyRepositoryInstallationToken(ctx, inst.InstallationID, name)
	if err != nil {
		return nil, "", "", "", err
	}
	// Reuse it for a few minutes at most, and never past its own expiry.
	expires = earlier(expires.Add(-time.Minute), t.now().Add(readTokenTTL))
	t.mu.Lock()
	t.tokens[key] = readToken{value: token, expires: expires}
	for k, v := range t.tokens {
		if !t.now().Before(v.expires) {
			delete(t.tokens, k)
		}
	}
	t.mu.Unlock()
	return client, owner, name, token, nil
}

func earlier(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func (t *GitHubTrees) RepositoryCommitTree(ctx context.Context, ws uuid.UUID, repository, commit string) (string, error) {
	client, owner, name, token, err := t.access(ctx, ws, repository)
	if err != nil {
		return "", err
	}
	return client.GetCommitTree(ctx, token, owner, name, commit)
}

func (t *GitHubTrees) RepositoryTree(ctx context.Context, ws uuid.UUID, repository, tree string, recursive bool) (*gh.TreeListing, error) {
	client, owner, name, token, err := t.access(ctx, ws, repository)
	if err != nil {
		return nil, err
	}
	return client.GetTree(ctx, token, owner, name, tree, recursive)
}
