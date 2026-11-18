package service

import (
	"bytes"
	"context"
	"fmt"
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

// as=attachment files conversation images on the issue itself: appended to its
// description, with no comment, so issue readers and coding tasks receive them
// as the issue's own images. An image already filed, as a comment or as an
// attachment, is not filed again, and non-images never reach the filing.
func TestConversationImagesAttachedToTheIssueItself(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := repository.Open(dsn)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	user, outsider, ws, team := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	issue, second := uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{user, outsider} {
		_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Images','hash')`, id, id.String()+"@test.invalid")
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id=$1`, id) })
	}
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Images',$2,$3)`, ws, "att-"+ws.String()[:8], user)
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
	for i, id := range []uuid.UUID{issue, second} {
		_, err = db.Exec(`INSERT INTO issues(id,workspace_id,team_id,number,identifier_text,title,description,creator_id,status,status_id)
			VALUES($1,$2,$3,$4,$5,'Crash',$6,$7,'todo',(SELECT id FROM team_statuses WHERE team_id=$3 LIMIT 1))`,
			id, ws, team, i+1, fmt.Sprintf("PRV-%d", i+1), "<p>Crash on save.</p>", user)
		require.NoError(t, err)
	}
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
	image, err := svc.Upload(ctx, ws, chat.ID, "crash.png", bytes.NewReader(testPNG(t, 4, 4)))
	require.NoError(t, err)
	_, err = svc.Upload(ctx, ws, chat.ID, "note.html", strings.NewReader("<html><script>alert(1)</script></html>"))
	require.ErrorIs(t, err, ErrAttachmentUnsupported, "non-images are never stored, so never filed")
	reply, err := repo.Send(ctx, ws, chat.ID, uuid.New(), "Attach this", "deepseek", "model", image.ID)
	require.NoError(t, err)
	var message domain.ConversationMessage
	require.NoError(t, db.Get(&message, `UPDATE conversation_messages SET status='streaming',lease_token=gen_random_uuid(),lease_expires_at=now()+interval '2 minutes' WHERE id=$1 RETURNING *`, reply.ID))
	attempt := domain.ConversationAttempt{Conversation: *chat, Message: message}
	// Each send queues an assistant reply; end them so the next send is
	// accepted while filing, which needs only the conversation, keeps working.
	endReplies := func() {
		_, err := db.Exec(`UPDATE conversation_messages SET status='completed',lease_token=NULL,lease_expires_at=NULL WHERE conversation_id=$1 AND status IN ('queued','streaming')`, chat.ID)
		require.NoError(t, err)
	}
	endReplies()

	output, issueID, err := svc.attachImagesToIssue(ctx, attempt, `{"identifier":"PRV-1","as":"attachment"}`)
	require.NoError(t, err)
	require.Equal(t, issue, *issueID)
	require.Equal(t, 1, output.(map[string]any)["filed"])
	var assetID uuid.UUID
	require.NoError(t, db.Get(&assetID, `SELECT asset_id FROM conversation_attachment_issues WHERE attachment_id=$1 AND issue_id=$2`, image.ID, issue))
	filed, err := assets.GetByID(ctx, assetID)
	require.NoError(t, err)
	require.Equal(t, team, *filed.TeamID, "the copy belongs to the issue's team")
	hidden, err := assets.GetByID(other, assetID)
	require.NoError(t, err)
	require.Nil(t, hidden, "outsiders of the private team cannot read it")
	var description string
	require.NoError(t, db.Get(&description, `SELECT description FROM issues WHERE id=$1`, issue))
	require.Contains(t, description, "<p>Crash on save.</p>", "the issue keeps its own description")
	require.Contains(t, description, "/assets/"+assetID.String(), "the image is attached to the issue itself")
	var comments int
	require.NoError(t, db.Get(&comments, `SELECT count(*) FROM comments WHERE issue_id=$1`, issue))
	require.Equal(t, 0, comments, "attachment mode posts no comment")

	// Filing again, either way, adds nothing and leaves the description alone.
	output, _, err = svc.attachImagesToIssue(ctx, attempt, `{"identifier":"PRV-1","attachment_ids":["`+image.ID.String()+`"],"as":"attachment"}`)
	require.NoError(t, err)
	require.Equal(t, 1, output.(map[string]any)["already_filed"])
	var unchanged string
	require.NoError(t, db.Get(&unchanged, `SELECT description FROM issues WHERE id=$1`, issue))
	require.Equal(t, description, unchanged)

	// Coding tasks receive the description images as the issue's own.
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

	// The comment path is unchanged: a comment is posted, the description is not.
	third, err := svc.Upload(ctx, ws, chat.ID, "trace.png", bytes.NewReader(testPNG(t, 4, 4)))
	require.NoError(t, err)
	_, err = repo.Send(ctx, ws, chat.ID, uuid.New(), "And this one as a comment", "deepseek", "model", third.ID)
	require.NoError(t, err)
	endReplies()
	output, _, err = svc.attachImagesToIssue(ctx, attempt, `{"identifier":"PRV-1","attachment_ids":["`+third.ID.String()+`"],"as":"comment"}`)
	require.NoError(t, err)
	require.Equal(t, 1, output.(map[string]any)["filed"])
	var comment string
	require.NoError(t, db.Get(&comment, `SELECT body FROM comments WHERE issue_id=$1`, issue))
	require.Contains(t, comment, "Images from the assistant conversation")
	var afterComment string
	require.NoError(t, db.Get(&afterComment, `SELECT description FROM issues WHERE id=$1`, issue))
	require.Equal(t, unchanged, afterComment)

	// Filing without IDs still files at most the newest 6 images.
	var newest []uuid.UUID
	for i := range 6 {
		uploaded, err := svc.Upload(ctx, ws, chat.ID, fmt.Sprintf("new%d.png", i), bytes.NewReader(testPNG(t, 4, 4)))
		require.NoError(t, err)
		newest = append(newest, uploaded.ID)
	}
	_, err = repo.Send(ctx, ws, chat.ID, uuid.New(), "Six more", "deepseek", "model", newest...)
	require.NoError(t, err)
	endReplies()
	output, _, err = svc.attachImagesToIssue(ctx, attempt, `{"identifier":"PRV-2"}`)
	require.NoError(t, err)
	require.Equal(t, 6, output.(map[string]any)["filed"], "the newest 6 images are filed, no more")
}
