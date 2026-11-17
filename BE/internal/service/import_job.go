package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/importer"
	"github.com/kuayle/kuayle-backend/internal/importer/jira"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/pkg/crypto"
)

var (
	ErrImportNotFound       = repository.ErrImportJobNotFound
	ErrImportNotEditable    = repository.ErrImportNotEditable
	ErrPreviewRequired      = errors.New("preview this import before starting it")
	ErrInvalidImportMapping = errors.New("the import mapping is invalid")
	ErrImportCancelled      = errors.New("import cancelled")
	errImporterGone         = errors.New("the account that created this import no longer exists")
)

// ImportValidationError is a user-facing validation message returned by the
// API before an import is started.
type ImportValidationError struct{ Message string }

func (e *ImportValidationError) Error() string { return e.Message }

type ImportCreateInput struct {
	Source      string
	Config      json.RawMessage
	Credentials json.RawMessage
}

// SourceFactory builds a tracker adapter from the stored config and the
// decrypted, job-lifetime credentials. The Jira factory is wired in main.go;
// tests inject a fake source.
type SourceFactory func(ctx context.Context, config, credentials json.RawMessage) (importer.Source, error)

// ImportService owns the import lifecycle: create, preview, map, start,
// cancel and the background worker that performs the writes.
type ImportService struct {
	repo       repository.ImportRepo
	store      importStore
	newSource  SourceFactory
	key        []byte
	limitBytes int64
	interval   time.Duration
	now        func() time.Time
	sleep      func(context.Context, time.Duration) error
	logger     log.FieldLogger
}

func NewImportService(repo repository.ImportRepo, store importStore, newSource SourceFactory, key []byte, attachmentLimit int64) *ImportService {
	if attachmentLimit <= 0 {
		attachmentLimit = domain.MaxUploadSize
	}
	return &ImportService{
		repo: repo, store: store, newSource: newSource, key: key, limitBytes: attachmentLimit,
		interval: 2 * time.Second, now: time.Now, sleep: importSleep, logger: log.StandardLogger(),
	}
}

func importSleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (s *ImportService) Create(ctx context.Context, workspaceID, creator uuid.UUID, input ImportCreateInput) (*domain.ImportJob, error) {
	if input.Source != domain.ImportSourceJira {
		return nil, &ImportValidationError{Message: "unsupported import source"}
	}
	if len(input.Credentials) == 0 {
		return nil, &ImportValidationError{Message: "credentials are required"}
	}
	var creds jiraImportCredentials
	if err := json.Unmarshal(input.Credentials, &creds); err != nil {
		return nil, &ImportValidationError{Message: "invalid credentials"}
	}
	// source_site is part of the idempotency key and must be derived from the
	// credentials' base URL, never from the client: Jira issue ids are only
	// unique within one site.
	site, err := jira.SiteFromBaseURL(creds.BaseURL)
	if err != nil {
		return nil, &ImportValidationError{Message: "invalid Jira base URL"}
	}
	encrypted, err := crypto.Encrypt(string(input.Credentials), s.key)
	if err != nil {
		return nil, err
	}
	job := &domain.ImportJob{
		WorkspaceID:    workspaceID,
		CreatedBy:      &creator,
		Source:         input.Source,
		SourceSite:     site,
		Config:         input.Config,
		Mapping:        json.RawMessage(`{}`),
		CredentialsEnc: &encrypted,
	}
	if err := s.repo.Create(ctx, job); err != nil {
		return nil, err
	}
	return job, nil
}

func (s *ImportService) Get(ctx context.Context, workspaceID, id uuid.UUID) (*domain.ImportJob, error) {
	return s.repo.Get(ctx, workspaceID, id)
}

func (s *ImportService) List(ctx context.Context, workspaceID uuid.UUID, limit, offset int) ([]domain.ImportJob, error) {
	return s.repo.List(ctx, workspaceID, limit, offset)
}

func (s *ImportService) ListItems(ctx context.Context, workspaceID, id uuid.UUID, limit, offset int) ([]domain.ImportJobItem, error) {
	return s.repo.ListItems(ctx, workspaceID, id, limit, offset)
}

func (s *ImportService) UpdateMapping(ctx context.Context, workspaceID, id uuid.UUID, raw json.RawMessage) error {
	mapping, err := decodeMapping(raw)
	if err != nil {
		return &ImportValidationError{Message: err.Error()}
	}
	if err := s.validateMapping(ctx, workspaceID, mapping); err != nil {
		return err
	}
	return s.repo.UpdateMapping(ctx, workspaceID, id, raw)
}

func (s *ImportService) validateMapping(ctx context.Context, workspaceID uuid.UUID, mapping importer.Mapping) error {
	teams, err := s.store.Teams(ctx, workspaceID)
	if err != nil {
		return err
	}
	teamSet := make(map[uuid.UUID]domain.Team, len(teams))
	for _, team := range teams {
		teamSet[team.ID] = team
	}
	statuses := make(map[uuid.UUID][]domain.TeamStatus)
	for teamID := range teamSet {
		list, err := s.store.Statuses(ctx, workspaceID, teamID)
		if err != nil {
			return err
		}
		statuses[teamID] = list
	}
	members, err := s.store.Members(ctx, workspaceID)
	if err != nil {
		return err
	}
	memberSet := make(map[uuid.UUID]bool, len(members))
	for _, member := range members {
		memberSet[member.UserID] = true
	}
	if err := mapping.Validate(importer.ValidationContext{Teams: teamSet, TeamStatuses: statuses, Members: memberSet}); err != nil {
		return &ImportValidationError{Message: err.Error()}
	}
	return nil
}

// Preview is a dry run: it reads from the source and writes only the preview
// JSON on the job. No Kuayle entity is created or updated.
func (s *ImportService) Preview(ctx context.Context, workspaceID, id uuid.UUID) (*importer.SourcePreview, error) {
	job, err := s.repo.Get(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	source, err := s.sourceFor(ctx, job)
	if err != nil {
		return nil, err
	}
	preview, err := source.Preview(ctx)
	if err != nil {
		return nil, fmt.Errorf("preview source: %w", err)
	}
	mapping, err := decodeMapping(job.Mapping)
	if err == nil {
		preview.UnmappedFields = mergeUnique(preview.UnmappedFields, mapping.UnmappedFor(preview))
	}
	raw, err := json.Marshal(preview)
	if err != nil {
		return nil, err
	}
	if err := s.repo.SetPreview(ctx, workspaceID, id, raw); err != nil {
		return nil, err
	}
	return preview, nil
}

// Start requires a completed preview and a valid mapping before queueing.
func (s *ImportService) Start(ctx context.Context, workspaceID, id uuid.UUID) error {
	job, err := s.repo.Get(ctx, workspaceID, id)
	if err != nil {
		return err
	}
	if job.Status != domain.ImportStatusPreviewed || len(job.Preview) == 0 {
		return ErrPreviewRequired
	}
	mapping, err := decodeMapping(job.Mapping)
	if err != nil {
		return &ImportValidationError{Message: err.Error()}
	}
	if err := s.validateMapping(ctx, workspaceID, mapping); err != nil {
		return err
	}
	var preview importer.SourcePreview
	if err := json.Unmarshal(job.Preview, &preview); err != nil {
		return err
	}
	return s.repo.Start(ctx, workspaceID, id, preview.Counts["issues"])
}

func (s *ImportService) Cancel(ctx context.Context, workspaceID, id uuid.UUID) error {
	return s.repo.RequestCancel(ctx, workspaceID, id)
}

// ImportOptions are the live targets the admin can map source values onto.
type ImportOptions struct {
	Teams    []domain.Team                     `json:"teams"`
	Statuses map[uuid.UUID][]domain.TeamStatus `json:"statuses"`
	Members  []importer.Member                 `json:"members"`
}

func (s *ImportService) Options(ctx context.Context, workspaceID uuid.UUID) (*ImportOptions, error) {
	teams, err := s.store.Teams(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	statuses := make(map[uuid.UUID][]domain.TeamStatus, len(teams))
	for _, team := range teams {
		list, err := s.store.Statuses(ctx, workspaceID, team.ID)
		if err != nil {
			return nil, err
		}
		statuses[team.ID] = list
	}
	members, err := s.store.Members(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	return &ImportOptions{Teams: teams, Statuses: statuses, Members: members}, nil
}

func (s *ImportService) sourceFor(ctx context.Context, job *domain.ImportJob) (importer.Source, error) {
	if job.CredentialsEnc == nil || *job.CredentialsEnc == "" {
		return nil, &ImportValidationError{Message: "this import's credentials have expired; create a new import"}
	}
	plain, err := crypto.Decrypt(*job.CredentialsEnc, s.key)
	if err != nil {
		return nil, errors.New("could not decrypt import credentials")
	}
	return s.newSource(ctx, job.Config, json.RawMessage(plain))
}

func decodeMapping(raw json.RawMessage) (importer.Mapping, error) {
	mapping := importer.NewMapping()
	if len(raw) == 0 {
		return mapping, nil
	}
	if err := json.Unmarshal(raw, &mapping); err != nil {
		return mapping, err
	}
	if mapping.Projects == nil {
		mapping.Projects = map[string]importer.ProjectTarget{}
	}
	if mapping.Statuses == nil {
		mapping.Statuses = map[string]importer.StatusTarget{}
	}
	if mapping.Users == nil {
		mapping.Users = map[string]importer.UserTarget{}
	}
	if mapping.Priorities == nil {
		mapping.Priorities = map[string]int{}
	}
	return mapping, nil
}

func mergeUnique(a, b []string) []string {
	seen := make(map[string]bool, len(a)+len(b))
	out := make([]string, 0, len(a)+len(b))
	for _, value := range append(append([]string{}, a...), b...) {
		if !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}

const (
	// staleAfter is how long a running job may go without a heartbeat before
	// recovery marks it failed.
	staleAfter = 10 * time.Minute
	// idleAfter is how long a draft or previewed job may sit untouched before
	// its credentials expire.
	idleAfter    = 24 * time.Hour
	recoverEvery = time.Minute
)

// recoverStale fails interrupted jobs and expires credentials on idle drafts.
// It runs at startup and once a minute from Run; keeping it separate makes the
// sweep testable without starting the polling loop.
func (s *ImportService) recoverStale(ctx context.Context) {
	if n, err := s.repo.FailStaleRunning(ctx, s.now().Add(-staleAfter)); err != nil {
		s.logger.WithError(err).Warn("could not recover stale imports")
	} else if n > 0 {
		s.logger.WithField("count", n).Warn("marked interrupted imports as failed")
	}
	if n, err := s.repo.ExpireIdle(ctx, s.now().Add(-idleAfter)); err != nil {
		s.logger.WithError(err).Warn("could not expire idle imports")
	} else if n > 0 {
		s.logger.WithField("count", n).Warn("expired credentials on idle imports")
	}
}

// Run polls for queued imports until ctx ends. Like the backup worker, it
// recovers interrupted jobs first and never dies on a transient error. A job
// is stale only when its heartbeat is older than ten minutes; recovery runs
// once a minute as well as at startup.
func (s *ImportService) Run(ctx context.Context) {
	s.recoverStale(ctx)
	lastRecover := s.now()
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		for {
			job, err := s.repo.ClaimNext(ctx)
			if err != nil {
				s.logger.WithError(err).Warn("could not claim an import")
				break
			}
			if job == nil {
				break
			}
			s.process(ctx, job)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if s.now().Sub(lastRecover) >= recoverEvery {
			s.recoverStale(ctx)
			lastRecover = s.now()
		}
	}
}
