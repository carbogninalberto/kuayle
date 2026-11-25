package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/config"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/pkg/audit"
	log "github.com/sirupsen/logrus"
)

// Restart-only settings (KUA-311). A sysadmin proposes values for the
// variables the API reads once, at startup; a dry run (Validate) checks them
// against the registry's rules and the whole configuration; "Apply and
// restart" (Apply) stores them as overrides, records a run, and makes the API
// exit so its supervisor (the Compose restart policy) starts it again.
//
// On every start ResolveStartup merges the stored overrides over the
// environment. A start that follows an apply must prove itself: it reports
// the run healthy within RestartHealthTimeout (WatchRestartHealth), or the
// previous overrides are restored and the API exits to start again with them.
// A start that crashes before it can report is counted in the run; the third
// such start, or one that comes too late, rolls back too. The database holds
// everything, so no other service, and no file, takes part.

var (
	// ErrInstanceRestartUnsupported means nothing starts the API again after
	// it exits (RESTART_SUPERVISED is not set), so it must not be stopped.
	ErrInstanceRestartUnsupported = errors.New("the API is not run by a supervisor that restarts it")
	// ErrInstanceRestartNoChanges means every proposed value is already in effect.
	ErrInstanceRestartNoChanges = errors.New("no setting would change")
	// ErrInstanceRestartActive means an earlier run has not finished.
	ErrInstanceRestartActive = domain.ErrRestartRunActive
)

const (
	// RestartHealthTimeout is how long a restarted API has to report healthy.
	RestartHealthTimeout = 90 * time.Second
	// restartMaxBoots is how many starts with the new values a run may
	// count before the next one rolls it back.
	restartMaxBoots = 3
	// restartBootWindow is how long after its first start a run may still be
	// waiting before a start rolls it back.
	restartBootWindow = 10 * time.Minute
	// restartExitDelay lets the response of the apply request leave first.
	restartExitDelay = time.Second
	// maxRestartChanges bounds one request.
	maxRestartChanges = 32
)

// InstanceRestartStore keeps the overrides and the runs.
type InstanceRestartStore interface {
	ListOverrides(ctx context.Context) ([]domain.InstanceConfigOverride, error)
	ActiveRestartRun(ctx context.Context) (*domain.InstanceRestartRun, error)
	LatestRestartRun(ctx context.Context) (*domain.InstanceRestartRun, error)
	BeginRestartRun(ctx context.Context, run domain.InstanceRestartRun, set map[string]string, remove []string) (*domain.InstanceRestartRun, error)
	MarkRestartBoot(ctx context.Context, id uuid.UUID) (*domain.InstanceRestartRun, error)
	ConfirmRestartRun(ctx context.Context, id uuid.UUID) (bool, error)
	RollbackRestartRun(ctx context.Context, id uuid.UUID, failure string) (bool, error)
}

// StartupResult is the configuration an API process runs with.
type StartupResult struct {
	// Base is the environment alone; Config is Base with Applied laid over it.
	Base, Config *config.Config
	// Applied are the restart-only overrides in effect.
	Applied map[string]string
	// Run is the apply this process must confirm, or nil.
	Run *domain.InstanceRestartRun
	// Notice tells the page that the stored settings were not used.
	Notice string
}

// ResolveStartup chooses the configuration of a starting process. confirm is
// true for the API server, which owns the state of a run; the worker only
// reads. It never fails: when the store cannot be read, or the stored values
// do not make a valid configuration, the environment alone is used.
func ResolveStartup(ctx context.Context, store InstanceRestartStore, base *config.Config, confirm bool, now func() time.Time, logger log.FieldLogger) *StartupResult {
	result := &StartupResult{Base: base, Config: base, Applied: map[string]string{}}
	if store == nil {
		return result
	}
	overrides, err := restartOverrides(ctx, store)
	if err != nil {
		logger.WithError(err).Error("restart-only settings could not be read; starting with the environment alone")
		return result
	}
	var run *domain.InstanceRestartRun
	if confirm {
		if run, err = store.ActiveRestartRun(ctx); err != nil {
			logger.WithError(err).Error("the pending apply and restart could not be read; starting with the environment alone")
			return result
		}
	}
	rollback := func(reason string) {
		result.Notice = "The previous settings were restored: " + reason
		rolled, rollbackErr := store.RollbackRestartRun(ctx, run.ID, reason)
		if rollbackErr != nil {
			logger.WithError(rollbackErr).Error("the apply and restart could not be rolled back")
		} else if rolled {
			auditRestart("system.config_restart_rolled_back", run, reason)
		}
		run = nil
		if overrides, err = restartOverrides(ctx, store); err != nil {
			overrides = map[string]string{}
		}
	}
	if run != nil {
		switch {
		case run.Status == domain.RestartRunApplying && run.BootAttempts >= restartMaxBoots:
			rollback("the API did not start successfully with the new settings")
		case run.Status == domain.RestartRunApplying && run.AppliedAt != nil && now().Sub(*run.AppliedAt) > restartBootWindow:
			rollback("the API did not report healthy in time with the new settings")
		default:
			marked, markErr := store.MarkRestartBoot(ctx, run.ID)
			if markErr != nil {
				logger.WithError(markErr).Error("the start of the apply and restart could not be recorded; starting with the environment alone")
				return result
			}
			if run = marked; run == nil {
				overrides, _ = restartOverrides(ctx, store)
			}
		}
	}
	if len(overrides) > 0 {
		effective, err := base.WithOverrides(overrides)
		if err != nil && run != nil {
			rollback("the new settings do not make a valid configuration: " + err.Error())
			if len(overrides) > 0 {
				effective, err = base.WithOverrides(overrides)
			} else {
				effective, err = base, nil
			}
		}
		if err != nil {
			logger.WithError(err).Error("the stored restart-only settings do not make a valid configuration; starting with the environment alone")
			result.Notice = "The stored settings were not used: " + err.Error()
			effective, overrides = base, map[string]string{}
		}
		result.Config = effective
	}
	result.Applied, result.Run = overrides, run
	return result
}

// restartOverrides reads the stored overrides of restart-editable variables.
// Rows of other variables (the runtime ones) and of names a release no longer
// lists are not its concern.
func restartOverrides(ctx context.Context, store InstanceRestartStore) (map[string]string, error) {
	rows, err := store.ListOverrides(ctx)
	if err != nil {
		return nil, err
	}
	overrides := map[string]string{}
	for _, row := range rows {
		if variable, ok := config.Lookup(row.Name); ok && variable.RestartEditable() {
			overrides[row.Name] = row.Value
		}
	}
	return overrides, nil
}

// WatchRestartHealth confirms run once probe succeeds, or rolls it back when
// it has not within timeout, then calls exit so the supervisor starts the API
// again with the previous values. It returns when the run is decided or ctx
// ends.
func WatchRestartHealth(ctx context.Context, store InstanceRestartStore, run *domain.InstanceRestartRun, probe func(context.Context) error,
	timeout, interval time.Duration, exit func(), logger log.FieldLogger) {
	if run == nil {
		return
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var lastErr error
	for {
		probeCtx, cancel := context.WithTimeout(ctx, interval+time.Second)
		lastErr = probe(probeCtx)
		cancel()
		if lastErr == nil {
			confirmed, err := store.ConfirmRestartRun(ctx, run.ID)
			switch {
			case err != nil:
				logger.WithError(err).Error("the apply and restart could not be confirmed")
			case confirmed:
				auditRestart("system.config_restart_confirmed", run, "")
				logger.WithField("run_id", run.ID).Info("restart-only settings applied")
			}
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-deadline.C:
			reason := fmt.Sprintf("the API was not healthy within %s of restarting with the new settings", timeout)
			logger.WithError(lastErr).WithField("run_id", run.ID).Error("rolling back restart-only settings: " + reason)
			rolled, err := store.RollbackRestartRun(ctx, run.ID, reason)
			if err != nil {
				logger.WithError(err).Error("the apply and restart could not be rolled back")
				return
			}
			if rolled {
				auditRestart("system.config_restart_rolled_back", run, reason)
				exit()
			}
			return
		case <-ticker.C:
		}
	}
}

// auditRestart logs the outcome of a run. Restart-editable variables are never
// secrets, and a value that carries credentials is withheld anyway.
func auditRestart(action string, run *domain.InstanceRestartRun, failure string) {
	actor := uuid.Nil
	if run.RequestedBy != nil {
		actor = *run.RequestedBy
	}
	fields := map[string]interface{}{"run_id": run.ID, "changes": auditChanges(run.Changes), "requested_at": run.RequestedAt}
	if failure != "" {
		fields["reason"] = failure
	}
	audit.Log(action, actor, fields)
}

func auditChanges(changes []domain.InstanceRestartChange) []map[string]string {
	out := make([]map[string]string, 0, len(changes))
	for _, change := range changes {
		out = append(out, map[string]string{"variable": change.Name, "from": auditValue(change.From), "to": auditValue(change.To)})
	}
	return out
}

func auditValue(value string) string {
	if config.CarriesCredentials(value) {
		return "(withheld)"
	}
	return value
}

// RestartIssue is a proposed value that cannot be applied. An empty Name is a
// problem of the configuration as a whole.
type RestartIssue struct {
	Name    string
	Message string
}

// RestartPlan is what a set of proposed values would do.
type RestartPlan struct {
	// Changes lists each variable that would change, with effective values.
	Changes []domain.InstanceRestartChange
	Issues  []RestartIssue

	set      map[string]string
	remove   []string
	previous map[string]*string
}

// Valid reports whether the proposal can be applied.
func (p *RestartPlan) Valid() bool { return len(p.Issues) == 0 }

// RestartInvalidError carries the issues of a proposal that cannot be applied.
type RestartInvalidError struct{ Issues []RestartIssue }

func (e *RestartInvalidError) Error() string {
	if len(e.Issues) == 0 {
		return "invalid configuration"
	}
	return e.Issues[0].Message
}

// RestartStatus is what the page shows about restart-only settings.
type RestartStatus struct {
	// Supported is false when nothing would start the API again.
	Supported bool
	// Run is the latest apply, or nil.
	Run *domain.InstanceRestartRun
	// Notice explains that stored settings were not used by this process.
	Notice string
}

// InstanceRestart is the apply-and-restart half of the instance configuration
// service.
type InstanceRestart struct {
	store      InstanceRestartStore
	startup    *StartupResult
	supervised bool
	exit       func()
	exitDelay  time.Duration
}

// NewInstanceRestart wires restart-only settings. startup is what
// ResolveStartup returned for this process, and exit makes the API stop so
// that its supervisor starts it again.
func NewInstanceRestart(store InstanceRestartStore, startup *StartupResult, supervised bool, exit func()) *InstanceRestart {
	return &InstanceRestart{store: store, startup: startup, supervised: supervised, exit: exit, exitDelay: restartExitDelay}
}

// Status reports whether applying is possible and the latest run.
func (r *InstanceRestart) Status(ctx context.Context) (RestartStatus, error) {
	run, err := r.store.LatestRestartRun(ctx)
	return RestartStatus{Supported: r.supervised, Run: run, Notice: r.startup.Notice}, err
}

// Plan is the dry run: it checks the proposed values and changes nothing. A
// nil value returns the variable to its environment value.
func (r *InstanceRestart) Plan(ctx context.Context, proposed map[string]*string) (*RestartPlan, error) {
	plan := &RestartPlan{set: map[string]string{}, previous: map[string]*string{}}
	if len(proposed) > maxRestartChanges {
		plan.Issues = append(plan.Issues, RestartIssue{Message: fmt.Sprintf("Change at most %d settings at once", maxRestartChanges)})
		return plan, nil
	}
	stored, err := restartOverrides(ctx, r.store)
	if err != nil {
		return nil, err
	}
	desired := map[string]string{}
	for name, value := range stored {
		desired[name] = value
	}
	baseStates, effectiveStates := describeByName(r.startup.Base), describeByName(r.startup.Config)

	names := make([]string, 0, len(proposed))
	for name := range proposed {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		variable, ok := config.Lookup(name)
		switch {
		case !ok:
			plan.Issues = append(plan.Issues, RestartIssue{Name: name, Message: name + " is not a setting of Kuayle"})
			continue
		case variable.Runtime != nil:
			plan.Issues = append(plan.Issues, RestartIssue{Name: name, Message: name + " changes without a restart; edit it directly"})
			continue
		case !variable.RestartEditable():
			plan.Issues = append(plan.Issues, RestartIssue{Name: name, Message: name + " cannot be changed from Settings; set it in .env"})
			continue
		}
		from := effectiveStates[name].Value
		if value := proposed[name]; value == nil {
			if _, has := stored[name]; !has {
				continue
			}
			delete(desired, name)
			plan.remove = append(plan.remove, name)
			plan.previous[name] = stringPointer(stored[name])
			plan.Changes = append(plan.Changes, domain.InstanceRestartChange{Name: name, From: from, To: baseStates[name].Value})
		} else {
			normalized, err := variable.NormalizeRestart(*value)
			if err != nil {
				plan.Issues = append(plan.Issues, RestartIssue{Name: name, Message: err.Error()})
				continue
			}
			if current, has := stored[name]; has && current == normalized || !has && normalized == from {
				continue
			}
			desired[name] = normalized
			plan.set[name] = normalized
			if current, has := stored[name]; has {
				plan.previous[name] = stringPointer(current)
			} else {
				plan.previous[name] = nil
			}
			plan.Changes = append(plan.Changes, domain.InstanceRestartChange{Name: name, From: from, To: normalized})
		}
	}
	if len(plan.Issues) == 0 && len(plan.Changes) > 0 {
		if _, err := r.startup.Base.WithOverrides(desired); err != nil {
			plan.Issues = append(plan.Issues, RestartIssue{Message: err.Error()})
		}
	}
	return plan, nil
}

// Apply stores a valid proposal, records the run and, shortly after returning,
// makes the API exit so that its supervisor starts it with the new values.
func (r *InstanceRestart) Apply(ctx context.Context, actor uuid.UUID, proposed map[string]*string) (*domain.InstanceRestartRun, error) {
	if !r.supervised {
		return nil, ErrInstanceRestartUnsupported
	}
	plan, err := r.Plan(ctx, proposed)
	if err != nil {
		return nil, err
	}
	if !plan.Valid() {
		return nil, &RestartInvalidError{Issues: plan.Issues}
	}
	if len(plan.Changes) == 0 {
		return nil, ErrInstanceRestartNoChanges
	}
	run, err := r.store.BeginRestartRun(ctx, domain.InstanceRestartRun{RequestedBy: &actor, Changes: plan.Changes, Previous: plan.previous}, plan.set, plan.remove)
	if err != nil {
		return nil, err
	}
	auditRestart("system.config_restart_requested", run, "")
	time.AfterFunc(r.exitDelay, r.exit)
	return run, nil
}

func describeByName(cfg *config.Config) map[string]config.VariableState {
	states := config.Describe(cfg, func(string) (string, bool) { return "", false })
	byName := make(map[string]config.VariableState, len(states))
	for _, state := range states {
		byName[state.Name] = state
	}
	return byName
}

func stringPointer(value string) *string { return &value }
