package service

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// ErrDemoMachineForbidden: public demo mode keeps Dev Machines for instance
// administrators, and this user is not one.
var ErrDemoMachineForbidden = errors.New("dev machines are limited to instance administrators in demo mode")

// demoMachineRefusal is ErrDemoMachineForbidden worded for a task, an
// approval card or an assistant tool failure.
const demoMachineRefusal = "Dev Machines are limited to instance administrators on this demo instance"

// MachineAccess is the public demo rule for Dev Machines, shared by every
// service that starts work on one: creating, retrying, reviewing, checking,
// previewing or messaging a coding task, running a custom tool, and using a
// machine directly. The route middleware applies the same predicate to the
// Dev Machine routes; services apply it again because the assistant, its
// approvals, automations and background workers reach machines without those
// routes.
//
// A nil MachineAccess restricts nothing (demo mode is not configured).
type MachineAccess struct {
	allowed func(uuid.UUID) bool
}

// NewMachineAccess takes config.DemoDevMachineAllowed: true for everyone when
// demo mode is off, and only for SYSADMINS when it is on.
func NewMachineAccess(allowed func(uuid.UUID) bool) *MachineAccess {
	return &MachineAccess{allowed: allowed}
}

// Require refuses a user the demo rule keeps off Dev Machines.
func (a *MachineAccess) Require(user uuid.UUID) error {
	if a == nil || a.allowed == nil || a.allowed(user) {
		return nil
	}
	return ErrDemoMachineForbidden
}

// RequireActor is Require for the caller in ctx. A context without a caller
// is refused while the rule is active.
func (a *MachineAccess) RequireActor(ctx context.Context) error {
	access, _ := domain.AccessFromContext(ctx)
	return a.Require(access.UserID)
}
