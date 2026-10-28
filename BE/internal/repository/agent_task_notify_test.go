package repository

import (
	"testing"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestTaskReviewNotifiesTheCreatorsLinkedTelegram(t *testing.T) {
	for _, scenario := range []string{"linked", "disabled", "unlinked"} {
		t.Run(scenario, func(t *testing.T) {
			db, repo, ctx, task, claimed := publishingTask(t)
			_, err := db.Exec(`INSERT INTO telegram_settings(workspace_id,bot_id,token_encrypted,webhook_secret,enabled,allowed_user_ids) VALUES($1,(random()*1e12)::bigint+1,'x','x',$2,'{12345}')`, task.WorkspaceID, scenario != "disabled")
			require.NoError(t, err)
			if scenario != "unlinked" {
				_, err = db.Exec(`INSERT INTO telegram_links(workspace_id,user_id,telegram_user_id) VALUES($1,$2,12345)`, task.WorkspaceID, task.CreatorID)
				require.NoError(t, err)
			}
			_, err = repo.EnsurePublication(ctx, *claimed)
			require.NoError(t, err)
			store := NewDevMachineRepository(db)
			op := leasePublication(t, store, ctx)
			require.NoError(t, store.StartPublication(ctx, op))
			require.NoError(t, store.CompletePublication(ctx, op, domain.AgentPublicationResult{Branch: "kuayle/rec-1", Commit: publishedCommit, PullRequestNumber: 12, PullRequestURL: "https://github.com/fixture/repo/pull/12"}))
			require.NoError(t, store.CompleteOperation(ctx, op.ID, *op.LeaseOwner))
			require.NoError(t, repo.Transition(ctx, *claimed, "awaiting_review", "Pull request opened; waiting for human review"))
			var texts []string
			require.NoError(t, db.Select(&texts, `SELECT text FROM telegram_deliveries WHERE workspace_id=$1 AND conversation_id=$2 AND status='queued'`, task.WorkspaceID, task.ConversationID))
			if scenario != "linked" {
				require.Empty(t, texts)
				return
			}
			require.Len(t, texts, 1, "intermediate states such as publishing do not notify")
			require.Contains(t, texts[0], "REC-1 · Recovery: coding task ready for review.")
			require.Contains(t, texts[0], "Pull request: https://github.com/fixture/repo/pull/12")
		})
	}
}
