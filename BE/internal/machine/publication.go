package machine

import (
	"context"
	"encoding/json"
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

// publicationBody describes the change for human review from facts Kuayle
// recorded: the issue, the changed files and commits, and the independent
// check. Agent output is never included; it is unverified and often noisy.
func publicationBody(publication *domain.AgentPublication, run *domain.DevMachineAgentRun) string {
	var body strings.Builder
	fmt.Fprintf(&body, "Implements %s: %s\n", publication.IssueIdentifier, markdownLine(publication.IssueTitle))
	var files, commits []string
	_ = json.Unmarshal(run.ChangedFiles, &files)
	_ = json.Unmarshal(run.Commits, &commits)
	if len(files) > 0 {
		body.WriteString("\n### Changes\n")
		for i, file := range files {
			if i == 50 {
				fmt.Fprintf(&body, "- and %d more files\n", len(files)-50)
				break
			}
			fmt.Fprintf(&body, "- `%s`\n", strings.ReplaceAll(markdownLine(file), "`", "'"))
		}
	}
	if len(commits) > 0 {
		short := make([]string, 0, len(commits))
		for _, commit := range commits {
			if len(commit) >= 7 {
				short = append(short, "`"+commit[:7]+"`")
			}
		}
		fmt.Fprintf(&body, "\nCommits: %s\n", strings.Join(short, ", "))
	}
	body.WriteString("\n### Verification\n")
	if len(publication.CheckArgv) > 0 && publication.CheckExitCode != nil {
		fmt.Fprintf(&body, "Kuayle ran `%s` independently on this branch in a clean container without credentials: passed (exit code %d).\n", strings.ReplaceAll(markdownLine(strings.Join(publication.CheckArgv, " ")), "`", "'"), *publication.CheckExitCode)
	} else {
		body.WriteString("No independent check is configured for this project.\n")
	}
	body.WriteString("\n### Review notes\n")
	agentName := map[string]string{"codex": "Codex", "claude-code": "Claude Code", "opencode": "OpenCode"}[run.ProviderID]
	if agentName == "" {
		agentName = markdownLine(run.ProviderID)
	}
	if run.Model != "" {
		agentName += " (" + markdownLine(run.Model) + ")"
	}
	fmt.Fprintf(&body, "- Written by %s through a Kuayle agent task; human review is required before merging.\n", agentName)
	fmt.Fprintf(&body, "- Review feedback on this pull request is sent back to the same task.\n- Kuayle agent task: %s\n", run.TaskID)
	return body.String()
}

// markdownLine keeps recorded values on one line and stops them from
// mentioning users or teams.
func markdownLine(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	return strings.ReplaceAll(truncateText(value, 300), "@", "@\u200b")
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
