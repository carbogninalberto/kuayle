package service

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type iconStore struct {
	mu    sync.Mutex
	files map[string][]byte
}

func (m *iconStore) Put(_ context.Context, key string, r io.Reader, _ string) (int64, error) {
	b, _ := io.ReadAll(r)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files[key] = b
	return int64(len(b)), nil
}
func (m *iconStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.files[key]
	if !ok {
		return nil, os.ErrNotExist
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}
func (m *iconStore) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.files, key)
	return nil
}
func (m *iconStore) URL(_ context.Context, key string) (string, error) { return key, nil }

// iconFakeRepo keeps one team in memory and, like the repository, reports the
// icon an update replaced.
type iconFakeRepo struct {
	repository.TeamRepo
	team *domain.Team
}

func (r *iconFakeRepo) GetByID(context.Context, uuid.UUID) (*domain.Team, error) {
	copied := *r.team
	return &copied, nil
}

func (r *iconFakeRepo) Update(_ context.Context, team *domain.Team) error {
	team.ReplacedIcon = r.team.Icon
	icon := r.team.Icon
	*r.team = *team
	if !team.IconSet {
		r.team.Icon = icon
		team.Icon = icon
	}
	return nil
}

func (r *iconFakeRepo) SetIcon(_ context.Context, _ uuid.UUID, icon string) (*domain.Team, error) {
	r.team.ReplacedIcon, r.team.Icon = r.team.Icon, &icon
	copied := *r.team
	return &copied, nil
}

func TestTeamServiceIconImage(t *testing.T) {
	workspaceID, teamID := uuid.New(), uuid.New()
	repo := &iconFakeRepo{team: &domain.Team{ID: teamID, WorkspaceID: workspaceID}}
	svc := NewTeamService(repo, new(mockTeamStatusRepo))
	store := &iconStore{files: map[string][]byte{}}
	svc.SetIconStore(store)
	ctx := context.Background()

	_, err := svc.UploadIcon(ctx, uuid.New(), teamID, strings.NewReader("x"), "image/png", ".png")
	assert.ErrorIs(t, err, ErrTeamNotFound, "another workspace's team")
	assert.Empty(t, store.files)

	updated, err := svc.UploadIcon(ctx, workspaceID, teamID, strings.NewReader("one"), "image/png", ".png")
	require.NoError(t, err)
	first, ok := domain.TeamIconImageFile(updated.Icon)
	require.True(t, ok)
	assert.LessOrEqual(t, len(*updated.Icon), 50, "fits the icon column")
	require.Len(t, store.files, 1)

	rc, err := svc.Icon(ctx, workspaceID, teamID, first)
	require.NoError(t, err)
	body, _ := io.ReadAll(rc)
	_ = rc.Close()
	assert.Equal(t, "one", string(body))
	_, err = svc.Icon(ctx, uuid.New(), teamID, first)
	assert.ErrorIs(t, err, ErrTeamNotFound)
	_, err = svc.Icon(ctx, workspaceID, teamID, uuid.New().String()+".png")
	assert.ErrorIs(t, err, ErrTeamNotFound, "only the current file is served")
	_, err = svc.Icon(ctx, workspaceID, teamID, "../x.png")
	assert.ErrorIs(t, err, ErrTeamNotFound)

	updated, err = svc.UploadIcon(ctx, workspaceID, teamID, strings.NewReader("two"), "image/png", ".png")
	require.NoError(t, err)
	assert.Len(t, store.files, 1, "the previous image is removed")
	assert.NotContains(t, *updated.Icon, first)

	forged := "image:" + uuid.New().String() + ".png"
	_, err = svc.Update(ctx, teamID, dto.UpdateTeamRequest{Icon: &forged})
	assert.ErrorIs(t, err, ErrInvalidTeamIcon, "image icons only come from the upload")
	_, err = svc.Create(ctx, workspaceID, uuid.New(), dto.CreateTeamRequest{Name: "X", Key: "X", Icon: &forged})
	assert.ErrorIs(t, err, ErrInvalidTeamIcon)

	same := *updated.Icon
	_, err = svc.Update(ctx, teamID, dto.UpdateTeamRequest{Icon: &same})
	require.NoError(t, err)
	assert.Len(t, store.files, 1, "resending the current image keeps it")

	name := "Renamed"
	renamed, err := svc.Update(ctx, teamID, dto.UpdateTeamRequest{Name: &name})
	require.NoError(t, err)
	assert.Equal(t, same, *renamed.Icon, "an update that does not set the icon keeps it")
	assert.Len(t, store.files, 1)

	emoji := "emoji:🚀"
	updated, err = svc.Update(ctx, teamID, dto.UpdateTeamRequest{Icon: &emoji})
	require.NoError(t, err)
	assert.Equal(t, emoji, *updated.Icon)
	assert.Empty(t, store.files, "switching to an emoji removes the image")
}

func TestTeamServiceIconPostgres(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := sqlx.Connect("pgx", url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	exec := func(query string, args ...any) {
		t.Helper()
		_, err := db.Exec(query, args...)
		require.NoError(t, err)
	}
	user, workspace, otherWorkspace, team := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Team icon','hash')`, user, user.String()+"@example.test")
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id=$1`, user) })
	for _, id := range []uuid.UUID{workspace, otherWorkspace} {
		exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Team icon',$2,$3)`, id, id.String(), user)
		t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM workspaces WHERE id=$1`, id) })
		exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'owner')`, id, user)
	}
	exec(`INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Icon','ICN')`, team, workspace)

	svc := NewTeamService(repository.NewTeamRepository(db), repository.NewTeamStatusRepository(db))
	store := &iconStore{files: map[string][]byte{}}
	svc.SetIconStore(store)
	ctx := domain.WithAccess(context.Background(), domain.Access{UserID: user, WorkspaceID: workspace})

	updated, err := svc.UploadIcon(ctx, workspace, team, strings.NewReader("png"), "image/png", ".png")
	require.NoError(t, err)
	var stored string
	require.NoError(t, db.Get(&stored, `SELECT icon FROM teams WHERE id=$1`, team))
	assert.Equal(t, *updated.Icon, stored)

	otherCtx := domain.WithAccess(context.Background(), domain.Access{UserID: user, WorkspaceID: otherWorkspace})
	_, err = svc.UploadIcon(otherCtx, otherWorkspace, team, strings.NewReader("png"), "image/png", ".png")
	assert.ErrorIs(t, err, ErrTeamNotFound, "a team of another workspace cannot be changed")

	emoji := "emoji:🚀"
	_, err = svc.Update(ctx, team, dto.UpdateTeamRequest{Icon: &emoji})
	require.NoError(t, err)
	require.NoError(t, db.Get(&stored, `SELECT icon FROM teams WHERE id=$1`, team))
	assert.Equal(t, emoji, stored)
	assert.Empty(t, store.files)
}

// Concurrent icon changes each delete exactly the file they replaced: when they
// finish only the file the team points at is left.
func TestTeamServiceConcurrentIconUploadsPostgres(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := sqlx.Connect("pgx", url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	exec := func(query string, args ...any) {
		t.Helper()
		_, err := db.Exec(query, args...)
		require.NoError(t, err)
	}
	user, workspace, team := uuid.New(), uuid.New(), uuid.New()
	exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Team icon race','hash')`, user, user.String()+"@example.test")
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id=$1`, user) })
	exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Team icon race',$2,$3)`, workspace, workspace.String(), user)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM workspaces WHERE id=$1`, workspace) })
	exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'owner')`, workspace, user)
	exec(`INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Race','RCE')`, team, workspace)

	svc := NewTeamService(repository.NewTeamRepository(db), repository.NewTeamStatusRepository(db))
	store := &iconStore{files: map[string][]byte{}}
	svc.SetIconStore(store)
	ctx := domain.WithAccess(context.Background(), domain.Access{UserID: user, WorkspaceID: workspace})

	for round := 0; round < 5; round++ {
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := svc.UploadIcon(ctx, workspace, team, strings.NewReader("png"), "image/png", ".png")
				assert.NoError(t, err)
			}()
		}
		wg.Wait()

		var current string
		require.NoError(t, db.Get(&current, `SELECT icon FROM teams WHERE id=$1`, team))
		file, ok := domain.TeamIconImageFile(&current)
		require.True(t, ok)
		store.mu.Lock()
		require.Len(t, store.files, 1, "round %d: replaced files are removed", round)
		_, kept := store.files[teamIconKey(team, file)]
		store.mu.Unlock()
		require.True(t, kept, "round %d: the current icon's file is kept", round)
	}
}

// An update of other fields that overlaps an upload must not restore the icon
// it read earlier: the team keeps pointing at a file that exists.
func TestTeamServiceNameUpdateKeepsConcurrentIconPostgres(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := sqlx.Connect("pgx", url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	exec := func(query string, args ...any) {
		t.Helper()
		_, err := db.Exec(query, args...)
		require.NoError(t, err)
	}
	user, workspace, team := uuid.New(), uuid.New(), uuid.New()
	exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Team icon rename','hash')`, user, user.String()+"@example.test")
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id=$1`, user) })
	exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Team icon rename',$2,$3)`, workspace, workspace.String(), user)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM workspaces WHERE id=$1`, workspace) })
	exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'owner')`, workspace, user)
	exec(`INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Rename','RNM')`, team, workspace)

	svc := NewTeamService(repository.NewTeamRepository(db), repository.NewTeamStatusRepository(db))
	store := &iconStore{files: map[string][]byte{}}
	svc.SetIconStore(store)
	ctx := domain.WithAccess(context.Background(), domain.Access{UserID: user, WorkspaceID: workspace})

	for round := 0; round < 10; round++ {
		var wg sync.WaitGroup
		for i := 0; i < 6; i++ {
			wg.Add(2)
			go func() {
				defer wg.Done()
				_, err := svc.UploadIcon(ctx, workspace, team, strings.NewReader("png"), "image/png", ".png")
				assert.NoError(t, err)
			}()
			go func() {
				defer wg.Done()
				name := "Renamed " + uuid.NewString()[:8]
				updated, err := svc.Update(ctx, team, dto.UpdateTeamRequest{Name: &name})
				assert.NoError(t, err)
				if err == nil {
					assert.Equal(t, name, updated.Name)
				}
			}()
		}
		wg.Wait()

		var current string
		require.NoError(t, db.Get(&current, `SELECT icon FROM teams WHERE id=$1`, team))
		file, ok := domain.TeamIconImageFile(&current)
		require.True(t, ok)
		store.mu.Lock()
		_, kept := store.files[teamIconKey(team, file)]
		count := len(store.files)
		store.mu.Unlock()
		require.True(t, kept, "round %d: the icon the team points at still exists", round)
		require.Equal(t, 1, count, "round %d: no orphaned files", round)
	}
}

// A delete that overlaps an upload removes the image the row had when it was
// deleted, not the one read earlier.
func TestTeamServiceDeleteRemovesConcurrentIconPostgres(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := sqlx.Connect("pgx", url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	exec := func(query string, args ...any) {
		t.Helper()
		_, err := db.Exec(query, args...)
		require.NoError(t, err)
	}
	user, workspace := uuid.New(), uuid.New()
	exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Team icon delete','hash')`, user, user.String()+"@example.test")
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id=$1`, user) })
	exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Team icon delete',$2,$3)`, workspace, workspace.String(), user)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM workspaces WHERE id=$1`, workspace) })
	exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'owner')`, workspace, user)

	svc := NewTeamService(repository.NewTeamRepository(db), repository.NewTeamStatusRepository(db))
	store := &iconStore{files: map[string][]byte{}}
	svc.SetIconStore(store)
	ctx := domain.WithAccess(context.Background(), domain.Access{UserID: user, WorkspaceID: workspace})

	for round := 0; round < 10; round++ {
		team := uuid.New()
		exec(`INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Delete','DEL')`, team, workspace)
		_, err := svc.UploadIcon(ctx, workspace, team, strings.NewReader("png"), "image/png", ".png")
		require.NoError(t, err)

		var wg sync.WaitGroup
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				// Either outcome is fine: the team may already be gone.
				_, _ = svc.UploadIcon(ctx, workspace, team, strings.NewReader("png"), "image/png", ".png")
			}()
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			assert.NoError(t, svc.Delete(ctx, workspace, team))
		}()
		wg.Wait()

		var count int
		require.NoError(t, db.Get(&count, `SELECT COUNT(*) FROM teams WHERE id=$1`, team))
		require.Zero(t, count)
		store.mu.Lock()
		left := len(store.files)
		store.mu.Unlock()
		// An upload that lost the race and stored a file for a deleted team cleans itself up.
		require.Zero(t, left, "round %d: no image is left for the deleted team", round)
	}
}

// storeHook runs a callback while an upload is storing its file, the window in
// which another request can change the team.
type storeHook struct {
	*iconStore
	onPut func()
}

func (h *storeHook) Put(ctx context.Context, key string, r io.Reader, contentType string) (int64, error) {
	if h.onPut != nil {
		h.onPut()
	}
	return h.iconStore.Put(ctx, key, r, contentType)
}

// An upload writes only the icon: a rename saved while the file is stored stays.
func TestTeamServiceIconUploadKeepsConcurrentRenamePostgres(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := sqlx.Connect("pgx", url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	exec := func(query string, args ...any) {
		t.Helper()
		_, err := db.Exec(query, args...)
		require.NoError(t, err)
	}
	user, workspace, team := uuid.New(), uuid.New(), uuid.New()
	exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Team icon keep','hash')`, user, user.String()+"@example.test")
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id=$1`, user) })
	exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Team icon keep',$2,$3)`, workspace, workspace.String(), user)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM workspaces WHERE id=$1`, workspace) })
	exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'owner')`, workspace, user)
	exec(`INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Before','KEP')`, team, workspace)

	svc := NewTeamService(repository.NewTeamRepository(db), repository.NewTeamStatusRepository(db))
	ctx := domain.WithAccess(context.Background(), domain.Access{UserID: user, WorkspaceID: workspace})
	store := &storeHook{iconStore: &iconStore{files: map[string][]byte{}}}
	svc.SetIconStore(store)
	store.onPut = func() {
		name := "Renamed"
		_, err := svc.Update(ctx, team, dto.UpdateTeamRequest{Name: &name})
		assert.NoError(t, err)
	}

	_, err = svc.UploadIcon(ctx, workspace, team, strings.NewReader("png"), "image/png", ".png")
	require.NoError(t, err)
	var name, icon string
	require.NoError(t, db.QueryRow(`SELECT name, icon FROM teams WHERE id=$1`, team).Scan(&name, &icon))
	assert.Equal(t, "Renamed", name, "the upload did not revert the rename")
	_, ok := domain.TeamIconImageFile(&icon)
	assert.True(t, ok)
}

// Deleting a workspace removes the icon images of all its teams, including one
// an upload stores while the delete runs, and leaves other workspaces' alone.
func TestWorkspaceDeleteRemovesTeamIconsPostgres(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := sqlx.Connect("pgx", url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	exec := func(query string, args ...any) {
		t.Helper()
		_, err := db.Exec(query, args...)
		require.NoError(t, err)
	}
	user := uuid.New()
	exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Workspace icon delete','hash')`, user, user.String()+"@example.test")
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id=$1`, user) })
	workspaces := repository.NewWorkspaceRepository(db)
	teams := NewTeamService(repository.NewTeamRepository(db), repository.NewTeamStatusRepository(db))
	store := &iconStore{files: map[string][]byte{}}
	teams.SetIconStore(store)
	svc := NewWorkspaceService(workspaces, repository.NewUserRepository(db))
	svc.SetTeamIconStore(store)
	newWorkspace := func() (uuid.UUID, []uuid.UUID, context.Context) {
		t.Helper()
		id := uuid.New()
		exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Workspace icon delete',$2,$3)`, id, id.String(), user)
		t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM workspaces WHERE id=$1`, id) })
		exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'owner')`, id, user)
		ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
		for i, team := range ids {
			exec(`INSERT INTO teams(id,workspace_id,name,key,icon,is_private) VALUES($1,$2,'Icon',$3,$4,$5)`, team, id, "WI"+string(rune('A'+i)), "emoji:🚀", i == 1)
		}
		return id, ids, domain.WithAccess(context.Background(), domain.Access{UserID: user, WorkspaceID: id})
	}

	kept, keptTeams, keptCtx := newWorkspace()
	_, err = teams.UploadIcon(keptCtx, kept, keptTeams[0], strings.NewReader("png"), "image/png", ".png")
	require.NoError(t, err)
	store.mu.Lock()
	keptFiles := len(store.files)
	store.mu.Unlock()
	require.Equal(t, 1, keptFiles)

	for round := 0; round < 10; round++ {
		ws, ids, ctx := newWorkspace()
		// Two teams (one private) have an image; the third keeps its emoji.
		for _, team := range ids[:2] {
			_, err := teams.UploadIcon(ctx, ws, team, strings.NewReader("png"), "image/png", ".png")
			require.NoError(t, err)
		}
		var wg sync.WaitGroup
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				// Either outcome is fine: the workspace may already be gone.
				_, _ = teams.UploadIcon(ctx, ws, ids[i%3], strings.NewReader("png"), "image/png", ".png")
			}()
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			assert.NoError(t, svc.Delete(context.Background(), ws.String(), user))
		}()
		wg.Wait()

		store.mu.Lock()
		left := len(store.files)
		store.mu.Unlock()
		require.Equal(t, keptFiles, left, "round %d: only the other workspace's icon is left", round)
	}
}
