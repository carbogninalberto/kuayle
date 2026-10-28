package repository

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestIssueConversationsRecheckScopeAcrossChannels(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db := isolatedPrivacyDatabase(t, url)
	owner, user, other, ws, foreign := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{owner, user, other} {
		_, err := db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Test','hash')`, id, id.String()+"@test.invalid")
		require.NoError(t, err)
	}
	for _, id := range []uuid.UUID{ws, foreign} {
		_, err := db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Test',$2,$3)`, id, id.String(), owner)
		require.NoError(t, err)
	}
	for _, id := range []uuid.UUID{owner, user, other} {
		role := "member"
		if id == owner {
			role = "owner"
		}
		_, err := db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,$3)`, ws, id, role)
		require.NoError(t, err)
	}
	team, public, foreignTeam := uuid.New(), uuid.New(), uuid.New()
	for i, id := range []uuid.UUID{team, public, foreignTeam} {
		workspace := ws
		if i == 2 {
			workspace = foreign
		}
		_, err := db.Exec(`INSERT INTO teams(id,workspace_id,name,key,is_private) VALUES($1,$2,'Team',$3,$4)`, id, workspace, []string{"PVT", "PUB", "OTH"}[i], i == 0)
		require.NoError(t, err)
	}
	_, err := db.Exec(`INSERT INTO team_members(team_id,user_id) VALUES($1,$2)`, team, user)
	require.NoError(t, err)
	for _, id := range []uuid.UUID{team, public, foreignTeam} {
		_, err = db.Exec(`INSERT INTO team_statuses(team_id,name,slug,category) VALUES($1,'Todo','todo','unstarted')`, id)
		require.NoError(t, err)
	}
	issue, foreignIssue := uuid.New(), uuid.New()
	for i, id := range []uuid.UUID{issue, foreignIssue} {
		workspace, issueTeam := ws, team
		if i == 1 {
			workspace, issueTeam = foreign, foreignTeam
		}
		_, err = db.Exec(`INSERT INTO issues(id,workspace_id,team_id,number,identifier_text,title,description,creator_id,status_id) VALUES($1,$2,$3,1,$4,'Private title','Private context',$5,(SELECT id FROM team_statuses WHERE team_id=$3 LIMIT 1))`, id, workspace, issueTeam, []string{"PVT-1", "OTH-1"}[i], owner)
		require.NoError(t, err)
	}
	ctx := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: ws, UserID: user})
	outsider := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: ws, UserID: other})
	repo := NewConversationRepository(db)
	_, err = repo.OpenIssue(outsider, ws, issue)
	require.ErrorIs(t, err, ErrConversationNotFound)
	_, err = repo.OpenIssue(ctx, ws, foreignIssue)
	require.ErrorIs(t, err, ErrConversationNotFound)
	_, err = repo.OpenIssue(context.Background(), ws, issue)
	require.ErrorIs(t, err, ErrConversationNotFound)
	results := make(chan *domain.Conversation, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			c, e := repo.OpenIssue(ctx, ws, issue)
			if e != nil {
				t.Error(e)
				return
			}
			results <- c
		})
	}
	wg.Wait()
	close(results)
	var c *domain.Conversation
	for result := range results {
		if c != nil {
			require.Equal(t, c.ID, result.ID)
		}
		c = result
	}
	require.NotNil(t, c)
	require.Equal(t, issue, *c.IssueID)
	require.Equal(t, team, *c.TeamID)
	_, err = repo.History(outsider, ws, c.ID, 0)
	require.ErrorIs(t, err, ErrConversationNotFound)
	t.Run("a completed team revocation cannot be followed by a previously authorized write", func(t *testing.T) {
		held, _, e := repo.authorized(ctx, ws, c.ID)
		require.NoError(t, e)
		defer held.Rollback()
		timeout, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		var pid int
		require.NoError(t, held.Get(&pid, `SELECT pg_backend_pid()`))
		revoked := make(chan error, 1)
		go func() {
			_, e := db.ExecContext(timeout, `DELETE FROM team_members WHERE team_id=$1 AND user_id=$2`, team, user)
			revoked <- e
		}()
		require.Eventually(t, func() bool {
			var waiting bool
			e := db.Get(&waiting, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))`, pid)
			return e == nil && waiting
		}, time.Second, 10*time.Millisecond, "team revocation must wait for the authorized conversation transaction")
		require.NoError(t, held.Rollback())
		require.NoError(t, <-revoked)
		_, e = db.Exec(`INSERT INTO team_members(team_id,user_id) VALUES($1,$2)`, team, user)
		require.NoError(t, e)
	})

	_, err = repo.Send(ctx, ws, c.ID, uuid.New(), "Explain this issue", "compatible", "test")
	require.NoError(t, err)
	a, err := repo.Claim(context.Background())
	require.NoError(t, err)
	require.NotNil(t, a)
	snapshot, err := repo.GenerationIssue(ctx, *a)
	require.NoError(t, err)
	require.Equal(t, "Private context", snapshot.Description)
	_, err = repo.Append(ctx, *a, "Private answer")
	require.NoError(t, err)
	require.NoError(t, repo.Finish(ctx, *a, "completed"))

	t.Run("workspace removal follows the authorization lock order", func(t *testing.T) {
		held, _, e := repo.authorized(ctx, ws, c.ID)
		require.NoError(t, e)
		defer held.Rollback()
		// The scoped authorization has already taken both membership locks. Remove
		// membership must queue at the workspace parent before touching team rows.
		timeout, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		removed := make(chan error, 1)
		go func() { removed <- NewWorkspaceRepository(db).RemoveMember(timeout, ws, user) }()
		require.Eventually(t, func() bool {
			var waiting bool
			e := db.Get(&waiting, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%FROM workspace_members%FOR UPDATE%')`)
			return e == nil && waiting
		}, time.Second, 10*time.Millisecond)
		require.NoError(t, held.Rollback())
		require.NoError(t, <-removed)
		_, e = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'member')`, ws, user)
		require.NoError(t, e)
		_, e = db.Exec(`INSERT INTO team_members(team_id,user_id) VALUES($1,$2)`, team, user)
		require.NoError(t, e)
	})

	// A queued Telegram delivery must recheck issue visibility before network I/O.
	tg := NewTelegramRepository(db)
	ownerCtx := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: ws, UserID: owner})
	require.NoError(t, tg.Save(ownerCtx, domain.TelegramSettings{WorkspaceID: ws, BotID: 42, TokenEncrypted: "encrypted-fixture", WebhookSecret: "header-fixture", Enabled: true, AllowedUserIDs: []int64{12345}}))
	_, err = db.Exec(`INSERT INTO telegram_links(workspace_id,user_id,telegram_user_id,conversation_id) VALUES($1,$2,12345,$3)`, ws, user, c.ID)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO telegram_deliveries(workspace_id,user_id,telegram_user_id,conversation_id,message_id) VALUES($1,$2,12345,$3,$4)`, ws, user, c.ID, a.Message.ID)
	require.NoError(t, err)
	delivery, err := tg.ClaimDelivery(context.Background())
	require.NoError(t, err)
	require.NotNil(t, delivery)
	_, err = repo.Send(ctx, ws, c.ID, uuid.New(), "Follow up", "compatible", "test")
	require.NoError(t, err)
	_, err = db.Exec(`DELETE FROM team_members WHERE team_id=$1 AND user_id=$2`, team, user)
	require.NoError(t, err)
	_, err = repo.History(ctx, ws, c.ID, 0)
	require.ErrorIs(t, err, ErrConversationNotFound)
	_, err = repo.Events(ctx, ws, c.ID, 0)
	require.ErrorIs(t, err, ErrConversationNotFound)
	_, err = repo.Send(ctx, ws, c.ID, uuid.New(), "Denied", "compatible", "test")
	require.ErrorIs(t, err, ErrConversationNotFound)
	_, err = repo.GenerationIssue(ctx, *a)
	require.ErrorIs(t, err, ErrConversationNotFound)
	chats, err := repo.List(ctx, ws)
	require.NoError(t, err)
	require.Empty(t, chats)
	next, err := repo.Claim(context.Background())
	require.NoError(t, err)
	require.Nil(t, next, "revoked issue must not reach a model")
	require.NoError(t, tg.Deliver(context.Background(), *delivery, func(domain.TelegramDelivery) error { t.Error("private output delivered after revocation"); return nil }))
	incoming := domain.TelegramUpdate{ID: 1, UserID: 12345, ChatID: 12345, Private: true, Text: "Denied Telegram input"}
	require.NoError(t, tg.Receive(context.Background(), ws, "header-fixture", incoming, "", "compatible", "test"))
	var count int
	require.NoError(t, db.Get(&count, `SELECT count(*) FROM conversation_messages WHERE conversation_id=$1`, c.ID))
	require.Equal(t, 4, count)
	// Existing privacy controls reject direct moves out of a private team.
	_, err = db.Exec(`UPDATE issues SET team_id=$2,status_id=(SELECT id FROM team_statuses WHERE team_id=$2 LIMIT 1),identifier_text='PUB-1' WHERE id=$1`, issue, public)
	require.ErrorContains(t, err, "private-team boundary")
	// Even after explicitly making the original team public, a move cannot
	// relabel the conversation's historical scope.
	_, err = db.Exec(`UPDATE teams SET is_private=false WHERE id=$1`, team)
	require.NoError(t, err)
	held, _, err := repo.authorized(ctx, ws, c.ID)
	require.NoError(t, err)
	defer held.Rollback()
	moved := make(chan error, 1)
	moveCtx, stopMove := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopMove()
	go func() {
		_, e := db.ExecContext(moveCtx, `UPDATE issues SET team_id=$2,status_id=(SELECT id FROM team_statuses WHERE team_id=$2 LIMIT 1),identifier_text='PUB-1' WHERE id=$1`, issue, public)
		moved <- e
	}()
	var heldPID int
	require.NoError(t, held.Get(&heldPID, `SELECT pg_backend_pid()`))
	require.Eventually(t, func() bool {
		var waiting bool
		e := db.Get(&waiting, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))`, heldPID)
		return e == nil && waiting
	}, time.Second, 10*time.Millisecond, "moving the issue must wait for an authorized conversation write")
	require.NoError(t, held.Rollback())
	err = <-moved
	require.NoError(t, err)
	_, err = repo.History(ctx, ws, c.ID, 0)
	require.ErrorIs(t, err, ErrConversationNotFound)
	_, err = db.Exec(`INSERT INTO team_members(team_id,user_id) VALUES($1,$2)`, team, user)
	require.NoError(t, err)
	_, err = repo.History(ctx, ws, c.ID, 0)
	require.ErrorIs(t, err, ErrConversationNotFound, "original and current issue scope must match")
	// Ordinary chat stays usable independently of issue scope.
	general, err := repo.Create(ctx, ws, "General")
	require.NoError(t, err)
	chats, err = repo.List(ctx, ws)
	require.NoError(t, err)
	require.Len(t, chats, 1)
	require.Equal(t, general.ID, chats[0].ID)
	t.Run("delivery and team deletion do not invert ancestor locks", func(t *testing.T) {
		fresh, e := repo.OpenIssue(ctx, ws, issue)
		require.NoError(t, e)
		_, e = db.Exec(`UPDATE telegram_links SET conversation_id=$1 WHERE workspace_id=$2 AND user_id=$3`, fresh.ID, ws, user)
		require.NoError(t, e)
		_, e = db.Exec(`INSERT INTO telegram_deliveries(workspace_id,user_id,telegram_user_id,conversation_id,text) VALUES($1,$2,12345,$3,'Ready')`, ws, user, fresh.ID)
		require.NoError(t, e)
		d, e := tg.ClaimDelivery(context.Background())
		require.NoError(t, e)
		require.NotNil(t, d)
		held, e := db.BeginTxx(context.Background(), nil)
		require.NoError(t, e)
		defer held.Rollback()
		_, e = held.Exec(`SELECT id FROM issues WHERE id=$1 FOR UPDATE`, issue)
		require.NoError(t, e)
		timeout, stop := context.WithTimeout(context.Background(), 8*time.Second)
		defer stop()
		sent, deleted := make(chan error, 1), make(chan error, 1)
		go func() { sent <- tg.Deliver(timeout, *d, func(domain.TelegramDelivery) error { return nil }) }()
		require.Eventually(t, func() bool {
			var waiting bool
			e := db.Get(&waiting, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%FROM issues WHERE id=%FOR SHARE%')`)
			return e == nil && waiting
		}, 2*time.Second, 10*time.Millisecond)
		go func() { _, e := db.ExecContext(timeout, `DELETE FROM teams WHERE id=$1`, public); deleted <- e }()
		require.Eventually(t, func() bool {
			var waiting bool
			e := db.Get(&waiting, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE 'DELETE FROM teams WHERE id=%')`)
			return e == nil && waiting
		}, 2*time.Second, 10*time.Millisecond)
		require.NoError(t, held.Rollback())
		require.NoError(t, <-sent)
		require.NoError(t, <-deleted)
	})

}
