package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/pkg/audit"
	log "github.com/sirupsen/logrus"
)

// ship_pull_request chains the individual tools behind one approval that lists
// every step: merge an issue's pull request (merge_pull_request), wait for the
// merge commit's checks on the default branch, publish a release of that
// commit (create_release), wait for the release build (get_workflow_run) and,
// when asked, upgrade this instance (upgrade_instance). Each step goes through
// its own tool's role check, GitHub permission preflight, workspace switch and
// audit record, when the chain is requested and again when the step runs. The
// first failing step stops the chain and is reported with the next step;
// nothing after it runs.
//
// The chain runs in the background after the approval, because its waits take
// minutes, and records its progress in the approval's payload so the card
// follows it. A restarted server resumes unfinished chains (ResumeShips) from
// the last recorded step; every step is safe to repeat.

const shipAction = "ship_pull_request"

const (
	shipMerge   = "merge"
	shipTests   = "tests"
	shipRelease = "release"
	shipBuild   = "build"
	shipUpgrade = "upgrade"
)

// shipStepActions is the tool each step runs as: its policy, switch and audit
// event are the ones that step uses.
var shipStepActions = map[string]string{
	shipMerge:   "merge_pull_request",
	shipTests:   "get_pull_request_checks",
	shipRelease: "create_release",
	shipBuild:   "get_workflow_run",
	shipUpgrade: "upgrade_instance",
}

var shipStepLabels = map[string]string{
	shipMerge:   "merge the pull request",
	shipTests:   "wait for the tests",
	shipRelease: "publish the release",
	shipBuild:   "wait for the release build",
	shipUpgrade: "upgrade the instance",
}

// instanceSourceRepository is the repository whose releases this instance
// installs (see instanceReleasesURL).
const instanceSourceRepository = "carbogninalberto/kuayle"

var assistantShipToolCatalog = []assistantTool{
	{name: shipAction, agentWork: true, releases: true, approval: true, description: "Ask the user to approve shipping an issue's pull request with one approval that lists every step: merge it (as merge_pull_request, at the head GitHub reports now), wait for the merge commit's checks on the default branch, publish a release at the merge commit with the given tag and notes (as create_release, with its test gate and secret scan), wait for the release build and, only when the user asked to upgrade, upgrade this instance to that release (as upgrade_instance). Each step keeps its own tool's permission checks: only workspace owners and admins can ship, and upgrading needs an instance administrator. The chain stops at the first failing step and reports which step failed, why and the next step; nothing after it runs. Before calling it, read get_pull_request_checks, then draft the notes with draft_release_notes in the workspace's style and add a bullet for this pull request, which is not merged yet. The card shows the notes for editing and each step's progress. Only call this when the user asked to merge and release in one go; nothing happens until the user approves.", parameters: `{"type":"object","properties":{"identifier":{"type":"string","maxLength":32},"tag":{"type":"string","minLength":1,"maxLength":100},"notes":{"type":"string","minLength":1,"maxLength":14000},"name":{"type":"string","maxLength":200},"upgrade":{"type":"boolean"}},"required":["identifier","tag","notes"],"additionalProperties":false}`},
}

// shipSteps lists the chain's steps in order, all pending.
func shipSteps(upgrade bool) []domain.ShipStep {
	names := []string{shipMerge, shipTests, shipRelease, shipBuild}
	if upgrade {
		names = append(names, shipUpgrade)
	}
	steps := make([]domain.ShipStep, len(names))
	for i, name := range names {
		steps[i] = domain.ShipStep{Step: name, Status: "pending"}
	}
	return steps
}

// prepareShip checks a ship_pull_request request with every step's own
// request checks before it is offered for approval, so the user is never asked
// for a chain one of whose steps is already refused. The chain's own role was
// checked by refuseRequest. A refusal is audited.
func (t *AssistantTools) prepareShip(ctx context.Context, ws, user uuid.UUID, arguments string) (uuid.UUID, domain.ShipPayload, error) {
	var input struct {
		Identifier string `json:"identifier"`
		Tag        string `json:"tag"`
		Notes      string `json:"notes"`
		Name       string `json:"name"`
		Upgrade    bool   `json:"upgrade"`
	}
	if t.work == nil || t.releases == nil || t.releaseRepos == nil {
		return uuid.Nil, domain.ShipPayload{}, toolFailure("unknown tool")
	}
	if err := decodeToolInput(arguments, &input); err != nil {
		return uuid.Nil, domain.ShipPayload{}, err
	}
	target := strings.ToUpper(strings.TrimSpace(input.Identifier))
	refuse := func(err error) (uuid.UUID, domain.ShipPayload, error) {
		var failure *errAssistantTool
		if errors.As(err, &failure) {
			auditAssistantAction("request_refused", user, ws, shipAction, map[string]interface{}{"reason": failure.message, "target": target})
		}
		return uuid.Nil, domain.ShipPayload{}, err
	}
	if input.Upgrade && t.system == nil {
		return refuse(toolFailure("this instance cannot be upgraded from chat. Next step: ship without upgrade and upgrade from Settings → Version"))
	}
	steps := shipSteps(input.Upgrade)
	for _, step := range steps {
		action := shipStepActions[step.Step]
		if err := t.refuseDisabled(ctx, ws, action); err != nil {
			var failure *errAssistantTool
			if errors.As(err, &failure) {
				return refuse(toolFailure("%s, and the step %q needs it. Next step: a workspace owner or admin can turn it on in Settings → Assistant", failure.message, shipStepLabels[step.Step]))
			}
			return uuid.Nil, domain.ShipPayload{}, err
		}
		if ownerScoped(action) {
			continue // prepareMerge checks merging against the task's creator
		}
		refusal, err := authorizeAssistantAction(ctx, t.members, t.system, ws, user, action, uuid.Nil)
		if err != nil {
			return uuid.Nil, domain.ShipPayload{}, err
		}
		if refusal != "" {
			return refuse(toolFailure("%s", refusal))
		}
	}
	mergeArguments, err := json.Marshal(map[string]string{"identifier": input.Identifier})
	if err != nil {
		return uuid.Nil, domain.ShipPayload{}, err
	}
	issueID, merge, err := t.prepareMerge(ctx, ws, user, string(mergeArguments))
	if err != nil {
		return refuse(err)
	}
	target = fmt.Sprintf("%s#%d", merge.Repository, merge.PullRequest)
	repo, err := t.releaseRepository(ctx, ws, merge.Repository)
	if err != nil {
		return refuse(err)
	}
	if t.pulls != nil {
		status, err := t.pulls.PullRequestStatus(ctx, ws, merge.Repository, merge.PullRequest)
		if err != nil {
			return refuse(pullRequestGitHubFailure("read the pull request", err))
		}
		if status.BaseRef != "" && status.BaseRef != repo.DefaultBranch {
			return refuse(toolFailure("pull request #%d merges into %s, not %s, the default branch releases are tagged on. Next step: merge it with merge_pull_request and release the default branch separately", merge.PullRequest, status.BaseRef, repo.DefaultBranch))
		}
	}
	release := domain.ReleasePayload{Repository: repo.FullName, Tag: strings.TrimSpace(input.Tag), Name: strings.TrimSpace(input.Name), Branch: repo.DefaultBranch, Notes: strings.TrimSpace(input.Notes)}
	if err := t.checkReleaseRequest(ctx, ws, &release); err != nil {
		return refuse(err)
	}
	exists, err := t.releases.TagExists(ctx, ws, release.Repository, release.Tag)
	if err != nil {
		return refuse(releaseGitHubFailure("check the tag", err))
	}
	if exists {
		return refuse(toolFailure("the tag %s already exists in %s. Next step: choose the next version (draft_release_notes suggests one)", release.Tag, release.Repository))
	}
	payload := domain.ShipPayload{Identifier: merge.Identifier, Title: merge.Title, Repository: merge.Repository, PullRequest: merge.PullRequest, Merge: merge, Release: release, Steps: steps}
	if input.Upgrade {
		if !strings.EqualFold(repo.FullName, instanceSourceRepository) {
			return refuse(toolFailure("this instance installs releases of %s, so a release of %s cannot upgrade it. Next step: ship without upgrade", instanceSourceRepository, repo.FullName))
		}
		info, err := t.system.checkUpgradeRequest(ctx, user)
		if err != nil {
			return refuse(err)
		}
		version := strings.TrimPrefix(release.Tag, "v")
		if compareVersions(version, info.current) <= 0 {
			return refuse(toolFailure("%s is not newer than the installed %s, so upgrading to it changes nothing. Next step: ship without upgrade, or choose a later tag", release.Tag, info.current))
		}
		if info.known && compareVersions(info.latest, version) > 0 {
			return refuse(toolFailure("the release list this instance upgrades from already has %s, newer than %s, and the upgrade installs the latest release. Next step: ship without upgrade, then upgrade from Settings → Version", info.latest, release.Tag))
		}
		audit.Log("system.update_requested", user, map[string]interface{}{"from": info.current, "to": version, "chain": shipAction})
		payload.Upgrade = &domain.UpgradePayload{CurrentVersion: info.current, LatestVersion: version}
	}
	return issueID, payload, nil
}

// editShipApproval returns a chain approval's payload with the approver's
// release notes, after the same notes check create_release has.
func editShipApproval(approval *domain.ConversationApproval, notes string) (json.RawMessage, error) {
	var payload domain.ShipPayload
	if err := json.Unmarshal(approval.Payload, &payload); err != nil {
		return nil, err
	}
	notes = strings.TrimSpace(notes)
	if err := checkReleaseNotes(notes); err != nil {
		return nil, &ApprovalEditError{Message: upperFirst(toolErrorText(err))}
	}
	if notes != payload.Release.Notes {
		payload.Release.Notes, payload.Release.NotesEdited = notes, true
	}
	return keepGitHubAccess(approval.Payload, payload)
}

// shipTiming bounds the chain's waits.
type shipTiming struct {
	// poll is the pause between two reads of GitHub or the release list.
	poll time.Duration
	// tests is how long the merge commit's checks may take.
	tests time.Duration
	// buildStart is how long the release build may take to appear, and build
	// how long it may run.
	buildStart, build time.Duration
	// listing is how long the release may take to reach the release list the
	// instance upgrades from.
	listing time.Duration
	// attempts is how often a step is tried when GitHub or the database cannot
	// be reached.
	attempts int
}

var defaultShipTiming = shipTiming{poll: 20 * time.Second, tests: time.Hour, buildStart: 5 * time.Minute, build: 90 * time.Minute, listing: 20 * time.Minute, attempts: 3}

// shipRunner runs the approved chains of this process, one run per approval.
type shipRunner struct {
	timing shipTiming
	now    func() time.Time
	sleep  func(ctx context.Context, d time.Duration) error
	// spawn runs a chain in the background; tests run it inline.
	spawn   func(run func())
	mu      sync.Mutex
	running map[uuid.UUID]bool
}

func newShipRunner() *shipRunner {
	return &shipRunner{timing: defaultShipTiming, now: time.Now, sleep: sleepContext, spawn: func(run func()) { go run() }, running: map[uuid.UUID]bool{}}
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// claim marks an approval's chain as running here; false if it already is.
func (r *shipRunner) claim(id uuid.UUID) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running[id] {
		return false
	}
	r.running[id] = true
	return true
}

func (r *shipRunner) done(id uuid.UUID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.running, id)
}

// shipChain is one run of a chain: the approval and how progress is saved.
type shipChain struct {
	ws       uuid.UUID
	approval *domain.ConversationApproval
	payload  domain.ShipPayload
	save     func(domain.ShipPayload) error
}

// ship starts an approved chain in the background and returns the approval,
// still approving; the run finishes it. Approving again while it runs here
// changes nothing; after a restart it resumes the chain.
func (s *AssistantApprovals) ship(ctx context.Context, ws, conversation uuid.UUID, approval *domain.ConversationApproval) (*domain.ConversationApproval, error) {
	var payload domain.ShipPayload
	if s.merger == nil || s.targets == nil || s.pulls == nil || s.releases == nil || json.Unmarshal(approval.Payload, &payload) != nil || len(payload.Steps) == 0 || (payload.Upgrade != nil && s.system == nil) {
		return s.conversations.FinishApproval(ctx, ws, conversation, approval.ID, nil, "This approval cannot be executed")
	}
	if !s.ships.claim(approval.ID) {
		return approval, nil
	}
	// The run outlives the request that approved it, as the same actor.
	ctx = context.WithoutCancel(ctx)
	s.ships.spawn(func() {
		defer s.ships.done(approval.ID)
		// Another server process may run the same chain: the database lock
		// lets one run it, from the progress stored when it took the lock.
		unlock, held, err := s.conversations.LockApprovalEffect(ctx, approval.ID)
		if err != nil || !held {
			if err != nil {
				log.WithFields(log.Fields{"workspace_id": ws, "approval_id": approval.ID}).WithError(err).Warn("ship_pull_request could not be locked; a restart or approving again resumes it")
			}
			return
		}
		defer unlock()
		status, data, err := s.conversations.ApprovalState(ctx, approval.ID)
		if err == nil && status != "approving" {
			return
		}
		if err != nil || json.Unmarshal(data, &payload) != nil {
			log.WithFields(log.Fields{"workspace_id": ws, "approval_id": approval.ID}).WithError(err).Warn("ship_pull_request progress could not be read; a restart or approving again resumes it")
			return
		}
		ctx, transitioned := repository.WithFinishTransition(ctx)
		chain := &shipChain{ws: ws, approval: approval, payload: payload, save: func(progress domain.ShipPayload) error {
			data, err := json.Marshal(progress)
			if err == nil {
				_, err = s.conversations.RecordApprovalProgress(ctx, ws, conversation, approval.ID, data)
			}
			return err
		}}
		message, err := s.runShip(ctx, chain)
		var finished *domain.ConversationApproval
		if err == nil {
			// No task is attached: the chain creates none, and the merged one may be
			// deleted during the waits.
			finished, err = s.conversations.FinishApproval(ctx, ws, conversation, approval.ID, nil, message)
		}
		if err != nil {
			// The claim stays in place; a restart or approving again resumes it.
			log.WithFields(log.Fields{"workspace_id": ws, "approval_id": approval.ID}).WithError(err).Warn("ship_pull_request stopped before it finished")
		}
		auditApprovalOutcome(ws, approval, finished, *transitioned, err)
	})
	return approval, nil
}

// ResumeShips resumes the chains a stopped server left approving, each as its
// actor through the usual approval path, which checks access again.
func (s *AssistantApprovals) ResumeShips(ctx context.Context) {
	claimed, err := s.conversations.ClaimedApprovals(ctx, shipAction)
	if err != nil {
		log.WithError(err).Warn("unfinished ship_pull_request approvals could not be listed")
		return
	}
	for _, c := range claimed {
		actor := domain.WithAccess(ctx, domain.Access{WorkspaceID: c.WorkspaceID, UserID: c.ActorID})
		if _, err := s.Resolve(actor, c.WorkspaceID, c.ConversationID, c.ID, true); err != nil {
			log.WithFields(log.Fields{"workspace_id": c.WorkspaceID, "approval_id": c.ID}).WithError(err).Warn("ship_pull_request could not be resumed")
		}
	}
}

// runShip runs the chain's steps in order from the first unfinished one. It
// returns "" when every step succeeded, or the failure reported on the
// approval: which step failed, why, and the next step. An error means progress
// could not be saved, so the chain stopped without finishing.
func (s *AssistantApprovals) runShip(ctx context.Context, chain *shipChain) (string, error) {
	steps := chain.payload.Steps
	actor := chain.approval.ActorID
	for i := range steps {
		step := &chain.payload.Steps[i]
		if step.Status == "done" {
			continue
		}
		step.Status, step.Detail, step.URL = "running", "", ""
		if err := chain.save(chain.payload); err != nil {
			return "", err
		}
		action := shipStepActions[step.Step]
		fields := map[string]interface{}{"approval_id": chain.approval.ID.String(), "chain": shipAction, "step": step.Step, "target": approvalTarget(chain.approval)}
		detail, failure := s.runShipStep(ctx, chain, step)
		if failure != "" {
			step.Status, step.Detail = "failed", failure
			skipped := []string{}
			for j := i + 1; j < len(steps); j++ {
				chain.payload.Steps[j].Status = "skipped"
				skipped = append(skipped, shipStepLabels[chain.payload.Steps[j].Step])
			}
			fields["outcome"], fields["error"] = "failed", truncateRunes(failure, 300)
			auditAssistantAction("action_failed", actor, chain.ws, action, fields)
			if err := chain.save(chain.payload); err != nil {
				return "", err
			}
			message := fmt.Sprintf("Step %d of %d (%s) failed: %s. Next step: %s", i+1, len(steps), shipStepLabels[step.Step], strings.TrimSuffix(failure, "."), shipNextStep(chain.payload, step.Step))
			if len(skipped) > 0 {
				message += " Not run: " + strings.Join(skipped, ", ") + "."
			}
			return message, nil
		}
		step.Status, step.Detail = "done", detail
		fields["outcome"] = "executed"
		auditAssistantAction("action_executed", actor, chain.ws, action, fields)
		if err := chain.save(chain.payload); err != nil {
			return "", err
		}
	}
	return "", nil
}

// runShipStep runs one step after its tool's checks, trying again when GitHub
// or the database cannot be reached. It returns what happened, or why it failed.
func (s *AssistantApprovals) runShipStep(ctx context.Context, chain *shipChain, step *domain.ShipStep) (string, string) {
	action := shipStepActions[step.Step]
	attempts := max(s.ships.timing.attempts, 1)
	for attempt := 1; ; attempt++ {
		failure, err := s.guardShipStep(ctx, chain.ws, chain.approval.ActorID, action)
		detail := ""
		if err == nil && failure == "" {
			switch step.Step {
			case shipMerge:
				detail, failure, err = s.shipMergeStep(ctx, chain)
			case shipTests:
				detail, failure, err = s.shipTestsStep(ctx, chain)
			case shipRelease:
				detail, step.URL, failure, err = s.shipReleaseStep(ctx, chain)
			case shipBuild:
				detail, step.URL, failure, err = s.shipBuildStep(ctx, chain)
			case shipUpgrade:
				detail, failure, err = s.shipUpgradeStep(ctx, chain)
			default:
				failure = "this step is unknown"
			}
		}
		if err == nil {
			return detail, failure
		}
		if isGitHubPermissionDenied(err) {
			return "", deniedGap(ctx, s.access, chain.ws, action).Message
		}
		log.WithFields(log.Fields{"workspace_id": chain.ws, "approval_id": chain.approval.ID, "step": step.Step, "attempt": attempt}).WithError(err).Warn("ship_pull_request step could not reach GitHub or the database")
		if attempt >= attempts || s.ships.sleep(ctx, s.ships.timing.poll) != nil {
			return "", "GitHub or the database could not be reached, so the step could not finish"
		}
	}
}

// guardShipStep is execute's shared check for the step's own tool: the role
// (owner-scoped merging is checked with its target), the GitHub permissions
// and the workspace switch, read now.
func (s *AssistantApprovals) guardShipStep(ctx context.Context, ws, actor uuid.UUID, action string) (string, error) {
	if !ownerScoped(action) {
		refusal, err := authorizeAssistantAction(ctx, s.members, s.system, ws, actor, action, uuid.Nil)
		if err != nil {
			return "", err
		}
		if refusal != "" {
			return upperFirst(refusal), nil
		}
	}
	if gap := preflightGitHub(ctx, s.access, ws, action); gap != nil && !gap.Unverified {
		return gap.Message, nil
	}
	if s.settings != nil {
		disabled, err := s.settings.Disabled(ctx, ws)
		if err != nil {
			return "", err
		}
		if disabled[action] {
			return fmt.Sprintf("The tool %s is disabled in this workspace", action), nil
		}
	}
	return "", nil
}

// shipMergeStep merges as merge_pull_request does and records the merge
// commit. A pull request already merged at the approved commit, by an earlier
// run of the chain, counts as merged.
func (s *AssistantApprovals) shipMergeStep(ctx context.Context, chain *shipChain) (string, string, error) {
	merge := chain.payload.Merge
	status, err := s.pulls.PullRequestStatus(ctx, chain.ws, merge.Repository, merge.PullRequest)
	if err != nil {
		return "", "", err
	}
	if status.State != "merged" {
		_, failure, err := s.applyMerge(ctx, chain.ws, chain.approval.ActorID, chain.approval.IssueID, merge)
		if err != nil || failure != "" {
			return "", failure, err
		}
		if status, err = s.pulls.PullRequestStatus(ctx, chain.ws, merge.Repository, merge.PullRequest); err != nil {
			return "", "", err
		}
	}
	if status.State != "merged" || status.HeadSHA != merge.Commit {
		return "", fmt.Sprintf("Pull request #%d was merged at %s, not at the approved %s", merge.PullRequest, shortSHA(status.HeadSHA), shortSHA(merge.Commit)), nil
	}
	if status.MergeCommit == "" {
		return "", "", errors.New("GitHub has not reported the merge commit yet")
	}
	chain.payload.MergeCommit = status.MergeCommit
	method := merge.MergeMethod
	if method == "" {
		method = "squash"
	}
	return fmt.Sprintf("Merged #%d by %s as %s", merge.PullRequest, method, shortSHA(status.MergeCommit)), "", nil
}

// shipTestsStep waits until every check of the merge commit passed on the
// default branch, as create_release's gate requires.
func (s *AssistantApprovals) shipTestsStep(ctx context.Context, chain *shipChain) (string, string, error) {
	release, sha := chain.payload.Release, chain.payload.MergeCommit
	where := fmt.Sprintf("on %s at %s", release.Branch, shortSHA(sha))
	deadline := s.ships.now().Add(s.ships.timing.tests)
	for {
		checks, coverage, err := s.releases.CommitChecks(ctx, chain.ws, release.Repository, sha)
		if err != nil {
			return "", "", err
		}
		summary, state, _ := summarizeChecks(checks, coverage)
		switch state {
		case "passing":
			return fmt.Sprintf("%d check(s) passed %s", summary["passed"], where), "", nil
		case "failing":
			return "", releaseGateRefusal(checks, coverage, release.Branch, sha), nil
		}
		if !s.ships.now().Before(deadline) {
			switch state {
			case "none":
				return "", fmt.Sprintf("GitHub reported no checks %s after %s, so its tests did not run", where, s.ships.timing.tests), nil
			case "incomplete":
				return "", fmt.Sprintf("Not every check %s could be read after %s, so it is not known that they passed", where, s.ships.timing.tests), nil
			}
			return "", fmt.Sprintf("The checks %s were still running after %s", where, s.ships.timing.tests), nil
		}
		if err := s.ships.sleep(ctx, s.ships.timing.poll); err != nil {
			return "", "", err
		}
	}
}

// shipReleaseStep publishes the release at the merge commit as create_release
// does, after its gate and secret scan run again.
func (s *AssistantApprovals) shipReleaseStep(ctx context.Context, chain *shipChain) (string, string, string, error) {
	release := chain.payload.Release
	release.TargetCommit = chain.payload.MergeCommit
	release.CompareURL = releaseCompareURL(release)
	failure, err := s.publishRelease(ctx, chain.ws, &release)
	if err != nil {
		return "", "", "", err
	}
	chain.payload.Release = release
	if failure != "" {
		return "", "", failure, nil
	}
	return fmt.Sprintf("Published %s at %s with %d scanned file(s)", release.Tag, shortSHA(release.TargetCommit), release.ScannedFiles), release.ReleaseURL, "", nil
}

// shipBuildStep waits for the workflow runs the release started. A repository
// without a release build passes unless the instance is to be upgraded, which
// needs the build's images.
func (s *AssistantApprovals) shipBuildStep(ctx context.Context, chain *shipChain) (string, string, string, error) {
	release := chain.payload.Release
	start := s.ships.now()
	for {
		runs, err := s.releases.WorkflowRuns(ctx, chain.ws, release.Repository, release.TargetCommit)
		if err != nil {
			return "", "", "", err
		}
		var built []releaseBuildRun
		running := false
		for _, run := range runs {
			// A release run reports its tag as the head branch; runs of an
			// earlier release of the same commit are not this release's build.
			if run.Event != "release" || run.HeadBranch != release.Tag {
				continue
			}
			built = append(built, releaseBuildRun{name: truncateRunes(run.Name, 120), url: run.HTMLURL})
			switch runState(run) {
			case "failure":
				return "", run.HTMLURL, fmt.Sprintf("The release build %q failed (%s)", truncateRunes(run.Name, 120), run.Conclusion), nil
			case "running":
				running = true
			}
		}
		elapsed := s.ships.now().Sub(start)
		switch {
		case len(built) > 0 && !running:
			names := make([]string, len(built))
			for i, run := range built {
				names[i] = run.name
			}
			return fmt.Sprintf("The release build succeeded: %s", strings.Join(names, ", ")), built[0].url, "", nil
		case len(built) == 0 && elapsed >= s.ships.timing.buildStart:
			if chain.payload.Upgrade != nil {
				return "", "", fmt.Sprintf("No release build started for %s within %s, and the upgrade installs what it builds", release.Tag, s.ships.timing.buildStart), nil
			}
			return fmt.Sprintf("No workflow run started for %s, so there was no build to wait for", release.Tag), "", "", nil
		case elapsed >= s.ships.timing.build:
			url := ""
			if len(built) > 0 {
				url = built[0].url
			}
			return "", url, fmt.Sprintf("The release build was still running after %s", s.ships.timing.build), nil
		}
		if err := s.ships.sleep(ctx, s.ships.timing.poll); err != nil {
			return "", "", "", err
		}
	}
}

// releaseBuildRun is a workflow run the release started.
type releaseBuildRun struct{ name, url string }

// shipUpgradeStep waits until the release list the instance upgrades from
// shows the new release, then starts the upgrade as upgrade_instance does. An
// instance already running the release, after a resumed chain, is done.
func (s *AssistantApprovals) shipUpgradeStep(ctx context.Context, chain *shipChain) (string, string, error) {
	upgrade := *chain.payload.Upgrade
	if strings.TrimPrefix(s.system.version, "v") == upgrade.LatestVersion {
		return fmt.Sprintf("The instance runs %s", upgrade.LatestVersion), "", nil
	}
	deadline := s.ships.now().Add(s.ships.timing.listing)
	for {
		info := s.system.versionInfo(ctx)
		if info.err == nil && info.known {
			// The updater installs the feed's latest release, so it must be the
			// approved one: a newer release would be installed in its place.
			latest := compareVersions(info.latest, upgrade.LatestVersion)
			if latest == 0 {
				break
			}
			if latest > 0 {
				return "", fmt.Sprintf("The release list this instance upgrades from shows %s as the latest release, not the approved %s, and the upgrade installs the latest one", info.latest, upgrade.LatestVersion), nil
			}
		}
		if !s.ships.now().Before(deadline) {
			return "", fmt.Sprintf("The release list this instance upgrades from did not show %s within %s", upgrade.LatestVersion, s.ships.timing.listing), nil
		}
		if err := s.ships.sleep(ctx, s.ships.timing.poll); err != nil {
			return "", "", err
		}
	}
	if failure := s.system.startUpgrade(ctx, chain.approval.ActorID, upgrade); failure != "" {
		return "", failure, nil
	}
	return fmt.Sprintf("Started the upgrade from %s to %s; the instance restarts", upgrade.CurrentVersion, upgrade.LatestVersion), "", nil
}

// shipNextStep is what to do after a step failed, given what already happened.
func shipNextStep(payload domain.ShipPayload, step string) string {
	release := payload.Release
	switch step {
	case shipMerge:
		return "nothing was merged or released; fix the reason above, then ask to ship again."
	case shipTests:
		return fmt.Sprintf("pull request #%d is merged as %s but nothing was released. Fix the failing or missing checks, follow them with get_workflow_run, then publish with create_release once a commit of %s passes.", payload.PullRequest, shortSHA(payload.MergeCommit), release.Branch)
	case shipRelease:
		return fmt.Sprintf("pull request #%d is merged as %s and its checks passed, but %s was not published. Fix the reason above, then publish with create_release at target_commit %s.", payload.PullRequest, shortSHA(payload.MergeCommit), release.Tag, payload.MergeCommit)
	case shipBuild:
		next := fmt.Sprintf("%s is published but its build did not succeed. Open the run for the logs, rerun it on GitHub, and follow it with get_workflow_run (tag %s)", release.Tag, release.Tag)
		if payload.Upgrade != nil {
			next += "; upgrade with upgrade_instance once it succeeds"
		}
		return next + "."
	case shipUpgrade:
		return fmt.Sprintf("%s is published and built, but the instance was not upgraded. Fix the reason above, then ask for upgrade_instance or upgrade from Settings → Version.", release.Tag)
	}
	return "ask again."
}
