package service

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/realtime"
	"github.com/kuayle/kuayle-backend/internal/repository"
	log "github.com/sirupsen/logrus"
)

// agentAssigneeStore is the persistence the assignee service needs.
type agentAssigneeStore interface {
	Identities(ctx context.Context, ws uuid.UUID) ([]domain.AgentIdentity, error)
	UpdateIdentity(ctx context.Context, ws, profile, actor uuid.UUID, avatarColor *string, teamIDs *[]uuid.UUID) error
	ForIssues(ctx context.Context, issueIDs []uuid.UUID) (map[uuid.UUID][]domain.IssueAgentAssignee, error)
	Set(ctx context.Context, ws, issue, actor uuid.UUID, profiles []uuid.UUID) (added, removed []repository.AgentAssignment, err error)
	Assignable(ctx context.Context, ws, issue, profile uuid.UUID) (*domain.AgentProfile, error)
	Pending(ctx context.Context, limit int) ([]repository.AgentAssignment, error)
	ExpirePending(ctx context.Context) ([]repository.AgentAssignment, error)
	Finish(ctx context.Context, assignment repository.AgentAssignment, task *uuid.UUID, failure string) (bool, error)
	MentionedAgents(ctx context.Context, ws, issue, actor uuid.UUID, ids []uuid.UUID) ([]domain.AgentProfile, error)
	OpenAgentTask(ctx context.Context, ws, issue, profile uuid.UUID) (*domain.AgentTask, error)
	AgentNames(ctx context.Context, ws uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]string, error)
}

// agentIssueChats opens the acting member's chat on an issue, which checks
// their membership (never a guest) and the issue's team visibility.
type agentIssueChats interface {
	OpenIssue(ctx context.Context, ws, issue uuid.UUID) (*domain.Conversation, error)
}

// agentTaskStarter is the task service, as assignments and mentions use it.
type agentTaskStarter interface {
	Create(ctx context.Context, ws, conversation uuid.UUID, input domain.AgentTaskRequest) (*domain.AgentTask, error)
}

type agentTaskCanceller interface {
	CancelUnassigned(ctx context.Context, ws, id, issue, profile, actor uuid.UUID) (bool, error)
}

type agentLiveMessenger interface {
	QueueLiveMessage(ctx context.Context, ws, id uuid.UUID, sender, body string) (*domain.AgentLiveMessage, error)
}

type agentProfileSeeder interface {
	EnsureBuiltinProfiles(ctx context.Context, ws uuid.UUID) error
}

// AgentAssigneeService makes agent profiles first-class members: they are
// assigned to issues and mentioned in comments. It grants nothing. Every
// task an agent runs is created through the same path as chat delegation,
// in the acting member's own issue chat, so it runs with that member's
// current permissions and never more; the per-team policy only narrows where
// members may put an agent to work.
type AgentAssigneeService struct {
	repo   agentAssigneeStore
	chats  agentIssueChats
	tasks  agentTaskStarter
	cancel agentTaskCanceller
	live   agentLiveMessenger
	hub    taskEventPublisher
	// policy reports why a profile cannot run under the workspace's machine
	// policy now; nil skips the check in listings.
	policy workflowPolicy
	seeder agentProfileSeeder
}

func NewAgentAssigneeService(repo agentAssigneeStore, chats agentIssueChats, tasks agentTaskStarter, cancel agentTaskCanceller, live agentLiveMessenger, hub taskEventPublisher) *AgentAssigneeService {
	return &AgentAssigneeService{repo: repo, chats: chats, tasks: tasks, cancel: cancel, live: live, hub: hub}
}

// SetMachinePolicy lets listings say whether each agent can run now.
func (s *AgentAssigneeService) SetMachinePolicy(policy func(ctx context.Context, ws uuid.UUID, profile domain.AgentProfile) error) {
	s.policy = policy
}

// SetProfileSeeder makes listings create the built-in profiles first.
func (s *AgentAssigneeService) SetProfileSeeder(seeder agentProfileSeeder) { s.seeder = seeder }

// Identities lists the workspace's agents for pickers, filters and settings.
func (s *AgentAssigneeService) Identities(ctx context.Context, ws uuid.UUID) ([]domain.AgentIdentity, error) {
	if s.seeder != nil {
		if err := s.seeder.EnsureBuiltinProfiles(ctx, ws); err != nil {
			return nil, err
		}
	}
	identities, err := s.repo.Identities(ctx, ws)
	if err != nil {
		return nil, err
	}
	for i := range identities {
		identities[i].Available = true
		if s.policy == nil {
			continue
		}
		identity := identities[i]
		err := s.policy(ctx, ws, domain.AgentProfile{ID: identity.ID, WorkspaceID: ws, Name: identity.Name, Harness: identity.Harness, Model: identity.Model, Effort: identity.Effort})
		if err != nil {
			identities[i].Available = false
			identities[i].Unavailable = agentStartFailure(err)
		}
	}
	return identities, nil
}

// AgentIdentityInput changes how an agent appears and where it can be
// assigned; a nil field is kept.
type AgentIdentityInput struct {
	AvatarColor *string      `json:"avatar_color"`
	TeamIDs     *[]uuid.UUID `json:"team_ids"`
}

var avatarColorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// UpdateIdentity is for workspace owners and administrators only; the
// repository re-checks the role inside the writing transaction.
func (s *AgentAssigneeService) UpdateIdentity(ctx context.Context, ws, profile uuid.UUID, input AgentIdentityInput) error {
	access, ok := domain.AccessFromContext(ctx)
	if !ok || access.WorkspaceID != ws || access.UserID == uuid.Nil {
		return repository.ErrAgentIdentityDenied
	}
	if input.AvatarColor != nil && *input.AvatarColor != "" && !avatarColorPattern.MatchString(*input.AvatarColor) {
		return repository.ErrAgentIdentityInvalid
	}
	if input.TeamIDs != nil && len(*input.TeamIDs) > 500 {
		return repository.ErrAgentIdentityInvalid
	}
	return s.repo.UpdateIdentity(ctx, ws, profile, access.UserID, input.AvatarColor, input.TeamIDs)
}

// ForIssues returns the agents assigned to the issues the caller can see.
func (s *AgentAssigneeService) ForIssues(ctx context.Context, issueIDs []uuid.UUID) (map[uuid.UUID][]domain.IssueAgentAssignee, error) {
	return s.repo.ForIssues(ctx, issueIDs)
}

// AgentNames resolves agent IDs to names for history display.
func (s *AgentAssigneeService) AgentNames(ctx context.Context, ws uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	return s.repo.AgentNames(ctx, ws, ids)
}

// agentAssignmentPrompt is the task an assigned agent starts with. The issue
// itself and the profile's instructions are added when the attempt runs.
const agentAssignmentPrompt = "You are assigned to this issue. Implement what it asks, following your profile's instructions."

// SetIssueAgents replaces the agents assigned to an issue, acting as the
// caller in ctx. Each added agent starts a task for the caller, as chat
// delegation would, with the agent's profile. Each removed agent's task is
// cancelled when it is still working; a task awaiting review keeps its pull
// request and a finished one is left as it is. A guest, or an agent not
// enabled for the issue's team, changes nothing.
func (s *AgentAssigneeService) SetIssueAgents(ctx context.Context, ws, issue uuid.UUID, profiles []uuid.UUID) ([]domain.IssueAgentAssignee, error) {
	access, ok := domain.AccessFromContext(ctx)
	if !ok || access.WorkspaceID != ws || access.UserID == uuid.Nil {
		return nil, repository.ErrAgentAssignForbidden
	}
	if len(profiles) > 20 {
		return nil, repository.ErrAgentNotAssignable
	}
	added, removed, err := s.repo.Set(ctx, ws, issue, access.UserID, profiles)
	if err != nil {
		return nil, err
	}
	for _, assignment := range removed {
		if assignment.TaskID == nil {
			continue
		}
		if _, err := s.cancel.CancelUnassigned(ctx, ws, *assignment.TaskID, issue, assignment.ProfileID, access.UserID); err != nil {
			log.WithError(err).WithField("task_id", *assignment.TaskID).Warn("Could not cancel the task of an unassigned agent")
		}
	}
	for _, assignment := range added {
		s.startAssignment(ctx, assignment)
	}
	if len(added)+len(removed) > 0 {
		s.changed(ws, issue)
	}
	assigned, err := s.repo.ForIssues(ctx, []uuid.UUID{issue})
	if err != nil {
		return nil, err
	}
	if assigned[issue] == nil {
		return []domain.IssueAgentAssignee{}, nil
	}
	return assigned[issue], nil
}

// changed tells clients to reload the issue's agents; they re-read them
// under their own access, so the event carries only the issue ID.
func (s *AgentAssigneeService) changed(ws, issue uuid.UUID) {
	if s.hub != nil {
		s.hub.Broadcast(ws, realtime.Event{Type: "issue.agents_updated", Payload: map[string]string{"issue_id": issue.String()}})
	}
}

// startAssignment starts the assignment's task for the member who made it.
// A permanent refusal is recorded on the assignment; a transient failure
// leaves it for Run, and the request ID makes every repeat the same task. It
// reports whether the assignment changed. When the agent was unassigned while
// its task was being created, the assignment is gone and nothing else would
// stop that task, so it is cancelled here.
func (s *AgentAssigneeService) startAssignment(ctx context.Context, assignment repository.AgentAssignment) bool {
	if assignment.AssignedBy == nil {
		return false
	}
	task, err := s.startTask(ctx, assignment.WorkspaceID, assignment.IssueID, assignment.ProfileID, *assignment.AssignedBy, assignment.RequestID, agentAssignmentPrompt)
	if err != nil {
		message, permanent := agentStartRefusal(err)
		if !permanent {
			log.WithError(err).WithField("issue_id", assignment.IssueID).Warn("Starting an assigned agent's task will be retried")
			return false
		}
		recorded, err := s.repo.Finish(ctx, assignment, nil, message)
		if err != nil {
			log.WithError(err).WithField("issue_id", assignment.IssueID).Warn("Could not record why an assigned agent's task did not start")
		}
		return recorded
	}
	recorded, err := s.repo.Finish(ctx, assignment, &task.ID, "")
	if err != nil {
		// Left unsettled: the worker repeats the start, which replays this task.
		log.WithError(err).WithField("issue_id", assignment.IssueID).Warn("Could not record an assigned agent's task")
		return false
	}
	if !recorded {
		// CancelUnassigned leaves the task alone when an assignment refers to
		// it, which is the case when a concurrent start already recorded it.
		if _, err := s.cancel.CancelUnassigned(ctx, assignment.WorkspaceID, task.ID, assignment.IssueID, assignment.ProfileID, *assignment.AssignedBy); err != nil {
			log.WithError(err).WithField("task_id", task.ID).Warn("Could not cancel the task of an agent unassigned while it started")
		}
	}
	return recorded
}

// startTask creates the agent's task as actor: the agent must still be
// enabled for the issue's team, and the actor must still be a non-guest
// member who can see the issue, which opening their issue chat checks.
func (s *AgentAssigneeService) startTask(ctx context.Context, ws, issue, profileID, actor, requestID uuid.UUID, prompt string) (*domain.AgentTask, error) {
	access := domain.WithAccess(ctx, domain.Access{WorkspaceID: ws, UserID: actor})
	profile, err := s.repo.Assignable(access, ws, issue, profileID)
	if err != nil {
		return nil, err
	}
	chat, err := s.chats.OpenIssue(access, ws, issue)
	if err != nil {
		return nil, err
	}
	return s.tasks.Create(access, ws, chat.ID, domain.AgentTaskRequest{RequestID: requestID, Prompt: prompt, AgentProfile: profile})
}

// agentStartRefusal explains a start that will never succeed as it is.
func agentStartRefusal(err error) (string, bool) {
	if errors.Is(err, repository.ErrAgentNotAssignable) {
		return "The agent can no longer be assigned in this team", true
	}
	return delegationFailure(err)
}

// agentStartFailure is agentStartRefusal's text, with a fallback.
func agentStartFailure(err error) string {
	if message, ok := agentStartRefusal(err); ok {
		return message
	}
	return "The agent cannot run right now"
}

// Run resumes assignments whose task start was interrupted.
func (s *AgentAssigneeService) Run(ctx context.Context) {
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.resume(ctx)
		}
	}
}

func (s *AgentAssigneeService) resume(ctx context.Context) {
	// Starts that kept failing for the whole window stop being retried.
	expired, err := s.repo.ExpirePending(ctx)
	if err != nil && ctx.Err() == nil {
		log.WithError(err).Warn("Could not expire agent assignments that never started")
	}
	for _, assignment := range expired {
		s.changed(assignment.WorkspaceID, assignment.IssueID)
	}
	pending, err := s.repo.Pending(ctx, 20)
	if err != nil {
		if ctx.Err() == nil {
			log.WithError(err).Warn("Could not list agent assignments to start")
		}
		return
	}
	for _, assignment := range pending {
		if s.startAssignment(ctx, assignment) {
			s.changed(assignment.WorkspaceID, assignment.IssueID)
		}
	}
}

// mentionRunes bounds the comment text an agent receives.
const mentionRunes = 2000

// mentionNamespace derives the request ID of a task started by a mention, so
// handling the same comment twice starts one task.
var mentionNamespace = uuid.MustParse("6f0b1a52-6c1e-4d57-9d0a-3a8f4b1d9c10")

// HandleComment delivers a comment to the agents it @mentions. Callers pass
// only comments people posted in a signed-in session. For each mentioned
// agent enabled in the issue's team, when the author is a non-guest member:
//
//   - the agent has an open task on the issue that the author started: the
//     comment is queued on that task's live channel (the running attempt's
//     inbox, else the next attempt's prompt). A task awaiting review is left
//     to the comment follow-up, which already starts a new attempt;
//   - the agent's open task was started by someone else: nothing is sent,
//     because that task carries its creator's permissions and credentials;
//   - the agent has no open task: a task starts for the author with the
//     comment as its request.
func (s *AgentAssigneeService) HandleComment(ctx context.Context, ws, issue uuid.UUID, comment domain.Comment) error {
	ids := extractMentionedUserIDs(comment.Body)
	if len(ids) == 0 {
		return nil
	}
	access := domain.WithAccess(ctx, domain.Access{WorkspaceID: ws, UserID: comment.UserID})
	profiles, err := s.repo.MentionedAgents(access, ws, issue, comment.UserID, ids)
	if err != nil || len(profiles) == 0 {
		return err
	}
	text := truncateRunes(commentText(comment.Body), mentionRunes)
	if text == "" {
		return nil
	}
	var failed error
	for _, profile := range profiles {
		if err := s.mention(access, ws, issue, profile, comment, text); err != nil {
			if _, permanent := agentStartRefusal(err); !permanent && !errors.Is(err, repository.ErrAgentLiveMessageClosed) && !errors.Is(err, repository.ErrAgentLiveMessageRateLimited) {
				failed = err
			}
		}
	}
	s.changed(ws, issue)
	return failed
}

func (s *AgentAssigneeService) mention(ctx context.Context, ws, issue uuid.UUID, profile domain.AgentProfile, comment domain.Comment, text string) error {
	task, err := s.repo.OpenAgentTask(ctx, ws, issue, profile.ID)
	if err != nil {
		return err
	}
	request := uuid.NewSHA1(mentionNamespace, append(comment.ID[:], profile.ID[:]...))
	if task == nil {
		_, err = s.startTask(ctx, ws, issue, profile.ID, comment.UserID, request, mentionPrompt(text))
		return err
	}
	// The task this very comment started already carries it.
	if task.CreatorID != comment.UserID || task.State == "awaiting_review" || task.RequestID == request {
		return nil
	}
	_, err = s.live.QueueLiveMessage(ctx, ws, task.ID, domain.AgentLiveSenderUser, text)
	return err
}

// mentionPrompt is the task a mention starts. The comment was written by the
// member the task runs for, so it is their request; it is fenced only to
// keep it apart from Kuayle's own text.
func mentionPrompt(text string) string {
	return "You were mentioned in a comment on this issue. Answer the question or do what it asks; when it needs no code change, say so in your summary.\n\nComment:\n~~~text\n" +
		strings.ReplaceAll(text, "~~~", "~ ~ ~") + "\n~~~"
}
