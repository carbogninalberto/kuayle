package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

var (
	// ErrAssistantToolDenied: only current workspace owners and admins write
	// custom tools and test-run them.
	ErrAssistantToolDenied = errors.New("custom tools are written by workspace owners and admins")
	// ErrAssistantToolNotFound covers missing tools, versions and runs, and
	// runs the caller may not read.
	ErrAssistantToolNotFound  = errors.New("custom tool not found")
	ErrAssistantToolNameTaken = errors.New("a custom tool with this name already exists")
	// ErrAssistantToolNotDraft: the version to activate is not a pending draft.
	ErrAssistantToolNotDraft = errors.New("there is no draft to activate")
)

// AssistantCustomToolRepository stores workspace custom tools, their versions
// and runs. Writes check the actor's current owner or admin role inside the
// transaction that writes.
type AssistantCustomToolRepository struct{ db *sqlx.DB }

func NewAssistantCustomToolRepository(db *sqlx.DB) *AssistantCustomToolRepository {
	return &AssistantCustomToolRepository{db}
}

// toolSelect reads tools with their version summary; script is the column
// expression for the script (” in lists).
func toolSelect(ctx context.Context, script string) string {
	return `SELECT t.id,t.workspace_id,t.name,t.description,t.parameters,` + script + ` AS script,t.interpreter,t.run_mode,t.read_only,t.timeout_seconds,
 t.enabled,t.status,t.current_version,
 (SELECT max(v.version) FROM assistant_custom_tool_versions v WHERE v.tool_id=t.id) AS latest_version,
 (SELECT max(v.version) FROM assistant_custom_tool_versions v WHERE v.tool_id=t.id AND v.version>t.current_version AND v.status='draft') AS pending_version,
 t.created_by_user_id,t.author_kind,t.created_at,t.updated_at
 FROM assistant_custom_tools t WHERE ` + workspaceVisible(ctx, "t.workspace_id")
}

// List returns the workspace's tools without scripts.
func (r *AssistantCustomToolRepository) List(ctx context.Context, ws uuid.UUID) ([]domain.AssistantCustomTool, error) {
	tools := []domain.AssistantCustomTool{}
	err := r.db.SelectContext(ctx, &tools, toolSelect(ctx, "''")+` AND t.workspace_id=$1 ORDER BY t.name`, ws)
	return tools, err
}

// Get returns one tool with its script.
func (r *AssistantCustomToolRepository) Get(ctx context.Context, ws, id uuid.UUID) (*domain.AssistantCustomTool, error) {
	return getTool(ctx, r.db, ws, id)
}

func getTool(ctx context.Context, db sqlx.QueryerContext, ws, id uuid.UUID) (*domain.AssistantCustomTool, error) {
	var tool domain.AssistantCustomTool
	err := sqlx.GetContext(ctx, db, &tool, toolSelect(ctx, "t.script")+` AND t.workspace_id=$1 AND t.id=$2`, ws, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAssistantToolNotFound
	}
	if err != nil {
		return nil, err
	}
	return &tool, nil
}

// Versions lists a tool's saved versions, newest first.
func (r *AssistantCustomToolRepository) Versions(ctx context.Context, ws, id uuid.UUID) ([]domain.AssistantCustomToolVersion, error) {
	if _, err := r.Get(ctx, ws, id); err != nil {
		return nil, err
	}
	versions := []domain.AssistantCustomToolVersion{}
	err := r.db.SelectContext(ctx, &versions, `SELECT * FROM assistant_custom_tool_versions WHERE tool_id=$1 ORDER BY version DESC`, id)
	return versions, err
}

// Version returns one saved version of a workspace tool.
func (r *AssistantCustomToolRepository) Version(ctx context.Context, ws, id uuid.UUID, version int) (*domain.AssistantCustomToolVersion, error) {
	var found domain.AssistantCustomToolVersion
	err := r.db.GetContext(ctx, &found, `SELECT v.* FROM assistant_custom_tool_versions v JOIN assistant_custom_tools t ON t.id=v.tool_id
 WHERE t.workspace_id=$1 AND t.id=$2 AND v.version=$3 AND `+workspaceVisible(ctx, "t.workspace_id"), ws, id, version)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAssistantToolNotFound
	}
	if err != nil {
		return nil, err
	}
	return &found, nil
}

// ForAssistant lists the active, enabled tools offered to the model, without
// scripts.
func (r *AssistantCustomToolRepository) ForAssistant(ctx context.Context, ws uuid.UUID, limit int) ([]domain.AssistantCustomTool, error) {
	tools := []domain.AssistantCustomTool{}
	err := r.db.SelectContext(ctx, &tools, toolSelect(ctx, "''")+` AND t.workspace_id=$1 AND t.status='active' AND t.enabled ORDER BY t.name LIMIT $2`, ws, limit)
	return tools, err
}

// ActiveByName returns the active, enabled tool with this name, without its
// script, or ErrAssistantToolNotFound.
func (r *AssistantCustomToolRepository) ActiveByName(ctx context.Context, ws uuid.UUID, name string) (*domain.AssistantCustomTool, error) {
	var tool domain.AssistantCustomTool
	err := r.db.GetContext(ctx, &tool, toolSelect(ctx, "''")+` AND t.workspace_id=$1 AND t.name=$2 AND t.status='active' AND t.enabled`, ws, name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAssistantToolNotFound
	}
	if err != nil {
		return nil, err
	}
	return &tool, nil
}

// lockToolAdmin holds the actor's owner or admin membership until commit, so
// a concurrent demotion waits for the write or is seen by it.
func lockToolAdmin(ctx context.Context, tx *sqlx.Tx, ws, actor uuid.UUID) error {
	var role string
	err := tx.GetContext(ctx, &role, `SELECT role FROM workspace_members WHERE workspace_id=$1 AND user_id=$2 AND role IN ('owner','admin') FOR SHARE`, ws, actor)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrAssistantToolDenied
	}
	return err
}

type toolRow struct {
	ID             uuid.UUID `db:"id"`
	Status         string    `db:"status"`
	CurrentVersion int       `db:"current_version"`
}

func (r *AssistantCustomToolRepository) write(ctx context.Context, ws, actor uuid.UUID, fn func(tx *sqlx.Tx) (uuid.UUID, error)) (*domain.AssistantCustomTool, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = lockToolAdmin(ctx, tx, ws, actor); err != nil {
		return nil, err
	}
	id, err := fn(tx)
	if err != nil {
		return nil, toolWriteError(err)
	}
	tool, err := getTool(ctx, tx, ws, id)
	if err != nil {
		return nil, err
	}
	return tool, tx.Commit()
}

func toolWriteError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "assistant_custom_tools_name" {
		return ErrAssistantToolNameTaken
	}
	return err
}

func lockTool(ctx context.Context, tx *sqlx.Tx, ws, id uuid.UUID) (*toolRow, error) {
	var row toolRow
	err := tx.GetContext(ctx, &row, `SELECT id,status,current_version FROM assistant_custom_tools WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, ws, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAssistantToolNotFound
	}
	return &row, err
}

func toolNameTaken(ctx context.Context, tx *sqlx.Tx, ws uuid.UUID, name string, except uuid.UUID) (bool, error) {
	var taken bool
	err := tx.GetContext(ctx, &taken, `SELECT EXISTS(SELECT 1 FROM assistant_custom_tools WHERE workspace_id=$1 AND name=$2 AND id<>$3)`, ws, name, except)
	return taken, err
}

const toolVersionColumns = `tool_id,version,name,description,parameters,script,interpreter,run_mode,read_only,timeout_seconds,status,author_kind,author_user_id`

func insertToolVersion(ctx context.Context, tx *sqlx.Tx, id uuid.UUID, version int, actor uuid.UUID, in domain.AssistantCustomToolInput, status string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO assistant_custom_tool_versions(`+toolVersionColumns+`) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		id, version, in.Name, in.Description, in.Parameters, in.Script, in.Interpreter, in.RunMode, in.ReadOnly, in.TimeoutSeconds, status, in.AuthorKind, actor)
	return err
}

func insertTool(ctx context.Context, tx *sqlx.Tx, ws, actor uuid.UUID, in domain.AssistantCustomToolInput) (uuid.UUID, error) {
	var id uuid.UUID
	if err := tx.GetContext(ctx, &id, `INSERT INTO assistant_custom_tools(workspace_id,name,description,parameters,script,interpreter,run_mode,read_only,timeout_seconds,status,current_version,created_by_user_id,author_kind)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,1,$11,$12) RETURNING id`, ws, in.Name, in.Description, in.Parameters, in.Script, in.Interpreter, in.RunMode, in.ReadOnly, in.TimeoutSeconds, in.Status, actor, in.AuthorKind); err != nil {
		return uuid.Nil, err
	}
	return id, insertToolVersion(ctx, tx, id, 1, actor, in, in.Status)
}

// saveToolVersion records an edit as the tool's next version. A draft edit
// of an active tool leaves the tool as it is until the draft is activated;
// any other edit takes effect at once and keeps or sets the requested status.
func saveToolVersion(ctx context.Context, tx *sqlx.Tx, ws, actor uuid.UUID, tool *toolRow, in domain.AssistantCustomToolInput) (int, error) {
	status := in.Status
	if status == "" {
		status = tool.Status
	}
	taken, err := toolNameTaken(ctx, tx, ws, in.Name, tool.ID)
	if err != nil {
		return 0, err
	}
	if taken {
		return 0, ErrAssistantToolNameTaken
	}
	var next int
	if err = tx.GetContext(ctx, &next, `SELECT max(version)+1 FROM assistant_custom_tool_versions WHERE tool_id=$1`, tool.ID); err != nil {
		return 0, err
	}
	if err = insertToolVersion(ctx, tx, tool.ID, next, actor, in, status); err != nil {
		return 0, err
	}
	if tool.Status == domain.AssistantSkillActive && status == domain.AssistantSkillDraft {
		_, err = tx.ExecContext(ctx, `UPDATE assistant_custom_tools SET updated_at=now() WHERE id=$1`, tool.ID)
		return next, err
	}
	return next, applyToolVersion(ctx, tx, tool.ID, next, status)
}

// applyToolVersion copies a saved version's content into the tool row.
func applyToolVersion(ctx context.Context, tx *sqlx.Tx, id uuid.UUID, version int, status string) error {
	_, err := tx.ExecContext(ctx, `UPDATE assistant_custom_tools t SET name=v.name,description=v.description,parameters=v.parameters,script=v.script,interpreter=v.interpreter,
 run_mode=v.run_mode,read_only=v.read_only,timeout_seconds=v.timeout_seconds,status=$3,current_version=v.version,updated_at=now()
 FROM assistant_custom_tool_versions v WHERE t.id=$1 AND v.tool_id=t.id AND v.version=$2`, id, version, status)
	return err
}

// Create adds a tool with its first version.
func (r *AssistantCustomToolRepository) Create(ctx context.Context, ws, actor uuid.UUID, in domain.AssistantCustomToolInput) (*domain.AssistantCustomTool, error) {
	return r.write(ctx, ws, actor, func(tx *sqlx.Tx) (uuid.UUID, error) {
		return insertTool(ctx, tx, ws, actor, in)
	})
}

// Update saves an edit as a new version.
func (r *AssistantCustomToolRepository) Update(ctx context.Context, ws, id, actor uuid.UUID, in domain.AssistantCustomToolInput) (*domain.AssistantCustomTool, error) {
	return r.write(ctx, ws, actor, func(tx *sqlx.Tx) (uuid.UUID, error) {
		tool, err := lockTool(ctx, tx, ws, id)
		if err != nil {
			return uuid.Nil, err
		}
		_, err = saveToolVersion(ctx, tx, ws, actor, tool, in)
		return id, err
	})
}

// Propose saves the assistant's draft: a new draft tool, or a draft version
// of the tool with this name. It never activates anything and never asks for
// auto runs.
func (r *AssistantCustomToolRepository) Propose(ctx context.Context, ws, actor uuid.UUID, in domain.AssistantCustomToolInput) (*domain.AssistantCustomTool, int, error) {
	in.Status, in.AuthorKind, in.RunMode = domain.AssistantSkillDraft, "assistant", domain.AssistantToolRunApproval
	version := 1
	tool, err := r.write(ctx, ws, actor, func(tx *sqlx.Tx) (uuid.UUID, error) {
		var id uuid.UUID
		err := tx.GetContext(ctx, &id, `SELECT id FROM assistant_custom_tools WHERE workspace_id=$1 AND name=$2`, ws, in.Name)
		if errors.Is(err, sql.ErrNoRows) {
			return insertTool(ctx, tx, ws, actor, in)
		}
		if err != nil {
			return uuid.Nil, err
		}
		existing, err := lockTool(ctx, tx, ws, id)
		if err != nil {
			return uuid.Nil, err
		}
		version, err = saveToolVersion(ctx, tx, ws, actor, existing, in)
		return id, err
	})
	return tool, version, err
}

// Restore saves an earlier version's content as the newest version, with the
// tool's current status.
func (r *AssistantCustomToolRepository) Restore(ctx context.Context, ws, id, actor uuid.UUID, version int) (*domain.AssistantCustomTool, error) {
	return r.write(ctx, ws, actor, func(tx *sqlx.Tx) (uuid.UUID, error) {
		tool, err := lockTool(ctx, tx, ws, id)
		if err != nil {
			return uuid.Nil, err
		}
		var old domain.AssistantCustomToolVersion
		err = tx.GetContext(ctx, &old, `SELECT * FROM assistant_custom_tool_versions WHERE tool_id=$1 AND version=$2`, id, version)
		if errors.Is(err, sql.ErrNoRows) {
			return uuid.Nil, ErrAssistantToolNotFound
		}
		if err != nil {
			return uuid.Nil, err
		}
		_, err = saveToolVersion(ctx, tx, ws, actor, tool, domain.AssistantCustomToolInput{Name: old.Name, Description: old.Description, Parameters: old.Parameters, Script: old.Script,
			Interpreter: old.Interpreter, RunMode: old.RunMode, ReadOnly: old.ReadOnly, TimeoutSeconds: old.TimeoutSeconds, AuthorKind: "user"})
		return id, err
	})
}

// Activate puts a draft into effect: the given draft version, or else the
// newest pending draft (or the draft tool's own content).
func (r *AssistantCustomToolRepository) Activate(ctx context.Context, ws, id, actor uuid.UUID, version *int) (*domain.AssistantCustomTool, error) {
	return r.write(ctx, ws, actor, func(tx *sqlx.Tx) (uuid.UUID, error) {
		tool, err := lockTool(ctx, tx, ws, id)
		if err != nil {
			return uuid.Nil, err
		}
		var draft domain.AssistantCustomToolVersion
		err = tx.GetContext(ctx, &draft, `SELECT * FROM assistant_custom_tool_versions WHERE tool_id=$1 AND status='draft'
 AND (version>$2 OR ($3 AND version=$2)) AND ($4::int IS NULL OR version=$4) ORDER BY version DESC LIMIT 1`,
			id, tool.CurrentVersion, tool.Status == domain.AssistantSkillDraft, version)
		if errors.Is(err, sql.ErrNoRows) {
			return uuid.Nil, ErrAssistantToolNotDraft
		}
		if err != nil {
			return uuid.Nil, err
		}
		taken, err := toolNameTaken(ctx, tx, ws, draft.Name, id)
		if err != nil {
			return uuid.Nil, err
		}
		if taken {
			return uuid.Nil, ErrAssistantToolNameTaken
		}
		if _, err = tx.ExecContext(ctx, `UPDATE assistant_custom_tool_versions SET status='active' WHERE tool_id=$1 AND version=$2`, id, draft.Version); err != nil {
			return uuid.Nil, err
		}
		return id, applyToolVersion(ctx, tx, id, draft.Version, domain.AssistantSkillActive)
	})
}

// SetEnabled turns a tool on or off without changing its content.
func (r *AssistantCustomToolRepository) SetEnabled(ctx context.Context, ws, id, actor uuid.UUID, enabled bool) (*domain.AssistantCustomTool, error) {
	return r.write(ctx, ws, actor, func(tx *sqlx.Tx) (uuid.UUID, error) {
		if _, err := lockTool(ctx, tx, ws, id); err != nil {
			return uuid.Nil, err
		}
		_, err := tx.ExecContext(ctx, `UPDATE assistant_custom_tools SET enabled=$2,updated_at=now() WHERE id=$1`, id, enabled)
		return id, err
	})
}

// Delete removes a tool, its versions and its run history.
func (r *AssistantCustomToolRepository) Delete(ctx context.Context, ws, id, actor uuid.UUID) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = lockToolAdmin(ctx, tx, ws, actor); err != nil {
		return err
	}
	if _, err = lockTool(ctx, tx, ws, id); err != nil {
		return err
	}
	// A queued or running run still holds the machine; it fails on its own
	// once the manager sees the tool is gone.
	if _, err = tx.ExecContext(ctx, `DELETE FROM assistant_custom_tools WHERE id=$1`, id); err != nil {
		return err
	}
	return tx.Commit()
}
