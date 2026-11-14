package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

// fakeWorktreeScanStore queues scans and returns the results a test prepared.
type fakeWorktreeScanStore struct {
	createScan *domain.WorktreeScan
	getScan    *domain.WorktreeScan
	createErr  error
	getErr     error
	creates    int
	gets       int
	user       uuid.UUID
	target     domain.WorktreeScanTarget
	request    domain.WorktreeScanRequest
}

func (f *fakeWorktreeScanStore) CreateWorktreeScan(_ context.Context, _, user uuid.UUID, target domain.WorktreeScanTarget, req domain.WorktreeScanRequest) (*domain.WorktreeScan, error) {
	f.creates++
	f.user, f.target, f.request = user, target, req
	if err := repository.ValidateWorktreeScanRequest(req); err != nil {
		return nil, err
	}
	if f.createErr != nil {
		return nil, f.createErr
	}
	if f.createScan != nil {
		return f.createScan, nil
	}
	return &domain.WorktreeScan{ID: uuid.New(), MachineName: "dev-1", Repository: "acme/app", WorkingBranch: "kua-141", WorkspacePath: "/workspace/tasks/ENG-1"}, nil
}

func (f *fakeWorktreeScanStore) GetWorktreeScan(context.Context, uuid.UUID, uuid.UUID) (*domain.WorktreeScan, error) {
	f.gets++
	if f.getErr != nil {
		return nil, f.getErr
	}
	if f.getScan == nil {
		return &domain.WorktreeScan{ID: uuid.New(), CompletedAt: ptrTime(time.Now())}, nil
	}
	return f.getScan, nil
}

func ptrTime(at time.Time) *time.Time { return &at }

func completedWorktreeScan(t *testing.T, result domain.WorktreeScanResult) *domain.WorktreeScan {
	t.Helper()
	data, err := json.Marshal(result)
	require.NoError(t, err)
	return &domain.WorktreeScan{ID: uuid.New(), MachineName: "dev-1", Repository: "acme/app", WorkingBranch: "kua-141",
		WorkspacePath: "/workspace/tasks/ENG-1", Result: data, CompletedAt: ptrTime(time.Now())}
}

type worktreeFixture struct {
	tools *AssistantTools
	store *fakeWorktreeScanStore
	ws    uuid.UUID
	user  uuid.UUID
	other uuid.UUID
	issue *domain.Issue
	task  uuid.UUID
}

func newWorktreeFixture(t *testing.T) *worktreeFixture {
	t.Helper()
	scan := newScanFixture(t)
	issue := &domain.Issue{ID: uuid.New(), Identifier: "ENG-1", Title: "Failed task"}
	task := uuid.New()
	scan.tools.issues = prIssues{issue: issue}
	scan.tools.SetAgentWork(prWork{followUp: &repository.FollowUpTarget{TaskID: task, CreatorID: scan.admin, State: "failed"}})
	store := &fakeWorktreeScanStore{}
	scan.tools.SetWorktreeScans(store)
	return &worktreeFixture{tools: scan.tools, store: store, ws: scan.ws, user: scan.admin, other: scan.plain, issue: issue, task: task}
}

func (f *worktreeFixture) run(t *testing.T, name, arguments string) (map[string]any, error) {
	t.Helper()
	result, _, err := f.tools.run(context.Background(), f.ws, f.user, name, arguments)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.LessOrEqual(t, len(encoded), assistantToolOutputBytes, "%s fits the tool result limit", name)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	return decoded, nil
}

func TestScanSchemasKeepGitHubAsTheDefault(t *testing.T) {
	for _, name := range []string{"repo_grep", "repo_glob", "repo_read_file"} {
		tool, ok := lookupAssistantTool(name)
		require.True(t, ok, name)
		var schema struct {
			Properties map[string]struct {
				Enum []string `json:"enum"`
			} `json:"properties"`
		}
		require.NoError(t, json.Unmarshal([]byte(tool.parameters), &schema), name)
		require.Equal(t, []string{"github", "task"}, schema.Properties["source"].Enum, name)
	}
	for _, name := range []string{"repo_list_files", "repo_symbols"} {
		tool, ok := lookupAssistantTool(name)
		require.True(t, ok, name)
		var schema struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		require.NoError(t, json.Unmarshal([]byte(tool.parameters), &schema), name)
		_, hasSource := schema.Properties["source"]
		require.False(t, hasSource, "%s reads only the pushed code", name)
	}
}

func TestScanToolsWithoutSourceReadTheSameRepository(t *testing.T) {
	f := newScanFixture(t)
	implicit, err := f.run(t, "repo_grep", map[string]any{"pattern": "Serve"})
	require.NoError(t, err)
	explicit, err := f.run(t, "repo_grep", map[string]any{"pattern": "Serve", "source": "github"})
	require.NoError(t, err)
	require.Equal(t, implicit, explicit)
}

func TestTaskSourceReadsTheLiveWorktree(t *testing.T) {
	f := newWorktreeFixture(t)
	f.store.createScan = completedWorktreeScan(t, domain.WorktreeScanResult{Branch: "kua-141-work", Commit: "abc123", Dirty: true,
		Matches: []domain.WorktreeMatch{{Path: "BE/main.go", Line: 7, Text: "Serve()"}}})
	result, err := f.run(t, "repo_grep", `{"identifier":"ENG-1","source":"task","pattern":"Serve\\(\\)","glob":"BE/**","fixed":true,"case_insensitive":true,"limit":5}`)
	require.NoError(t, err)
	require.Equal(t, "task", result["source"])
	require.Equal(t, "dev-1", result["machine"])
	require.Equal(t, "acme/app", result["repository"])
	require.Equal(t, "kua-141-work", result["branch"])
	require.Equal(t, "abc123", result["commit"])
	require.Equal(t, true, result["dirty"])
	require.Equal(t, "/workspace/tasks/ENG-1", result["path"])
	require.Len(t, result["matches"], 1)
	require.Equal(t, "BE/main.go", result["matches"].([]any)[0].(map[string]any)["path"])

	require.Equal(t, f.user, f.store.user)
	require.NotNil(t, f.store.target.TaskID)
	require.Equal(t, f.task, *f.store.target.TaskID)
	require.Equal(t, domain.WorktreeScanGrep, f.store.request.Kind)
	require.Equal(t, `Serve\(\)`, f.store.request.Pattern)
	require.Equal(t, "BE/**", f.store.request.Glob)
	require.True(t, f.store.request.Fixed)
	require.True(t, f.store.request.CaseInsensitive)
	require.Equal(t, 5, f.store.request.Limit)
}

// worktreeSettings is the workspace's per-tool switches in a unit test.
type worktreeSettings map[string]bool

func (s worktreeSettings) Disabled(context.Context, uuid.UUID) (map[string]bool, error) {
	return s, nil
}

func (s worktreeSettings) Set(context.Context, uuid.UUID, uuid.UUID, string, bool) error { return nil }

func TestTaskReadDefaultsItsLineRange(t *testing.T) {
	f := newWorktreeFixture(t)
	f.store.createScan = completedWorktreeScan(t, domain.WorktreeScanResult{Content: "one", StartLine: 1, EndLine: 1})
	_, err := f.run(t, "repo_read_file", `{"identifier":"ENG-1","source":"task","path":"BE/main.go"}`)
	require.NoError(t, err)
	require.Equal(t, 1, f.store.request.StartLine)
	require.Equal(t, scanReadLines, f.store.request.EndLine, "an open read asks for one full batch")

	f.store.createScan = completedWorktreeScan(t, domain.WorktreeScanResult{Content: "five", StartLine: 5, EndLine: 5})
	_, err = f.run(t, "repo_read_file", `{"identifier":"ENG-1","source":"task","path":"BE/main.go","start_line":5}`)
	require.NoError(t, err)
	require.Equal(t, 5, f.store.request.StartLine)
	require.Equal(t, 5+scanReadLines-1, f.store.request.EndLine)
}

func TestTaskReadRefusesAReversedRange(t *testing.T) {
	f := newWorktreeFixture(t)
	_, err := f.run(t, "repo_read_file", `{"identifier":"ENG-1","source":"task","path":"BE/main.go","start_line":10,"end_line":5}`)
	require.Error(t, err)
	require.Contains(t, toolErrorText(err), "end_line is before start_line")
	require.Zero(t, f.store.creates)
}

func TestTaskSourceIsRefusedWhenTheWorktreeSwitchIsOff(t *testing.T) {
	f := newWorktreeFixture(t)
	f.tools.SetSettings(worktreeSettings{"get_task_worktree": true})
	_, err := f.run(t, "repo_grep", `{"identifier":"ENG-1","source":"task","pattern":"x"}`)
	require.Error(t, err)
	require.Contains(t, toolErrorText(err), "get_task_worktree")
	require.Contains(t, toolErrorText(err), "Settings → Assistant")
	require.Zero(t, f.store.creates)
}

func TestTaskLimitsAreClampedLikeTheGitHubScans(t *testing.T) {
	f := newWorktreeFixture(t)
	f.store.createScan = completedWorktreeScan(t, domain.WorktreeScanResult{Files: []string{"BE/main.go"}})
	_, err := f.run(t, "repo_glob", `{"identifier":"ENG-1","source":"task","pattern":"**/*.go","limit":600}`)
	require.NoError(t, err)
	require.Equal(t, domain.WorktreeScanMaxFiles, f.store.request.Limit)

	_, err = f.run(t, "repo_grep", `{"identifier":"ENG-1","source":"task","pattern":"x","limit":600}`)
	require.NoError(t, err)
	require.Equal(t, domain.WorktreeScanMaxMatches, f.store.request.Limit)
}

func TestTrimWorktreeContentCutsAtALineBoundary(t *testing.T) {
	line := strings.Repeat("a", 100)
	content := strings.TrimSuffix(strings.Repeat(line+"\n", 200), "\n")
	cut, start, end, truncated := trimWorktreeContent(content, 10, 209, false)
	require.True(t, truncated)
	require.Equal(t, 10, start)
	require.Equal(t, 10+strings.Count(cut, "\n"), end, "end_line is the last complete line")
	for _, kept := range strings.Split(cut, "\n") {
		require.Len(t, kept, 100, "no partial line is reported")
	}
}

func TestTrimWorktreeContentMarksOneHugeLineAsCut(t *testing.T) {
	cut, start, end, truncated := trimWorktreeContent(strings.Repeat("é", scanReadBytes), 7, 7, false)
	require.True(t, truncated)
	require.Equal(t, 7, start)
	require.Equal(t, 7, end, "a cut line is not reported as a complete one")
	require.True(t, strings.HasSuffix(cut, " …"))
	require.True(t, utf8.ValidString(cut))
}

func TestTaskSourceBuildsEachScanRequest(t *testing.T) {
	f := newWorktreeFixture(t)
	f.store.createScan = completedWorktreeScan(t, domain.WorktreeScanResult{Files: []string{"BE/main.go"}})
	result, err := f.run(t, "repo_glob", `{"identifier":"ENG-1","source":"task","pattern":"**/*.go","limit":3}`)
	require.NoError(t, err)
	require.Equal(t, []any{"BE/main.go"}, result["files"])
	require.Equal(t, domain.WorktreeScanGlob, f.store.request.Kind)
	require.Equal(t, "**/*.go", f.store.request.Glob)
	require.Equal(t, 3, f.store.request.Limit)

	f.store.createScan = completedWorktreeScan(t, domain.WorktreeScanResult{Content: "two\nthree", StartLine: 2, EndLine: 3, TotalLines: 9})
	result, err = f.run(t, "repo_read_file", `{"identifier":"ENG-1","source":"task","path":"BE/main.go","start_line":2,"end_line":3}`)
	require.NoError(t, err)
	require.Equal(t, "BE/main.go", result["file"])
	require.Equal(t, "two\nthree", result["content"])
	require.EqualValues(t, 2, result["start_line"])
	require.EqualValues(t, 3, result["end_line"])
	require.EqualValues(t, 9, result["total_lines"])
	require.Equal(t, domain.WorktreeScanRead, f.store.request.Kind)
	require.Equal(t, "BE/main.go", f.store.request.Path)
	require.Equal(t, 2, f.store.request.StartLine)
	require.Equal(t, 3, f.store.request.EndLine)
}

func TestTaskSourceRequiresAnIdentifierAndRefusesARef(t *testing.T) {
	f := newWorktreeFixture(t)
	_, err := f.run(t, "repo_grep", `{"source":"task","pattern":"x"}`)
	require.Error(t, err)
	require.Contains(t, toolErrorText(err), "identifier")
	require.Zero(t, f.store.creates)

	_, err = f.run(t, "repo_grep", `{"identifier":"ENG-1","source":"task","ref":"main","pattern":"x"}`)
	require.Error(t, err)
	require.Contains(t, toolErrorText(err), "ref")
	require.Zero(t, f.store.creates)
}

func TestTaskSourceOnlyForTheTaskCreator(t *testing.T) {
	f := newWorktreeFixture(t)
	f.tools.SetAgentWork(prWork{followUp: &repository.FollowUpTarget{TaskID: f.task, CreatorID: f.other, State: "failed"}})
	_, err := f.run(t, "repo_grep", `{"identifier":"ENG-1","source":"task","pattern":"x"}`)
	require.Error(t, err)
	require.Contains(t, toolErrorText(err), "only the task's creator")
	require.Zero(t, f.store.creates)
}

func TestTaskScanErrorsNameTheNextStep(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"unavailable", repository.ErrWorktreeUnavailable, "not running or its checkout is gone"},
		{"not the creator", repository.ErrConversationNotFound, "only the task's creator"},
		{"invalid", repository.ErrConversationInvalid, "invalid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newWorktreeFixture(t)
			f.store.createErr = tc.err
			_, err := f.run(t, "repo_grep", `{"identifier":"ENG-1","source":"task","pattern":"x"}`)
			require.Error(t, err)
			require.Contains(t, toolErrorText(err), tc.want)
		})
	}
	f := newWorktreeFixture(t)
	f.store.createScan = &domain.WorktreeScan{ID: uuid.New(), MachineName: "dev-1", Error: ptrString("rg failed"), CompletedAt: ptrTime(time.Now())}
	_, err := f.run(t, "repo_grep", `{"identifier":"ENG-1","source":"task","pattern":"x"}`)
	require.Error(t, err)
	require.Contains(t, toolErrorText(err), "the scan failed on the machine: rg failed")
}

func TestTaskScanTimesOut(t *testing.T) {
	f := newWorktreeFixture(t)
	f.tools.worktreeScanWait = 20 * time.Millisecond
	pending := &domain.WorktreeScan{ID: uuid.New(), MachineName: "dev-1"}
	f.store.createScan, f.store.getScan = pending, pending
	started := time.Now()
	_, err := f.run(t, "repo_grep", `{"identifier":"ENG-1","source":"task","pattern":"x"}`)
	require.Error(t, err)
	require.Contains(t, toolErrorText(err), "did not answer within 20 s")
	require.Less(t, time.Since(started), time.Second)
	require.Greater(t, f.store.gets, 0)
}

func TestGetTaskWorktreeReportsItsGitState(t *testing.T) {
	f := newWorktreeFixture(t)
	ahead, behind := 2, 1
	f.store.createScan = completedWorktreeScan(t, domain.WorktreeScanResult{Branch: "kua-141-work", Commit: "abc123", Dirty: true,
		Changed: []string{" M BE/main.go", "?? new.txt"}, Ahead: &ahead, Behind: &behind, LastSubject: "fix(scan): read the worktree"})
	result, err := f.run(t, "get_task_worktree", `{"identifier":"ENG-1"}`)
	require.NoError(t, err)
	require.Equal(t, "ENG-1", result["identifier"])
	require.Equal(t, "dev-1", result["machine"])
	require.Equal(t, "acme/app", result["repository"])
	require.Equal(t, "kua-141-work", result["branch"])
	require.Equal(t, "abc123", result["commit"])
	require.Equal(t, true, result["dirty"])
	require.Equal(t, []any{" M BE/main.go", "?? new.txt"}, result["changed"])
	require.EqualValues(t, 2, result["ahead"])
	require.EqualValues(t, 1, result["behind"])
	require.Equal(t, "fix(scan): read the worktree", result["last_subject"])
	require.Equal(t, domain.WorktreeScanStatus, f.store.request.Kind)
	require.Equal(t, f.task, *f.store.target.TaskID)
}

func TestListMachineWorktreesUsesMachineOrIdentifier(t *testing.T) {
	machine := uuid.New()
	summary := domain.WorktreeSummary{Path: "/workspace/tasks/ENG-1", Branch: "kua-141", Commit: "abc123", Dirty: true, ModifiedAt: time.Now()}

	f := newWorktreeFixture(t)
	f.store.createScan = completedWorktreeScan(t, domain.WorktreeScanResult{Worktrees: []domain.WorktreeSummary{summary}})
	result, err := f.run(t, "list_machine_worktrees", `{"machine_id":"`+machine.String()+`"}`)
	require.NoError(t, err)
	require.Equal(t, domain.WorktreeScanList, f.store.request.Kind)
	require.NotNil(t, f.store.target.MachineID)
	require.Equal(t, machine, *f.store.target.MachineID)
	require.Nil(t, f.store.target.TaskID)
	require.Len(t, result["worktrees"], 1)
	require.Equal(t, "/workspace/tasks/ENG-1", result["worktrees"].([]any)[0].(map[string]any)["path"])

	f = newWorktreeFixture(t)
	f.store.createScan = completedWorktreeScan(t, domain.WorktreeScanResult{Worktrees: []domain.WorktreeSummary{summary}})
	_, err = f.run(t, "list_machine_worktrees", `{"identifier":"ENG-1"}`)
	require.NoError(t, err)
	require.NotNil(t, f.store.target.TaskID)
	require.Equal(t, f.task, *f.store.target.TaskID)
	require.Nil(t, f.store.target.MachineID)

	_, err = f.run(t, "list_machine_worktrees", `{"machine_id":"`+machine.String()+`","identifier":"ENG-1"}`)
	require.Error(t, err)
	require.Contains(t, toolErrorText(err), "not both")
	_, err = f.run(t, "list_machine_worktrees", `{}`)
	require.Error(t, err)
	require.Contains(t, toolErrorText(err), "machine_id or identifier")
}

// linkedRepoFake lists linked repositories so scan target errors can name them.
type linkedRepoFake struct {
	repos []domain.GitHubRepoModel
}

func (f linkedRepoFake) GetLinkedRepositoryByFullName(_ context.Context, _ uuid.UUID, name string) (*domain.GitHubRepoModel, error) {
	for _, repo := range f.repos {
		if repo.FullName == name {
			return &repo, nil
		}
	}
	return nil, nil
}

func (f linkedRepoFake) ListLinkedRepositories(context.Context, uuid.UUID) ([]domain.GitHubRepoModel, error) {
	return f.repos, nil
}

func TestScanTargetErrorsListTheLinkedRepositories(t *testing.T) {
	f := newScanFixture(t)
	repos := []domain.GitHubRepoModel{{FullName: "acme/app"}, {FullName: "acme/other"}}
	f.tools.SetRepositoryScans(newTestCheckouts(t, f.source, testScanLimits()), linkedRepoFake{repos: repos})

	_, _, err := f.tools.run(context.Background(), f.ws, f.admin, "repo_list_files", `{}`)
	require.Error(t, err)
	require.Contains(t, toolErrorText(err), "acme/app")
	require.Contains(t, toolErrorText(err), "acme/other")
	require.Contains(t, toolErrorText(err), "identifier")

	_, _, err = f.tools.run(context.Background(), f.ws, f.admin, "repo_list_files", `{"repository":"evil/other"}`)
	require.Error(t, err)
	require.Contains(t, toolErrorText(err), "acme/app")
	require.Contains(t, toolErrorText(err), "acme/other")
}

func TestScanTargetErrorsListAtMostTenRepositories(t *testing.T) {
	f := newScanFixture(t)
	repos := []domain.GitHubRepoModel{}
	for i := range 12 {
		repos = append(repos, domain.GitHubRepoModel{FullName: "acme/r" + string(rune('a'+i))})
	}
	f.tools.SetRepositoryScans(newTestCheckouts(t, f.source, testScanLimits()), linkedRepoFake{repos: repos})
	_, _, err := f.tools.run(context.Background(), f.ws, f.admin, "repo_list_files", `{"repository":"evil/other"}`)
	require.Error(t, err)
	message := toolErrorText(err)
	require.Contains(t, message, "acme/rj")
	require.NotContains(t, message, "acme/rk")
}

func TestWorktreeToolsAreReadOnlyAndNeedWiring(t *testing.T) {
	names := map[string]bool{}
	for _, tool := range assistantWorktreeToolCatalog {
		names[tool.name] = true
		require.True(t, tool.worktree, tool.name)
		require.False(t, tool.write || tool.approval, "%s is read-only and acts at once", tool.name)
		require.False(t, (&AssistantTools{}).available(tool), "%s is offered only once wired", tool.name)
	}
	require.Equal(t, map[string]bool{"get_task_worktree": true, "list_machine_worktrees": true}, names)
}

func TestTaskScanResultsStayWithinTheBudget(t *testing.T) {
	f := newWorktreeFixture(t)
	matches := make([]domain.WorktreeMatch, 0, 200)
	for i := range 200 {
		matches = append(matches, domain.WorktreeMatch{Path: "BE/main.go", Line: i + 1, Text: strings.Repeat("x", 300)})
	}
	f.store.createScan = completedWorktreeScan(t, domain.WorktreeScanResult{Matches: matches})
	result, err := f.run(t, "repo_grep", `{"identifier":"ENG-1","source":"task","pattern":"x","limit":100}`)
	require.NoError(t, err)
	require.Equal(t, true, result["truncated"])
	require.Less(t, len(result["matches"].([]any)), 200)
}

func TestSubagentsCannotReadTaskWorktrees(t *testing.T) {
	f := newSubagentFixture(t, func(_ context.Context, _ string, messages []chatCompletionMessage, _ []chatToolSpec) (string, []chatToolCall, error) {
		if !hasToolResult(messages) {
			return "", []chatToolCall{toolCall("1", "repo_grep", `{"identifier":"ENG-1","source":"task","pattern":"x"}`)}, nil
		}
		return `{"summary":"read"}`, nil, nil
	})
	out, err := f.spawn(t, task("live"))
	require.NoError(t, err)
	require.Len(t, out.Subagents[0].Steps, 1)
	step := out.Subagents[0].Steps[0]
	require.False(t, step.OK)
	require.Contains(t, step.Error, "source \"task\"")
	require.Contains(t, step.Error, "sub-tasks")
}

func TestSubagentScanSpecsDoNotOfferTheTaskSource(t *testing.T) {
	var offered []chatToolSpec
	f := newSubagentFixture(t, func(_ context.Context, _ string, _ []chatCompletionMessage, tools []chatToolSpec) (string, []chatToolCall, error) {
		offered = tools
		return `{"summary":"done"}`, nil, nil
	})
	_, err := f.spawn(t, task("specs"))
	require.NoError(t, err)
	require.NotEmpty(t, offered)
	for _, spec := range offered {
		var schema struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		require.NoError(t, json.Unmarshal(spec.Function.Parameters, &schema), spec.Function.Name)
		_, hasSource := schema.Properties["source"]
		require.False(t, hasSource, "%s keeps sub-tasks on the pushed code", spec.Function.Name)
	}
}
