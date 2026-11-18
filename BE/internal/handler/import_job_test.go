package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/importer"
	"github.com/kuayle/kuayle-backend/internal/importer/jira"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/crypto"
)

func TestParseItemPage(t *testing.T) {
	for _, tc := range []struct {
		limitRaw, offsetRaw   string
		wantLimit, wantOffset int
	}{
		{"", "", 200, 0},
		{"50", "10", 50, 10},
		{"abc", "-5", 200, 0},
	} {
		limit, offset := parseItemPage(tc.limitRaw, tc.offsetRaw)
		if limit != tc.wantLimit || offset != tc.wantOffset {
			t.Errorf("parseItemPage(%q, %q) = %d, %d; want %d, %d",
				tc.limitRaw, tc.offsetRaw, limit, offset, tc.wantLimit, tc.wantOffset)
		}
	}
}

// importRepoStub implements only what these handler tests reach; any other
// ImportRepo method panics through the nil embedded interface.
type importRepoStub struct {
	repository.ImportRepo
	job   *domain.ImportJob
	items []domain.ImportJobItem
}

func (s *importRepoStub) Get(_ context.Context, workspaceID, id uuid.UUID) (*domain.ImportJob, error) {
	if s.job == nil || s.job.ID != id || s.job.WorkspaceID != workspaceID {
		return nil, repository.ErrImportJobNotFound
	}
	copyJob := *s.job
	return &copyJob, nil
}

// ListItems applies the real repository's limit clamp, so the handler, the
// service and the repository cap are exercised together.
func (s *importRepoStub) ListItems(_ context.Context, _, _ uuid.UUID, limit, offset int) ([]domain.ImportJobItem, error) {
	limit = repository.ImportItemsQueryLimit(limit)
	if offset >= len(s.items) {
		return []domain.ImportJobItem{}, nil
	}
	page := s.items[offset:]
	if len(page) > limit {
		page = page[:limit]
	}
	return page, nil
}

type failingImportSource struct{ err error }

func (f failingImportSource) Preview(context.Context) (*importer.SourcePreview, error) {
	return nil, f.err
}
func (f failingImportSource) Fetch(context.Context, importer.EmitFunc) error { return f.err }
func (f failingImportSource) OpenAttachment(context.Context, importer.SourceAttachment) (io.ReadCloser, error) {
	return nil, f.err
}

func importRequest(t *testing.T, h *ImportJobHandler, ws *domain.Workspace, handle func(*ImportJobHandler, *echo.Context) error, method, target, id string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, target, nil)
	recorder := httptest.NewRecorder()
	c := echo.New().NewContext(request, recorder)
	c.Set("workspace", ws)
	c.SetPathValues(echo.PathValues{{Name: "id", Value: id}})
	require.NoError(t, handle(h, c))
	return recorder
}

// The UI asks for the report without a limit. The default page and every
// allowed size must come back full with has_more set while rows remain.
func TestImportItemsPageThroughHandlerServiceAndRepositoryCap(t *testing.T) {
	ws := &domain.Workspace{ID: uuid.New(), Slug: "acme"}
	job := &domain.ImportJob{ID: uuid.New(), WorkspaceID: ws.ID, Status: domain.ImportStatusFailed}
	repo := &importRepoStub{job: job}
	for i := 0; i < 1000; i++ {
		repo.items = append(repo.items, domain.ImportJobItem{
			JobID: job.ID, WorkspaceID: ws.ID, ExternalType: domain.ImportExternalIssue,
			ExternalID: fmt.Sprint(i), ExternalKey: fmt.Sprintf("PROJ-%d", i), Outcome: domain.ImportOutcomeFailed,
		})
	}
	h := NewImportJobHandler(service.NewImportService(repo, nil, nil, []byte("0123456789abcdef0123456789abcdef"), 0))

	read := func(query string) dto.ImportItemsResponse {
		rec := importRequest(t, h, ws, (*ImportJobHandler).ListItems, http.MethodGet, "/imports/x/items"+query, job.ID.String())
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var out dto.ImportItemsResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
		return out
	}

	first := read("")
	require.Len(t, first.Items, 200, "the default page is 200 rows")
	require.True(t, first.HasMore)
	require.Equal(t, "PROJ-0", first.Items[0].ExternalKey)

	total := 0
	for offset := 0; ; {
		page := read(fmt.Sprintf("?offset=%d", offset))
		total += len(page.Items)
		offset += len(page.Items)
		if !page.HasMore {
			break
		}
	}
	require.Equal(t, 1000, total, "Load more reaches every row")

	for _, limit := range []int{201, 300, 500} {
		page := read(fmt.Sprintf("?limit=%d", limit))
		require.Len(t, page.Items, limit)
		require.True(t, page.HasMore)
	}
	clamped := read("?limit=9999&offset=600")
	require.Len(t, clamped.Items, 400)
	require.False(t, clamped.HasMore)

	rec := importRequest(t, h, &domain.Workspace{ID: uuid.New()}, (*ImportJobHandler).ListItems, http.MethodGet, "/imports/x/items", job.ID.String())
	require.Equal(t, http.StatusNotFound, rec.Code, "another workspace cannot read the report")
}

// A failed Jira call during preview is a 502 with an actionable message, not
// a bare 500, and the message carries no credentials.
func TestImportPreviewSourceFailureIsBadGateway(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	secret := "SECRET-TOKEN"
	encrypted, err := crypto.Encrypt(`{"base_url":"https://acme.atlassian.net","token":"`+secret+`"}`, key)
	require.NoError(t, err)
	ws := &domain.Workspace{ID: uuid.New(), Slug: "acme"}
	job := &domain.ImportJob{ID: uuid.New(), WorkspaceID: ws.ID, Status: domain.ImportStatusDraft, CredentialsEnc: &encrypted}
	factory := func(context.Context, json.RawMessage, json.RawMessage) (importer.Source, error) {
		return failingImportSource{err: fmt.Errorf("search: %w", &jira.StatusError{Status: http.StatusUnauthorized})}, nil
	}
	h := NewImportJobHandler(service.NewImportService(&importRepoStub{job: job}, nil, factory, key, 0))

	rec := importRequest(t, h, ws, (*ImportJobHandler).Preview, http.MethodPost, "/imports/x/preview", job.ID.String())
	require.Equal(t, http.StatusBadGateway, rec.Code, rec.Body.String())
	var body dto.ErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "IMPORT_SOURCE_FAILED", body.Error.Code)
	require.Contains(t, body.Error.Message, "rejected the credentials (HTTP 401)")
	require.NotContains(t, rec.Body.String(), secret)

	// A job that can no longer be previewed is refused before Jira is called.
	job.Status = domain.ImportStatusRunning
	rec = importRequest(t, h, ws, (*ImportJobHandler).Preview, http.MethodPost, "/imports/x/preview", job.ID.String())
	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
}
