package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	log "github.com/sirupsen/logrus"
)

var (
	// ErrLessonDenied: the actor is not a current owner or admin (decisions),
	// or not a non-guest member (reading and proposing).
	ErrLessonDenied   = errors.New("workspace memory access denied")
	ErrLessonNotFound = errors.New("lesson not found")
	// ErrLessonState: the lesson is not in a state the action applies to.
	ErrLessonState = errors.New("lesson state does not allow this")
	// ErrLessonLimit: the repository already has its maximum of approved lessons.
	ErrLessonLimit = errors.New("too many approved lessons for this repository")
	// ErrLessonPendingLimit: too many proposals wait on the repository.
	ErrLessonPendingLimit = errors.New("too many proposals wait on this repository")
	ErrLessonDuplicate    = errors.New("this lesson already exists for the repository")
	// ErrLessonRepository: not a repository connected to the workspace.
	ErrLessonRepository = errors.New("repository is not connected to this workspace")
	// ErrLessonOrder: a reorder must name every approved lesson of the
	// repository exactly once.
	ErrLessonOrder = errors.New("the order does not match the approved lessons")
)

// WorkspaceMemoryRepository stores the lessons of a workspace's repositories
// (KUA-134). Reads re-check that the actor is a non-guest member and hide the
// lessons of private teams the actor cannot see; every decision re-checks,
// inside its transaction, that the actor is a current owner or admin, and
// records an audit event.
type WorkspaceMemoryRepository struct{ db *sqlx.DB }

func NewWorkspaceMemoryRepository(db *sqlx.DB) *WorkspaceMemoryRepository {
	return &WorkspaceMemoryRepository{db}
}

// lessonVisible keeps the lessons of private teams from people outside them.
// $1 is the workspace and $2 the actor.
const lessonVisible = `(l.team_id IS NULL OR kuayle_team_access(l.team_id,$2,$1))`

func lessonRepositoryKey(repository string) string {
	return strings.ToLower(strings.TrimSpace(repository))
}

// MemberRole returns the actor's current workspace role, or "".
func (r *WorkspaceMemoryRepository) MemberRole(ctx context.Context, ws, actor uuid.UUID) (string, error) {
	return lessonRole(ctx, r.db, ws, actor)
}

func lessonRole(ctx context.Context, q sqlx.QueryerContext, ws, actor uuid.UUID) (string, error) {
	var role string
	err := sqlx.GetContext(ctx, q, &role, `SELECT role FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`, ws, actor)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return role, err
}

// begin opens a write transaction for a non-guest member, or, with manage,
// for a current owner or admin. The membership row stays locked against a
// role change until the transaction ends.
func (r *WorkspaceMemoryRepository) begin(ctx context.Context, ws, actor uuid.UUID, manage bool) (*sqlx.Tx, string, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, "", err
	}
	var role string
	err = tx.GetContext(ctx, &role, `SELECT role FROM workspace_members WHERE workspace_id=$1 AND user_id=$2 FOR SHARE`, ws, actor)
	if err == nil && (role == domain.RoleGuest || manage && role != domain.RoleOwner && role != domain.RoleAdmin) {
		err = sql.ErrNoRows
	}
	if err != nil {
		tx.Rollback()
		if errors.Is(err, sql.ErrNoRows) {
			return nil, "", ErrLessonDenied
		}
		return nil, "", err
	}
	return tx, role, nil
}

// lockLessonRepository serializes the writes that count or order the lessons
// of one repository.
func lockLessonRepository(ctx context.Context, tx *sqlx.Tx, ws uuid.UUID, key string) error {
	_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('workspace_lessons:'||$1::text||':'||$2::text,0))`, ws, key)
	return err
}

func lessonAudit(ctx context.Context, tx *sqlx.Tx, ws, lesson uuid.UUID, actor *uuid.UUID, action string, detail any) error {
	raw, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO workspace_lesson_events(workspace_id,lesson_id,actor_id,action,detail) VALUES($1,$2,$3,$4,$5)`, ws, lesson, actor, action, raw)
	return err
}

// List returns the lessons the actor can see, optionally of one repository
// and in one state: approved lessons in prompt order, the others newest first.
func (r *WorkspaceMemoryRepository) List(ctx context.Context, ws, actor uuid.UUID, repository, state string) ([]domain.Lesson, error) {
	role, err := lessonRole(ctx, r.db, ws, actor)
	if err != nil {
		return nil, err
	}
	if role == "" || role == domain.RoleGuest {
		return nil, ErrLessonDenied
	}
	lessons := []domain.Lesson{}
	err = r.db.SelectContext(ctx, &lessons, `SELECT l.id,l.workspace_id,l.repository_full_name,l.team_id,l.state,l.text,l.proposed_text,l.source_event,l.proposer,l.source_task_id,l.proposed_by,l.decided_by,l.decided_at,l.position,l.created_at,l.updated_at
 FROM workspace_lessons l WHERE l.workspace_id=$1 AND `+lessonVisible+` AND ($3='' OR l.repository_key=$3) AND ($4='' OR l.state=$4)
 ORDER BY l.repository_key,(l.state='approved') DESC,CASE WHEN l.state='approved' THEN l.position END,l.created_at DESC,l.id LIMIT 500`, ws, actor, lessonRepositoryKey(repository), state)
	return lessons, err
}

// lockLesson locks a lesson the actor can see and its repository's counters.
func lockLesson(ctx context.Context, tx *sqlx.Tx, ws, actor, id uuid.UUID) (*domain.Lesson, error) {
	var key string
	err := tx.GetContext(ctx, &key, `SELECT l.repository_key FROM workspace_lessons l WHERE l.id=$3 AND l.workspace_id=$1 AND `+lessonVisible, ws, actor, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrLessonNotFound
	}
	if err != nil {
		return nil, err
	}
	if err = lockLessonRepository(ctx, tx, ws, key); err != nil {
		return nil, err
	}
	var lesson domain.Lesson
	err = tx.GetContext(ctx, &lesson, `SELECT l.id,l.workspace_id,l.repository_full_name,l.team_id,l.state,l.text,l.proposed_text,l.source_event,l.proposer,l.source_task_id,l.proposed_by,l.decided_by,l.decided_at,l.position,l.created_at,l.updated_at
 FROM workspace_lessons l WHERE l.id=$3 AND l.workspace_id=$1 AND `+lessonVisible+` FOR UPDATE OF l`, ws, actor, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrLessonNotFound
	}
	return &lesson, err
}

const lessonReturning = ` RETURNING id,workspace_id,repository_full_name,team_id,state,text,proposed_text,source_event,proposer,source_task_id,proposed_by,decided_by,decided_at,position,created_at,updated_at`

// nextLessonPosition places a newly approved lesson last, within the
// repository's maximum of approved lessons. The caller holds the repository lock.
func nextLessonPosition(ctx context.Context, tx *sqlx.Tx, ws uuid.UUID, key string) (int, error) {
	var current struct {
		Count int `db:"count"`
		Last  int `db:"last"`
	}
	if err := tx.GetContext(ctx, &current, `SELECT count(*) AS count,COALESCE(max(position),0) AS last FROM workspace_lessons WHERE workspace_id=$1 AND repository_key=$2 AND state='approved'`, ws, key); err != nil {
		return 0, err
	}
	if current.Count >= domain.LessonsApprovedPerRepository {
		return 0, ErrLessonLimit
	}
	return current.Last + 1, nil
}

// Add records a lesson a member wrote by hand for a repository connected to
// the workspace. An owner's or admin's lesson is approved at once; any other
// member's waits as a proposal.
func (r *WorkspaceMemoryRepository) Add(ctx context.Context, ws, actor uuid.UUID, repository, text string) (*domain.Lesson, error) {
	text, err := domain.CleanLesson(text)
	if err != nil {
		return nil, err
	}
	tx, role, err := r.begin(ctx, ws, actor, false)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var name string
	err = tx.GetContext(ctx, &name, `SELECT full_name FROM github_repos WHERE workspace_id=$1 AND lower(full_name)=$2 ORDER BY is_active DESC,created_at LIMIT 1`, ws, lessonRepositoryKey(repository))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrLessonRepository
	}
	if err != nil {
		return nil, err
	}
	key := lessonRepositoryKey(name)
	if err = lockLessonRepository(ctx, tx, ws, key); err != nil {
		return nil, err
	}
	state, action, position := domain.LessonProposed, "proposed", 0
	if role == domain.RoleOwner || role == domain.RoleAdmin {
		state, action = domain.LessonApproved, "added"
		if position, err = nextLessonPosition(ctx, tx, ws, key); err != nil {
			return nil, err
		}
	} else {
		var pending int
		if err = tx.GetContext(ctx, &pending, `SELECT count(*) FROM workspace_lessons WHERE workspace_id=$1 AND repository_key=$2 AND state='proposed'`, ws, key); err != nil {
			return nil, err
		}
		if pending >= domain.LessonsPendingPerRepository {
			return nil, ErrLessonPendingLimit
		}
	}
	var lesson domain.Lesson
	err = tx.GetContext(ctx, &lesson, `INSERT INTO workspace_lessons(workspace_id,repository_full_name,repository_key,state,text,proposed_text,source_event,proposer,proposed_by,decided_by,decided_at,position,dedupe_key)
 VALUES($1,$2,$3,$4::text,$5,$5,'manual','person',$6::uuid,CASE WHEN $4::text='approved' THEN $6::uuid END,CASE WHEN $4::text='approved' THEN now() END,$7,$8) ON CONFLICT DO NOTHING`+lessonReturning,
		ws, name, key, state, text, actor, position, domain.LessonDedupeKey(domain.LessonEventManual, text))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrLessonDuplicate
	}
	if err != nil {
		return nil, err
	}
	if err = lessonAudit(ctx, tx, ws, lesson.ID, &actor, action, map[string]any{"event": domain.LessonEventManual, "proposer": domain.LessonProposerPerson, "text": text}); err != nil {
		return nil, err
	}
	return &lesson, tx.Commit()
}

// Approve makes a proposal reach agents, optionally with the text a person
// edited it to, placed after the repository's other approved lessons.
func (r *WorkspaceMemoryRepository) Approve(ctx context.Context, ws, actor, id uuid.UUID, text *string) (*domain.Lesson, error) {
	tx, _, err := r.begin(ctx, ws, actor, true)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	lesson, err := lockLesson(ctx, tx, ws, actor, id)
	if err != nil {
		return nil, err
	}
	if lesson.State != domain.LessonProposed {
		return nil, ErrLessonState
	}
	approved := lesson.Text
	if text != nil {
		if approved, err = domain.CleanLesson(*text); err != nil {
			return nil, err
		}
	}
	position, err := nextLessonPosition(ctx, tx, ws, lessonRepositoryKey(lesson.Repository))
	if err != nil {
		return nil, err
	}
	var updated domain.Lesson
	if err = tx.GetContext(ctx, &updated, `UPDATE workspace_lessons SET state='approved',text=$2,position=$3,decided_by=$4,decided_at=now(),updated_at=now() WHERE id=$1`+lessonReturning, id, approved, position, actor); err != nil {
		return nil, err
	}
	if err = lessonAudit(ctx, tx, ws, id, &actor, "approved", map[string]any{"text": approved, "edited": approved != lesson.ProposedText}); err != nil {
		return nil, err
	}
	return &updated, tx.Commit()
}

// Edit changes the text of a proposed or approved lesson, and, with share,
// makes a private team's lesson a lesson of the whole workspace.
func (r *WorkspaceMemoryRepository) Edit(ctx context.Context, ws, actor, id uuid.UUID, text *string, share bool) (*domain.Lesson, error) {
	tx, _, err := r.begin(ctx, ws, actor, true)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	lesson, err := lockLesson(ctx, tx, ws, actor, id)
	if err != nil {
		return nil, err
	}
	if lesson.State != domain.LessonProposed && lesson.State != domain.LessonApproved {
		return nil, ErrLessonState
	}
	edited := lesson.Text
	if text != nil {
		if edited, err = domain.CleanLesson(*text); err != nil {
			return nil, err
		}
	}
	share = share && lesson.TeamID != nil
	if edited == lesson.Text && !share {
		return lesson, nil
	}
	var updated domain.Lesson
	err = tx.GetContext(ctx, &updated, `UPDATE workspace_lessons SET text=$2,team_id=CASE WHEN $3 THEN NULL ELSE team_id END,updated_at=now() WHERE id=$1`+lessonReturning, id, edited, share)
	if uniqueViolation(err) {
		return nil, ErrLessonDuplicate
	}
	if err != nil {
		return nil, err
	}
	if edited != lesson.Text {
		if err = lessonAudit(ctx, tx, ws, id, &actor, "edited", map[string]any{"from": lesson.Text, "to": edited}); err != nil {
			return nil, err
		}
	}
	if share {
		if err = lessonAudit(ctx, tx, ws, id, &actor, "shared", map[string]any{"team_id": lesson.TeamID}); err != nil {
			return nil, err
		}
	}
	return &updated, tx.Commit()
}

// Retire stops an approved lesson from reaching agents.
func (r *WorkspaceMemoryRepository) Retire(ctx context.Context, ws, actor, id uuid.UUID) (*domain.Lesson, error) {
	return r.close(ctx, ws, actor, id, domain.LessonApproved, domain.LessonRetired)
}

// Drop refuses a proposal. Its subject is not proposed again.
func (r *WorkspaceMemoryRepository) Drop(ctx context.Context, ws, actor, id uuid.UUID) (*domain.Lesson, error) {
	return r.close(ctx, ws, actor, id, domain.LessonProposed, domain.LessonDropped)
}

func (r *WorkspaceMemoryRepository) close(ctx context.Context, ws, actor, id uuid.UUID, from, to string) (*domain.Lesson, error) {
	tx, _, err := r.begin(ctx, ws, actor, true)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	lesson, err := lockLesson(ctx, tx, ws, actor, id)
	if err != nil {
		return nil, err
	}
	if lesson.State != from {
		return nil, ErrLessonState
	}
	var updated domain.Lesson
	if err = tx.GetContext(ctx, &updated, `UPDATE workspace_lessons SET state=$2,decided_by=$3,decided_at=now(),updated_at=now() WHERE id=$1`+lessonReturning, id, to, actor); err != nil {
		return nil, err
	}
	if err = lessonAudit(ctx, tx, ws, id, &actor, to, map[string]any{"text": lesson.Text}); err != nil {
		return nil, err
	}
	return &updated, tx.Commit()
}

// Reorder sets the prompt order of a repository's approved lessons. ids must
// name each of them exactly once.
func (r *WorkspaceMemoryRepository) Reorder(ctx context.Context, ws, actor uuid.UUID, repository string, ids []uuid.UUID) ([]domain.Lesson, error) {
	tx, _, err := r.begin(ctx, ws, actor, true)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	key := lessonRepositoryKey(repository)
	if err = lockLessonRepository(ctx, tx, ws, key); err != nil {
		return nil, err
	}
	current := []struct {
		ID       uuid.UUID `db:"id"`
		Position int       `db:"position"`
	}{}
	if err = tx.SelectContext(ctx, &current, `SELECT id,position FROM workspace_lessons WHERE workspace_id=$1 AND repository_key=$2 AND state='approved' FOR UPDATE`, ws, key); err != nil {
		return nil, err
	}
	positions := map[uuid.UUID]int{}
	for _, lesson := range current {
		positions[lesson.ID] = lesson.Position
	}
	seen := map[uuid.UUID]bool{}
	for _, id := range ids {
		if _, known := positions[id]; !known || seen[id] {
			return nil, ErrLessonOrder
		}
		seen[id] = true
	}
	if len(ids) != len(current) {
		return nil, ErrLessonOrder
	}
	for index, id := range ids {
		if positions[id] == index+1 {
			continue
		}
		if _, err = tx.ExecContext(ctx, `UPDATE workspace_lessons SET position=$2,updated_at=now() WHERE id=$1`, id, index+1); err != nil {
			return nil, err
		}
		if err = lessonAudit(ctx, tx, ws, id, &actor, "reordered", map[string]any{"from": positions[id], "to": index + 1}); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return r.List(ctx, ws, actor, key, domain.LessonApproved)
}

// Events lists the newest audit events of a lesson for an owner or admin.
func (r *WorkspaceMemoryRepository) Events(ctx context.Context, ws, actor, id uuid.UUID) ([]domain.LessonEvent, error) {
	role, err := lessonRole(ctx, r.db, ws, actor)
	if err != nil {
		return nil, err
	}
	if role != domain.RoleOwner && role != domain.RoleAdmin {
		return nil, ErrLessonDenied
	}
	var found bool
	if err = r.db.GetContext(ctx, &found, `SELECT EXISTS(SELECT 1 FROM workspace_lessons l WHERE l.id=$3 AND l.workspace_id=$1 AND `+lessonVisible+`)`, ws, actor, id); err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrLessonNotFound
	}
	events := []domain.LessonEvent{}
	err = r.db.SelectContext(ctx, &events, `SELECT id,lesson_id,actor_id,action,detail,created_at FROM workspace_lesson_events WHERE workspace_id=$1 AND lesson_id=$2 ORDER BY id DESC LIMIT 100`, ws, id)
	return events, err
}

// ApprovedTexts returns the approved lessons of a repository that a task of
// the team may receive, in prompt order: lessons of the whole workspace and
// those of the task's own (private) team. Nothing that is proposed, retired
// or dropped is ever returned.
func (r *WorkspaceMemoryRepository) ApprovedTexts(ctx context.Context, ws, team uuid.UUID, repository string) ([]string, error) {
	texts := []string{}
	err := r.db.SelectContext(ctx, &texts, `SELECT l.text FROM workspace_lessons l WHERE l.workspace_id=$1 AND l.repository_key=$2 AND l.state='approved'
 AND (l.team_id IS NULL OR l.team_id=$3) ORDER BY l.position,l.created_at,l.id LIMIT $4`, ws, lessonRepositoryKey(repository), team, domain.LessonsApprovedPerRepository)
	return texts, err
}

// TaskRepository names the repository a task's checkout works on, or "" when
// the task has no checkout yet.
func (r *WorkspaceMemoryRepository) TaskRepository(ctx context.Context, task domain.AgentTask) (string, error) {
	var name string
	err := r.db.GetContext(ctx, &name, `SELECT repository_full_name FROM dev_machine_checkouts WHERE id=$1 AND workspace_id=$2`, task.CheckoutID, task.WorkspaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return name, err
}

// lessonProposal is what a task event proposes. Subject identifies it for
// deduplication (a CI check's name; otherwise the text itself).
type lessonProposal struct {
	TaskID   uuid.UUID
	Event    string
	Proposer string
	Author   *uuid.UUID
	Text     string
	Subject  string
}

// Outcomes of a proposal that was not recorded.
const (
	lessonRecorded     = ""
	lessonDuplicate    = "duplicate"
	lessonNoText       = "a lesson needs some text"
	lessonNoRepository = "this task has no repository checkout"
	lessonTaskLimit    = "this task proposed enough lessons of this kind"
	lessonPendingLimit = "too many proposals already wait on this repository"
)

// proposeLesson records a proposal for the repository of the task's checkout
// inside the caller's transaction, once per subject and within the task's and
// the repository's limits. It returns why nothing was recorded, if so. A
// lesson from a private team's task is scoped to that team. The proposal is
// never shown to an agent: only an owner's or admin's approval does that.
func proposeLesson(ctx context.Context, tx *sqlx.Tx, in lessonProposal) (string, error) {
	text := domain.LessonExcerpt(in.Text)
	if text == "" {
		return lessonNoText, nil
	}
	var source struct {
		WorkspaceID uuid.UUID `db:"workspace_id"`
		TeamID      uuid.UUID `db:"team_id"`
		Private     bool      `db:"is_private"`
		Repository  string    `db:"repository_full_name"`
	}
	err := tx.GetContext(ctx, &source, `SELECT t.workspace_id,tm.id AS team_id,tm.is_private,c.repository_full_name FROM agent_tasks t
 JOIN issues i ON i.id=t.issue_id AND i.workspace_id=t.workspace_id
 JOIN teams tm ON tm.id=i.team_id AND tm.workspace_id=t.workspace_id
 JOIN dev_machine_checkouts c ON c.id=t.checkout_id AND c.workspace_id=t.workspace_id WHERE t.id=$1`, in.TaskID)
	if errors.Is(err, sql.ErrNoRows) {
		return lessonNoRepository, nil
	}
	if err != nil {
		return "", err
	}
	key := lessonRepositoryKey(source.Repository)
	if err = lockLessonRepository(ctx, tx, source.WorkspaceID, key); err != nil {
		return "", err
	}
	var team *uuid.UUID
	if source.Private {
		team = &source.TeamID
	}
	subject := in.Subject
	if subject == "" {
		subject = text
	}
	dedupe := domain.LessonDedupeKey(in.Event, subject)
	var counts struct {
		Known   bool `db:"known"`
		Task    int  `db:"task"`
		Pending int  `db:"pending"`
	}
	if err = tx.GetContext(ctx, &counts, `SELECT
 EXISTS(SELECT 1 FROM workspace_lessons WHERE workspace_id=$1 AND repository_key=$2 AND team_id IS NOT DISTINCT FROM $3 AND dedupe_key=$4) AS known,
 (SELECT count(*) FROM workspace_lessons WHERE source_task_id=$5 AND source_event=$6) AS task,
 (SELECT count(*) FROM workspace_lessons WHERE workspace_id=$1 AND repository_key=$2 AND state='proposed') AS pending`,
		source.WorkspaceID, key, team, dedupe, in.TaskID, in.Event); err != nil {
		return "", err
	}
	switch {
	case counts.Known:
		return lessonDuplicate, nil
	case counts.Task >= domain.LessonProposalsPerTaskEvent:
		return lessonTaskLimit, nil
	case counts.Pending >= domain.LessonsPendingPerRepository:
		return lessonPendingLimit, nil
	}
	var id uuid.UUID
	err = tx.GetContext(ctx, &id, `INSERT INTO workspace_lessons(workspace_id,repository_full_name,repository_key,team_id,state,text,proposed_text,source_event,proposer,source_task_id,proposed_by,dedupe_key)
 VALUES($1,$2,$3,$4,'proposed',$5,$5,$6,$7,$8,$9,$10) ON CONFLICT DO NOTHING RETURNING id`,
		source.WorkspaceID, source.Repository, key, team, text, in.Event, in.Proposer, in.TaskID, in.Author, dedupe)
	if errors.Is(err, sql.ErrNoRows) {
		return lessonDuplicate, nil
	}
	if err != nil {
		return "", err
	}
	return lessonRecorded, lessonAudit(ctx, tx, source.WorkspaceID, id, in.Author, "proposed", map[string]any{"event": in.Event, "proposer": in.Proposer, "task_id": in.TaskID, "text": text})
}

// suggestLesson proposes a lesson from a task event (a person's message or
// comment, a review that requested changes, a failed CI check) without ever
// failing the event: it runs in a savepoint and a failure is only logged.
func suggestLesson(ctx context.Context, tx *sqlx.Tx, in lessonProposal) {
	if _, err := tx.ExecContext(ctx, `SAVEPOINT workspace_lesson`); err != nil {
		return
	}
	if _, err := proposeLesson(ctx, tx, in); err != nil {
		log.WithError(err).WithField("task_id", in.TaskID).Warn("could not propose a workspace lesson")
		tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT workspace_lesson`)
	}
	tx.ExecContext(ctx, `RELEASE SAVEPOINT workspace_lesson`)
}

// recordMemoryOutbox handles an outbox file addressed to the workspace memory
// ({"to":"memory","lesson":"..."}) inside RecordTaskOutbox's transaction. A
// valid lesson from an implementing task that can still send becomes a
// proposal marked as written by an agent; the same lesson is proposed once.
// Anything refused is recorded in the message registry with the reason,
// within the sender's hourly budget. It reports whether a record was added.
func recordMemoryOutbox(ctx context.Context, tx *sqlx.Tx, sender messageSender, dedupe string, data []byte, ended bool) (bool, error) {
	lesson, parseErr := domain.ParseAgentMemoryLesson(data)
	reason := ""
	switch {
	case parseErr != nil:
		reason = parseErr.Error()
	case sender.Kind != domain.AgentTaskKindImplement || ended:
		reason = "the sending task can no longer send messages"
	default:
		var err error
		if reason, err = proposeLesson(ctx, tx, lessonProposal{TaskID: sender.ID, Event: domain.LessonEventTask, Proposer: domain.LessonProposerAgent, Text: lesson}); err != nil {
			return false, err
		}
		if reason == lessonRecorded {
			return true, nil
		}
		if reason == lessonDuplicate {
			return false, nil
		}
	}
	body := lesson
	if parseErr != nil {
		body = domain.AgentMessagePreview(data)
	}
	return insertAgentMessage(ctx, tx, domain.AgentMessage{WorkspaceID: sender.WorkspaceID, FromTaskID: sender.ID, FromIssueID: sender.IssueID, FromRunID: &sender.RunID,
		ToRef: domain.AgentOutboxMemory, Channel: domain.AgentMessageInternal, Body: body, State: "rejected", RejectReason: &reason, CommentState: "none", DedupeKey: dedupe})
}
