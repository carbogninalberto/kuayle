package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// publishingTask drives a task through a real independent check into publishing.
func publishingTask(t *testing.T) (*sqlx.DB, *AgentTaskRepository, context.Context, domain.AgentTask, *domain.AgentTask) {
	t.Helper()
	db, repo, ctx, task, fixture := taskRecoveryFixture(t)
	claimed := prepareTaskStart(t, db, repo, ctx, task, fixture)
	installation, repositoryID := uuid.New(), uuid.New()
	_, err := db.Exec(`INSERT INTO github_installations(id,workspace_id,installation_id,account_login,account_type,installed_by) VALUES($1,$2,1,'fixture','Organization',$3)`, installation, task.WorkspaceID, task.CreatorID)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO github_repos(id,installation_id,workspace_id,github_repo_id,full_name,default_branch) VALUES($1,$2,$3,1,'fixture/repo','main')`, repositoryID, installation, task.WorkspaceID)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO dev_machine_checkouts(id,workspace_id,machine_id,issue_id,github_repo_id,repository_full_name,base_branch,working_branch,workspace_path,status,scope_key) VALUES($1,$2,$3,$4,$5,'fixture/repo','main','kuayle/rec-1','/workspace/tasks/rec-1','ready',$6)`, task.CheckoutID, task.WorkspaceID, *claimed.MachineID, task.IssueID, repositoryID, task.ID.String())
	require.NoError(t, err)
	_, err = repo.Start(ctx, *claimed)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE dev_machine_agent_runs SET status='succeeded',completed_at=now(),exit_code=0,checkout_id=$2 WHERE id=$1`, task.RunID, task.CheckoutID)
	require.NoError(t, err)
	require.NoError(t, repo.Transition(ctx, *claimed, "validating", "Native attempt finished"))
	_, err = db.Exec(`UPDATE agent_tasks SET validation_command='["true"]' WHERE id=$1`, task.ID)
	require.NoError(t, err)
	step, err := repo.EnsureValidation(ctx, *claimed)
	require.NoError(t, err)
	store := NewDevMachineRepository(db)
	ops, err := store.LeaseOperations(ctx, "validation-fixture", 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, ops, 1)
	op := ops[0]
	require.NoError(t, store.CreateRuntimeService(ctx, &domain.DevMachineService{ID: step.ID, WorkspaceID: task.WorkspaceID, MachineID: op.MachineID, AgentRunID: &task.RunID, ServiceType: "agent", ServiceKey: "validation", ContainerName: "validation", ImageRef: "fixture", InternalHost: "validation", InternalPort: 8080, Status: "pending", HealthStatus: "unknown"}))
	require.NoError(t, store.StartValidation(ctx, op, "container"))
	require.NoError(t, store.CompleteValidation(ctx, op, 0))
	require.NoError(t, store.CompleteOperation(ctx, op.ID, *op.LeaseOwner))
	require.NoError(t, repo.Transition(ctx, *claimed, "publishing", "Checks passed"))
	return db, repo, ctx, task, claimed
}

func leasePublication(t *testing.T, store *DevMachineRepository, ctx context.Context) domain.DevMachineOperation {
	t.Helper()
	ops, err := store.LeaseOperations(ctx, "publication-fixture", 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, ops, 1)
	require.Equal(t, domain.DevMachineOpPublishAgent, ops[0].Action)
	return ops[0]
}

func TestAgentTaskCannotAwaitReviewWithoutRecordedPullRequest(t *testing.T) {
	db, repo, ctx, task, claimed := publishingTask(t)
	_, err := db.Exec(`UPDATE dev_machine_agent_runs SET pull_request_url='https://github.com/acme/app/pull/9' WHERE id=$1`, task.RunID)
	require.NoError(t, err)
	require.ErrorIs(t, repo.Transition(ctx, *claimed, "awaiting_review", "Agent claimed it opened a PR"), ErrAgentTaskConflict)
	view, err := repo.Publication(ctx, task.WorkspaceID, task.ID, task.RunID)
	require.NoError(t, err)
	require.Nil(t, view.Step)
	require.Nil(t, view.PullRequestURL, "an agent-reported URL is not a recorded publication")
}

func TestPublicationRecordsPullRequestOnceAndGatesReview(t *testing.T) {
	db, repo, ctx, task, claimed := publishingTask(t)
	step, err := repo.EnsurePublication(ctx, *claimed)
	require.NoError(t, err)
	require.Equal(t, domain.DevMachineAgentRunStepStatusQueued, step.Status)
	again, err := repo.EnsurePublication(ctx, *claimed)
	require.NoError(t, err)
	require.Equal(t, step.ID, again.ID)
	store := NewDevMachineRepository(db)
	op := leasePublication(t, store, ctx)
	publication, err := store.GetPublication(ctx, op)
	require.NoError(t, err)
	require.NotEmpty(t, publication.IssueIdentifier)
	require.Equal(t, []string{"true"}, publication.CheckArgv, "the PR body cites the check that actually passed")
	require.Equal(t, 0, *publication.CheckExitCode)
	require.Empty(t, publication.AuthorName, "no workspace author keeps the deployment default")
	_, err = db.Exec(`UPDATE dev_machine_workspace_policies SET agent_git_name='carbogninalberto' WHERE workspace_id=$1`, task.WorkspaceID)
	require.Error(t, err, "a commit author needs both name and email")
	_, err = db.Exec(`UPDATE dev_machine_workspace_policies SET agent_git_name='carbogninalberto',agent_git_email='dev@example.com' WHERE workspace_id=$1`, task.WorkspaceID)
	require.NoError(t, err)
	publication, err = store.GetPublication(ctx, op)
	require.NoError(t, err)
	require.Equal(t, "carbogninalberto", publication.AuthorName)
	require.Equal(t, "dev@example.com", publication.AuthorEmail)
	require.Empty(t, publication.PublishedCommit, "nothing published yet")
	// Later publications fold agent commits only above what Kuayle published on
	// this checkout's branch. Commits only observed in the checkout, and other
	// branches or repositories, do not count.
	_, err = db.Exec(`INSERT INTO dev_machine_git_refs(workspace_id,machine_id,ref_type,repository_full_name,ref_name,commit_sha,created_at,source) VALUES
 ($1,$2,'commit','fixture/repo','kuayle/rec-1','1111111111111111111111111111111111111111',now()-interval '2 minutes','published'),
 ($1,$2,'commit','fixture/repo','kuayle/rec-1','2222222222222222222222222222222222222222',now()-interval '1 minute','published'),
 ($1,$2,'commit','fixture/repo','kuayle/rec-1','5555555555555555555555555555555555555555',now(),'observed'),
 ($1,$2,'commit','fixture/repo','kuayle/other','3333333333333333333333333333333333333333',now(),'published'),
 ($1,$2,'commit','other/repo','kuayle/rec-1','4444444444444444444444444444444444444444',now(),'published')`, task.WorkspaceID, *claimed.MachineID)
	require.NoError(t, err)
	publication, err = store.GetPublication(ctx, op)
	require.NoError(t, err)
	require.Equal(t, "2222222222222222222222222222222222222222", publication.PublishedCommit)
	result := domain.AgentPublicationResult{Branch: "kuayle/fixture", Commit: "0123456789abcdef0123456789abcdef01234567", PullRequestNumber: 12, PullRequestURL: "https://github.com/acme/app/pull/12"}
	require.ErrorIs(t, store.CompletePublication(ctx, op, result), ErrAgentTaskConflict, "a result needs a real start")
	require.NoError(t, store.StartPublication(ctx, op))
	require.NoError(t, store.StartPublication(ctx, op), "recovery restarts an in-progress publication")
	stale := op
	stale.Attempts++
	require.ErrorIs(t, store.CompletePublication(ctx, stale, result), ErrAgentTaskConflict)
	require.NoError(t, store.CompletePublication(ctx, op, result))
	require.ErrorIs(t, store.CompletePublication(ctx, op, result), ErrAgentTaskConflict, "a recorded publication is final")
	require.ErrorIs(t, repo.Transition(ctx, *claimed, "awaiting_review", "Published"), ErrAgentTaskConflict, "operation completion must settle first")
	require.NoError(t, store.CompleteOperation(ctx, op.ID, *op.LeaseOwner))
	require.NoError(t, repo.Transition(ctx, *claimed, "awaiting_review", "Published"))
	view, err := repo.Publication(ctx, task.WorkspaceID, task.ID, task.RunID)
	require.NoError(t, err)
	require.Equal(t, domain.DevMachineAgentRunStepStatusSucceeded, view.Step.Status)
	require.Equal(t, result.PullRequestURL, *view.PullRequestURL)
	require.Equal(t, result.Branch, *view.Branch)
	var refs []struct {
		RefType string  `db:"ref_type"`
		Repo    string  `db:"repository_full_name"`
		SHA     *string `db:"commit_sha"`
		Number  *int    `db:"pull_request_number"`
	}
	require.NoError(t, db.Select(&refs, `SELECT ref_type,repository_full_name,commit_sha,pull_request_number FROM dev_machine_git_refs WHERE agent_run_id=$1 ORDER BY ref_type`, task.RunID))
	var published int
	require.NoError(t, db.Get(&published, `SELECT count(*) FROM dev_machine_git_refs WHERE agent_run_id=$1 AND source='published'`, task.RunID))
	require.Equal(t, 3, published, "publication refs are marked as pushed by Kuayle")
	require.Len(t, refs, 3)
	var checkoutRepo string
	require.NoError(t, db.Get(&checkoutRepo, `SELECT c.repository_full_name FROM dev_machine_checkouts c JOIN dev_machine_agent_runs r ON r.checkout_id=c.id WHERE r.id=$1`, task.RunID))
	for _, ref := range refs {
		require.Equal(t, checkoutRepo, ref.Repo)
	}
	require.Equal(t, result.Commit, *refs[1].SHA)
	require.Equal(t, 12, *refs[2].Number)
	var events int
	require.NoError(t, db.Get(&events, `SELECT count(*) FROM conversation_events WHERE conversation_id=$1 AND type='task.publication_updated'`, task.ConversationID))
	require.Equal(t, 3, events, "queued, running and succeeded")
	stranger := domain.WithAccess(ctx, domain.Access{WorkspaceID: task.WorkspaceID, UserID: uuid.New()})
	_, err = repo.Publication(stranger, task.WorkspaceID, task.ID, task.RunID)
	require.ErrorIs(t, err, ErrConversationNotFound)
}

func TestPublicationWithoutChangesIsSkippedNotReviewable(t *testing.T) {
	db, repo, ctx, _, claimed := publishingTask(t)
	_, err := repo.EnsurePublication(ctx, *claimed)
	require.NoError(t, err)
	store := NewDevMachineRepository(db)
	op := leasePublication(t, store, ctx)
	require.NoError(t, store.StartPublication(ctx, op))
	require.NoError(t, store.CompletePublication(ctx, op, domain.AgentPublicationResult{}))
	require.NoError(t, store.CompleteOperation(ctx, op.ID, *op.LeaseOwner))
	step, err := repo.EnsurePublication(ctx, *claimed)
	require.NoError(t, err)
	require.Equal(t, domain.DevMachineAgentRunStepStatusSkipped, step.Status)
	require.ErrorIs(t, repo.Transition(ctx, *claimed, "awaiting_review", "Nothing published"), ErrAgentTaskConflict)
}

func TestExhaustedPublicationFailsInsteadOfWaiting(t *testing.T) {
	db, repo, ctx, task, claimed := publishingTask(t)
	_, err := repo.EnsurePublication(ctx, *claimed)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE dev_machine_operations SET status='failed' WHERE agent_run_id=$1 AND action='publish_agent'`, task.RunID)
	require.NoError(t, err)
	step, err := repo.EnsurePublication(ctx, *claimed)
	require.NoError(t, err)
	require.Equal(t, domain.DevMachineAgentRunStepStatusFailed, step.Status)
}

func TestPublicationRejectsRevokedAuthority(t *testing.T) {
	for _, scenario := range []string{"cancelled", "membership", "policy", "machine_generation", "expired_lease", "superseded_attempt"} {
		t.Run(scenario, func(t *testing.T) {
			db, repo, ctx, task, claimed := publishingTask(t)
			_, err := repo.EnsurePublication(ctx, *claimed)
			require.NoError(t, err)
			store := NewDevMachineRepository(db)
			op := leasePublication(t, store, ctx)
			require.NoError(t, store.StartPublication(ctx, op))
			switch scenario {
			case "cancelled":
				err = repo.RequestCancel(ctx, task.WorkspaceID, task.ID)
			case "membership":
				_, err = db.Exec(`DELETE FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`, task.WorkspaceID, task.CreatorID)
			case "policy":
				_, err = db.Exec(`UPDATE dev_machine_workspace_policies SET enabled=false WHERE workspace_id=$1`, task.WorkspaceID)
			case "machine_generation":
				_, err = db.Exec(`UPDATE dev_machines SET generation=generation+1 WHERE id=$1`, op.MachineID)
			case "expired_lease":
				_, err = db.Exec(`UPDATE dev_machine_operations SET lease_expires_at=now()-interval '1 second' WHERE id=$1`, op.ID)
			case "superseded_attempt":
				_, err = db.Exec(`UPDATE agent_tasks SET run_id=gen_random_uuid() WHERE id=$1`, task.ID)
			}
			require.NoError(t, err)
			_, err = store.GetPublication(ctx, op)
			require.Error(t, err)
			require.Error(t, store.CompletePublication(ctx, op, domain.AgentPublicationResult{Branch: "b", Commit: "c", PullRequestNumber: 1, PullRequestURL: "https://github.com/acme/app/pull/1"}))
			var refs int
			require.NoError(t, db.Get(&refs, `SELECT count(*) FROM dev_machine_git_refs WHERE agent_run_id=$1`, task.RunID))
			require.Zero(t, refs)
		})
	}
}

func TestMergedPullRequestCompletesOnlyTheRecordedReviewTask(t *testing.T) {
	db, repo, ctx, task, claimed := publishingTask(t)
	_, err := repo.EnsurePublication(ctx, *claimed)
	require.NoError(t, err)
	store := NewDevMachineRepository(db)
	op := leasePublication(t, store, ctx)
	require.NoError(t, store.StartPublication(ctx, op))
	require.NoError(t, store.CompletePublication(ctx, op, domain.AgentPublicationResult{Branch: "kuayle/rec-1", Commit: "0123456789abcdef0123456789abcdef01234567", PullRequestNumber: 12, PullRequestURL: "https://github.com/fixture/repo/pull/12"}))
	require.NoError(t, store.CompleteOperation(ctx, op.ID, *op.LeaseOwner))
	background := context.Background()
	n, err := repo.CompleteMergedPullRequest(background, task.WorkspaceID, "fixture/repo", 12)
	require.NoError(t, err)
	require.Zero(t, n, "publishing is not yet reviewable")
	require.NoError(t, repo.Transition(ctx, *claimed, "awaiting_review", "Published"))
	for _, other := range []struct {
		workspace uuid.UUID
		repo      string
		number    int
	}{{uuid.New(), "fixture/repo", 12}, {task.WorkspaceID, "other/repo", 12}, {task.WorkspaceID, "fixture/repo", 13}} {
		n, err = repo.CompleteMergedPullRequest(background, other.workspace, other.repo, other.number)
		require.NoError(t, err)
		require.Zero(t, n)
	}
	n, err = repo.CompleteMergedPullRequest(background, task.WorkspaceID, "Fixture/Repo", 12)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	n, err = repo.CompleteMergedPullRequest(background, task.WorkspaceID, "fixture/repo", 12)
	require.NoError(t, err)
	require.Zero(t, n, "webhook redelivery is idempotent")
	current, err := repo.Get(ctx, task.WorkspaceID, task.ID)
	require.NoError(t, err)
	require.Equal(t, "completed", current.State)
	var actor *uuid.UUID
	require.NoError(t, db.Get(&actor, `SELECT actor_id FROM agent_task_transitions WHERE task_id=$1 AND to_state='completed'`, task.ID))
	require.Nil(t, actor)
}

// Deleting a machine destroys its runtime but keeps the record while agent tasks
// have history on it: attempts, logs and the pull request refs that let a later
// merge complete the task. The kept record stays deleted for the user: it no
// longer appears in their machine list and the manager stops revisiting it.
func TestMachinePurgeKeepsAgentTaskHistory(t *testing.T) {
	db, _, ctx, task, claimed := publishingTask(t)
	store := NewDevMachineRepository(db)
	machine := *claimed.MachineID
	_, err := db.Exec(`UPDATE dev_machine_operations SET status='completed',lease_owner=NULL,lease_expires_at=NULL WHERE machine_id=$1`, machine)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE dev_machines SET status='destroyed',destroyed_at=now(),delete_requested_at=now(),docker_network_name=NULL,workspace_volume_name=NULL WHERE id=$1`, machine)
	require.NoError(t, err)
	var owner uuid.UUID
	require.NoError(t, db.Get(&owner, `SELECT created_by_user_id FROM dev_machines WHERE id=$1`, machine))
	listed := func() bool {
		machines, _, err := store.ListMachinesForUser(ctx, task.WorkspaceID, owner, "", nil, 100, 0)
		require.NoError(t, err)
		for _, listedMachine := range machines {
			if listedMachine.ID == machine {
				return true
			}
		}
		return false
	}
	pendingPurge := func() bool {
		requests, err := store.ListPermanentDeleteRequests(ctx, 1000)
		require.NoError(t, err)
		for _, request := range requests {
			if request.ID == machine {
				return true
			}
		}
		return false
	}

	require.NoError(t, store.PurgePermanentDeleteRequest(ctx, task.WorkspaceID, machine))
	var requested *time.Time
	require.NoError(t, db.Get(&requested, `SELECT delete_requested_at FROM dev_machines WHERE id=$1`, machine), "the record is kept")
	require.NotNil(t, requested, "the kept record stays deleted for the user")
	require.False(t, listed(), "a deleted machine does not reappear in the machine list")
	require.False(t, pendingPurge(), "the manager does not revisit a record kept as history")

	// Clearing destroyed machines hides a kept record the same way.
	_, err = db.Exec(`UPDATE dev_machines SET delete_requested_at=NULL WHERE id=$1`, machine)
	require.NoError(t, err)
	require.True(t, listed())
	result, err := store.BulkPurgeMachines(ctx, task.WorkspaceID, domain.DevMachinePurge{IncludeFailed: true, IncludeExpired: true})
	require.NoError(t, err)
	require.Equal(t, 1, result.Removed)
	require.False(t, listed())
	var runs int
	require.NoError(t, db.Get(&runs, `SELECT count(*) FROM dev_machine_agent_runs WHERE id=$1`, task.RunID))
	require.Equal(t, 1, runs)
	result, err = store.BulkPurgeMachines(ctx, task.WorkspaceID, domain.DevMachinePurge{IncludeFailed: true, IncludeExpired: true})
	require.NoError(t, err)
	require.Zero(t, result.Removed, "a hidden record is not counted twice")

	// Once no agent task needs the history, the pending deletion completes.
	_, err = db.Exec(`UPDATE dev_machine_agent_runs SET task_id=NULL WHERE machine_id=$1`, machine)
	require.NoError(t, err)
	require.True(t, pendingPurge())
	require.NoError(t, store.PurgePermanentDeleteRequest(ctx, task.WorkspaceID, machine))
	var left int
	require.NoError(t, db.Get(&left, `SELECT count(*) FROM dev_machines WHERE id=$1`, machine))
	require.Zero(t, left)
}
