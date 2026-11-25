package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// Members of a pipeline task (KUA-252): see domain/task_member.go. These
// are the pipeline tick's (P2.5): it runs for the task's creator, whose
// current access every write checks, as for the room.

var (
	// ErrMemberBusy is a member whose turn is queued or running.
	ErrMemberBusy = errors.New("the member is already taking a turn")
	// ErrMemberWrites is the role that writes code: its turns are
	// iterations of the root task, not turns of a member task.
	ErrMemberWrites = errors.New("the role that writes code takes its turns on the root task")
	// ErrMemberUnavailable is a role that cannot take its first turn: the
	// agent profile that plays it was archived.
	ErrMemberUnavailable = errors.New("the agent profile that plays the role was archived")
)

// memberTurnPosts bounds the posts one turn reads (a page of the room);
// memberPromptBytes is what a task's prompt may hold. What does not fit
// waits for the next turn.
const (
	memberTurnPosts   = 200
	memberPromptBytes = 32768
)

// memberRoot opens the transaction of a pipeline task's room for its
// creator: the task locked in the usual order, the room serialized by the
// task's advisory lock, as postTaskMessageTx does.
func (r *AgentTaskRepository) memberRoot(ctx context.Context, rootID uuid.UUID) (*sqlx.Tx, *domain.AgentTask, *domain.Pipeline, error) {
	var creator struct {
		WorkspaceID uuid.UUID `db:"workspace_id"`
		CreatorID   uuid.UUID `db:"creator_id"`
	}
	if err := r.db.GetContext(ctx, &creator, `SELECT workspace_id,creator_id FROM agent_tasks WHERE id=$1`, rootID); errors.Is(err, sql.ErrNoRows) {
		return nil, nil, nil, ErrConversationNotFound
	} else if err != nil {
		return nil, nil, nil, err
	}
	ctx = domain.WithAccess(ctx, domain.Access{WorkspaceID: creator.WorkspaceID, UserID: creator.CreatorID})
	tx, root, err := r.transaction(ctx, rootID, true)
	if err != nil {
		return nil, nil, nil, err
	}
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "workflow-runner:"+root.ID.String()); err != nil {
		tx.Rollback()
		return nil, nil, nil, err
	}
	pipeline, err := taskPipeline(ctx, tx, root)
	if err != nil {
		tx.Rollback()
		return nil, nil, nil, err
	}
	return tx, root, pipeline, nil
}

// ensureWriterMember creates the member of the first writer, played by the
// root task, the first time; its profile is pinned then.
func ensureWriterMember(ctx context.Context, tx *sqlx.Tx, root domain.AgentTask, pipeline domain.Pipeline) error {
	writer, ok := pipeline.Writer()
	if !ok {
		return ErrTaskRoomUnavailable
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO task_members(workspace_id,task_id,role,profile_id,state) VALUES($1,$2,$3,$4,$5) ON CONFLICT (task_id,role) DO NOTHING`,
		root.WorkspaceID, root.ID, writer.Role, writer.ProfileID, domain.MemberState(root.State))
	return err
}

// holdWriteLock gives a pipeline task's write lock to the writer whose turn
// the task's next iteration is (KUA-289), in the iteration's transaction:
// the writers' turns are the root task's attempts, one at a time, so the
// lock moves only between turns. A move is recorded with its time, which
// tells one writer's runs from another's; the writer is a member from its
// first turn, with its profile pinned.
func holdWriteLock(ctx context.Context, tx *sqlx.Tx, task *domain.AgentTask, writer domain.PipelineMember) error {
	if !writer.Writer {
		return ErrConversationInvalid
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO task_members(workspace_id,task_id,role,profile_id,state) VALUES($1,$2,$3,$4,'idle') ON CONFLICT (task_id,role) DO NOTHING`,
		task.WorkspaceID, task.ID, writer.Role, writer.ProfileID); err != nil {
		return err
	}
	return tx.GetContext(ctx, task, `UPDATE agent_tasks SET pipeline_state=jsonb_set(pipeline_state,'{writer}',to_jsonb($2::text))||CASE WHEN COALESCE(pipeline_state->>'writer',$2)=$2 THEN '{}'::jsonb
 ELSE jsonb_build_object('moves',COALESCE(pipeline_state->'moves','[]'::jsonb)||jsonb_build_array(jsonb_build_object('role',$2::text,'at',now()))) END WHERE id=$1 RETURNING *`, task.ID, writer.Role)
}

// writerMoved is true for a run of a pipeline task (r) after which the
// write lock moved to another writer: its session is another writer's.
const writerMoved = `EXISTS(SELECT 1 FROM agent_tasks wt,jsonb_array_elements(COALESCE(wt.pipeline_state->'moves','[]'::jsonb)) move
 WHERE wt.id=r.task_id AND wt.pipeline AND (move->>'at')::timestamptz>r.created_at)`

// runWriter is the writer that ran a run (r) of pipeline task $1: the one
// the write lock last moved to before the run, or the first writer.
const runWriter = `COALESCE((SELECT move->>'role' FROM agent_tasks wt,jsonb_array_elements(COALESCE(wt.pipeline_state->'moves','[]'::jsonb)) move
 WHERE wt.id=$1 AND (move->>'at')::timestamptz<=r.created_at ORDER BY (move->>'at')::timestamptz DESC LIMIT 1),
 (SELECT m.role FROM task_members m WHERE m.task_id=$1 AND m.workspace_id=$2 AND m.member_task_id IS NULL ORDER BY m.created_at,m.id LIMIT 1))`

// Members lists the members of a pipeline task, oldest first. The first
// writer (the root task) exists from the first call; the others exist once
// they took a turn (StartMemberTurn, or holdWriteLock for a writer).
func (r *AgentTaskRepository) Members(ctx context.Context, rootID uuid.UUID) ([]domain.TaskMember, error) {
	tx, root, pipeline, err := r.memberRoot(ctx, rootID)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = ensureWriterMember(ctx, tx, *root, *pipeline); err != nil {
		return nil, err
	}
	members := []domain.TaskMember{}
	if err = tx.SelectContext(ctx, &members, `SELECT * FROM task_members WHERE task_id=$1 AND workspace_id=$2 ORDER BY created_at,role`, root.ID, root.WorkspaceID); err != nil {
		return nil, err
	}
	return members, tx.Commit()
}

// unreadPosts lists the room after a role's cursor, oldest first, bounded:
// the oldest page for a member, and the newest for a role that is not one
// yet (KUA-288), whose first turn is called by the newest posts; what was
// said before it joined is history it may skip.
func unreadPosts(ctx context.Context, q sqlx.QueryerContext, root domain.AgentTask, role string) ([]domain.TaskMessage, error) {
	posts := []domain.TaskMessage{}
	err := sqlx.SelectContext(ctx, q, &posts, `SELECT * FROM (SELECT * FROM task_messages WHERE task_id=$1 AND workspace_id=$2
 AND seq>COALESCE((SELECT last_read_seq FROM task_members WHERE task_id=$1 AND role=$3),0)
 ORDER BY CASE WHEN EXISTS(SELECT 1 FROM task_members WHERE task_id=$1 AND role=$3) THEN seq ELSE -seq END LIMIT $4) page ORDER BY seq`, root.ID, root.WorkspaceID, role, memberTurnPosts)
	return posts, err
}

// MemberInbox is what a role has not read in its task's room, as its next
// turn would read it (domain.RoomInbox). A role that took no turn yet has
// read nothing.
func (r *AgentTaskRepository) MemberInbox(ctx context.Context, rootID uuid.UUID, role string) ([]domain.TaskMessage, error) {
	var root domain.AgentTask
	if err := r.db.GetContext(ctx, &root, `SELECT * FROM agent_tasks WHERE id=$1`, rootID); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrConversationNotFound
	} else if err != nil {
		return nil, err
	}
	posts, err := unreadPosts(ctx, r.db, root, role)
	if err != nil {
		return nil, err
	}
	return domain.RoomInbox(role, posts), nil
}

// MarkMemberRead moves a member's room cursor to seq; it never moves back.
// The writer's turns are the root task's iterations, which read the room
// through it.
func (r *AgentTaskRepository) MarkMemberRead(ctx context.Context, rootID uuid.UUID, role string, seq int64) error {
	_, err := r.db.ExecContext(ctx, `UPDATE task_members m SET last_read_seq=$3,updated_at=now() WHERE m.task_id=$1 AND m.role=$2 AND m.last_read_seq<$3
 AND $3<=(SELECT COALESCE(max(seq),0) FROM task_messages WHERE task_id=$1)`, rootID, role, seq)
	return err
}

// StartMemberTurn starts the next turn of a role that does not write code,
// as a new generation of its member task: the first turn creates the
// member, pinning its profile, and its review task. The turn reads the
// posts its role has not read (bounded, oldest first) and the newest commit
// Kuayle published from the root task's attempts; it must end with a
// verdict for that commit when asked (askVerdict, or a review request in
// the posts it reads). A member whose turn is queued or running, or whose
// task waits on a usage limit, is ErrMemberBusy; a turn that finds no free
// task slot on its machine (memberSlot) is ErrNoTaskSlot.
func (r *AgentTaskRepository) StartMemberTurn(ctx context.Context, rootID uuid.UUID, role string, askVerdict bool) (*domain.TaskMember, error) {
	tx, root, pipeline, err := r.memberRoot(ctx, rootID)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	roster, ok := pipeline.Member(role)
	if !ok {
		return nil, ErrTaskRoomUnavailable
	}
	if roster.Writer {
		return nil, ErrMemberWrites
	}
	// The lead manages (KUA-300): it reads as the roles that review do, and
	// is never asked for a verdict.
	manages := pipeline.Behaviour(role) == domain.AgentBehaviourManages
	if err = ensureWriterMember(ctx, tx, *root, *pipeline); err != nil {
		return nil, err
	}
	var member *domain.TaskMember
	var task *domain.AgentTask
	var existing domain.TaskMember
	err = tx.GetContext(ctx, &existing, `SELECT * FROM task_members WHERE task_id=$1 AND role=$2 AND workspace_id=$3 FOR UPDATE`, root.ID, role, root.WorkspaceID)
	switch {
	case err == nil:
		member = &existing
		// The member task shares the root's conversation, which is locked:
		// locking it here keeps the order its own transitions use.
		task = &domain.AgentTask{}
		if err = tx.GetContext(ctx, task, `SELECT * FROM agent_tasks WHERE id=$1 AND workspace_id=$2 FOR UPDATE`, member.MemberTaskID, root.WorkspaceID); err != nil {
			return nil, err
		}
		if member.State != domain.MemberIdle || (task.State != "blocked" && task.State != "failed" && task.State != "cancelled") || task.UsageLimitMessage != nil {
			return nil, ErrMemberBusy
		}
		// Nothing of the previous turn may still be active.
		var pending bool
		if err = tx.GetContext(ctx, &pending, `SELECT EXISTS(SELECT 1 FROM dev_machine_agent_runs WHERE task_id=$1 AND status IN ('queued','starting','running','waiting_input'))
 OR EXISTS(SELECT 1 FROM dev_machine_operations o WHERE (o.checkout_id=$2 OR o.agent_run_id IN (SELECT id FROM dev_machine_agent_runs WHERE task_id=$1)) AND o.status IN ('pending','leased') AND o.action NOT IN ('start_preview','stop_preview','cleanup_task'))`, task.ID, task.CheckoutID); err != nil {
			return nil, err
		}
		if pending {
			return nil, ErrMemberBusy
		}
	case errors.Is(err, sql.ErrNoRows):
	default:
		return nil, err
	}
	profileID := roster.ProfileID
	if member != nil {
		profileID = member.ProfileID
	}
	var profile domain.AgentProfile
	if err = tx.GetContext(ctx, &profile, `SELECT * FROM agent_profiles WHERE id=$1 AND workspace_id=$2`, profileID, root.WorkspaceID); err != nil {
		return nil, err
	}
	if member == nil && profile.ArchivedAt != nil {
		return nil, ErrMemberUnavailable
	}

	posts, err := unreadPosts(ctx, tx, *root, role)
	if err != nil {
		return nil, err
	}
	target, published, err := publishedTarget(ctx, tx, *root)
	if err != nil {
		return nil, err
	}
	turn := domain.MemberTurn{Role: role, Target: target, Instructions: profile.Instructions}
	if member != nil && member.PinnedCommit != nil {
		turn.Since = *member.PinnedCommit
	}
	if member != nil {
		if turn.Reviewed, err = earlierReview(ctx, tx, *member); err != nil {
			return nil, err
		}
	}
	asked := func() bool {
		return target != nil && !manages && (askVerdict || domain.RoomReviewRequested(roster, domain.RoomInbox(role, posts)))
	}
	if turn.VerdictAsked = asked(); target != nil && !manages {
		if turn.CI, err = recordedReviewCI(ctx, tx, published, target.CommitSHA); err != nil {
			return nil, err
		}
	}
	phase := ""
	if root.WorkflowNode != nil {
		phase = *root.WorkflowNode
	}
	// What does not fit this turn's prompt waits for the next turn; a first
	// turn keeps the newest posts instead.
	var prompt string
	for {
		turn.Posts = posts
		if manages {
			prompt = domain.LeadTurnPrompt(domain.LeadTurn{Pipeline: *pipeline, Phase: phase, Target: target, Instructions: profile.Instructions, Posts: posts})
		} else {
			prompt = domain.MemberTurnPrompt(turn)
		}
		if len(prompt) <= memberPromptBytes || len(posts) <= 1 {
			break
		}
		if member == nil {
			posts = posts[1:]
		} else {
			posts = posts[:len(posts)-1]
		}
		turn.VerdictAsked = asked()
	}
	if len(prompt) > memberPromptBytes {
		return nil, ErrConversationInvalid
	}
	read := int64(0)
	if member != nil {
		read = member.LastReadSeq
	}
	// The turn takes a task slot on its machine, or waits for one.
	machine, err := memberSlot(ctx, tx, *root, role, task, read)
	if err != nil {
		return nil, err
	}
	var slot any
	if machine != uuid.Nil {
		slot = machine
	}
	if len(posts) > 0 {
		read = posts[len(posts)-1].Seq
	}
	var pinned *string
	if target != nil {
		pinned = &target.CommitSHA
	}

	reason := fmt.Sprintf("Waiting for a machine for the %s's turn in the task room", role)
	if member == nil {
		input := domain.AgentReviewRequest{Provider: profile.Harness, Model: profile.Model, Thinking: profile.Effort,
			RequestID: uuid.NewSHA1(workflowNamespace, []byte(root.ID.String()+"/member/"+role))}
		if task, err = insertReviewAgentTask(ctx, tx, *root, *root.ConversationID, root.CreatorID, input, prompt, reason); err != nil {
			return nil, err
		}
		if slot != nil {
			if err = tx.GetContext(ctx, task, `UPDATE agent_tasks SET machine_id=$2 WHERE id=$1 RETURNING *`, task.ID, slot); err != nil {
				return nil, err
			}
		}
		member = &domain.TaskMember{}
		if err = tx.GetContext(ctx, member, `INSERT INTO task_members(workspace_id,task_id,role,profile_id,member_task_id,last_read_seq,state,turns,pinned_commit,verdict_asked)
 VALUES($1,$2,$3,$4,$5,$6,$7,1,$8,$9) RETURNING *`, root.WorkspaceID, root.ID, role, profile.ID, task.ID, read, domain.MemberState(task.State), pinned, turn.VerdictAsked); err != nil {
			return nil, err
		}
		if err = recordAgentTaskTransition(ctx, tx, *task, "", &root.CreatorID); err != nil {
			return nil, err
		}
		return member, tx.Commit()
	}
	if err = tx.GetContext(ctx, task, `UPDATE agent_tasks SET prompt=$2,machine_id=COALESCE($3,machine_id),generation=generation+1,run_id=gen_random_uuid(),failure_count=0,failure_pending=false,failure_class=NULL,sign_in_required=false,
 usage_limit_resumes=0,resume_native_session=false,platform_retries=0,platform_retry_pending=false,live_resume_generation=NULL,lease_token=NULL,lease_expires_at=NULL,next_poll_at=now()
 WHERE id=$1 RETURNING *`, task.ID, prompt, slot); err != nil {
		return nil, err
	}
	if err = tx.GetContext(ctx, member, `UPDATE task_members SET last_read_seq=$2,turns=turns+1,pinned_commit=$3,verdict_asked=$4,updated_at=now() WHERE id=$1 RETURNING *`,
		member.ID, read, pinned, turn.VerdictAsked); err != nil {
		return nil, err
	}
	if err = transitionAgentTask(ctx, tx, task, "queued", fmt.Sprintf("Turn %d of the %s in the task room", member.Turns, role), nil); err != nil {
		return nil, err
	}
	if err = tx.GetContext(ctx, member, `SELECT * FROM task_members WHERE id=$1`, member.ID); err != nil {
		return nil, err
	}
	return member, tx.Commit()
}

// earlierReview is what a member's earlier review left for its next pass
// (P4.4): its role's latest verdict, and the final message of its last
// turn (the newest run of its task, as PreviousAttempt orders them); nil
// before the role gave a verdict.
func earlierReview(ctx context.Context, tx *sqlx.Tx, member domain.TaskMember) (*domain.MemberReview, error) {
	var review domain.MemberReview
	err := tx.GetContext(ctx, &review, `SELECT v.commit_sha,v.verdict,v.findings,
 COALESCE((SELECT r.summary FROM dev_machine_agent_runs r WHERE r.task_id=$4 AND r.workspace_id=$2 ORDER BY r.created_at DESC,r.id LIMIT 1),'') AS last_message
 FROM task_verdicts v WHERE v.task_id=$1 AND v.workspace_id=$2 AND v.role=$3 ORDER BY v.seq DESC LIMIT 1`, member.TaskID, member.WorkspaceID, member.Role, member.MemberTaskID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &review, err
}

// pipelinePublicationRun is the last run of a pipeline task (t) that
// published a commit. The writer's turns all run on the root task, and a turn
// that changed nothing is not published, so the task's latest run need not
// be it.
const pipelinePublicationRun = `(SELECT g.agent_run_id FROM dev_machine_git_refs g JOIN dev_machine_agent_runs r ON r.id=g.agent_run_id AND r.workspace_id=g.workspace_id
 WHERE r.task_id=t.id AND r.workspace_id=t.workspace_id AND g.ref_type='commit' AND g.source='published' AND g.commit_sha IS NOT NULL ORDER BY g.created_at DESC,g.id DESC LIMIT 1)`

// publishedTarget is the work a member reads: the newest commit Kuayle
// published from the root task's attempts, with its pull request, and the
// root as of the attempt that published it; nil before anything is
// published with a pull request.
func publishedTarget(ctx context.Context, q sqlx.QueryerContext, root domain.AgentTask) (*domain.AgentReviewTarget, domain.AgentTask, error) {
	var run uuid.UUID
	err := sqlx.GetContext(ctx, q, &run, `SELECT `+pipelinePublicationRun+` FROM agent_tasks t WHERE t.id=$1 AND t.workspace_id=$2`, root.ID, root.WorkspaceID)
	if err == nil && run == uuid.Nil {
		err = sql.ErrNoRows
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil, root, nil
	}
	if err != nil {
		return nil, root, err
	}
	published := root
	published.RunID = run
	target, err := reviewTarget(ctx, q, published)
	if errors.Is(err, ErrNoReviewTarget) {
		return nil, root, nil
	}
	return target, published, err
}

// PublishedWork is the newest pull request and commit Kuayle published from
// a task's attempts, whichever attempt published it; nil before anything is
// published with a pull request.
func (r *AgentTaskRepository) PublishedWork(ctx context.Context, task domain.AgentTask) (*domain.AgentReviewTarget, error) {
	target, _, err := publishedTarget(ctx, r.db, task)
	return target, err
}

// MemberTasks is the task whose generations are each member's turns, by
// role, for every member but the writer (whose turns are the root task's).
func (r *AgentTaskRepository) MemberTasks(ctx context.Context, root domain.AgentTask) (map[string]domain.AgentTask, error) {
	var rows []struct {
		Role string `db:"member_role"`
		domain.AgentTask
	}
	if err := r.db.SelectContext(ctx, &rows, `SELECT m.role AS member_role,t.* FROM task_members m JOIN agent_tasks t ON t.id=m.member_task_id AND t.workspace_id=m.workspace_id
 WHERE m.task_id=$1 AND m.workspace_id=$2`, root.ID, root.WorkspaceID); err != nil {
		return nil, err
	}
	tasks := make(map[string]domain.AgentTask, len(rows))
	for _, row := range rows {
		tasks[row.Role] = row.AgentTask
	}
	return tasks, nil
}

// LatestVerdicts is the latest verdict of each role for one commit of a
// pipeline task, or with commit "" on any commit. A verdict counts for the
// commit it names only: whether it also holds for a head that merely merged
// the base is the tick's call.
func (r *AgentTaskRepository) LatestVerdicts(ctx context.Context, root domain.AgentTask, commit string) (map[string]domain.TaskVerdict, error) {
	var verdicts []domain.TaskVerdict
	if err := r.db.SelectContext(ctx, &verdicts, `SELECT DISTINCT ON (role) * FROM task_verdicts WHERE task_id=$1 AND workspace_id=$3 AND ($2='' OR commit_sha=$2) ORDER BY role,seq DESC`, root.ID, commit, root.WorkspaceID); err != nil {
		return nil, err
	}
	byRole := make(map[string]domain.TaskVerdict, len(verdicts))
	for _, verdict := range verdicts {
		byRole[verdict.Role] = verdict
	}
	return byRole, nil
}

// TaskMember is the member whose turns a task runs; nil for a task that
// runs none (any review requested otherwise, and every implementing task).
func (r *AgentTaskRepository) TaskMember(ctx context.Context, task domain.AgentTask) (*domain.TaskMember, error) {
	if task.Kind != domain.AgentTaskKindReview {
		return nil, nil
	}
	var member domain.TaskMember
	err := r.db.GetContext(ctx, &member, `SELECT * FROM task_members WHERE member_task_id=$1 AND workspace_id=$2`, task.ID, task.WorkspaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &member, err
}

// syncMemberState keeps the state of the member whose turns a task runs in
// step with the task, in the transition's transaction: for a pipeline task,
// the writer holding its write lock.
func syncMemberState(ctx context.Context, tx *sqlx.Tx, task domain.AgentTask) error {
	if task.Kind != domain.AgentTaskKindReview && task.WorkflowVersionID == nil {
		return nil
	}
	_, err := tx.ExecContext(ctx, `UPDATE task_members SET state=$3,updated_at=now() WHERE workspace_id=$2 AND (member_task_id=$1 OR (task_id=$1 AND member_task_id IS NULL AND ($4='' OR role=$4))) AND state<>$3`,
		task.ID, task.WorkspaceID, domain.MemberState(task.State), domain.PipelineHolderRole(task.PipelineState))
	if err != nil || task.Kind != domain.AgentTaskKindReview || domain.MemberState(task.State) != domain.MemberIdle {
		return err
	}
	// A member's turn that ended, however it ended, wakes the tick of its
	// root task waiting for it.
	_, err = tx.ExecContext(ctx, `UPDATE agent_tasks SET workflow_wake_at=now() WHERE id=(SELECT task_id FROM task_members WHERE member_task_id=$1 AND workspace_id=$2)
 AND workspace_id=$2 AND pipeline AND state='awaiting_review'`, task.ID, task.WorkspaceID)
	return err
}

// memberOfTask is the member whose turns a locked task runs, locked; nil
// for any other task.
func memberOfTask(ctx context.Context, tx *sqlx.Tx, task domain.AgentTask) (*domain.TaskMember, error) {
	var member domain.TaskMember
	err := tx.GetContext(ctx, &member, `SELECT * FROM task_members WHERE member_task_id=$1 AND workspace_id=$2 FOR UPDATE`, task.ID, task.WorkspaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &member, err
}

// completeMemberTurn ends a member's turn whose attempt succeeded with
// output as its final message. A turn asked for a verdict that posted none
// for its commit in the room gives the one its final message carries (the
// Verdict: line), posted as the member; a turn that gave neither is reported
// in the room by Kuayle. The member task then rests, its session kept, until
// its next turn, and the root task is woken.
func completeMemberTurn(ctx context.Context, tx *sqlx.Tx, task *domain.AgentTask, member domain.TaskMember, output string) error {
	root := domain.AgentTask{}
	if err := tx.GetContext(ctx, &root, `SELECT * FROM agent_tasks WHERE id=$1 AND workspace_id=$2 FOR UPDATE`, member.TaskID, member.WorkspaceID); err != nil {
		return err
	}
	reason := "Turn finished"
	if member.VerdictAsked && member.PinnedCommit != nil {
		commit := *member.PinnedCommit
		var given *string
		if err := tx.GetContext(ctx, &given, `SELECT (SELECT verdict FROM task_verdicts WHERE task_id=$1 AND role=$2 AND run_id=$3 AND commit_sha=$4 ORDER BY seq DESC LIMIT 1)`, root.ID, member.Role, task.RunID, commit); err != nil {
			return err
		}
		if verdict, findings := domain.ParseAgentReview(output); given == nil && verdict != domain.AgentReviewVerdictUnknown {
			if runes := []rune(findings); len(runes) > domain.RoomMaxAgentRunes {
				findings = string(runes[:domain.RoomMaxAgentRunes])
			}
			post := domain.TaskRoomPost{AuthorKind: domain.RoomAuthorMember, AuthorRole: member.Role, AuthorMemberID: &member.ID, RunID: &task.RunID,
				Kind: domain.RoomVerdict, Body: findings, Data: domain.TaskRoomData{Verdict: verdict, Commit: commit}, DedupeKey: "verdict:" + task.RunID.String()}
			message, err := postTaskMessageTx(ctx, tx, &root, post)
			if err != nil && !errors.Is(err, ErrTaskRoomRateLimited) {
				return err
			}
			if err == nil && message.RejectReason == nil {
				given = &verdict
			}
		}
		if given == nil {
			value, err := json.Marshal(map[string]string{"role": member.Role, "commit": commit, "verdict": "none"})
			if err != nil {
				return err
			}
			if _, err = postTaskMessageTx(ctx, tx, &root, domain.TaskRoomPost{AuthorKind: domain.RoomAuthorKuayle, Kind: domain.RoomFact, Body: domain.NoVerdictNote(member.Role, commit),
				Data: domain.TaskRoomData{Fact: domain.PipelineFactVerdict, Value: value}, DedupeKey: "no-verdict:" + task.RunID.String()}); err != nil {
				return err
			}
			reason = "Turn finished without a verdict"
		} else {
			reason = "Turn finished: " + *given
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE task_members SET verdict_asked=false,updated_at=now() WHERE id=$1`, member.ID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE agent_tasks SET workflow_wake_at=now() WHERE id=$1 AND workspace_id=$2`, root.ID, root.WorkspaceID); err != nil {
		return err
	}
	return transitionAgentTask(ctx, tx, task, "blocked", reason+"; it waits for its next turn in the task room", nil)
}

// verdictCommit is the full commit a member's verdict names, or why the
// room refuses it. A verdict counts only from the member of its role (not a
// role's name alone), for the commit its turn reads or one that Kuayle
// published from the task's attempts. Kuayle keeps no clone to ask git, so
// its own publication records (dev_machine_git_refs, source 'published')
// are what says a commit is on the task's branch; an abbreviation must name
// one of them only.
func verdictCommit(ctx context.Context, tx *sqlx.Tx, root domain.AgentTask, post domain.TaskRoomPost) (string, string, error) {
	const notMember = "a verdict comes from the member task of its role"
	if post.AuthorMemberID == nil {
		return "", notMember, nil
	}
	var member domain.TaskMember
	err := tx.GetContext(ctx, &member, `SELECT * FROM task_members WHERE id=$1 AND task_id=$2 AND workspace_id=$3`, *post.AuthorMemberID, root.ID, root.WorkspaceID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && (member.Role != post.AuthorRole || member.MemberTaskID == nil)) {
		return "", notMember, nil
	}
	if err != nil {
		return "", "", err
	}
	commit := post.Data.Commit
	if member.PinnedCommit != nil && strings.HasPrefix(*member.PinnedCommit, commit) {
		return *member.PinnedCommit, "", nil
	}
	var commits []string
	if err = tx.SelectContext(ctx, &commits, `SELECT DISTINCT g.commit_sha FROM dev_machine_git_refs g JOIN dev_machine_agent_runs r ON r.id=g.agent_run_id AND r.workspace_id=g.workspace_id
 WHERE r.task_id=$1 AND r.workspace_id=$2 AND g.ref_type='commit' AND g.source='published' AND g.commit_sha LIKE $3||'%' LIMIT 2`, root.ID, root.WorkspaceID, commit); err != nil {
		return "", "", err
	}
	switch len(commits) {
	case 1:
		return commits[0], "", nil
	case 0:
		return "", fmt.Sprintf("commit %q is not one Kuayle published on this task's branch", commit), nil
	}
	return "", fmt.Sprintf("commit %q names more than one commit of this task's branch: give it whole", commit), nil
}

// memberRefusal says why a member's post names a member that is not its
// own, or "".
func memberRefusal(ctx context.Context, tx *sqlx.Tx, root domain.AgentTask, post domain.TaskRoomPost) (string, error) {
	if post.AuthorMemberID == nil {
		return "", nil
	}
	var role string
	err := tx.GetContext(ctx, &role, `SELECT role FROM task_members WHERE id=$1 AND task_id=$2 AND workspace_id=$3`, *post.AuthorMemberID, root.ID, root.WorkspaceID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && role != post.AuthorRole) {
		return "the post names another member", nil
	}
	return "", err
}

// recordVerdict stores the verdict a room post gave.
func recordVerdict(ctx context.Context, tx *sqlx.Tx, message domain.TaskMessage) error {
	findings := message.Body
	if !utf8.ValidString(findings) || len(findings) > domain.MaxAgentReviewFindings {
		findings = ""
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO task_verdicts(workspace_id,task_id,role,commit_sha,verdict,findings,run_id,seq) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`,
		message.WorkspaceID, message.TaskID, *message.AuthorRole, message.Data.Commit, message.Data.Verdict, findings, message.RunID, message.Seq)
	return err
}

// MissingVerdict is the commit a member's turn running as run was asked to
// review and has not given a verdict for yet; "" for any other run. The
// machine asks the turn for it once more before the run ends.
func (r *DevMachineRepository) MissingVerdict(ctx context.Context, runID uuid.UUID) (string, error) {
	var commit string
	err := r.db.GetContext(ctx, &commit, `SELECT m.pinned_commit FROM dev_machine_agent_runs r
 JOIN task_members m ON m.member_task_id=r.task_id AND m.workspace_id=r.workspace_id
 JOIN agent_tasks t ON t.id=m.member_task_id AND t.run_id=r.id
 WHERE r.id=$1 AND m.verdict_asked AND m.pinned_commit IS NOT NULL
 AND NOT EXISTS(SELECT 1 FROM task_verdicts v WHERE v.task_id=m.task_id AND v.role=m.role AND v.run_id=$1 AND v.commit_sha=m.pinned_commit)`, runID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return commit, err
}

// MemberCommit is the commit the turn of a pipeline member running as run
// reads (KUA-289): the machine checks it out before it mounts the member's
// worktree read-only. "" for any other run.
func (r *DevMachineRepository) MemberCommit(ctx context.Context, runID uuid.UUID) (string, error) {
	var commit string
	err := r.db.GetContext(ctx, &commit, `SELECT m.pinned_commit FROM dev_machine_agent_runs r
 JOIN task_members m ON m.member_task_id=r.task_id AND m.workspace_id=r.workspace_id
 JOIN agent_tasks t ON t.id=m.member_task_id AND t.run_id=r.id
 WHERE r.id=$1 AND m.pinned_commit IS NOT NULL`, runID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return commit, err
}
