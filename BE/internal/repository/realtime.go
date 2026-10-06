package repository

import (
	"context"
	"github.com/google/uuid"
)

// RealtimeAccess is intentionally uncached. Open sockets must observe workspace
// removal and privacy changes at delivery, not only at the upgrade handshake.
func (r *WorkspaceRepository) RealtimeAccess(ctx context.Context, workspaceID, userID uuid.UUID) (bool, bool, error) {
	var policy struct {
		Member  bool `db:"member"`
		Private bool `db:"private"`
	}
	err := r.db.GetContext(ctx, &policy, `SELECT
 EXISTS(SELECT 1 FROM workspace_members WHERE workspace_id=$1 AND user_id=$2) AS member,
 EXISTS(SELECT 1 FROM workspace_privacy WHERE workspace_id=$1) AS private`, workspaceID, userID)
	return policy.Member, policy.Private, err
}
