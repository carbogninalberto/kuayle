package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	log "github.com/sirupsen/logrus"
)

// An approved merge_pull_request whose branch Kuayle brought up to date waits
// as waiting_checks (KUA-207). Every server polls the waits that are due:
// ClaimMergeWaits takes them with SKIP LOCKED and pushes their next check
// forward as a lease, and the approval's effect lock keeps a second process
// out while one reads and merges. Each pass re-checks the approval as
// approving does (role, GitHub permissions, the tool switch, the task and the
// head) and runs the merge mechanism again (mergeGate.merge): merged, a final
// refusal, or another wait a minute later. A pass that cannot reach GitHub
// backs off for mergeWaitErrorPoll. The wait ends after mergeWaitTimeout.
// The final result resolves the approval, which updates the card in the
// conversation, and notifies the approver.

// mergeWaitBatch bounds the waits one pass claims.
const mergeWaitBatch = 10

const notificationApprovalFinished = "approval_finished"

// RunMergeWaits continues waiting merges every interval until ctx ends.
func (s *AssistantApprovals) RunMergeWaits(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		s.ContinueMergeWaits(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// ContinueMergeWaits runs one pass over the waits that are due and returns how
// many it claimed.
func (s *AssistantApprovals) ContinueMergeWaits(ctx context.Context) int {
	if s.conversations == nil {
		return 0
	}
	claimed, err := s.conversations.ClaimMergeWaits(ctx, mergeWaitBatch, mergeWaitErrorPoll)
	if err != nil {
		log.WithError(err).Warn("Waiting merges could not be claimed")
		return 0
	}
	for _, c := range claimed {
		if err := s.continueMergeWait(ctx, c); err != nil {
			// The claim's lease brings it back after mergeWaitErrorPoll.
			log.WithFields(log.Fields{"workspace_id": c.WorkspaceID, "approval_id": c.ID}).WithError(err).Warn("A waiting merge could not be continued")
		}
	}
	return len(claimed)
}

// continueMergeWait runs one pass of a waiting merge as its actor.
func (s *AssistantApprovals) continueMergeWait(ctx context.Context, c repository.ClaimedApproval) error {
	ctx = domain.WithAccess(ctx, domain.Access{WorkspaceID: c.WorkspaceID, UserID: c.ActorID})
	unlock, held, err := s.conversations.LockApprovalEffect(ctx, c.ID)
	if err != nil || !held {
		return err
	}
	defer unlock()
	status, data, err := s.conversations.ApprovalState(ctx, c.ID)
	if err != nil {
		return err
	}
	if status != domain.ApprovalWaitingChecks {
		return nil
	}
	var payload domain.MergePayload
	if json.Unmarshal(data, &payload) != nil || payload.Wait == nil {
		_, err = s.finishMergeWait(ctx, c, payload, nil, "This approval cannot be executed")
		return err
	}
	return s.stepMergeWait(ctx, c, data, payload)
}

// stepMergeWait runs one pass of a waiting merge and records its result.
func (s *AssistantApprovals) stepMergeWait(ctx context.Context, c repository.ClaimedApproval, original json.RawMessage, payload domain.MergePayload) error {
	pass := s.passMergeWait(ctx, c, payload)
	if pass.finish {
		_, err := s.finishMergeWait(ctx, c, payload, pass.task, pass.failure)
		return err
	}
	payload.Wait = pass.wait
	data, err := keepGitHubAccess(original, payload)
	if err != nil {
		return err
	}
	_, err = s.conversations.UpdateApprovalWait(ctx, c.WorkspaceID, c.ConversationID, c.ID, data, s.clock().Add(pass.after), pass.changed)
	return err
}

// mergeWaitPass is what one pass of a waiting merge decided: finish it
// (merged with task, or failed with failure), or read the pull request again
// after a while with the wait as it is now (changed when the card should
// show it).
type mergeWaitPass struct {
	finish  bool
	task    *uuid.UUID
	failure string
	wait    *domain.MergeWait
	after   time.Duration
	changed bool
}

// passMergeWait runs one pass of a waiting merge against GitHub. It writes
// nothing to the database; a failure to reach GitHub backs off, until the
// wait's deadline.
func (s *AssistantApprovals) passMergeWait(ctx context.Context, c repository.ClaimedApproval, payload domain.MergePayload) mergeWaitPass {
	ws := c.WorkspaceID
	wait := payload.Wait
	now := s.clock()
	fail := func(failure string) mergeWaitPass { return mergeWaitPass{finish: true, failure: failure, wait: wait} }
	if s.pulls == nil || s.merger == nil || wait == nil {
		return fail("This approval cannot be executed")
	}
	// Kuayle's own merge completes the task at once; when the approval was not
	// resolved after it (the server stopped), the pull request tells.
	if status, err := s.pulls.PullRequestStatus(ctx, ws, payload.Repository, payload.PullRequest); err == nil && status.State == "merged" && status.HeadSHA == wait.Head {
		return mergeWaitPass{finish: true, task: &payload.TaskID, wait: wait}
	}
	// The same checks as approving: role, GitHub permissions and the switch.
	refusal, err := s.guardShipStep(ctx, ws, c.ActorID, "merge_pull_request")
	if err == nil && refusal != "" {
		return fail(refusal)
	}
	var task *uuid.UUID
	var hold mergeHold
	before := *wait
	if err == nil {
		task, hold, wait, err = s.mergeApproved(ctx, ws, c.ActorID, c.IssueID, payload)
	}
	if err != nil {
		if isGitHubPermissionDenied(err) {
			return fail(deniedGap(ctx, s.access, ws, "merge_pull_request").Message)
		}
		if !now.Before(wait.Deadline) {
			return fail(mergeWaitExpiredRefusal(payload.PullRequest, wait))
		}
		log.WithFields(log.Fields{"workspace_id": ws, "approval_id": c.ID}).WithError(err).Warn("A waiting merge could not reach GitHub; it tries again later")
		return mergeWaitPass{wait: wait, after: mergeWaitErrorPoll}
	}
	switch {
	case hold.reason == "":
		return mergeWaitPass{finish: true, task: task, wait: wait}
	case hold.waits(wait):
		wait.Waiting = truncateRunes(hold.reason, 600)
		if !now.Before(wait.Deadline) {
			return fail(mergeWaitExpiredRefusal(payload.PullRequest, wait))
		}
		return mergeWaitPass{wait: wait, after: mergeWaitPoll, changed: *wait != before}
	}
	return fail(hold.reason)
}

// finishMergeWait resolves a waiting merge, records the outcome once and
// notifies the approver.
func (s *AssistantApprovals) finishMergeWait(ctx context.Context, c repository.ClaimedApproval, payload domain.MergePayload, task *uuid.UUID, failure string) (*domain.ConversationApproval, error) {
	ctx, transitioned := repository.WithFinishTransition(ctx)
	finished, err := s.conversations.FinishApproval(ctx, c.WorkspaceID, c.ConversationID, c.ID, task, failure)
	if err != nil {
		return nil, err
	}
	auditApprovalOutcome(c.WorkspaceID, finished, finished, *transitioned, nil)
	if *transitioned {
		s.notifyMergeFinished(ctx, c.WorkspaceID, finished, payload)
	}
	return finished, nil
}

// notifyMergeFinished tells the approver how a merge that waited for its
// checks ended, through the same notification path as approval requests: an
// inbox row on the issue and a live event that opens the conversation. It is
// best effort.
func (s *AssistantApprovals) notifyMergeFinished(ctx context.Context, ws uuid.UUID, approval *domain.ConversationApproval, payload domain.MergePayload) {
	if s.notifications == nil || s.userEvents == nil || approval == nil || approval.IssueID == uuid.Nil {
		return
	}
	title := fmt.Sprintf("Merged pull request #%d of %s", payload.PullRequest, payload.Identifier)
	if approval.Status != "approved" {
		reason := ""
		if approval.Error != nil {
			reason = ": " + truncateRunes(*approval.Error, 200)
		}
		title = fmt.Sprintf("Pull request #%d of %s was not merged%s", payload.PullRequest, payload.Identifier, reason)
	}
	event := notificationEvent{
		Type: notificationApprovalFinished, Title: title, IssueID: &approval.IssueID, IssueIdentifier: payload.Identifier,
		Extra: map[string]string{"approval_id": approval.ID.String(), "conversation_id": approval.ConversationID.String(), "status": approval.Status},
	}
	if err := s.notifications.Create(ctx, approval.ActorID, ws, event.IssueID, event.Type, event.Title); err != nil {
		log.WithField("approval_id", approval.ID).WithError(err).Warn("Could not notify about a finished merge")
		return
	}
	publishNotification(s.userEvents, ws, approval.ActorID, event)
}
