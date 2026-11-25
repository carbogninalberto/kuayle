package repository

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// boardColumns reads a task's row of the board (KUA-262) from Kuayle's own
// records:
//   - the pull request: the newest one Kuayle published from any attempt of
//     the task (a later attempt that has not published yet keeps it);
//   - the head: GitHub's last reported head when it was observed after that
//     publication, otherwise the published commit;
//   - the changed files: the newest attempt that recorded any (an attempt
//     that could not read its branch records none);
//   - CI: the check suites webhooks reported on that head, for the task;
//   - the approval: when a person approved the merge Kuayle is carrying out
//     now, at the task's current workflow step or as an assistant merge
//     waiting for checks. A failed merge waits for a person again and is
//     not counted. On a pipeline, the owner's approval in the tick's memory
//     (pipeline_state: "approved", and the "approved" and "head_sha" facts
//     it last posted) counts while the tick holds it for the board's head,
//     from when its room last heard it hold.
const boardColumns = `t.id,t.issue_id,t.state,t.workflow_node,t.workflow_status,t.updated_at,i.identifier_text,i.title,
 COALESCE(p.repository_full_name,k.repository_full_name) AS repository_full_name,COALESCE(p.pull_request_number,0) AS pull_request_number,
 COALESCE(k.base_branch,'') AS base_branch,COALESCE(head.sha,'') AS head_sha,
 COALESCE((SELECT r.changed_files FROM dev_machine_agent_runs r WHERE r.task_id=t.id AND r.workspace_id=t.workspace_id AND jsonb_typeof(r.changed_files)='array' AND r.changed_files<>'[]'::jsonb ORDER BY r.created_at DESC,r.id DESC LIMIT 1),'[]') AS changed_files,
 COALESCE((SELECT jsonb_agg(DISTINCT s.outcome) FROM agent_task_signals s WHERE s.task_id=t.id AND s.kind='ci' AND s.head_sha=head.sha AND s.head_sha<>''),'[]') AS ci_outcomes,
 COALESCE((SELECT (w.merge->>'since')::timestamptz FROM agent_task_workflow_steps w WHERE w.task_id=t.id AND w.sequence=t.workflow_sequence AND w.finished_at IS NULL
   AND w.merge IS NOT NULL AND COALESCE(w.merge->>'failure','')='' AND NOT COALESCE((w.merge->>'deferred')::boolean,false)),
  (SELECT (a.payload->'wait'->>'since')::timestamptz FROM conversation_approvals a WHERE a.status='waiting_checks' AND a.action='merge_pull_request'
   AND a.issue_id=t.issue_id AND a.payload->>'task_id'=t.id::text ORDER BY a.created_at DESC LIMIT 1),
  (SELECT max(m.created_at) FROM task_messages m WHERE t.pipeline AND COALESCE(t.pipeline_state->'approved'->>'head','')<>''
   AND t.pipeline_state->'facts'->'approved'='true'::jsonb AND t.pipeline_state->'facts'->>'head_sha'=head.sha
   AND m.task_id=t.id AND m.workspace_id=t.workspace_id AND m.author_kind='kuayle' AND m.kind='fact' AND m.data->>'fact'='approved' AND m.data->'value'='true'::jsonb)) AS approved_at`

const boardJoins = ` LEFT JOIN dev_machine_checkouts k ON k.id=t.checkout_id AND k.workspace_id=t.workspace_id
 LEFT JOIN LATERAL (SELECT g.repository_full_name,g.pull_request_number,g.agent_run_id,g.created_at FROM dev_machine_git_refs g JOIN dev_machine_agent_runs r ON r.id=g.agent_run_id AND r.task_id=t.id
  WHERE g.workspace_id=t.workspace_id AND g.issue_id=t.issue_id AND g.ref_type='pull_request' AND g.source='published' AND g.pull_request_number IS NOT NULL ORDER BY g.created_at DESC LIMIT 1) p ON true
 LEFT JOIN LATERAL (SELECT g.commit_sha FROM dev_machine_git_refs g WHERE g.agent_run_id=p.agent_run_id AND g.workspace_id=t.workspace_id AND g.ref_type='commit' AND g.source='published' AND g.commit_sha IS NOT NULL ORDER BY g.created_at DESC LIMIT 1) pc ON true
 LEFT JOIN pull_request_heads h ON h.workspace_id=t.workspace_id AND h.repository_full_name=p.repository_full_name AND h.pull_request_number=p.pull_request_number
 LEFT JOIN LATERAL (SELECT CASE WHEN h.head_sha IS NOT NULL AND (pc.commit_sha IS NULL OR h.observed_at>=p.created_at) THEN h.head_sha ELSE pc.commit_sha END AS sha) head ON true`

// Board lists the active implementing tasks on one repository (matched
// without case) that Queue shows the actor: their own tasks, from their own
// issue conversations whose team they can still see, as a non-guest member
// or with a token holding agent_tasks:read. A task the board cannot place on
// a repository (no checkout and nothing published yet) is left out. The
// service derives the merge order and the overlap.
func (r *AgentTaskRepository) Board(ctx context.Context, ws uuid.UUID, repository string) ([]domain.AgentBoardTask, error) {
	actor, err := conversationActor(ctx, ws)
	if err != nil {
		return nil, err
	}
	tx, err := beginConversationTransaction(ctx, r.db)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = conversationMember(ctx, tx, ws, actor); err != nil {
		return nil, err
	}
	return boardTasks(ctx, tx, ws, actor, repository)
}

// boardTasks is shared by the API and the lead, after the caller has checked
// the actor's workspace membership in this transaction.
func boardTasks(ctx context.Context, tx *sqlx.Tx, ws, actor uuid.UUID, repository string) ([]domain.AgentBoardTask, error) {
	var rows []struct {
		IssueID        uuid.UUID       `db:"issue_id"`
		ID             uuid.UUID       `db:"id"`
		State          string          `db:"state"`
		WorkflowNode   *string         `db:"workflow_node"`
		WorkflowStatus *string         `db:"workflow_status"`
		UpdatedAt      time.Time       `db:"updated_at"`
		Identifier     string          `db:"identifier_text"`
		Title          string          `db:"title"`
		Repository     string          `db:"repository_full_name"`
		PullRequest    int             `db:"pull_request_number"`
		Base           string          `db:"base_branch"`
		Head           string          `db:"head_sha"`
		Files          json.RawMessage `db:"changed_files"`
		Outcomes       json.RawMessage `db:"ci_outcomes"`
		ApprovedAt     *time.Time      `db:"approved_at"`
	}
	err := tx.SelectContext(ctx, &rows, `SELECT `+boardColumns+` FROM `+ownTaskRows+boardJoins+`
 WHERE `+ownTaskVisible()+` AND t.state NOT IN ('completed','failed','cancelled') AND LOWER(COALESCE(p.repository_full_name,k.repository_full_name))=LOWER($3)
 ORDER BY t.updated_at DESC,t.id LIMIT 50`, ws, actor, repository)
	if err != nil {
		return nil, err
	}
	board := make([]domain.AgentBoardTask, 0, len(rows))
	for _, row := range rows {
		task := domain.AgentBoardTask{TaskID: row.ID, IssueID: row.IssueID, IssueIdentifier: row.Identifier, IssueTitle: row.Title, State: row.State,
			WorkflowNode: row.WorkflowNode, WorkflowStatus: row.WorkflowStatus, Repository: row.Repository, PullRequest: row.PullRequest,
			Head: row.Head, Base: row.Base, ApprovedAt: row.ApprovedAt, UpdatedAt: row.UpdatedAt}
		_ = json.Unmarshal(row.Files, &task.ChangedFiles)
		var outcomes []string
		_ = json.Unmarshal(row.Outcomes, &outcomes)
		task.CI = boardCI(outcomes)
		board = append(board, task)
	}
	return board, nil
}

// boardCI classifies the check suites reported on a head: failing when one
// failed, passing when one passed and none failed, empty otherwise.
func boardCI(outcomes []string) string {
	ci := ""
	for _, outcome := range outcomes {
		switch {
		case failedCIOutcomes[outcome]:
			return "failing"
		case passedCIOutcomes[outcome]:
			ci = "passing"
		}
	}
	return ci
}

// leadBoard reads as the locked root's creator in the member-turn transaction.
// Before publication, the checkout places the issue on its repository's board.
func leadBoard(ctx context.Context, tx *sqlx.Tx, root domain.AgentTask, target *domain.AgentReviewTarget) (string, error) {
	var repository string
	if target != nil {
		repository = target.Repository
	} else if err := tx.GetContext(ctx, &repository, `SELECT COALESCE((SELECT repository_full_name FROM dev_machine_checkouts
 WHERE id=$1 AND workspace_id=$2 AND issue_id=$3),'')`, root.CheckoutID, root.WorkspaceID, root.IssueID); err != nil {
		return "", err
	}
	if repository == "" {
		return "The repository is not known yet.", nil
	}
	board, err := boardTasks(ctx, tx, root.WorkspaceID, root.CreatorID, repository)
	if err != nil {
		return "", err
	}
	return domain.LeadBoard(domain.ArrangeBoard(board), root.ID, root.IssueID), nil
}
