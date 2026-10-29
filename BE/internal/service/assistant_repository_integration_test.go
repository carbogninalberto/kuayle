package service

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

// A repository configured in the team's development settings is the one a
// delegated coding agent uses, so the assistant must not need it spelled out.
func TestAssistantDelegationUsesTheInheritedDevelopmentRepository(t *testing.T) {
	f := newToolFixture(t)
	tools := f.service.tools
	tools.SetRepositories(repository.NewDevMachineRepository(f.db))
	var team uuid.UUID
	require.NoError(t, f.db.Get(&team, `SELECT id FROM teams WHERE workspace_id=$1`, f.ws))
	installation, app, other, secret := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	var issue uuid.UUID
	require.NoError(t, f.db.Get(&issue, `INSERT INTO issues(workspace_id,team_id,number,identifier_text,title,creator_id,status_id) VALUES($1,$2,1,'ENG-1','Dark mode',$3,(SELECT id FROM team_statuses WHERE team_id=$2 LIMIT 1)) RETURNING id`, f.ws, team, f.user))
	_, err := f.db.Exec(`INSERT INTO github_installations(id,workspace_id,installation_id,account_login,account_type,installed_by) VALUES($1,$2,7,'acme','Organization',$3)`, installation, f.ws, f.user)
	require.NoError(t, err)
	_, err = f.db.Exec(`INSERT INTO github_repos(id,installation_id,workspace_id,github_repo_id,full_name,default_branch) VALUES($1,$4,$5,1,'acme/app','main'),($2,$4,$5,2,'acme/other','trunk'),($3,$4,$5,3,'acme/secret','main')`, app, other, secret, installation, f.ws)
	require.NoError(t, err)
	_, err = f.db.Exec(`INSERT INTO dev_machine_workspace_policies(workspace_id,enabled,allowed_repositories) VALUES($1,TRUE,'["acme/app","acme/other"]')`, f.ws)
	require.NoError(t, err)

	delegate := func() (domain.DelegationPayload, error) {
		_, payload, err := tools.prepareDelegation(f.ctx, f.ws, f.user, `{"identifier":"ENG-1","instructions":"Implement dark mode"}`)
		return payload, err
	}
	developmentRepository := func() any {
		output, _, err := tools.run(f.ctx, f.ws, f.user, "get_issue", `{"identifier":"ENG-1"}`)
		require.NoError(t, err)
		return output.(map[string]any)["development_repository"]
	}

	_, err = delegate()
	require.EqualError(t, err, "no development repository is configured for ENG-1 in its issue, project, team or workspace development settings; ask the user which repository to use and link it with link_repository", "the assistant asks only when nothing is configured")
	require.Nil(t, developmentRepository())

	_, err = f.db.Exec(`INSERT INTO dev_machine_scope_settings(workspace_id,team_id,github_repo_id,base_branch) VALUES($1,$2,$3,'develop')`, f.ws, team, app)
	require.NoError(t, err)
	payload, err := delegate()
	require.NoError(t, err)
	require.Equal(t, "acme/app", payload.Repository)
	require.Equal(t, "develop", payload.BaseBranch)
	require.Equal(t, "team", payload.RepositorySource)
	require.Equal(t, map[string]any{"repository": "acme/app", "base_branch": "develop", "source": "team"}, developmentRepository())
	encoded, err := json.Marshal(payload)
	require.NoError(t, err)
	require.JSONEq(t, `{"identifier":"ENG-1","title":"Dark mode","instructions":"Implement dark mode","repository":"acme/app","base_branch":"develop","repository_source":"team"}`, string(encoded), "the approval shows the repository the agent will use")

	// An explicit issue repository still wins over the team's.
	_, err = f.db.Exec(`INSERT INTO dev_machine_scope_settings(workspace_id,issue_id,github_repo_id) VALUES($1,$2,$3)`, f.ws, issue, other)
	require.NoError(t, err)
	payload, err = delegate()
	require.NoError(t, err)
	require.Equal(t, "acme/other", payload.Repository)
	require.Equal(t, "trunk", payload.BaseBranch, "without a base branch the repository's default branch is used")
	require.Equal(t, "issue", payload.RepositorySource)

	// The workspace policy still decides which repositories may be used.
	_, err = f.db.Exec(`UPDATE dev_machine_scope_settings SET github_repo_id=$2 WHERE issue_id=$1`, issue, secret)
	require.NoError(t, err)
	_, err = delegate()
	require.EqualError(t, err, `the development repository "acme/secret" of ENG-1 is not allowed by the workspace's Dev Machine policy`)
}
