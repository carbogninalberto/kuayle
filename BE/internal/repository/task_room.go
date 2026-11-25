package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strings"

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
	if err = wakeRoomTick(ctx, tx, task); err != nil {
		return nil, err
	}
	return message, tx.Commit()
}

// Wake the task waiting for a person's post or a delegated decision.
func wakeRoomTick(ctx context.Context, tx *sqlx.Tx, task *domain.AgentTask) error {
	_, err := tx.ExecContext(ctx, `UPDATE agent_tasks SET workflow_wake_at=now() WHERE id=$1 AND workspace_id=$2 AND pipeline AND state='awaiting_review'`, task.ID, task.WorkspaceID)
	return err
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

// RecordRoomOutbox posts the outbox files of a task that are addressed to
// its room ("to":"@role" or "@all") for the role the task plays, and returns
// the other files. A file that does not parse, is too large or is refused is
// kept as a refused post with the reason, which its sender reads on its next
// turn. A file taken twice in a run is posted once (the same dedupe key as
// agent messages). A task that runs no pipeline has no room: all its files
// are returned, and the agent messages refuse the room ones.
func (r *AgentTaskRepository) RecordRoomOutbox(ctx context.Context, taskID uuid.UUID, files []domain.AgentOutboxFile) ([]domain.AgentOutboxFile, int, error) {
	var room, rest []domain.AgentOutboxFile
	for _, file := range files {
		if domain.IsRoomOutbox(file.Data) {
			room = append(room, file)
		} else {
			rest = append(rest, file)
		}
	}
	if len(room) == 0 {
		return files, 0, nil
	}
	sender, err := r.roomSender(ctx, taskID)
	if errors.Is(err, ErrTaskRoomUnavailable) {
		return files, 0, nil
	}
	if err != nil {
		return files, 0, err
	}
	slices.SortStableFunc(room, func(a, b domain.AgentOutboxFile) int { return strings.Compare(a.Name, b.Name) })
	recorded := 0
	var errs []error
	for _, file := range room {
		digest := sha256.Sum256([]byte(sender.runID.String() + "\x00" + file.Name + "\x00" + string(file.Data)))
		post := domain.TaskRoomPost{AuthorKind: domain.RoomAuthorMember, AuthorRole: sender.role, AuthorMemberID: sender.memberID, RunID: &sender.runID, Kind: domain.RoomSay, DedupeKey: hex.EncodeToString(digest[:])}
		message, parseErr := domain.ParseAgentOutboxMessage(file.Data)
		switch {
		case file.Oversize:
			post.Body, post.Refused = domain.AgentMessagePreview(file.Data), "message file is too large"
		case parseErr != nil:
			post.Body, post.Refused = domain.AgentMessagePreview(file.Data), parseErr.Error()
		default:
			post.ToRoles, post.Kind, post.Data, post.Body, post.ReplyTo = message.Room.ToRoles, message.Room.Kind, message.Room.Data, message.Body, message.ReplyTo
		}
		_, err := r.RecordRoomMessage(ctx, sender.taskID, post)
		switch {
		case err == nil:
			recorded++
		case errors.Is(err, ErrTaskRoomRateLimited):
			// The sender spent its hourly budget; the file is dropped as
			// agent messages over budget are.
		default:
			errs = append(errs, err)
		}
	}
	return rest, recorded, errors.Join(errs...)
}

// roomPoster is who posts a task's room files: the task whose room it is,
// the role it plays there, its member (once there is one) and the run.
type roomPoster struct {
	taskID   uuid.UUID
	role     string
	memberID *uuid.UUID
	runID    uuid.UUID
}

// roomSender says which room a task posts to and as which role. A member's
// task posts to its root task's room as the member's role. An implementing
// task pinned to a pipeline is the root of its room and plays the writer
// holding its write lock.
func (r *AgentTaskRepository) roomSender(ctx context.Context, taskID uuid.UUID) (roomPoster, error) {
	var member struct {
		ID     uuid.UUID `db:"id"`
		TaskID uuid.UUID `db:"task_id"`
		Role   string    `db:"role"`
		RunID  uuid.UUID `db:"run_id"`
	}
	err := r.db.GetContext(ctx, &member, `SELECT m.id,m.task_id,m.role,t.run_id FROM task_members m JOIN agent_tasks t ON t.id=m.member_task_id AND t.workspace_id=m.workspace_id
 WHERE m.member_task_id=$1 AND t.kind=$2`, taskID, domain.AgentTaskKindReview)
	if err == nil {
		return roomPoster{taskID: member.TaskID, role: member.Role, memberID: &member.ID, runID: member.RunID}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return roomPoster{}, err
	}
	var row struct {
		RunID uuid.UUID       `db:"run_id"`
		Graph []byte          `db:"graph"`
		State json.RawMessage `db:"pipeline_state"`
	}
	err = r.db.GetContext(ctx, &row, `SELECT t.run_id,v.graph,t.pipeline_state
 FROM agent_tasks t JOIN workflow_versions v ON v.id=t.workflow_version_id AND v.workspace_id=t.workspace_id
 WHERE t.id=$1 AND t.kind=$2`, taskID, domain.AgentTaskKindImplement)
	if errors.Is(err, sql.ErrNoRows) {
		return roomPoster{}, ErrTaskRoomUnavailable
	}
	if err != nil {
		return roomPoster{}, err
	}
	if domain.WorkflowKind(row.Graph) != domain.WorkflowKindPipeline {
		return roomPoster{}, ErrTaskRoomUnavailable
	}
	pipeline, err := domain.ParsePipeline(row.Graph)
	if err != nil {
		return roomPoster{}, err
	}
	// The root task plays the writer holding the write lock: its run is
	// that writer's turn.
	writer := pipeline.Holder(domain.PipelineHolderRole(row.State))
	if writer.Role == "" {
		return roomPoster{}, ErrTaskRoomUnavailable
	}
	var memberID *uuid.UUID
	if err = r.db.GetContext(ctx, &memberID, `SELECT (SELECT id FROM task_members WHERE task_id=$1 AND role=$2 AND member_task_id IS NULL)`, taskID, writer.Role); err != nil {
		return roomPoster{}, err
	}
	return roomPoster{taskID: taskID, role: writer.Role, memberID: memberID, runID: row.RunID}, nil
}

// RoomMessagePageSize is the maximum number of posts read at once.
const RoomMessagePageSize = 200

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
	err = tx.SelectContext(ctx, &messages, `SELECT * FROM task_messages WHERE task_id=$1 AND workspace_id=$2 AND seq>$3 ORDER BY seq LIMIT $4`, task.ID, ws, after, RoomMessagePageSize)
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
	// The outbox supplies neither the author nor the grant. Read only the
	// creator's accepted, structured grant for this role and this decision.
	approval := post.Kind == domain.RoomRequest && post.Data.Action == domain.RoomActionApprove
	delegated := post.AuthorKind == domain.RoomAuthorMember && post.AuthorRole == domain.PipelineLeadRole && (approval || post.Kind == domain.RoomWaiver)
	var grantSeq int64
	if delegated {
		// Before approval grants existed, a check-bearing grant delegated that
		// check to the lead and ignored any extra role or action fields.
		if err = tx.GetContext(ctx, &grantSeq, `SELECT COALESCE((SELECT seq FROM task_messages
 WHERE task_id=$1 AND workspace_id=$2 AND author_kind='human' AND author_user_id=$3
 AND kind='grant' AND reject_reason IS NULL
 AND CASE WHEN $4::boolean THEN data->>'role'='lead' AND data->>'action'='approve' AND NOT (data ? 'check')
          ELSE data->>'check'=$5 END
 ORDER BY seq DESC LIMIT 1),0)`, task.ID, task.WorkspaceID, task.CreatorID, approval, post.Data.Check); err != nil {
			return nil, err
		}
	}
	reason := post.Refused
	if reason == "" {
		reason = domain.TaskRoomRefusal(*pipeline, post, grantSeq != 0)
	}
	var seq int64
	if err = tx.GetContext(ctx, &seq, `SELECT COALESCE(max(seq),0)+1 FROM task_messages WHERE task_id=$1`, task.ID); err != nil {
		return nil, err
	}
	if reason == "" && post.ReplyTo != nil && (*post.ReplyTo < 1 || *post.ReplyTo >= seq) {
		reason = "it replies to a message that does not exist"
	}
	// A member's post names its own member; a verdict counts only from the
	// member of its role, for a commit of the task's branch, named whole.
	if reason == "" && post.AuthorKind == domain.RoomAuthorMember {
		if reason, err = memberRefusal(ctx, tx, *task, post); err != nil {
			return nil, err
		}
	}
	if reason == "" && post.Kind == domain.RoomVerdict {
		var commit string
		if commit, reason, err = verdictCommit(ctx, tx, *task, post); err != nil {
			return nil, err
		}
		post.Data.Commit, post.Data.Role = commit, post.AuthorRole
	}
	if reason == "" && post.Kind == domain.RoomGrant {
		action := post.Data.Action
		if action == "" {
			action = domain.RoomActionWaive
		}
		post.Data = domain.TaskRoomData{Role: domain.PipelineLeadRole, Action: action, Check: post.Data.Check}
	}
	if reason == "" && (approval || post.Kind == domain.RoomWaiver) {
		if reason, err = bindRoomDecisionHead(ctx, tx, task, &post); err != nil {
			return nil, err
		}
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
	if message.Kind == domain.RoomVerdict && message.RejectReason == nil {
		if err = recordVerdict(ctx, tx, message); err != nil {
			return nil, err
		}
	}
	if rejected == nil && (post.Kind == domain.RoomGrant || post.Kind == domain.RoomWaiver || approval) {
		if err = auditRoomDecision(ctx, tx, task, message, grantSeq); err != nil {
			return nil, err
		}
	}
	// A lead's split request wakes the tick too, which carries it out at
	// once when the creator granted the split (KUA-313).
	split := post.Kind == domain.RoomRequest && post.Data.Action == domain.RoomRequestSplit
	if (delegated || split) && rejected == nil {
		if err = wakeRoomTick(ctx, tx, task); err != nil {
			return nil, err
		}
	}
	if task.ConversationID != nil {
		if _, err = conversationEvent(ctx, tx, *task.ConversationID, "task.room_message", message); err != nil {
			return nil, err
		}
	}
	return &message, nil
}

// bindRoomDecisionHead uses the last head Kuayle announced under the room
// lock. An intervening push cannot transfer a decision to another head.
func bindRoomDecisionHead(ctx context.Context, tx *sqlx.Tx, task *domain.AgentTask, post *domain.TaskRoomPost) (string, error) {
	var head string
	err := tx.GetContext(ctx, &head, `SELECT COALESCE((SELECT data->>'value' FROM task_messages
 WHERE task_id=$1 AND workspace_id=$2 AND author_kind='kuayle' AND kind='fact' AND data->>'fact'='head_sha'
 ORDER BY seq DESC LIMIT 1),'')`, task.ID, task.WorkspaceID)
	if err != nil {
		return "", err
	}
	if post.Kind == domain.RoomRequest {
		if head == "" || post.Data.Commit != head {
			return "approval must name the current room head", nil
		}
		post.Data = domain.TaskRoomData{Action: domain.RoomActionApprove, Commit: head}
	} else {
		if head == "" && !post.Data.Standing {
			return "a head-bound waiver needs a current room head", nil
		}
		post.Data = domain.TaskRoomData{Check: post.Data.Check, Standing: post.Data.Standing, Commit: head}
	}
	return "", nil
}

// auditRoomDecision links an accepted delegated decision to the grant or
// pinned definition that authorized it. A member's run has no user actor.
func auditRoomDecision(ctx context.Context, tx *sqlx.Tx, task *domain.AgentTask, m domain.TaskMessage, grantSeq int64) error {
	action := m.Kind
	if m.Kind == domain.RoomRequest {
		action = domain.RoomActionApprove
	}
	detail := map[string]any{"sequence": m.Seq, "data": m.Data, "reason": m.Body, "author_kind": m.AuthorKind}
	if m.AuthorKind == domain.RoomAuthorMember {
		detail["author_role"], detail["run_id"] = m.AuthorRole, m.RunID
		if grantSeq > 0 {
			detail["grant_sequence"] = grantSeq
		} else {
			detail["workflow_version_id"] = task.WorkflowVersionID
		}
	}
	return workflowAudit(ctx, tx, task.WorkspaceID, m.AuthorUserID, "task_workflow", task.ID, action, detail)
}
