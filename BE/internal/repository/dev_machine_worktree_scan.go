package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// ErrWorktreeUnavailable means the scan's task or machine cannot be read: the
// task has no ready checkout on a running machine, or a machine-only list scan
// names a machine the requester does not own.
var ErrWorktreeUnavailable = errors.New("the task's worktree is not available on a running machine")

// ErrWorktreeScanBusy means the requester already has as many unfinished scans
// as one machine should run at once.
var ErrWorktreeScanBusy = errors.New("too many worktree scans are already running on this machine")

const (
	worktreeScanKeyPrefix = "worktree-scan:"
	// A scan holds one exec on the machine. Four let one conversation read a
	// machine while its agent works without flooding it.
	worktreeScanConcurrencyLimit = 4
)

// worktreeScanColumns joins the machine and, for a checkout scan, the task
// checkout whose branch and path the result and its header describe.
const worktreeScanColumns = `s.id,s.workspace_id,s.machine_id,s.checkout_id,s.task_id,s.requested_by,s.request,COALESCE(s.result,'null'::jsonb) AS result,s.error,s.created_at,s.completed_at,
 m.name AS machine_name,COALESCE(c.repository_full_name,'') AS repository,
 COALESCE(c.working_branch,'') AS working_branch,COALESCE(c.base_branch,'') AS base_branch,
 COALESCE(c.workspace_path,'') AS workspace_path`

const worktreeScanJoin = `FROM dev_machine_worktree_scans s
 JOIN dev_machines m ON m.id=s.machine_id
 LEFT JOIN dev_machine_checkouts c ON c.id=s.checkout_id`

// CreateWorktreeScan validates and queues one read-only scan. The scan row and
// its operation share an ID, so the manager writes the result to the row it
// leased. A task scan is allowed while the task's agent runs: nothing here
// changes the checkout or the machine.
func (r *DevMachineRepository) CreateWorktreeScan(ctx context.Context, ws, user uuid.UUID, target domain.WorktreeScanTarget, req domain.WorktreeScanRequest) (*domain.WorktreeScan, error) {
	if err := ValidateWorktreeScanRequest(req); err != nil {
		return nil, err
	}
	if (target.TaskID == nil) == (target.MachineID == nil) {
		return nil, ErrConversationInvalid
	}
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	scan := &domain.WorktreeScan{ID: uuid.New(), WorkspaceID: ws, RequestedBy: user}
	var generation int64
	if target.TaskID != nil {
		var task struct {
			CreatorID  uuid.UUID  `db:"creator_id"`
			MachineID  *uuid.UUID `db:"machine_id"`
			CheckoutID uuid.UUID  `db:"checkout_id"`
		}
		err = tx.GetContext(ctx, &task, `SELECT creator_id,machine_id,checkout_id FROM agent_tasks
			WHERE id=$1 AND workspace_id=$2`, *target.TaskID, ws)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrConversationNotFound
		}
		if err != nil {
			return nil, err
		}
		if task.CreatorID != user {
			return nil, ErrConversationNotFound
		}
		if task.MachineID == nil {
			return nil, ErrWorktreeUnavailable
		}
		var checkout struct {
			ID                 uuid.UUID `db:"id"`
			RepositoryFullName string    `db:"repository_full_name"`
			WorkingBranch      string    `db:"working_branch"`
			BaseBranch         string    `db:"base_branch"`
			WorkspacePath      string    `db:"workspace_path"`
			MachineName        string    `db:"machine_name"`
			Generation         int64     `db:"generation"`
		}
		err = tx.GetContext(ctx, &checkout, `SELECT c.id,c.repository_full_name,c.working_branch,c.base_branch,c.workspace_path,
			 m.name AS machine_name,m.generation
			FROM dev_machine_checkouts c
			JOIN dev_machines m ON m.id=c.machine_id AND m.workspace_id=c.workspace_id
			WHERE c.id=$1 AND c.workspace_id=$2 AND c.machine_id=$3 AND c.status='ready'
			  AND m.status='running' AND m.delete_requested_at IS NULL`,
			task.CheckoutID, ws, *task.MachineID)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrWorktreeUnavailable
		}
		if err != nil {
			return nil, err
		}
		scan.MachineID = *task.MachineID
		scan.CheckoutID = &checkout.ID
		scan.TaskID = target.TaskID
		scan.MachineName = checkout.MachineName
		scan.Repository = checkout.RepositoryFullName
		scan.WorkingBranch = checkout.WorkingBranch
		scan.BaseBranch = checkout.BaseBranch
		scan.WorkspacePath = checkout.WorkspacePath
		generation = checkout.Generation
	} else {
		if req.Kind != domain.WorktreeScanList {
			return nil, ErrConversationInvalid
		}
		var machine struct {
			ID         uuid.UUID `db:"id"`
			Name       string    `db:"name"`
			Generation int64     `db:"generation"`
		}
		err = tx.GetContext(ctx, &machine, `SELECT id,name,generation FROM dev_machines
			WHERE id=$1 AND workspace_id=$2 AND created_by_user_id=$3
			  AND status='running' AND delete_requested_at IS NULL
			FOR SHARE`, *target.MachineID, ws, user)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrWorktreeUnavailable
		}
		if err != nil {
			return nil, err
		}
		scan.MachineID = machine.ID
		scan.MachineName = machine.Name
		generation = machine.Generation
	}
	// The count and the insert are one decision: without this lock two requests
	// for the same machine and requester could both read a free slot.
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1::text || ':' || $2::text))`,
		scan.MachineID, user); err != nil {
		return nil, err
	}
	var pending int
	if err = tx.GetContext(ctx, &pending, `SELECT COUNT(*) FROM dev_machine_worktree_scans
		WHERE machine_id=$1 AND requested_by=$2 AND completed_at IS NULL
		  AND created_at > now() - interval '2 minutes'`, scan.MachineID, user); err != nil {
		return nil, err
	}
	if pending >= worktreeScanConcurrencyLimit {
		return nil, ErrWorktreeScanBusy
	}
	request, err := json.Marshal(req)
	if err != nil {
		return nil, ErrConversationInvalid
	}
	scan.Request = request
	// A scan result is a transcript detail, not history: keep an hour of them.
	if _, err = tx.ExecContext(ctx, `DELETE FROM dev_machine_worktree_scans
		WHERE workspace_id=$1 AND created_at < now() - interval '1 hour'`, ws); err != nil {
		return nil, err
	}
	if err = tx.GetContext(ctx, scan, `INSERT INTO dev_machine_worktree_scans
		(id,workspace_id,machine_id,checkout_id,task_id,requested_by,request)
		VALUES($1,$2,$3,$4,$5,$6,$7::jsonb)
		RETURNING created_at`, scan.ID, ws, scan.MachineID, scan.CheckoutID, scan.TaskID, user, string(request)); err != nil {
		return nil, err
	}
	operation := &domain.DevMachineOperation{
		ID: scan.ID, WorkspaceID: ws, MachineID: scan.MachineID, Action: domain.DevMachineOpScanWorktree,
		Status: domain.DevMachineOpStatusPending, Generation: generation,
		IdempotencyKey: worktreeScanKeyPrefix + scan.ID.String(), RequestedByUserID: &user,
		MaxAttempts: 1, CheckoutID: scan.CheckoutID, AgentTaskID: scan.TaskID,
	}
	if err = enqueueOperation(ctx, tx, operation); err != nil {
		return nil, err
	}
	return scan, tx.Commit()
}

// GetWorktreeScan returns one scan with its joined header, or nil when the
// workspace has no such scan.
func (r *DevMachineRepository) GetWorktreeScan(ctx context.Context, ws, id uuid.UUID) (*domain.WorktreeScan, error) {
	scan := &domain.WorktreeScan{}
	err := r.db.GetContext(ctx, scan, `SELECT `+worktreeScanColumns+` `+worktreeScanJoin+`
		WHERE s.workspace_id=$1 AND s.id=$2`, ws, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	normalizeWorktreeScanResult(scan)
	return scan, nil
}

// WorktreeScanForOperation returns the scan the manager leased. Its checkout
// path, branch and base branch tell the runtime what to read.
func (r *DevMachineRepository) WorktreeScanForOperation(ctx context.Context, opID uuid.UUID) (*domain.WorktreeScan, error) {
	scan := &domain.WorktreeScan{}
	err := r.db.GetContext(ctx, scan, `SELECT `+worktreeScanColumns+` `+worktreeScanJoin+`
		WHERE s.id=$1 AND EXISTS(SELECT 1 FROM dev_machine_operations o
			WHERE o.id=s.id AND o.action='scan_worktree')`, opID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	normalizeWorktreeScanResult(scan)
	return scan, nil
}

// normalizeWorktreeScanResult keeps a missing JSON result as nil instead of the
// JSON null the COALESCE above returns for a nullable column.
func normalizeWorktreeScanResult(scan *domain.WorktreeScan) {
	if len(scan.Result) > 0 && string(scan.Result) == "null" {
		scan.Result = nil
	}
}

// CompleteWorktreeScan records a finished scan once. A later write for the same
// scan does not replace its result.
func (r *DevMachineRepository) CompleteWorktreeScan(ctx context.Context, id uuid.UUID, result json.RawMessage, errMsg *string) error {
	if len(result) > 65536 {
		return errors.New("worktree scan result is too large")
	}
	var resultValue any
	if len(result) > 0 {
		resultValue = string(result)
	}
	_, err := r.db.ExecContext(ctx, `UPDATE dev_machine_worktree_scans
		SET result=$2::jsonb,error=$3,completed_at=now()
		WHERE id=$1 AND completed_at IS NULL`, id, resultValue, errMsg)
	return err
}

// ValidateWorktreeScanRequest is the one contract for a scan request: services
// and test doubles call it so a fake refuses what production refuses.
func ValidateWorktreeScanRequest(req domain.WorktreeScanRequest) error {
	if req.Limit < 0 {
		return ErrConversationInvalid
	}
	switch req.Kind {
	case domain.WorktreeScanGrep:
		if !validWorktreeScanText(req.Pattern, 1, domain.WorktreeScanLineRunes) ||
			!validWorktreeScanText(req.Glob, 0, domain.WorktreeScanLineRunes) ||
			req.Limit > domain.WorktreeScanMaxMatches {
			return ErrConversationInvalid
		}
	case domain.WorktreeScanGlob:
		if !validWorktreeScanText(req.Glob, 0, domain.WorktreeScanLineRunes) ||
			req.Limit > domain.WorktreeScanMaxFiles {
			return ErrConversationInvalid
		}
	case domain.WorktreeScanRead:
		if !validWorktreeScanPath(req.Path) || req.StartLine < 1 || req.EndLine < req.StartLine ||
			req.Limit > domain.WorktreeScanReadLines {
			return ErrConversationInvalid
		}
	case domain.WorktreeScanStatus:
		if req.Limit > domain.WorktreeScanMaxChanged {
			return ErrConversationInvalid
		}
	case domain.WorktreeScanList:
		if req.Limit > domain.WorktreeScanMaxList {
			return ErrConversationInvalid
		}
	default:
		return ErrConversationInvalid
	}
	return nil
}

func validWorktreeScanText(value string, minimum, maximum int) bool {
	if strings.ContainsRune(value, 0) {
		return false
	}
	length := utf8.RuneCountInString(value)
	return length >= minimum && length <= maximum
}

func validWorktreeScanPath(path string) bool {
	if path == "" || len(path) > 500 || strings.HasPrefix(path, "/") || strings.ContainsRune(path, 0) {
		return false
	}
	for _, part := range strings.Split(path, "/") {
		if part == ".." {
			return false
		}
	}
	return true
}
