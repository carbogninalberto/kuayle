package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

type mentionRecorder struct{ requests []MentionReview }

func (r *mentionRecorder) ReviewMention(_ context.Context, request MentionReview) error {
	r.requests = append(r.requests, request)
	return nil
}

// An author association says how someone relates to the repository, not what
// they may do there: an outside collaborator or an organization member can be
// limited to read access. Mentions and review feedback only steer an agent
// when GitHub confirms the author can push.
func TestMentionAndReviewTriggersRequireWritePermissionPostgres(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := repository.Open(dsn)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	owner, ws := uuid.New(), uuid.New()
	_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Write access test','hash')`, owner, owner.String()+"@test.invalid")
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id=$1`, owner) })
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Write access test',$2,$3)`, ws, ws.String(), owner)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM workspaces WHERE id=$1`, ws) })
	repo := repository.NewGitHubRepository(db)
	inst := &domain.GitHubInstallation{ID: uuid.New(), WorkspaceID: ws, InstallationID: time.Now().UnixNano(), AccountLogin: "acme", AccountType: "Organization", InstalledBy: owner}
	require.NoError(t, repo.CreateInstallation(ctx, inst))
	linked := &domain.GitHubRepoModel{ID: uuid.New(), WorkspaceID: ws, InstallationID: inst.ID, GitHubRepoID: time.Now().UnixNano(), FullName: "acme/app", DefaultBranch: "main", IsActive: true}
	require.NoError(t, repo.CreateRepo(ctx, linked))

	bot := newBotStoreFake()
	bot.settings[ws] = &domain.GitHubBotSettings{WorkspaceID: ws, Abilities: map[string]bool{string(domain.GitHubBotMentionReviews): true}}
	signals, mentions := &signalRecorder{}, &mentionRecorder{}
	svc := NewGitHubService(repo, repository.NewIssueRepository(db), nil, nil, nil, nil, nil, "https://kuayle.test", "", &GlobalGitHubAppConfig{Slug: "kuayle-acme"})
	svc.SetBotSettings(bot)
	svc.SetTaskSignals(signals)
	svc.SetMentionReviews(mentions)
	permissions := map[string]string{"reader": "read", "triager": "read", "writer": "write", "maintainer": "maintain", "admin": "admin", "stranger": "none"}
	var asked []string
	lookupErr := errors.New("GitHub is unavailable")
	svc.repositoryPermission = func(_ context.Context, gotInst *domain.GitHubInstallation, gotRepo *domain.GitHubRepoModel, login string) (string, error) {
		require.Equal(t, inst.InstallationID, gotInst.InstallationID)
		require.Equal(t, "acme/app", gotRepo.FullName, "the repository comes from Kuayle's link, not the payload")
		asked = append(asked, login)
		if login == "outage" {
			return "", lookupErr
		}
		return permissions[login], nil
	}
	var nextID int64
	deliver := func(kind string, body map[string]any) error {
		t.Helper()
		body["repository"] = map[string]any{"id": linked.GitHubRepoID, "full_name": "attacker/other"}
		body["installation"] = map[string]any{"id": inst.InstallationID}
		payload, err := json.Marshal(body)
		require.NoError(t, err)
		return svc.HandleWebhookEvent(ctx, kind, payload)
	}
	mention := func(association, login string) error {
		nextID++
		return deliver("issue_comment", map[string]any{
			"action":  "created",
			"issue":   map[string]any{"number": 12, "pull_request": map[string]any{"url": "https://api.github.com/repos/acme/app/pulls/12"}},
			"comment": map[string]any{"id": nextID, "body": "@kuayle-acme please review", "author_association": association, "user": map[string]any{"login": login, "type": "User"}},
		})
	}
	review := func(association, login, state string) error {
		nextID++
		return deliver("pull_request_review", map[string]any{
			"action": "submitted", "pull_request": map[string]any{"number": 12},
			"review": map[string]any{"id": nextID, "state": state, "body": "Rename it", "author_association": association, "user": map[string]any{"login": login}},
		})
	}

	for _, c := range []struct {
		association, login string
		trusted, lookedUp  bool
	}{
		{"COLLABORATOR", "reader", false, true},
		{"COLLABORATOR", "writer", true, true},
		{"COLLABORATOR", "maintainer", true, true},
		{"MEMBER", "triager", false, true},
		{"MEMBER", "admin", true, true},
		{"COLLABORATOR", "stranger", false, true},
		{"OWNER", "boss", true, false},
		{"CONTRIBUTOR", "writer", false, false},
		{"NONE", "admin", false, false},
	} {
		name := c.association + " " + c.login
		asked, mentions.requests, signals.inputs = nil, nil, nil
		require.NoError(t, mention(c.association, c.login), name)
		require.Equal(t, c.trusted, len(mentions.requests) == 1, "mention review for %s", name)
		require.Equal(t, c.lookedUp, len(asked) == 1, "permission lookup on mention for %s: %v", name, asked)

		asked = nil
		require.NoError(t, review(c.association, c.login, "changes_requested"), name)
		require.Len(t, signals.inputs, 1, name)
		require.Equal(t, c.trusted, signals.inputs[0].Trusted, "review feedback for %s", name)
		require.Equal(t, c.lookedUp, len(asked) == 1, "permission lookup on review for %s: %v", name, asked)
		if c.lookedUp {
			require.Equal(t, []string{c.login}, asked)
		}
	}

	// A lookup that fails acts on nothing and fails the delivery, which GitHub
	// can redeliver once the lookup works again.
	asked, mentions.requests, signals.inputs = nil, nil, nil
	require.ErrorIs(t, mention("COLLABORATOR", "outage"), lookupErr)
	require.ErrorIs(t, review("MEMBER", "outage", "changes_requested"), lookupErr)
	require.Empty(t, mentions.requests)
	require.Empty(t, signals.inputs)

	// Only a changes_requested review can steer the agent, so other reviews
	// are recorded without asking GitHub.
	asked = nil
	require.NoError(t, review("COLLABORATOR", "outage", "approved"))
	require.Empty(t, asked)
	require.Len(t, signals.inputs, 1)
	require.False(t, signals.inputs[0].Trusted)
}
