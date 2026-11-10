package repository

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/lib/pq"
)

var (
	// ErrAgentAssignForbidden: the actor is a guest or not a member.
	ErrAgentAssignForbidden = errors.New("only workspace members can assign agents")
	// ErrAgentNotAssignable: the agent is archived, foreign, or not enabled
	// for the issue's team.
	ErrAgentNotAssignable = errors.New("the agent cannot be assigned in this team")
	ErrAgentIssueNotFound = errors.New("issue not found")
	// ErrAgentIdentityDenied: only owners and administrators manage agents.
	ErrAgentIdentityDenied   = errors.New("only workspace administrators manage agents")
	ErrAgentIdentityNotFound = errors.New("agent not found")
	ErrAgentIdentityInvalid  = errors.New("invalid agent settings")
)

// AgentAssigneeRepository stores agent identities, the teams they can be
// assigned in, and the agents assigned to issues. issue_assignees (people)
// is not touched here.
type AgentAssigneeRepository struct{ db *sqlx.DB }

func NewAgentAssigneeRepository(db *sqlx.DB) *AgentAssigneeRepository {
	return &AgentAssigneeRepository{db: db}
}

const agentIdentityColumns = `p.id,p.workspace_id,p.name,p.role,p.harness,p.model,p.effort,p.builtin_key,p.archived_at,COALESCE(ai.avatar_color,'') AS avatar_color`

// Identities lists the workspace's active agents with the teams, among those
// the caller can see, where each can be assigned.
func (r *AgentAssigneeRepository) Identities(ctx context.Context, ws uuid.UUID) ([]domain.AgentIdentity, error) {
	identities := []domain.AgentIdentity{}
	err := r.db.SelectContext(ctx, &identities, `SELECT `+agentIdentityColumns+` FROM agent_profiles p LEFT JOIN agent_identities ai ON ai.profile_id=p.id
 WHERE p.workspace_id=$1 AND p.archived_at IS NULL ORDER BY p.builtin_key IS NULL, lower(p.name), p.id`, ws)
	if err != nil {
		return nil, err
	}
	var policies []struct {
		ProfileID uuid.UUID `db:"profile_id"`
		TeamID    uuid.UUID `db:"team_id"`
	}
	err = r.db.SelectContext(ctx, &policies, `SELECT tp.profile_id,tp.team_id FROM agent_team_policies tp JOIN teams t ON t.id=tp.team_id AND t.workspace_id=tp.workspace_id
 WHERE tp.workspace_id=$1 AND tp.assignable AND `+teamVisible(ctx, "tp.team_id")+` ORDER BY t.name, tp.team_id`, ws)
	if err != nil {
		return nil, err
	}
	teams := map[uuid.UUID][]uuid.UUID{}
	for _, policy := range policies {
		teams[policy.ProfileID] = append(teams[policy.ProfileID], policy.TeamID)
	}
	for i := range identities {
		identities[i].TeamIDs = teams[identities[i].ID]
		if identities[i].TeamIDs == nil {
			identities[i].TeamIDs = []uuid.UUID{}
		}
	}
	return identities, nil
}

// UpdateIdentity sets an agent's avatar colour (nil keeps it) and the teams
// where it can be assigned (nil keeps them). The actor must be a current
// owner or administrator, checked inside the writing transaction; every team
// must belong to the workspace.
func (r *AgentAssigneeRepository) UpdateIdentity(ctx context.Context, ws, profile, actor uuid.UUID, avatarColor *string, teamIDs *[]uuid.UUID) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var role string
	err = tx.GetContext(ctx, &role, `SELECT role FROM workspace_members WHERE workspace_id=$1 AND user_id=$2 AND role IN ('owner','admin') FOR SHARE`, ws, actor)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrAgentIdentityDenied
	}
	if err != nil {
		return err
	}
	var found uuid.UUID
	err = tx.GetContext(ctx, &found, `SELECT id FROM agent_profiles WHERE id=$1 AND workspace_id=$2 AND archived_at IS NULL FOR UPDATE`, profile, ws)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrAgentIdentityNotFound
	}
	if err != nil {
		return err
	}
	if avatarColor != nil {
		if _, err = tx.ExecContext(ctx, `INSERT INTO agent_identities(profile_id,workspace_id,avatar_color,updated_by) VALUES($1,$2,$3,$4)
 ON CONFLICT (profile_id) DO UPDATE SET avatar_color=EXCLUDED.avatar_color,updated_by=EXCLUDED.updated_by,updated_at=now()`, profile, ws, *avatarColor, actor); err != nil {
			return err
		}
	}
	if teamIDs != nil {
		ids := distinctUUIDs(*teamIDs)
		if _, err = tx.ExecContext(ctx, `UPDATE agent_team_policies SET assignable=false,updated_by=$3,updated_at=now() WHERE profile_id=$1 AND assignable AND NOT (team_id = ANY($2))`, profile, pq.Array(ids), actor); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO agent_team_policies(profile_id,team_id,workspace_id,assignable,updated_by)
 SELECT $1,t.id,$2,true,$4 FROM teams t WHERE t.id = ANY($3) AND t.workspace_id=$2
 ON CONFLICT (profile_id,team_id) DO UPDATE SET assignable=true,updated_by=EXCLUDED.updated_by,updated_at=now()`, profile, ws, pq.Array(ids), actor)
		if err != nil {
			return err
		}
		if n, _ := result.RowsAffected(); int(n) != len(ids) {
			return ErrAgentIdentityInvalid
		}
	}
	return tx.Commit()
}

func distinctUUIDs(ids []uuid.UUID) []uuid.UUID {
	seen := make(map[uuid.UUID]bool, len(ids))
	out := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

const issueAgentColumns = `a.issue_id,a.profile_id,a.workspace_id,p.name,p.role,p.harness,p.model,COALESCE(ai.avatar_color,'') AS avatar_color,a.assigned_by,
 (SELECT COALESCE(NULLIF(u.display_name,''),u.name) FROM users u WHERE u.id=a.assigned_by) AS assigned_by_name,
 a.request_id,a.task_id,t.state AS task_state,t.reason AS task_reason,a.error,a.created_at`

const issueAgentJoins = ` FROM issue_agent_assignees a JOIN agent_profiles p ON p.id=a.profile_id
 LEFT JOIN agent_identities ai ON ai.profile_id=p.id LEFT JOIN agent_tasks t ON t.id=a.task_id`

// ForIssues returns the agents assigned to each issue the caller can see,
// with the state of each agent's task.
func (r *AgentAssigneeRepository) ForIssues(ctx context.Context, issueIDs []uuid.UUID) (map[uuid.UUID][]domain.IssueAgentAssignee, error) {
	result := make(map[uuid.UUID][]domain.IssueAgentAssignee, len(issueIDs))
	if len(issueIDs) == 0 {
		return result, nil
	}
	var rows []domain.IssueAgentAssignee
	err := r.db.SelectContext(ctx, &rows, `SELECT `+issueAgentColumns+issueAgentJoins+`
 WHERE a.issue_id = ANY($1) AND `+issueVisible(ctx, "a.issue_id")+` ORDER BY a.created_at, p.name, a.profile_id`, pq.Array(issueIDs))
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.IssueID] = append(result[row.IssueID], row)
	}
	return result, nil
}

// AgentAssignment is an assignment whose task has to be started or stopped.
type AgentAssignment struct {
	IssueID     uuid.UUID  `db:"issue_id"`
	ProfileID   uuid.UUID  `db:"profile_id"`
	WorkspaceID uuid.UUID  `db:"workspace_id"`
	AssignedBy  *uuid.UUID `db:"assigned_by"`
	RequestID   uuid.UUID  `db:"request_id"`
	TaskID      *uuid.UUID `db:"task_id"`
}

// Set replaces the agents assigned to an issue and records the change in the
// issue's history. In one transaction it re-checks that the actor is a
// current non-guest member who can see the issue, and that every added agent
// is active and enabled for the issue's team; otherwise nothing changes. It
// returns the added assignments (their tasks are not started yet) and the
// removed ones.
func (r *AgentAssigneeRepository) Set(ctx context.Context, ws, issue, actor uuid.UUID, profiles []uuid.UUID) (added, removed []AgentAssignment, err error) {
	profiles = distinctUUIDs(profiles)
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	var role string
	err = tx.GetContext(ctx, &role, `SELECT role FROM workspace_members WHERE workspace_id=$1 AND user_id=$2 FOR SHARE`, ws, actor)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && role == "guest") {
		return nil, nil, ErrAgentAssignForbidden
	}
	if err != nil {
		return nil, nil, err
	}
	// The issue row lock serializes concurrent changes to its agents.
	var team uuid.UUID
	err = tx.GetContext(ctx, &team, `SELECT team_id FROM issues WHERE id=$1 AND workspace_id=$2 AND `+teamVisible(ctx, "team_id")+` FOR UPDATE`, issue, ws)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrAgentIssueNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	var current []AgentAssignment
	if err = tx.SelectContext(ctx, &current, `SELECT issue_id,profile_id,workspace_id,assigned_by,request_id,task_id FROM issue_agent_assignees WHERE issue_id=$1 ORDER BY created_at, profile_id`, issue); err != nil {
		return nil, nil, err
	}
	wanted := make(map[uuid.UUID]bool, len(profiles))
	for _, id := range profiles {
		wanted[id] = true
	}
	had := make(map[uuid.UUID]bool, len(current))
	for _, assignment := range current {
		had[assignment.ProfileID] = true
		if !wanted[assignment.ProfileID] {
			removed = append(removed, assignment)
		}
	}
	for _, id := range profiles {
		if had[id] {
			continue
		}
		var assignment AgentAssignment
		err = tx.GetContext(ctx, &assignment, `INSERT INTO issue_agent_assignees(issue_id,profile_id,workspace_id,assigned_by)
 SELECT $1,p.id,$2,$3 FROM agent_profiles p JOIN agent_team_policies tp ON tp.profile_id=p.id AND tp.team_id=$4 AND tp.assignable
 WHERE p.id=$5 AND p.workspace_id=$2 AND p.archived_at IS NULL
 RETURNING issue_id,profile_id,workspace_id,assigned_by,request_id,task_id`, issue, ws, actor, team, id)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, ErrAgentNotAssignable
		}
		if err != nil {
			return nil, nil, err
		}
		added = append(added, assignment)
	}
	if len(added) == 0 && len(removed) == 0 {
		return nil, nil, nil
	}
	if len(removed) > 0 {
		ids := make([]uuid.UUID, len(removed))
		for i, assignment := range removed {
			ids[i] = assignment.ProfileID
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM issue_agent_assignees WHERE issue_id=$1 AND profile_id = ANY($2)`, issue, pq.Array(ids)); err != nil {
			return nil, nil, err
		}
	}
	before := make([]uuid.UUID, 0, len(current))
	for _, assignment := range current {
		before = append(before, assignment.ProfileID)
	}
	oldValue, newValue := agentIDList(before), agentIDList(profiles)
	if _, err = tx.ExecContext(ctx, `INSERT INTO issue_history(id,issue_id,user_id,field,old_value,new_value,agent) VALUES($1,$2,$3,'agent_assignees',$4,$5,$6)`,
		uuid.New(), issue, actor, oldValue, newValue, domain.ActingAgent(ctx)); err != nil {
		return nil, nil, err
	}
	return added, removed, tx.Commit()
}

// agentIDList is the history value of a set of agents: sorted, comma-separated.
func agentIDList(ids []uuid.UUID) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = id.String()
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

// AgentNames resolves agent IDs of a workspace to names, archived ones
// included, for history display.
func (r *AgentAssigneeRepository) AgentNames(ctx context.Context, ws uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	names := make(map[uuid.UUID]string, len(ids))
	if len(ids) == 0 {
		return names, nil
	}
	var rows []struct {
		ID   uuid.UUID `db:"id"`
		Name string    `db:"name"`
	}
	if err := r.db.SelectContext(ctx, &rows, `SELECT id,name FROM agent_profiles WHERE workspace_id=$1 AND id = ANY($2)`, ws, pq.Array(ids)); err != nil {
		return nil, err
	}
	for _, row := range rows {
		names[row.ID] = row.Name
	}
	return names, nil
}

// Assignable returns the agent's profile when it is active and enabled for
// the team of an issue the caller can see, else ErrAgentNotAssignable.
func (r *AgentAssigneeRepository) Assignable(ctx context.Context, ws, issue, profile uuid.UUID) (*domain.AgentProfile, error) {
	result := &domain.AgentProfile{}
	err := r.db.GetContext(ctx, result, `SELECT p.* FROM agent_profiles p JOIN issues i ON i.id=$2 AND i.workspace_id=p.workspace_id
 JOIN agent_team_policies tp ON tp.profile_id=p.id AND tp.team_id=i.team_id AND tp.assignable
 WHERE p.id=$3 AND p.workspace_id=$1 AND p.archived_at IS NULL AND `+teamVisible(ctx, "i.team_id"), ws, issue, profile)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAgentNotAssignable
	}
	return result, err
}

// AgentStartWindow is how long a start that keeps failing transiently is
// retried before the assignment records an error instead.
const AgentStartWindow = 15 * time.Minute

// ExpirePending records an error on assignments whose task could not be
// started within AgentStartWindow, so a start that never succeeds stops being
// retried and cannot crowd out newer ones. It returns the assignments changed.
func (r *AgentAssigneeRepository) ExpirePending(ctx context.Context) ([]AgentAssignment, error) {
	expired := []AgentAssignment{}
	err := r.db.SelectContext(ctx, &expired, `UPDATE issue_agent_assignees SET error='The task could not be started; assign the agent again to retry'
 WHERE task_id IS NULL AND error IS NULL AND created_at < now()-make_interval(secs => $1)
 RETURNING issue_id,profile_id,workspace_id,assigned_by,request_id,task_id`, AgentStartWindow.Seconds())
	return expired, err
}

// Pending lists assignments whose task was not started yet and that did not
// fail for good: a crash or a transient failure interrupted the start. Rows
// older than AgentStartWindow are left to ExpirePending.
func (r *AgentAssigneeRepository) Pending(ctx context.Context, limit int) ([]AgentAssignment, error) {
	pending := []AgentAssignment{}
	err := r.db.SelectContext(ctx, &pending, `SELECT issue_id,profile_id,workspace_id,assigned_by,request_id,task_id FROM issue_agent_assignees
 WHERE task_id IS NULL AND error IS NULL AND assigned_by IS NOT NULL AND created_at < now()-interval '20 seconds'
 AND created_at >= now()-make_interval(secs => $2) ORDER BY created_at LIMIT $1`, limit, AgentStartWindow.Seconds())
	return pending, err
}

// Finish records the task an assignment started, or why none could start.
// The request ID fences an assignment that was removed and made again. It
// reports whether the assignment was updated: false means it is gone (the
// agent was unassigned meanwhile) or was already settled.
func (r *AgentAssigneeRepository) Finish(ctx context.Context, assignment AgentAssignment, task *uuid.UUID, failure string) (bool, error) {
	message := sql.NullString{String: failure, Valid: task == nil}
	result, err := r.db.ExecContext(ctx, `UPDATE issue_agent_assignees SET task_id=$4,error=left($5,1024) WHERE issue_id=$1 AND profile_id=$2 AND request_id=$3 AND task_id IS NULL AND error IS NULL`,
		assignment.IssueID, assignment.ProfileID, assignment.RequestID, task, message)
	if err != nil {
		return false, err
	}
	n, _ := result.RowsAffected()
	return n > 0, nil
}

// MentionedAgents returns, among ids, the agents the caller can mention on
// the issue: active profiles enabled for its team. The caller must be a
// current non-guest member who can see the issue.
func (r *AgentAssigneeRepository) MentionedAgents(ctx context.Context, ws, issue, actor uuid.UUID, ids []uuid.UUID) ([]domain.AgentProfile, error) {
	profiles := []domain.AgentProfile{}
	if len(ids) == 0 {
		return profiles, nil
	}
	err := r.db.SelectContext(ctx, &profiles, `SELECT p.* FROM agent_profiles p JOIN issues i ON i.id=$2 AND i.workspace_id=p.workspace_id
 JOIN agent_team_policies tp ON tp.profile_id=p.id AND tp.team_id=i.team_id AND tp.assignable
 JOIN workspace_members wm ON wm.workspace_id=p.workspace_id AND wm.user_id=$3 AND wm.role<>'guest'
 WHERE p.workspace_id=$1 AND p.archived_at IS NULL AND p.id = ANY($4) AND `+teamVisible(ctx, "i.team_id")+` ORDER BY p.id`, ws, issue, actor, pq.Array(ids))
	return profiles, err
}

// OpenAgentTask returns the agent's newest implementing task on the issue
// that is not finished or being cancelled, or nil.
func (r *AgentAssigneeRepository) OpenAgentTask(ctx context.Context, ws, issue, profile uuid.UUID) (*domain.AgentTask, error) {
	task := &domain.AgentTask{}
	err := r.db.GetContext(ctx, task, `SELECT * FROM agent_tasks WHERE workspace_id=$1 AND issue_id=$2 AND agent_profile_id=$3 AND kind='implement'
 AND state NOT IN ('completed','cancelled','cancelling') ORDER BY created_at DESC, id LIMIT 1`, ws, issue, profile)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return task, err
}

// TaskStatus is a task's current state and reason, read without
// authorization for the worker's own change detection.
func (r *AgentTaskRepository) TaskStatus(ctx context.Context, id uuid.UUID) (state, reason string, err error) {
	var row struct {
		State  string `db:"state"`
		Reason string `db:"reason"`
	}
	err = r.db.GetContext(ctx, &row, `SELECT state,reason FROM agent_tasks WHERE id=$1`, id)
	return row.State, row.Reason, err
}

// CancelUnassigned requests cancellation of the task an unassigned agent was
// running on the issue, on behalf of the member who unassigned it. Only a
// task still working is cancelled (domain.AgentTaskWorking): one awaiting
// review keeps its pull request. The caller has already authorized the
// unassignment; the task must be the agent's on that issue and no longer
// referenced by an assignment. It reports whether cancellation was requested.
func (r *AgentTaskRepository) CancelUnassigned(ctx context.Context, ws, id, issue, profile, actor uuid.UUID) (bool, error) {
	tx, task, err := r.transaction(ctx, id, false)
	if errors.Is(err, ErrConversationNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if task.WorkspaceID != ws || task.IssueID != issue || task.AgentProfileID == nil || *task.AgentProfileID != profile || !domain.AgentTaskWorking(task.State) {
		return false, nil
	}
	var reassigned bool
	if err = tx.GetContext(ctx, &reassigned, `SELECT EXISTS(SELECT 1 FROM issue_agent_assignees WHERE task_id=$1)`, id); err != nil || reassigned {
		return false, err
	}
	if err = transitionAgentTask(ctx, tx, task, "cancelling", "The agent was unassigned from the issue", &actor); err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE agent_tasks SET lease_token=NULL,lease_expires_at=NULL,next_poll_at=now() WHERE id=$1`, id); err != nil {
		return false, err
	}
	return true, tx.Commit()
}
