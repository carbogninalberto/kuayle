package service

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/realtime"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

// Filing conversation images on a private-team issue copies them into issue
// assets of that team: teammates and the issue's coding tasks get them,
// outsiders do not, and filing again adds nothing.
func TestConversationImagesFiledOnTheIssueReachTeammatesAndTasks(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := repository.Open(dsn)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	user, outsider, ws, team, issue := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{user, outsider} {
		_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Images','hash')`, id, id.String()+"@test.invalid")
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id=$1`, id) })
	}
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Images',$2,$3)`, ws, "img-"+ws.String()[:8], user)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM workspaces WHERE id=$1`, ws) })
	for _, id := range []uuid.UUID{user, outsider} {
		_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'member')`, ws, id)
		require.NoError(t, err)
	}
	_, err = db.Exec(`INSERT INTO teams(id,workspace_id,name,key,is_private) VALUES($1,$2,'Private','PRV',true)`, team, ws)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO team_members(team_id,user_id) VALUES($1,$2)`, team, user)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO team_statuses(team_id,name,slug,category) VALUES($1,'Todo','todo','unstarted')`, team)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO issues(id,workspace_id,team_id,number,identifier_text,title,creator_id,status,status_id) VALUES($1,$2,$3,1,'PRV-1','Crash',$4,'todo',(SELECT id FROM team_statuses WHERE team_id=$3 LIMIT 1))`, issue, ws, team, user)
	require.NoError(t, err)
	ctx := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: ws, UserID: user})
	other := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: ws, UserID: outsider})

	issueRepo, teamRepo, assets := repository.NewIssueRepository(db), repository.NewTeamRepository(db), repository.NewAssetRepository(db)
	issues := NewIssueService(issueRepo, teamRepo, repository.NewTeamStatusRepository(db), repository.NewIssueHistoryRepository(db), realtime.NewHub(), nil)
	repo := repository.NewConversationRepository(db)
	svc := NewConversationService(repo, NewAISettingsService(nil, nil, nil, nil))
	svc.SetTools(NewAssistantTools(issueRepo, teamRepo, repository.NewWorkspaceRepository(db), issues))
	storage := newMemoryStorage()
	svc.SetAttachments(NewImageStore(storage), assets, NewCommentService(repository.NewCommentRepository(db), issueRepo, realtime.NewHub(), nil), repository.NewWorkspaceRepository(db))

	chat, err := repo.OpenIssue(ctx, ws, issue)
	require.NoError(t, err)
	_, err = svc.Upload(other, ws, chat.ID, "x.png", bytes.NewReader(testPNG(t, 4, 4)))
	require.ErrorIs(t, err, repository.ErrConversationNotFound, "an outsider cannot upload to the chat")
	require.Empty(t, storage.objects)
	image, err := svc.Upload(ctx, ws, chat.ID, "crash.png", bytes.NewReader(testPNG(t, 4, 4)))
	require.NoError(t, err)
	reply, err := repo.Send(ctx, ws, chat.ID, uuid.New(), "File this", "deepseek", "model", image.ID)
	require.NoError(t, err)
	var message domain.ConversationMessage
	require.NoError(t, db.Get(&message, `UPDATE conversation_messages SET status='streaming',lease_token=gen_random_uuid(),lease_expires_at=now()+interval '2 minutes' WHERE id=$1 RETURNING *`, reply.ID))
	attempt := domain.ConversationAttempt{Conversation: *chat, Message: message}

	history, err := repo.GenerationContext(ctx, attempt)
	require.NoError(t, err)
	require.Len(t, history, 1)
	require.Len(t, history[0].Attachments, 1, "the model context carries the image")

	output, issueID, err := svc.attachImagesToIssue(ctx, attempt, `{"identifier":"PRV-1"}`)
	require.NoError(t, err)
	require.Equal(t, issue, *issueID)
	require.Equal(t, 1, output.(map[string]any)["filed"])
	var assetID uuid.UUID
	require.NoError(t, db.Get(&assetID, `SELECT asset_id FROM conversation_attachment_issues WHERE attachment_id=$1 AND issue_id=$2`, image.ID, issue))
	filed, err := assets.GetByID(ctx, assetID)
	require.NoError(t, err)
	require.NotNil(t, filed)
	require.Equal(t, team, *filed.TeamID, "the copy belongs to the issue's team")
	require.NotEqual(t, image.StorageKey, filed.StorageKey)
	hidden, err := assets.GetByID(other, assetID)
	require.NoError(t, err)
	require.Nil(t, hidden, "outsiders of the private team cannot read it")
	var body string
	require.NoError(t, db.Get(&body, `SELECT body FROM comments WHERE issue_id=$1`, issue))
	require.True(t, strings.Contains(body, "/assets/"+assetID.String()), body)

	output, _, err = svc.attachImagesToIssue(ctx, attempt, `{"identifier":"PRV-1","attachment_ids":["`+image.ID.String()+`"]}`)
	require.NoError(t, err)
	require.Equal(t, 0, output.(map[string]any)["filed"])
	var comments int
	require.NoError(t, db.Get(&comments, `SELECT count(*) FROM comments WHERE issue_id=$1`, issue))
	require.Equal(t, 1, comments, "filing again adds no comment")
	_, _, err = svc.attachImagesToIssue(ctx, attempt, `{"identifier":"PRV-1","attachment_ids":["`+uuid.NewString()+`"]}`)
	require.ErrorContains(t, err, "not in this conversation")

	tasks := repository.NewAgentTaskRepository(db)
	task := domain.AgentTask{ID: uuid.New(), WorkspaceID: ws, IssueID: issue, CreatorID: user, RunID: uuid.New()}
	images, err := tasks.IssueImages(context.Background(), task, 20)
	require.NoError(t, err)
	require.Len(t, images, 1)
	require.Equal(t, assetID, images[0].ID)
	outsiderTask := task
	outsiderTask.CreatorID = outsider
	images, err = tasks.IssueImages(context.Background(), outsiderTask, 20)
	require.NoError(t, err)
	require.Empty(t, images, "a task only receives images its creator can read")

	// An attempt's list is fixed once, so its prompt and copied files match.
	created, err := tasks.Create(ctx, ws, chat.ID, domain.AgentTaskRequest{RequestID: uuid.New(), Prompt: "Fix the crash", Provider: "codex", Model: "m", Thinking: "high"})
	require.NoError(t, err)
	items, err := attemptAttachments(context.Background(), tasks, *created)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, ".kuayle/attachments/01-crash.png", items[0].Path)
	_, err = db.Exec(`INSERT INTO comments(issue_id,user_id,body) VALUES($1,$2,$3)`, issue, user, `<img src="/api/workspaces/x/assets/`+assetID.String()+`">`)
	require.NoError(t, err)
	again, err := attemptAttachments(context.Background(), tasks, *created)
	require.NoError(t, err)
	require.Equal(t, items, again)
	manager, err := repository.NewDevMachineRepository(db).ListRunAttachments(context.Background(), created.RunID)
	require.NoError(t, err)
	require.Equal(t, filed.StorageKey, manager[0].StorageKey, "the manager copies the issue asset")
	require.Contains(t, withAttachments(created.Prompt, items), ".kuayle/attachments/01-crash.png")
}
