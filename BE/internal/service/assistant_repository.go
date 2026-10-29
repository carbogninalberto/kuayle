package service

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
)

// assistantRepositoryStore is the part of the Dev Machine store that the
// development settings use for repositories.
type assistantRepositoryStore interface {
	GetScopeSetting(ctx context.Context, workspaceID uuid.UUID, teamID, projectID, issueID *uuid.UUID) (*domain.DevMachineScopeSetting, error)
	UpsertScopeSetting(ctx context.Context, setting *domain.DevMachineScopeSetting) error
	GetLinkedRepositoryByFullName(ctx context.Context, workspaceID uuid.UUID, fullName string) (*domain.GitHubRepoModel, error)
	GetLinkedRepository(ctx context.Context, workspaceID, repositoryID uuid.UUID) (*domain.GitHubRepoModel, error)
	GetPolicy(ctx context.Context, workspaceID uuid.UUID) (*domain.DevMachineWorkspacePolicy, error)
}

var _ assistantRepositoryStore = (*repository.DevMachineRepository)(nil)

// SetRepositories enables link_repository and lets get_issue and
// delegate_issue report the issue's inherited development repository.
func (t *AssistantTools) SetRepositories(store assistantRepositoryStore) {
	t.repositories = store
}

// linkRepository sets the development repository of an issue or its project,
// with the rules of PUT /dev-machine-scope-setting: dev_machine:manage, and
// workspace administration for any scope wider than one issue. It also applies
// the Dev Machine policy's repository allowlist up front. Other development
// settings of the scope are kept.
func (t *AssistantTools) linkRepository(ctx context.Context, ws uuid.UUID, role, arguments string) (any, *uuid.UUID, error) {
	var input struct {
		Identifier string  `json:"identifier"`
		Repository string  `json:"repository"`
		BaseBranch *string `json:"base_branch"`
		Scope      string  `json:"scope"`
	}
	if err := decodeToolInput(arguments, &input); err != nil {
		return nil, nil, err
	}
	if input.Scope == "" {
		input.Scope = "issue"
	}
	if input.Scope != "issue" && input.Scope != "project" {
		return nil, nil, toolFailure("invalid arguments")
	}
	if !domain.HasPermission(role, domain.PermDevMachineManage) {
		return nil, nil, toolFailure("your role cannot change development settings")
	}
	issue, err := t.issues.GetByIdentifier(ctx, ws, strings.ToUpper(strings.TrimSpace(input.Identifier)))
	if err != nil || issue == nil {
		return nil, nil, toolFailure("issue not found")
	}
	setting := &domain.DevMachineScopeSetting{WorkspaceID: ws, IssueID: &issue.ID}
	if input.Scope == "project" {
		if role != domain.RoleOwner && role != domain.RoleAdmin {
			return nil, nil, toolFailure("only workspace owners and admins can set a project's repository")
		}
		if issue.ProjectID == nil {
			return nil, nil, toolFailure("this issue has no project")
		}
		setting = &domain.DevMachineScopeSetting{WorkspaceID: ws, ProjectID: issue.ProjectID}
	}
	name := strings.TrimSpace(input.Repository)
	linked, err := t.repositories.GetLinkedRepositoryByFullName(ctx, ws, name)
	if err != nil {
		return nil, nil, err
	}
	if linked == nil {
		return nil, nil, toolFailure("repository %q is not linked to this workspace's GitHub installation", truncateRunes(name, 200))
	}
	policy, err := t.repositories.GetPolicy(ctx, ws)
	if err != nil {
		return nil, nil, err
	}
	if policy != nil && !repositoryAllowed(policy.AllowedRepositories, linked.FullName) {
		return nil, nil, toolFailure("repository %q is not allowed by the workspace's Dev Machine policy", linked.FullName)
	}
	branch := linked.DefaultBranch
	if input.BaseBranch != nil {
		branch = strings.TrimSpace(*input.BaseBranch)
	}
	if !validGitRef(branch) {
		return nil, nil, toolFailure("invalid base branch")
	}
	existing, err := t.repositories.GetScopeSetting(ctx, ws, setting.TeamID, setting.ProjectID, setting.IssueID)
	if err != nil {
		return nil, nil, err
	}
	if existing != nil {
		setting = existing
	}
	setting.GitHubRepoID, setting.BaseBranch = &linked.ID, &branch
	if err = t.repositories.UpsertScopeSetting(ctx, setting); err != nil {
		return nil, nil, toolFailure("the repository could not be linked")
	}
	return map[string]any{"identifier": issue.Identifier, "scope": input.Scope, "repository": linked.FullName, "base_branch": branch}, &issue.ID, nil
}
