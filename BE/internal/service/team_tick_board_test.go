package service

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

// The board in the room (P4.6, KUA-293): the task's place on its
// repository's board (the other active tasks that change the same files, in
// merge order) is a fact Kuayle posts to whoever plans the merge order, the
// lead or else the writer, when it changes.

// boardSpace is where boardTask adds tasks: a workspace of the scenario's
// user with a team, a machine and a repository named acme/app.
type boardSpace struct{ ws, team, machine uuid.UUID }

// board is the scenario's own workspace. The root task's published pull
// request is placed on its issue, as Kuayle's publication records it, and
// its attempts changed files.
func (w *tickWorld) board(files string) boardSpace {
	w.t.Helper()
	w.exec(`UPDATE dev_machine_git_refs SET issue_id=$2 WHERE agent_run_id IN (SELECT id FROM dev_machine_agent_runs WHERE task_id=$1)`, w.root, w.f.issue)
	w.exec(`UPDATE dev_machine_agent_runs SET changed_files=$2 WHERE task_id=$1`, w.root, files)
	b := boardSpace{ws: w.f.ws, machine: w.machine}
	require.NoError(w.t, w.f.db.Get(&b.team, `SELECT team_id FROM issues WHERE id=$1`, w.f.issue))
	return b
}

// otherSpace is another workspace of the same user, with a repository of
// the same name.
func (w *tickWorld) otherSpace() boardSpace {
	w.t.Helper()
	b := boardSpace{ws: uuid.New(), team: uuid.New(), machine: uuid.New()}
	installation := uuid.New()
	suffix := strings.ReplaceAll(b.machine.String(), "-", "")
	w.exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Other',$2,$3)`, b.ws, b.ws.String(), w.f.user)
	w.exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'owner')`, b.ws, w.f.user)
	w.exec(`INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Engineering','ENG')`, b.team, b.ws)
	w.exec(`INSERT INTO team_statuses(team_id,name,slug,category) VALUES($1,'Backlog','backlog','backlog')`, b.team)
	w.exec(`INSERT INTO github_installations(id,workspace_id,installation_id,account_login,account_type,installed_by) VALUES($1,$2,2,'acme','Organization',$3)`, installation, b.ws, w.f.user)
	w.exec(`INSERT INTO github_repos(installation_id,workspace_id,github_repo_id,full_name,default_branch) VALUES($1,$2,2,$3,'main')`, installation, b.ws, tickRepository)
	w.exec(`INSERT INTO dev_machines(id,workspace_id,created_by_user_id,routing_key,name,status,desired_status,generation,repo_url,repo_provider,repo_owner,repo_name,base_branch,working_branch,machine_size,cpu_millis,memory_mb,disk_gb)
 VALUES($1,$2,$3,$4,$5,'running','running',1,'','github','','','','','small',1000,2048,20)`, b.machine, b.ws, w.f.user, suffix[:16], "machine-"+suffix)
	return b
}

// boardTask adds an active task of the user in b, on a new issue ENG-n, on
// acme/app: its attempt changed files and published pull request pr.
func (w *tickWorld) boardTask(b boardSpace, n, pr int, files string) uuid.UUID {
	w.t.Helper()
	ctx := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: b.ws, UserID: w.f.user})
	issue, branch := uuid.New(), fmt.Sprintf("kuayle/eng-%d", n)
	w.exec(`INSERT INTO issues(id,workspace_id,team_id,number,identifier_text,title,creator_id,status_id) VALUES($1,$2,$3,$4,$5,'Board',$6,(SELECT id FROM team_statuses WHERE team_id=$3 LIMIT 1))`,
		issue, b.ws, b.team, n, fmt.Sprintf("ENG-%d", n), w.f.user)
	chat, err := repository.NewConversationRepository(w.f.db).OpenIssue(ctx, b.ws, issue)
	require.NoError(w.t, err)
	task, err := w.f.tasks.Repo.Create(ctx, b.ws, chat.ID, domain.AgentTaskRequest{RequestID: uuid.New(), Prompt: "Board", Provider: "codex"})
	require.NoError(w.t, err)
	w.exec(`UPDATE agent_tasks SET state='awaiting_review',machine_id=$2 WHERE id=$1`, task.ID, b.machine)
	w.exec(`INSERT INTO dev_machine_checkouts(id,workspace_id,machine_id,issue_id,github_repo_id,repository_full_name,base_branch,working_branch,workspace_path,status,scope_key)
 SELECT $1,$2,$3,$4,id,full_name,'main',$5,$6,'ready',$7 FROM github_repos WHERE workspace_id=$2 AND full_name=$8`, task.CheckoutID, b.ws, b.machine, issue, branch, "/workspace/tasks/"+branch, task.ID.String(), tickRepository)
	w.exec(`INSERT INTO dev_machine_agent_runs(id,workspace_id,machine_id,task_id,checkout_id,requested_by_user_id,provider_id,mode,prompt,command_argv,max_runtime_seconds,status,completed_at,changed_files)
 VALUES($1,$2,$3,$4,$5,$6,'codex','autonomous','Board','["codex"]',60,'succeeded',now(),$7)`, task.RunID, b.ws, b.machine, task.ID, task.CheckoutID, w.f.user, files)
	w.exec(`INSERT INTO dev_machine_git_refs(workspace_id,machine_id,agent_run_id,issue_id,ref_type,repository_full_name,ref_name,commit_sha,pull_request_number,url,source)
 VALUES($1,$2,$3,$4,'commit',$5,$6,$7,NULL,NULL,'published'),($1,$2,$3,$4,'pull_request',$5,$6,NULL,$8,$9,'published')`,
		b.ws, b.machine, task.RunID, issue, tickRepository, branch, sha(0x500+n), pr, fmt.Sprintf("https://github.com/%s/pull/%d", tickRepository, pr))
	return task.ID
}

// boardApprove has a person approve a board task's merge, which Kuayle is
// carrying out: it goes ahead of every pull request nobody approved.
func (w *tickWorld) boardApprove(task uuid.UUID) {
	w.t.Helper()
	since := time.Now().UTC()
	wait := fmt.Sprintf(`{"approved":%q,"head":%q,"updates":0,"since":%q,"deadline":%q}`, sha(1), sha(1), since.Format(time.RFC3339Nano), since.Add(time.Hour).Format(time.RFC3339Nano))
	w.exec(`INSERT INTO agent_task_workflow_steps(task_id,sequence,node_id,node_type,visit,reason,merge) VALUES($1,1,'ship','approval',1,'Ready to ship',$2)`, task, wait)
	w.exec(`UPDATE agent_tasks SET workflow_sequence=1 WHERE id=$1`, task)
}

// pass wakes the root task's tick, as any event does, and lets it run.
func (w *tickWorld) pass() {
	w.t.Helper()
	w.exec(`UPDATE agent_tasks SET workflow_wake_at=now() WHERE id=$1`, w.root)
	w.drain()
}

func TestTickPostsTheBoardToTheWriterWhenItChanges(t *testing.T) {
	w := newTickWorld(t, nil)
	w.agent = w.writesAndApproves
	w.drain()
	require.Equal(t, "integrate", w.phase(), "CI runs on the reviewed head")
	b := w.board(`["ui/toggle.ts","ui/theme.ts"]`)
	w.pass()
	require.Empty(t, w.facts(domain.RoomFactBoard), "nothing overlaps: the board is not posted")

	// ENG-2 changes one of the same files; its pull request comes after #12.
	other := w.boardTask(b, 2, 13, `["ui/toggle.ts","api/theme.go"]`)
	w.pass()
	board := w.facts(domain.RoomFactBoard)
	require.Len(t, board, 1)
	require.Equal(t, []string{"implementer"}, []string(board[0].ToRoles), "no lead: the writer gets it")
	require.JSONEq(t, `{"overlap":[{"issue":"ENG-2","pull_request":13,"ahead":false,"files":["ui/toggle.ts"],"file_count":1}]}`, string(board[0].Data.Value))
	require.Contains(t, board[0].Body, "ENG-2 (pull request #13) also changes ui/toggle.ts and comes after this task in the merge order")

	// Nothing changed for this task, though the board moved: no new post.
	w.pass()
	w.boardTask(b, 3, 5, `["docs/readme.md"]`)
	w.pass()
	require.Len(t, w.facts(domain.RoomFactBoard), 1)

	// The order changes: ENG-2's merge is approved, so it goes first.
	w.boardApprove(other)
	w.pass()
	board = w.facts(domain.RoomFactBoard)
	require.Len(t, board, 2)
	require.JSONEq(t, `{"overlap":[{"issue":"ENG-2","pull_request":13,"ahead":true,"files":["ui/toggle.ts"],"file_count":1}]}`, string(board[1].Data.Value))
	require.Contains(t, board[1].Body, "ENG-2 (pull request #13) also changes ui/toggle.ts and is ahead of this task in the merge order")

	// The overlap changes: ENG-2's branch no longer touches the same files.
	w.exec(`UPDATE dev_machine_agent_runs SET changed_files='["api/theme.go"]' WHERE task_id=$1`, other)
	w.pass()
	board = w.facts(domain.RoomFactBoard)
	require.Len(t, board, 3)
	require.JSONEq(t, `{"overlap":[]}`, string(board[2].Data.Value))
	require.Contains(t, board[2].Body, "No other active task on acme/app changes the same files")

	require.Equal(t, 1, w.runtime.turns["implementer"], "the board waits for the writer's next turn")
	require.Equal(t, "integrate", w.phase())
}

func TestTickPostsTheBoardToTheLead(t *testing.T) {
	w := newTickWorld(t, func(p *domain.Pipeline) { p.Lead = &domain.PipelineLead{ProfileID: p.Roster[1].ProfileID} })
	w.agent = w.writesAndApproves
	w.drain()
	b := w.board(`["ui/toggle.ts"]`)
	w.boardTask(b, 2, 13, `["ui/toggle.ts"]`)
	w.pass()
	board := w.facts(domain.RoomFactBoard)
	require.Len(t, board, 1)
	require.Equal(t, []string{domain.PipelineLeadRole}, []string(board[0].ToRoles))
	require.Equal(t, 1, w.runtime.turns["implementer"])
}

// The board is the creator's in the task's workspace: the same user's task
// in another workspace, on a repository of the same name changing the same
// files, is not on it.
func TestTickTheBoardStaysInTheTasksWorkspace(t *testing.T) {
	w := newTickWorld(t, nil)
	w.agent = w.writesAndApproves
	w.drain()
	b := w.board(`["ui/toggle.ts"]`)
	w.boardTask(w.otherSpace(), 7, 3, `["ui/toggle.ts"]`)
	w.pass()
	require.Empty(t, w.facts(domain.RoomFactBoard))

	w.boardTask(b, 2, 13, `["ui/toggle.ts"]`)
	w.pass()
	board := w.facts(domain.RoomFactBoard)
	require.Len(t, board, 1)
	require.JSONEq(t, `{"overlap":[{"issue":"ENG-2","pull_request":13,"ahead":false,"files":["ui/toggle.ts"],"file_count":1}]}`, string(board[0].Data.Value))
}

// boardOrder is the scenario's board of acme/app as the creator reads it
// (GET /agent-tasks/board): the issues in merge order, the approved ones
// marked.
func (w *tickWorld) boardOrder() []string {
	w.t.Helper()
	board, err := w.f.tasks.Board(w.f.ctx, w.f.ws, tickRepository)
	require.NoError(w.t, err)
	order := []string{}
	for _, task := range board {
		name := task.IssueIdentifier
		if task.ApprovedAt != nil {
			name += " approved"
		}
		order = append(order, name)
	}
	return order
}

// The board ranks a pipeline task whose owner approved its head as approved,
// as it does a graph task whose merge was approved, and only while the head
// is the one the tick holds the approval for.
func TestTheBoardRanksAnApprovedPipelineTask(t *testing.T) {
	w := newTickWorld(t, nil)
	w.agent = w.writesAndApproves
	// The repository allows no merge method: the approved task stays on the
	// board.
	w.github.methodErr = ErrMergeMethodNotAllowed
	w.throughReview()
	b := w.board(`["ui/toggle.ts"]`)
	w.boardTask(b, 2, 11, `["api/a.go"]`)
	graph := w.boardTask(b, 3, 13, `["api/b.go"]`)
	require.Equal(t, []string{"ENG-2", "ENG-1", "ENG-3"}, w.boardOrder(), "nobody approved: by pull request number")

	require.NoError(t, w.decide(WorkflowDecisionApprove))
	w.drain()
	require.NotEqual(t, "completed", w.task().State)
	require.Empty(t, w.github.merged)
	require.Equal(t, []string{"ENG-1 approved", "ENG-2", "ENG-3"}, w.boardOrder())
	w.boardApprove(graph)
	require.Equal(t, []string{"ENG-1 approved", "ENG-3 approved", "ENG-2"}, w.boardOrder(), "approved tasks first, by approval time")

	// Another workspace is not read: neither the same user's approved
	// pipeline task there, nor a stray post naming this task there.
	other := w.otherSpace()
	away := w.boardTask(other, 7, 3, `["ui/toggle.ts"]`)
	w.exec(`UPDATE agent_tasks SET pipeline=true,pipeline_state=$2 WHERE id=$1`, away, fmt.Sprintf(`{"approved":{"head":%q,"by":%q},"facts":{"head_sha":%q,"approved":true}}`, sha(0x507), w.f.user, sha(0x507)))
	w.exec(`INSERT INTO task_messages(workspace_id,task_id,seq,author_kind,kind,body,data,created_at) VALUES($1,$2,1,'kuayle','fact','','{"fact":"approved","value":true}',now()-interval '1 hour'),
 ($1,$3,100000,'kuayle','fact','','{"fact":"approved","value":true}',now()+interval '1 hour')`, other.ws, away, w.root)
	require.Equal(t, []string{"ENG-1 approved", "ENG-3 approved", "ENG-2"}, w.boardOrder())

	// Someone pushes another head: the approval was of the one before.
	w.head(sha(0x77))
	w.exec(`INSERT INTO pull_request_heads(workspace_id,repository_full_name,pull_request_number,head_sha,observed_at) VALUES($1,$2,12,$3,now())`, w.f.ws, tickRepository, sha(0x77))
	require.Equal(t, []string{"ENG-3 approved", "ENG-2", "ENG-1"}, w.boardOrder())
	// Read by the tick too, it stays an approval of the older head.
	w.pass()
	approved := w.facts("approved")
	require.JSONEq(t, `false`, string(approved[len(approved)-1].Data.Value))
	require.Equal(t, []string{"ENG-3 approved", "ENG-2", "ENG-1"}, w.boardOrder())
}
