package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
	cryptoutil "github.com/kuayle/kuayle-backend/pkg/crypto"
)

var (
	ErrDevMachinesDisabled     = errors.New("dev machines are disabled")
	ErrMachineNotFound         = errors.New("dev machine not found")
	ErrAgentRunNotFound        = errors.New("agent run not found")
	ErrEnvironmentNotFound     = errors.New("development environment not found")
	ErrTerminalSessionNotFound = errors.New("terminal session not found")
	ErrInvalidOperation        = errors.New("operation is not valid for the current machine state")
	ErrMachineQuota            = errors.New("dev machine quota exceeded")
	ErrProviderNotAllowed      = errors.New("agent provider is not allowed")
	ErrRepositoryNotAllowed    = errors.New("repository is not allowed")
	ErrServiceNotAvailable     = errors.New("machine service is not available")
	ErrCheckoutNotEligible     = errors.New("machine is not eligible for this issue checkout")
)

type DevMachineImages struct {
	IDE       string
	Browser   string
	Collector string
	Egress    string
}

type DevMachineService struct {
	store          repository.DevMachineStore
	agents         *agent.Registry
	enabled        bool
	domain         string
	frontendOrigin string
	encryptionKey  []byte
	ticketTTL      time.Duration
	images         DevMachineImages
}

func NewDevMachineService(store repository.DevMachineStore, agents *agent.Registry, enabled bool, domain string, encryptionKey []byte, ticketTTL time.Duration, images DevMachineImages, frontendURL ...string) *DevMachineService {
	frontendOrigin := ""
	if len(frontendURL) > 0 {
		frontendOrigin = normalizeOrigin(frontendURL[0])
	}
	return &DevMachineService{
		store: store, agents: agents, enabled: enabled, domain: strings.TrimSuffix(strings.ToLower(domain), "."),
		frontendOrigin: frontendOrigin, encryptionKey: encryptionKey, ticketTTL: ticketTTL, images: images,
	}
}

func (s *DevMachineService) Create(ctx context.Context, workspaceID, userID uuid.UUID, req dto.CreateDevMachineRequest) (*domain.DevMachine, *domain.DevMachineOperation, error) {
	if req.Repo != nil {
		if err := validateRepository(*req.Repo); err != nil {
			return nil, nil, err
		}
	}
	if (req.BaseBranch != "" && !validGitRef(req.BaseBranch)) || (req.WorkingBranch != "" && !validGitRef(req.WorkingBranch)) {
		return nil, nil, fmt.Errorf("invalid base or working branch")
	}
	req.Services.IDE = true
	policy, err := s.enabledPolicy(ctx, workspaceID)
	if err != nil {
		return nil, nil, err
	}
	workspaceCount, err := s.store.CountActiveMachines(ctx, workspaceID, nil)
	if err != nil {
		return nil, nil, err
	}
	userCount, err := s.store.CountActiveMachines(ctx, workspaceID, &userID)
	if err != nil {
		return nil, nil, err
	}
	if workspaceCount >= policy.MaxConcurrentMachines || userCount >= policy.MaxMachinesPerUser {
		return nil, nil, ErrMachineQuota
	}

	cpuMillis, memoryMB, diskGB, _, ok := domain.DevMachineSize(req.Size)
	if !ok {
		return nil, nil, fmt.Errorf("unsupported machine size %q", req.Size)
	}
	if diskGB > policy.MaxDiskGB {
		return nil, nil, fmt.Errorf("machine disk request exceeds workspace limit")
	}
	maxRuntime := policy.MaxRuntimeMinutes
	name := normalizeMachineName(req.Name)
	if name == "" {
		name, err = s.GenerateName(ctx, workspaceID, userID)
		if err != nil {
			return nil, nil, err
		}
	}
	if !validMachineName(name) {
		return nil, nil, fmt.Errorf("invalid machine name")
	}
	nameExists, err := s.store.MachineNameExistsForUser(ctx, workspaceID, userID, name)
	if err != nil {
		return nil, nil, err
	}
	if nameExists {
		return nil, nil, fmt.Errorf("machine name already exists")
	}

	issue, project, issueID, projectID, err := s.resolveRequestedContext(ctx, workspaceID, req.IssueID, req.ProjectID)
	if err != nil {
		return nil, nil, err
	}
	defaults, err := s.resolveDevelopmentSetting(ctx, workspaceID, issue, project)
	if err != nil {
		return nil, nil, err
	}

	var environmentID *uuid.UUID
	if req.EnvironmentID != nil {
		parsed, parseErr := uuid.Parse(*req.EnvironmentID)
		if parseErr != nil {
			return nil, nil, fmt.Errorf("invalid environment_id")
		}
		if err := s.validateReadyEnvironment(ctx, workspaceID, parsed); err != nil {
			return nil, nil, err
		}
		environmentID = &parsed
	} else if !req.EnvironmentBuilder && defaults.EnvironmentID != nil {
		if err := s.validateReadyEnvironment(ctx, workspaceID, *defaults.EnvironmentID); err != nil {
			return nil, nil, err
		}
		environmentID = defaults.EnvironmentID
	}

	var repositoryModel *domain.GitHubRepoModel
	var baseBranch, workingBranch string
	if req.Repo != nil {
		fullName := req.Repo.Owner + "/" + req.Repo.Name
		repositoryModel, err = s.store.GetLinkedRepositoryByFullName(ctx, workspaceID, fullName)
		if err != nil {
			return nil, nil, err
		}
		if repositoryModel == nil {
			return nil, nil, fmt.Errorf("invalid linked repository")
		}
	} else if defaults.GitHubRepoID != nil {
		repositoryModel, err = s.store.GetLinkedRepository(ctx, workspaceID, *defaults.GitHubRepoID)
		if err != nil {
			return nil, nil, err
		}
		if repositoryModel == nil {
			return nil, nil, fmt.Errorf("invalid linked repository")
		}
	}
	if repositoryModel != nil {
		if !repositoryAllowed(policy.AllowedRepositories, repositoryModel.FullName) {
			return nil, nil, fmt.Errorf("%w: %s", ErrRepositoryNotAllowed, repositoryModel.FullName)
		}
		baseBranch = repositoryModel.DefaultBranch
		if defaults.BaseBranch != nil && req.Repo == nil {
			baseBranch = *defaults.BaseBranch
		}
		if req.BaseBranch != "" {
			baseBranch = req.BaseBranch
		}
		workingBranch = req.WorkingBranch
		if workingBranch == "" {
			workingBranch = defaultWorkingBranch(name, issue)
		}
		if !validGitRef(baseBranch) || !validGitRef(workingBranch) {
			return nil, nil, fmt.Errorf("invalid base or working branch")
		}
	} else if req.BaseBranch != "" || req.WorkingBranch != "" {
		return nil, nil, fmt.Errorf("repository is required when branch is provided")
	}

	routingKey, err := randomHex(10)
	if err != nil {
		return nil, nil, err
	}
	machineID := uuid.New()
	servicesConfig, _ := json.Marshal(req.Services)
	now := time.Now().UTC()
	machine := &domain.DevMachine{
		ID: machineID, WorkspaceID: workspaceID, CreatedByUserID: &userID, RoutingKey: routingKey,
		Name: name, Status: domain.DevMachineStatusQueued, DesiredStatus: domain.DevMachineStatusRunning,
		Generation: 1, ProjectID: projectID, IssueID: issueID, BaseBranch: baseBranch, WorkingBranch: workingBranch,
		MachineSize: req.Size, CPUMillis: cpuMillis, MemoryMB: memoryMB, DiskGB: diskGB, PidsLimit: 512,
		MaxRuntimeMinutes: maxRuntime, ServicesConfig: servicesConfig, Labels: json.RawMessage(`{}`),
		ExpiresAt: now.Add(time.Duration(maxRuntime) * time.Minute), EnvironmentID: environmentID,
		KeepRunning: req.KeepRunning, EnvironmentBuilder: req.EnvironmentBuilder, LastActivityAt: &now,
	}
	if repositoryModel != nil {
		machine.RepositoryAffinityID = &repositoryModel.ID
		machine.RepoProvider = "github"
		machine.RepoOwner, machine.RepoName = splitRepositoryFullName(repositoryModel.FullName)
		machine.RepoURL = "https://github.com/" + repositoryModel.FullName
	}

	providers, err := s.buildProviders(machineID, policy, req.Agents)
	if err != nil {
		return nil, nil, err
	}
	services := s.buildServices(machine)
	if machine.EnvironmentID != nil {
		environment, _ := s.store.GetEnvironment(ctx, workspaceID, *machine.EnvironmentID)
		if environment != nil {
			environmentImageRef := readyEnvironmentImageRef(environment)
			for index := range services {
				if services[index].ServiceType == "ide" || services[index].ServiceType == "terminal" {
					services[index].ImageRef = environmentImageRef
				}
			}
		}
	}
	volumeName := "kuayle-workspace-" + routingKey
	volumes := []domain.DevMachineVolume{{
		ID: uuid.New(), MachineID: machineID, VolumeType: "workspace", RuntimeName: volumeName,
		MountPath: "/workspace", SizeLimitBytes: int64(diskGB) * 1024 * 1024 * 1024,
	}}
	envVars, err := s.buildEnvVars(machineID, req.EnvVars)
	if err != nil {
		return nil, nil, err
	}
	collectorToken, err := randomHex(32)
	if err != nil {
		return nil, nil, err
	}
	encryptedCollectorToken, err := cryptoutil.Encrypt(collectorToken, s.encryptionKey)
	if err != nil {
		return nil, nil, err
	}
	envVars = append(envVars, domain.DevMachineEnvVar{
		ID: uuid.New(), MachineID: machineID, TargetService: "collector", Name: "KUAYLE_MACHINE_TOKEN",
		EncryptedValue: encryptedCollectorToken, EncryptionKeyVersion: 1, IsSecret: true, ExpiresAt: &machine.ExpiresAt,
	})
	collectorTokenHash := sha256.Sum256([]byte(collectorToken))
	tokens := []domain.DevMachineToken{{
		ID: uuid.New(), MachineID: machineID, TokenHash: hex.EncodeToString(collectorTokenHash[:]),
		Scopes: json.RawMessage(`["events:write","logs:write","heartbeat:write"]`), ExpiresAt: machine.ExpiresAt,
	}}
	operation := &domain.DevMachineOperation{
		ID: uuid.New(), MachineID: machineID, WorkspaceID: workspaceID, Action: domain.DevMachineOpSpawn,
		Status: domain.DevMachineOpStatusPending, Generation: machine.Generation,
		IdempotencyKey: fmt.Sprintf("spawn:%d", machine.Generation), RequestedByUserID: &userID, MaxAttempts: 5,
	}
	if err := s.store.CreateBundle(ctx, machine, providers, services, volumes, envVars, tokens, operation); err != nil {
		return nil, nil, err
	}
	s.emit(ctx, machine, nil, &userID, "lifecycle", "machine.queued", map[string]any{"operation_id": operation.ID})
	return machine, operation, nil
}

func (s *DevMachineService) resolveRequestedContext(ctx context.Context, workspaceID uuid.UUID, rawIssueID, rawProjectID *string) (*domain.Issue, *domain.Project, *uuid.UUID, *uuid.UUID, error) {
	var issue *domain.Issue
	var project *domain.Project
	var issueID *uuid.UUID
	var projectID *uuid.UUID
	if rawIssueID != nil {
		parsed, err := uuid.Parse(*rawIssueID)
		if err != nil {
			return nil, nil, nil, nil, fmt.Errorf("invalid issue_id")
		}
		loaded, err := s.store.GetIssueDevelopmentContext(ctx, workspaceID, parsed)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		if loaded == nil {
			return nil, nil, nil, nil, fmt.Errorf("invalid issue_id for workspace")
		}
		issue = loaded
		issueID = &parsed
	}
	if rawProjectID != nil {
		parsed, err := uuid.Parse(*rawProjectID)
		if err != nil {
			return nil, nil, nil, nil, fmt.Errorf("invalid project_id")
		}
		loaded, err := s.store.GetProjectDevelopmentContext(ctx, workspaceID, parsed)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		if loaded == nil {
			return nil, nil, nil, nil, fmt.Errorf("invalid project_id for workspace")
		}
		project = loaded
		projectID = &parsed
	}
	if issue != nil {
		if issue.ProjectID != nil {
			if projectID != nil && *projectID != *issue.ProjectID {
				return nil, nil, nil, nil, fmt.Errorf("issue does not belong to project_id")
			}
			if projectID == nil {
				projectID = issue.ProjectID
			}
		} else if projectID != nil {
			return nil, nil, nil, nil, fmt.Errorf("issue does not belong to project_id")
		}
	}
	if project == nil && projectID != nil {
		loaded, err := s.store.GetProjectDevelopmentContext(ctx, workspaceID, *projectID)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		if loaded == nil {
			return nil, nil, nil, nil, fmt.Errorf("invalid project_id for workspace")
		}
		project = loaded
	}
	return issue, project, issueID, projectID, nil
}

func (s *DevMachineService) validateReadyEnvironment(ctx context.Context, workspaceID, environmentID uuid.UUID) error {
	environment, err := s.store.GetEnvironment(ctx, workspaceID, environmentID)
	if err != nil {
		return err
	}
	if environment == nil || environment.Status != "ready" {
		return fmt.Errorf("invalid development environment")
	}
	if !strings.HasPrefix(readyEnvironmentImageRef(environment), "sha256:") {
		return fmt.Errorf("invalid development environment")
	}
	return nil
}

func readyEnvironmentImageRef(environment *domain.DevMachineEnvironment) string {
	if environment == nil {
		return ""
	}
	if environment.ImageDigest != nil && strings.HasPrefix(strings.TrimSpace(*environment.ImageDigest), "sha256:") {
		return strings.TrimSpace(*environment.ImageDigest)
	}
	return strings.TrimSpace(environment.ImageRef)
}

func splitRepositoryFullName(fullName string) (string, string) {
	parts := strings.SplitN(fullName, "/", 2)
	if len(parts) != 2 {
		return "", fullName
	}
	return parts[0], parts[1]
}

func defaultWorkingBranch(machineName string, issue *domain.Issue) string {
	if issue != nil && issue.Identifier != "" {
		return "kuayle/" + strings.ToLower(issue.Identifier)
	}
	return "kuayle/" + machineName
}

func (s *DevMachineService) buildProviders(machineID uuid.UUID, policy *domain.DevMachineWorkspacePolicy, requested []dto.DevMachineAgentInput) ([]domain.DevMachineAgentProvider, error) {
	seen := make(map[string]bool)
	providers := make([]domain.DevMachineAgentProvider, 0, len(requested))
	for _, input := range requested {
		if seen[input.Provider] {
			continue
		}
		seen[input.Provider] = true
		provider, ok := s.agents.Get(input.Provider)
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrProviderNotAllowed, input.Provider)
		}
		metadata := provider.Metadata()
		if metadata.Custom && !policy.AllowCustomProviders {
			return nil, fmt.Errorf("%w: custom providers require workspace admin enablement", ErrProviderNotAllowed)
		}
		if !metadata.Custom && !providerAllowed(policy.AllowedProviders, input.Provider) {
			return nil, fmt.Errorf("%w: %s", ErrProviderNotAllowed, input.Provider)
		}
		config, err := json.Marshal(input.Config)
		if err != nil {
			return nil, fmt.Errorf("marshal provider config: %w", err)
		}
		if input.Config == nil {
			config = []byte(`{}`)
		}
		invocation, err := provider.BuildInvocation(agent.RunInput{
			Mode: agent.Mode(input.Mode), Prompt: "configuration validation", WorkspacePath: "/workspace", Config: config,
		})
		if err != nil {
			return nil, fmt.Errorf("invalid %s provider configuration: %w", input.Provider, err)
		}
		modes, _ := json.Marshal(metadata.SupportedModes)
		secrets, _ := json.Marshal(invocation.SecretNames)
		providers = append(providers, domain.DevMachineAgentProvider{
			ID: uuid.New(), MachineID: machineID, ProviderID: metadata.ID, DisplayName: metadata.DisplayName,
			ImageRef: invocation.Image, SupportedModes: modes, RequiredSecrets: secrets, Config: config,
			Enabled: true, IsCustom: metadata.Custom,
		})
	}
	return providers, nil
}

func (s *DevMachineService) buildServices(machine *domain.DevMachine) []domain.DevMachineService {
	base := "kuayle-" + machine.RoutingKey + "-"
	services := []domain.DevMachineService{
		newService(machine.ID, "collector", "collector", base+"collector", s.images.Collector, machine.RoutingKey+"-collector", 8091),
		newService(machine.ID, "egress", "egress", base+"egress", s.images.Egress, machine.RoutingKey+"-egress", 3128),
	}
	var config dto.DevMachineServicesInput
	_ = json.Unmarshal(machine.ServicesConfig, &config)
	if config.IDE {
		services = append(services, newService(machine.ID, "ide", "ide", base+"ide", s.images.IDE, machine.RoutingKey+"-ide", 8080))
		services = append(services, newService(machine.ID, "terminal", "terminal", base+"ide", s.images.IDE, machine.RoutingKey+"-ide", 7681))
	}
	if config.Browser {
		services = append(services, newService(machine.ID, "browser", "browser", base+"browser", s.images.Browser, machine.RoutingKey+"-browser", 3000))
	}
	return services
}

func newService(machineID uuid.UUID, serviceType, key, name, image, host string, port int) domain.DevMachineService {
	return domain.DevMachineService{
		ID: uuid.New(), MachineID: machineID, ServiceType: serviceType, ServiceKey: key,
		ContainerName: name, ImageRef: image, InternalHost: host, InternalPort: port,
		Status: "pending", HealthStatus: "unknown",
	}
}

func (s *DevMachineService) buildEnvVars(machineID uuid.UUID, inputs []dto.DevMachineEnvVarInput) ([]domain.DevMachineEnvVar, error) {
	envVars := make([]domain.DevMachineEnvVar, 0, len(inputs))
	for _, input := range inputs {
		if input.Name == "GITHUB_TOKEN" || strings.HasPrefix(input.Name, "KUAYLE_") {
			return nil, fmt.Errorf("invalid environment variable %s: name is reserved", input.Name)
		}
		if !validEnvName(input.Name) {
			return nil, fmt.Errorf("invalid environment variable name %q", input.Name)
		}
		if input.Provider != nil && input.TargetService != "agent" {
			return nil, fmt.Errorf("provider-scoped variables must target agent services")
		}
		encrypted, err := cryptoutil.Encrypt(input.Value, s.encryptionKey)
		if err != nil {
			return nil, fmt.Errorf("encrypt %s: %w", input.Name, err)
		}
		secret := true
		if input.Secret != nil {
			secret = *input.Secret
		}
		var expiresAt *time.Time
		if input.TTLMinutes != nil {
			expires := time.Now().UTC().Add(time.Duration(*input.TTLMinutes) * time.Minute)
			expiresAt = &expires
		}
		envVars = append(envVars, domain.DevMachineEnvVar{
			ID: uuid.New(), MachineID: machineID, ProviderID: input.Provider, TargetService: input.TargetService,
			Name: input.Name, EncryptedValue: encrypted, EncryptionKeyVersion: 1, IsSecret: secret, ExpiresAt: expiresAt,
		})
	}
	return envVars, nil
}

func (s *DevMachineService) Get(ctx context.Context, workspaceID, machineID uuid.UUID) (*domain.DevMachine, error) {
	machine, err := s.store.GetMachine(ctx, workspaceID, machineID)
	if err == nil && machine == nil {
		return nil, ErrMachineNotFound
	}
	return machine, err
}

func (s *DevMachineService) GetForUser(ctx context.Context, workspaceID, machineID, userID uuid.UUID) (*domain.DevMachine, error) {
	machine, err := s.store.GetMachineForUser(ctx, workspaceID, machineID, userID)
	if err == nil && machine == nil {
		return nil, ErrMachineNotFound
	}
	return machine, err
}

func (s *DevMachineService) List(ctx context.Context, workspaceID, userID uuid.UUID, params dto.DevMachineListParams) ([]domain.DevMachine, int, error) {
	params.Defaults()
	var issueID *uuid.UUID
	if params.IssueID != "" {
		parsed, err := uuid.Parse(params.IssueID)
		if err != nil {
			return nil, 0, fmt.Errorf("invalid issue_id")
		}
		issueID = &parsed
	}
	return s.store.ListMachinesForUser(ctx, workspaceID, userID, params.Status, issueID, params.PerPage, params.Offset())
}

func (s *DevMachineService) GenerateName(ctx context.Context, workspaceID, userID uuid.UUID) (string, error) {
	adjectives := []string{"amber", "brisk", "calm", "clear", "quiet", "rapid", "silver", "steady", "swift", "vivid"}
	nouns := []string{"badger", "cedar", "comet", "falcon", "harbor", "maple", "orchid", "otter", "summit", "willow"}
	for attempt := 0; attempt < 20; attempt++ {
		suffix, err := randomHex(2)
		if err != nil {
			return "", err
		}
		name := adjectives[int(suffix[0])%len(adjectives)] + "-" + nouns[int(suffix[1])%len(nouns)] + "-" + suffix
		exists, err := s.store.MachineNameExistsForUser(ctx, workspaceID, userID, name)
		if err != nil {
			return "", err
		}
		if !exists {
			return name, nil
		}
	}
	return "", fmt.Errorf("unable to allocate a unique machine name")
}

func (s *DevMachineService) NameAvailable(ctx context.Context, workspaceID, userID uuid.UUID, name string) (bool, error) {
	name = normalizeMachineName(name)
	if !validMachineName(name) {
		return false, nil
	}
	exists, err := s.store.MachineNameExistsForUser(ctx, workspaceID, userID, name)
	return !exists, err
}

func (s *DevMachineService) Update(ctx context.Context, workspaceID, machineID, userID uuid.UUID, request dto.UpdateDevMachineRequest) (*domain.DevMachine, error) {
	machine, err := s.store.UpdateMachinePreferencesForUser(ctx, workspaceID, machineID, userID, request.KeepRunning)
	if err != nil {
		return nil, err
	}
	if machine == nil {
		return nil, ErrMachineNotFound
	}
	return machine, nil
}

func (s *DevMachineService) TouchActivity(ctx context.Context, workspaceID, machineID, userID uuid.UUID) error {
	if _, err := s.GetForUser(ctx, workspaceID, machineID, userID); err != nil {
		return err
	}
	return s.store.TouchMachineActivity(ctx, machineID, time.Now().UTC())
}

func (s *DevMachineService) Delete(ctx context.Context, workspaceID, machineID, userID uuid.UUID) (*domain.DevMachineOperation, error) {
	return s.requestPermanentDelete(ctx, workspaceID, machineID, userID)
}

func (s *DevMachineService) PermanentDelete(ctx context.Context, workspaceID, machineID, userID uuid.UUID) error {
	_, err := s.requestPermanentDelete(ctx, workspaceID, machineID, userID)
	return err
}

func (s *DevMachineService) requestPermanentDelete(ctx context.Context, workspaceID, machineID, userID uuid.UUID) (*domain.DevMachineOperation, error) {
	machine, err := s.store.GetMachine(ctx, workspaceID, machineID)
	if err != nil || machine == nil {
		if err == nil {
			err = ErrMachineNotFound
		}
		return nil, err
	}
	operation, err := s.store.RequestPermanentDelete(ctx, workspaceID, machineID, &userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		if errors.Is(err, repository.ErrIdempotencyKeyConflict) {
			return nil, fmt.Errorf("%w: %v", ErrInvalidOperation, err)
		}
		return nil, err
	}
	payload := map[string]any{}
	if operation != nil {
		payload["operation_id"] = operation.ID
	}
	s.emit(ctx, machine, nil, &userID, "lifecycle", "machine.permanent_delete_requested", payload)
	return operation, nil
}

func (s *DevMachineService) BulkDelete(ctx context.Context, workspaceID, userID uuid.UUID, request dto.BulkDeleteDevMachinesRequest) (int, error) {
	if len(request.MachineIDs) == 0 {
		if request.OldOnly {
			return 0, nil
		}
		return 0, fmt.Errorf("invalid bulk delete request")
	}
	machineIDs := make([]uuid.UUID, 0, len(request.MachineIDs))
	for _, raw := range request.MachineIDs {
		machineID, err := uuid.Parse(raw)
		if err != nil {
			return 0, fmt.Errorf("invalid machine id")
		}
		machineIDs = append(machineIDs, machineID)
	}
	count := 0
	for _, machineID := range machineIDs {
		if _, err := s.Delete(ctx, workspaceID, machineID, userID); err != nil {
			if errors.Is(err, ErrMachineNotFound) {
				continue
			}
			return count, err
		}
		count++
	}
	return count, nil
}

func (s *DevMachineService) BulkPermanentDelete(ctx context.Context, workspaceID uuid.UUID, request dto.PurgeDevMachinesRequest) (int, error) {
	machineIDs := make([]uuid.UUID, 0, len(request.MachineIDs))
	for _, raw := range request.MachineIDs {
		machineID, err := uuid.Parse(raw)
		if err != nil {
			return 0, fmt.Errorf("invalid machine id")
		}
		machineIDs = append(machineIDs, machineID)
	}
	olderThanDays := request.OlderThanDays
	if olderThanDays == 0 {
		olderThanDays = 7
	}
	return s.store.BulkPurgeMachines(ctx, workspaceID, machineIDs, time.Now().UTC().AddDate(0, 0, -olderThanDays), request.IncludeFailed, request.IncludeExpired)
}

func (s *DevMachineService) GetScopeSetting(ctx context.Context, workspaceID uuid.UUID, scopeType string, scopeID *uuid.UUID) (*domain.DevMachineScopeSetting, error) {
	teamID, projectID, issueID, err := scopePointers(scopeType, scopeID)
	if err != nil {
		return nil, err
	}
	setting, err := s.store.GetScopeSetting(ctx, workspaceID, teamID, projectID, issueID)
	if err != nil || setting != nil {
		return setting, err
	}
	return &domain.DevMachineScopeSetting{WorkspaceID: workspaceID, TeamID: teamID, ProjectID: projectID, IssueID: issueID}, nil
}

func (s *DevMachineService) ListScopeSettings(ctx context.Context, workspaceID uuid.UUID) ([]domain.DevMachineScopeSetting, error) {
	return s.store.ListScopeSettings(ctx, workspaceID)
}

func (s *DevMachineService) UpdateScopeSetting(ctx context.Context, workspaceID uuid.UUID, request dto.DevMachineScopeSettingRequest) (*domain.DevMachineScopeSetting, error) {
	var scopeID *uuid.UUID
	if request.ScopeID != nil {
		parsed, _ := uuid.Parse(*request.ScopeID)
		scopeID = &parsed
	}
	teamID, projectID, issueID, err := scopePointers(request.ScopeType, scopeID)
	if err != nil {
		return nil, err
	}
	exists, err := s.store.ScopeResourceExists(ctx, workspaceID, request.ScopeType, scopeID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("invalid development setting scope")
	}
	setting := &domain.DevMachineScopeSetting{WorkspaceID: workspaceID, TeamID: teamID, ProjectID: projectID, IssueID: issueID}
	if request.GitHubRepoID != nil {
		repositoryID, _ := uuid.Parse(*request.GitHubRepoID)
		repository, err := s.store.GetLinkedRepository(ctx, workspaceID, repositoryID)
		if err != nil {
			return nil, err
		}
		if repository == nil {
			return nil, fmt.Errorf("invalid linked repository")
		}
		setting.GitHubRepoID = &repositoryID
		baseBranch := repository.DefaultBranch
		if request.BaseBranch != nil {
			baseBranch = strings.TrimSpace(*request.BaseBranch)
		}
		if !validGitRef(baseBranch) {
			return nil, fmt.Errorf("invalid base branch")
		}
		setting.BaseBranch = &baseBranch
	} else if request.BaseBranch != nil {
		return nil, fmt.Errorf("github_repo_id is required when base_branch is provided")
	}
	if request.EnvironmentID != nil {
		environmentID, _ := uuid.Parse(*request.EnvironmentID)
		environment, err := s.store.GetEnvironment(ctx, workspaceID, environmentID)
		if err != nil {
			return nil, err
		}
		if environment == nil || environment.Status != "ready" {
			return nil, fmt.Errorf("invalid development environment")
		}
		setting.EnvironmentID = &environmentID
	}
	if err := s.store.UpsertScopeSetting(ctx, setting); err != nil {
		return nil, err
	}
	return setting, nil
}

func (s *DevMachineService) DeleteScopeSetting(ctx context.Context, workspaceID uuid.UUID, scopeType string, scopeID *uuid.UUID) error {
	teamID, projectID, issueID, err := scopePointers(scopeType, scopeID)
	if err != nil {
		return err
	}
	if err := s.store.DeleteScopeSetting(ctx, workspaceID, teamID, projectID, issueID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	return nil
}

func scopePointers(scopeType string, scopeID *uuid.UUID) (teamID, projectID, issueID *uuid.UUID, err error) {
	switch scopeType {
	case "workspace":
		if scopeID != nil {
			err = fmt.Errorf("workspace scope does not accept scope_id")
		}
	case "team":
		teamID = scopeID
	case "project":
		projectID = scopeID
	case "issue":
		issueID = scopeID
	default:
		err = fmt.Errorf("invalid development setting scope")
	}
	if scopeType != "workspace" && scopeID == nil {
		err = fmt.Errorf("scope_id is required")
	}
	return
}

func (s *DevMachineService) resolveDevelopmentSetting(ctx context.Context, workspaceID uuid.UUID, issue *domain.Issue, project *domain.Project) (*domain.DevMachineScopeSetting, error) {
	resolved := &domain.DevMachineScopeSetting{WorkspaceID: workspaceID}
	var scopes [][3]*uuid.UUID
	if issue != nil {
		scopes = append(scopes, [3]*uuid.UUID{nil, nil, &issue.ID})
		if issue.ProjectID != nil {
			scopes = append(scopes, [3]*uuid.UUID{nil, issue.ProjectID, nil})
		}
		scopes = append(scopes, [3]*uuid.UUID{&issue.TeamID, nil, nil})
	} else if project != nil {
		scopes = append(scopes, [3]*uuid.UUID{nil, &project.ID, nil})
		if project.TeamID != nil {
			scopes = append(scopes, [3]*uuid.UUID{project.TeamID, nil, nil})
		}
	}
	scopes = append(scopes, [3]*uuid.UUID{nil, nil, nil})
	for _, scope := range scopes {
		if resolved.GitHubRepoID != nil && resolved.EnvironmentID != nil {
			break
		}
		setting, err := s.store.GetScopeSetting(ctx, workspaceID, scope[0], scope[1], scope[2])
		if err != nil {
			return nil, err
		}
		if setting == nil {
			continue
		}
		if resolved.GitHubRepoID == nil && setting.GitHubRepoID != nil {
			resolved.GitHubRepoID, resolved.BaseBranch = setting.GitHubRepoID, setting.BaseBranch
		}
		if resolved.EnvironmentID == nil && setting.EnvironmentID != nil {
			resolved.EnvironmentID = setting.EnvironmentID
		}
	}
	return resolved, nil
}

func (s *DevMachineService) CheckoutIssue(ctx context.Context, workspaceID, machineID, userID uuid.UUID, request dto.CheckoutIssueRequest) (*domain.DevMachineCheckout, error) {
	issueID, _ := uuid.Parse(request.IssueID)
	machine, err := s.GetForUser(ctx, workspaceID, machineID, userID)
	if err != nil {
		return nil, err
	}
	issue, err := s.store.GetIssueDevelopmentContext(ctx, workspaceID, issueID)
	if err != nil {
		return nil, err
	}
	if issue == nil {
		return nil, fmt.Errorf("invalid issue")
	}
	if machine.Status != domain.DevMachineStatusRunning || machine.DesiredStatus != domain.DevMachineStatusRunning {
		return nil, fmt.Errorf("machine must be running")
	}
	var project *domain.Project
	if issue.ProjectID != nil {
		project, err = s.store.GetProjectDevelopmentContext(ctx, workspaceID, *issue.ProjectID)
		if err != nil {
			return nil, err
		}
	}
	setting, err := s.resolveDevelopmentSetting(ctx, workspaceID, issue, project)
	if err != nil {
		return nil, err
	}
	if setting.GitHubRepoID == nil {
		return nil, fmt.Errorf("%w: issue has no development repository", ErrCheckoutNotEligible)
	}
	if setting.EnvironmentID != nil && (machine.EnvironmentID == nil || *machine.EnvironmentID != *setting.EnvironmentID) {
		return nil, fmt.Errorf("%w: issue requires a different development environment", ErrCheckoutNotEligible)
	}
	linkedRepository, err := s.store.GetLinkedRepository(ctx, workspaceID, *setting.GitHubRepoID)
	if err != nil {
		return nil, err
	}
	if linkedRepository == nil || !repositoryAllowedMust(s, ctx, workspaceID, linkedRepository.FullName) {
		return nil, ErrRepositoryNotAllowed
	}
	if machine.RepositoryAffinityID != nil && *machine.RepositoryAffinityID != linkedRepository.ID {
		return nil, fmt.Errorf("%w: machine is assigned to another repository", ErrCheckoutNotEligible)
	}
	existing, err := s.store.ListCheckouts(ctx, workspaceID, machineID)
	if err != nil {
		return nil, err
	}
	for index := range existing {
		if existing[index].IssueID == issueID {
			if existing[index].Status == "failed" {
				existing[index].Status = "queued"
				existing[index].LastError = nil
				operation := &domain.DevMachineOperation{
					ID: uuid.New(), MachineID: machineID, CheckoutID: &existing[index].ID, WorkspaceID: workspaceID,
					Action: domain.DevMachineOpCheckoutIssue, Status: domain.DevMachineOpStatusPending, Generation: machine.Generation,
					IdempotencyKey: "checkout-issue-retry:" + issueID.String() + ":" + uuid.NewString(), RequestedByUserID: &userID, MaxAttempts: 5,
				}
				if err := s.store.CreateCheckout(ctx, &existing[index], operation); err != nil {
					if errors.Is(err, repository.ErrCheckoutMachineConflict) {
						return nil, fmt.Errorf("%w: %v", ErrCheckoutNotEligible, err)
					}
					return nil, err
				}
			}
			return &existing[index], nil
		}
	}
	baseBranch := linkedRepository.DefaultBranch
	if setting.BaseBranch != nil {
		baseBranch = *setting.BaseBranch
	}
	identifier := strings.ToLower(issue.Identifier)
	if !validWorkspacePathSegment(identifier) {
		return nil, fmt.Errorf("invalid issue identifier for checkout path")
	}
	checkout := &domain.DevMachineCheckout{
		ID: uuid.New(), WorkspaceID: workspaceID, MachineID: machineID, IssueID: issueID,
		GitHubRepoID: linkedRepository.ID, RepositoryFullName: linkedRepository.FullName, BaseBranch: baseBranch,
		WorkingBranch: "kuayle/" + identifier, WorkspacePath: "/workspace/tasks/" + identifier, Status: "queued",
	}
	operation := &domain.DevMachineOperation{
		ID: uuid.New(), MachineID: machineID, CheckoutID: &checkout.ID, WorkspaceID: workspaceID,
		Action: domain.DevMachineOpCheckoutIssue, Status: domain.DevMachineOpStatusPending, Generation: machine.Generation,
		IdempotencyKey: "checkout-issue:" + issueID.String(), RequestedByUserID: &userID, MaxAttempts: 5,
	}
	if err := s.store.CreateCheckout(ctx, checkout, operation); err != nil {
		if errors.Is(err, repository.ErrCheckoutMachineConflict) {
			return nil, fmt.Errorf("%w: %v", ErrCheckoutNotEligible, err)
		}
		return nil, err
	}
	return checkout, nil
}

func repositoryAllowedMust(s *DevMachineService, ctx context.Context, workspaceID uuid.UUID, fullName string) bool {
	policy, err := s.enabledPolicy(ctx, workspaceID)
	return err == nil && repositoryAllowed(policy.AllowedRepositories, fullName)
}

func (s *DevMachineService) ListCheckouts(ctx context.Context, workspaceID, machineID, userID uuid.UUID) ([]domain.DevMachineCheckout, error) {
	if _, err := s.GetForUser(ctx, workspaceID, machineID, userID); err != nil {
		return nil, err
	}
	return s.store.ListCheckouts(ctx, workspaceID, machineID)
}

func (s *DevMachineService) ListEnvironments(ctx context.Context, workspaceID uuid.UUID) ([]domain.DevMachineEnvironment, error) {
	return s.store.ListEnvironments(ctx, workspaceID)
}

func (s *DevMachineService) GetEnvironment(ctx context.Context, workspaceID, environmentID uuid.UUID) (*domain.DevMachineEnvironment, error) {
	environment, err := s.store.GetEnvironment(ctx, workspaceID, environmentID)
	if err == nil && (environment == nil || environment.Status == "delete_requested") {
		return nil, ErrEnvironmentNotFound
	}
	return environment, err
}

func (s *DevMachineService) RequestEnvironmentDeletion(ctx context.Context, workspaceID, environmentID uuid.UUID) error {
	if err := s.store.RequestEnvironmentDeletion(ctx, workspaceID, environmentID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("invalid development environment")
		}
		return err
	}
	return nil
}

func (s *DevMachineService) SnapshotEnvironment(ctx context.Context, workspaceID, userID uuid.UUID, request dto.CreateDevMachineEnvironmentRequest) (*domain.DevMachineEnvironment, error) {
	machineID, _ := uuid.Parse(request.SourceMachineID)
	machine, err := s.GetForUser(ctx, workspaceID, machineID, userID)
	if err != nil {
		return nil, err
	}
	if machine.Status != domain.DevMachineStatusPaused && machine.Status != domain.DevMachineStatusStopped {
		return nil, fmt.Errorf("machine must be paused or stopped before saving an environment")
	}
	if !machine.EnvironmentBuilder {
		return nil, fmt.Errorf("only an Environment Builder can be saved as a development environment")
	}
	environmentID := uuid.New()
	environment := &domain.DevMachineEnvironment{
		ID: environmentID, WorkspaceID: workspaceID, Name: strings.TrimSpace(request.Name),
		ImageRef: "kuayle/dev-environment-" + environmentID.String() + ":snapshot", Status: "pending",
		SourceMachineID: &machineID, CreatedByUserID: &userID,
	}
	if environment.Name == "" || strings.ContainsAny(environment.Name, "\x00\r\n") {
		return nil, fmt.Errorf("invalid development environment name")
	}
	operation := &domain.DevMachineOperation{
		ID: uuid.New(), MachineID: machineID, EnvironmentID: &environmentID, WorkspaceID: workspaceID,
		Action: domain.DevMachineOpSnapshotEnvironment, Status: domain.DevMachineOpStatusPending,
		Generation: machine.Generation, IdempotencyKey: "snapshot-environment:" + environmentID.String(),
		RequestedByUserID: &userID, MaxAttempts: 2,
	}
	if err := s.store.CreateEnvironment(ctx, environment, operation); err != nil {
		return nil, err
	}
	return environment, nil
}

func (s *DevMachineService) Lifecycle(ctx context.Context, workspaceID, machineID, userID uuid.UUID, action domain.DevMachineOperationAction, idempotencyKey string) (*domain.DevMachineOperation, error) {
	machine, err := s.GetForUser(ctx, workspaceID, machineID, userID)
	if err != nil {
		return nil, err
	}
	if idempotencyKey != "" {
		if len(idempotencyKey) > 255 || strings.TrimSpace(idempotencyKey) == "" || strings.ContainsAny(idempotencyKey, "\x00\r\n") {
			return nil, fmt.Errorf("invalid idempotency key")
		}
		existing, err := s.store.GetOperationByIdempotency(ctx, workspaceID, machineID, idempotencyKey)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			if existing.Action != action || existing.RequestedByUserID == nil || *existing.RequestedByUserID != userID {
				return nil, fmt.Errorf("%w: idempotency key was already used for another operation", ErrInvalidOperation)
			}
			return existing, nil
		}
	}
	if (action == domain.DevMachineOpStart || action == domain.DevMachineOpSpawn) && !machine.ExpiresAt.After(time.Now().UTC()) {
		return nil, fmt.Errorf("%w: machine has expired", ErrInvalidOperation)
	}
	if !domain.ValidOperationForStatus(action, machine.Status) {
		return nil, fmt.Errorf("%w: cannot %s a %s machine", ErrInvalidOperation, action, machine.Status)
	}
	if action == domain.DevMachineOpPause || action == domain.DevMachineOpStop {
		activeRun, err := s.store.HasActiveAgentRun(ctx, machineID)
		if err != nil {
			return nil, err
		}
		if activeRun {
			return nil, fmt.Errorf("%w: cancel the active agent run before %s", ErrInvalidOperation, action)
		}
	}
	if machine.DesiredStatus == domain.DevMachineStatusDestroyed && action != domain.DevMachineOpTeardown {
		return nil, fmt.Errorf("%w: machine teardown is already queued", ErrInvalidOperation)
	}
	if action == domain.DevMachineOpStart && (machine.Status == domain.DevMachineStatusStopped || machine.Status == domain.DevMachineStatusFailed) {
		policy, err := s.enabledPolicy(ctx, workspaceID)
		if err != nil {
			return nil, err
		}
		workspaceCount, err := s.store.CountActiveMachines(ctx, workspaceID, nil)
		if err != nil {
			return nil, err
		}
		quotaUserID := userID
		if machine.CreatedByUserID != nil {
			quotaUserID = *machine.CreatedByUserID
		}
		userCount, err := s.store.CountActiveMachines(ctx, workspaceID, &quotaUserID)
		if err != nil {
			return nil, err
		}
		if workspaceCount >= policy.MaxConcurrentMachines || userCount >= policy.MaxMachinesPerUser {
			return nil, ErrMachineQuota
		}
	}
	desired := machine.DesiredStatus
	switch action {
	case domain.DevMachineOpStart, domain.DevMachineOpSpawn:
		desired = domain.DevMachineStatusRunning
	case domain.DevMachineOpPause:
		desired = domain.DevMachineStatusPaused
	case domain.DevMachineOpStop:
		desired = domain.DevMachineStatusStopped
	case domain.DevMachineOpTeardown:
		desired = domain.DevMachineStatusDestroyed
	}
	generation := machine.Generation + 1
	if idempotencyKey == "" {
		idempotencyKey = fmt.Sprintf("%s:%d", action, generation)
	}
	operation := &domain.DevMachineOperation{
		ID: uuid.New(), MachineID: machine.ID, WorkspaceID: workspaceID, Action: action,
		Status: domain.DevMachineOpStatusPending, Generation: generation, IdempotencyKey: idempotencyKey,
		RequestedByUserID: &userID, MaxAttempts: 5,
	}
	if err := s.store.SetDesiredAndEnqueue(ctx, workspaceID, machineID, desired, operation); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: machine state changed while queuing operation", ErrInvalidOperation)
		}
		if errors.Is(err, repository.ErrIdempotencyKeyConflict) {
			return nil, fmt.Errorf("%w: %v", ErrInvalidOperation, err)
		}
		return nil, err
	}
	s.emit(ctx, machine, nil, &userID, "lifecycle", "operation.queued", map[string]any{"action": action, "operation_id": operation.ID})
	return operation, nil
}

func (s *DevMachineService) ListServices(ctx context.Context, workspaceID, machineID, userID uuid.UUID) ([]domain.DevMachineService, error) {
	if _, err := s.GetForUser(ctx, workspaceID, machineID, userID); err != nil {
		return nil, err
	}
	return s.store.ListServices(ctx, workspaceID, machineID)
}

func (s *DevMachineService) ListProviders(ctx context.Context, workspaceID, machineID, userID uuid.UUID) ([]domain.DevMachineAgentProvider, error) {
	if _, err := s.GetForUser(ctx, workspaceID, machineID, userID); err != nil {
		return nil, err
	}
	return s.store.ListProviders(ctx, workspaceID, machineID)
}

func (s *DevMachineService) AvailableProviders(ctx context.Context, workspaceID uuid.UUID) ([]dto.AgentProviderResponse, error) {
	policy, err := s.store.GetPolicy(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	responses := make([]dto.AgentProviderResponse, 0)
	for _, provider := range s.agents.List() {
		metadata := provider.Metadata()
		if policy != nil {
			if metadata.Custom && !policy.AllowCustomProviders {
				continue
			}
			if !metadata.Custom && !providerAllowed(policy.AllowedProviders, metadata.ID) {
				continue
			}
		}
		modes := make([]string, len(metadata.SupportedModes))
		for i, mode := range metadata.SupportedModes {
			modes[i] = string(mode)
		}
		responses = append(responses, dto.AgentProviderResponse{
			ID: metadata.ID, DisplayName: metadata.DisplayName, DefaultImage: metadata.DefaultImage,
			RequiredSecrets: metadata.RequiredSecrets, SupportedModes: modes, Custom: metadata.Custom,
		})
	}
	return responses, nil
}

func (s *DevMachineService) ConfiguredProviders(ctx context.Context, workspaceID, machineID, userID uuid.UUID) ([]dto.AgentProviderResponse, error) {
	if _, err := s.GetForUser(ctx, workspaceID, machineID, userID); err != nil {
		return nil, err
	}
	providers, err := s.store.ListProviders(ctx, workspaceID, machineID)
	if err != nil {
		return nil, err
	}
	responses := make([]dto.AgentProviderResponse, 0, len(providers))
	for _, provider := range providers {
		if !provider.Enabled {
			continue
		}
		var requiredSecrets, supportedModes []string
		_ = json.Unmarshal(provider.RequiredSecrets, &requiredSecrets)
		_ = json.Unmarshal(provider.SupportedModes, &supportedModes)
		responses = append(responses, dto.AgentProviderResponse{
			ID: provider.ProviderID, DisplayName: provider.DisplayName, DefaultImage: provider.ImageRef,
			RequiredSecrets: requiredSecrets, SupportedModes: supportedModes, Custom: provider.IsCustom,
		})
	}
	return responses, nil
}

func (s *DevMachineService) CreateAgentRun(ctx context.Context, workspaceID, machineID, userID uuid.UUID, req dto.CreateAgentRunRequest) (*domain.DevMachineAgentRun, error) {
	policy, err := s.enabledPolicy(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	machine, err := s.GetForUser(ctx, workspaceID, machineID, userID)
	if err != nil {
		return nil, err
	}
	startOfDay := time.Now().UTC().Truncate(24 * time.Hour)
	dailyRuns, err := s.store.CountAgentRunsSince(ctx, workspaceID, startOfDay)
	if err != nil {
		return nil, err
	}
	if dailyRuns >= policy.MaxDailyAgentRuns {
		return nil, fmt.Errorf("%w: daily agent run limit reached", ErrMachineQuota)
	}
	if machine.Status != domain.DevMachineStatusRunning || machine.DesiredStatus != domain.DevMachineStatusRunning {
		return nil, fmt.Errorf("machine must be running")
	}
	var checkoutID *uuid.UUID
	var selectedCheckout *domain.DevMachineCheckout
	if req.CheckoutID != nil {
		parsed, _ := uuid.Parse(*req.CheckoutID)
		checkout, err := s.store.GetCheckout(ctx, workspaceID, machineID, parsed)
		if err != nil {
			return nil, err
		}
		if checkout == nil || checkout.Status != "ready" {
			return nil, fmt.Errorf("checkout must be ready")
		}
		checkoutID = &parsed
		selectedCheckout = checkout
	}
	if machine.RepositoryAffinityID != nil && checkoutID == nil {
		checkouts, err := s.store.ListCheckouts(ctx, workspaceID, machineID)
		if err != nil {
			return nil, err
		}
		if len(checkouts) == 1 && checkouts[0].Status == "ready" {
			checkoutID = &checkouts[0].ID
			selectedCheckout = &checkouts[0]
		} else if len(checkouts) > 1 {
			return nil, fmt.Errorf("checkout_id is required when a machine has multiple issue branches")
		}
	}
	remainingRuntime := int(time.Until(machine.ExpiresAt).Seconds())
	if remainingRuntime < 30 {
		return nil, fmt.Errorf("machine has expired or has insufficient runtime remaining")
	}
	active, err := s.store.HasActiveAgentRun(ctx, machineID)
	if err != nil {
		return nil, err
	}
	if active {
		return nil, fmt.Errorf("%w: this machine already has an active agent run", ErrMachineQuota)
	}
	registered, err := s.store.GetProvider(ctx, workspaceID, machineID, req.Provider)
	if err != nil {
		return nil, err
	}
	if registered == nil {
		return nil, fmt.Errorf("%w: %s", ErrProviderNotAllowed, req.Provider)
	}
	if (registered.IsCustom && !policy.AllowCustomProviders) || (!registered.IsCustom && !providerAllowed(policy.AllowedProviders, req.Provider)) {
		return nil, fmt.Errorf("%w: %s", ErrProviderNotAllowed, req.Provider)
	}
	provider, ok := s.agents.Get(req.Provider)
	if !ok {
		return nil, fmt.Errorf("unknown provider %s", req.Provider)
	}
	allowedSecretSet := make(map[string]bool, len(req.AllowedSecrets))
	for _, name := range req.AllowedSecrets {
		if !validEnvName(name) {
			return nil, fmt.Errorf("invalid secret name %q", name)
		}
		if name == "GITHUB_TOKEN" || strings.HasPrefix(name, "KUAYLE_") {
			return nil, fmt.Errorf("secret %s is managed by Kuayle and cannot be selected", name)
		}
		allowedSecretSet[name] = true
	}
	var requiredSecrets []string
	_ = json.Unmarshal(registered.RequiredSecrets, &requiredSecrets)
	for _, required := range requiredSecrets {
		if !allowedSecretSet[required] {
			return nil, fmt.Errorf("invalid allowed secrets: provider %s requires %s", req.Provider, required)
		}
	}
	activeSecrets, err := s.store.ListEnvVarsInternal(ctx, machineID, &req.Provider, "agent")
	if err != nil {
		return nil, err
	}
	activeSecretNames := make(map[string]bool, len(activeSecrets))
	for _, envVar := range activeSecrets {
		activeSecretNames[envVar.Name] = true
	}
	for _, required := range requiredSecrets {
		if !activeSecretNames[required] {
			return nil, fmt.Errorf("provider %s requires an active %s secret", req.Provider, required)
		}
	}
	if req.Config != nil {
		return nil, fmt.Errorf("invalid run-specific provider config: configure the provider when creating the machine")
	}
	config := registered.Config
	prompt := buildAgentPrompt(machine, selectedCheckout, req)
	workspacePath := "/workspace"
	runIssueID := machine.IssueID
	if selectedCheckout != nil {
		workspacePath = selectedCheckout.WorkspacePath
		runIssueID = &selectedCheckout.IssueID
	}
	invocation, err := provider.BuildInvocation(agent.RunInput{
		Mode: agent.Mode(req.Mode), Prompt: prompt, WorkspacePath: workspacePath,
		AcceptanceCriteria: req.AcceptanceCriteria, TestCommand: req.TestCommand, ExtraArgs: req.ExtraArgs, Config: config,
	})
	if err != nil {
		return nil, err
	}
	commandArgv, _ := json.Marshal(invocation.Argv)
	criteria, _ := json.Marshal(req.AcceptanceCriteria)
	allowedCommands, _ := json.Marshal(req.AllowedCommands)
	forbiddenPaths, _ := json.Marshal(req.ForbiddenPaths)
	allowedSecrets, _ := json.Marshal(req.AllowedSecrets)
	testCommandJSON, _ := json.Marshal(req.TestCommand)
	testCommand := json.RawMessage(testCommandJSON)
	runID := uuid.New()
	timeout := req.MaxRuntimeSeconds
	if timeout == 0 {
		timeout = 3600
	}
	if timeout > machine.MaxRuntimeMinutes*60 {
		return nil, fmt.Errorf("agent runtime exceeds machine maximum")
	}
	if timeout > remainingRuntime {
		return nil, fmt.Errorf("agent runtime exceeds machine lifetime")
	}
	run := &domain.DevMachineAgentRun{
		ID: runID, MachineID: machineID, WorkspaceID: workspaceID, IssueID: runIssueID, CheckoutID: checkoutID,
		RequestedByUserID: &userID, ProviderID: req.Provider, Mode: req.Mode,
		Status: domain.DevMachineAgentRunStatusQueued, Prompt: prompt, AcceptanceCriteria: criteria,
		AllowedCommands: allowedCommands, ForbiddenPaths: forbiddenPaths, AllowedSecrets: allowedSecrets, TestCommand: &testCommand,
		CommandArgv: commandArgv, MaxRuntimeSeconds: timeout, PushBranch: req.PushBranch == nil || *req.PushBranch,
		OpenPullRequest: req.OpenPullRequest,
	}
	operation := &domain.DevMachineOperation{
		ID: uuid.New(), MachineID: machineID, AgentRunID: &runID, WorkspaceID: workspaceID,
		Action: domain.DevMachineOpRunAgent, Status: domain.DevMachineOpStatusPending, Generation: machine.Generation,
		IdempotencyKey: "run-agent:" + runID.String(), RequestedByUserID: &userID, MaxAttempts: 3,
	}
	if err := s.store.CreateAgentRun(ctx, run, operation); err != nil {
		return nil, err
	}
	_ = s.store.TouchMachineActivity(ctx, machineID, time.Now().UTC())
	s.emit(ctx, machine, &runID, &userID, "agent", "agent_run.queued", map[string]any{"provider_id": req.Provider, "mode": req.Mode})
	return run, nil
}

func (s *DevMachineService) ListAgentRuns(ctx context.Context, workspaceID, userID uuid.UUID, machineID *uuid.UUID, page, perPage int) ([]domain.DevMachineAgentRun, int, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 50
	}
	if machineID != nil {
		if _, err := s.GetForUser(ctx, workspaceID, *machineID, userID); err != nil {
			return nil, 0, err
		}
	}
	return s.store.ListAgentRunsForUser(ctx, workspaceID, userID, machineID, perPage, (page-1)*perPage)
}

func (s *DevMachineService) GetAgentRun(ctx context.Context, workspaceID, runID, userID uuid.UUID) (*domain.DevMachineAgentRun, error) {
	run, err := s.store.GetAgentRunForUser(ctx, workspaceID, runID, userID)
	if err == nil && run == nil {
		return nil, ErrAgentRunNotFound
	}
	return run, err
}

func (s *DevMachineService) CancelAgentRun(ctx context.Context, workspaceID, runID, userID uuid.UUID) error {
	run, err := s.GetAgentRun(ctx, workspaceID, runID, userID)
	if err != nil {
		return err
	}
	machine, err := s.GetForUser(ctx, workspaceID, run.MachineID, userID)
	if err != nil {
		if errors.Is(err, ErrMachineNotFound) {
			return ErrAgentRunNotFound
		}
		return err
	}
	operation := &domain.DevMachineOperation{
		ID: uuid.New(), MachineID: run.MachineID, AgentRunID: &runID, WorkspaceID: workspaceID,
		Action: domain.DevMachineOpCancelAgent, Status: domain.DevMachineOpStatusPending,
		Generation: machine.Generation, IdempotencyKey: "cancel-agent:" + runID.String(), RequestedByUserID: &userID, MaxAttempts: 3,
	}
	return s.store.CancelAgentRun(ctx, workspaceID, runID, operation)
}

func (s *DevMachineService) GetAgentRunTrace(ctx context.Context, workspaceID, runID, userID uuid.UUID, params dto.TraceListParams) (*dto.AgentRunTraceResponse, error) {
	run, err := s.GetAgentRun(ctx, workspaceID, runID, userID)
	if err != nil {
		return nil, err
	}
	// Verify run belongs to workspace
	if run.WorkspaceID != workspaceID {
		return nil, ErrAgentRunNotFound
	}
	params.Defaults()
	steps, err := s.store.ListAgentRunSteps(ctx, runID)
	if err != nil {
		return nil, err
	}
	events, err := s.store.ListAgentRunEvents(ctx, runID, params.EventsAfterID, params.EventsLimit+1)
	if err != nil {
		return nil, err
	}
	logs, err := s.store.ListAgentRunLogs(ctx, runID, params.LogsAfterID, params.LogsLimit+1)
	if err != nil {
		return nil, err
	}
	hasMoreEvents := len(events) > params.EventsLimit
	if hasMoreEvents {
		events = events[:params.EventsLimit]
	}
	hasMoreLogs := len(logs) > params.LogsLimit
	if hasMoreLogs {
		logs = logs[:params.LogsLimit]
	}
	nextEventID := params.EventsAfterID
	if len(events) > 0 {
		nextEventID = events[len(events)-1].ID
	}
	nextLogID := params.LogsAfterID
	if len(logs) > 0 {
		nextLogID = logs[len(logs)-1].ID
	}
	return &dto.AgentRunTraceResponse{
		Run: run, Steps: steps, Events: events, Logs: logs,
		NextEventID: nextEventID, NextLogID: nextLogID,
		HasMoreEvents: hasMoreEvents, HasMoreLogs: hasMoreLogs,
	}, nil
}

func (s *DevMachineService) LaunchService(ctx context.Context, workspaceID, machineID, userID uuid.UUID, serviceKey string, checkoutID *uuid.UUID) (*dto.LaunchServiceResponse, error) {
	if _, err := s.enabledPolicy(ctx, workspaceID); err != nil {
		return nil, err
	}
	machine, err := s.GetForUser(ctx, workspaceID, machineID, userID)
	if err != nil {
		return nil, err
	}
	if s.domain == "" {
		return nil, fmt.Errorf("dev machine domain is not configured")
	}
	if !machine.ExpiresAt.After(time.Now().UTC()) {
		return nil, fmt.Errorf("%w: machine has expired", ErrInvalidOperation)
	}
	if pending, handled, err := s.pendingLaunch(ctx, workspaceID, machine, userID); handled || err != nil {
		return pending, err
	}
	service, err := s.store.GetService(ctx, workspaceID, machineID, serviceKey)
	if err != nil {
		return nil, err
	}
	if service == nil || service.Status != "running" || (service.ServiceType != "ide" && service.ServiceType != "terminal" && service.ServiceType != "browser") {
		return nil, ErrServiceNotAvailable
	}
	var checkout *domain.DevMachineCheckout
	if checkoutID != nil {
		checkout, err = s.store.GetCheckout(ctx, workspaceID, machineID, *checkoutID)
		if err != nil {
			return nil, err
		}
		if checkout == nil || checkout.Status != "ready" {
			return nil, fmt.Errorf("checkout must be ready")
		}
	}
	host := machine.RoutingKey + "." + s.domain
	if service.ServiceType == "browser" {
		host = machine.RoutingKey + "-browser." + s.domain
	} else if service.ServiceType == "terminal" {
		host = machine.RoutingKey + "-terminal." + s.domain
	}
	raw, err := randomHex(32)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256([]byte(raw))
	expiresAt := time.Now().UTC().Add(s.ticketTTL)
	if machine.ExpiresAt.Before(expiresAt) {
		expiresAt = machine.ExpiresAt
	}
	ticket := &domain.DevMachineAccessTicket{
		ID: uuid.New(), WorkspaceID: workspaceID, MachineID: machineID, ServiceID: service.ID, UserID: userID,
		TokenHash: hex.EncodeToString(hash[:]), Status: domain.DevMachineAccessTicketStatusActive,
		BoundHost: host, ExpiresAt: expiresAt,
	}
	if err := s.store.CreateAccessTicket(ctx, ticket); err != nil {
		return nil, err
	}
	_ = s.store.TouchMachineActivity(ctx, machine.ID, time.Now().UTC())
	s.emit(ctx, machine, nil, &userID, "gateway", "service.launch_requested", map[string]any{"service": service.ServiceType})
	launchPath := "/"
	query := url.Values{"ticket": []string{raw}}
	if service.ServiceType == "browser" {
		query.Set("autoconnect", "true")
		query.Set("resize", "remote")
		query.Set("enable_webrtc", "false")
	}
	if checkout != nil && service.ServiceType == "ide" {
		query.Set("folder", checkout.WorkspacePath)
	} else if checkout != nil && service.ServiceType == "terminal" {
		query.Set("arg", checkout.WorkspacePath)
	}
	launchURL := (&url.URL{Scheme: "https", Host: host, Path: launchPath, RawQuery: query.Encode()}).String()
	return &dto.LaunchServiceResponse{Status: "ready", LaunchURL: launchURL, ExpiresAt: ticket.ExpiresAt}, nil
}

func (s *DevMachineService) pendingLaunch(ctx context.Context, workspaceID uuid.UUID, machine *domain.DevMachine, userID uuid.UUID) (*dto.LaunchServiceResponse, bool, error) {
	if machine.Status == domain.DevMachineStatusRunning && machine.DesiredStatus == domain.DevMachineStatusRunning {
		return nil, false, nil
	}
	if machine.Status == domain.DevMachineStatusPaused {
		if machine.DesiredStatus == domain.DevMachineStatusDestroyed {
			return nil, true, fmt.Errorf("%w: machine teardown is already queued", ErrInvalidOperation)
		}
		if machine.DesiredStatus != domain.DevMachineStatusPaused && machine.DesiredStatus != domain.DevMachineStatusRunning {
			return nil, true, fmt.Errorf("%w: machine is transitioning to %s", ErrInvalidOperation, machine.DesiredStatus)
		}
		var operation *domain.DevMachineOperation
		var err error
		if machine.DesiredStatus != domain.DevMachineStatusRunning {
			operation, err = s.Lifecycle(ctx, workspaceID, machine.ID, userID, domain.DevMachineOpStart, fmt.Sprintf("launch-resume:%s:%d", userID, machine.Generation+1))
			if err != nil {
				return nil, true, err
			}
		}
		return &dto.LaunchServiceResponse{Status: "resuming", Operation: operationDTO(operation), RetryAfterSeconds: 2}, true, nil
	}
	if (machine.Status == domain.DevMachineStatusQueued || machine.Status == domain.DevMachineStatusSpawning) && machine.DesiredStatus == domain.DevMachineStatusRunning {
		return &dto.LaunchServiceResponse{Status: "pending", RetryAfterSeconds: 2}, true, nil
	}
	return nil, true, fmt.Errorf("%w: machine is not running", ErrInvalidOperation)
}

func (s *DevMachineService) CreateTerminalSession(ctx context.Context, workspaceID, machineID, userID uuid.UUID, req dto.CreateTerminalSessionRequest) (*dto.TerminalSessionLaunchResponse, error) {
	if _, err := s.enabledPolicy(ctx, workspaceID); err != nil {
		return nil, err
	}
	machine, err := s.GetForUser(ctx, workspaceID, machineID, userID)
	if err != nil {
		return nil, err
	}
	if s.domain == "" {
		return nil, fmt.Errorf("dev machine domain is not configured")
	}
	if s.frontendOrigin == "" {
		return nil, fmt.Errorf("frontend origin is not configured")
	}
	if !machine.ExpiresAt.After(time.Now().UTC()) {
		return nil, fmt.Errorf("%w: machine has expired", ErrInvalidOperation)
	}
	if pending, handled, err := s.pendingLaunch(ctx, workspaceID, machine, userID); handled || err != nil {
		if err != nil {
			return nil, err
		}
		return &dto.TerminalSessionLaunchResponse{Status: pending.Status, Operation: pending.Operation, RetryAfterSeconds: pending.RetryAfterSeconds}, nil
	}
	var checkoutID *uuid.UUID
	var checkout *domain.DevMachineCheckout
	if req.CheckoutID != nil {
		parsed, err := uuid.Parse(*req.CheckoutID)
		if err != nil {
			return nil, fmt.Errorf("invalid checkout_id")
		}
		checkout, err = s.store.GetCheckout(ctx, workspaceID, machineID, parsed)
		if err != nil {
			return nil, err
		}
		if checkout == nil || checkout.Status != "ready" {
			return nil, fmt.Errorf("checkout must be ready")
		}
		checkoutID = &parsed
	}
	terminalService, err := s.store.GetService(ctx, workspaceID, machineID, "terminal")
	if err != nil {
		return nil, err
	}
	if terminalService == nil || terminalService.Status != "running" {
		return nil, ErrServiceNotAvailable
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "Terminal"
	}
	if strings.ContainsAny(name, "\x00\r\n") {
		return nil, fmt.Errorf("invalid terminal session name")
	}
	runtimeName := "term-" + uuid.NewString()
	session := &domain.DevMachineTerminalSession{
		ID: uuid.New(), WorkspaceID: workspaceID, MachineID: machineID, CheckoutID: checkoutID,
		UserID: userID, Name: name, RuntimeSessionName: runtimeName, Status: "active",
	}
	if err := s.store.CreateTerminalSession(ctx, session); err != nil {
		return nil, err
	}
	raw, err := randomHex(32)
	if err != nil {
		return nil, err
	}
	cwd := terminalWorkingDirectory(machine, checkout)
	host := machine.RoutingKey + "-terminal." + s.domain
	expiresAt := time.Now().UTC().Add(s.ticketTTL)
	if machine.ExpiresAt.Before(expiresAt) {
		expiresAt = machine.ExpiresAt
	}
	ticket := &domain.DevMachineAccessTicket{
		ID: uuid.New(), WorkspaceID: workspaceID, MachineID: machineID, ServiceID: terminalService.ID, UserID: userID,
		TokenHash: terminalTicketHash(raw, s.frontendOrigin, runtimeName, cwd), Status: domain.DevMachineAccessTicketStatusActive,
		BoundHost: host, ExpiresAt: expiresAt,
	}
	if err := s.store.CreateAccessTicket(ctx, ticket); err != nil {
		return nil, err
	}
	_ = s.store.TouchMachineActivity(ctx, machine.ID, time.Now().UTC())
	s.emit(ctx, machine, nil, &userID, "gateway", "terminal_session.launch_requested", map[string]any{"session_id": session.ID})
	query := url.Values{}
	query.Set("ticket", raw)
	query.Set("session", runtimeName)
	query.Set("cwd", cwd)
	websocketURL := url.URL{Scheme: "wss", Host: host, Path: "/ws", RawQuery: query.Encode()}
	return &dto.TerminalSessionLaunchResponse{
		Status: "ready", Session: terminalSessionResponse(*session), LaunchURL: websocketURL.String(), WebSocketURL: websocketURL.String(),
		Protocol: "ttyd.v1", ExpiresAt: ticket.ExpiresAt,
	}, nil
}

func (s *DevMachineService) ListTerminalSessions(ctx context.Context, workspaceID, machineID, userID uuid.UUID) ([]dto.TerminalSessionResponse, error) {
	if _, err := s.GetForUser(ctx, workspaceID, machineID, userID); err != nil {
		return nil, err
	}
	sessions, err := s.store.ListTerminalSessions(ctx, workspaceID, machineID)
	if err != nil {
		return nil, err
	}
	responses := make([]dto.TerminalSessionResponse, 0, len(sessions))
	for _, session := range sessions {
		responses = append(responses, terminalSessionResponse(session))
	}
	return responses, nil
}

func (s *DevMachineService) CloseTerminalSession(ctx context.Context, workspaceID, machineID, userID, sessionID uuid.UUID) (*dto.TerminalSessionResponse, error) {
	if _, err := s.GetForUser(ctx, workspaceID, machineID, userID); err != nil {
		return nil, err
	}
	session, err := s.store.CloseTerminalSession(ctx, workspaceID, machineID, sessionID)
	if err != nil {
		return nil, err
	}
	if session == nil {
		return nil, ErrTerminalSessionNotFound
	}
	response := terminalSessionResponse(*session)
	return &response, nil
}

func terminalSessionResponse(session domain.DevMachineTerminalSession) dto.TerminalSessionResponse {
	var checkoutID *string
	if session.CheckoutID != nil {
		value := session.CheckoutID.String()
		checkoutID = &value
	}
	return dto.TerminalSessionResponse{
		ID: session.ID.String(), MachineID: session.MachineID.String(), CheckoutID: checkoutID,
		Name: session.Name, RuntimeSessionName: session.RuntimeSessionName, Status: session.Status,
		CreatedAt: session.CreatedAt, LastActivityAt: session.LastActivityAt, ClosedAt: session.ClosedAt,
	}
}

func operationDTO(operation *domain.DevMachineOperation) *dto.DevMachineOperationResponse {
	if operation == nil {
		return nil
	}
	return &dto.DevMachineOperationResponse{
		ID: operation.ID.String(), Action: string(operation.Action), Status: string(operation.Status),
		Generation: operation.Generation, IdempotencyKey: operation.IdempotencyKey, Attempts: operation.Attempts,
		ErrorCode: operation.ErrorCode, ErrorMessage: operation.ErrorMessage, CreatedAt: operation.CreatedAt,
		CompletedAt: operation.CompletedAt,
	}
}

func terminalWorkingDirectory(machine *domain.DevMachine, checkout *domain.DevMachineCheckout) string {
	if checkout != nil && checkout.WorkspacePath != "" {
		return checkout.WorkspacePath
	}
	if machine.RepoOwner != "" && machine.RepoName != "" {
		return "/workspace"
	}
	return "/workspace/tasks"
}

func terminalTicketHash(rawTicket, frontendOrigin, runtimeSessionName, workingDirectory string) string {
	hash := sha256.Sum256([]byte(strings.Join([]string{rawTicket, frontendOrigin, runtimeSessionName, workingDirectory}, "\n")))
	return hex.EncodeToString(hash[:])
}

func (s *DevMachineService) ListEvents(ctx context.Context, workspaceID, machineID, userID uuid.UUID, afterID int64, limit int) ([]domain.DevMachineEvent, error) {
	if _, err := s.GetForUser(ctx, workspaceID, machineID, userID); err != nil {
		return nil, err
	}
	return s.store.ListEvents(ctx, workspaceID, machineID, afterID, limit)
}

func (s *DevMachineService) ListLogs(ctx context.Context, workspaceID, machineID, userID uuid.UUID, runID *uuid.UUID, afterID int64, limit int) ([]domain.DevMachineLogChunk, error) {
	if _, err := s.GetForUser(ctx, workspaceID, machineID, userID); err != nil {
		return nil, err
	}
	return s.store.ListLogs(ctx, workspaceID, machineID, runID, afterID, limit)
}

func (s *DevMachineService) ListResourceSamples(ctx context.Context, workspaceID, machineID, userID uuid.UUID, limit int) ([]domain.DevMachineResourceSample, error) {
	if _, err := s.GetForUser(ctx, workspaceID, machineID, userID); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 500 {
		limit = 120
	}
	return s.store.ListResourceSamples(ctx, workspaceID, machineID, limit)
}

func (s *DevMachineService) IngestEvent(ctx context.Context, rawToken string, input dto.CollectorEventInput) error {
	if !s.enabled {
		return ErrDevMachinesDisabled
	}
	token, machine, err := s.authenticateMachineToken(ctx, rawToken, "events:write")
	if err != nil {
		return err
	}
	var runID *uuid.UUID
	if input.AgentRunID != nil {
		parsed, _ := uuid.Parse(*input.AgentRunID)
		run, err := s.store.GetAgentRun(ctx, machine.WorkspaceID, parsed)
		if err != nil || run == nil || run.MachineID != token.MachineID {
			return ErrAgentRunNotFound
		}
		runID = &parsed
	}
	secrets, err := s.machineSecretValues(ctx, machine.ID)
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(redactPayload(input.Payload, secrets))
	if input.Payload == nil {
		payload = []byte(`{}`)
	}
	occurredAt := time.Now().UTC()
	if input.OccurredAt != nil && input.OccurredAt.Before(occurredAt.Add(5*time.Minute)) && input.OccurredAt.After(occurredAt.Add(-24*time.Hour)) {
		occurredAt = input.OccurredAt.UTC()
	}
	return s.store.CreateEvent(ctx, &domain.DevMachineEvent{
		WorkspaceID: machine.WorkspaceID, MachineID: machine.ID, AgentRunID: runID,
		Source: input.Source, EventType: input.EventType, Payload: payload, OccurredAt: occurredAt,
	})
}

func (s *DevMachineService) IngestLog(ctx context.Context, rawToken string, input dto.CollectorLogInput) error {
	if !s.enabled {
		return ErrDevMachinesDisabled
	}
	_, machine, err := s.authenticateMachineToken(ctx, rawToken, "logs:write")
	if err != nil {
		return err
	}
	var runID, serviceID *uuid.UUID
	if input.AgentRunID != nil {
		parsed, _ := uuid.Parse(*input.AgentRunID)
		run, err := s.store.GetAgentRun(ctx, machine.WorkspaceID, parsed)
		if err != nil || run == nil || run.MachineID != machine.ID {
			return ErrAgentRunNotFound
		}
		runID = &parsed
	}
	if input.ServiceID != nil {
		parsed, _ := uuid.Parse(*input.ServiceID)
		services, err := s.store.ListServices(ctx, machine.WorkspaceID, machine.ID)
		if err != nil {
			return err
		}
		matched := false
		for _, service := range services {
			if service.ID == parsed {
				matched = true
				break
			}
		}
		if !matched {
			return ErrServiceNotAvailable
		}
		serviceID = &parsed
	}
	secrets, err := s.machineSecretValues(ctx, machine.ID)
	if err != nil {
		return err
	}
	return s.store.CreateLogChunk(ctx, &domain.DevMachineLogChunk{
		WorkspaceID: machine.WorkspaceID, MachineID: machine.ID, AgentRunID: runID, ServiceID: serviceID,
		Stream: input.Stream, Sequence: input.Sequence, Content: redactText(input.Content, secrets), Truncated: input.Truncated,
	})
}

func (s *DevMachineService) authenticateMachineToken(ctx context.Context, rawToken, scope string) (*domain.DevMachineToken, *domain.DevMachine, error) {
	if len(rawToken) != 64 {
		return nil, nil, errors.New("invalid machine token")
	}
	hash := sha256.Sum256([]byte(rawToken))
	token, machine, err := s.store.AuthenticateMachineToken(ctx, hex.EncodeToString(hash[:]), scope)
	if err != nil {
		return nil, nil, err
	}
	if token == nil || machine == nil {
		return nil, nil, errors.New("invalid machine token")
	}
	return token, machine, nil
}

func (s *DevMachineService) GetPolicy(ctx context.Context, workspaceID uuid.UUID) (*domain.DevMachineWorkspacePolicy, error) {
	policy, err := s.store.GetPolicy(ctx, workspaceID)
	if err != nil || policy != nil {
		return policy, err
	}
	return &domain.DevMachineWorkspacePolicy{
		WorkspaceID: workspaceID, MaxConcurrentMachines: 5, MaxMachinesPerUser: 2, MaxDailyAgentRuns: 25,
		MaxRuntimeMinutes: 480, MaxDiskGB: 100, IdlePauseMinutes: 240,
		AllowedProviders:    json.RawMessage(`["claude-code","opencode","codex"]`),
		AllowedRepositories: json.RawMessage(`[]`),
	}, nil
}

func (s *DevMachineService) UpdatePolicy(ctx context.Context, workspaceID uuid.UUID, req dto.DevMachinePolicyRequest) (*domain.DevMachineWorkspacePolicy, error) {
	providers, _ := json.Marshal(req.AllowedProviders)
	repositories, _ := json.Marshal(req.AllowedRepositories)
	idlePauseMinutes := req.IdlePauseMinutes
	if idlePauseMinutes == 0 {
		idlePauseMinutes = 240
	}
	policy := &domain.DevMachineWorkspacePolicy{
		WorkspaceID: workspaceID, Enabled: req.Enabled, MaxConcurrentMachines: req.MaxConcurrentMachines,
		MaxMachinesPerUser: req.MaxMachinesPerUser, MaxDailyAgentRuns: req.MaxDailyAgentRuns,
		MaxRuntimeMinutes: req.MaxRuntimeMinutes, MaxDiskGB: req.MaxDiskGB, AllowedProviders: providers,
		AllowedRepositories: repositories, AllowCustomProviders: req.AllowCustomProviders, IdlePauseMinutes: idlePauseMinutes,
	}
	if err := s.store.UpsertPolicy(ctx, policy); err != nil {
		return nil, err
	}
	return policy, nil
}

func (s *DevMachineService) enabledPolicy(ctx context.Context, workspaceID uuid.UUID) (*domain.DevMachineWorkspacePolicy, error) {
	if !s.enabled {
		return nil, ErrDevMachinesDisabled
	}
	policy, err := s.store.GetPolicy(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	if policy == nil || !policy.Enabled {
		return nil, ErrDevMachinesDisabled
	}
	return policy, nil
}

func (s *DevMachineService) emit(ctx context.Context, machine *domain.DevMachine, runID *uuid.UUID, actorID *uuid.UUID, source, eventType string, payload any) {
	data, _ := json.Marshal(payload)
	_ = s.store.CreateEvent(ctx, &domain.DevMachineEvent{
		WorkspaceID: machine.WorkspaceID, MachineID: machine.ID, AgentRunID: runID, ActorUserID: actorID,
		Source: source, EventType: eventType, Payload: data, OccurredAt: time.Now().UTC(),
	})
}

func providerAllowed(raw json.RawMessage, provider string) bool {
	var allowed []string
	if json.Unmarshal(raw, &allowed) != nil {
		return false
	}
	for _, candidate := range allowed {
		if candidate == provider {
			return true
		}
	}
	return false
}

func repositoryAllowed(raw json.RawMessage, repository string) bool {
	var allowed []string
	if json.Unmarshal(raw, &allowed) != nil || len(allowed) == 0 {
		return true
	}
	for _, candidate := range allowed {
		if strings.EqualFold(candidate, repository) {
			return true
		}
	}
	return false
}

func normalizeOrigin(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil {
		return ""
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return ""
	}
	return scheme + "://" + strings.ToLower(parsed.Host)
}

func randomHex(bytes int) (string, error) {
	buffer := make([]byte, bytes)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

func buildAgentPrompt(machine *domain.DevMachine, checkout *domain.DevMachineCheckout, request dto.CreateAgentRunRequest) string {
	var builder strings.Builder
	builder.WriteString(request.Prompt)
	builder.WriteString("\n\nKuayle execution contract:\n")
	workspacePath := "/workspace"
	repository, baseBranch, workingBranch := machine.RepoOwner+"/"+machine.RepoName, machine.BaseBranch, machine.WorkingBranch
	if checkout != nil {
		workspacePath, repository, baseBranch, workingBranch = checkout.WorkspacePath, checkout.RepositoryFullName, checkout.BaseBranch, checkout.WorkingBranch
	}
	fmt.Fprintf(&builder, "- Repository: %s\n- Base branch: %s\n- Working branch: %s\n", repository, baseBranch, workingBranch)
	fmt.Fprintf(&builder, "- Work only inside %s. Never print or persist credentials.\n", workspacePath)
	if len(request.ForbiddenPaths) > 0 {
		fmt.Fprintf(&builder, "- Forbidden paths: %s\n", strings.Join(request.ForbiddenPaths, ", "))
	}
	if len(request.AllowedCommands) > 0 {
		fmt.Fprintf(&builder, "- Allowed commands: %s\n", strings.Join(request.AllowedCommands, ", "))
	}
	if len(request.AcceptanceCriteria) > 0 {
		builder.WriteString("- Acceptance criteria:\n")
		for _, criterion := range request.AcceptanceCriteria {
			fmt.Fprintf(&builder, "  - %s\n", criterion)
		}
	}
	if len(request.TestCommand) > 0 {
		fmt.Fprintf(&builder, "- Run this exact test argv: %q\n", request.TestCommand)
	}
	if request.PushBranch == nil || *request.PushBranch {
		builder.WriteString("- Commit the completed changes and push the working branch using the provided GitHub App credential.\n")
	}
	builder.WriteString("- Finish with a concise summary, changed files, commits, tests, and risks.\n")
	return builder.String()
}

func validateRepository(input dto.DevMachineRepoInput) error {
	repositoryURL, err := url.Parse(input.URL)
	if err != nil || repositoryURL.Scheme != "https" || !strings.EqualFold(repositoryURL.Hostname(), "github.com") || repositoryURL.Port() != "" || repositoryURL.User != nil || repositoryURL.RawQuery != "" || repositoryURL.Fragment != "" {
		return fmt.Errorf("repository URL must be an HTTPS github.com URL without credentials, query, or fragment")
	}
	path := strings.TrimSuffix(strings.Trim(repositoryURL.EscapedPath(), "/"), ".git")
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(parts) != 2 || !strings.EqualFold(parts[0], input.Owner) || !strings.EqualFold(parts[1], input.Name) {
		return fmt.Errorf("repository URL must match repo owner and name")
	}
	if !validGitHubOwner(input.Owner) || !validGitHubRepository(input.Name) {
		return fmt.Errorf("invalid GitHub repository owner or name")
	}
	return nil
}

func validGitHubOwner(value string) bool {
	if value == "" || len(value) > 100 || value[0] == '-' || strings.HasSuffix(value, "-") {
		return false
	}
	for _, character := range value {
		if (character < 'A' || character > 'Z') && (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' {
			return false
		}
	}
	return true
}

func validGitHubRepository(value string) bool {
	if value == "" || len(value) > 100 || value == "." || value == ".." || strings.HasSuffix(value, ".") {
		return false
	}
	for _, character := range value {
		if (character < 'A' || character > 'Z') && (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' && character != '_' && character != '.' {
			return false
		}
	}
	return true
}

func normalizeMachineName(value string) string {
	return strings.TrimSpace(value)
}

func validMachineName(value string) bool {
	if len(value) < 3 || len(value) > 255 || value[0] < 'a' || value[0] > 'z' || value[len(value)-1] == '-' {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' {
			return false
		}
	}
	return true
}

func validGitRef(value string) bool {
	if value == "" || strings.HasPrefix(value, "-") || strings.HasPrefix(value, ".") || strings.HasSuffix(value, "/") || strings.HasSuffix(value, ".") || strings.Contains(value, "..") || strings.Contains(value, "@{") || strings.Contains(value, "//") {
		return false
	}
	for _, character := range value {
		if character <= ' ' || strings.ContainsRune("~^:?*[\\", character) {
			return false
		}
	}
	for _, component := range strings.Split(value, "/") {
		if component == "" || component == "." || component == ".." || strings.HasSuffix(component, ".lock") {
			return false
		}
	}
	return true
}

func validWorkspacePathSegment(value string) bool {
	if value == "" || value == "." || value == ".." || strings.Contains(value, "/") || strings.Contains(value, "..") {
		return false
	}
	for _, character := range value {
		if (character < 'A' || character > 'Z') && (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '_' && character != '-' && character != '.' {
			return false
		}
	}
	return true
}

func validEnvName(value string) bool {
	if value == "" || !((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z') || value[0] == '_') {
		return false
	}
	for _, character := range value[1:] {
		if (character < 'A' || character > 'Z') && (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '_' {
			return false
		}
	}
	return true
}

func (s *DevMachineService) machineSecretValues(ctx context.Context, machineID uuid.UUID) ([]string, error) {
	envVars, err := s.store.ListEnvVarsInternal(ctx, machineID, nil, "")
	if err != nil {
		return nil, err
	}
	values := make([]string, 0, len(envVars))
	for _, envVar := range envVars {
		value, err := cryptoutil.Decrypt(envVar.EncryptedValue, s.encryptionKey)
		if err != nil {
			return nil, err
		}
		if value != "" {
			values = append(values, value)
		}
	}
	return values, nil
}

func redactPayload(value any, secrets []string) any {
	switch typed := value.(type) {
	case string:
		return redactText(typed, secrets)
	case []any:
		for index := range typed {
			typed[index] = redactPayload(typed[index], secrets)
		}
	case map[string]any:
		redacted := make(map[string]any, len(typed))
		for key, item := range typed {
			redacted[redactText(key, secrets)] = redactPayload(item, secrets)
		}
		return redacted
	}
	return value
}

func redactText(value string, secrets []string) string {
	for _, secret := range secrets {
		if secret != "" {
			value = strings.ReplaceAll(value, secret, "[REDACTED]")
		}
	}
	return value
}
