package service

import (
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// Only the task's creator and workspace owners and admins may decide, as the
// repository enforces; other members are not offered decision buttons that
// would fail (review finding 8).
func TestWorkflowDecidersMatchTheRepositoryRule(t *testing.T) {
	creator, other := uuid.New(), uuid.New()
	for _, tc := range []struct {
		name   string
		actor  uuid.UUID
		role   string
		status string
		want   bool
	}{
		{"creator while waiting", creator, domain.RoleMember, domain.WorkflowStatusWaiting, true},
		{"creator while paused", creator, domain.RoleMember, domain.WorkflowStatusPaused, true},
		{"admin", other, domain.RoleAdmin, domain.WorkflowStatusPaused, true},
		{"owner", other, domain.RoleOwner, domain.WorkflowStatusWaiting, true},
		{"another member", other, domain.RoleMember, domain.WorkflowStatusWaiting, false},
		{"a guest", other, domain.RoleGuest, domain.WorkflowStatusWaiting, false},
		{"nothing to decide", creator, domain.RoleOwner, domain.WorkflowStatusRunning, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, workflowDecider(tc.status, tc.actor, creator, tc.role))
		})
	}
}
