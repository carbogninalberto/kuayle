package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kuayle/kuayle-backend/internal/domain"
)

// authorCanWrite reports whether the author of a GitHub comment or review may
// push to the linked repository, which is what lets them steer a coding agent.
//
// The author association in a webhook says how someone relates to the
// repository, not what they may do there:
//   - OWNER owns the repository or its organization and always has admin
//     access, so no lookup is needed.
//   - MEMBER (of the owning organization) and COLLABORATOR (invited to the
//     repository) can both be limited to read or triage access, so GitHub is
//     asked for their actual permission.
//   - Any other association (CONTRIBUTOR, FIRST_TIME_CONTRIBUTOR, NONE, ...)
//     has no push access and is refused without a lookup.
//
// A failed lookup is returned as an error and grants nothing; nothing is
// cached, so a revoked permission applies to the next event.
func (s *GitHubService) authorCanWrite(ctx context.Context, inst *domain.GitHubInstallation, repo *domain.GitHubRepoModel, association, login string) (bool, error) {
	switch association {
	case "OWNER":
		return true, nil
	case "MEMBER", "COLLABORATOR":
	default:
		return false, nil
	}
	if login == "" {
		return false, nil
	}
	lookup := s.repositoryPermission
	if lookup == nil {
		lookup = s.githubRepositoryPermission
	}
	permission, err := lookup(ctx, inst, repo, login)
	if err != nil {
		return false, fmt.Errorf("checking GitHub repository permission: %w", err)
	}
	switch permission {
	case "admin", "maintain", "write":
		return true, nil
	}
	return false, nil
}

// githubRepositoryPermission asks GitHub with the installation token; the
// repository name is the one Kuayle linked, never the webhook's.
func (s *GitHubService) githubRepositoryPermission(ctx context.Context, inst *domain.GitHubInstallation, repo *domain.GitHubRepoModel, login string) (string, error) {
	owner, name, ok := strings.Cut(repo.FullName, "/")
	if !ok || owner == "" || name == "" {
		return "", fmt.Errorf("invalid linked repository name")
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	client, _, err := s.getClient(ctx, inst.WorkspaceID)
	if err != nil {
		return "", fmt.Errorf("configuring GitHub client")
	}
	token, _, err := client.GetInstallationTokenContext(ctx, inst.InstallationID)
	if err != nil {
		return "", fmt.Errorf("authenticating GitHub client")
	}
	return client.GetCollaboratorPermission(ctx, token, owner, name, login)
}
