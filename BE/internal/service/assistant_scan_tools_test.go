package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

type scanFixture struct {
	tools  *AssistantTools
	source *snapshotFake
	ws     uuid.UUID
	// admin names repositories directly; plain is a member, who scans only
	// through an issue they can see.
	admin uuid.UUID
	plain uuid.UUID
	guest uuid.UUID
}

func newScanFixture(t *testing.T, entries ...tarEntry) *scanFixture {
	t.Helper()
	if len(entries) == 0 {
		entries = []tarEntry{
			{name: "acme-app-1/go.mod", body: "module acme\n"},
			{name: "acme-app-1/BE/main.go", body: "package main\n\n// Server starts the API.\nfunc main() {\n\tServe()\n}\n\ntype Server struct{}\n\nfunc (s *Server) Serve() error { return nil }\n"},
			{name: "acme-app-1/BE/main_test.go", body: "package main\n\nfunc TestServe(t *testing.T) {}\n"},
			{name: "acme-app-1/UI/src/app.ts", body: "export function render() {}\nexport const API_URL = '/api'\nexport class Store {}\ninterface Hidden {}\n"},
			{name: "acme-app-1/tools/gen.py", body: "import os\n\ndef generate():\n    pass\n\nclass Builder:\n    pass\n"},
			{name: "acme-app-1/logo.png", body: "\x89PNG\x00\x00serve"},
		}
	}
	f := &scanFixture{source: &snapshotFake{archive: tarball(t, entries...)}, ws: uuid.New(), admin: uuid.New(), plain: uuid.New(), guest: uuid.New()}
	members := prMembers{f.admin: domain.RoleAdmin, f.plain: domain.RoleMember, f.guest: domain.RoleGuest}
	f.tools = NewAssistantTools(prIssues{}, nil, members, nil)
	f.tools.SetRepositoryScans(newTestCheckouts(t, f.source, testScanLimits()), releaseRepos{})
	return f
}

func (f *scanFixture) run(t *testing.T, name string, args map[string]any) (map[string]any, error) {
	t.Helper()
	if _, ok := args["repository"]; !ok {
		args["repository"] = "acme/app"
	}
	data, _ := json.Marshal(args)
	result, _, err := f.tools.run(context.Background(), f.ws, f.admin, name, string(data))
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

func TestScanToolsAreReadOnlyDeclareContentsReadAndNeedWiring(t *testing.T) {
	names := map[string]bool{}
	for _, tool := range assistantScanToolCatalog {
		names[tool.name] = true
		require.True(t, tool.scan, tool.name)
		require.False(t, tool.write || tool.approval, "%s is read-only and acts at once", tool.name)
		require.False(t, (&AssistantTools{}).available(tool), "%s is offered only once wired", tool.name)
		require.Equal(t, map[string]string{"contents": "read"}, assistantActionPolicies[tool.name].github, tool.name)
		require.Equal(t, roleMember, assistantActionPolicies[tool.name].role)
	}
	require.Equal(t, map[string]bool{"repo_list_files": true, "repo_glob": true, "repo_grep": true, "repo_read_file": true, "repo_symbols": true}, names)
}

func TestScanToolsRefuseGuestsAndUnlinkedRepositories(t *testing.T) {
	f := newScanFixture(t)
	_, _, err := f.tools.run(context.Background(), f.ws, f.guest, "repo_list_files", `{"repository":"acme/app"}`)
	require.Error(t, err)
	require.Contains(t, toolErrorText(err), "no longer have access")
	_, _, err = f.tools.run(context.Background(), f.ws, uuid.New(), "repo_list_files", `{"repository":"acme/app"}`)
	require.Error(t, err, "outsiders are refused")

	_, err = f.run(t, "repo_list_files", map[string]any{"repository": "evil/other"})
	require.Error(t, err)
	require.Contains(t, toolErrorText(err), "not linked")
	_, err = f.run(t, "repo_list_files", map[string]any{"ref": "--upload-pack=x"})
	require.Error(t, err)
	require.Contains(t, toolErrorText(err), "invalid ref")
	require.Zero(t, f.source.calls.Load(), "nothing is downloaded for a refused call")
}

// The scan token can read a repository the caller cannot see on GitHub, so a
// repository named directly is for workspace owners and admins; a member scans
// through an issue they can see. The Dev Machine allowlist bounds every scan.
func TestScanByRepositoryNameIsForAdminsAndFollowsTheAllowlist(t *testing.T) {
	f := newScanFixture(t)
	_, _, err := f.tools.run(context.Background(), f.ws, f.plain, "repo_read_file", `{"repository":"acme/app","path":"BE/main.go"}`)
	require.Error(t, err)
	require.Contains(t, toolErrorText(err), "only workspace owners and admins can scan a repository by name")
	require.Zero(t, f.source.calls.Load(), "nothing is downloaded for a member naming a repository")

	_, err = f.run(t, "repo_read_file", map[string]any{"path": "BE/main.go"})
	require.NoError(t, err, "an admin can name a linked repository")

	f = newScanFixture(t)
	f.tools.SetRepositories(scanPolicyStore{allowed: `["acme/other"]`})
	_, err = f.run(t, "repo_list_files", map[string]any{})
	require.Error(t, err)
	require.Contains(t, toolErrorText(err), "not allowed by the workspace's Dev Machine policy")
	require.Zero(t, f.source.calls.Load())
}

// scanPolicyStore is a workspace whose Dev Machine policy allows only some repositories.
type scanPolicyStore struct {
	assistantRepositoryStore
	allowed string
}

func (s scanPolicyStore) GetPolicy(context.Context, uuid.UUID) (*domain.DevMachineWorkspacePolicy, error) {
	return &domain.DevMachineWorkspacePolicy{AllowedRepositories: json.RawMessage(s.allowed)}, nil
}

func TestRepoListFilesAndGlobReportWhatTheyUsed(t *testing.T) {
	f := newScanFixture(t)
	all, err := f.run(t, "repo_list_files", map[string]any{})
	require.NoError(t, err)
	require.Equal(t, "acme/app", all["repository"])
	require.Equal(t, "main", all["ref"], "the default branch is scanned without a ref")
	require.Equal(t, scanSHA, all["commit"])
	require.EqualValues(t, 6, all["total_files"])

	under, err := f.run(t, "repo_list_files", map[string]any{"path": "BE/"})
	require.NoError(t, err)
	require.Equal(t, "BE", under["path"])
	require.Equal(t, []any{"BE/main.go", "BE/main_test.go"}, filePaths(under["files"]))

	globbed, err := f.run(t, "repo_glob", map[string]any{"pattern": "**/*_test.go"})
	require.NoError(t, err)
	require.Equal(t, "**/*_test.go", globbed["pattern"])
	require.Equal(t, []any{"BE/main_test.go"}, filePaths(globbed["files"]))
	require.EqualValues(t, 1, globbed["matched"])

	_, err = f.run(t, "repo_glob", map[string]any{"pattern": "[unclosed"})
	require.Error(t, err)
	_, err = f.run(t, "repo_list_files", map[string]any{"path": "../etc"})
	require.Error(t, err)
	require.EqualValues(t, 1, f.source.calls.Load(), "the tool calls share one checkout")
}

func filePaths(value any) []any {
	var paths []any
	for _, entry := range value.([]any) {
		paths = append(paths, entry.(map[string]any)["path"])
	}
	return paths
}

func TestRepoGrepFindsLinesSkipsBinariesAndCaps(t *testing.T) {
	f := newScanFixture(t)
	result, err := f.run(t, "repo_grep", map[string]any{"pattern": `Serve\(`, "glob": "BE/**"})
	require.NoError(t, err)
	require.Equal(t, `Serve\(`, result["pattern"])
	require.Equal(t, "BE/**", result["glob"])
	matches := result["matches"].([]any)
	require.Len(t, matches, 3, "main.go twice and TestServe(")
	first := matches[0].(map[string]any)
	require.Equal(t, "BE/main.go", first["path"])
	require.EqualValues(t, 5, first["line"])
	require.Equal(t, "\tServe()", first["text"])

	insensitive, err := f.run(t, "repo_grep", map[string]any{"pattern": "serve", "fixed": true, "case_insensitive": true})
	require.NoError(t, err)
	for _, match := range insensitive["matches"].([]any) {
		require.NotEqual(t, "logo.png", match.(map[string]any)["path"], "binary files are not searched")
	}
	limited, err := f.run(t, "repo_grep", map[string]any{"pattern": ".", "limit": 3})
	require.NoError(t, err)
	require.Len(t, limited["matches"], 3)
	require.Equal(t, true, limited["truncated"])

	_, err = f.run(t, "repo_grep", map[string]any{"pattern": "(unclosed"})
	require.Error(t, err)
	require.Contains(t, toolErrorText(err), "invalid pattern")
}

func TestRepoGrepResultStaysWithinTheBudget(t *testing.T) {
	var entries []tarEntry
	for i := range 60 {
		entries = append(entries, tarEntry{name: fmt.Sprintf("r/f%02d.txt", i), body: strings.Repeat("needle "+strings.Repeat("x", 2000)+"\n", 3)})
	}
	f := newScanFixture(t, entries...)
	result, err := f.run(t, "repo_grep", map[string]any{"pattern": "needle", "limit": 100})
	require.NoError(t, err)
	require.Equal(t, true, result["truncated"])
	for _, match := range result["matches"].([]any) {
		require.LessOrEqual(t, len(match.(map[string]any)["text"].(string)), 400, "long lines are cut")
	}
}

func TestRepoReadFileReadsLineRangesWithinCaps(t *testing.T) {
	var long strings.Builder
	for i := 1; i <= 1000; i++ {
		fmt.Fprintf(&long, "line %d\n", i)
	}
	f := newScanFixture(t, tarEntry{name: "r/a.go", body: "one\ntwo\nthree\nfour\n"}, tarEntry{name: "r/long.txt", body: long.String()}, tarEntry{name: "r/bin", body: "\x00\x01"})
	result, err := f.run(t, "repo_read_file", map[string]any{"path": "a.go", "start_line": 2, "end_line": 3})
	require.NoError(t, err)
	require.Equal(t, "a.go", result["path"])
	require.Equal(t, "two\nthree", result["content"])
	require.EqualValues(t, 2, result["start_line"])
	require.EqualValues(t, 3, result["end_line"])
	require.EqualValues(t, 4, result["total_lines"])

	page, err := f.run(t, "repo_read_file", map[string]any{"path": "long.txt"})
	require.NoError(t, err)
	require.Equal(t, true, page["truncated"])
	require.EqualValues(t, 1, page["start_line"])
	require.Less(t, page["end_line"].(float64), float64(1000))
	require.EqualValues(t, 1000, page["total_lines"])

	for _, bad := range []string{"../a.go", "/etc/passwd", "missing.go", ".git/config", "bin"} {
		_, err := f.run(t, "repo_read_file", map[string]any{"path": bad})
		require.Error(t, err, bad)
	}
	_, err = f.run(t, "repo_read_file", map[string]any{"path": "a.go", "start_line": 9})
	require.Error(t, err)
}

func TestRepoSymbolsListsDeclarationsPerLanguage(t *testing.T) {
	f := newScanFixture(t)
	result, err := f.run(t, "repo_symbols", map[string]any{})
	require.NoError(t, err)
	got := map[string]string{}
	for _, entry := range result["symbols"].([]any) {
		symbol := entry.(map[string]any)
		got[symbol["path"].(string)+":"+symbol["name"].(string)] = symbol["kind"].(string)
	}
	require.Equal(t, "function", got["BE/main.go:main"])
	require.Equal(t, "type", got["BE/main.go:Server"])
	require.Equal(t, "method", got["BE/main.go:Serve"])
	require.Equal(t, "function", got["BE/main_test.go:TestServe"])
	require.Equal(t, "function", got["UI/src/app.ts:render"])
	require.Equal(t, "const", got["UI/src/app.ts:API_URL"])
	require.Equal(t, "class", got["UI/src/app.ts:Store"])
	require.Equal(t, "interface", got["UI/src/app.ts:Hidden"])
	require.Equal(t, "function", got["tools/gen.py:generate"])
	require.Equal(t, "class", got["tools/gen.py:Builder"])

	one, err := f.run(t, "repo_symbols", map[string]any{"path": "tools/gen.py"})
	require.NoError(t, err)
	require.Equal(t, "tools/gen.py", one["path"])
	require.Len(t, one["symbols"], 2)
}
