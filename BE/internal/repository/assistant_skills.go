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
	// ErrAssistantSkillDenied: only current workspace owners and admins write skills.
	ErrAssistantSkillDenied = errors.New("assistant skills are written by workspace owners and admins")
	// ErrAssistantSkillNotFound covers missing skills, versions and teams, and
	// those of private teams the caller cannot see.
	ErrAssistantSkillNotFound  = errors.New("assistant skill not found")
	ErrAssistantSkillNameTaken = errors.New("a skill with this name already exists in this scope")
	// ErrAssistantSkillNotDraft: the version to activate is not a pending draft.
	ErrAssistantSkillNotDraft = errors.New("there is no draft to activate")
)

// AssistantSkillRepository stores workspace skills and their versions. Reads
// apply the caller's team visibility from the request context; writes check the
// actor's current owner or admin role inside the transaction that writes.
type AssistantSkillRepository struct{ db *sqlx.DB }

func NewAssistantSkillRepository(db *sqlx.DB) *AssistantSkillRepository {
	return &AssistantSkillRepository{db}
}

// skillSelect reads skills with their team and version summary; body is the
// column expression for the body (” in lists).
func skillSelect(ctx context.Context, body string) string {
	return `SELECT s.id,s.workspace_id,s.team_id,t.key AS team_key,t.name AS team_name,s.name,s.description,` + body + ` AS body,
 s.enabled,s.status,s.current_version,
 (SELECT max(v.version) FROM assistant_skill_versions v WHERE v.skill_id=s.id) AS latest_version,
 (SELECT max(v.version) FROM assistant_skill_versions v WHERE v.skill_id=s.id AND v.version>s.current_version AND v.status='draft') AS pending_version,
 s.created_by_user_id,s.author_kind,s.created_at,s.updated_at
 FROM assistant_skills s LEFT JOIN teams t ON t.id=s.team_id
 WHERE ` + workspaceVisible(ctx, "s.workspace_id") + ` AND (s.team_id IS NULL OR ` + teamVisible(ctx, "s.team_id") + `)`
}

// List returns the workspace's skills the caller can see, without bodies:
// workspace-wide skills and those of teams visible to the caller.
func (r *AssistantSkillRepository) List(ctx context.Context, ws uuid.UUID) ([]domain.AssistantSkill, error) {
	skills := []domain.AssistantSkill{}
	err := r.db.SelectContext(ctx, &skills, skillSelect(ctx, "''")+` AND s.workspace_id=$1 ORDER BY t.key NULLS FIRST, lower(s.name)`, ws)
	return skills, err
}

// Get returns one visible skill with its body.
func (r *AssistantSkillRepository) Get(ctx context.Context, ws, id uuid.UUID) (*domain.AssistantSkill, error) {
	return getSkill(ctx, r.db, ws, id)
}

func getSkill(ctx context.Context, db sqlx.QueryerContext, ws, id uuid.UUID) (*domain.AssistantSkill, error) {
	var skill domain.AssistantSkill
	err := sqlx.GetContext(ctx, db, &skill, skillSelect(ctx, "s.body")+` AND s.workspace_id=$1 AND s.id=$2`, ws, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAssistantSkillNotFound
	}
	if err != nil {
		return nil, err
	}
	return &skill, nil
}

// Versions lists a visible skill's saved versions, newest first.
func (r *AssistantSkillRepository) Versions(ctx context.Context, ws, id uuid.UUID) ([]domain.AssistantSkillVersion, error) {
	if _, err := r.Get(ctx, ws, id); err != nil {
		return nil, err
	}
	versions := []domain.AssistantSkillVersion{}
	err := r.db.SelectContext(ctx, &versions, `SELECT * FROM assistant_skill_versions WHERE skill_id=$1 ORDER BY version DESC`, id)
	return versions, err
}

// ForAssistant lists the active, enabled skills the caller can see, without
// bodies, for the assistant's system prompt. With a team, only workspace-wide
// skills and that team's skills are listed; team skills come first.
func (r *AssistantSkillRepository) ForAssistant(ctx context.Context, ws uuid.UUID, team *uuid.UUID, limit int) ([]domain.AssistantSkill, error) {
	skills := []domain.AssistantSkill{}
	err := r.db.SelectContext(ctx, &skills, skillSelect(ctx, "''")+` AND s.workspace_id=$1 AND s.status='active' AND s.enabled
 AND ($2::uuid IS NULL OR s.team_id IS NULL OR s.team_id=$2) ORDER BY s.team_id IS NULL, t.key, lower(s.name) LIMIT $3`, ws, team, limit)
	return skills, err
}

// LoadForAssistant returns the active, enabled skills with this name (ignoring
// case) that ForAssistant would list, with bodies; teamKey, when given, keeps
// only that team's skill. The caller resolves ambiguity.
func (r *AssistantSkillRepository) LoadForAssistant(ctx context.Context, ws uuid.UUID, team *uuid.UUID, name, teamKey string) ([]domain.AssistantSkill, error) {
	skills := []domain.AssistantSkill{}
	err := r.db.SelectContext(ctx, &skills, skillSelect(ctx, "s.body")+` AND s.workspace_id=$1 AND s.status='active' AND s.enabled
 AND ($2::uuid IS NULL OR s.team_id IS NULL OR s.team_id=$2) AND lower(s.name)=lower($3) AND ($4='' OR lower(t.key)=lower($4))
 ORDER BY s.team_id IS NULL, t.key LIMIT 5`, ws, team, name, teamKey)
	return skills, err
}

// lockSkillAdmin holds the actor's owner or admin membership until commit, so
// a concurrent demotion waits for the write or is seen by it.
func lockSkillAdmin(ctx context.Context, tx *sqlx.Tx, ws, actor uuid.UUID) error {
	var role string
	err := tx.GetContext(ctx, &role, `SELECT role FROM workspace_members WHERE workspace_id=$1 AND user_id=$2 AND role IN ('owner','admin') FOR SHARE`, ws, actor)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrAssistantSkillDenied
	}
	return err
}

type skillRow struct {
	ID             uuid.UUID  `db:"id"`
	TeamID         *uuid.UUID `db:"team_id"`
	Status         string     `db:"status"`
	CurrentVersion int        `db:"current_version"`
}

func (r *AssistantSkillRepository) write(ctx context.Context, ws, actor uuid.UUID, fn func(tx *sqlx.Tx) (uuid.UUID, error)) (*domain.AssistantSkill, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = lockSkillAdmin(ctx, tx, ws, actor); err != nil {
		return nil, err
	}
	id, err := fn(tx)
	if err != nil {
		return nil, skillWriteError(err)
	}
	skill, err := getSkill(ctx, tx, ws, id)
	if err != nil {
		return nil, err
	}
	return skill, tx.Commit()
}

func skillWriteError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "assistant_skills_name" {
		return ErrAssistantSkillNameTaken
	}
	return err
}

// lockSkill locks a visible skill of the workspace for a write.
func lockSkill(ctx context.Context, tx *sqlx.Tx, ws, id uuid.UUID) (*skillRow, error) {
	var row skillRow
	err := tx.GetContext(ctx, &row, `SELECT s.id,s.team_id,s.status,s.current_version FROM assistant_skills s WHERE s.workspace_id=$1 AND s.id=$2
 AND (s.team_id IS NULL OR `+teamVisible(ctx, "s.team_id")+`) FOR UPDATE`, ws, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAssistantSkillNotFound
	}
	return &row, err
}

// nameTaken reports whether another skill in the same scope has this name.
func nameTaken(ctx context.Context, tx *sqlx.Tx, ws uuid.UUID, team *uuid.UUID, name string, except uuid.UUID) (bool, error) {
	var taken bool
	err := tx.GetContext(ctx, &taken, `SELECT EXISTS(SELECT 1 FROM assistant_skills WHERE workspace_id=$1 AND team_id IS NOT DISTINCT FROM $2 AND lower(name)=lower($3) AND id<>$4)`, ws, team, name, except)
	return taken, err
}

func insertSkill(ctx context.Context, tx *sqlx.Tx, ws, actor uuid.UUID, in domain.AssistantSkillInput) (uuid.UUID, error) {
	if in.TeamID != nil {
		var found bool
		if err := tx.GetContext(ctx, &found, `SELECT EXISTS(SELECT 1 FROM teams t WHERE t.id=$1 AND t.workspace_id=$2 AND `+teamVisible(ctx, "t.id")+`)`, *in.TeamID, ws); err != nil {
			return uuid.Nil, err
		}
		if !found {
			return uuid.Nil, ErrAssistantSkillNotFound
		}
	}
	var id uuid.UUID
	if err := tx.GetContext(ctx, &id, `INSERT INTO assistant_skills(workspace_id,team_id,name,description,body,status,current_version,created_by_user_id,author_kind)
 VALUES($1,$2,$3,$4,$5,$6,1,$7,$8) RETURNING id`, ws, in.TeamID, in.Name, in.Description, in.Body, in.Status, actor, in.AuthorKind); err != nil {
		return uuid.Nil, err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO assistant_skill_versions(skill_id,version,name,description,body,status,author_kind,author_user_id) VALUES($1,1,$2,$3,$4,$5,$6,$7)`,
		id, in.Name, in.Description, in.Body, in.Status, in.AuthorKind, actor)
	return id, err
}

// saveVersion records an edit as the skill's next version. A draft edit of an
// active skill leaves the skill as it is until the draft is activated; any
// other edit takes effect at once and keeps or sets the requested status.
func saveVersion(ctx context.Context, tx *sqlx.Tx, ws, actor uuid.UUID, skill *skillRow, in domain.AssistantSkillInput) error {
	status := in.Status
	if status == "" {
		status = skill.Status
	}
	taken, err := nameTaken(ctx, tx, ws, skill.TeamID, in.Name, skill.ID)
	if err != nil {
		return err
	}
	if taken {
		return ErrAssistantSkillNameTaken
	}
	var next int
	if err = tx.GetContext(ctx, &next, `SELECT max(version)+1 FROM assistant_skill_versions WHERE skill_id=$1`, skill.ID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO assistant_skill_versions(skill_id,version,name,description,body,status,author_kind,author_user_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`,
		skill.ID, next, in.Name, in.Description, in.Body, status, in.AuthorKind, actor); err != nil {
		return err
	}
	if skill.Status == domain.AssistantSkillActive && status == domain.AssistantSkillDraft {
		_, err = tx.ExecContext(ctx, `UPDATE assistant_skills SET updated_at=now() WHERE id=$1`, skill.ID)
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE assistant_skills SET name=$2,description=$3,body=$4,status=$5,current_version=$6,updated_at=now() WHERE id=$1`,
		skill.ID, in.Name, in.Description, in.Body, status, next)
	return err
}

// Create adds a skill with its first version.
func (r *AssistantSkillRepository) Create(ctx context.Context, ws, actor uuid.UUID, in domain.AssistantSkillInput) (*domain.AssistantSkill, error) {
	return r.write(ctx, ws, actor, func(tx *sqlx.Tx) (uuid.UUID, error) {
		return insertSkill(ctx, tx, ws, actor, in)
	})
}

// Update saves an edit as a new version; the team scope does not change.
func (r *AssistantSkillRepository) Update(ctx context.Context, ws, id, actor uuid.UUID, in domain.AssistantSkillInput) (*domain.AssistantSkill, error) {
	return r.write(ctx, ws, actor, func(tx *sqlx.Tx) (uuid.UUID, error) {
		skill, err := lockSkill(ctx, tx, ws, id)
		if err != nil {
			return uuid.Nil, err
		}
		return id, saveVersion(ctx, tx, ws, actor, skill, in)
	})
}

// Propose saves the assistant's draft: a new draft skill, or a draft version
// of the skill with this name in the same scope. It never activates anything.
func (r *AssistantSkillRepository) Propose(ctx context.Context, ws, actor uuid.UUID, in domain.AssistantSkillInput) (*domain.AssistantSkill, int, error) {
	in.Status, in.AuthorKind = domain.AssistantSkillDraft, "assistant"
	version := 1
	skill, err := r.write(ctx, ws, actor, func(tx *sqlx.Tx) (uuid.UUID, error) {
		var id uuid.UUID
		err := tx.GetContext(ctx, &id, `SELECT id FROM assistant_skills WHERE workspace_id=$1 AND team_id IS NOT DISTINCT FROM $2 AND lower(name)=lower($3)`, ws, in.TeamID, in.Name)
		if errors.Is(err, sql.ErrNoRows) {
			return insertSkill(ctx, tx, ws, actor, in)
		}
		if err != nil {
			return uuid.Nil, err
		}
		existing, err := lockSkill(ctx, tx, ws, id)
		if err != nil {
			return uuid.Nil, err
		}
		if err = saveVersion(ctx, tx, ws, actor, existing, in); err != nil {
			return uuid.Nil, err
		}
		return id, tx.GetContext(ctx, &version, `SELECT max(version) FROM assistant_skill_versions WHERE skill_id=$1`, id)
	})
	return skill, version, err
}

// Restore saves an earlier version's content as the newest version, with the
// skill's current status.
func (r *AssistantSkillRepository) Restore(ctx context.Context, ws, id, actor uuid.UUID, version int) (*domain.AssistantSkill, error) {
	return r.write(ctx, ws, actor, func(tx *sqlx.Tx) (uuid.UUID, error) {
		skill, err := lockSkill(ctx, tx, ws, id)
		if err != nil {
			return uuid.Nil, err
		}
		var old domain.AssistantSkillVersion
		err = tx.GetContext(ctx, &old, `SELECT * FROM assistant_skill_versions WHERE skill_id=$1 AND version=$2`, id, version)
		if errors.Is(err, sql.ErrNoRows) {
			return uuid.Nil, ErrAssistantSkillNotFound
		}
		if err != nil {
			return uuid.Nil, err
		}
		return id, saveVersion(ctx, tx, ws, actor, skill, domain.AssistantSkillInput{Name: old.Name, Description: old.Description, Body: old.Body, AuthorKind: "user"})
	})
}

// Activate puts a draft into effect: the given draft version, or else the
// newest pending draft (or the draft skill's own content).
func (r *AssistantSkillRepository) Activate(ctx context.Context, ws, id, actor uuid.UUID, version *int) (*domain.AssistantSkill, error) {
	return r.write(ctx, ws, actor, func(tx *sqlx.Tx) (uuid.UUID, error) {
		skill, err := lockSkill(ctx, tx, ws, id)
		if err != nil {
			return uuid.Nil, err
		}
		var draft domain.AssistantSkillVersion
		err = tx.GetContext(ctx, &draft, `SELECT * FROM assistant_skill_versions WHERE skill_id=$1 AND status='draft'
 AND (version>$2 OR ($3 AND version=$2)) AND ($4::int IS NULL OR version=$4) ORDER BY version DESC LIMIT 1`,
			id, skill.CurrentVersion, skill.Status == domain.AssistantSkillDraft, version)
		if errors.Is(err, sql.ErrNoRows) {
			return uuid.Nil, ErrAssistantSkillNotDraft
		}
		if err != nil {
			return uuid.Nil, err
		}
		taken, err := nameTaken(ctx, tx, ws, skill.TeamID, draft.Name, id)
		if err != nil {
			return uuid.Nil, err
		}
		if taken {
			return uuid.Nil, ErrAssistantSkillNameTaken
		}
		if _, err = tx.ExecContext(ctx, `UPDATE assistant_skill_versions SET status='active' WHERE skill_id=$1 AND version=$2`, id, draft.Version); err != nil {
			return uuid.Nil, err
		}
		_, err = tx.ExecContext(ctx, `UPDATE assistant_skills SET name=$2,description=$3,body=$4,status='active',current_version=$5,updated_at=now() WHERE id=$1`,
			id, draft.Name, draft.Description, draft.Body, draft.Version)
		return id, err
	})
}

// SetEnabled turns a skill on or off without changing its content.
func (r *AssistantSkillRepository) SetEnabled(ctx context.Context, ws, id, actor uuid.UUID, enabled bool) (*domain.AssistantSkill, error) {
	return r.write(ctx, ws, actor, func(tx *sqlx.Tx) (uuid.UUID, error) {
		if _, err := lockSkill(ctx, tx, ws, id); err != nil {
			return uuid.Nil, err
		}
		_, err := tx.ExecContext(ctx, `UPDATE assistant_skills SET enabled=$2,updated_at=now() WHERE id=$1`, id, enabled)
		return id, err
	})
}

// Delete removes a skill and its versions.
func (r *AssistantSkillRepository) Delete(ctx context.Context, ws, id, actor uuid.UUID) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = lockSkillAdmin(ctx, tx, ws, actor); err != nil {
		return err
	}
	if _, err = lockSkill(ctx, tx, ws, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM assistant_skills WHERE id=$1`, id); err != nil {
		return err
	}
	return tx.Commit()
}
