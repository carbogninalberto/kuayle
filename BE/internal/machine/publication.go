package machine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/kuayle/kuayle-backend/internal/domain"
)

// GitIdentity is the commit author recorded for agent publications. The pull
// request itself is opened by the repository-scoped GitHub App installation.
type GitIdentity struct {
	Name  string
	Email string
}

var defaultAgentGitIdentity = GitIdentity{Name: "Kuayle Agent", Email: "agent@kuayle.invalid"}

// SetAgentGitIdentity overrides the commit author for agent publications.
func (m *Manager) SetAgentGitIdentity(identity GitIdentity) {
	m.agentGitIdentity = identity
}

func (m *Manager) gitIdentity() GitIdentity {
	if m.agentGitIdentity.Name == "" || m.agentGitIdentity.Email == "" {
		return defaultAgentGitIdentity
	}
	return m.agentGitIdentity
}

// publishAgent commits, pushes and opens the pull request for an independently
// validated task attempt. The native harness never receives the credential.
func (m *Manager) publishAgent(ctx context.Context, machine *domain.DevMachine, op *domain.DevMachineOperation) error {
	publication, err := m.store.GetPublication(ctx, *op)
	if err != nil {
		return err
	}
	switch publication.Step.Status {
	case domain.DevMachineAgentRunStepStatusSucceeded, domain.DevMachineAgentRunStepStatusFailed, domain.DevMachineAgentRunStepStatusCancelled, domain.DevMachineAgentRunStepStatusSkipped:
		return nil
	}
	run, err := m.store.GetAgentRunInternal(ctx, publication.Step.AgentRunID)
	if err != nil {
		return err
	}
	if run == nil || run.TaskID == nil || run.CheckoutID == nil {
		return &terminalOperationError{code: "publication_unavailable", message: "Publication requires the original task checkout"}
	}
	checkout, err := m.store.GetCheckoutInternal(ctx, *run.CheckoutID)
	if err != nil {
		return err
	}
	if checkout == nil || checkout.Status != "ready" || checkout.MachineID != machine.ID || checkout.WorkspaceID != machine.WorkspaceID || checkout.ScopeKey != run.TaskID.String() {
		return &terminalOperationError{code: "publication_unavailable", message: "Task checkout is unavailable"}
	}
	owner, repository, ok := splitRepositoryFullName(checkout.RepositoryFullName)
	if !ok {
		return &terminalOperationError{code: "publication_unavailable", message: "Task repository is invalid"}
	}
	if err = m.store.StartPublication(ctx, *op); err != nil {
		return err
	}
	githubAPI, token, err := m.repositoryToken(ctx, machine, checkout.RepositoryFullName)
	if err != nil || token == "" {
		return &terminalOperationError{code: "publication_unavailable", message: "Repository-scoped GitHub App token is unavailable"}
	}
	services, err := m.store.ListServices(ctx, machine.WorkspaceID, machine.ID)
	if err != nil {
		return err
	}
	title := truncateText(publication.IssueIdentifier+": "+publication.IssueTitle, 240)
	identity := m.gitIdentity()
	commit, err := m.runtime.PublishCheckout(ctx, machine, services, checkout, PublishRequest{
		AuthorName: identity.Name, AuthorEmail: identity.Email, Token: token,
		Message: title + "\n\nKuayle agent task " + run.TaskID.String(),
	})
	if err != nil {
		return errors.New("publication_failed: " + strings.ReplaceAll(safeError(err), token, "[redacted]"))
	}
	result := domain.AgentPublicationResult{}
	if commit != "" {
		pullRequest, prErr := githubAPI.FindOpenPullRequest(token, owner, repository, checkout.WorkingBranch, checkout.BaseBranch)
		if prErr == nil && pullRequest == nil {
			pullRequest, prErr = githubAPI.CreatePullRequest(token, owner, repository, title, checkout.WorkingBranch, checkout.BaseBranch, publicationBody(publication, run))
		}
		if prErr != nil {
			return errors.New("pull_request_failed: " + strings.ReplaceAll(safeError(prErr), token, "[redacted]"))
		}
		safeURL, valid := safeGitHubPullRequestURL(pullRequest.HTMLURL, checkout.RepositoryFullName)
		if !valid || pullRequest.Number <= 0 {
			return &terminalOperationError{code: "pull_request_invalid", message: "GitHub returned an unexpected pull request URL"}
		}
		result = domain.AgentPublicationResult{Branch: checkout.WorkingBranch, Commit: commit, PullRequestNumber: pullRequest.Number, PullRequestURL: safeURL}
	}
	return m.store.CompletePublication(ctx, *op, result)
}

// publicationBody links the issue for existing GitHub automation. Agent text is
// untrusted, so it is fenced: mentions and markup cannot act on the repository.
func publicationBody(publication *domain.AgentPublication, run *domain.DevMachineAgentRun) string {
	var body strings.Builder
	fmt.Fprintf(&body, "Implements %s.\n\nKuayle opened this pull request after the task's independent check passed. Human review is required before merging.\n", publication.IssueIdentifier)
	if run.Summary != nil && strings.TrimSpace(*run.Summary) != "" {
		summary := strings.ReplaceAll(truncateText(*run.Summary, 4000), "~~~", "~ ~ ~")
		fmt.Fprintf(&body, "\nAgent summary (unverified):\n\n~~~text\n%s\n~~~\n", summary)
	}
	fmt.Fprintf(&body, "\nKuayle agent task: %s\n", run.TaskID)
	return body.String()
}

func truncateText(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	value = value[:limit]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}
