package handler

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	mw "github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/realtime"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type undoHandlerFixture struct {
	repo  *testIssueRepo
	h     *IssueHandler
	ws    uuid.UUID
	user  uuid.UUID
	issue *domain.Issue
}

func newUndoHandlerFixture(creator uuid.UUID) *undoHandlerFixture {
	f := &undoHandlerFixture{repo: newTestIssueRepo(), ws: uuid.New(), user: uuid.New()}
	if creator == uuid.Nil {
		creator = f.user
	}
	f.issue = &domain.Issue{ID: uuid.New(), WorkspaceID: f.ws, Identifier: "ENG-1", Title: "Undo me", CreatorID: creator, Priority: domain.PriorityNone, Version: 1}
	f.repo.issues["ENG-1"] = f.issue
	notifSvc := service.NewNotificationService(&testNotifRepo{})
	issueSvc := service.NewIssueService(f.repo, newTestTeamRepo(), &testTeamStatusRepo{}, &testHistoryRepo{}, realtime.NewHub(), notifSvc)
	f.h = NewIssueHandler(issueSvc, service.NewCommentService(&testCommentRepo{}, f.repo, realtime.NewHub(), notifSvc), &testUserRepo{}, &testTeamStatusRepo{}, nil, nil, nil)
	return f
}

func (f *undoHandlerFixture) call(t *testing.T, handler func(*echo.Context) error, method, path, body, role string, scopes []string) (int, map[string]any) {
	t.Helper()
	c, rec := setupIssueContext(echo.New(), method, path, body)
	c.Set("workspace", &domain.Workspace{ID: f.ws, Name: "Test", Slug: "test"})
	c.Set("workspace_role", role)
	c.Set(string(mw.UserIDKey), f.user)
	if scopes != nil {
		c.Set(mw.TokenScopesKey, scopes)
	}
	c.SetPathValues(echo.PathValues{{Name: "identifier", Value: "ENG-1"}})
	require.NoError(t, handler(c))
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestIssueHandler_UpdateReturnsUndoThatPutsTheValueBack(t *testing.T) {
	f := newUndoHandlerFixture(uuid.Nil)

	code, body := f.call(t, f.h.Update, http.MethodPatch, "/issues/ENG-1", `{"priority":1}`, domain.RoleMember, nil)
	require.Equal(t, http.StatusOK, code)
	data := body
	undo := data["undo"].(map[string]any)
	assert.EqualValues(t, data["version"], undo["expected_version"])
	assert.EqualValues(t, 0, undo["previous"].(map[string]any)["priority"])

	payload, err := json.Marshal(undo)
	require.NoError(t, err)
	code, body = f.call(t, f.h.Undo, http.MethodPost, "/issues/ENG-1/undo", string(payload), domain.RoleMember, nil)
	require.Equal(t, http.StatusOK, code)
	assert.EqualValues(t, 0, body["priority"])
	_, hasUndo := body["undo"]
	assert.False(t, hasUndo, "an undo cannot itself be undone")
	assert.Equal(t, domain.PriorityNone, f.issue.Priority)
}

func TestIssueHandler_UndoAfterConflictingEditIsRefused(t *testing.T) {
	f := newUndoHandlerFixture(uuid.Nil)
	f.issue.Priority, f.issue.Version = domain.PriorityLow, 9

	code, body := f.call(t, f.h.Undo, http.MethodPost, "/issues/ENG-1/undo", `{"expected_version":8,"previous":{"priority":0}}`, domain.RoleMember, nil)

	require.Equal(t, http.StatusConflict, code)
	errBody := body["error"].(map[string]any)
	assert.Equal(t, "EDITED_MEANWHILE", errBody["code"])
	assert.Equal(t, "This item was edited in the meantime, so nothing was undone.", errBody["message"])
	assert.Equal(t, domain.PriorityLow, f.issue.Priority, "nothing changed")
}

func TestIssueHandler_BulkUndoIsOneStep(t *testing.T) {
	f := newUndoHandlerFixture(uuid.Nil)
	other := &domain.Issue{ID: uuid.New(), WorkspaceID: f.ws, Identifier: "ENG-2", Priority: domain.PriorityHigh, Version: 3}
	f.repo.issues["ENG-2"] = other
	f.issue.Priority, f.issue.Version = domain.PriorityHigh, 2

	items := dto.BulkIssueUndo{Items: []dto.BulkIssueUndoItem{
		{ID: f.issue.ID.String(), ExpectedVersion: 2, Previous: dto.IssueUndoPrevious{Priority: ptr(0)}},
		{ID: other.ID.String(), ExpectedVersion: 2, Previous: dto.IssueUndoPrevious{Priority: ptr(0)}},
	}}
	payload, _ := json.Marshal(items)
	code, _ := f.call(t, f.h.BulkUndo, http.MethodPost, "/issues/bulk/undo", string(payload), domain.RoleMember, nil)
	require.Equal(t, http.StatusConflict, code)
	assert.Equal(t, domain.PriorityHigh, f.issue.Priority, "one conflict refuses the whole bulk undo")

	items.Items[1].ExpectedVersion = 3
	payload, _ = json.Marshal(items)
	code, body := f.call(t, f.h.BulkUndo, http.MethodPost, "/issues/bulk/undo", string(payload), domain.RoleMember, nil)
	require.Equal(t, http.StatusOK, code)
	assert.EqualValues(t, 2, body["updated"])
	assert.Equal(t, domain.PriorityNone, other.Priority)
}

func TestIssueHandler_DeleteThenRestoreWithinGracePeriod(t *testing.T) {
	f := newUndoHandlerFixture(uuid.Nil)

	code, body := f.call(t, f.h.Delete, http.MethodDelete, "/issues/ENG-1", "", domain.RoleMember, nil)
	require.Equal(t, http.StatusOK, code)
	until, err := time.Parse(time.RFC3339, body["restorable_until"].(string))
	require.NoError(t, err)
	assert.WithinDuration(t, time.Now().Add(service.DefaultIssueDeleteGracePeriod), until, time.Minute)

	code, body = f.call(t, f.h.Restore, http.MethodPost, "/issues/ENG-1/restore", "", domain.RoleMember, nil)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "ENG-1", body["identifier"])
	assert.Contains(t, f.repo.issues, "ENG-1")
}

func TestIssueHandler_RestoreAfterGracePeriodIsGone(t *testing.T) {
	f := newUndoHandlerFixture(uuid.Nil)
	f.repo.softDelete(f.issue.ID)
	f.repo.deleted[f.issue.ID].DeletedAt = time.Now().Add(-service.DefaultIssueDeleteGracePeriod - time.Hour)

	code, body := f.call(t, f.h.Restore, http.MethodPost, "/issues/ENG-1/restore", "", domain.RoleOwner, nil)

	require.Equal(t, http.StatusGone, code)
	assert.Equal(t, "GRACE_EXPIRED", body["error"].(map[string]any)["code"])
	assert.NotContains(t, f.repo.issues, "ENG-1")
}

// Restore follows Delete: a member restores only issues they created, and a
// token without issue:delete is limited to its owner's issues.
func TestIssueHandler_RestorePermissions(t *testing.T) {
	for _, tc := range []struct {
		name    string
		role    string
		scopes  []string
		ownIsue bool
		want    int
	}{
		{"member cannot restore another member's issue", domain.RoleMember, nil, false, http.StatusForbidden},
		{"member restores their own issue", domain.RoleMember, nil, true, http.StatusOK},
		{"admin restores any issue", domain.RoleAdmin, nil, false, http.StatusOK},
		{"own-scope token cannot restore another user's issue", domain.RoleAdmin, []string{domain.PermIssueDeleteOwn}, false, http.StatusForbidden},
		{"issue:delete token restores another user's issue", domain.RoleAdmin, []string{domain.PermIssueDeleteOwn, domain.PermIssueDelete}, false, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			creator := uuid.New()
			f := newUndoHandlerFixture(creator)
			if tc.ownIsue {
				f.issue.CreatorID = f.user
			}
			f.repo.softDelete(f.issue.ID)
			code, _ := f.call(t, f.h.Restore, http.MethodPost, "/issues/ENG-1/restore", "", tc.role, tc.scopes)
			assert.Equal(t, tc.want, code)
			if tc.want != http.StatusOK {
				assert.NotContains(t, f.repo.issues, "ENG-1", "a refused restore restores nothing")
			}
		})
	}
}

func ptr[T any](v T) *T { return &v }
