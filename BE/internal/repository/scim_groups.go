package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/lib/pq"
)

type scimGroupRow struct {
	domain.SCIMGroup
	MembersJSON json.RawMessage `db:"members"`
}

const scimGroupColumns = `id, external_id, display_name, team_id, created_at, updated_at,
	COALESCE((SELECT jsonb_agg(jsonb_build_object('value', gm.user_id) ORDER BY gm.user_id)
		FROM scim_group_members gm
		JOIN workspace_members wm ON wm.workspace_id=gm.workspace_id AND wm.user_id=gm.user_id
		WHERE gm.workspace_id=g.workspace_id AND gm.group_id=g.id), '[]') AS members`

func (row scimGroupRow) resource() (domain.SCIMGroup, error) {
	err := json.Unmarshal(row.MembersJSON, &row.Members)
	return row.SCIMGroup, err
}

func (r *SCIMTransaction) Group(ctx context.Context, id uuid.UUID) (domain.SCIMGroup, error) {
	var row scimGroupRow
	err := r.tx.GetContext(ctx, &row, `SELECT `+scimGroupColumns+` FROM scim_groups g WHERE workspace_id=$1 AND id=$2`, r.workspaceID, id)
	if err != nil {
		return domain.SCIMGroup{}, err
	}
	return row.resource()
}

func (r *SCIMTransaction) Groups(ctx context.Context, query domain.SCIMQuery) ([]domain.SCIMGroup, int, error) {
	const where = ` FROM scim_groups g WHERE workspace_id=$1 AND CASE $2::text
		WHEN '' THEN true
		WHEN 'displayname' THEN lower(display_name)=lower($3)
		WHEN 'externalid' THEN NULLIF(external_id,'')=$3
		END`
	var total int
	if err := r.tx.GetContext(ctx, &total, `SELECT count(*)`+where, r.workspaceID, query.Attribute, query.Value); err != nil {
		return nil, 0, err
	}
	var rows []scimGroupRow
	err := r.tx.SelectContext(ctx, &rows, `SELECT `+scimGroupColumns+where+` ORDER BY created_at, id LIMIT $4 OFFSET $5`,
		r.workspaceID, query.Attribute, query.Value, query.Count, query.Start-1)
	if err != nil {
		return nil, 0, err
	}
	groups := make([]domain.SCIMGroup, 0, len(rows))
	for _, row := range rows {
		group, err := row.resource()
		if err != nil {
			return nil, 0, err
		}
		groups = append(groups, group)
	}
	return groups, total, nil
}

func (r *SCIMTransaction) SaveGroup(ctx context.Context, group domain.SCIMGroup) (domain.SCIMGroup, error) {
	if group.ID == uuid.Nil {
		err := r.tx.GetContext(ctx, &group.ID, `INSERT INTO scim_groups (workspace_id,external_id,display_name)
			VALUES ($1,$2,$3) RETURNING id`, r.workspaceID, group.ExternalID, group.DisplayName)
		if err != nil {
			return group, err
		}
	} else {
		if _, err := r.tx.ExecContext(ctx, `UPDATE scim_groups SET external_id=$3, display_name=$4, updated_at=clock_timestamp()
			WHERE workspace_id=$1 AND id=$2`, r.workspaceID, group.ID, group.ExternalID, group.DisplayName); err != nil {
			return group, err
		}
	}
	ids := make([]string, 0, len(group.Members))
	for _, member := range group.Members {
		ids = append(ids, member.Value.String())
	}
	if _, err := r.tx.ExecContext(ctx, `DELETE FROM scim_group_members
		WHERE workspace_id=$1 AND group_id=$2 AND NOT (user_id=ANY($3::uuid[]))`, r.workspaceID, group.ID, pq.Array(ids)); err != nil {
		return group, err
	}
	// Compare the resulting membership with the requested set; a foreign,
	// unknown or inactive new user aborts the whole group update.
	if _, err := r.tx.ExecContext(ctx, `INSERT INTO scim_group_members(workspace_id, group_id, user_id)
		SELECT su.workspace_id, $2, su.user_id FROM scim_users su
		JOIN workspace_members wm ON wm.workspace_id=su.workspace_id AND wm.user_id=su.user_id
		WHERE su.workspace_id=$1 AND su.active AND su.user_id=ANY($3::uuid[])
		FOR SHARE OF wm ON CONFLICT DO NOTHING`, r.workspaceID, group.ID, pq.Array(ids)); err != nil {
		return group, err
	}
	saved, err := r.Group(ctx, group.ID)
	if err != nil {
		return group, err
	}
	if len(saved.Members) != len(ids) {
		return group, ErrSCIMMember
	}
	if saved.TeamID != nil {
		err = r.syncTeam(ctx, saved.ID, *saved.TeamID)
	}
	return saved, err
}

func (r *SCIMTransaction) DeleteGroup(ctx context.Context, id uuid.UUID) error {
	group, err := r.Group(ctx, id)
	if err != nil {
		return err
	}
	if _, err := r.tx.ExecContext(ctx, `DELETE FROM scim_groups WHERE workspace_id=$1 AND id=$2`, r.workspaceID, id); err != nil {
		return err
	}
	if group.TeamID != nil {
		return r.syncTeam(ctx, id, *group.TeamID)
	}
	return nil
}

func (r *SCIMTransaction) MapGroup(ctx context.Context, groupID uuid.UUID, teamID *uuid.UUID) error {
	group, err := r.Group(ctx, groupID)
	if err != nil {
		return err
	}
	if teamID != nil {
		var selected uuid.UUID
		err := r.tx.GetContext(ctx, &selected, `SELECT id FROM teams WHERE workspace_id=$1 AND id=$2 FOR KEY SHARE`, r.workspaceID, *teamID)
		if err != nil {
			return err
		}
	}
	if _, err := r.tx.ExecContext(ctx, `UPDATE scim_groups SET team_id=$3 WHERE workspace_id=$1 AND id=$2`, r.workspaceID, groupID, teamID); err != nil {
		return err
	}
	if group.TeamID != nil && (teamID == nil || *group.TeamID != *teamID) {
		// The old team has no desired members after the mapping moves.
		if err := r.syncTeam(ctx, uuid.Nil, *group.TeamID); err != nil {
			return err
		}
	}
	if teamID != nil {
		return r.syncTeam(ctx, groupID, *teamID)
	}
	return nil
}

func (r *SCIMTransaction) syncTeam(ctx context.Context, groupID, teamID uuid.UUID) error {
	var selected uuid.UUID
	err := r.tx.GetContext(ctx, &selected, `SELECT id FROM teams WHERE workspace_id=$1 AND id=$2 FOR KEY SHARE`, r.workspaceID, teamID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil // The team was deleted, which also clears its mapping and grants.
	}
	if err != nil {
		return err
	}
	const desired = `SELECT gm.user_id FROM scim_group_members gm
		JOIN workspace_members wm ON wm.workspace_id=gm.workspace_id AND wm.user_id=gm.user_id
		WHERE gm.workspace_id=$1 AND gm.group_id=$2 FOR SHARE OF wm`
	if _, err := r.tx.ExecContext(ctx, `DELETE FROM team_members tm USING scim_team_grants sg
		WHERE tm.team_id=$3 AND tm.team_id=sg.team_id AND tm.user_id=sg.user_id
		AND tm.user_id NOT IN (`+desired+`)`, r.workspaceID, groupID, teamID); err != nil {
		return err
	}
	_, err = r.tx.ExecContext(ctx, `WITH added AS (
		INSERT INTO team_members(team_id,user_id) SELECT $3, desired.user_id FROM (`+desired+`) desired
		ON CONFLICT DO NOTHING RETURNING team_id,user_id
	)
	INSERT INTO scim_team_grants(team_id,user_id) SELECT team_id,user_id FROM added`, r.workspaceID, groupID, teamID)
	return err
}
