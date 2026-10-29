package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestRepositoryDefaultsReportTheInheritedTeamRepository(t *testing.T) {
	ws, team, project, issueID, app, other, secret := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	develop := "develop"
	store := &devMachineStoreFake{
		policy: testPolicy(ws),
		reposByID: map[uuid.UUID]*domain.GitHubRepoModel{
			app:    {ID: app, WorkspaceID: ws, FullName: "acme/app", DefaultBranch: "main", IsActive: true},
			other:  {ID: other, WorkspaceID: ws, FullName: "acme/other", DefaultBranch: "trunk", IsActive: true},
			secret: {ID: secret, WorkspaceID: ws, FullName: "acme/secret", DefaultBranch: "main", IsActive: true},
		},
		issues:   map[uuid.UUID]*domain.Issue{issueID: {ID: issueID, WorkspaceID: ws, TeamID: team, ProjectID: &project}},
		projects: map[uuid.UUID]*domain.Project{project: {ID: project, WorkspaceID: ws, TeamID: &team}},
		scopeSettings: map[string]*domain.DevMachineScopeSetting{
			scopeKey(&team, nil, nil): {WorkspaceID: ws, TeamID: &team, GitHubRepoID: &app, BaseBranch: &develop},
		},
	}
	store.policy.AllowedRepositories = json.RawMessage(`["acme/app","ACME/other"]`)
	svc := newTestDevMachineService(store)
	rawIssue, rawProject := issueID.String(), project.String()

	defaults, err := svc.RepositoryDefaults(context.Background(), ws, &rawIssue, nil)
	require.NoError(t, err)
	team1 := &DevelopmentRepository{ID: app, FullName: "acme/app", BaseBranch: "develop", Source: "team", Allowed: true}
	require.Equal(t, team1, defaults.Repository, "an issue without its own repository uses its team's")
	require.Equal(t, team1, defaults.Inherited)
	require.Equal(t, []DevelopmentRepositoryOption{{ID: app, FullName: "acme/app", DefaultBranch: "main"}, {ID: other, FullName: "acme/other", DefaultBranch: "trunk"}}, defaults.Repositories, "only repositories the policy allows are offered")

	defaults, err = svc.RepositoryDefaults(context.Background(), ws, nil, &rawProject)
	require.NoError(t, err)
	require.Equal(t, team1, defaults.Repository, "a project inherits its team's repository")

	store.scopeSettings[scopeKey(nil, nil, &issueID)] = &domain.DevMachineScopeSetting{WorkspaceID: ws, IssueID: &issueID, GitHubRepoID: &other}
	defaults, err = svc.RepositoryDefaults(context.Background(), ws, &rawIssue, nil)
	require.NoError(t, err)
	require.Equal(t, &DevelopmentRepository{ID: other, FullName: "acme/other", BaseBranch: "trunk", Source: "issue", Allowed: true}, defaults.Repository, "an explicit issue repository wins")
	require.Equal(t, team1, defaults.Inherited, "the inherited value skips the issue's own setting")

	store.scopeSettings[scopeKey(nil, nil, &issueID)].GitHubRepoID = &secret
	defaults, err = svc.RepositoryDefaults(context.Background(), ws, &rawIssue, nil)
	require.NoError(t, err)
	require.Equal(t, &DevelopmentRepository{ID: secret, FullName: "acme/secret", BaseBranch: "main", Source: "issue"}, defaults.Repository, "a default the policy refuses is reported as not allowed, not silently replaced")

	defaults, err = svc.RepositoryDefaults(context.Background(), ws, nil, nil)
	require.NoError(t, err)
	require.Nil(t, defaults.Repository, "without an issue or project only the workspace default applies")

	missing := uuid.NewString()
	_, err = svc.RepositoryDefaults(context.Background(), ws, &missing, nil)
	require.ErrorIs(t, err, ErrInvalidMachineInput, "issues outside the caller's visible workspace scope are refused")

	store.policy.Enabled = false
	_, err = svc.RepositoryDefaults(context.Background(), ws, &rawIssue, nil)
	require.ErrorIs(t, err, ErrDevMachinesDisabled)
}
