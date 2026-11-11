package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

// fakeMemoryStore holds roles and the approved texts per team; it records
// which decisions reached it.
type fakeMemoryStore struct {
	workspaceMemoryStore
	roles      map[uuid.UUID]string
	approved   map[uuid.UUID][]string
	repository string
	decisions  []string
	asked      []string
}

func (f *fakeMemoryStore) MemberRole(_ context.Context, _, actor uuid.UUID) (string, error) {
	return f.roles[actor], nil
}

func (f *fakeMemoryStore) List(context.Context, uuid.UUID, uuid.UUID, string, string) ([]domain.Lesson, error) {
	return []domain.Lesson{{Text: "Listed"}}, nil
}

func (f *fakeMemoryStore) Add(_ context.Context, _, _ uuid.UUID, _, text string) (*domain.Lesson, error) {
	f.decisions = append(f.decisions, "add")
	return &domain.Lesson{Text: text}, nil
}

func (f *fakeMemoryStore) decide(name string) (*domain.Lesson, error) {
	f.decisions = append(f.decisions, name)
	return &domain.Lesson{}, nil
}

func (f *fakeMemoryStore) Approve(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, *string) (*domain.Lesson, error) {
	return f.decide("approve")
}

func (f *fakeMemoryStore) Edit(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, *string, bool) (*domain.Lesson, error) {
	return f.decide("edit")
}

func (f *fakeMemoryStore) Retire(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (*domain.Lesson, error) {
	return f.decide("retire")
}

func (f *fakeMemoryStore) Drop(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (*domain.Lesson, error) {
	return f.decide("drop")
}

func (f *fakeMemoryStore) Reorder(context.Context, uuid.UUID, uuid.UUID, string, []uuid.UUID) ([]domain.Lesson, error) {
	f.decisions = append(f.decisions, "reorder")
	return nil, nil
}

func (f *fakeMemoryStore) Events(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) ([]domain.LessonEvent, error) {
	f.decisions = append(f.decisions, "audit")
	return nil, nil
}

func (f *fakeMemoryStore) ApprovedTexts(_ context.Context, _, team uuid.UUID, repo string) ([]string, error) {
	f.asked = append(f.asked, repo)
	return append([]string(nil), f.approved[team]...), nil
}

func (f *fakeMemoryStore) TaskRepository(context.Context, domain.AgentTask) (string, error) {
	return f.repository, nil
}

func TestOnlyOwnersAndAdminsDecideLessons(t *testing.T) {
	ws := uuid.New()
	owner, admin, member, guest, outsider := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	store := &fakeMemoryStore{roles: map[uuid.UUID]string{owner: domain.RoleOwner, admin: domain.RoleAdmin, member: domain.RoleMember, guest: domain.RoleGuest}}
	svc := NewWorkspaceMemoryService(store, nil)
	as := func(user uuid.UUID) context.Context {
		return domain.WithAccess(context.Background(), domain.Access{WorkspaceID: ws, UserID: user})
	}
	text := "Lesson"
	decisions := map[string]func(ctx context.Context) error{
		"approve": func(ctx context.Context) error { _, err := svc.Approve(ctx, ws, uuid.New(), &text); return err },
		"edit":    func(ctx context.Context) error { _, err := svc.Edit(ctx, ws, uuid.New(), &text, false); return err },
		"retire":  func(ctx context.Context) error { _, err := svc.Retire(ctx, ws, uuid.New()); return err },
		"drop":    func(ctx context.Context) error { _, err := svc.Drop(ctx, ws, uuid.New()); return err },
		"reorder": func(ctx context.Context) error { _, err := svc.Reorder(ctx, ws, "acme/app", nil); return err },
		"audit":   func(ctx context.Context) error { _, err := svc.Audit(ctx, ws, uuid.New()); return err },
	}
	for name, decide := range decisions {
		for _, manager := range []uuid.UUID{owner, admin} {
			store.decisions = nil
			require.NoError(t, decide(as(manager)), name)
			require.Equal(t, []string{name}, store.decisions)
		}
		store.decisions = nil
		require.ErrorIs(t, decide(as(member)), repository.ErrLessonDenied, name)
		require.ErrorIs(t, decide(as(guest)), ErrMemoryDenied, name)
		require.ErrorIs(t, decide(as(outsider)), ErrMemoryDenied, name)
		// A session for another workspace, or none, decides nothing here.
		require.ErrorIs(t, decide(domain.WithAccess(context.Background(), domain.Access{WorkspaceID: uuid.New(), UserID: owner})), ErrMemoryDenied, name)
		require.ErrorIs(t, decide(context.Background()), ErrMemoryDenied, name)
		require.Empty(t, store.decisions, name)
	}

	// Members read and propose; guests and outsiders do neither.
	list, err := svc.List(as(member), ws, "acme/app", "")
	require.NoError(t, err)
	require.False(t, list.CanManage)
	require.Len(t, list.Lessons, 1)
	require.Equal(t, LessonLimits{LessonCharacters: domain.LessonMaxRunes, ApprovedPerRepository: domain.LessonsApprovedPerRepository, PromptBytes: domain.LessonsPromptMaxBytes}, list.Limits)
	list, err = svc.List(as(admin), ws, "", domain.LessonProposed)
	require.NoError(t, err)
	require.True(t, list.CanManage)
	_, err = svc.List(as(member), ws, "", "pending")
	require.ErrorIs(t, err, repository.ErrLessonState)
	_, err = svc.Add(as(member), ws, "acme/app", "Mine")
	require.NoError(t, err)
	for _, user := range []uuid.UUID{guest, outsider} {
		_, err = svc.List(as(user), ws, "", "")
		require.ErrorIs(t, err, ErrMemoryDenied)
		_, err = svc.Add(as(user), ws, "acme/app", "Mine")
		require.ErrorIs(t, err, ErrMemoryDenied)
	}
}

func TestApprovedLessonsAreAPlainBoundedRedactedBlock(t *testing.T) {
	ws, team, otherTeam, machine := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	store := &fakeMemoryStore{repository: "acme/app", approved: map[uuid.UUID][]string{team: {
		"Run `make check` before pushing.",
		"Deploy with the token hunter2-deploy-token, never by hand.",
		"The bot pushes as ghp_" + strings.Repeat("a", 36) + " over https://bot:s3cr3tpass@github.com/acme/app.git",
		"Renumber migrations after merging main.",
	}}}
	var askedMachine uuid.UUID
	svc := NewWorkspaceMemoryService(store, func(_ context.Context, workspace, id uuid.UUID) ([]string, error) {
		require.Equal(t, ws, workspace)
		askedMachine = id
		return []string{"hunter2-deploy-token"}, nil
	})
	task := domain.AgentTask{WorkspaceID: ws, TeamID: team, MachineID: &machine, ReservedMachineID: uuid.New()}

	block, err := svc.ApprovedLessons(context.Background(), task)
	require.NoError(t, err)
	require.Equal(t, machine, askedMachine, "the task's machine names the secrets to redact")
	require.Equal(t, []string{"acme/app"}, store.asked)
	lines := strings.Split(block, "\n")
	require.Len(t, lines, 5, "a heading and one line per lesson, in order")
	require.Equal(t, "- Run `make check` before pushing.", lines[1])
	require.Equal(t, "- Deploy with the token [REDACTED], never by hand.", lines[2])
	require.Equal(t, "- The bot pushes as [REDACTED] over https://bot:[REDACTED]@github.com/acme/app.git", lines[3])
	require.Equal(t, "- Renumber migrations after merging main.", lines[4])
	for _, secret := range []string{"hunter2", "ghp_", "s3cr3tpass"} {
		require.NotContains(t, block, secret)
	}

	// Without a machine yet the reserved one is asked; credential formats are
	// redacted even when no secret source is configured.
	task.MachineID = nil
	_, err = svc.ApprovedLessons(context.Background(), task)
	require.NoError(t, err)
	require.Equal(t, task.ReservedMachineID, askedMachine)
	block, err = NewWorkspaceMemoryService(store, nil).ApprovedLessons(context.Background(), task)
	require.NoError(t, err)
	require.NotContains(t, block, "ghp_")
	require.Contains(t, block, "hunter2-deploy-token", "unknown values cannot be recognised without the machine's secrets")

	// Nothing approved, another team or no checkout: nothing for the prompt.
	other := task
	other.TeamID = otherTeam
	block, err = svc.ApprovedLessons(context.Background(), other)
	require.NoError(t, err)
	require.Equal(t, "", block)
	store.repository = ""
	block, err = svc.ApprovedLessons(context.Background(), task)
	require.NoError(t, err)
	require.Equal(t, "", block)
	store.repository = "acme/app"

	// The block stays within the prompt budget, whole lessons in order.
	long := []string{}
	for range domain.LessonsApprovedPerRepository {
		long = append(long, strings.Repeat("y", domain.LessonMaxRunes))
	}
	store.approved[team] = long
	block, err = svc.ApprovedLessonsForRepository(context.Background(), ws, team, "acme/app", uuid.Nil)
	require.NoError(t, err)
	require.LessOrEqual(t, len(block), domain.LessonsPromptMaxBytes)
	require.Greater(t, strings.Count(block, "\n"), 3)

	// A failing secret source fails closed: no unredacted lessons.
	failing := NewWorkspaceMemoryService(store, func(context.Context, uuid.UUID, uuid.UUID) ([]string, error) {
		return nil, errors.New("vault unavailable")
	})
	block, err = failing.ApprovedLessons(context.Background(), task)
	require.Error(t, err)
	require.Equal(t, "", block)
}
