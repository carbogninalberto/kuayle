package repository

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// migrationActionFixture is a workspace with an owner, the machine's creator
// (a member) and another member, plus an admin of another workspace whose
// own machine has an action too.
type migrationActionFixture struct {
	db                                 *sqlx.DB
	repo                               *AssistantCommandRepository
	ctx                                context.Context
	ws, otherWS                        uuid.UUID
	owner, creator, member, otherAdmin uuid.UUID
	machine, otherMachine              uuid.UUID
}

func newMigrationActionFixture(t *testing.T) migrationActionFixture {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db := isolatedPrivacyDatabase(t, url)
	f := migrationActionFixture{db: db, repo: NewAssistantCommandRepository(db), ctx: context.Background(),
		ws: uuid.New(), otherWS: uuid.New(), owner: uuid.New(), creator: uuid.New(), member: uuid.New(), otherAdmin: uuid.New(),
		machine: uuid.New(), otherMachine: uuid.New()}
	exec := func(query string, args ...any) {
		t.Helper()
		_, err := db.Exec(query, args...)
		require.NoError(t, err)
	}
	for _, user := range []uuid.UUID{f.owner, f.creator, f.member, f.otherAdmin} {
		exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'User','test')`, user, user.String()+"@example.test")
	}
	exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Main',$2,$3),($4,'Other',$5,$6)`,
		f.ws, "main-"+f.ws.String()[:8], f.owner, f.otherWS, "other-"+f.otherWS.String()[:8], f.otherAdmin)
	exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'owner'),($1,$3,'member'),($1,$4,'member'),($5,$6,'admin')`,
		f.ws, f.owner, f.creator, f.member, f.otherWS, f.otherAdmin)
	for _, m := range []struct{ id, ws, by uuid.UUID }{{f.machine, f.ws, f.creator}, {f.otherMachine, f.otherWS, f.otherAdmin}} {
		exec(`INSERT INTO dev_machines(id,workspace_id,created_by_user_id,routing_key,name,status,desired_status,generation,repo_url,repo_provider,repo_owner,repo_name,base_branch,working_branch,machine_size,cpu_millis,memory_mb,disk_gb)
 VALUES($1,$2,$3,$4,'Migrations','running','running',1,'','github','','','','','small',1000,2048,20)`, m.id, m.ws, m.by, strings.ReplaceAll(m.id.String(), "-", "")[:16])
	}
	return f
}

func (f migrationActionFixture) action(command, sealed string) *domain.MigrationAction {
	return &domain.MigrationAction{Name: "migrate-db", Description: "Run the database migrations", Command: command, EncryptedSecret: sealed}
}

func (f migrationActionFixture) storedSecret(t *testing.T, id uuid.UUID) string {
	t.Helper()
	var sealed string
	require.NoError(t, f.db.Get(&sealed, `SELECT encrypted_secret FROM dev_machine_migration_actions WHERE id=$1`, id))
	return sealed
}

// KUA-181: editing an action without a new secret keeps the stored one; a
// new secret replaces it. Both run against the real repository.
func TestMigrationActionUpdateWithoutASecretKeepsTheStoredOne(t *testing.T) {
	f := newMigrationActionFixture(t)
	created, err := f.repo.CreateMachineMigrationAction(f.ctx, f.ws, f.machine, f.creator, f.action(`migrate -url "$KUAYLE_MIGRATION_SECRET" up`, "sealed1"))
	require.NoError(t, err)

	updated, err := f.repo.UpdateMachineMigrationAction(f.ctx, f.ws, f.machine, created.ID, f.owner, f.action(`migrate -url "$KUAYLE_MIGRATION_SECRET" up 1`, ""), false)
	require.NoError(t, err, "an omitted secret keeps the stored one")
	require.Equal(t, `migrate -url "$KUAYLE_MIGRATION_SECRET" up 1`, updated.Command)
	require.Equal(t, "sealed1", f.storedSecret(t, created.ID))

	_, err = f.repo.UpdateMachineMigrationAction(f.ctx, f.ws, f.machine, created.ID, f.creator, f.action(`migrate -url "$KUAYLE_MIGRATION_SECRET" up`, "sealed2"), true)
	require.NoError(t, err)
	require.Equal(t, "sealed2", f.storedSecret(t, created.ID))

	_, err = f.repo.UpdateMachineMigrationAction(f.ctx, f.ws, f.machine, created.ID, f.creator, f.action(`migrate up`, ""), true)
	require.Error(t, err, "a new secret cannot be empty")
	_, err = f.repo.CreateMachineMigrationAction(f.ctx, f.ws, f.machine, f.creator, &domain.MigrationAction{Name: "other", Description: "Other", Command: "migrate up"})
	require.Error(t, err, "a new action needs a secret")
}

// Reading a machine's actions is for its creator and the workspace's owners
// and admins, as configuring them is; anyone else is refused, and nobody sees
// another workspace's or machine's actions.
func TestMigrationActionsAreReadByTheCreatorOwnersAndAdminsOnly(t *testing.T) {
	f := newMigrationActionFixture(t)
	own, err := f.repo.CreateMachineMigrationAction(f.ctx, f.ws, f.machine, f.creator, f.action(`migrate -url "$KUAYLE_MIGRATION_SECRET" up`, "sealed1"))
	require.NoError(t, err)
	other, err := f.repo.CreateMachineMigrationAction(f.ctx, f.otherWS, f.otherMachine, f.otherAdmin, f.action(`migrate -url "$KUAYLE_MIGRATION_SECRET" up`, "sealed2"))
	require.NoError(t, err)

	for _, user := range []uuid.UUID{f.creator, f.owner} {
		listed, err := f.repo.ListMachineMigrationActions(f.ctx, f.ws, f.machine, user)
		require.NoError(t, err)
		require.Len(t, listed, 1)
		require.Equal(t, own.ID, listed[0].ID)
	}
	_, err = f.repo.ListMachineMigrationActions(f.ctx, f.ws, f.machine, f.member)
	require.ErrorIs(t, err, ErrMigrationActionForbidden, "a member who did not create the machine reads nothing")

	listed, err := f.repo.ListMachineMigrationActions(f.ctx, f.otherWS, f.otherMachine, f.otherAdmin)
	require.NoError(t, err)
	require.Len(t, listed, 1, "an admin sees only their own machine's actions")
	require.Equal(t, other.ID, listed[0].ID)
	_, err = f.repo.ListMachineMigrationActions(f.ctx, f.otherWS, f.machine, f.otherAdmin)
	require.ErrorIs(t, err, ErrMigrationActionNotFound, "a machine of another workspace is not found")
}
