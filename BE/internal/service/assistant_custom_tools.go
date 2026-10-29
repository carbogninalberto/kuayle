package service

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
)

const assistantCustomToolDescriptionRunes = 1000

// ErrAssistantToolsDenied: guests and non-members cannot read custom tools.
var ErrAssistantToolsDenied = errors.New("assistant custom tools access denied")

// ErrInvalidAssistantTool carries a user-safe validation message.
type ErrInvalidAssistantTool struct{ Message string }

func (e *ErrInvalidAssistantTool) Error() string { return e.Message }

func invalidTool(message string) error { return &ErrInvalidAssistantTool{Message: message} }

type assistantCustomToolStore interface {
	List(ctx context.Context, ws uuid.UUID) ([]domain.AssistantCustomTool, error)
	Get(ctx context.Context, ws, id uuid.UUID) (*domain.AssistantCustomTool, error)
	Versions(ctx context.Context, ws, id uuid.UUID) ([]domain.AssistantCustomToolVersion, error)
	Version(ctx context.Context, ws, id uuid.UUID, version int) (*domain.AssistantCustomToolVersion, error)
	Create(ctx context.Context, ws, actor uuid.UUID, in domain.AssistantCustomToolInput) (*domain.AssistantCustomTool, error)
	Update(ctx context.Context, ws, id, actor uuid.UUID, in domain.AssistantCustomToolInput) (*domain.AssistantCustomTool, error)
	Restore(ctx context.Context, ws, id, actor uuid.UUID, version int) (*domain.AssistantCustomTool, error)
	Activate(ctx context.Context, ws, id, actor uuid.UUID, version *int) (*domain.AssistantCustomTool, error)
	SetEnabled(ctx context.Context, ws, id, actor uuid.UUID, enabled bool) (*domain.AssistantCustomTool, error)
	Delete(ctx context.Context, ws, id, actor uuid.UUID) error
	QueueRun(ctx context.Context, ws, actor uuid.UUID, req repository.ToolRunRequest) (*domain.AssistantToolRun, error)
	Run(ctx context.Context, ws, id, actor uuid.UUID) (*domain.AssistantToolRun, error)
	Runs(ctx context.Context, ws, tool, actor uuid.UUID, limit int) ([]domain.AssistantToolRun, error)
}

// toolRunTargets resolves an issue's newest coding task for test runs.
type toolRunTargets interface {
	FollowUpTarget(ctx context.Context, ws, issue uuid.UUID) (*repository.FollowUpTarget, error)
}

// AssistantCustomToolService serves Settings → Assistant → Custom tools.
// Non-guest members read tools and the runs they asked for; the repository
// lets only current owners and admins write tools and test-run them.
type AssistantCustomToolService struct {
	tools   assistantCustomToolStore
	members assistantMemberRepo
	issues  repository.IssueRepo
	targets toolRunTargets
}

func NewAssistantCustomToolService(tools assistantCustomToolStore, members assistantMemberRepo, issues repository.IssueRepo, targets toolRunTargets) *AssistantCustomToolService {
	return &AssistantCustomToolService{tools: tools, members: members, issues: issues, targets: targets}
}

// AssistantCustomToolRequest is a tool's content as the API receives it.
// Interpreter defaults to sh, run mode to approval and the timeout to ten
// minutes; Status is "draft" or "active".
type AssistantCustomToolRequest struct {
	Name           string          `json:"name"`
	Description    string          `json:"description"`
	Parameters     json.RawMessage `json:"parameters"`
	Script         string          `json:"script"`
	Interpreter    string          `json:"interpreter"`
	RunMode        string          `json:"run_mode"`
	ReadOnly       bool            `json:"read_only"`
	TimeoutSeconds int             `json:"timeout_seconds"`
	Status         string          `json:"status"`
}

// AssistantToolTestRunRequest runs a saved version (default: the newest, even
// a draft) on a machine the admin owns, or on the machine of an issue's newest
// coding task they created.
type AssistantToolTestRunRequest struct {
	Inputs     json.RawMessage `json:"inputs"`
	MachineID  *uuid.UUID      `json:"machine_id"`
	Identifier string          `json:"identifier"`
	Version    *int            `json:"version"`
}

type AssistantCustomToolList struct {
	Tools []domain.AssistantCustomTool `json:"tools"`
	// Interpreters the check container provides.
	Interpreters []string `json:"interpreters"`
	// CanManage: the caller is an owner or admin; the server enforces writes.
	CanManage bool `json:"can_manage"`
}

// builtInToolName reports a name the built-in registry uses.
func builtInToolName(name string) bool {
	if name == attachImagesToolName {
		return true
	}
	_, ok := lookupAssistantTool(name)
	return ok
}

// validateCustomTool trims, bounds and canonicalizes a tool's content. Names
// must not collide with a built-in tool, with or without the custom_ prefix
// the model sees.
func validateCustomTool(req AssistantCustomToolRequest) (domain.AssistantCustomToolInput, error) {
	name, description := strings.TrimSpace(req.Name), strings.TrimSpace(req.Description)
	if !domain.ValidCustomToolName(name) {
		return domain.AssistantCustomToolInput{}, invalidTool("name must be snake_case: lowercase letters, digits and underscores, starting with a letter, at most 48 characters")
	}
	if builtInToolName(name) || builtInToolName(domain.CustomToolPrefix+name) {
		return domain.AssistantCustomToolInput{}, invalidTool("name is taken by a built-in assistant tool")
	}
	if description == "" || utf8.RuneCountInString(description) > assistantCustomToolDescriptionRunes || strings.ContainsRune(description, 0) {
		return domain.AssistantCustomToolInput{}, invalidTool("description must be 1 to 1000 characters")
	}
	_, parameters, err := domain.ParseToolSchema(req.Parameters)
	var schemaErr *domain.ErrToolSchema
	if errors.As(err, &schemaErr) {
		return domain.AssistantCustomToolInput{}, invalidTool(schemaErr.Message)
	}
	if err != nil {
		return domain.AssistantCustomToolInput{}, err
	}
	if strings.TrimSpace(req.Script) == "" || len(req.Script) > domain.CustomToolScriptBytes || !utf8.ValidString(req.Script) || strings.ContainsRune(req.Script, 0) {
		return domain.AssistantCustomToolInput{}, invalidTool("script must be 1 byte to 64 KiB of text")
	}
	interpreter := req.Interpreter
	if interpreter == "" {
		interpreter = "sh"
	}
	if !slices.Contains(domain.CustomToolInterpreters, interpreter) {
		return domain.AssistantCustomToolInput{}, invalidTool("interpreter must be one of " + strings.Join(domain.CustomToolInterpreters, ", "))
	}
	mode := req.RunMode
	if mode == "" {
		mode = domain.AssistantToolRunApproval
	}
	if mode != domain.AssistantToolRunApproval && mode != domain.AssistantToolRunAuto {
		return domain.AssistantCustomToolInput{}, invalidTool("run_mode must be approval or auto")
	}
	if mode == domain.AssistantToolRunAuto && !req.ReadOnly {
		return domain.AssistantCustomToolInput{}, invalidTool("only a tool marked read-only can run without approval")
	}
	timeout := req.TimeoutSeconds
	if timeout == 0 {
		timeout = domain.CustomToolDefaultTimeout
	}
	if timeout < domain.CustomToolMinTimeout || timeout > domain.CustomToolMaxTimeout {
		return domain.AssistantCustomToolInput{}, invalidTool("timeout_seconds must be from 10 to 1800")
	}
	if req.Status != "" && req.Status != domain.AssistantSkillDraft && req.Status != domain.AssistantSkillActive {
		return domain.AssistantCustomToolInput{}, invalidTool("status must be draft or active")
	}
	return domain.AssistantCustomToolInput{Name: name, Description: description, Parameters: parameters, Script: req.Script, Interpreter: interpreter, RunMode: mode,
		ReadOnly: req.ReadOnly, TimeoutSeconds: timeout, Status: req.Status, AuthorKind: "user"}, nil
}

// access allows current non-guest members and reports whether the caller is
// an owner or admin. Writes re-check the role in the writing transaction.
func (s *AssistantCustomToolService) access(ctx context.Context, ws uuid.UUID) (uuid.UUID, bool, error) {
	access, ok := domain.AccessFromContext(ctx)
	if !ok || access.WorkspaceID != ws {
		return uuid.Nil, false, ErrAssistantToolsDenied
	}
	member, err := s.members.GetMember(ctx, ws, access.UserID)
	if err != nil {
		return uuid.Nil, false, err
	}
	if member == nil || member.Role == domain.RoleGuest {
		return uuid.Nil, false, ErrAssistantToolsDenied
	}
	return access.UserID, member.Role == domain.RoleOwner || member.Role == domain.RoleAdmin, nil
}

func (s *AssistantCustomToolService) List(ctx context.Context, ws uuid.UUID) (*AssistantCustomToolList, error) {
	_, manage, err := s.access(ctx, ws)
	if err != nil {
		return nil, err
	}
	tools, err := s.tools.List(ctx, ws)
	if err != nil {
		return nil, err
	}
	return &AssistantCustomToolList{Tools: tools, Interpreters: domain.CustomToolInterpreters, CanManage: manage}, nil
}

func (s *AssistantCustomToolService) Get(ctx context.Context, ws, id uuid.UUID) (*domain.AssistantCustomTool, error) {
	if _, _, err := s.access(ctx, ws); err != nil {
		return nil, err
	}
	return s.tools.Get(ctx, ws, id)
}

func (s *AssistantCustomToolService) Versions(ctx context.Context, ws, id uuid.UUID) ([]domain.AssistantCustomToolVersion, error) {
	if _, _, err := s.access(ctx, ws); err != nil {
		return nil, err
	}
	return s.tools.Versions(ctx, ws, id)
}

// Create adds a tool; it is active unless the request asks for a draft.
func (s *AssistantCustomToolService) Create(ctx context.Context, ws uuid.UUID, req AssistantCustomToolRequest) (*domain.AssistantCustomTool, error) {
	user, _, err := s.access(ctx, ws)
	if err != nil {
		return nil, err
	}
	if req.Status == "" {
		req.Status = domain.AssistantSkillActive
	}
	in, err := validateCustomTool(req)
	if err != nil {
		return nil, err
	}
	return s.tools.Create(ctx, ws, user, in)
}

// Update saves a new version. Without a status it keeps the tool's own; a
// draft edit of an active tool waits for activation.
func (s *AssistantCustomToolService) Update(ctx context.Context, ws, id uuid.UUID, req AssistantCustomToolRequest) (*domain.AssistantCustomTool, error) {
	user, _, err := s.access(ctx, ws)
	if err != nil {
		return nil, err
	}
	in, err := validateCustomTool(req)
	if err != nil {
		return nil, err
	}
	return s.tools.Update(ctx, ws, id, user, in)
}

func (s *AssistantCustomToolService) SetEnabled(ctx context.Context, ws, id uuid.UUID, enabled bool) (*domain.AssistantCustomTool, error) {
	user, _, err := s.access(ctx, ws)
	if err != nil {
		return nil, err
	}
	return s.tools.SetEnabled(ctx, ws, id, user, enabled)
}

// Activate puts a draft into effect; without a version, the newest draft.
func (s *AssistantCustomToolService) Activate(ctx context.Context, ws, id uuid.UUID, version *int) (*domain.AssistantCustomTool, error) {
	user, _, err := s.access(ctx, ws)
	if err != nil {
		return nil, err
	}
	return s.tools.Activate(ctx, ws, id, user, version)
}

// Restore saves an earlier version's content as a new version.
func (s *AssistantCustomToolService) Restore(ctx context.Context, ws, id uuid.UUID, version int) (*domain.AssistantCustomTool, error) {
	user, _, err := s.access(ctx, ws)
	if err != nil {
		return nil, err
	}
	return s.tools.Restore(ctx, ws, id, user, version)
}

func (s *AssistantCustomToolService) Delete(ctx context.Context, ws, id uuid.UUID) error {
	user, _, err := s.access(ctx, ws)
	if err != nil {
		return err
	}
	return s.tools.Delete(ctx, ws, id, user)
}

// TestRun queues an owner's or admin's test run of a saved version with
// sample inputs, validated against that version's schema.
func (s *AssistantCustomToolService) TestRun(ctx context.Context, ws, id uuid.UUID, req AssistantToolTestRunRequest) (*domain.AssistantToolRun, error) {
	user, manage, err := s.access(ctx, ws)
	if err != nil {
		return nil, err
	}
	if !manage {
		return nil, repository.ErrAssistantToolDenied
	}
	tool, err := s.tools.Get(ctx, ws, id)
	if err != nil {
		return nil, err
	}
	version := tool.LatestVersion
	if req.Version != nil {
		version = *req.Version
	}
	saved, err := s.tools.Version(ctx, ws, id, version)
	if err != nil {
		return nil, err
	}
	schema, _, err := domain.ParseToolSchema(saved.Parameters)
	if err != nil {
		return nil, err
	}
	_, inputs, err := schema.ValidateInputs(req.Inputs)
	var schemaErr *domain.ErrToolSchema
	if errors.As(err, &schemaErr) {
		return nil, invalidTool(schemaErr.Message)
	}
	if err != nil {
		return nil, err
	}
	run := repository.ToolRunRequest{ToolID: id, Version: version, Inputs: inputs, Test: true}
	identifier := strings.ToUpper(strings.TrimSpace(req.Identifier))
	switch {
	case req.MachineID != nil && identifier != "":
		return nil, invalidTool("give machine_id or identifier, not both")
	case req.MachineID != nil:
		run.MachineID = req.MachineID
	case identifier != "":
		issue, err := s.issues.GetByIdentifier(ctx, ws, identifier)
		if err != nil || issue == nil {
			return nil, repository.ErrAssistantToolNotFound
		}
		target, err := s.targets.FollowUpTarget(ctx, ws, issue.ID)
		if errors.Is(err, repository.ErrNoMergeTarget) {
			return nil, repository.ErrToolMachineUnavailable
		}
		if err != nil {
			return nil, err
		}
		run.TaskID, run.IssueID = &target.TaskID, &issue.ID
	default:
		return nil, invalidTool("give machine_id (one of your machines) or identifier (an issue whose newest coding task you created)")
	}
	return s.tools.QueueRun(ctx, ws, user, run)
}

// Runs lists a tool's runs: all of them for owners and admins, the caller's
// own for members.
func (s *AssistantCustomToolService) Runs(ctx context.Context, ws, id uuid.UUID) ([]domain.AssistantToolRun, error) {
	user, _, err := s.access(ctx, ws)
	if err != nil {
		return nil, err
	}
	return s.tools.Runs(ctx, ws, id, user, 50)
}

func (s *AssistantCustomToolService) Run(ctx context.Context, ws, id uuid.UUID) (*domain.AssistantToolRun, error) {
	user, _, err := s.access(ctx, ws)
	if err != nil {
		return nil, err
	}
	return s.tools.Run(ctx, ws, id, user)
}
