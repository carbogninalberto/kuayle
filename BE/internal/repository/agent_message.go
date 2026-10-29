package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/lib/pq"
)

var (
	ErrAgentMessageNotFound  = errors.New("agent message not found")
	ErrAgentMessageForbidden = errors.New("agent message moderation is not allowed")
	ErrAgentMessageConflict  = errors.New("agent message state changed")
	ErrAgentMessageInvalid   = errors.New("invalid agent message request")
)

// agentMessagesPerPrompt bounds how many messages one attempt receives.
const agentMessagesPerPrompt = 10

// AgentMessageRepository is the agent messaging registry ("navigator").
type AgentMessageRepository struct{ db *sqlx.DB }

func NewAgentMessageRepository(db *sqlx.DB) *AgentMessageRepository {
	return &AgentMessageRepository{db: db}
}

// RecordAgentOutbox records the outbox files the manager took from a
// machine's task worktrees, each for the task whose worktree it came from.
func (r *DevMachineRepository) RecordAgentOutbox(ctx context.Context, machineID uuid.UUID, files []domain.AgentOutboxFile) (int, error) {
	byWorktree := map[string][]domain.AgentOutboxFile{}
	for _, file := range files {
		byWorktree[file.Worktree] = append(byWorktree[file.Worktree], file)
	}
	messages := NewAgentMessageRepository(r.db)
	recorded := 0
	var errs []error
	for worktree, group := range byWorktree {
		task, err := r.TaskForWorktree(ctx, machineID, worktree)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if task == nil {
			continue
		}
		count, err := messages.RecordTaskOutbox(ctx, *task, group)
		recorded += count
		if err != nil {
			errs = append(errs, err)
		}
	}
	return recorded, errors.Join(errs...)
}

type messageSender struct {
	ID          uuid.UUID `db:"id"`
	WorkspaceID uuid.UUID `db:"workspace_id"`
	CreatorID   uuid.UUID `db:"creator_id"`
	RunID       uuid.UUID `db:"run_id"`
	Kind        string    `db:"kind"`
	State       string    `db:"state"`
	IssueID     uuid.UUID `db:"issue_id"`
	TeamID      uuid.UUID `db:"team_id"`
	Allowed     bool      `db:"allowed"`
}

type messageRecipient struct {
	IssueID uuid.UUID  `db:"id"`
	TaskID  *uuid.UUID `db:"task_id"`
}

// RecordTaskOutbox validates and records one task's outbox files. Every file
// becomes one registry record (refused ones included) within the sender's
// hourly budget; a file taken twice is recorded once. Recipients must be a
// related issue (parent, sub-issue, sibling, blocker or blocked issue) of the
// sender's issue in the same team. Files addressed to the orchestrator go to
// the task's live channel instead (agent_live_messages).
func (r *AgentMessageRepository) RecordTaskOutbox(ctx context.Context, taskID uuid.UUID, files []domain.AgentOutboxFile) (int, error) {
	if len(files) == 0 {
		return 0, nil
	}
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	// Serializes one sender's budget without locking the task row, whose lock
	// order (issue, conversation, task) belongs to task transitions.
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('agent_messages:'||$1::text,0))`, taskID); err != nil {
		return 0, err
	}
	var sender messageSender
	err = tx.GetContext(ctx, &sender, `SELECT t.id,t.workspace_id,t.creator_id,t.run_id,t.kind,t.state,t.issue_id,i.team_id,
  (EXISTS(SELECT 1 FROM workspace_members wm WHERE wm.workspace_id=t.workspace_id AND wm.user_id=t.creator_id AND wm.role<>'guest')
   AND kuayle_team_access(i.team_id,t.creator_id,t.workspace_id)) AS allowed
 FROM agent_tasks t JOIN issues i ON i.id=t.issue_id AND i.workspace_id=t.workspace_id WHERE t.id=$1`, taskID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var sent, live int
	if err = tx.GetContext(ctx, &sent, `SELECT count(*) FROM agent_messages WHERE from_task_id=$1 AND created_at>now()-interval '1 hour'`, taskID); err != nil {
		return 0, err
	}
	if err = tx.GetContext(ctx, &live, `SELECT count(*) FROM agent_live_messages WHERE task_id=$1 AND direction='from_agent' AND created_at>now()-interval '1 hour'`, taskID); err != nil {
		return 0, err
	}
	files = append([]domain.AgentOutboxFile(nil), files...)
	sort.SliceStable(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	recorded := 0
	ended := !sender.Allowed || sender.State == "completed" || sender.State == "cancelled" || sender.State == "failed"
	for _, file := range files {
		digest := sha256.Sum256([]byte(sender.RunID.String() + "\x00" + file.Name + "\x00" + string(file.Data)))
		message, parseErr := domain.ParseAgentOutboxMessage(file.Data)
		// Messages to the orchestrator belong to the task's live channel, with
		// their own budget; agents of any task kind may write them.
		if !file.Oversize && (parseErr == nil && message.To == domain.AgentOutboxOrchestrator || parseErr != nil && domain.AgentMessageRef(file.Data) == "ORCHESTRATOR") {
			if live >= domain.AgentLiveMessagesPerHour {
				continue
			}
			var inserted bool
			switch {
			case parseErr != nil:
				inserted, err = recordLiveOutbox(ctx, tx, sender, hex.EncodeToString(digest[:]), nil, domain.AgentMessagePreview(file.Data), parseErr.Error())
			case ended:
				inserted, err = recordLiveOutbox(ctx, tx, sender, hex.EncodeToString(digest[:]), &message, "", "the sending task can no longer send messages")
			default:
				inserted, err = recordLiveOutbox(ctx, tx, sender, hex.EncodeToString(digest[:]), &message, "", "")
			}
			if err != nil {
				return recorded, err
			}
			if inserted {
				recorded++
				live++
			}
			continue
		}
		if sent >= domain.AgentMessagesPerHour {
			continue
		}
		row := domain.AgentMessage{WorkspaceID: sender.WorkspaceID, FromTaskID: sender.ID, FromIssueID: sender.IssueID, FromRunID: &sender.RunID, Channel: domain.AgentMessageInternal, CommentState: "none", DedupeKey: hex.EncodeToString(digest[:])}
		reject := func(reason string) {
			row.State, row.RejectReason = "rejected", &reason
		}
		switch {
		case file.Oversize:
			row.ToRef, row.Body = "?", domain.AgentMessagePreview(file.Data)
			reject("message file is too large")
		case parseErr != nil:
			row.ToRef, row.Body = domain.AgentMessageRef(file.Data), domain.AgentMessagePreview(file.Data)
			reject(parseErr.Error())
		default:
			row.ToRef, row.Channel, row.Body = message.To, message.Channel, message.Body
			if sender.Kind != domain.AgentTaskKindImplement || ended {
				reject("the sending task can no longer send messages")
				break
			}
			var recipient messageRecipient
			err = tx.GetContext(ctx, &recipient, `WITH me AS (SELECT id,parent_id,team_id,workspace_id FROM issues WHERE id=$1),
 related(id) AS (
  SELECT me.parent_id FROM me WHERE me.parent_id IS NOT NULL
  UNION SELECT c.id FROM me JOIN issues c ON c.parent_id=me.id
  UNION SELECT s.id FROM me JOIN issues s ON me.parent_id IS NOT NULL AND s.parent_id=me.parent_id
  UNION SELECT d.blocker_id FROM me JOIN `+issueBlockerEdges+` d ON d.blocked_id=me.id
  UNION SELECT d.blocked_id FROM me JOIN `+issueBlockerEdges+` d ON d.blocker_id=me.id)
 SELECT i.id,(SELECT t.id FROM agent_tasks t WHERE t.issue_id=i.id AND t.workspace_id=i.workspace_id AND t.kind='implement' ORDER BY t.created_at DESC,t.id LIMIT 1) AS task_id
 FROM related r JOIN issues i ON i.id=r.id JOIN me ON i.team_id=me.team_id AND i.workspace_id=me.workspace_id AND i.id<>me.id
 WHERE i.identifier_text=$2 LIMIT 1`, sender.IssueID, message.To)
			if errors.Is(err, sql.ErrNoRows) {
				reject("not a related issue of this task")
				break
			}
			if err != nil {
				return recorded, err
			}
			row.ToIssueID, row.ToTaskID, row.State = &recipient.IssueID, recipient.TaskID, "queued"
			var hold struct {
				CreatedBy *uuid.UUID `db:"created_by"`
			}
			err = tx.GetContext(ctx, &hold, `SELECT created_by FROM agent_message_holds WHERE workspace_id=$1 AND issue_a=LEAST($2::uuid,$3::uuid) AND issue_b=GREATEST($2::uuid,$3::uuid)`, sender.WorkspaceID, sender.IssueID, recipient.IssueID)
			if err == nil {
				row.State, row.HeldBy = "held", hold.CreatedBy
			} else if !errors.Is(err, sql.ErrNoRows) {
				return recorded, err
			}
			if row.State == "queued" && row.Channel == domain.AgentMessageComment {
				row.CommentState = "pending"
			}
		}
		result, err := tx.NamedExecContext(ctx, `INSERT INTO agent_messages(workspace_id,from_task_id,from_issue_id,from_run_id,to_ref,to_issue_id,to_task_id,channel,body,state,reject_reason,dedupe_key,held_by,held_at,comment_state,created_at)
 VALUES(:workspace_id,:from_task_id,:from_issue_id,:from_run_id,:to_ref,:to_issue_id,:to_task_id,:channel,:body,:state,:reject_reason,:dedupe_key,:held_by,CASE WHEN CAST(:state AS text)='held' THEN now() END,:comment_state,clock_timestamp())
 ON CONFLICT (from_task_id,dedupe_key) DO NOTHING`, row)
		if err != nil {
			return recorded, err
		}
		if inserted, _ := result.RowsAffected(); inserted > 0 {
			recorded++
			sent++
		}
	}
	return recorded, tx.Commit()
}

const deliverySelect = `SELECT m.id,fi.identifier_text AS from_identifier,m.from_task_id,m.channel,m.body,m.created_at
 FROM agent_messages m JOIN issues fi ON fi.id=m.from_issue_id`

// Claim marks the messages queued for a task's issue as delivered to its
// current attempt and returns them, oldest first, within room bytes of
// prompt. A repeated call for the same attempt returns the same messages.
// Messages claimed for an attempt that was never created are claimed again.
func (r *AgentMessageRepository) Claim(ctx context.Context, task domain.AgentTask, room int) ([]domain.AgentMessageDelivery, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	deliveries := []domain.AgentMessageDelivery{}
	if err = tx.SelectContext(ctx, &deliveries, deliverySelect+` WHERE m.workspace_id=$1 AND m.to_issue_id=$2 AND m.state='delivered' AND m.delivered_run_id=$3 ORDER BY m.created_at,m.id`, task.WorkspaceID, task.IssueID, task.RunID); err != nil || len(deliveries) > 0 {
		return deliveries, err
	}
	candidates := []domain.AgentMessageDelivery{}
	if err = tx.SelectContext(ctx, &candidates, deliverySelect+` WHERE m.workspace_id=$1 AND m.to_issue_id=$2 AND (m.state='queued'
  OR (m.state='delivered' AND NOT EXISTS (SELECT 1 FROM dev_machine_agent_runs rn WHERE rn.id=m.delivered_run_id) AND NOT EXISTS (SELECT 1 FROM agent_tasks tk WHERE tk.run_id=m.delivered_run_id)))
 ORDER BY m.created_at,m.id LIMIT $3 FOR UPDATE OF m SKIP LOCKED`, task.WorkspaceID, task.IssueID, agentMessagesPerPrompt); err != nil {
		return nil, err
	}
	ids := []uuid.UUID{}
	for _, candidate := range candidates {
		cost := len(candidate.Body) + 200
		if cost > room {
			break
		}
		room -= cost
		deliveries = append(deliveries, candidate)
		ids = append(ids, candidate.ID)
	}
	if len(ids) == 0 {
		return deliveries, nil
	}
	if _, err = tx.ExecContext(ctx, `UPDATE agent_messages SET state='delivered',delivered_run_id=$2,delivered_at=now(),to_task_id=$3,updated_at=now() WHERE id=ANY($1)`, pq.Array(ids), task.RunID, task.ID); err != nil {
		return nil, err
	}
	return deliveries, tx.Commit()
}

// ClaimAgentMessages lets the task runtime place queued messages in a prompt.
func (r *AgentTaskRepository) ClaimAgentMessages(ctx context.Context, task domain.AgentTask, room int) ([]domain.AgentMessageDelivery, error) {
	return NewAgentMessageRepository(r.db).Claim(ctx, task, room)
}

// AgentMessageComment is a comment Kuayle posted, or failed to post, for a
// comment-channel message.
type AgentMessageComment struct {
	MessageID   uuid.UUID
	WorkspaceID uuid.UUID
	IssueID     uuid.UUID
	Identifier  string
	CommentID   uuid.UUID
	Posted      bool
}

// PostNextComment posts one due comment-channel message on its recipient
// issue, authored by the sending task's creator and marked as the sending
// task's agent, in the same transaction that records it as posted. It fails
// the comment instead when that creator can no longer see the issue. It
// returns nil when nothing is due.
func (r *AgentMessageRepository) PostNextComment(ctx context.Context) (*AgentMessageComment, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var due struct {
		ID             uuid.UUID `db:"id"`
		WorkspaceID    uuid.UUID `db:"workspace_id"`
		FromTaskID     uuid.UUID `db:"from_task_id"`
		Body           string    `db:"body"`
		ToIssueID      uuid.UUID `db:"to_issue_id"`
		Identifier     string    `db:"identifier"`
		FromIdentifier string    `db:"from_identifier"`
		CreatorID      uuid.UUID `db:"creator_id"`
		Allowed        bool      `db:"allowed"`
	}
	err = tx.GetContext(ctx, &due, `SELECT m.id,m.workspace_id,m.from_task_id,m.body,m.to_issue_id,ti.identifier_text AS identifier,fi.identifier_text AS from_identifier,ft.creator_id,
  (ti.team_id=fi.team_id AND EXISTS(SELECT 1 FROM workspace_members wm WHERE wm.workspace_id=m.workspace_id AND wm.user_id=ft.creator_id AND wm.role<>'guest')
   AND kuayle_team_access(ti.team_id,ft.creator_id,m.workspace_id)) AS allowed
 FROM agent_messages m JOIN agent_tasks ft ON ft.id=m.from_task_id JOIN issues fi ON fi.id=m.from_issue_id JOIN issues ti ON ti.id=m.to_issue_id
 WHERE m.comment_state='pending' AND m.state IN ('queued','delivered') ORDER BY m.created_at,m.id LIMIT 1 FOR UPDATE OF m SKIP LOCKED`)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	result := &AgentMessageComment{MessageID: due.ID, WorkspaceID: due.WorkspaceID, IssueID: due.ToIssueID, Identifier: due.Identifier}
	if !due.Allowed {
		if _, err = tx.ExecContext(ctx, `UPDATE agent_messages SET comment_state='failed',updated_at=now() WHERE id=$1`, due.ID); err != nil {
			return nil, err
		}
		return result, tx.Commit()
	}
	result.CommentID, result.Posted = uuid.New(), true
	if _, err = tx.ExecContext(ctx, `INSERT INTO comments(id,issue_id,user_id,body,agent_task_id) VALUES($1,$2,$3,$4,$5)`, result.CommentID, due.ToIssueID, due.CreatorID, agentCommentBody(due.FromIdentifier, due.Body), due.FromTaskID); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE agent_messages SET comment_state='posted',comment_id=$2,updated_at=now() WHERE id=$1`, due.ID, result.CommentID); err != nil {
		return nil, err
	}
	return result, tx.Commit()
}

// agentCommentBody renders an agent's plain text as escaped comment HTML,
// one paragraph per line, under a line naming the sending agent.
func agentCommentBody(from, body string) string {
	var b strings.Builder
	b.WriteString("<p><strong>Message from the agent working on " + html.EscapeString(from) + "</strong></p>")
	for _, line := range strings.Split(body, "\n") {
		b.WriteString("<p>" + html.EscapeString(line) + "</p>")
	}
	return b.String()
}

// moderationAccess resolves the reader: a current non-guest member of ws.
func (r *AgentMessageRepository) reader(ctx context.Context, q sqlx.QueryerContext, ws uuid.UUID) (uuid.UUID, error) {
	actor, err := conversationActor(ctx, ws)
	if err != nil {
		return uuid.Nil, ErrAgentMessageNotFound
	}
	var member bool
	if err = sqlx.GetContext(ctx, q, &member, `SELECT EXISTS(SELECT 1 FROM workspace_members WHERE workspace_id=$1 AND user_id=$2 AND role<>'guest')`, ws, actor); err != nil {
		return uuid.Nil, err
	}
	if !member {
		return uuid.Nil, ErrAgentMessageNotFound
	}
	return actor, nil
}

// viewSelect reads messages the reader may see: those whose sending issue is
// visible to them. The recipient is named only when it is visible too.
func viewSelect(ctx context.Context, actor uuid.UUID) string {
	return `SELECT m.*,fi.identifier_text AS from_identifier,fi.title AS from_title,
  CASE WHEN ti.id IS NOT NULL AND ` + teamVisible(ctx, "ti.team_id") + ` THEN ti.identifier_text END AS to_identifier,
  CASE WHEN ti.id IS NOT NULL AND ` + teamVisible(ctx, "ti.team_id") + ` THEN ti.title END AS to_title,
  hu.name AS held_by_name,ru.name AS released_by_name,du.name AS dropped_by_name,h.id AS conversation_hold_id,
  (` + workspaceAdmin(ctx, "m.workspace_id") + ` OR ft.creator_id=` + uuidSQL(actor) + ` OR COALESCE(tt.creator_id=` + uuidSQL(actor) + `,FALSE)) AS can_moderate
 FROM agent_messages m JOIN issues fi ON fi.id=m.from_issue_id JOIN agent_tasks ft ON ft.id=m.from_task_id
 LEFT JOIN issues ti ON ti.id=m.to_issue_id LEFT JOIN agent_tasks tt ON tt.id=m.to_task_id
 LEFT JOIN users hu ON hu.id=m.held_by LEFT JOIN users ru ON ru.id=m.released_by LEFT JOIN users du ON du.id=m.dropped_by
 LEFT JOIN agent_message_holds h ON m.to_issue_id IS NOT NULL AND h.issue_a=LEAST(m.from_issue_id,m.to_issue_id) AND h.issue_b=GREATEST(m.from_issue_id,m.to_issue_id)
 WHERE m.workspace_id=$1 AND ` + teamVisible(ctx, "fi.team_id")
}

// List pages the registry, newest first, 50 per page.
func (r *AgentMessageRepository) List(ctx context.Context, ws uuid.UUID, filter domain.AgentMessageFilter) (*domain.AgentMessagePage, error) {
	actor, err := r.reader(ctx, r.db, ws)
	if err != nil {
		return nil, err
	}
	query, args := viewSelect(ctx, actor), []any{ws}
	add := func(clause string, value any) {
		args = append(args, value)
		query += " AND " + strings.ReplaceAll(clause, "?", fmt.Sprintf("$%d", len(args)))
	}
	if filter.TaskID != nil {
		add("(m.from_task_id=? OR m.to_task_id=?)", *filter.TaskID)
	}
	if filter.IssueID != nil {
		add("(m.from_issue_id=? OR m.to_issue_id=?)", *filter.IssueID)
	}
	if filter.Issue != "" {
		add("(fi.identifier_text=? OR (ti.identifier_text=? AND "+teamVisible(ctx, "ti.team_id")+"))", filter.Issue)
	}
	if filter.State != "" {
		add("m.state=?", filter.State)
	}
	if filter.Channel != "" {
		add("m.channel=?", filter.Channel)
	}
	if filter.Before != nil {
		add("(m.created_at,m.id)<(SELECT b.created_at,b.id FROM agent_messages b WHERE b.id=? AND b.workspace_id=$1)", *filter.Before)
	}
	const pageSize = 50
	query += fmt.Sprintf(" ORDER BY m.created_at DESC,m.id DESC LIMIT %d", pageSize+1)
	messages := []domain.AgentMessageView{}
	if err = r.db.SelectContext(ctx, &messages, query, args...); err != nil {
		return nil, err
	}
	for i := range messages {
		maskRecipient(&messages[i])
	}
	page := &domain.AgentMessagePage{Messages: messages}
	if len(messages) > pageSize {
		page.Messages = messages[:pageSize]
		page.NextBefore = &messages[pageSize-1].ID
	}
	return page, nil
}

// maskRecipient hides what the agent wrote as the recipient when the reader
// cannot see the issue it resolved to (it moved to another team).
func maskRecipient(message *domain.AgentMessageView) {
	if message.ToIssueID != nil && message.ToIdentifier == nil {
		message.ToRef = ""
	}
}

// Get reads one visible message and its audited actions.
func (r *AgentMessageRepository) Get(ctx context.Context, ws, id uuid.UUID) (*domain.AgentMessageView, []domain.AgentMessageAction, error) {
	actor, err := r.reader(ctx, r.db, ws)
	if err != nil {
		return nil, nil, err
	}
	var message domain.AgentMessageView
	if err = r.db.GetContext(ctx, &message, viewSelect(ctx, actor)+` AND m.id=$2`, ws, id); errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrAgentMessageNotFound
	} else if err != nil {
		return nil, nil, err
	}
	maskRecipient(&message)
	actions := []domain.AgentMessageAction{}
	err = r.db.SelectContext(ctx, &actions, `SELECT a.id,a.message_id,a.action,a.actor_id,u.name AS actor_name,a.created_at FROM agent_message_actions a LEFT JOIN users u ON u.id=a.actor_id WHERE a.workspace_id=$1 AND a.message_id=$2 ORDER BY a.id`, ws, id)
	return &message, actions, err
}

// Act holds, releases or drops one message for a moderator: a workspace
// administrator or the creator of the sending or receiving task. Holding
// applies only before delivery. Every action is audited.
func (r *AgentMessageRepository) Act(ctx context.Context, ws, id uuid.UUID, action string) (*domain.AgentMessageView, error) {
	actor, err := r.reader(ctx, r.db, ws)
	if err != nil {
		return nil, err
	}
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var message domain.AgentMessageView
	if err = tx.GetContext(ctx, &message, viewSelect(ctx, actor)+` AND m.id=$2 FOR UPDATE OF m`, ws, id); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAgentMessageNotFound
	} else if err != nil {
		return nil, err
	}
	if !message.CanModerate {
		return nil, ErrAgentMessageForbidden
	}
	var update string
	switch {
	case action == "hold" && message.State == "queued":
		update = `state='held',held_by=$2,held_at=now(),comment_state=CASE WHEN comment_state='pending' THEN 'none' ELSE comment_state END`
	case action == "release" && message.State == "held":
		update = `state='queued',released_by=$2,released_at=now(),comment_state=CASE WHEN channel='comment' AND comment_id IS NULL THEN 'pending' ELSE comment_state END`
	case action == "drop" && (message.State == "queued" || message.State == "held"):
		update = `state='dropped',dropped_by=$2,dropped_at=now(),comment_state=CASE WHEN comment_state='pending' THEN 'none' ELSE comment_state END`
	case action != "hold" && action != "release" && action != "drop":
		return nil, ErrAgentMessageInvalid
	default:
		return nil, ErrAgentMessageConflict
	}
	if _, err = tx.ExecContext(ctx, `UPDATE agent_messages SET `+update+`,updated_at=now() WHERE id=$1`, id, actor); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO agent_message_actions(workspace_id,message_id,action,actor_id) VALUES($1,$2,$3,$4)`, ws, id, action, actor); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	view, _, err := r.Get(ctx, ws, id)
	return view, err
}

func holdSelect(ctx context.Context, actor uuid.UUID) string {
	return `SELECT h.id,h.issue_a,h.issue_b,a.identifier_text AS issue_a_identifier,b.identifier_text AS issue_b_identifier,h.created_by,u.name AS created_by_name,h.created_at,
  (SELECT count(*) FROM agent_messages m WHERE m.workspace_id=h.workspace_id AND m.state='held' AND LEAST(m.from_issue_id,m.to_issue_id)=h.issue_a AND GREATEST(m.from_issue_id,m.to_issue_id)=h.issue_b) AS held_messages,
  (` + workspaceAdmin(ctx, "h.workspace_id") + ` OR EXISTS(SELECT 1 FROM agent_tasks t WHERE t.workspace_id=h.workspace_id AND t.issue_id IN (h.issue_a,h.issue_b) AND t.kind='implement' AND t.creator_id=` + uuidSQL(actor) + `)) AS can_moderate
 FROM agent_message_holds h JOIN issues a ON a.id=h.issue_a JOIN issues b ON b.id=h.issue_b LEFT JOIN users u ON u.id=h.created_by
 WHERE h.workspace_id=$1 AND ` + teamVisible(ctx, "a.team_id") + ` AND ` + teamVisible(ctx, "b.team_id")
}

// Holds lists the held conversations whose issues the reader can see.
func (r *AgentMessageRepository) Holds(ctx context.Context, ws uuid.UUID) ([]domain.AgentMessageHold, error) {
	actor, err := r.reader(ctx, r.db, ws)
	if err != nil {
		return nil, err
	}
	holds := []domain.AgentMessageHold{}
	err = r.db.SelectContext(ctx, &holds, holdSelect(ctx, actor)+` ORDER BY h.created_at DESC LIMIT 100`, ws)
	return holds, err
}

// HoldConversation holds the messages between two issues' agents: queued
// ones now and new ones until released. Moderators are workspace
// administrators and creators of either issue's implementing tasks.
func (r *AgentMessageRepository) HoldConversation(ctx context.Context, ws, first, second uuid.UUID) (*domain.AgentMessageHold, error) {
	actor, err := r.reader(ctx, r.db, ws)
	if err != nil {
		return nil, err
	}
	if first == second || first == uuid.Nil || second == uuid.Nil {
		return nil, ErrAgentMessageInvalid
	}
	low, high := first, second
	if strings.Compare(low.String(), high.String()) > 0 {
		low, high = high, low
	}
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var visible int
	if err = tx.GetContext(ctx, &visible, `SELECT count(*) FROM issues i WHERE i.id IN ($1,$2) AND i.workspace_id=$3 AND `+teamVisible(ctx, "i.team_id"), low, high, ws); err != nil {
		return nil, err
	}
	if visible != 2 {
		return nil, ErrAgentMessageNotFound
	}
	var allowed bool
	if err = tx.GetContext(ctx, &allowed, `SELECT `+workspaceAdmin(ctx, "$1::uuid")+` OR EXISTS(SELECT 1 FROM agent_tasks t WHERE t.workspace_id=$1 AND t.issue_id IN ($2,$3) AND t.kind='implement' AND t.creator_id=$4)`, ws, low, high, actor); err != nil {
		return nil, err
	}
	if !allowed {
		return nil, ErrAgentMessageForbidden
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO agent_message_holds(workspace_id,issue_a,issue_b,created_by) VALUES($1,$2,$3,$4) ON CONFLICT (issue_a,issue_b) DO NOTHING`, ws, low, high, actor); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE agent_messages SET state='held',held_by=$4,held_at=now(),comment_state=CASE WHEN comment_state='pending' THEN 'none' ELSE comment_state END,updated_at=now()
 WHERE workspace_id=$1 AND state='queued' AND LEAST(from_issue_id,to_issue_id)=$2 AND GREATEST(from_issue_id,to_issue_id)=$3`, ws, low, high, actor); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO agent_message_actions(workspace_id,issue_a,issue_b,action,actor_id) VALUES($1,$2,$3,'hold_conversation',$4)`, ws, low, high, actor); err != nil {
		return nil, err
	}
	var hold domain.AgentMessageHold
	if err = tx.GetContext(ctx, &hold, holdSelect(ctx, actor)+` AND h.issue_a=$2 AND h.issue_b=$3`, ws, low, high); err != nil {
		return nil, err
	}
	return &hold, tx.Commit()
}

// ReleaseConversation ends a held conversation and releases its held
// messages for delivery.
func (r *AgentMessageRepository) ReleaseConversation(ctx context.Context, ws, id uuid.UUID) error {
	actor, err := r.reader(ctx, r.db, ws)
	if err != nil {
		return err
	}
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var hold domain.AgentMessageHold
	if err = tx.GetContext(ctx, &hold, holdSelect(ctx, actor)+` AND h.id=$2 FOR UPDATE OF h`, ws, id); errors.Is(err, sql.ErrNoRows) {
		return ErrAgentMessageNotFound
	} else if err != nil {
		return err
	}
	if !hold.CanModerate {
		return ErrAgentMessageForbidden
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM agent_message_holds WHERE id=$1`, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE agent_messages SET state='queued',released_by=$4,released_at=now(),comment_state=CASE WHEN channel='comment' AND comment_id IS NULL THEN 'pending' ELSE comment_state END,updated_at=now()
 WHERE workspace_id=$1 AND state='held' AND LEAST(from_issue_id,to_issue_id)=$2 AND GREATEST(from_issue_id,to_issue_id)=$3`, ws, hold.IssueA, hold.IssueB, actor); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO agent_message_actions(workspace_id,issue_a,issue_b,action,actor_id) VALUES($1,$2,$3,'release_conversation',$4)`, ws, hold.IssueA, hold.IssueB, actor); err != nil {
		return err
	}
	return tx.Commit()
}
