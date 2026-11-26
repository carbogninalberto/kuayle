package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

var ErrScheduledJobLimit = errors.New("scheduled job limit reached")

func (r *ConversationRepository) CreateScheduledJob(ctx context.Context, a domain.ConversationAttempt, j domain.ConversationJob) (*domain.ConversationJob, error) {
	tx, c, err := r.authorized(ctx, a.Conversation.WorkspaceID, a.Conversation.ID)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = fenceConversationAttempt(ctx, tx, a); err != nil {
		return nil, err
	}
	// Serialize quota checks across conversations, including one user in multiple workspaces.
	for _, key := range []string{"jobs.user." + c.CreatorID.String(), "jobs.workspace." + c.WorkspaceID.String()} {
		if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, key); err != nil {
			return nil, err
		}
	}
	var allowed bool
	if err = tx.GetContext(ctx, &allowed, `SELECT count(*) FILTER (WHERE conversation_id=$1)<10
 AND count(*) FILTER (WHERE user_id=$2)<30 AND count(*) FILTER (WHERE workspace_id=$3)<200
 FROM conversation_jobs WHERE state='scheduled' AND (user_id=$2 OR workspace_id=$3)`, c.ID, c.CreatorID, c.WorkspaceID); err != nil {
		return nil, err
	}
	if !allowed {
		return nil, ErrScheduledJobLimit
	}
	if err = fillConversationProject(ctx, tx, c); err != nil {
		return nil, err
	}
	err = tx.GetContext(ctx, &j, `INSERT INTO conversation_jobs(workspace_id,user_id,conversation_id,parent_id,team_id,project_id,issue_id,kind,spec,action,instructions,next_run_at,deadline,timezone)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) RETURNING *`, c.WorkspaceID, c.CreatorID, c.ID, a.Message.ParentID, c.TeamID, c.ProjectID, c.IssueID, j.Kind, j.Spec, j.Action, j.Instructions, j.NextRunAt, j.Deadline, j.Timezone)
	if err != nil {
		return nil, err
	}
	return commitJob(ctx, tx, &j, nil)
}

func commitJob(ctx context.Context, tx *sqlx.Tx, j *domain.ConversationJob, message *domain.ConversationMessage) (*domain.ConversationJob, error) {
	if message != nil {
		if _, err := tx.ExecContext(ctx, `UPDATE conversations SET updated_at=now() WHERE id=$1`, j.ConversationID); err != nil {
			return nil, err
		}
		// addConversationMessage already queued deliveries for links pointing at
		// this conversation. Also reach the creator when Telegram is open elsewhere.
		if _, err := tx.ExecContext(ctx, `INSERT INTO telegram_deliveries(workspace_id,user_id,telegram_user_id,conversation_id,message_id)
  SELECT l.workspace_id,l.user_id,l.telegram_user_id,$3,$4 FROM telegram_links l JOIN telegram_settings s ON s.workspace_id=l.workspace_id
  WHERE l.workspace_id=$1 AND l.user_id=$2 AND s.enabled AND l.telegram_user_id=ANY(s.allowed_user_ids)
  ON CONFLICT DO NOTHING`, j.WorkspaceID, j.UserID, j.ConversationID, message.ID); err != nil {
			return nil, err
		}
	}

	if _, err := conversationEvent(ctx, tx, j.ConversationID, "scheduled_job.updated", j); err != nil {
		return nil, err
	}
	return j, tx.Commit()
}

func (r *ConversationRepository) ListScheduledJobs(ctx context.Context, ws, conversation uuid.UUID) ([]domain.ConversationJob, error) {
	tx, _, err := r.authorized(ctx, ws, conversation)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	jobs := []domain.ConversationJob{}
	err = tx.SelectContext(ctx, &jobs, `SELECT * FROM conversation_jobs WHERE conversation_id=$1 ORDER BY (state='scheduled') DESC,next_run_at DESC,id LIMIT 20`, conversation)
	return jobs, err
}

func (r *ConversationRepository) ClaimScheduledJob(ctx context.Context, lease time.Duration) (*domain.ConversationJob, error) {
	var j domain.ConversationJob
	err := r.db.GetContext(ctx, &j, `WITH due AS (SELECT id FROM conversation_jobs WHERE state='scheduled'
 AND next_run_at<=now() AND (lease_until IS NULL OR lease_until<=now()) ORDER BY next_run_at,id LIMIT 1 FOR UPDATE SKIP LOCKED)
 UPDATE conversation_jobs j SET lease_token=gen_random_uuid(),lease_until=now()+make_interval(secs=>$1),attempts=attempts+1
 FROM due WHERE j.id=due.id RETURNING j.*`, lease.Seconds())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &j, err
}

// Drop an inaccessible claim without putting its former conversation or issue into a notification.
func (r *ConversationRepository) RetireInaccessibleJob(ctx context.Context, j domain.ConversationJob) error {
	_, err := r.db.ExecContext(ctx, `UPDATE conversation_jobs SET state='cancelled',last_outcome='Access removed',lease_token=NULL,lease_until=NULL
 WHERE id=$1 AND lease_token=$2 AND state='scheduled'`, j.ID, j.LeaseToken)
	return err
}

func (r *ConversationRepository) CancelScheduledJob(ctx context.Context, ws, conversation, id uuid.UUID) (*domain.ConversationJob, bool, error) {
	tx, _, err := r.authorized(ctx, ws, conversation)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	var j domain.ConversationJob
	err = tx.GetContext(ctx, &j, `SELECT * FROM conversation_jobs WHERE id=$1 AND conversation_id=$2 FOR UPDATE`, id, conversation)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, ErrConversationNotFound
	}
	if err != nil {
		return nil, false, err
	}
	if j.State != "scheduled" {
		return &j, false, nil
	}
	err = tx.GetContext(ctx, &j, `UPDATE conversation_jobs SET state='cancelled',last_outcome='Cancelled',last_error='',lease_token=NULL,lease_until=NULL WHERE id=$1 RETURNING *`, id)
	if err != nil {
		return nil, false, err
	}
	notice, err := addConversationMessage(ctx, tx, conversation, uuid.New(), &j.ParentID, "assistant", "Scheduled job cancelled: "+j.Instructions, "completed", "", "")
	if err != nil {
		return nil, false, err
	}
	saved, err := commitJob(ctx, tx, &j, notice)
	return saved, err == nil, err
}

type JobOutcome struct {
	ConditionMet            bool
	ProbeRunID              *uuid.UUID
	ApprovalProbe           json.RawMessage
	State, Summary, Error   string
	Next                    time.Time
	Notice, Provider, Model string
	Fired                   bool
}

// Commit the generation (or notice), card change and occurrence together. A
// cancelled or replaced lease cannot write even if its probe has just returned.
func (r *ConversationRepository) FinishScheduledJob(ctx context.Context, j domain.ConversationJob, o JobOutcome) (*domain.ConversationJob, error) {
	tx, _, err := r.authorized(ctx, j.WorkspaceID, j.ConversationID)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var expired bool
	err = tx.GetContext(ctx, &expired, `SELECT deadline<=clock_timestamp() FROM conversation_jobs
 WHERE id=$1 AND conversation_id=$2 AND state='scheduled' AND lease_token=$3 AND lease_until>clock_timestamp() FOR UPDATE`, j.ID, j.ConversationID, j.LeaseToken)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrConversationConflict
	}
	if err != nil {
		return nil, err
	}
	if expired {
		o = JobOutcome{State: "expired", Summary: "Expired", Notice: "Scheduled job expired: " + j.Instructions}
	}
	request := uuid.NewSHA1(j.ID, []byte(strconv.Itoa(j.FireCount)))
	var message *domain.ConversationMessage
	switch {
	case o.Model != "":
		if err = activeConversation(ctx, tx, j.ConversationID); err != nil {
			return nil, err
		}
		parent, e := addConversationMessage(ctx, tx, j.ConversationID, request, nil, "user", j.Instructions, "completed", "", "")
		if e != nil {
			return nil, e
		}
		message, err = addJobReply(ctx, tx, j.ConversationID, request, &parent.ID, o.Provider, o.Model, &j.ID, o.ApprovalProbe)
	case o.Notice != "":
		message, err = addConversationMessage(ctx, tx, j.ConversationID, request, &j.ParentID, "assistant", o.Notice, "completed", "", "")
	}
	if err != nil {
		return nil, err
	}
	if o.Next.IsZero() || o.Next.After(j.Deadline) {
		o.Next = j.Deadline
	}
	err = tx.GetContext(ctx, &j, `UPDATE conversation_jobs SET state=$2,last_outcome=$3,last_error=$4,next_run_at=$5,probe_run_id=$7,condition_met=condition_met OR $8,lease_token=NULL,lease_until=NULL,
 attempts=CASE WHEN $4='' THEN 0 ELSE attempts END,fire_count=fire_count+$6::boolean::int,fired_at=CASE WHEN $6 THEN now() ELSE fired_at END WHERE id=$1 RETURNING *`, j.ID, o.State, o.Summary, o.Error, o.Next, o.Fired, o.ProbeRunID, o.ConditionMet)
	if err != nil {
		return nil, err
	}
	return commitJob(ctx, tx, &j, message)
}

func addJobReply(ctx context.Context, tx *sqlx.Tx, conversation, request uuid.UUID, parent *uuid.UUID, provider, model string, job *uuid.UUID, probe json.RawMessage) (*domain.ConversationMessage, error) {
	return addConversationMessageWith(ctx, tx, conversation, request, parent, "assistant", "", "queued", provider, model, func(m *domain.ConversationMessage) error {
		if job == nil {
			return nil
		}
		m.ScheduledJobID, m.ScheduledProbe = job, probe
		_, err := tx.ExecContext(ctx, `UPDATE conversation_messages SET scheduled_job_id=$2,scheduled_probe=$3 WHERE id=$1`, m.ID, job, probe)
		return err
	})
}
