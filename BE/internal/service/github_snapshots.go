package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// RepositorySnapshot downloads the archive of ref of a workspace repository
// for a repository scan. The token can only read the repository's contents,
// is scoped to that one repository and lives in memory for this call only.
func (s *GitHubService) RepositorySnapshot(ctx context.Context, ws uuid.UUID, repository, ref string) (*RepositorySnapshot, error) {
	owner, name, ok := strings.Cut(repository, "/")
	if !ok {
		return nil, fmt.Errorf("invalid repository %q", repository)
	}
	inst, err := s.ghRepo.GetInstallationByWorkspace(ctx, ws)
	if err != nil || inst == nil {
		return nil, fmt.Errorf("GitHub not connected")
	}
	client, _, err := s.getClient(ctx, ws)
	if err != nil {
		return nil, err
	}
	token, _, err := client.GetReadOnlyRepositoryInstallationToken(ctx, inst.InstallationID, name)
	if err != nil {
		return nil, err
	}
	// Pin the commit first, so every file read comes from one commit.
	commit, err := client.GetCommit(token, owner, name, ref)
	if err != nil {
		return nil, err
	}
	archive, err := client.RepositoryTarball(ctx, token, owner, name, commit.SHA)
	if err != nil {
		return nil, err
	}
	return &RepositorySnapshot{Commit: commit.SHA, Archive: archive}, nil
}

var _ repositorySnapshots = (*GitHubService)(nil)
