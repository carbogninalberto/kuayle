package service

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// developmentScope is one level of the issue → project → team → workspace
// development setting chain; kind names it for callers ("issue", ...).
type developmentScope struct {
	kind                 string
	team, project, issue *uuid.UUID
}

// developmentScopes lists the scopes consulted for an issue (or, without one,
// a project), most specific first. The workspace scope always comes last.
func developmentScopes(issue *domain.Issue, project *domain.Project) []developmentScope {
	var scopes []developmentScope
	if issue != nil {
		scopes = append(scopes, developmentScope{kind: "issue", issue: &issue.ID})
		if issue.ProjectID != nil {
			scopes = append(scopes, developmentScope{kind: "project", project: issue.ProjectID})
		}
		teamID := issue.TeamID
		scopes = append(scopes, developmentScope{kind: "team", team: &teamID})
	} else if project != nil {
		scopes = append(scopes, developmentScope{kind: "project", project: &project.ID})
		if project.TeamID != nil {
			scopes = append(scopes, developmentScope{kind: "team", team: project.TeamID})
		}
	}
	return append(scopes, developmentScope{kind: "workspace"})
}

type scopeSettingReader interface {
	GetScopeSetting(ctx context.Context, workspaceID uuid.UUID, teamID, projectID, issueID *uuid.UUID) (*domain.DevMachineScopeSetting, error)
}

// resolveDevelopmentScopes merges the settings of scopes field by field: each
// field takes the most specific configured value. It also returns the kind of
// scope that supplied the repository ("" when none did).
func resolveDevelopmentScopes(ctx context.Context, store scopeSettingReader, workspaceID uuid.UUID, scopes []developmentScope) (*domain.DevMachineScopeSetting, string, error) {
	resolved := &domain.DevMachineScopeSetting{WorkspaceID: workspaceID}
	repositorySource := ""
	for _, scope := range scopes {
		if resolved.GitHubRepoID != nil && resolved.EnvironmentID != nil && resolved.TestCommand != nil && resolved.AgentProvider != nil && resolved.AgentInstructions != nil && resolved.ComposeFile != nil && resolved.PreviewCommand != nil {
			break
		}
		setting, err := store.GetScopeSetting(ctx, workspaceID, scope.team, scope.project, scope.issue)
		if err != nil {
			return nil, "", err
		}
		if setting == nil {
			continue
		}
		if resolved.GitHubRepoID == nil && setting.GitHubRepoID != nil {
			resolved.GitHubRepoID, resolved.BaseBranch = setting.GitHubRepoID, setting.BaseBranch
			repositorySource = scope.kind
		}
		if resolved.EnvironmentID == nil && setting.EnvironmentID != nil {
			resolved.EnvironmentID = setting.EnvironmentID
		}
		if resolved.TestCommand == nil && setting.TestCommand != nil {
			command := append(json.RawMessage(nil), (*setting.TestCommand)...)
			resolved.TestCommand = &command
		}
		if resolved.AgentProvider == nil && setting.AgentProvider != nil {
			resolved.AgentProvider = setting.AgentProvider
		}
		if resolved.AgentInstructions == nil && setting.AgentInstructions != nil {
			resolved.AgentInstructions = setting.AgentInstructions
		}
		if resolved.ComposeFile == nil && setting.ComposeFile != nil {
			resolved.ComposeFile = setting.ComposeFile
		}
		if resolved.PreviewCommand == nil && setting.PreviewCommand != nil {
			resolved.PreviewCommand, resolved.PreviewPort = setting.PreviewCommand, setting.PreviewPort
		}
	}
	return resolved, repositorySource, nil
}

// DevelopmentRepository is the repository a coding agent or Dev Machine uses
// when none is chosen explicitly. Source is the scope that configured it;
// Allowed is false when the workspace policy refuses it, in which case work
// that relies on it is refused instead of falling back to another repository.
type DevelopmentRepository struct {
	ID         uuid.UUID `json:"id"`
	FullName   string    `json:"full_name"`
	BaseBranch string    `json:"base_branch"`
	Source     string    `json:"source"`
	Allowed    bool      `json:"allowed"`
}

// DevelopmentRepositoryOption is a linked repository the policy allows.
type DevelopmentRepositoryOption struct {
	ID            uuid.UUID `json:"id"`
	FullName      string    `json:"full_name"`
	DefaultBranch string    `json:"default_branch"`
}

// RepositoryDefaults is the effective repository of an issue or project, the
// value it would inherit without its own setting, and the repositories that
// may be chosen explicitly instead.
type RepositoryDefaults struct {
	Repository   *DevelopmentRepository        `json:"repository"`
	Inherited    *DevelopmentRepository        `json:"inherited_repository"`
	Repositories []DevelopmentRepositoryOption `json:"repositories"`
}

type linkedRepositoryReader interface {
	GetLinkedRepository(ctx context.Context, workspaceID, repositoryID uuid.UUID) (*domain.GitHubRepoModel, error)
}

// developmentRepository describes a resolved setting's repository. A
// repository that is no longer linked (or inactive) is reported as absent.
func developmentRepository(ctx context.Context, store linkedRepositoryReader, policy *domain.DevMachineWorkspacePolicy, workspaceID uuid.UUID, setting *domain.DevMachineScopeSetting, source string) (*DevelopmentRepository, error) {
	if setting == nil || setting.GitHubRepoID == nil {
		return nil, nil
	}
	linked, err := store.GetLinkedRepository(ctx, workspaceID, *setting.GitHubRepoID)
	if err != nil || linked == nil {
		return nil, err
	}
	branch := linked.DefaultBranch
	if setting.BaseBranch != nil {
		branch = *setting.BaseBranch
	}
	allowed := policy != nil && repositoryAllowed(policy.AllowedRepositories, linked.FullName)
	return &DevelopmentRepository{ID: linked.ID, FullName: linked.FullName, BaseBranch: branch, Source: source, Allowed: allowed}, nil
}

// RepositoryDefaults resolves the development repository for an issue or a
// project (workspace default without either). The issue and project are read
// with the same visibility as machine creation, so private-team and other
// workspaces' resources are refused.
func (s *DevMachineService) RepositoryDefaults(ctx context.Context, workspaceID uuid.UUID, rawIssueID, rawProjectID *string) (*RepositoryDefaults, error) {
	policy, err := s.enabledPolicy(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	issue, project, _, _, err := s.resolveRequestedContext(ctx, workspaceID, rawIssueID, rawProjectID)
	if err != nil {
		return nil, err
	}
	if issue != nil {
		// A project_id beside an issue only confirms the issue's own project.
		project = nil
	}
	scopes := developmentScopes(issue, project)
	result := &RepositoryDefaults{Repositories: []DevelopmentRepositoryOption{}}
	setting, source, err := resolveDevelopmentScopes(ctx, s.store, workspaceID, scopes)
	if err != nil {
		return nil, err
	}
	if result.Repository, err = developmentRepository(ctx, s.store, policy, workspaceID, setting, source); err != nil {
		return nil, err
	}
	if len(scopes) > 1 {
		setting, source, err = resolveDevelopmentScopes(ctx, s.store, workspaceID, scopes[1:])
		if err != nil {
			return nil, err
		}
		if result.Inherited, err = developmentRepository(ctx, s.store, policy, workspaceID, setting, source); err != nil {
			return nil, err
		}
	}
	linked, err := s.store.ListLinkedRepositories(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	for _, repository := range linked {
		if repositoryAllowed(policy.AllowedRepositories, repository.FullName) {
			result.Repositories = append(result.Repositories, DevelopmentRepositoryOption{ID: repository.ID, FullName: repository.FullName, DefaultBranch: repository.DefaultBranch})
		}
	}
	sort.SliceStable(result.Repositories, func(i, j int) bool { return result.Repositories[i].FullName < result.Repositories[j].FullName })
	return result, nil
}

// issueDevelopmentRepository is the repository a coding task on issue would
// use, for the assistant: an error says what to do when there is none.
func issueDevelopmentRepository(ctx context.Context, store assistantRepositoryStore, workspaceID uuid.UUID, issue *domain.Issue) (*DevelopmentRepository, error) {
	setting, source, err := resolveDevelopmentScopes(ctx, store, workspaceID, developmentScopes(issue, nil))
	if err != nil {
		return nil, err
	}
	policy, err := store.GetPolicy(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	return developmentRepository(ctx, store, policy, workspaceID, setting, source)
}

func missingDevelopmentRepository(identifier string) error {
	return toolFailure("no development repository is configured for %s in its issue, project, team or workspace development settings; ask the user which repository to use and link it with link_repository", identifier)
}

func refusedDevelopmentRepository(identifier, repository string) error {
	return toolFailure("the development repository %q of %s is not allowed by the workspace's Dev Machine policy", repository, identifier)
}
