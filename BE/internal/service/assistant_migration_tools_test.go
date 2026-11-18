package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

// migrationCommandStore stands in for the command repository: the machine a
// migration runs on is the one the test sets, every other lookup defaults to
// "unavailable" as in commandStoreStub.
type migrationCommandStore struct {
	commandStoreStub
	machine *domain.DevMachine
}

func (f *migrationCommandStore) UsableMachine(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (*domain.DevMachine, error) {
	if f.machine != nil {
		return f.machine, nil
	}
	return nil, repository.ErrCommandMachineUnavailable
}

// migrationStoreStub stands in for the migration action repository.
type migrationStoreStub struct {
	actions []repository.UserMigrationAction
}

func (f *migrationStoreStub) MigrationActionsForUser(context.Context, uuid.UUID, uuid.UUID) ([]repository.UserMigrationAction, error) {
	return f.actions, nil
}

func (f *migrationStoreStub) MigrationAction(_ context.Context, _, _, machine uuid.UUID, name string) (*domain.MigrationAction, error) {
	for _, action := range f.actions {
		if action.MachineID == machine && action.Name == name {
			configured := action.MigrationAction
			return &configured, nil
		}
	}
	return nil, repository.ErrMigrationActionNotFound
}

func migrationToolsWith(store *migrationCommandStore, migrations *migrationStoreStub, user uuid.UUID) *AssistantTools {
	tools := &AssistantTools{members: prMembers{user: domain.RoleMember}}
	tools.SetCommands(store, nil)
	tools.SetMigrations(migrations)
	return tools
}

func userMigrationAction(name, machine, description, command string) repository.UserMigrationAction {
	return repository.UserMigrationAction{MigrationAction: domain.MigrationAction{MachineID: uuid.New(), Name: name, Description: description, Command: command},
		MachineName: machine, MachineStatus: string(domain.DevMachineStatusRunning)}
}

func runningMachineNamed(name string) *domain.DevMachine {
	return &domain.DevMachine{ID: uuid.New(), Name: name, Status: domain.DevMachineStatusRunning, CPUMillis: 4000, MemoryMB: 8192, DiskGB: 40}
}

// queueRunStore captures the run an approval queues.
type queueRunStore struct {
	commandStoreStub
	queued []repository.CommandRunRequest
}

func (f *queueRunStore) QueueRun(_ context.Context, _, _ uuid.UUID, req repository.CommandRunRequest) (*domain.CommandRun, error) {
	f.queued = append(f.queued, req)
	return &domain.CommandRun{}, nil
}

// With no action configured at all, the model is told how one is set up.
func TestPrepareMigrationRefusesWhenNoActionIsConfigured(t *testing.T) {
	user := uuid.New()
	tools := migrationToolsWith(&migrationCommandStore{}, &migrationStoreStub{}, user)

	_, err := tools.prepareMigration(context.Background(), commandAttempt(user), `{"action":"migrate-db"}`)
	require.EqualError(t, err, "No migration action is configured on your machines. Ask the user to have a workspace admin or the machine's creator configure one in the machine's settings; they choose the command and store the database connection secret, so it is requested only by name.")
}

// An unknown name is refused with every configured action, its machine and
// its description, so the model can retry with a valid one.
func TestPrepareMigrationRefusesUnknownAction(t *testing.T) {
	user := uuid.New()
	migrations := &migrationStoreStub{actions: []repository.UserMigrationAction{
		userMigrationAction("migrate-db", "laptop-box", "Runs the schema migrations.", "dbmate up"),
	}}
	tools := migrationToolsWith(&migrationCommandStore{}, migrations, user)

	_, err := tools.prepareMigration(context.Background(), commandAttempt(user), `{"action":"other"}`)
	require.ErrorContains(t, err, `No migration action named "other" is configured.`)
	require.ErrorContains(t, err, `migrate-db on machine "laptop-box" — Runs the schema migrations.`)
}

// The same name on several machines is ambiguous: the model must name the
// machine, and both are listed to choose from.
func TestPrepareMigrationAsksForMachineWhenAmbiguous(t *testing.T) {
	user := uuid.New()
	migrations := &migrationStoreStub{actions: []repository.UserMigrationAction{
		userMigrationAction("migrate-db", "laptop-box", "Runs the schema migrations.", "dbmate up"),
		userMigrationAction("migrate-db", "rack-box", "Runs the schema migrations.", "dbmate up"),
	}}
	tools := migrationToolsWith(&migrationCommandStore{}, migrations, user)

	_, err := tools.prepareMigration(context.Background(), commandAttempt(user), `{"action":"migrate-db"}`)
	require.ErrorContains(t, err, `The migration action "migrate-db" is configured on several machines; give machine_id of one:`)
	require.ErrorContains(t, err, `"laptop-box"`)
	require.ErrorContains(t, err, `"rack-box"`)
}

// One configured action on a running machine becomes the approval payload:
// the admin-configured command and the machine, never the connection secret.
func TestPrepareMigrationPayload(t *testing.T) {
	user := uuid.New()
	machine := runningMachineNamed("laptop-box")
	action := userMigrationAction("migrate-db", "laptop-box", "Runs the schema migrations.", "dbmate up")
	action.MachineID = machine.ID
	action.EncryptedSecret = "aGVsbG8=" // never leaves the repository
	migrations := &migrationStoreStub{actions: []repository.UserMigrationAction{action}}
	tools := migrationToolsWith(&migrationCommandStore{machine: machine}, migrations, user)

	payload, err := tools.prepareMigration(context.Background(), commandAttempt(user), `{"action":"migrate-db"}`)
	require.NoError(t, err)
	require.Equal(t, "migrate-db", payload.Action)
	require.Equal(t, "Runs the schema migrations.", payload.Description)
	require.Equal(t, "dbmate up", payload.Command)
	require.Equal(t, machine.ID, payload.MachineID)
	require.Equal(t, "laptop-box", payload.Machine)
	require.Equal(t, 4000, payload.CPUMillis)
	require.False(t, payload.Reused)
	require.False(t, payload.Temporary)
	require.Equal(t, 60, payload.TimeoutSeconds)
	data, err := json.Marshal(payload)
	require.NoError(t, err)
	require.NotContains(t, string(data), "secret", "the payload never carries the connection secret")
	require.NotContains(t, string(data), action.EncryptedSecret)

	// With machine_id the action is resolved on that machine alone.
	payload, err = tools.prepareMigration(context.Background(), commandAttempt(user), `{"action":"migrate-db","machine_id":"`+machine.ID.String()+`"}`)
	require.NoError(t, err)
	require.Equal(t, "dbmate up", payload.Command)
	require.Equal(t, machine.ID, payload.MachineID)
}

// run_migration shares run_command's role refusals: a guest, and a role
// without Dev Machine access, are refused with the next step.
func TestPrepareMigrationRefusesGuestAndLimitedRoles(t *testing.T) {
	user := uuid.New()
	tools := &AssistantTools{members: prMembers{user: domain.RoleGuest}}
	_, err := tools.prepareMigration(context.Background(), commandAttempt(user), `{"action":"migrate-db"}`)
	require.EqualError(t, err, guestCommandRefusal+".")
	tools.members = prMembers{user: "limited"}
	_, err = tools.prepareMigration(context.Background(), commandAttempt(user), `{"action":"migrate-db"}`)
	require.EqualError(t, err, roleCommandRefusal+".")
}

// An approved migration queues one run of the admin-configured command, in
// /workspace, read-only, bound to the approval.
func TestRunMigrationApprovalQueuesAMigrationRun(t *testing.T) {
	user, ws, conversation := uuid.New(), uuid.New(), uuid.New()
	machine := runningMachineNamed("laptop-box")
	payload := domain.MigrationRunPayload{CommandMachine: domain.CommandMachineOf(machine, false), Action: "migrate-db",
		Description: "Runs the schema migrations.", Command: "dbmate up", TimeoutSeconds: 60}
	data, err := json.Marshal(payload)
	require.NoError(t, err)
	approval := &domain.ConversationApproval{ID: uuid.New(), ToolCallID: uuid.New(), ActorID: user, Payload: data}
	store := &queueRunStore{}
	approvals := &AssistantApprovals{conversations: &repository.ConversationRepository{}, commands: store}

	// The unit test has no conversations store, so finishing the approval is
	// refused without access; the run itself was queued first.
	_, _ = approvals.runMigration(context.Background(), ws, conversation, approval)
	require.Len(t, store.queued, 1)
	queued := store.queued[0]
	require.Equal(t, machine.ID, queued.MachineID)
	require.Equal(t, "dbmate up", queued.Command)
	require.True(t, queued.Migration)
	require.Equal(t, "migrate-db", queued.ActionName)
	require.Equal(t, "/workspace", queued.WorkingDirectory)
	require.False(t, queued.Write)
	require.False(t, queued.Reused)
	require.NotNil(t, queued.ApprovalID)
	require.Equal(t, approval.ID, *queued.ApprovalID)
	require.Equal(t, conversation, *queued.ConversationID)
	require.Equal(t, approval.ToolCallID, *queued.ToolCallID)
}

// A malformed payload queues nothing.
func TestRunMigrationApprovalCannotExecuteAMalformedPayload(t *testing.T) {
	ws, conversation := uuid.New(), uuid.New()
	approval := &domain.ConversationApproval{ID: uuid.New(), ToolCallID: uuid.New(), ActorID: uuid.New(), Payload: json.RawMessage(`{"action":42}`)}
	store := &queueRunStore{}
	approvals := &AssistantApprovals{conversations: &repository.ConversationRepository{}, commands: store}

	_, _ = approvals.runMigration(context.Background(), ws, conversation, approval)
	require.Empty(t, store.queued, "a malformed payload queues no run")
}

// The migration guidance is in the system prompt only while run_migration is
// offered and an action is configured; at most eight are listed.
func TestMigrationPromptListsConfiguredActions(t *testing.T) {
	user := uuid.New()
	tools := &AssistantTools{}
	require.Empty(t, tools.migrationPrompt(context.Background(), uuid.New(), user), "not wired")
	tools.SetCommands(&migrationCommandStore{}, nil)
	tools.SetMigrations(&migrationStoreStub{})
	require.Empty(t, tools.migrationPrompt(context.Background(), uuid.New(), user), "no action is configured")

	actions := []repository.UserMigrationAction{userMigrationAction("migrate-db", "laptop-box", "Runs the schema migrations.", "dbmate up")}
	tools.SetMigrations(&migrationStoreStub{actions: actions})
	prompt := tools.migrationPrompt(context.Background(), uuid.New(), user)
	require.Contains(t, prompt, "run_migration(action)")
	require.Contains(t, prompt, `migrate-db on machine "laptop-box" (running): Runs the schema migrations.`)

	for i := 0; i < 9; i++ {
		actions = append(actions, userMigrationAction("migrate-many-"+string(rune('a'+i)), "rack-box", "Runs the schema migrations.", "dbmate up"))
	}
	tools.SetMigrations(&migrationStoreStub{actions: actions})
	prompt = tools.migrationPrompt(context.Background(), uuid.New(), user)
	require.Contains(t, prompt, "migrate-db")
	require.Contains(t, prompt, "migrate-many-g", "at most eight actions are listed")
	require.NotContains(t, prompt, "migrate-many-h", "at most eight actions are listed")

	tools.SetSettings(worktreeSettings{"run_migration": true})
	require.Empty(t, tools.migrationPrompt(context.Background(), uuid.New(), user))
}
