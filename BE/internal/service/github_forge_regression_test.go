package service

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

// These tests pin the GitHub behaviour that the forge abstraction must keep:
// linking, status automation, comments and the ignore rules. They run against
// a disposable PostgreSQL database (DATABASE_URL), like the other integration
// tests, because the webhook processors write through the concrete repository.

type regressionBot struct {
	calls     int
	abilities map[string]bool
}

func (b *regressionBot) Get(_ context.Context, ws uuid.UUID) (*domain.GitHubBotSettings, error) {
	b.calls++
	return &domain.GitHubBotSettings{WorkspaceID: ws, Abilities: b.abilities}, nil
}

type githubRegression struct {
	db       *sqlx.DB
	svc      *GitHubService
	repo     *repository.GitHubRepository
	ws       uuid.UUID
	team     uuid.UUID
	statuses map[string]uuid.UUID
	install  int64
	repoID   int64
}

func newGitHubRegression(t *testing.T) *githubRegression {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := repository.Open(dsn)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	owner, ws, team := uuid.New(), uuid.New(), uuid.New()
	_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Forge regression','hash')`, owner, owner.String()+"@test.invalid")
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id=$1`, owner) })
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Forge regression',$2,$3)`, ws, ws.String(), owner)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM workspaces WHERE id=$1`, ws) })
	_, err = db.Exec(`INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Engineering','ENG')`, team, ws)
	require.NoError(t, err)
	for i, status := range []struct{ slug, category string }{
		{"backlog", "backlog"}, {"todo", "unstarted"}, {"in_progress", "started"},
		{"in_review", "started"}, {"done", "completed"}, {"cancelled", "cancelled"},
	} {
		_, err = db.Exec(`INSERT INTO team_statuses(team_id,name,slug,category,position,is_default) VALUES($1,$2,$3,$4,$5,$6)`,
			team, status.slug, status.slug, status.category, i, i == 0)
		require.NoError(t, err)
	}
	statuses := map[string]uuid.UUID{}
	for _, slug := range []string{"backlog", "todo", "in_progress", "in_review", "done"} {
		var id uuid.UUID
		require.NoError(t, db.Get(&id, `SELECT id FROM team_statuses WHERE team_id=$1 AND slug=$2`, team, slug))
		statuses[slug] = id
	}
	repo := repository.NewGitHubRepository(db)
	install := time.Now().UnixNano()
	repoID := install + 1
	require.NoError(t, repo.CreateInstallation(ctx, &domain.GitHubInstallation{
		ID: uuid.New(), WorkspaceID: ws, InstallationID: install,
		AccountLogin: "acme", AccountType: "Organization", InstalledBy: owner,
	}))
	inst, err := repo.GetInstallationByGitHubID(ctx, install)
	require.NoError(t, err)
	require.NoError(t, repo.CreateRepo(ctx, &domain.GitHubRepoModel{
		ID: uuid.New(), WorkspaceID: ws, InstallationID: inst.ID,
		GitHubRepoID: repoID, FullName: "acme/app", DefaultBranch: "main", IsActive: true,
	}))

	svc := NewGitHubService(repo, repository.NewIssueRepository(db), repository.NewTeamRepository(db),
		repository.NewTeamStatusRepository(db), repository.NewIssueHistoryRepository(db), nil, nil,
		"https://kuayle.test", "", nil)
	return &githubRegression{db: db, svc: svc, repo: repo, ws: ws, team: team, statuses: statuses, install: install, repoID: repoID}
}

func (f *githubRegression) addIssue(t *testing.T, number int, identifier, title string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := f.db.Exec(`INSERT INTO issues(id,workspace_id,team_id,number,identifier_text,title,creator_id,status,status_id)
		VALUES($1,$2,$3,$4,$5,$6,(SELECT owner_id FROM workspaces WHERE id=$2),'todo',$7)`,
		id, f.ws, f.team, number, identifier, title, f.statuses["todo"])
	require.NoError(t, err)
	return id
}

func (f *githubRegression) setRules(t *testing.T) {
	t.Helper()
	for event, slug := range map[string]string{
		"branch_created": "in_progress", "pr_opened": "in_review", "pr_merged": "done",
	} {
		id := f.statuses[slug]
		_, err := f.db.Exec(`INSERT INTO github_auto_transitions(id,workspace_id,event,target_status,target_status_id,is_active)
			VALUES($1,$2,$3,$4,$5,true) ON CONFLICT (workspace_id,event) DO UPDATE SET target_status=EXCLUDED.target_status,target_status_id=EXCLUDED.target_status_id,is_active=true`,
			uuid.New(), f.ws, event, slug, id)
		require.NoError(t, err)
	}
}

func (f *githubRegression) disableComment(t *testing.T) *regressionBot {
	t.Helper()
	bot := &regressionBot{abilities: map[string]bool{"link_issues": false}}
	f.svc.SetBotSettings(bot)
	return bot
}

func (f *githubRegression) prEvent(action string, merged bool, number int, title, head, body string) []byte {
	payload, _ := json.Marshal(map[string]any{
		"action": action,
		"number": number,
		"pull_request": map[string]any{
			"id": number + 1000, "number": number, "title": title, "body": body,
			"state": map[bool]string{true: "closed", false: "open"}[merged || action == "closed"],
			"draft": false, "merged": merged, "html_url": "https://github.com/acme/app/pull/" + itoa(number),
			"head": map[string]any{"ref": head}, "base": map[string]any{"ref": "main"},
			"user":      map[string]any{"login": "alice", "avatar_url": "https://avatars.example.com/a"},
			"merged_at": map[bool]any{true: "2026-01-02T03:04:05Z", false: nil}[merged],
			"closed_at": map[bool]any{true: "2026-01-02T03:04:05Z", false: nil}[merged || action == "closed"],
		},
		"repository":   map[string]any{"id": f.repoID},
		"installation": map[string]any{"id": f.install},
	})
	return payload
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

func (f *githubRegression) issueStatus(t *testing.T, issueID uuid.UUID) string {
	t.Helper()
	var status string
	require.NoError(t, f.db.Get(&status, `SELECT status FROM issues WHERE id=$1`, issueID))
	return status
}

func TestGitHubRegressionLinksBranchAndTitle(t *testing.T) {
	f := newGitHubRegression(t)
	ctx := context.Background()
	f.setRules(t)
	issue := f.addIssue(t, 1, "KUA-1", "Fix")
	f.disableComment(t)
	require.NoError(t, f.svc.HandleWebhookEvent(ctx, "pull_request", f.prEvent("opened", false, 1, "KUA-1 fix", "feat/kua-1-x", "Closes KUA-1")))
	var pr domain.GitHubPullRequest
	require.NoError(t, f.db.Get(&pr, `SELECT * FROM github_pull_requests WHERE number=1 AND workspace_id=$1`, f.ws))
	require.NotNil(t, pr.IssueID)
	require.Equal(t, issue, *pr.IssueID)
	require.Equal(t, "in_review", f.issueStatus(t, issue))
}

func TestGitHubRegressionAmbiguousAndNonIssueReferencesDoNotLink(t *testing.T) {
	f := newGitHubRegression(t)
	ctx := context.Background()
	f.setRules(t)
	f.addIssue(t, 1, "KUA-1", "One")
	f.addIssue(t, 2, "KUA-2", "Two")
	f.disableComment(t)
	require.NoError(t, f.svc.HandleWebhookEvent(ctx, "pull_request", f.prEvent("opened", false, 1, "KUA-1 and KUA-2", "feat/two", "")))
	var pr domain.GitHubPullRequest
	require.NoError(t, f.db.Get(&pr, `SELECT * FROM github_pull_requests WHERE number=1 AND workspace_id=$1`, f.ws))
	require.Nil(t, pr.IssueID)

	require.NoError(t, f.svc.HandleWebhookEvent(ctx, "pull_request", f.prEvent("opened", false, 2, "SHA-256 hardening", "feat/sha", "no issue here")))
	require.NoError(t, f.db.Get(&pr, `SELECT * FROM github_pull_requests WHERE number=2 AND workspace_id=$1`, f.ws))
	require.Nil(t, pr.IssueID)
}

func TestGitHubRegressionStatusAutomation(t *testing.T) {
	f := newGitHubRegression(t)
	ctx := context.Background()
	f.setRules(t)
	issue := f.addIssue(t, 1, "KUA-1", "Fix")
	f.disableComment(t)

	// A newly inserted opened event moves to In Review.
	require.NoError(t, f.svc.HandleWebhookEvent(ctx, "pull_request", f.prEvent("opened", false, 1, "KUA-1 fix", "feat/kua-1", "")))
	require.Equal(t, "in_review", f.issueStatus(t, issue))

	// A repeated opened event does not transition again, even after the issue
	// was moved by a person.
	_, err := f.db.Exec(`UPDATE issues SET status='todo' WHERE id=$1`, issue)
	require.NoError(t, err)
	require.NoError(t, f.svc.HandleWebhookEvent(ctx, "pull_request", f.prEvent("opened", false, 1, "KUA-1 fix", "feat/kua-1", "")))
	require.Equal(t, "todo", f.issueStatus(t, issue))

	// ready_for_review moves to In Review.
	require.NoError(t, f.svc.HandleWebhookEvent(ctx, "pull_request", f.prEvent("ready_for_review", false, 1, "KUA-1 fix", "feat/kua-1", "")))
	require.Equal(t, "in_review", f.issueStatus(t, issue))

	// A close without merge changes nothing.
	require.NoError(t, f.svc.HandleWebhookEvent(ctx, "pull_request", f.prEvent("closed", false, 2, "KUA-1 other", "feat/kua-1", "")))
	require.Equal(t, "in_review", f.issueStatus(t, issue))

	// A merged close moves to Done.
	require.NoError(t, f.svc.HandleWebhookEvent(ctx, "pull_request", f.prEvent("closed", true, 3, "KUA-1 merged", "feat/kua-1", "")))
	require.Equal(t, "done", f.issueStatus(t, issue))
}

func TestGitHubRegressionLinksCommitsAndBranches(t *testing.T) {
	f := newGitHubRegression(t)
	ctx := context.Background()
	f.setRules(t)
	issue := f.addIssue(t, 1, "KUA-1", "Fix")
	f.disableComment(t)

	push, _ := json.Marshal(map[string]any{
		"ref": "refs/heads/main",
		"commits": []map[string]any{{
			"id": "abc123", "message": "KUA-1 fix the thing", "url": "https://github.com/acme/app/commit/abc123",
			"timestamp": "2026-01-02T03:04:05Z", "author": map[string]any{"username": "alice"},
		}},
		"repository":   map[string]any{"id": f.repoID},
		"installation": map[string]any{"id": f.install},
	})
	require.NoError(t, f.svc.HandleWebhookEvent(ctx, "push", push))
	var commit domain.GitHubCommit
	require.NoError(t, f.db.Get(&commit, `SELECT * FROM github_commits WHERE sha='abc123' AND workspace_id=$1`, f.ws))
	require.NotNil(t, commit.IssueID)
	require.Equal(t, issue, *commit.IssueID)

	create, _ := json.Marshal(map[string]any{
		"ref_type": "branch", "ref": "feat/kua-1-x",
		"repository":   map[string]any{"id": f.repoID, "html_url": "https://github.com/acme/app"},
		"installation": map[string]any{"id": f.install},
	})
	require.NoError(t, f.svc.HandleWebhookEvent(ctx, "create", create))
	var branch domain.GitHubBranch
	require.NoError(t, f.db.Get(&branch, `SELECT * FROM github_branches WHERE name='feat/kua-1-x' AND workspace_id=$1`, f.ws))
	require.NotNil(t, branch.IssueID)
	require.Equal(t, "in_progress", f.issueStatus(t, issue))
}

func TestGitHubRegressionIgnoresUnknownRepositories(t *testing.T) {
	f := newGitHubRegression(t)
	ctx := context.Background()
	f.setRules(t)
	f.addIssue(t, 1, "KUA-1", "Fix")
	f.disableComment(t)

	unknownInstall, _ := json.Marshal(map[string]any{
		"action": "opened", "number": 1,
		"pull_request": map[string]any{"id": 1, "number": 1, "title": "KUA-1", "state": "open", "head": map[string]any{"ref": "feat/kua-1"}, "base": map[string]any{"ref": "main"}, "user": map[string]any{"login": "alice"}},
		"repository":   map[string]any{"id": f.repoID},
		"installation": map[string]any{"id": f.install + 99},
	})
	require.NoError(t, f.svc.HandleWebhookEvent(ctx, "pull_request", unknownInstall))

	_, err := f.db.Exec(`UPDATE github_repos SET is_active=false WHERE workspace_id=$1`, f.ws)
	require.NoError(t, err)
	require.NoError(t, f.svc.HandleWebhookEvent(ctx, "pull_request", f.prEvent("opened", false, 2, "KUA-1", "feat/kua-1", "")))

	var count int
	require.NoError(t, f.db.Get(&count, `SELECT count(*) FROM github_pull_requests WHERE workspace_id=$1`, f.ws))
	require.Zero(t, count)
}

func TestGitHubRegressionCommentsOnlyOnOpened(t *testing.T) {
	f := newGitHubRegression(t)
	ctx := context.Background()
	f.setRules(t)
	f.addIssue(t, 1, "KUA-1", "Fix")
	bot := &regressionBot{abilities: map[string]bool{"link_issues": true}}
	f.svc.SetBotSettings(bot)

	// With no GitHub App configured the comment attempt fails, which proves it
	// ran only for the opened event.
	require.Error(t, f.svc.HandleWebhookEvent(ctx, "pull_request", f.prEvent("opened", false, 1, "KUA-1", "feat/kua-1", "")))
	require.NoError(t, f.svc.HandleWebhookEvent(ctx, "pull_request", f.prEvent("closed", false, 1, "KUA-1", "feat/kua-1", "")))
	require.GreaterOrEqual(t, bot.calls, 2)
}
