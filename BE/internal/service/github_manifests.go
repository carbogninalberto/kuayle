package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	gh "github.com/kuayle/kuayle-backend/pkg/github"
)

// Bounds on what a check suggestion reads from GitHub.
const (
	maxManifestFolders = 20
	maxManifestFiles   = 16
	maxManifestBytes   = 256 << 10
)

// repositoryReader returns a client and a token scoped to one repository.
func (s *GitHubService) repositoryReader(ctx context.Context, workspaceID uuid.UUID, fullName string) (*gh.Client, string, string, string, error) {
	owner, name, ok := strings.Cut(fullName, "/")
	if !ok {
		return nil, "", "", "", fmt.Errorf("invalid repository %q", fullName)
	}
	inst, err := s.ghRepo.GetInstallationByWorkspace(ctx, workspaceID)
	if err != nil || inst == nil {
		return nil, "", "", "", fmt.Errorf("GitHub not connected")
	}
	client, _, err := s.getClient(ctx, workspaceID)
	if err != nil {
		return nil, "", "", "", err
	}
	token, _, err := client.GetRepositoryInstallationToken(inst.InstallationID, name)
	if err != nil {
		return nil, "", "", "", err
	}
	return client, token, owner, name, nil
}

// RepositoryPaths lists the files at the root of ref and in its first-level
// folders, skipping folders a check suggestion ignores.
func (s *GitHubService) RepositoryPaths(ctx context.Context, workspaceID uuid.UUID, fullName, ref string) ([]string, error) {
	client, token, owner, name, err := s.repositoryReader(ctx, workspaceID, fullName)
	if err != nil {
		return nil, err
	}
	root, err := client.ListTree(token, owner, name, ref)
	if err != nil {
		return nil, err
	}
	var paths []string
	folders := 0
	for _, entry := range root {
		switch {
		case entry.Type == "blob":
			paths = append(paths, entry.Path)
		case entry.Type == "tree" && folders < maxManifestFolders && !ignoredCheckFolders[entry.Path] && !strings.HasPrefix(entry.Path, ".") && safeCheckFolder.MatchString(entry.Path):
			folders++
			children, err := client.ListTree(token, owner, name, entry.SHA)
			if err != nil {
				return nil, err
			}
			for _, child := range children {
				if child.Type == "blob" {
					paths = append(paths, entry.Path+"/"+child.Path)
				}
			}
		}
	}
	return paths, nil
}

// RepositoryFiles reads up to maxManifestFiles files at ref.
func (s *GitHubService) RepositoryFiles(ctx context.Context, workspaceID uuid.UUID, fullName, ref string, paths []string) (map[string][]byte, error) {
	files := map[string][]byte{}
	if len(paths) == 0 {
		return files, nil
	}
	client, token, owner, name, err := s.repositoryReader(ctx, workspaceID, fullName)
	if err != nil {
		return nil, err
	}
	for i, path := range paths {
		if i == maxManifestFiles {
			break
		}
		content, err := client.GetFileContent(token, owner, name, path, ref, maxManifestBytes)
		if err != nil {
			return nil, err
		}
		files[path] = content
	}
	return files, nil
}
