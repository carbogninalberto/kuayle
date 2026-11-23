package migrations_test

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestWorkspaceRolesMigration(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	admin, err := sql.Open("pgx", url)
	require.NoError(t, err)
	defer admin.Close()
	name := "roles_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = admin.Exec("CREATE DATABASE " + name)
	require.NoError(t, err)
	defer admin.ExecContext(context.Background(), "DROP DATABASE "+name+" WITH (FORCE)")
	target := databaseURLWithName(t, url, name)
	m, err := migrate.New("file://.", target)
	require.NoError(t, err)
	defer m.Close()
	require.NoError(t, m.Migrate(201))
	db, err := sql.Open("pgx", target)
	require.NoError(t, err)
	defer db.Close()
	fixture := insertBaseMigrationFixture(t, db)
	users := map[string]uuid.UUID{domain.RoleOwner: fixture.userA}
	for _, role := range []string{domain.RoleAdmin, domain.RoleMember, domain.RoleGuest} {
		user := uuid.New()
		_, err = db.Exec("INSERT INTO users(id,email,name,password_hash) VALUES ($1,$2,$3,'test')", user, role+"@example.test", role)
		require.NoError(t, err)
		_, err = db.Exec("INSERT INTO workspace_members(workspace_id,user_id,role) VALUES ($1,$2,$3)", fixture.workspaceA, user, role)
		require.NoError(t, err)
		users[role] = user
	}
	// Older installations allowed several owners before owner_id was introduced.
	_, err = db.Exec("UPDATE workspace_members SET role='owner' WHERE workspace_id=$1 AND user_id=$2", fixture.workspaceA, users[domain.RoleAdmin])
	require.NoError(t, err)
	require.ErrorContains(t, m.Migrate(202), "Multiple owner memberships in workspaces: migration-miga")
	var owners int
	require.NoError(t, db.QueryRow("SELECT count(*) FROM workspace_members WHERE workspace_id=$1 AND role='owner'", fixture.workspaceA).Scan(&owners))
	require.Equal(t, 2, owners)
	_, err = db.Exec("UPDATE workspace_members SET role='admin' WHERE workspace_id=$1 AND user_id=$2", fixture.workspaceA, users[domain.RoleAdmin])
	require.NoError(t, err)
	require.NoError(t, m.Force(201))
	require.NoError(t, m.Migrate(202))
	for role, user := range users {
		var permissions pq.StringArray
		err = db.QueryRow(`SELECT r.permissions FROM workspace_members m JOIN workspace_roles r ON (r.workspace_id,r.key)=(m.workspace_id,m.role_key) WHERE m.workspace_id=$1 AND m.user_id=$2`, fixture.workspaceA, user).Scan(&permissions)
		require.NoError(t, err)
		require.ElementsMatch(t, domain.RolePermissions[role], permissions, role)
	}
	custom := uuid.NewString()
	_, err = db.Exec(`INSERT INTO workspace_roles(workspace_id,key,name,permissions) VALUES($1,$2,'Triage','{issues:read}')`, fixture.workspaceA, custom)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE workspace_members SET role='guest',assigned_role=$3 WHERE workspace_id=$1 AND user_id=$2`, fixture.workspaceA, users[domain.RoleMember], custom)
	require.NoError(t, err)
	_, err = db.Exec(`DELETE FROM workspace_roles WHERE workspace_id=$1 AND key=$2`, fixture.workspaceA, custom)
	require.Equal(t, "23503", postgresCode(err))
	gatewayAllowed := func() bool {
		t.Helper()
		var grant bool
		require.NoError(t, db.QueryRow(`SELECT EXISTS(SELECT 1 FROM dev_machine_role_access WHERE workspace_id=$1 AND user_id=$2)`, fixture.workspaceA, users[domain.RoleMember]).Scan(&grant))
		return grant
	}
	require.False(t, gatewayAllowed())
	_, err = db.Exec(`UPDATE workspace_roles SET permissions='{dev_machine:read,dev_machine:manage}' WHERE workspace_id=$1 AND key=$2`, fixture.workspaceA, custom)
	require.NoError(t, err)
	require.True(t, gatewayAllowed())
	down, err := os.ReadFile("000202_workspace_roles.down.sql")
	require.NoError(t, err)
	tx, err := db.Begin()
	require.NoError(t, err)
	_, err = tx.Exec(string(down))
	require.ErrorContains(t, err, "Remove team overrides and custom roles")
	require.NoError(t, tx.Rollback())
	_, err = db.Exec(`INSERT INTO team_role_overrides(workspace_id,team_id,user_id,role_key) VALUES($1,$2,$3,'guest')`, fixture.workspaceA, fixture.teamB, users[domain.RoleMember])
	require.Equal(t, "23503", postgresCode(err))
	_, err = db.Exec(`UPDATE workspace_members SET role='owner' WHERE workspace_id=$1 AND user_id=$2`, fixture.workspaceA, users[domain.RoleGuest])
	require.Equal(t, "23505", postgresCode(err))
	_, err = db.Exec(`UPDATE workspace_members SET role='member',assigned_role=NULL WHERE workspace_id=$1 AND user_id=$2`, fixture.workspaceA, users[domain.RoleMember])
	require.NoError(t, err)
	_, err = db.Exec(`DELETE FROM workspace_roles WHERE workspace_id=$1 AND key=$2`, fixture.workspaceA, custom)
	require.NoError(t, err)
	require.NoError(t, m.Steps(-1))
}
