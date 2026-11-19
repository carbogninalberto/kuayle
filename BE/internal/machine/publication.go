package machine

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/kuayle/kuayle-backend/internal/domain"
	githubclient "github.com/kuayle/kuayle-backend/pkg/github"
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

// publishAgent commits, pushes and opens the pull request for a validated
// attempt or an approved stopped task. The harness never receives the credential.
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
	services, err := m.store.ListServices(ctx, machine.WorkspaceID, machine.ID)
	if err != nil {
		return err
	}
	if publication.FromStoppedTask && publication.Step.Status == domain.DevMachineAgentRunStepStatusQueued {
		status, scanErr := m.runtime.ScanWorktree(ctx, machine, services, WorktreeScanJob{
			Path: checkout.WorkspacePath, Branch: checkout.WorkingBranch, BaseBranch: checkout.BaseBranch,
			Request: domain.WorktreeScanRequest{Kind: domain.WorktreeScanStatus}, PublicationStatus: true,
		})
		if scanErr != nil || status == nil || status.Commit == "" || status.Truncated {
			return &terminalOperationError{code: "publication_unavailable", message: domain.StoppedPublicationUnavailable}
		}
		// Before the first push, compare with the base. An unknown base falls
		// through to the publisher, which fetches it from GitHub.
		firstPushAtBase := publication.PublishedCommit == "" && status.Ahead != nil && *status.Ahead == 0
		if !status.Dirty && (status.Commit == publication.PublishedCommit || firstPushAtBase) {
			return m.store.CompletePublication(ctx, *op, domain.AgentPublicationResult{})
		}
	}
	// Record that publication started before changing the checkout.
	// A retried operation can then finish pushing or opening the same draft.
	if err = m.store.StartPublication(ctx, *op); err != nil {
		return err
	}
	githubAPI, token, err := m.repositoryToken(ctx, machine, checkout.RepositoryFullName)
	if err != nil || token == "" {
		return &terminalOperationError{code: "publication_github_unavailable", message: "Repository-scoped GitHub App token is unavailable"}
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
	lease, err := m.runtime.SyncCheckout(ctx, machine, services, checkout, token, publication.PublishedCommit)
	if err != nil {
		return errors.New("checkout_sync_failed: " + strings.ReplaceAll(safeError(err), token, "[redacted]"))
	}
	if publication.FromStoppedTask {
		// An open pull request must be a draft before its branch receives
		// unchecked changes.
		if _, err = stoppedPullRequest(githubAPI, token, owner, repository, checkout); err != nil {
			return errors.New("pull_request_failed: " + strings.ReplaceAll(safeError(err), token, "[redacted]"))
		}
	}
	commit, err := m.runtime.PublishCheckout(ctx, machine, services, checkout, PublishRequest{
		AuthorName: identity.Name, AuthorEmail: identity.Email, Token: token, PublishedCommit: publication.PublishedCommit, Lease: lease,
		Message: title + "\n\nKuayle agent task " + run.TaskID.String(),
	})
	remains := errors.Is(err, ErrPublishedWorkRemains)
	if err != nil && !remains {
		return errors.New("publication_failed: " + strings.ReplaceAll(safeError(err), token, "[redacted]"))
	}
	result := domain.AgentPublicationResult{}
	if remains && !publication.FromStoppedTask {
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
		var pullRequest *githubclient.PullRequest
		var prErr error
		if publication.FromStoppedTask {
			body = "**not validated: published from a failed task**\n\n" + body
			pullRequest, prErr = stoppedPullRequest(githubAPI, token, owner, repository, checkout)
		} else {
			pullRequest, prErr = githubAPI.FindOpenPullRequest(token, owner, repository, checkout.WorkingBranch, checkout.BaseBranch)
		}
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

func stoppedPullRequest(github githubAPI, token, owner, repository string, checkout *domain.DevMachineCheckout) (*githubclient.PullRequest, error) {
	pullRequest, err := github.FindOpenPullRequest(token, owner, repository, checkout.WorkingBranch, checkout.BaseBranch)
	if err == nil && pullRequest != nil && !pullRequest.Draft {
		err = github.ConvertPullRequestToDraft(token, pullRequest.NodeID)
	}
	return pullRequest, err
}

// publicationValidated reports whether a configured check passed on this
// attempt. Only a succeeded validation step records the check's argv.
func publicationValidated(publication *domain.AgentPublication) bool {
	return !publication.FromStoppedTask && len(publication.CheckArgv) > 0 && publication.CheckExitCode != nil
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
