package service

import (
	"errors"
	"fmt"
	"testing"

	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

// Each refusal of an approved command or temporary machine says what happened
// and what to do next; a transient error has no message, so the claim stays.
func TestCommandApprovalFailureNamesEachRefusal(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"not a member", repository.ErrConversationNotFound, "You no longer have access to this workspace"},
		{"guest", repository.ErrCommandRunGuest, "Guests cannot run commands; ask a workspace member to run it, or ask an admin to change your role"},
		{"role without Dev Machines", repository.ErrCommandRunForbidden, "Your role cannot use Dev Machines; ask a workspace member to run it, or ask an admin to change your role"},
		{"wrapped guest", fmt.Errorf("create: %w", repository.ErrCommandRunGuest), "Guests cannot run commands; ask a workspace member to run it, or ask an admin to change your role"},
		{"machine", repository.ErrCommandMachineUnavailable, upperFirst(notMachineOwner)},
		{"transient", errors.New("connection reset"), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, commandApprovalFailure(tc.err))
		})
	}
}
