package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/lib/pq"
)

var (
	// ErrTaskRoomUnavailable is a task that runs no pipeline: it has no room.
	ErrTaskRoomUnavailable = errors.New("the task has no room")
	// ErrTaskRoomRateLimited is a sender that used its hourly budget.
	ErrTaskRoomRateLimited = errors.New("too many room messages from this sender")
)

// TaskRoomRefusedError is a post the room refuses from a person or from
// Kuayle, or one of an unknown kind; nothing is stored. A member's refused
// post is stored instead, with the reason.
type TaskRoomRefusedError struct{ Reason string }

func (e *TaskRoomRefusedError) Error() string { return "room post refused: " + e.Reason }

// roomReasonRunes bounds a stored refusal (it may quote what was sent).
const roomReasonRunes = 200

// PostRoomMessage posts to the room of a pipeline task as its creator.
func (r *AgentTaskRepository) PostRoomMessage(ctx context.Context, ws, id uuid.UUID, post domain.TaskRoomPost) (*domain.TaskMessage, error) {
	actor, err := conversationActor(ctx, ws)
	if err != nil {
		return nil, err
	}
	tx, task, err := r.transaction(ctx, id, true)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if task.WorkspaceID != ws {
		return nil, ErrConversationNotFound
	}
	post.AuthorKind, post.AuthorRole, post.AuthorUserID, post.AuthorMemberID, post.RunID = domain.RoomAuthorHuman, "", &actor, nil, nil
	message, err := postTaskMessageTx(ctx, tx, task, post)
	if err != nil {
		return nil, err
	}
	return message, tx.Commit()
}

// RecordRoomMessage posts to the room of a pipeline task for one of its
// roles or for Kuayle. They work for the task's creator, so the creator's
// current access is checked as for their own posts. A member's refused post
// is stored with the reason for its sender and is never delivered. People
// post only through PostRoomMessage.
func (r *AgentTaskRepository) RecordRoomMessage(ctx context.Context, id uuid.UUID, post domain.TaskRoomPost) (*domain.TaskMessage, error) {
	if post.AuthorKind != domain.RoomAuthorMember && post.AuthorKind != domain.RoomAuthorKuayle {
		return nil, &TaskRoomRefusedError{"people post as the task's creator"}
	}
	var owner struct {
		WorkspaceID uuid.UUID `db:"workspace_id"`
		CreatorID   uuid.UUID `db:"creator_id"`
	}
	if err := r.db.GetContext(ctx, &owner, `SELECT workspace_id,creator_id FROM agent_tasks WHERE id=$1`, id); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrConversationNotFound
	} else if err != nil {
		return nil, err
	}
	ctx = domain.WithAccess(ctx, domain.Access{WorkspaceID: owner.WorkspaceID, UserID: owner.CreatorID})
	tx, task, err := r.transaction(ctx, id, true)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	post.AuthorUserID = nil
	if post.AuthorKind == domain.RoomAuthorKuayle {
		post.AuthorRole, post.AuthorMemberID = "", nil
	}
	message, err := postTaskMessageTx(ctx, tx, task, post)
	if err != nil {
		return nil, err
	}
	return message, tx.Commit()
}

// RoomMessages lists the room of a pipeline task after a sequence number,
// oldest first, for its creator. Refused posts are listed with their reason.
func (r *AgentTaskRepository) RoomMessages(ctx context.Context, ws, id uuid.UUID, after int64) ([]domain.TaskMessage, error) {
	if _, err := conversationActor(ctx, ws); err != nil {
		return nil, err
	}
	tx, task, err := r.transaction(ctx, id, true)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if task.WorkspaceID != ws {
		return nil, ErrConversationNotFound
	}
	if _, err = taskPipeline(ctx, tx, task); err != nil {
		return nil, err
	}
	messages := []domain.TaskMessage{}
	err = tx.SelectContext(ctx, &messages, `SELECT * FROM task_messages WHERE task_id=$1 AND workspace_id=$2 AND seq>$3 ORDER BY seq LIMIT 200`, task.ID, ws, after)
	return messages, err
}

// taskPipeline is the pipeline an implementing task is pinned to.
func taskPipeline(ctx context.Context, tx *sqlx.Tx, task *domain.AgentTask) (*domain.Pipeline, error) {
	if task.Kind != domain.AgentTaskKindImplement || task.WorkflowVersionID == nil {
		return nil, ErrTaskRoomUnavailable
	}
	var raw []byte
	err := tx.GetContext(ctx, &raw, `SELECT graph FROM workflow_versions WHERE id=$1 AND workspace_id=$2`, task.WorkflowVersionID, task.WorkspaceID)
	if errors.Is(err, sql.ErrNoRows) || err == nil && domain.WorkflowKind(raw) != domain.WorkflowKindPipeline {
		return nil, ErrTaskRoomUnavailable
	}
	if err != nil {
		return nil, err
	}
	return domain.ParsePipeline(raw)
}

// postTaskMessageTx stores one post. The caller holds the task in the usual
// lock order; the task's advisory lock (the workflow runner's) then orders
// the room, so seq is the task's next number. A repeated dedupe key returns
// the first post. Each sender has an hourly budget, refusals included: a
// role that of agent messages, the creator that of live messages.
func postTaskMessageTx(ctx context.Context, tx *sqlx.Tx, task *domain.AgentTask, post domain.TaskRoomPost) (*domain.TaskMessage, error) {
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "workflow-runner:"+task.ID.String()); err != nil {
		return nil, err
	}
	pipeline, err := taskPipeline(ctx, tx, task)
	if err != nil {
		return nil, err
	}
	var message domain.TaskMessage
	if post.DedupeKey != "" {
		err = tx.GetContext(ctx, &message, `SELECT * FROM task_messages WHERE task_id=$1 AND dedupe_key=$2`, task.ID, post.DedupeKey)
		if !errors.Is(err, sql.ErrNoRows) {
			return &message, err
		}
	}
	if post.AuthorKind != domain.RoomAuthorKuayle {
		budget := domain.AgentMessagesPerHour
		if post.AuthorKind == domain.RoomAuthorHuman {
			budget = domain.AgentLiveMessagesPerHour
		}
		var sent int
		if err = tx.GetContext(ctx, &sent, `SELECT count(*) FROM task_messages WHERE task_id=$1 AND author_kind=$2 AND COALESCE(author_role,'')=$3
 AND author_user_id IS NOT DISTINCT FROM $4 AND created_at>now()-interval '1 hour'`, task.ID, post.AuthorKind, post.AuthorRole, post.AuthorUserID); err != nil {
			return nil, err
		}
		if sent >= budget {
			return nil, ErrTaskRoomRateLimited
		}
	}
	post = post.Clean()
	granted := false
	if post.Kind == domain.RoomWaiver && post.AuthorRole == domain.PipelineLeadRole {
		if err = tx.GetContext(ctx, &granted, `SELECT EXISTS(SELECT 1 FROM task_messages WHERE task_id=$1 AND kind='grant' AND reject_reason IS NULL AND data->>'check'=$2)`, task.ID, post.Data.Check); err != nil {
			return nil, err
		}
	}
	reason := domain.TaskRoomRefusal(*pipeline, post, granted)
	var seq int64
	if err = tx.GetContext(ctx, &seq, `SELECT COALESCE(max(seq),0)+1 FROM task_messages WHERE task_id=$1`, task.ID); err != nil {
		return nil, err
	}
	if reason == "" && post.ReplyTo != nil && (*post.ReplyTo < 1 || *post.ReplyTo >= seq) {
		reason = "it replies to a message that does not exist"
	}
	if reason != "" && (post.AuthorKind != domain.RoomAuthorMember || !slices.Contains(domain.RoomKinds, post.Kind)) {
		return nil, &TaskRoomRefusedError{reason}
	}
	var rejected *string
	if reason != "" {
		// A refused post keeps what its sender needs to recognise it.
		if runes := []rune(reason); len(runes) > roomReasonRunes {
			reason = string(runes[:roomReasonRunes])
		}
		rejected = &reason
		post.ToRoles, post.Data, post.ReplyTo = nil, domain.TaskRoomData{}, nil
		post.Body = domain.AgentMessagePreview([]byte(post.Body))
	}
	data, err := json.Marshal(post.Data)
	if err != nil {
		return nil, err
	}
	err = tx.GetContext(ctx, &message, `INSERT INTO task_messages(workspace_id,task_id,seq,author_kind,author_role,author_user_id,author_member_id,run_id,to_roles,kind,body,data,reply_to,dedupe_key,reject_reason)
 VALUES($1,$2,$3,$4,NULLIF($5,''),$6,$7,$8,COALESCE($9::text[],'{}'),$10,$11,$12,$13,NULLIF($14,''),$15) RETURNING *`,
		task.WorkspaceID, task.ID, seq, post.AuthorKind, post.AuthorRole, post.AuthorUserID, post.AuthorMemberID, post.RunID, pq.StringArray(post.ToRoles),
		post.Kind, post.Body, data, post.ReplyTo, post.DedupeKey, rejected)
	if err != nil {
		return nil, err
	}
	if task.ConversationID != nil {
		if _, err = conversationEvent(ctx, tx, *task.ConversationID, "task.room_message", message); err != nil {
			return nil, err
		}
	}
	return &message, nil
}
