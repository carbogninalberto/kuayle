package service

import (
	"testing"

	gh "github.com/kuayle/kuayle-backend/pkg/github"
	"github.com/stretchr/testify/require"
)

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
		{"shared migration", []gh.CompareFile{{Filename: "BE/migrations/" + baseFile, Status: "modified"}}, false, []string{"base-commit", "base-be", "base-migrations"}},
		{"removed migration", []gh.CompareFile{{Filename: branchFile, Status: "removed"}}, false, nil},
		{"outside migration directory", []gh.CompareFile{{Filename: "docs/178_other.up.sql", Status: "added"}}, false, nil},
		{"truncated comparison", make([]gh.CompareFile, maxVerifiedFiles), true, []string{"head-commit", "head-be", "head-migrations", "base-commit", "base-be", "base-migrations"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls []string
			trees := map[string][]gh.TreeEntry{
				"base-commit":     {{Type: "tree", Path: "BE", SHA: "base-be"}},
				"base-be":         {{Type: "tree", Path: "migrations", SHA: "base-migrations"}},
				"base-migrations": {{Type: "blob", Path: baseFile}},
				"head-commit":     {{Type: "tree", Path: "BE", SHA: "head-be"}},
				"head-be":         {{Type: "tree", Path: "migrations", SHA: "head-migrations"}},
				"head-migrations": {{Type: "blob", Path: "178_bulk_issue_approvals.up.sql"}, {Type: "blob", Path: baseFile}},
			}
			repair, err := pullRequestMigrationRepair(func(ref string) ([]gh.TreeEntry, error) {
				calls = append(calls, ref)
				return trees[ref], nil
			}, "base-commit", "head-commit", "release/next", tc.files)
			require.NoError(t, err)
			require.Equal(t, tc.treeRefs, calls)
			if tc.clashes {
				require.Contains(t, repair, branchFile)
				require.Contains(t, repair, "BE/migrations/"+baseFile)
				require.Contains(t, repair, "origin/release/next")
			} else {
				require.Empty(t, repair)
			}
		})
	}
}
