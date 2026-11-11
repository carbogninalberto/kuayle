package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
)

// memoryStoreStub answers as an admin's store would and records what the
// handler passed down; fail makes every write return that error.
type memoryStoreStub struct {
	role     string
	fail     error
	text     *string
	share    bool
	state    string
	repo     string
	ordered  []uuid.UUID
	approved bool
}

func (s *memoryStoreStub) MemberRole(context.Context, uuid.UUID, uuid.UUID) (string, error) {
	return s.role, nil
}
func (s *memoryStoreStub) List(_ context.Context, _, _ uuid.UUID, repo, state string) ([]domain.Lesson, error) {
	s.repo, s.state = repo, state
	return []domain.Lesson{{Text: "Run the checks", State: domain.LessonApproved}}, s.fail
}
func (s *memoryStoreStub) Add(_ context.Context, _, _ uuid.UUID, repo, text string) (*domain.Lesson, error) {
	s.repo, s.text = repo, &text
	return &domain.Lesson{Text: text, Repository: repo}, s.fail
}
func (s *memoryStoreStub) Approve(_ context.Context, _, _, _ uuid.UUID, text *string) (*domain.Lesson, error) {
	s.text, s.approved = text, true
	return &domain.Lesson{State: domain.LessonApproved}, s.fail
}
func (s *memoryStoreStub) Edit(_ context.Context, _, _, _ uuid.UUID, text *string, share bool) (*domain.Lesson, error) {
	s.text, s.share = text, share
	return &domain.Lesson{}, s.fail
}
func (s *memoryStoreStub) Retire(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (*domain.Lesson, error) {
	return &domain.Lesson{State: domain.LessonRetired}, s.fail
}
func (s *memoryStoreStub) Drop(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (*domain.Lesson, error) {
	return &domain.Lesson{State: domain.LessonDropped}, s.fail
}
func (s *memoryStoreStub) Reorder(_ context.Context, _, _ uuid.UUID, repo string, ids []uuid.UUID) ([]domain.Lesson, error) {
	s.repo, s.ordered = repo, ids
	return []domain.Lesson{}, s.fail
}
func (s *memoryStoreStub) Events(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) ([]domain.LessonEvent, error) {
	return []domain.LessonEvent{{Action: "approved"}}, s.fail
}
func (s *memoryStoreStub) ApprovedTexts(context.Context, uuid.UUID, uuid.UUID, string) ([]string, error) {
	return nil, nil
}
func (s *memoryStoreStub) TaskRepository(context.Context, domain.AgentTask) (string, error) {
	return "", nil
}

func memoryRequest(t *testing.T, store *memoryStoreStub, handle func(*WorkspaceMemoryHandler, *echo.Context) error, method, target, body, lesson string) *httptest.ResponseRecorder {
	t.Helper()
	ws := &domain.Workspace{ID: uuid.New()}
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	request = request.WithContext(domain.WithAccess(request.Context(), domain.Access{WorkspaceID: ws.ID, UserID: uuid.New()}))
	recorder := httptest.NewRecorder()
	c := echo.New().NewContext(request, recorder)
	c.Set("workspace", ws)
	if lesson != "" {
		c.SetPathValues(echo.PathValues{{Name: "lessonId", Value: lesson}})
	}
	require.NoError(t, handle(NewWorkspaceMemoryHandler(service.NewWorkspaceMemoryService(store, nil)), c))
	return recorder
}

func TestWorkspaceMemoryRoutesBindDecisionsAndMapRefusals(t *testing.T) {
	id := uuid.NewString()
	store := &memoryStoreStub{role: domain.RoleAdmin}

	rec := memoryRequest(t, store, (*WorkspaceMemoryHandler).List, http.MethodGet, "/memory/lessons?repository=acme/app&state=proposed", "", "")
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "acme/app", store.repo)
	require.Equal(t, domain.LessonProposed, store.state)
	var listed service.LessonList
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &listed))
	require.True(t, listed.CanManage)
	require.Len(t, listed.Lessons, 1)
	rec = memoryRequest(t, store, (*WorkspaceMemoryHandler).List, http.MethodGet, "/memory/lessons?state=pending", "", "")
	require.Equal(t, http.StatusBadRequest, rec.Code)

	rec = memoryRequest(t, store, (*WorkspaceMemoryHandler).Add, http.MethodPost, "/memory/lessons", `{"repository":"acme/app","text":"Run the checks"}`, "")
	require.Equal(t, http.StatusCreated, rec.Code)
	require.Equal(t, "Run the checks", *store.text)
	rec = memoryRequest(t, store, (*WorkspaceMemoryHandler).Add, http.MethodPost, "/memory/lessons", `{"repository":"acme/app","text":"x","state":"approved"}`, "")
	require.Equal(t, http.StatusBadRequest, rec.Code, "a caller cannot choose the state")

	// Approving takes no body, or the edited text.
	store.text = nil
	rec = memoryRequest(t, store, (*WorkspaceMemoryHandler).Approve, http.MethodPost, "/memory/lessons/"+id+"/approve", "", id)
	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, store.approved)
	require.Nil(t, store.text)
	rec = memoryRequest(t, store, (*WorkspaceMemoryHandler).Approve, http.MethodPost, "/memory/lessons/"+id+"/approve", `{"text":"Edited"}`, id)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "Edited", *store.text)
	rec = memoryRequest(t, store, (*WorkspaceMemoryHandler).Approve, http.MethodPost, "/memory/lessons/nope/approve", "", "nope")
	require.Equal(t, http.StatusNotFound, rec.Code)

	rec = memoryRequest(t, store, (*WorkspaceMemoryHandler).Edit, http.MethodPut, "/memory/lessons/"+id, `{"workspace_wide":true}`, id)
	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, store.share)
	require.Nil(t, store.text)
	rec = memoryRequest(t, store, (*WorkspaceMemoryHandler).Edit, http.MethodPut, "/memory/lessons/"+id, `{}`, id)
	require.Equal(t, http.StatusBadRequest, rec.Code, "an edit changes something")

	first, second := uuid.New(), uuid.New()
	rec = memoryRequest(t, store, (*WorkspaceMemoryHandler).Reorder, http.MethodPut, "/memory/lessons/order", `{"repository":"acme/app","ids":["`+first.String()+`","`+second.String()+`"]}`, "")
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, []uuid.UUID{first, second}, store.ordered)
	rec = memoryRequest(t, store, (*WorkspaceMemoryHandler).Reorder, http.MethodPut, "/memory/lessons/order", `{"ids":[]}`, "")
	require.Equal(t, http.StatusBadRequest, rec.Code)

	for _, handle := range []func(*WorkspaceMemoryHandler, *echo.Context) error{(*WorkspaceMemoryHandler).Retire, (*WorkspaceMemoryHandler).Drop, (*WorkspaceMemoryHandler).Audit} {
		rec = memoryRequest(t, store, handle, http.MethodPost, "/memory/lessons/"+id, "", id)
		require.Equal(t, http.StatusOK, rec.Code)
	}

	// Refusals keep their meaning over HTTP.
	for status, err := range map[int]error{
		http.StatusForbidden:           repository.ErrLessonDenied,
		http.StatusNotFound:            repository.ErrLessonNotFound,
		http.StatusBadRequest:          domain.ErrLessonTooLong,
		http.StatusConflict:            repository.ErrLessonState,
		http.StatusInternalServerError: context.DeadlineExceeded,
	} {
		rec = memoryRequest(t, &memoryStoreStub{role: domain.RoleOwner, fail: err}, (*WorkspaceMemoryHandler).Retire, http.MethodPost, "/memory/lessons/"+id+"/retire", "", id)
		require.Equal(t, status, rec.Code, err)
	}
	for _, err := range []error{repository.ErrLessonLimit, repository.ErrLessonPendingLimit, repository.ErrLessonDuplicate} {
		rec = memoryRequest(t, &memoryStoreStub{role: domain.RoleOwner, fail: err}, (*WorkspaceMemoryHandler).Add, http.MethodPost, "/memory/lessons", `{"repository":"acme/app","text":"x"}`, "")
		require.Equal(t, http.StatusConflict, rec.Code, err)
	}

	// Members cannot decide and guests cannot read, whatever the client shows.
	rec = memoryRequest(t, &memoryStoreStub{role: domain.RoleMember}, (*WorkspaceMemoryHandler).Drop, http.MethodPost, "/memory/lessons/"+id+"/drop", "", id)
	require.Equal(t, http.StatusForbidden, rec.Code)
	rec = memoryRequest(t, &memoryStoreStub{role: domain.RoleGuest}, (*WorkspaceMemoryHandler).List, http.MethodGet, "/memory/lessons", "", "")
	require.Equal(t, http.StatusForbidden, rec.Code)
}
