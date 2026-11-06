package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// A reply scans a repository and runs research sub-tasks; each step is
// recorded with what it read, and a sub-task's own tool calls appear in the
// spawn step's result rather than as separate steps.
func TestResearchToolsRecordWhatTheyReadInTheConversation(t *testing.T) {
	f := newToolFixture(t)
	// Naming a repository directly is for owners and admins.
	_, err := f.db.Exec(`UPDATE workspace_members SET role='admin' WHERE workspace_id=$1 AND user_id=$2`, f.ws, f.user)
	require.NoError(t, err)
	source := &snapshotFake{archive: tarball(t,
		tarEntry{name: "acme-app-1/BE/main.go", body: "package main\n\nfunc main() { Serve() }\n"},
		tarEntry{name: "acme-app-1/.env", body: "SECRET=1"},
		tarEntry{name: "acme-app-1/.gitignore", body: ".env\n"},
	)}
	f.service.tools.SetRepositoryScans(newTestCheckouts(t, source, testScanLimits()), releaseRepos{})
	f.service.tools.SetWebSearch(researchFake{config: ResearchConfig{SearchProvider: "brave", SearchAPIKey: searchKey}}, &providerFake{name: "brave"})
	f.service.tools.SetSubagents(true)
	f.service.subagentCall = func(_ context.Context, model string, messages []chatCompletionMessage, _ []chatToolSpec) (string, []chatToolCall, error) {
		require.Equal(t, "model", model, "without a cheap model the conversation's model is used")
		if !hasToolResult(messages) {
			return "", []chatToolCall{toolCall("s1", "repo_read_file", `{"repository":"acme/app","path":"BE/main.go"}`)}, nil
		}
		return `{"summary":"main calls Serve","files":["BE/main.go:3"]}`, nil, nil
	}

	a := f.attempt(t, "Where is Serve called?")
	spawn := `{"tasks":[{"title":"Callers","instructions":"Find the callers of Serve in acme/app."}]}`
	provider, requests := scriptedProvider(t, toolCallStream("repo_grep", `{"repository":"acme/app","pattern":"Serve\\("}`), toolCallStream(subagentToolName, spawn), finalAnswer)
	require.NoError(t, f.service.converse(f.ctx, a, provider.URL, "key", []chatCompletionMessage{{Role: "user", Content: "Where is Serve called?"}}, func(string) error { return nil }))
	require.Len(t, *requests, 3)

	h, err := f.repo.History(f.ctx, f.ws, a.Conversation.ID, 0)
	require.NoError(t, err)
	calls := map[string]domain.ConversationToolCall{}
	for _, call := range h.Tools {
		calls[call.Name] = call
	}
	require.Len(t, calls, 2, "sub-task tool calls are not separate conversation steps")
	grep := calls["repo_grep"]
	require.Equal(t, "completed", grep.Status)
	var grepOutput struct {
		Repository string      `json:"repository"`
		Commit     string      `json:"commit"`
		Pattern    string      `json:"pattern"`
		Matches    []grepMatch `json:"matches"`
	}
	require.NoError(t, json.Unmarshal(*grep.Output, &grepOutput))
	require.Equal(t, "acme/app", grepOutput.Repository)
	require.Equal(t, scanSHA, grepOutput.Commit)
	require.Equal(t, `Serve\(`, grepOutput.Pattern)
	require.Equal(t, []grepMatch{{Path: "BE/main.go", Line: 3, Text: "func main() { Serve() }"}}, grepOutput.Matches)
	require.NotContains(t, string(*grep.Output), "SECRET", "an ignored file is never searched")

	spawned := calls[subagentToolName]
	require.Equal(t, "completed", spawned.Status)
	var out SubagentsOutput
	require.NoError(t, json.Unmarshal(*spawned.Output, &out))
	require.Len(t, out.Subagents, 1)
	sub := out.Subagents[0]
	require.Equal(t, "completed", sub.Status)
	require.Equal(t, "Callers", sub.Title)
	require.Equal(t, []string{"BE/main.go:3"}, sub.Files)
	require.Len(t, sub.Steps, 1)
	require.Equal(t, "repo_read_file", sub.Steps[0].Tool)
	require.True(t, sub.Steps[0].OK)
	require.Equal(t, 1, out.Budget.StepsUsed)
	require.EqualValues(t, 1, source.calls.Load(), "the reply and its sub-task share one checkout")

	// The parent model saw both results as untrusted data.
	last := (*requests)[2]["messages"].([]any)
	require.True(t, strings.HasPrefix(last[len(last)-1].(map[string]any)["content"].(string), `{"untrusted_data":`))
}
