package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/lib/pq"
)

// The task notebook (domain.Notebook) lives in agent_task_notebooks, one row
// per task, with its history in agent_task_notebook_changes. Every write
// locks the row, so changes are serialized, and happens in the transaction
// of what caused it (a workflow move, a retry, a live message), after that
// transaction's task lock. Agents never write it directly: their updates
// arrive as outbox files (RecordAgentOutbox) and are validated here.

// notebookHistoryKept is how many changes of a task are kept.
const notebookHistoryKept = 200

type notebookRow struct {
	TaskID      uuid.UUID       `db:"task_id"`
	WorkspaceID uuid.UUID       `db:"workspace_id"`
	Document    json.RawMessage `db:"document"`
	Revision    int             `db:"revision"`
	UpdatedAt   time.Time       `db:"updated_at"`
}

// notebookTask is what a notebook change needs to know about its task.
type notebookTask struct {
	ID          uuid.UUID  `db:"id"`
	WorkspaceID uuid.UUID  `db:"workspace_id"`
	CreatorID   uuid.UUID  `db:"creator_id"`
	Kind        string     `db:"kind"`
	Prompt      string     `db:"prompt"`
	RunID       uuid.UUID  `db:"run_id"`
	Generation  int64      `db:"generation"`
	Sequence    int        `db:"workflow_sequence"`
	Node        *string    `db:"workflow_node"`
	Version     *uuid.UUID `db:"workflow_version_id"`
	CreatedAt   time.Time  `db:"created_at"`
}

func notebookTaskOf(ctx context.Context, q sqlx.QueryerContext, id uuid.UUID) (*notebookTask, error) {
	var task notebookTask
	err := sqlx.GetContext(ctx, q, &task, `SELECT id,workspace_id,creator_id,kind,prompt,run_id,generation,workflow_sequence,workflow_node,workflow_version_id,created_at FROM agent_tasks WHERE id=$1`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &task, err
}

// initialNotebook is a task's notebook before anything was recorded: it
// holds the request the task was started with, as its creator's instruction.
func initialNotebook(task notebookTask) domain.Notebook {
	notebook := domain.NewNotebook()
	request, _, _ := strings.Cut(task.Prompt, domain.TaskFollowUpMarker)
	creator := task.CreatorID
	notebook.AddInstruction(domain.NotebookInstruction{Source: domain.NotebookInstructionRequest, AuthorID: &creator, Text: request, At: task.CreatedAt})
	return notebook
}

// lockNotebook creates the task's notebook when it has none and locks it.
func lockNotebook(ctx context.Context, tx *sqlx.Tx, task notebookTask) (*notebookRow, domain.Notebook, error) {
	initial, err := json.Marshal(initialNotebook(task))
	if err != nil {
		return nil, domain.Notebook{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO agent_task_notebooks(task_id,workspace_id,document) VALUES($1,$2,$3) ON CONFLICT (task_id) DO NOTHING`, task.ID, task.WorkspaceID, initial); err != nil {
		return nil, domain.Notebook{}, err
	}
	var row notebookRow
	if err = tx.GetContext(ctx, &row, `SELECT task_id,workspace_id,document,revision,updated_at FROM agent_task_notebooks WHERE task_id=$1 FOR UPDATE`, task.ID); err != nil {
		return nil, domain.Notebook{}, err
	}
	notebook, err := domain.DecodeNotebook(row.Document)
	return &row, notebook, err
}

// notebookStepLabel names the workflow step the task is on, or "".
func notebookStepLabel(ctx context.Context, q sqlx.QueryerContext, task notebookTask) string {
	if task.Version == nil || task.Node == nil {
		return ""
	}
	// The graph is read as stored and parsed here: a query that failed
	// inside the caller's transaction would abort it.
	var raw json.RawMessage
	if err := sqlx.GetContext(ctx, q, &raw, `SELECT graph FROM workflow_versions WHERE id=$1 AND workspace_id=$2`, task.Version, task.WorkspaceID); err != nil {
		return ""
	}
	var graph domain.WorkflowGraph
	if json.Unmarshal(raw, &graph) != nil {
		return ""
	}
	node, _ := graph.Node(*task.Node)
	return truncateBytes(node.Label, 200)
}

// saveNotebook stores a changed notebook as its next revision and records
// who changed which sections at which step.
func saveNotebook(ctx context.Context, tx *sqlx.Tx, task notebookTask, row *notebookRow, notebook domain.Notebook, change domain.NotebookChange) error {
	document, err := json.Marshal(notebook)
	if err != nil {
		return err
	}
	if len(document) > domain.NotebookMaxBytes {
		return ErrConversationInvalid
	}
	row.Revision++
	if _, err = tx.ExecContext(ctx, `UPDATE agent_task_notebooks SET document=$2,revision=$3,updated_at=now() WHERE task_id=$1`, task.ID, document, row.Revision); err != nil {
		return err
	}
	if change.RunID == nil {
		run := task.RunID
		change.RunID = &run
	}
	generation := task.Generation
	var sequence *int
	if task.Version != nil {
		sequence = &task.Sequence
	}
	if change.Sections == nil {
		change.Sections = []string{}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO agent_task_notebook_changes(task_id,revision,author,user_id,run_id,generation,step_sequence,step_label,sections,summary) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		task.ID, row.Revision, change.Author, change.UserID, change.RunID, generation, sequence, notebookStepLabel(ctx, tx, task), pq.Array(change.Sections), truncateBytes(strings.Join(strings.Fields(change.Summary), " "), 500)); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `DELETE FROM agent_task_notebook_changes WHERE task_id=$1 AND revision<=$2`, task.ID, row.Revision-notebookHistoryKept)
	return err
}

func notebookFits(notebook domain.Notebook) bool {
	document, err := json.Marshal(notebook)
	return err == nil && len(document) <= domain.NotebookMaxBytes
}

// noteNotebook records what Kuayle knows without the agent, in the caller's
// transaction: instructions people gave, findings of reviews and failing
// checks, and events. actor is the person who caused it, or nil. Notes that
// add nothing (an instruction already recorded) leave the notebook as it is.
func noteNotebook(ctx context.Context, tx *sqlx.Tx, taskID uuid.UUID, actor *uuid.UUID, notes ...domain.NotebookNote) error {
	if len(notes) == 0 {
		return nil
	}
	task, err := notebookTaskOf(ctx, tx, taskID)
	if err != nil || task == nil || task.Kind != domain.AgentTaskKindImplement {
		return err
	}
	row, notebook, err := lockNotebook(ctx, tx, *task)
	if err != nil {
		return err
	}
	now := time.Now()
	sections := map[string]bool{}
	var summary []string
	author := domain.NotebookAuthorKuayle
	for _, note := range notes {
		switch {
		case note.Instruction != nil:
			instruction := *note.Instruction
			if instruction.At.IsZero() {
				instruction.At = now
			}
			if notebook.AddInstruction(instruction) {
				sections["instructions"] = true
				summary = append(summary, "instruction ("+instruction.Source+")")
				// The instruction is the person's, also when the assistant
				// drafted it; the history says who gave it.
				author = domain.NotebookAuthorPerson
			}
		case note.Finding != "":
			if notebook.AddFinding(note.FindingSource, note.Finding, now) {
				sections["findings"] = true
				summary = append(summary, "finding ("+note.FindingSource+")")
			}
		case note.Event != "":
			if notebook.AddEvent(note.Event, now) {
				sections["events"] = true
				summary = append(summary, note.Event)
			}
		}
	}
	if len(sections) == 0 || !notebookFits(notebook) {
		// What caused the note (a message, a workflow move) never fails
		// because the notebook is full; its sections are bounded, so this
		// is a last guard.
		return nil
	}
	return saveNotebook(ctx, tx, *task, row, notebook, domain.NotebookChange{Author: author, UserID: actor, Sections: sortedKeys(sections), Summary: strings.Join(summary, "; ")})
}

func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// setNotebookPlan stores the plan a planning step produced (written by its
// agent) or forgets the plan of a planning step that failed.
func setNotebookPlan(ctx context.Context, tx *sqlx.Tx, taskID uuid.UUID, plan string) error {
	task, err := notebookTaskOf(ctx, tx, taskID)
	if err != nil || task == nil {
		return err
	}
	row, notebook, err := lockNotebook(ctx, tx, *task)
	if err != nil {
		return err
	}
	if notebook.Plan.Text == strings.TrimSpace(plan) {
		return nil
	}
	notebook.SetPlan(plan)
	if !notebookFits(notebook) {
		return nil
	}
	change := domain.NotebookChange{Author: domain.NotebookAuthorAgent, Sections: []string{"plan"}, Summary: "the planning step's plan"}
	if plan == "" {
		change.Author, change.Summary = domain.NotebookAuthorKuayle, "the planning step failed; its plan was dropped"
	}
	return saveNotebook(ctx, tx, *task, row, notebook, change)
}

// recordNotebookUpdates validates and merges the notebook updates an agent
// left in its task's outbox. A valid update is merged and counted on the
// task's current attempt; an invalid one leaves the notebook as it is and
// records why on that attempt, which then cannot succeed (the manager checks
// when the attempt ends). The files' strings were redacted by the manager.
func (r *DevMachineRepository) recordNotebookUpdates(ctx context.Context, taskID uuid.UUID, files []domain.AgentOutboxFile) error {
	if len(files) == 0 {
		return nil
	}
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	task, err := notebookTaskOf(ctx, tx, taskID)
	if err != nil || task == nil || task.Kind != domain.AgentTaskKindImplement {
		return err
	}
	row, notebook, err := lockNotebook(ctx, tx, *task)
	if err != nil {
		return err
	}
	files = append([]domain.AgentOutboxFile(nil), files...)
	sort.SliceStable(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	for _, file := range files {
		update, parseErr := domain.ParseNotebookUpdate(file.Data, file.Oversize)
		if parseErr == nil && file.Unreadable != "" {
			parseErr = errors.New(file.Unreadable)
		}
		if parseErr != nil {
			if _, err = tx.ExecContext(ctx, `UPDATE dev_machine_agent_runs SET notebook_problem=$2 WHERE id=$1 AND task_id=$3`, task.RunID, truncateBytes(parseErr.Error(), 1000), task.ID); err != nil {
				return err
			}
			continue
		}
		// Merge into a copy, so a refused merge leaves the notebook untouched.
		copied, _ := json.Marshal(notebook)
		merged, _ := domain.DecodeNotebook(copied)
		sections := merged.ApplyUpdate(update, time.Now())
		document, encodeErr := json.Marshal(merged)
		if encodeErr != nil || len(document) > domain.NotebookMaxBytes {
			if _, err = tx.ExecContext(ctx, `UPDATE dev_machine_agent_runs SET notebook_problem='the notebook would grow past its size limit; shorten the update' WHERE id=$1 AND task_id=$2`, task.RunID, task.ID); err != nil {
				return err
			}
			continue
		}
		notebook = merged
		if err = saveNotebook(ctx, tx, *task, row, notebook, domain.NotebookChange{Author: domain.NotebookAuthorAgent, Sections: sections, Summary: "update from " + file.Name}); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE dev_machine_agent_runs SET notebook_updates=notebook_updates+1,notebook_problem=NULL WHERE id=$1 AND task_id=$2`, task.RunID, task.ID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// NotebookUpdateState reports how many valid notebook updates the attempt
// left and why its last one was refused ("" when it was not).
func (r *DevMachineRepository) NotebookUpdateState(ctx context.Context, runID uuid.UUID) (int, string, error) {
	var row struct {
		Updates int     `db:"notebook_updates"`
		Problem *string `db:"notebook_problem"`
	}
	if err := r.db.GetContext(ctx, &row, `SELECT notebook_updates,notebook_problem FROM dev_machine_agent_runs WHERE id=$1`, runID); err != nil {
		return 0, "", err
	}
	if row.Problem != nil {
		return row.Updates, *row.Problem, nil
	}
	return row.Updates, "", nil
}

// AttemptNotebook returns the notebook the task's next attempt receives,
// creating it (with the task's request) when the task has none yet.
func (r *AgentTaskRepository) AttemptNotebook(ctx context.Context, task domain.AgentTask) (*domain.Notebook, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	current, err := notebookTaskOf(ctx, tx, task.ID)
	if err != nil {
		return nil, err
	}
	if current == nil || current.WorkspaceID != task.WorkspaceID {
		return nil, ErrConversationNotFound
	}
	_, notebook, err := lockNotebook(ctx, tx, *current)
	if err != nil {
		return nil, err
	}
	return &notebook, tx.Commit()
}

// Notebook returns the task's notebook and its newest changes to the task's
// creator, under the same checks as every task read (the workspace, the
// creator's current role and the issue's team, private teams included).
// Reading never writes: a task without a notebook yet shows its request.
func (r *AgentTaskRepository) Notebook(ctx context.Context, ws, id uuid.UUID) (*domain.TaskNotebook, error) {
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
	result := &domain.TaskNotebook{TaskID: id, History: []domain.NotebookChange{}}
	var row notebookRow
	err = tx.GetContext(ctx, &row, `SELECT task_id,workspace_id,document,revision,updated_at FROM agent_task_notebooks WHERE task_id=$1 AND workspace_id=$2`, id, ws)
	if errors.Is(err, sql.ErrNoRows) {
		result.Notebook = initialNotebook(notebookTask{ID: task.ID, WorkspaceID: task.WorkspaceID, CreatorID: task.CreatorID, Prompt: task.Prompt, CreatedAt: task.CreatedAt})
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	if result.Notebook, err = domain.DecodeNotebook(row.Document); err != nil {
		return nil, err
	}
	result.Revision, result.UpdatedAt = row.Revision, &row.UpdatedAt
	var changes []struct {
		domain.NotebookChange
		Sections pq.StringArray `db:"sections"`
	}
	if err = tx.SelectContext(ctx, &changes, `SELECT revision,author,user_id,run_id,generation,step_sequence,step_label,sections,summary,created_at FROM agent_task_notebook_changes WHERE task_id=$1 ORDER BY revision DESC LIMIT 50`, id); err != nil {
		return nil, err
	}
	for _, change := range changes {
		entry := change.NotebookChange
		entry.Sections = append([]string{}, change.Sections...)
		result.History = append(result.History, entry)
	}
	return result, nil
}

// AgentRunPrompt is the prompt an attempt received, by source.
type AgentRunPrompt struct {
	RunID uuid.UUID `json:"run_id"`
	// Recorded is false for attempts that ran before prompts were recorded.
	Recorded         bool                  `json:"recorded"`
	WorktreeReadOnly bool                  `json:"worktree_read_only"`
	Sources          []domain.PromptSource `json:"sources"`
	// MachineID is where the attempt ran; its secrets are redacted from the
	// sources before they are returned.
	MachineID uuid.UUID `json:"-"`
}

// RunPrompt returns the prompt an attempt received, split by source, to the
// task's creator (the same checks as the attempt's logs and diff).
func (r *AgentTaskRepository) RunPrompt(ctx context.Context, ws, id, run uuid.UUID) (*AgentRunPrompt, error) {
	if _, err := conversationActor(ctx, ws); err != nil {
		return nil, err
	}
	tx, _, err := r.transaction(ctx, id, true)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var row struct {
		MachineID uuid.UUID       `db:"machine_id"`
		ReadOnly  bool            `db:"worktree_read_only"`
		Sources   domain.NullJSON `db:"prompt_sources"`
	}
	err = tx.GetContext(ctx, &row, `SELECT machine_id,worktree_read_only,prompt_sources FROM dev_machine_agent_runs WHERE id=$1 AND task_id=$2 AND workspace_id=$3`, run, id, ws)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrConversationNotFound
	}
	if err != nil {
		return nil, err
	}
	result := &AgentRunPrompt{RunID: run, WorktreeReadOnly: row.ReadOnly, Sources: []domain.PromptSource{}, MachineID: row.MachineID}
	if len(row.Sources) > 0 {
		if err = json.Unmarshal(row.Sources, &result.Sources); err != nil {
			return nil, err
		}
		result.Recorded = true
	}
	return result, nil
}
