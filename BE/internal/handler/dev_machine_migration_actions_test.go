package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/service"
	cryptoutil "github.com/kuayle/kuayle-backend/pkg/crypto"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
)

// migrationActionStoreFake mirrors the repository's authority rules: the
// machine's creator and workspace owners and admins may configure actions,
// and authority is checked before the action is looked up.
type migrationActionStoreFake struct {
	repository.DevMachineStore
	creator         uuid.UUID
	admins          map[uuid.UUID]bool
	actions         []*domain.MigrationAction
	updateAction    *domain.MigrationAction
	updateNewSecret *bool
}

func (f *migrationActionStoreFake) mayConfigure(user uuid.UUID) bool {
	return user == f.creator || f.admins[user]
}

func (f *migrationActionStoreFake) ListMachineMigrationActions(_ context.Context, _, machine, user uuid.UUID) ([]domain.MigrationAction, error) {
	if !f.mayConfigure(user) {
		return nil, repository.ErrMigrationActionForbidden
	}
	actions := make([]domain.MigrationAction, 0, len(f.actions))
	for _, action := range f.actions {
		if action.MachineID == machine {
			actions = append(actions, *action)
		}
	}
	return actions, nil
}

func (f *migrationActionStoreFake) CreateMachineMigrationAction(_ context.Context, ws, machine, user uuid.UUID, action *domain.MigrationAction) (*domain.MigrationAction, error) {
	if !f.mayConfigure(user) {
		return nil, repository.ErrMigrationActionForbidden
	}
	for _, existing := range f.actions {
		if existing.MachineID == machine && existing.Name == action.Name {
			return nil, repository.ErrMigrationActionConflict
		}
	}
	created := *action
	created.ID = uuid.New()
	created.WorkspaceID, created.MachineID, created.CreatedByUserID = ws, machine, &user
	f.actions = append(f.actions, &created)
	return &created, nil
}

func (f *migrationActionStoreFake) UpdateMachineMigrationAction(_ context.Context, _, machine, actionID, user uuid.UUID, action *domain.MigrationAction, newSecret bool, validateStoredSecret func(string) error) (*domain.MigrationAction, error) {
	newSecretValue := newSecret
	f.updateAction, f.updateNewSecret = action, &newSecretValue
	if !f.mayConfigure(user) {
		return nil, repository.ErrMigrationActionForbidden
	}
	for _, existing := range f.actions {
		if existing.ID != actionID || existing.MachineID != machine {
			continue
		}
		if !newSecret {
			if err := validateStoredSecret(existing.EncryptedSecret); err != nil {
				return nil, err
			}
		}
		for _, other := range f.actions {
			if other != existing && other.MachineID == machine && other.Name == action.Name {
				return nil, repository.ErrMigrationActionConflict
			}
		}
		existing.Name, existing.Description, existing.Command = action.Name, action.Description, action.Command
		if newSecret {
			existing.EncryptedSecret = action.EncryptedSecret
		}
		updated := *existing
		return &updated, nil
	}
	return nil, repository.ErrMigrationActionNotFound
}

func (f *migrationActionStoreFake) DeleteMachineMigrationAction(_ context.Context, _, machine, actionID, user uuid.UUID) error {
	if !f.mayConfigure(user) {
		return repository.ErrMigrationActionForbidden
	}
	for i, existing := range f.actions {
		if existing.ID == actionID && existing.MachineID == machine {
			f.actions = append(f.actions[:i], f.actions[i+1:]...)
			return nil
		}
	}
	return repository.ErrMigrationActionNotFound
}

func migrationActionHandler(store *migrationActionStoreFake) *DevMachineHandler {
	machineService := service.NewDevMachineService(
		store, agent.NewRegistry(), true, "machines.example.test", cryptoutil.DeriveKey("test"), time.Minute, service.DevMachineImages{},
	)
	machineService.SetMigrationActions(store)
	return NewDevMachineHandler(machineService)
}

func sendMigrationAction(t *testing.T, h *DevMachineHandler, method, path, body string, workspaceID, machineID, userID, actionID uuid.UUID) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	ctx := devMachineHandlerContext(request, recorder, workspaceID, userID)
	values := echo.PathValues{{Name: "machineId", Value: machineID.String()}}
	if actionID != uuid.Nil {
		values = append(values, echo.PathValue{Name: "actionId", Value: actionID.String()})
	}
	ctx.SetPathValues(values)
	switch method {
	case http.MethodGet:
		require.NoError(t, h.ListMigrationActions(ctx))
	case http.MethodPost:
		require.NoError(t, h.CreateMigrationAction(ctx))
	case http.MethodPut:
		require.NoError(t, h.UpdateMigrationAction(ctx))
	case http.MethodDelete:
		require.NoError(t, h.DeleteMigrationAction(ctx))
	}
	return recorder
}

// A member who is not the machine's creator may not list, create, update or
// delete its migration actions; the machine's creator (a member) and a
// workspace owner who does not own the machine may, and no response ever
// carries the secret.
func TestDevMachineMigrationActionAuthority(t *testing.T) {
	workspaceID, machineID := uuid.New(), uuid.New()
	creator, owner, member := uuid.New(), uuid.New(), uuid.New()
	store := &migrationActionStoreFake{creator: creator, admins: map[uuid.UUID]bool{owner: true}}
	h := migrationActionHandler(store)
	base := "/dev-machines/" + machineID.String() + "/migration-actions"
	body := `{"name":"migrate","description":"Run the database migrations","command":"migrate --url $KUAYLE_MIGRATION_SECRET --up","secret":"postgres://hunter2@example/db"}`

	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete} {
		denied := sendMigrationAction(t, h, method, base, body, workspaceID, machineID, member, uuid.New())
		require.Equal(t, http.StatusForbidden, denied.Code, method)
	}

	created := sendMigrationAction(t, h, http.MethodPost, base, body, workspaceID, machineID, creator, uuid.Nil)
	require.Equal(t, http.StatusCreated, created.Code, created.Body.String())
	require.NotContains(t, created.Body.String(), "secret", "no secret field appears at all")
	require.NotContains(t, created.Body.String(), "postgres://hunter2@example/db")
	var action domain.MigrationAction
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &action))
	require.Equal(t, "migrate", action.Name)
	require.Len(t, store.actions, 1)
	require.NotEmpty(t, store.actions[0].EncryptedSecret, "the secret is stored encrypted")
	require.NotEqual(t, "postgres://hunter2@example/db", store.actions[0].EncryptedSecret)

	listed := sendMigrationAction(t, h, http.MethodGet, base, "", workspaceID, machineID, creator, uuid.Nil)
	require.Equal(t, http.StatusOK, listed.Code, listed.Body.String())
	require.Contains(t, listed.Body.String(), `"actions":[`)
	require.NotContains(t, listed.Body.String(), "secret")

	actionID := store.actions[0].ID
	// An update without a secret keeps the stored one.
	updated := sendMigrationAction(t, h, http.MethodPut, base+"/"+actionID.String(), `{"name":"migrate","description":"Nightly migrations","command":"migrate --url $KUAYLE_MIGRATION_SECRET --up --all"}`, workspaceID, machineID, owner, actionID)
	require.Equal(t, http.StatusOK, updated.Code, updated.Body.String())
	require.NotContains(t, updated.Body.String(), "secret")
	require.NotNil(t, *store.updateNewSecret)
	require.False(t, *store.updateNewSecret, "an omitted secret keeps the stored one")
	require.Empty(t, store.updateAction.EncryptedSecret)
	require.NotEmpty(t, store.actions[0].EncryptedSecret)
	require.Equal(t, "Nightly migrations", store.actions[0].Description)

	// A workspace owner may delete without owning the machine.
	deleted := sendMigrationAction(t, h, http.MethodDelete, base+"/"+actionID.String(), "", workspaceID, machineID, owner, actionID)
	require.Equal(t, http.StatusNoContent, deleted.Code)
	require.Empty(t, store.actions)
	missing := sendMigrationAction(t, h, http.MethodDelete, base+"/"+actionID.String(), "", workspaceID, machineID, owner, actionID)
	require.Equal(t, http.StatusNotFound, missing.Code)
}

// Invalid input is refused before anything is stored, with the secret never
// echoed back.
func TestDevMachineMigrationActionInvalidInput(t *testing.T) {
	workspaceID, machineID, creator := uuid.New(), uuid.New(), uuid.New()
	base := "/dev-machines/" + machineID.String() + "/migration-actions"
	for _, test := range []struct {
		name string
		body string
	}{
		{name: "empty name", body: `{"name":"","description":"Run migrations","command":"migrate --up","secret":"s"}`},
		{name: "missing secret on create", body: `{"name":"migrate","description":"Run migrations","command":"migrate --up"}`},
		{name: "oversized secret", body: fmt.Sprintf(`{"name":"migrate","description":"Run migrations","command":"migrate --up","secret":%q}`, strings.Repeat("x", domain.MigrationSecretMaxBytes+1))},
		{name: "blank description", body: `{"name":"migrate","description":"  ","command":"migrate --up","secret":"s"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &migrationActionStoreFake{creator: creator}
			h := migrationActionHandler(store)
			recorder := sendMigrationAction(t, h, http.MethodPost, base, test.body, workspaceID, machineID, creator, uuid.Nil)
			require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
			var body dto.ErrorResponse
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
			require.Equal(t, "INVALID_MIGRATION_ACTION", body.Error.Code)
			require.NotContains(t, recorder.Body.String(), "secret-value")
			require.Empty(t, store.actions, "nothing is stored")
		})
	}
}

// A second action of the same name on the machine is a conflict, and an
// update may set a new secret, which is re-encrypted and never returned.
func TestDevMachineMigrationActionConflictAndNewSecret(t *testing.T) {
	workspaceID, machineID, creator := uuid.New(), uuid.New(), uuid.New()
	store := &migrationActionStoreFake{creator: creator}
	h := migrationActionHandler(store)
	base := "/dev-machines/" + machineID.String() + "/migration-actions"
	body := `{"name":"migrate","description":"Run migrations","command":"migrate --up","secret":"first-secret"}`

	created := sendMigrationAction(t, h, http.MethodPost, base, body, workspaceID, machineID, creator, uuid.Nil)
	require.Equal(t, http.StatusCreated, created.Code, created.Body.String())

	duplicate := sendMigrationAction(t, h, http.MethodPost, base, body, workspaceID, machineID, creator, uuid.Nil)
	require.Equal(t, http.StatusConflict, duplicate.Code, duplicate.Body.String())
	var conflict dto.ErrorResponse
	require.NoError(t, json.Unmarshal(duplicate.Body.Bytes(), &conflict))
	require.Equal(t, "MIGRATION_ACTION_CONFLICT", conflict.Error.Code)

	actionID := store.actions[0].ID
	rotated := sendMigrationAction(t, h, http.MethodPut, base+"/"+actionID.String(), `{"name":"migrate","description":"Run migrations","command":"migrate --up","secret":"second-secret"}`, workspaceID, machineID, creator, actionID)
	require.Equal(t, http.StatusOK, rotated.Code, rotated.Body.String())
	require.NotContains(t, rotated.Body.String(), "secret")
	require.True(t, *store.updateNewSecret, "a sent secret replaces the stored one")
	require.NotEmpty(t, store.updateAction.EncryptedSecret)
	require.NotEqual(t, "second-secret", store.updateAction.EncryptedSecret)
}

func TestDevMachineMigrationActionRejectsStoredSecretInAnEdit(t *testing.T) {
	workspaceID, machineID, creator := uuid.New(), uuid.New(), uuid.New()
	store := &migrationActionStoreFake{creator: creator}
	h := migrationActionHandler(store)
	base := "/dev-machines/" + machineID.String() + "/migration-actions"
	connection := "fixture-" + uuid.NewString()
	input := dto.DevMachineMigrationActionInput{
		Name: "migrate", Description: "Run migrations", Command: "migrate up", Secret: connection,
	}
	body, err := json.Marshal(input)
	require.NoError(t, err)
	created := sendMigrationAction(t, h, http.MethodPost, base, string(body), workspaceID, machineID, creator, uuid.Nil)
	require.Equal(t, http.StatusCreated, created.Code)
	actionID := store.actions[0].ID

	for name, edit := range map[string]dto.DevMachineMigrationActionInput{
		"command":     {Name: input.Name, Description: input.Description, Command: "migrate up " + connection},
		"description": {Name: input.Name, Description: "Run migrations for " + connection, Command: input.Command},
	} {
		t.Run(name, func(t *testing.T) {
			body, err := json.Marshal(edit)
			require.NoError(t, err)
			updated := sendMigrationAction(t, h, http.MethodPut, base+"/"+actionID.String(), string(body), workspaceID, machineID, creator, actionID)
			require.Equal(t, http.StatusBadRequest, updated.Code)
			require.False(t, strings.Contains(updated.Body.String(), connection), "response exposed the stored secret")
			require.True(t, store.actions[0].Command == input.Command, "command was changed")
			require.True(t, store.actions[0].Description == input.Description, "description was changed")
		})
	}
}
