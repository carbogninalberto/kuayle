// Package instanceconfigtest holds an in-memory stand-in for the instance
// configuration repository, for the service and handler tests of restart-only
// settings.
package instanceconfigtest

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// RestartStore mirrors the repository: overrides and runs, one active
// run at a time, a rollback that restores only the run's variables.
type RestartStore struct {
	mu        sync.Mutex
	Overrides map[string]string
	Runs      []*domain.InstanceRestartRun
	FailList  error
}

// NewRestartStore returns an empty store.
func NewRestartStore() *RestartStore {
	return &RestartStore{Overrides: map[string]string{}}
}

func (s *RestartStore) ListOverrides(context.Context) ([]domain.InstanceConfigOverride, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.FailList != nil {
		return nil, s.FailList
	}
	rows := []domain.InstanceConfigOverride{}
	for name, value := range s.Overrides {
		rows = append(rows, domain.InstanceConfigOverride{Name: name, Value: value, UpdatedAt: time.Now()})
	}
	return rows, nil
}

func (s *RestartStore) active() *domain.InstanceRestartRun {
	for _, run := range s.Runs {
		if run.Active() {
			return run
		}
	}
	return nil
}

func (s *RestartStore) ActiveRestartRun(context.Context) (*domain.InstanceRestartRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return copyRun(s.active()), nil
}

func (s *RestartStore) LatestRestartRun(context.Context) (*domain.InstanceRestartRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.Runs) == 0 {
		return nil, nil
	}
	return copyRun(s.Runs[len(s.Runs)-1]), nil
}

func (s *RestartStore) BeginRestartRun(_ context.Context, run domain.InstanceRestartRun, set map[string]string, remove []string) (*domain.InstanceRestartRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active() != nil {
		return nil, domain.ErrRestartRunActive
	}
	run.ID, run.Status, run.RequestedAt = uuid.New(), domain.RestartRunRestarting, time.Now()
	s.Runs = append(s.Runs, &run)
	for name, value := range set {
		s.Overrides[name] = value
	}
	for _, name := range remove {
		delete(s.Overrides, name)
	}
	return copyRun(&run), nil
}

func (s *RestartStore) find(id uuid.UUID) *domain.InstanceRestartRun {
	for _, run := range s.Runs {
		if run.ID == id {
			return run
		}
	}
	return nil
}

func (s *RestartStore) MarkRestartBoot(_ context.Context, id uuid.UUID) (*domain.InstanceRestartRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	run := s.find(id)
	if run == nil || !run.Active() {
		return nil, nil
	}
	run.Status, run.BootAttempts = domain.RestartRunApplying, run.BootAttempts+1
	if run.AppliedAt == nil {
		now := time.Now()
		run.AppliedAt = &now
	}
	return copyRun(run), nil
}

func (s *RestartStore) ConfirmRestartRun(_ context.Context, id uuid.UUID) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	run := s.find(id)
	if run == nil || run.Status != domain.RestartRunApplying {
		return false, nil
	}
	run.Status = domain.RestartRunConfirmed
	return true, nil
}

func (s *RestartStore) RollbackRestartRun(_ context.Context, id uuid.UUID, failure string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	run := s.find(id)
	if run == nil || !run.Active() {
		return false, nil
	}
	run.Status, run.Failure = domain.RestartRunRolledBack, failure
	for name, value := range run.Previous {
		if value == nil {
			delete(s.Overrides, name)
		} else {
			s.Overrides[name] = *value
		}
	}
	return true, nil
}

func copyRun(run *domain.InstanceRestartRun) *domain.InstanceRestartRun {
	if run == nil {
		return nil
	}
	copied := *run
	return &copied
}

// Status is the status of the latest run.
func (s *RestartStore) Status() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Runs[len(s.Runs)-1].Status
}
