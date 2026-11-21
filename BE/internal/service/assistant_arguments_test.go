package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

func TestAssistantArgumentErrorsReachTheModel(t *testing.T) {
	f := newToolFixture(t)
	f.service.tools.SetSkills(repository.NewAssistantSkillRepository(f.db))
	f.service.tools.SetAgentWork(&mergeWorkFake{})
	for _, tc := range []struct {
		name, args string
		want       []string
	}{
		{"load_skill", "{}", []string{"name", "required", "team_key"}},
		{"get_issue", `{"identifier":"ENG-1","private-input ` + strings.Repeat("!", 512) + `":true}`, []string{"unknown", "identifier"}},
		{"get_issue", `{"identifier":"ENG-1","unexpected":"private-input"}`, []string{"unexpected", "identifier"}},
		{"search_issues", `{"query":"hello","limit":"private-input"}`, []string{"limit", "integer", "query"}},
		{"search_issues", `{"query":"hello","limit":1.0}`, []string{"limit", "integer literal", "query"}},
		{"search_issues", `{"query":"hello","limit":35}`, []string{"limit", "20", "query"}},
		{"list_agent_work", `{"state":"private-input"}`, []string{"state", "queued", "limit"}},
		{"list_agent_work", `{"identifier":"ENG-1"}`, []string{"identifier", "get_agent_work", "state", "limit"}},
		{"delegate_issue", `{"identifier":true,"instructions":"Review"}`, []string{"identifier", "string"}},
	} {
		t.Run(tc.name+tc.args, func(t *testing.T) {
			a := f.attempt(t, "Read the task")
			changes := 0
			msg, err := f.service.runToolCall(f.ctx, a, 0, toolCall("call-1", tc.name, tc.args), &changes)
			require.NoError(t, err)
			var reply map[string]string
			require.NoError(t, json.Unmarshal([]byte(msg), &reply))
			for _, word := range append(tc.want, tc.name, "accepted") {
				require.Contains(t, strings.ToLower(reply["error"]), word)
			}
			require.NotContains(t, reply["error"], "private-input")
		})
	}
}

type argumentCustomTools struct{ assistantCustomTools }

func (argumentCustomTools) ActiveByName(context.Context, uuid.UUID, string) (*domain.AssistantCustomTool, error) {
	return &domain.AssistantCustomTool{Parameters: json.RawMessage(`{"type":"object","properties":{"count":{"type":"integer","maximum":3},"mode":{"type":"string"}},"required":["count"]}`)}, nil
}

func TestCustomToolArgumentErrors(t *testing.T) {
	f := newPRFixture()
	f.tools.SetCustomTools(argumentCustomTools{})
	for _, tc := range []struct {
		args string
		want []string
	}{
		{`{"count":2}`, []string{"count", "inputs", "machine_id"}},
		{`{"inputs":{"count":4}}`, []string{"count", "3", "mode"}},
		{`{"inputs":{"private-input ` + strings.Repeat("!", 512) + `":2}}`, []string{"unknown", "count", "mode"}},
		{`{"inputs":{"unknown":"private-input"}}`, []string{"unknown", "count", "mode"}},
	} {
		_, err := f.tools.prepareCustomTool(t.Context(), f.ws, f.admin, "sample", tc.args)
		require.Error(t, err)
		for _, word := range tc.want {
			require.ErrorContains(t, err, word)
		}
		require.NotContains(t, err.Error(), "private-input")
	}
}

func TestScanPathSelectsOneFileOrDirectory(t *testing.T) {
	f := newScanFixture(t,
		tarEntry{name: "repo/.config.d/one", body: "needle"},
		tarEntry{name: "repo/Dockerfile", body: "needle"},
		tarEntry{name: "repo/pages/[id].ts", body: "needle"},
		tarEntry{name: "repo/pages/i.ts", body: "needle"},
	)
	for _, tc := range []struct{ path, glob, file string }{
		{".config.d", ".config.d/**", ".config.d/one"},
		{"Dockerfile", "Dockerfile", "Dockerfile"},
		{"pages/[id].ts", `pages/\[id].ts`, "pages/[id].ts"},
	} {
		result, err := f.run(t, "repo_grep", map[string]any{"pattern": "needle", "path": tc.path})
		require.NoError(t, err)
		require.Equal(t, tc.glob, result["glob"])
		require.Equal(t, []any{tc.file}, filePaths(result["matches"]))
		result, err = f.run(t, "repo_glob", map[string]any{"path": tc.path})
		require.NoError(t, err)
		require.Equal(t, []any{tc.file}, filePaths(result["files"]))
	}
}

func TestTaskScanPathKeepsTheLiveTargetAndLiteralFilter(t *testing.T) {
	f := newWorktreeFixture(t)
	f.store.createScan = completedWorktreeScan(t, domain.WorktreeScanResult{})
	for _, tc := range []struct{ name, args string }{
		{"repo_grep", `{"identifier":"ENG-1","source":"task","path":"pages/[id]","pattern":"match"}`},
		{"repo_glob", `{"identifier":"ENG-1","source":"task","path":"pages/[id]"}`},
	} {
		_, err := f.run(t, tc.name, tc.args)
		require.NoError(t, err)
		require.Equal(t, `pages/\[id]/**`, f.store.request.Glob)
	}
}

func TestSingleScanRepositoryDefaultUsesExistingAccessChecks(t *testing.T) {
	f := newScanFixture(t)
	f.tools.scanRepos = linkedRepoFake{repos: []domain.GitHubRepoModel{{FullName: "acme/app", DefaultBranch: "main"}}}
	run := func(user uuid.UUID, args string) error {
		_, _, err := f.tools.run(t.Context(), f.ws, user, "repo_list_files", args)
		return err
	}
	require.Error(t, run(f.plain, "{}"))
	f.tools.SetRepositories(scanPolicyStore{allowed: `["acme/other"]`})
	require.ErrorContains(t, run(f.admin, `{"identifier":"HIDDEN-1"}`), "issue not found")
	require.ErrorContains(t, run(f.admin, "{}"), "policy")
	require.Zero(t, f.source.calls.Load())
	f.tools.SetRepositories(nil)
	require.NoError(t, run(f.admin, "{}"))
	require.EqualValues(t, 1, f.source.calls.Load())
}

func TestScanSuggestsLinkedRepositoryWithoutOpeningIt(t *testing.T) {
	f := newScanFixture(t)
	f.tools.scanRepos = linkedRepoFake{repos: []domain.GitHubRepoModel{{FullName: "acme/app", DefaultBranch: "main"}}}
	for _, name := range []string{"acne/app", "app/app", "acme/appp"} {
		args, _ := json.Marshal(map[string]string{"repository": name})
		_, _, err := f.tools.run(t.Context(), f.ws, f.admin, "repo_list_files", string(args))
		require.ErrorContains(t, err, "did you mean acme/app?")
		_, _, err = f.tools.run(t.Context(), f.ws, f.plain, "repo_list_files", string(args))
		require.Error(t, err)
		require.NotContains(t, err.Error(), "acme/app")
	}
	require.Zero(t, f.source.calls.Load())
}

type unpublishedWork struct{ assistantAgentWork }

func (unpublishedWork) IssueAgentWork(context.Context, uuid.UUID, uuid.UUID) ([]repository.IssueAgentWork, error) {
	return []repository.IssueAgentWork{{State: "blocked", Reason: "waiting for dependency"}}, nil
}

func TestPRToolsExplainUnpublishedTask(t *testing.T) {
	f := newPRFixture()
	f.targets.target = nil
	f.tools.SetAgentWork(unpublishedWork{})
	for _, name := range []string{"get_pull_request", "get_pull_request_checks"} {
		_, err := f.run(t, name, `{"identifier":"ENG-1"}`)
		require.ErrorContains(t, err, "blocked")
		require.ErrorContains(t, err, "waiting for dependency")
		require.ErrorContains(t, err, "Next step")
	}
	require.Empty(t, f.github.calls)
}
