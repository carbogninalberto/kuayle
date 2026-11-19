package machine

import (
	"context"
	"errors"
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

// SetFrontendURL is Kuayle's public address, which links an uploaded bot
// picture in pull request descriptions.
func (m *Manager) SetFrontendURL(frontendURL string) {
	m.frontendURL = frontendURL
}

func (m *Manager) gitIdentity() GitIdentity {
	if m.agentGitIdentity.Name == "" || m.agentGitIdentity.Email == "" {
		return defaultAgentGitIdentity
	}
	return m.agentGitIdentity
}

// Refresh a previously published task worktree before the next harness starts.
func (m *Manager) syncBeforeTaskVisit(ctx context.Context, machine *domain.DevMachine, run *domain.DevMachineAgentRun, services []domain.DevMachineService, checkout *domain.DevMachineCheckout) error {
	published, err := m.store.LastPublishedCommit(ctx, run.ID)
	if err != nil || published == "" {
		return err
	}
	_, token, err := m.repositoryToken(ctx, machine, checkout.RepositoryFullName)
	if err != nil {
		return errors.New("checkout_sync_failed: repository credential unavailable")
	}
	policy, err := m.store.GetPolicy(ctx, machine.WorkspaceID)
	if err != nil {
		return err
	}
	identity := m.gitIdentity()
	if policy != nil && policy.AgentGitName != nil && policy.AgentGitEmail != nil {
		identity = GitIdentity{Name: *policy.AgentGitName, Email: *policy.AgentGitEmail}
	}
	_, err = m.runtime.SyncCheckout(ctx, machine, services, checkout, PublishRequest{
		PublishedCommit: published, Token: token, AuthorName: identity.Name, AuthorEmail: identity.Email,
		Message: "Save pending task changes before follow-up",
	})
	var blocked *checkoutSyncBlockedError
	if errors.As(err, &blocked) {
		// The implementing agent can resolve the saved merge in this visit.
		return nil
	}
	if err != nil {
		return errors.New("checkout_sync_failed: " + strings.ReplaceAll(err.Error(), token, "[redacted]"))
	}
	return nil
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
	// A workspace-configured author (e.g. a verified GitHub email) takes
	// precedence over the deployment default.
	if publication.AuthorName != "" && publication.AuthorEmail != "" {
		identity = GitIdentity{Name: publication.AuthorName, Email: publication.AuthorEmail}
	}
	// Commits others pushed to the pull request branch (a maintainer merging the
	// base branch into it) come first, so this publication builds on them
	// instead of being refused. When the agent rewrote the commits Kuayle
	// published itself, the sync returns Kuayle's published commit as the lease
	// for a guarded force push instead of changing the checkout.
	request := PublishRequest{
		AuthorName: identity.Name, AuthorEmail: identity.Email, Token: token, PublishedCommit: publication.PublishedCommit,
		Message: title + "\n\nKuayle agent task " + run.TaskID.String(),
	}
	request.Lease, err = m.runtime.SyncCheckout(ctx, machine, services, checkout, request)
	if err != nil {
		var blocked *checkoutSyncBlockedError
		if errors.As(err, &blocked) {
			detail := strings.ToValidUTF8(strings.ReplaceAll(blocked.detail, token, "[redacted]"), "�")
			return &terminalOperationError{code: domain.PublicationCheckoutBlockedCode, message: truncateText(detail, 1900)}
		}
		return errors.New("checkout_sync_failed: " + strings.ReplaceAll(safeError(err), token, "[redacted]"))
	}
	commit, err := m.runtime.PublishCheckout(ctx, machine, services, checkout, request)
	remains := errors.Is(err, ErrPublishedWorkRemains)
	if err != nil && !remains {
		return errors.New("publication_failed: " + strings.ReplaceAll(safeError(err), token, "[redacted]"))
	}
	result := domain.AgentPublicationResult{}
	if remains {
		// Nothing was pushed, so the earlier work stays on the branch and in its
		// pull request. An open pull request is named, for the reason that
		// blocks the task: none is created, updated or readied here. Without
		// one, nothing is in a pull request any more (the pull request was
		// squash-merged and closed, and the old branch now conflicts with the
		// base), so this records what a run with nothing to publish records.
		pullRequest, prErr := githubAPI.FindOpenPullRequest(token, owner, repository, checkout.WorkingBranch, checkout.BaseBranch)
		if prErr != nil {
			return errors.New("pull_request_failed: " + strings.ReplaceAll(safeError(prErr), token, "[redacted]"))
		}
		if pullRequest != nil {
			safeURL, valid := safeGitHubPullRequestURL(pullRequest.HTMLURL, checkout.RepositoryFullName)
			if !valid {
				return &terminalOperationError{code: "pull_request_invalid", message: "GitHub returned an unexpected pull request URL"}
			}
			result = domain.AgentPublicationResult{Branch: checkout.WorkingBranch, Remains: true, PullRequestURL: safeURL}
		}
	} else if commit != "" {
		// The bot's signature stays: it is the bot's name and picture, and
		// GitHub shows the description as the bot's first comment.
		content := m.pullRequestFiles(ctx, githubAPI, token, run, services, checkout, publication)
		body := publication.Bot.Signature(m.frontendURL) + publicationBody(publication, content, m.frontendURL)
		pullRequest, prErr := githubAPI.FindOpenPullRequest(token, owner, repository, checkout.WorkingBranch, checkout.BaseBranch)
		if prErr == nil && pullRequest == nil {
			// Without an independent check the change opens as a draft, so it
			// cannot be merged before someone has looked at it.
			pullRequest, prErr = githubAPI.CreatePullRequest(token, owner, repository, title, checkout.WorkingBranch, checkout.BaseBranch, body, !publicationValidated(publication))
		} else if prErr == nil {
			// A reused pull request describes this attempt.
			prErr = githubAPI.UpdatePullRequestBody(token, owner, repository, pullRequest.Number, body)
			// A draft opened by an unchecked attempt becomes mergeable once a
			// later attempt passes. The reverse is not done: a ready pull
			// request stays ready.
			if prErr == nil && pullRequest.Draft && publicationValidated(publication) {
				prErr = githubAPI.MarkPullRequestReadyForReview(token, pullRequest.NodeID)
			}
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

// publicationValidated reports whether a configured check passed on this
// attempt. Only a succeeded validation step records the check's argv.
func publicationValidated(publication *domain.AgentPublication) bool {
	return len(publication.CheckArgv) > 0 && publication.CheckExitCode != nil
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
