package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
)

// WorkflowService serves Settings → Agents (profiles) and Settings →
// Workflows, and the task's workflow view. Non-guest members read and choose
// workflows; the repository lets only current owners and admins write, and
// audits every change.
type WorkflowService struct {
	repo   *repository.WorkflowRepository
	agents *agent.Registry
	tasks  *AgentTaskService
}

func NewWorkflowService(repo *repository.WorkflowRepository, agents *agent.Registry, tasks *AgentTaskService) *WorkflowService {
	return &WorkflowService{repo: repo, agents: agents, tasks: tasks}
}

// ErrWorkflowsDenied: guests and non-members cannot read workflows.
var ErrWorkflowsDenied = errors.New("workflows access denied")

// ErrInvalidWorkflowInput carries a user-safe validation message.
type ErrInvalidWorkflowInput struct{ Message string }

func (e *ErrInvalidWorkflowInput) Error() string { return e.Message }

func (s *WorkflowService) access(ctx context.Context, ws uuid.UUID) (uuid.UUID, bool, error) {
	access, ok := domain.AccessFromContext(ctx)
	if !ok || access.WorkspaceID != ws || access.UserID == uuid.Nil {
		return uuid.Nil, false, ErrWorkflowsDenied
	}
	role, err := s.repo.MemberRole(ctx, ws, access.UserID)
	if err != nil {
		return uuid.Nil, false, err
	}
	if role == "" || role == domain.RoleGuest {
		return uuid.Nil, false, ErrWorkflowsDenied
	}
	return access.UserID, role == domain.RoleOwner || role == domain.RoleAdmin, nil
}

// AgentProfileList is Settings → Agents → Profiles.
type AgentProfileList struct {
	Profiles  []domain.AgentProfile `json:"profiles"`
	CanManage bool                  `json:"can_manage"`
	Harnesses []string              `json:"harnesses"`
	// ModelHarnesses are the harnesses that take a model and an effort; a
	// profile for any other harness leaves both empty.
	ModelHarnesses []string `json:"model_harnesses"`
	// ModelFormats says how a harness writes its model names when it is not
	// a bare name (OpenCode: provider/model), with an example for the form.
	ModelFormats map[string]HarnessModelFormat `json:"model_formats"`
	Roles        []string                      `json:"roles"`
}

func (s *WorkflowService) harnesses() []string {
	out := []string{}
	if s.agents == nil {
		return out
	}
	for _, provider := range s.agents.List() {
		meta := provider.Metadata()
		for _, mode := range meta.SupportedModes {
			if mode == agent.ModeAutonomous && !meta.Custom {
				out = append(out, meta.ID)
				break
			}
		}
	}
	return out
}

// modelHarnesses lists the harnesses a profile can set a model and effort for.
func (s *WorkflowService) modelHarnesses() []string {
	out := []string{}
	for _, harness := range s.harnesses() {
		if harnessAcceptsModel(harness) {
			out = append(out, harness)
		}
	}
	return out
}

// modelFormats is the model name form of each harness that has one.
func (s *WorkflowService) modelFormats() map[string]HarnessModelFormat {
	out := map[string]HarnessModelFormat{}
	for _, harness := range s.modelHarnesses() {
		if format, ok := harnessModelFormats[harness]; ok {
			out[harness] = format
		}
	}
	return out
}

func (s *WorkflowService) Profiles(ctx context.Context, ws uuid.UUID) (*AgentProfileList, error) {
	_, canManage, err := s.access(ctx, ws)
	if err != nil {
		return nil, err
	}
	if err = s.repo.EnsureBuiltinProfiles(ctx, ws); err != nil {
		return nil, err
	}
	profiles, err := s.repo.Profiles(ctx, ws)
	if err != nil {
		return nil, err
	}
	return &AgentProfileList{Profiles: profiles, CanManage: canManage, Harnesses: s.harnesses(), ModelHarnesses: s.modelHarnesses(), ModelFormats: s.modelFormats(),
		Roles: []string{domain.AgentRoleImplementer, domain.AgentRoleReviewer, domain.AgentRoleTester, domain.AgentRoleDocsWriter, domain.AgentRoleSecurityReviewer}}, nil
}

// AgentProfileRequest is a profile as the API receives it. Unknown fields
// (for example repository, sudo or Docker settings) are refused: a profile
// can never carry permissions.
type AgentProfileRequest struct {
	Name         string `json:"name"`
	Role         string `json:"role"`
	Harness      string `json:"harness"`
	Model        string `json:"model"`
	Effort       string `json:"effort"`
	Instructions string `json:"instructions"`
	// SkillIDs replaces the profile's skills; omitted keeps them.
	SkillIDs []uuid.UUID `json:"skill_ids,omitempty"`
}

func singleLine(value string) bool { return !strings.ContainsFunc(value, unicode.IsControl) }

// validateProfile checks a profile as it is saved. Its coding agent, model and
// effort must be able to start a task (profileProblem), so a profile that
// could never run is refused here instead of failing every task later.
// Profiles stored before that rule keep loading; it applies when they are
// saved again.
func (s *WorkflowService) validateProfile(ctx context.Context, ws uuid.UUID, in AgentProfileRequest) (domain.AgentProfileInput, error) {
	out := domain.AgentProfileInput{Name: strings.TrimSpace(in.Name), Role: in.Role, Harness: in.Harness, Model: strings.TrimSpace(in.Model), Effort: strings.TrimSpace(in.Effort), Instructions: strings.TrimSpace(in.Instructions), SkillIDs: in.SkillIDs}
	seen := map[uuid.UUID]bool{}
	for _, id := range in.SkillIDs {
		if seen[id] {
			return out, &ErrInvalidWorkflowInput{"a skill is listed twice"}
		}
		seen[id] = true
	}
	known := false
	for _, harness := range s.harnesses() {
		known = known || harness == out.Harness
	}
	switch {
	case out.Name == "" || utf8.RuneCountInString(out.Name) > 80 || !singleLine(out.Name):
		return out, &ErrInvalidWorkflowInput{"the name is 1 to 80 characters on one line"}
	case !domain.ValidAgentRole(out.Role):
		return out, &ErrInvalidWorkflowInput{"choose a role"}
	case !known:
		return out, &ErrInvalidWorkflowInput{"choose a coding agent that runs on its own"}
	case utf8.RuneCountInString(out.Model) > 128 || !singleLine(out.Model):
		return out, &ErrInvalidWorkflowInput{"the model is at most 128 characters"}
	case utf8.RuneCountInString(out.Effort) > 32 || !singleLine(out.Effort):
		return out, &ErrInvalidWorkflowInput{"the effort is at most 32 characters"}
	case len(out.SkillIDs) > domain.AgentProfileMaxSkills:
		return out, &ErrInvalidWorkflowInput{"a profile has at most 20 skills"}
	case len(out.Instructions) > domain.AgentProfileInstructionsMaxBytes || !utf8.ValidString(out.Instructions):
		return out, &ErrInvalidWorkflowInput{"the instructions are at most 64 KB"}
	}
	if s.tasks != nil {
		reason, err := s.tasks.profileProblem(ctx, ws, domain.AgentProfile{Harness: out.Harness, Model: out.Model, Effort: out.Effort})
		if err != nil {
			return out, err
		}
		if reason != "" {
			return out, &ErrInvalidWorkflowInput{reason}
		}
	}
	return out, nil
}

func (s *WorkflowService) CreateProfile(ctx context.Context, ws uuid.UUID, in AgentProfileRequest) (*domain.AgentProfile, error) {
	actor, _, err := s.access(ctx, ws)
	if err != nil {
		return nil, err
	}
	input, err := s.validateProfile(ctx, ws, in)
	if err != nil {
		return nil, err
	}
	return s.repo.CreateProfile(ctx, ws, actor, input)
}

func (s *WorkflowService) UpdateProfile(ctx context.Context, ws, id uuid.UUID, in AgentProfileRequest) (*domain.AgentProfile, error) {
	actor, _, err := s.access(ctx, ws)
	if err != nil {
		return nil, err
	}
	input, err := s.validateProfile(ctx, ws, in)
	if err != nil {
		return nil, err
	}
	return s.repo.UpdateProfile(ctx, ws, id, actor, input)
}

func (s *WorkflowService) ArchiveProfile(ctx context.Context, ws, id uuid.UUID) error {
	actor, _, err := s.access(ctx, ws)
	if err != nil {
		return err
	}
	return s.repo.ArchiveProfile(ctx, ws, id, actor)
}

// WorkflowSummary is one entry of Settings → Workflows. The Standard
// workflow is listed first with ID "standard": the built-in flow until the
// workspace edits it (Customized), then the workspace's stored copy.
type WorkflowSummary struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Description   string          `json:"description"`
	Builtin       bool            `json:"builtin"`
	Customized    bool            `json:"customized"`
	LatestVersion int             `json:"latest_version"`
	VersionID     *uuid.UUID      `json:"version_id,omitempty"`
	Archived      bool            `json:"archived"`
	Graph         json.RawMessage `json:"graph"`
	UpdatedAt     *string         `json:"updated_at,omitempty"`
}

type WorkflowList struct {
	Workflows []WorkflowSummary        `json:"workflows"`
	Defaults  []domain.WorkflowDefault `json:"defaults"`
	CanManage bool                     `json:"can_manage"`
}

// standardSummary is the built-in Standard workflow. Its version is 0 until
// the workspace edits it, so a save from a stale editor can't overwrite an
// edit someone made meanwhile.
func standardSummary() WorkflowSummary {
	graph, _ := json.Marshal(domain.StandardWorkflowGraph())
	return WorkflowSummary{ID: domain.StandardWorkflowID, Name: "Standard", Description: "Implement, validate, publish a pull request and wait for human review, with follow-ups on CI failures, change requests and comments.", Builtin: true, Graph: graph}
}

func summaryOf(w repository.WorkflowDetail) WorkflowSummary {
	updated := w.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z")
	out := WorkflowSummary{ID: w.ID.String(), Name: w.Name, Description: w.Description, LatestVersion: w.LatestVersion, VersionID: w.VersionID, Archived: w.ArchivedAt != nil, Graph: w.Graph, UpdatedAt: &updated}
	if w.BuiltinKey != nil {
		out.ID, out.Name, out.Builtin, out.Customized = domain.StandardWorkflowID, "Standard", true, true
	}
	return out
}

// standardView is the Standard workflow as the workspace runs it: its edited
// copy, else the built-in flow. For the editor, the built-in graph is the
// editable copy (the workspace's implementer on the implement step), so a
// first save is valid as it is.
func (s *WorkflowService) standardView(ctx context.Context, ws uuid.UUID, editable bool) (*WorkflowView, error) {
	override, err := s.repo.StandardOverride(ctx, ws)
	if err != nil {
		return nil, err
	}
	if override != nil {
		versions, err := s.repo.Versions(ctx, ws, override.ID)
		if err != nil {
			return nil, err
		}
		return &WorkflowView{WorkflowSummary: summaryOf(*override), Versions: versions}, nil
	}
	view := &WorkflowView{WorkflowSummary: standardSummary(), Versions: []domain.WorkflowVersion{}}
	if editable {
		implementer, _, err := s.builtinProfileIDs(ctx, ws)
		if err != nil {
			return nil, err
		}
		if view.Graph, err = json.Marshal(standardCopy(implementer)); err != nil {
			return nil, err
		}
	}
	return view, nil
}

func (s *WorkflowService) List(ctx context.Context, ws uuid.UUID) (*WorkflowList, error) {
	_, canManage, err := s.access(ctx, ws)
	if err != nil {
		return nil, err
	}
	workflows, err := s.repo.Workflows(ctx, ws)
	if err != nil {
		return nil, err
	}
	defaults, err := s.repo.Defaults(ctx, ws)
	if err != nil {
		return nil, err
	}
	standard, err := s.standardView(ctx, ws, false)
	if err != nil {
		return nil, err
	}
	out := &WorkflowList{Workflows: []WorkflowSummary{standard.WorkflowSummary}, Defaults: defaults, CanManage: canManage}
	for _, w := range workflows {
		out.Workflows = append(out.Workflows, summaryOf(w))
	}
	return out, nil
}

// WorkflowView is one workflow with its versions.
type WorkflowView struct {
	WorkflowSummary
	Versions []domain.WorkflowVersion `json:"versions"`
}

func (s *WorkflowService) Get(ctx context.Context, ws uuid.UUID, id string) (*WorkflowView, error) {
	_, canManage, err := s.access(ctx, ws)
	if err != nil {
		return nil, err
	}
	if id == domain.StandardWorkflowID {
		return s.standardView(ctx, ws, canManage)
	}
	workflowID, err := uuid.Parse(id)
	if err != nil {
		return nil, repository.ErrWorkflowNotFound
	}
	w, err := s.repo.Workflow(ctx, ws, workflowID)
	if err != nil {
		return nil, err
	}
	versions, err := s.repo.Versions(ctx, ws, workflowID)
	if err != nil {
		return nil, err
	}
	return &WorkflowView{WorkflowSummary: summaryOf(*w), Versions: versions}, nil
}

// WorkflowRequest creates or saves a workflow. Template names a built-in
// starting point ("review_loop", "full_cycle" or "standard") instead of a
// graph.
type WorkflowRequest struct {
	Name            string          `json:"name"`
	Description     string          `json:"description"`
	Graph           json.RawMessage `json:"graph,omitempty"`
	Template        string          `json:"template,omitempty"`
	ExpectedVersion int             `json:"expected_version,omitempty"`
}

func validateWorkflowMeta(name, description string) (string, string, error) {
	name, description = strings.TrimSpace(name), strings.TrimSpace(description)
	if name == "" || utf8.RuneCountInString(name) > 80 || !singleLine(name) {
		return "", "", &ErrInvalidWorkflowInput{"the name is 1 to 80 characters on one line"}
	}
	if utf8.RuneCountInString(description) > 500 {
		return "", "", &ErrInvalidWorkflowInput{"the description is at most 500 characters"}
	}
	return name, description, nil
}

// builtinProfileIDs returns the workspace's built-in implementer and
// reviewer profiles, creating them if needed.
func (s *WorkflowService) builtinProfileIDs(ctx context.Context, ws uuid.UUID) (uuid.UUID, uuid.UUID, error) {
	if err := s.repo.EnsureBuiltinProfiles(ctx, ws); err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	profiles, err := s.repo.Profiles(ctx, ws)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	var implementer, reviewer uuid.UUID
	for _, p := range profiles {
		if p.ArchivedAt != nil {
			continue
		}
		if p.BuiltinKey != nil && *p.BuiltinKey == domain.AgentRoleImplementer || implementer == uuid.Nil && p.Role == domain.AgentRoleImplementer {
			implementer = p.ID
		}
		if p.BuiltinKey != nil && *p.BuiltinKey == domain.AgentRoleReviewer || reviewer == uuid.Nil && p.Role == domain.AgentRoleReviewer {
			reviewer = p.ID
		}
	}
	if implementer == uuid.Nil || reviewer == uuid.Nil {
		return uuid.Nil, uuid.Nil, &ErrInvalidWorkflowInput{"create an implementer and a reviewer profile first"}
	}
	return implementer, reviewer, nil
}

// standardCopy is an editable copy of the Standard flow: the built-in
// implementer, the configured check, a pull request and a human review that
// can send the work back (at most five times, like today's follow-ups).
func standardCopy(implementer uuid.UUID) domain.WorkflowGraph {
	graph := domain.StandardWorkflowGraph()
	for i := range graph.Nodes {
		switch graph.Nodes[i].ID {
		case "implement":
			graph.Nodes[i].ProfileID = &implementer
			graph.Nodes[i].Prompt = "{{review_comments}}\n\n{{failing_checks}}"
		case "review":
			graph.Nodes[i].MaxVisits = 6
			graph.Nodes[i].Prompt = "Review the pull request"
		}
	}
	graph.Edges = append(graph.Edges, domain.WorkflowEdge{ID: "e-publish-failed", Source: "publish", Target: "failed", Condition: domain.WorkflowPublishFailed})
	return graph
}

func (s *WorkflowService) Create(ctx context.Context, ws uuid.UUID, in WorkflowRequest) (*WorkflowSummary, error) {
	actor, canManage, err := s.access(ctx, ws)
	if err != nil {
		return nil, err
	}
	if !canManage {
		return nil, repository.ErrWorkflowDenied
	}
	name, description, err := validateWorkflowMeta(in.Name, in.Description)
	if err != nil {
		return nil, err
	}
	graph := in.Graph
	if in.Template != "" {
		implementer, reviewer, err := s.builtinProfileIDs(ctx, ws)
		if err != nil {
			return nil, err
		}
		var template domain.WorkflowGraph
		switch in.Template {
		case "review_loop":
			template = domain.ReviewLoopWorkflowGraph(implementer, reviewer)
		case "full_cycle":
			template = domain.FullCycleWorkflowGraph(implementer, reviewer)
		case domain.StandardWorkflowID:
			template = standardCopy(implementer)
		default:
			return nil, &ErrInvalidWorkflowInput{"unknown template"}
		}
		if graph, err = json.Marshal(template); err != nil {
			return nil, err
		}
	}
	if err = s.requireStartable(ctx, ws, graph); err != nil {
		return nil, err
	}
	created, err := s.repo.CreateWorkflow(ctx, ws, actor, name, description, graph, "created")
	if err != nil {
		return nil, err
	}
	summary := summaryOf(*created)
	return &summary, nil
}

func (s *WorkflowService) Save(ctx context.Context, ws uuid.UUID, id string, in WorkflowRequest) (*WorkflowSummary, error) {
	actor, canManage, err := s.access(ctx, ws)
	if err != nil {
		return nil, err
	}
	if !canManage {
		return nil, repository.ErrWorkflowDenied
	}
	if id == domain.StandardWorkflowID {
		return s.saveStandard(ctx, ws, actor, in)
	}
	workflowID, err := uuid.Parse(id)
	if err != nil {
		return nil, repository.ErrWorkflowNotFound
	}
	name, description, err := validateWorkflowMeta(in.Name, in.Description)
	if err != nil {
		return nil, err
	}
	if err = s.requireStartable(ctx, ws, in.Graph); err != nil {
		return nil, err
	}
	saved, err := s.repo.SaveVersion(ctx, ws, workflowID, actor, in.ExpectedVersion, name, description, in.Graph)
	if err != nil {
		return nil, err
	}
	summary := summaryOf(*saved)
	return &summary, nil
}

// saveStandard stores an edit of the Standard workflow: the first edit
// creates the workspace's copy (expected version 0), later ones add versions.
// Its name stays "Standard".
func (s *WorkflowService) saveStandard(ctx context.Context, ws, actor uuid.UUID, in WorkflowRequest) (*WorkflowSummary, error) {
	_, description, err := validateWorkflowMeta("Standard", in.Description)
	if err != nil {
		return nil, err
	}
	if err = s.requireStartable(ctx, ws, in.Graph); err != nil {
		return nil, err
	}
	override, err := s.repo.StandardOverride(ctx, ws)
	if err != nil {
		return nil, err
	}
	var saved *repository.WorkflowDetail
	switch {
	case override == nil && in.ExpectedVersion != 0, override != nil && in.ExpectedVersion == 0:
		return nil, repository.ErrWorkflowStale
	case override == nil:
		saved, err = s.repo.CreateStandardOverride(ctx, ws, actor, description, in.Graph)
	default:
		saved, err = s.repo.SaveVersion(ctx, ws, override.ID, actor, in.ExpectedVersion, override.Name, description, in.Graph)
	}
	if err != nil {
		return nil, err
	}
	summary := summaryOf(*saved)
	return &summary, nil
}

// Duplicate copies a workflow's latest version (or the Standard flow) into
// a new workflow.
func (s *WorkflowService) Duplicate(ctx context.Context, ws uuid.UUID, id, name string) (*WorkflowSummary, error) {
	actor, canManage, err := s.access(ctx, ws)
	if err != nil {
		return nil, err
	}
	if !canManage {
		return nil, repository.ErrWorkflowDenied
	}
	var graph json.RawMessage
	var description, source string
	if id == domain.StandardWorkflowID {
		standard, err := s.standardView(ctx, ws, true)
		if err != nil {
			return nil, err
		}
		graph, source, description = standard.Graph, "Standard", standard.Description
	} else {
		workflowID, err := uuid.Parse(id)
		if err != nil {
			return nil, repository.ErrWorkflowNotFound
		}
		w, err := s.repo.Workflow(ctx, ws, workflowID)
		if err != nil {
			return nil, err
		}
		graph, source, description = w.Graph, w.Name, w.Description
	}
	if strings.TrimSpace(name) == "" {
		name = "Copy of " + source
		if utf8.RuneCountInString(name) > 80 {
			name = string([]rune(name)[:80])
		}
	}
	name, description, err = validateWorkflowMeta(name, description)
	if err != nil {
		return nil, err
	}
	created, err := s.repo.CreateWorkflow(ctx, ws, actor, name, description, graph, "duplicated")
	if err != nil {
		return nil, err
	}
	summary := summaryOf(*created)
	return &summary, nil
}

func (s *WorkflowService) Archive(ctx context.Context, ws uuid.UUID, id string) error {
	actor, _, err := s.access(ctx, ws)
	if err != nil {
		return err
	}
	workflowID, err := uuid.Parse(id)
	if err != nil {
		return repository.ErrWorkflowDenied
	}
	return s.repo.ArchiveWorkflow(ctx, ws, workflowID, actor)
}

// Delete removes a workflow for good. Deleting "standard" discards the
// workspace's edits and restores the built-in Standard workflow.
func (s *WorkflowService) Delete(ctx context.Context, ws uuid.UUID, id string) error {
	actor, _, err := s.access(ctx, ws)
	if err != nil {
		return err
	}
	if id == domain.StandardWorkflowID {
		override, err := s.repo.StandardOverride(ctx, ws)
		if err != nil {
			return err
		}
		if override == nil {
			return repository.ErrWorkflowNotFound
		}
		return s.repo.DeleteWorkflow(ctx, ws, override.ID, actor)
	}
	workflowID, err := uuid.Parse(id)
	if err != nil {
		return repository.ErrWorkflowNotFound
	}
	return s.repo.DeleteWorkflow(ctx, ws, workflowID, actor)
}

// WorkflowDefaultRequest sets a scope's workflow: "standard", a workflow ID,
// or for teams and projects null to inherit.
type WorkflowDefaultRequest struct {
	Scope      string    `json:"scope"`
	ScopeID    uuid.UUID `json:"scope_id"`
	WorkflowID *string   `json:"workflow_id"`
}

func (s *WorkflowService) SetDefault(ctx context.Context, ws uuid.UUID, in WorkflowDefaultRequest) (*WorkflowList, error) {
	actor, canManage, err := s.access(ctx, ws)
	if err != nil {
		return nil, err
	}
	if !canManage {
		return nil, repository.ErrWorkflowDenied
	}
	switch in.Scope {
	case domain.WorkflowScopeWorkspace, domain.WorkflowScopeTeam, domain.WorkflowScopeProject:
	default:
		return nil, &ErrInvalidWorkflowInput{"the scope is the workspace, a team or a project"}
	}
	if in.Scope == domain.WorkflowScopeWorkspace {
		in.ScopeID = ws
	}
	inherit := in.WorkflowID == nil
	if inherit && in.Scope == domain.WorkflowScopeWorkspace {
		return nil, &ErrInvalidWorkflowInput{"the workspace always has a default; choose Standard to use the built-in flow"}
	}
	var workflow *uuid.UUID
	if !inherit && *in.WorkflowID != domain.StandardWorkflowID {
		id, err := uuid.Parse(*in.WorkflowID)
		if err != nil {
			return nil, repository.ErrWorkflowNotFound
		}
		workflow = &id
	}
	// A default starts every task of its scope, so one that cannot start is
	// refused with the same problems the editor shows.
	if !inherit {
		var chosen *repository.WorkflowDetail
		if workflow != nil {
			chosen, err = s.repo.Workflow(ctx, ws, *workflow)
		} else {
			chosen, err = s.repo.StandardOverride(ctx, ws)
		}
		if err != nil {
			return nil, err
		}
		if chosen != nil {
			if err = s.requireStartable(ctx, ws, chosen.Graph); err != nil {
				return nil, err
			}
		}
	}
	if err = s.repo.SetDefault(ctx, ws, actor, in.Scope, in.ScopeID, workflow, inherit); err != nil {
		return nil, err
	}
	return s.List(ctx, ws)
}

// Validate checks a graph without saving it, for the editor.
func (s *WorkflowService) Validate(ctx context.Context, ws uuid.UUID, raw json.RawMessage) ([]domain.WorkflowIssue, error) {
	if _, _, err := s.access(ctx, ws); err != nil {
		return nil, err
	}
	return s.graphIssues(ctx, ws, raw)
}

// graphIssues is every problem of a graph: its structure
// (domain.ValidateWorkflowGraph) and each agent step whose profile cannot
// start a task, reported on that step with the step and profile named.
func (s *WorkflowService) graphIssues(ctx context.Context, ws uuid.UUID, raw json.RawMessage) ([]domain.WorkflowIssue, error) {
	graph, err := domain.ParseWorkflowGraph(raw)
	if err != nil {
		var invalid *domain.WorkflowInvalidError
		if errors.As(err, &invalid) {
			return invalid.Issues, nil
		}
		return nil, err
	}
	profiles, err := s.repo.ActiveProfiles(ctx, ws)
	if err != nil {
		return nil, err
	}
	issues := domain.ValidateWorkflowGraph(*graph, profiles)
	if s.tasks == nil {
		return issues, nil
	}
	reasons := map[uuid.UUID]string{}
	for _, node := range graph.Nodes {
		if node.Type != domain.WorkflowNodeAgent || node.ProfileID == nil {
			continue
		}
		profile, ok := profiles[*node.ProfileID]
		if !ok {
			continue
		}
		reason, checked := reasons[profile.ID]
		if !checked {
			if reason, err = s.tasks.profileProblem(ctx, ws, profile); err != nil {
				return nil, err
			}
			reasons[profile.ID] = reason
		}
		if reason != "" {
			issues = append(issues, domain.WorkflowIssue{Code: "unusable_profile", NodeID: node.ID,
				Message: fmt.Sprintf("%s: the profile %q cannot start a task: %s", strings.TrimSpace(node.Label), profile.Name, reason)})
		}
	}
	return issues, nil
}

// requireStartable refuses a graph that has problems, with all of them, as a
// save does. The repository validates the structure again in the saving
// transaction; the profile checks need the task service, so they run here.
func (s *WorkflowService) requireStartable(ctx context.Context, ws uuid.UUID, raw json.RawMessage) error {
	issues, err := s.graphIssues(ctx, ws, raw)
	if err != nil {
		return err
	}
	if len(issues) > 0 {
		return &domain.WorkflowInvalidError{Issues: issues}
	}
	return nil
}

func (s *WorkflowService) Audit(ctx context.Context, ws uuid.UUID, entity string, id uuid.UUID) ([]repository.WorkflowAuditEvent, error) {
	_, canManage, err := s.access(ctx, ws)
	if err != nil {
		return nil, err
	}
	if !canManage {
		return nil, repository.ErrWorkflowDenied
	}
	return s.repo.AuditEvents(ctx, ws, entity, id)
}

// TaskWorkflow is the task's workflow graph and the path it took, for the
// task's Agents panel. A task without a workflow version shows the Standard
// graph with its state placed on it.
type TaskWorkflow struct {
	WorkflowID     string                `json:"workflow_id"`
	Name           string                `json:"name"`
	Version        int                   `json:"version"`
	Builtin        bool                  `json:"builtin"`
	Graph          json.RawMessage       `json:"graph"`
	Node           string                `json:"node"`
	Status         string                `json:"status"`
	Visits         map[string]int        `json:"visits"`
	Iterations     int                   `json:"iterations"`
	Sequence       int                   `json:"sequence"`
	Steps          []domain.WorkflowStep `json:"steps"`
	Profiles       []domain.AgentProfile `json:"profiles"`
	CanDecide      bool                  `json:"can_decide"`
	TaskState      string                `json:"task_state"`
	TaskReason     string                `json:"task_reason"`
	TaskGeneration int64                 `json:"task_generation"`
}

func (s *WorkflowService) TaskWorkflow(ctx context.Context, ws, id uuid.UUID) (*TaskWorkflow, error) {
	task, err := s.tasks.Repo.Get(ctx, ws, id)
	if err != nil {
		return nil, err
	}
	out := &TaskWorkflow{TaskState: task.State, TaskReason: task.Reason, TaskGeneration: task.Generation, Steps: []domain.WorkflowStep{}, Profiles: []domain.AgentProfile{}, Visits: map[string]int{}}
	if !task.RunsWorkflow() {
		standard := standardSummary()
		out.WorkflowID, out.Name, out.Version, out.Builtin, out.Graph = standard.ID, standard.Name, 1, true, standard.Graph
		out.Node, out.Status = domain.StandardWorkflowNode(task.State), task.State
		out.Iterations = int(task.Generation)
		return out, nil
	}
	version, err := s.repo.Version(ctx, ws, *task.WorkflowVersionID)
	if err != nil {
		return nil, err
	}
	w, err := s.repo.Workflow(ctx, ws, version.WorkflowID)
	if err != nil {
		return nil, err
	}
	cursor := task.Cursor()
	out.WorkflowID, out.Name, out.Version, out.Graph = w.ID.String(), w.Name, version.Version, version.Graph
	if w.BuiltinKey != nil {
		out.WorkflowID, out.Builtin = domain.StandardWorkflowID, true
	}
	out.Node, out.Visits, out.Iterations, out.Sequence = cursor.Node, cursor.Visits, cursor.Iterations, task.WorkflowSequence
	if task.WorkflowStatus != nil {
		out.Status = *task.WorkflowStatus
	}
	if out.Steps, err = s.tasks.Repo.WorkflowSteps(ctx, ws, id); err != nil {
		return nil, err
	}
	profiles, err := s.repo.Profiles(ctx, ws)
	if err != nil {
		return nil, err
	}
	out.Profiles = profiles
	if access, ok := domain.AccessFromContext(ctx); ok {
		role, err := s.repo.MemberRole(ctx, ws, access.UserID)
		if err != nil {
			return nil, err
		}
		out.CanDecide = workflowDecider(out.Status, access.UserID, task.CreatorID, role)
	}
	return out, nil
}

// workflowDecider mirrors who the repository lets decide (the task's creator
// or a workspace owner or admin who can see it), so the UI offers decisions
// only to them; the decision itself is re-checked when it is made.
func workflowDecider(status string, actor, creator uuid.UUID, role string) bool {
	if status != domain.WorkflowStatusPaused && status != domain.WorkflowStatusWaiting {
		return false
	}
	return actor == creator || role == domain.RoleOwner || role == domain.RoleAdmin
}

// Decide applies a person's decision on a task's workflow.
func (s *WorkflowService) Decide(ctx context.Context, ws, id uuid.UUID, sequence int, decision string) (*domain.AgentTask, error) {
	if s.tasks.Workflow() == nil {
		return nil, ErrWorkflowDecision
	}
	return s.tasks.Workflow().Decide(ctx, ws, id, sequence, decision)
}
