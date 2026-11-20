package service

import (
	"net/http"
	"testing"

	gh "github.com/kuayle/kuayle-backend/pkg/github"
	"github.com/stretchr/testify/require"
)

func TestPullRequestStatusChecksMigrationsAtTheLiveBase(t *testing.T) {
	const branchPath = "BE/migrations/000178_bulk_issue_approvals.up.sql"
	const basePath = "BE/migrations/000178_saml_sso.down.sql"
	for _, tc := range []struct {
		name string
		code int
		tree string
	}{
		{"collision", 200, `{"tree":[{"type":"blob","path":"000178_saml_sso.down.sql"}]}`},
		{"unavailable tree", 503, `{}`},
		{"truncated tree", 200, `{"truncated":true,"tree":[]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &gh.Client{}
			client.UseHTTPClient(&http.Client{Transport: setupTransport(func(r *http.Request) (*http.Response, error) {
				switch r.URL.Path {
				case "/repos/acme/app/pulls/302":
					return setupResponse(200, `{"state":"open","head":{"sha":"task-head"},"base":{"ref":"release/next","sha":"old-base"}}`), nil
				case "/repos/acme/app/compare/release/next...task-head":
					return setupResponse(200, `{"ahead_by":1,"behind_by":1,"base_commit":{"sha":"live-base"},"merge_base_commit":{"sha":"old-base"},"files":[{"status":"added","filename":"`+branchPath+`"}]}`), nil
				case "/repos/acme/app/git/trees/live-base":
					return setupResponse(200, `{"tree":[{"path":"BE","type":"tree","sha":"backend-tree"}]}`), nil
				case "/repos/acme/app/git/trees/backend-tree":
					return setupResponse(200, `{"tree":[{"path":"migrations","type":"tree","sha":"migration-tree"}]}`), nil
				case "/repos/acme/app/git/trees/migration-tree":
					return setupResponse(tc.code, tc.tree), nil
				default:
					t.Errorf("unexpected GitHub request %s", r.URL.Path)
					return setupResponse(404, `{}`), nil
				}
			})})
			status, err := readPullRequestStatus(client, "fixture-token", "acme", "app", 302)
			require.NoError(t, err)
			require.Equal(t, 1, status.Ahead)
			require.Equal(t, 1, status.Behind)
			require.Equal(t, "old-base", status.MergeBase)
			require.Equal(t, tc.name != "collision", status.migrationCheckUnavailable)
			if tc.name == "collision" {
				require.Contains(t, status.migrationRepair, branchPath)
				require.Contains(t, status.migrationRepair, basePath)
			} else {
				require.Empty(t, status.migrationRepair)
			}
		})
	}
}

func TestPullRequestMigrationRepairComparesVersionsWithTheBase(t *testing.T) {
	const branchFile = "BE/migrations/178_bulk_issue_approvals.up.sql"
	const baseFile = "000178_saml_sso.down.sql"
	for _, tc := range []struct {
		name     string
		files    []gh.CompareFile
		clashes  bool
		treeRefs []string
	}{
		{"different slugs and leading zeroes", []gh.CompareFile{{Filename: branchFile, Status: "added"}}, true, []string{"base-commit", "base-be", "base-migrations"}},
		{"same path added independently", []gh.CompareFile{{Filename: "BE/migrations/" + baseFile, Status: "added"}}, true, []string{"base-commit", "base-be", "base-migrations"}},
		{"shared migration", []gh.CompareFile{{Filename: "BE/migrations/" + baseFile, Status: "modified"}}, false, nil},
		{"removed migration", []gh.CompareFile{{Filename: branchFile, Status: "removed"}}, false, nil},
		{"outside migration directory", []gh.CompareFile{{Filename: "docs/178_other.up.sql", Status: "added"}}, false, nil},
		{"truncated comparison", make([]gh.CompareFile, maxVerifiedFiles), true, []string{"head-commit", "head-be", "head-migrations", "common", "base-commit", "base-be", "base-migrations"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls []string
			trees := map[string][]gh.TreeEntry{
				"base-commit":     {{Type: "tree", Path: "BE", SHA: "base-be"}},
				"base-be":         {{Type: "tree", Path: "migrations", SHA: "base-migrations"}},
				"base-migrations": {{Type: "blob", Path: baseFile}},
				"head-commit":     {{Type: "tree", Path: "BE", SHA: "head-be"}},
				"head-be":         {{Type: "tree", Path: "migrations", SHA: "head-migrations"}},
				"head-migrations": {{Type: "blob", Path: "178_bulk_issue_approvals.up.sql"}},
			}
			repair, err := pullRequestMigrationRepair(func(ref string) ([]gh.TreeEntry, error) {
				calls = append(calls, ref)
				return trees[ref], nil
			}, "base-commit", "head-commit", "common", "release/next", tc.files)
			require.NoError(t, err)
			require.Equal(t, tc.treeRefs, calls)
			if tc.clashes {
				if tc.name != "same path added independently" {
					require.Contains(t, repair, branchFile)
				}
				require.Contains(t, repair, "BE/migrations/"+baseFile)
				require.Contains(t, repair, "origin/release/next")
			} else {
				require.Empty(t, repair)
			}
		})
	}
}
