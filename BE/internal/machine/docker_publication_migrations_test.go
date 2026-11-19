package machine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func addMigrationPair(f *scriptFixture, dir, stem string) {
	f.t.Helper()
	require.NoError(f.t, os.MkdirAll(filepath.Join(dir, "BE/migrations"), 0o755))
	f.writeAndCommit(dir, "BE/migrations/"+stem+".up.sql", "SELECT 1;\n", 0o644, kuayleIdentity)
	f.writeAndCommit(dir, "BE/migrations/"+stem+".down.sql", "SELECT 2;\n", 0o644, kuayleIdentity)
}

func TestPublicationRenumbersRacingMigrationPairs(t *testing.T) {
	f := newScriptFixture(t)
	addMigrationPair(f, f.checkout, "000177_existing")
	f.run(f.checkout, nil, "push", "-q", "origin", "HEAD:main")
	winner := filepath.Join(f.root, "winner")
	f.run(f.root, nil, "clone", "-q", f.origin, winner)
	f.run(winner, nil, "checkout", "-qb", "kuayle/winner")
	addMigrationPair(f, winner, "000178_saml_sso")
	winnerHead := f.run(winner, nil, "rev-parse", "HEAD")

	addMigrationPair(f, f.checkout, "000178_bulk_issue_approvals")
	addMigrationPair(f, f.checkout, "000179_bulk_issue_approvals_more")
	f.writeAndCommit(f.checkout, "runner.txt", "BE/migrations/000178_bulk_issue_approvals.up.sql\n000178_bulk_issue_approvals.down.sql\n000179_bulk_issue_approvals_more\n000178_bulk_issue_approvals_fixture\n", 0o755, kuayleIdentity)
	head := f.head()

	output, err := f.script(publishPushScript, "fixture-credential\n", "push", "acme/app", "kuayle/winner", winner, winnerHead, "main", "agent@kuayle.invalid")
	require.NoError(t, err, output)
	require.Equal(t, winnerHead, f.run(f.origin, nil, "rev-parse", "refs/heads/kuayle/winner"))
	f.run(winner, nil, "push", "-q", "origin", "HEAD:main")

	output, err = f.push(head, "")
	require.NoError(t, err, output)
	published, err := pushedCommit(output, head)
	require.NoError(t, err)
	require.Equal(t, f.originTip(), published)
	require.NotEqual(t, head, published)
	require.Equal(t, head, f.run(f.origin, nil, "rev-parse", published+"^"))
	require.Equal(t, winnerHead, f.run(f.origin, nil, "rev-parse", "main"))
	migrations := f.run(f.origin, nil, "ls-tree", "-r", "--name-only", published, "BE/migrations/")
	require.NotContains(t, migrations, "000178_bulk_issue_approvals.")
	require.NotContains(t, migrations, "000179_bulk_issue_approvals_more.")
	for _, stem := range []string{"000180_bulk_issue_approvals", "000181_bulk_issue_approvals_more"} {
		require.Equal(t, "SELECT 1;", f.run(f.origin, nil, "show", published+":BE/migrations/"+stem+".up.sql"))
		require.Equal(t, "SELECT 2;", f.run(f.origin, nil, "show", published+":BE/migrations/"+stem+".down.sql"))
	}
	require.Equal(t, "BE/migrations/000180_bulk_issue_approvals.up.sql\n000180_bulk_issue_approvals.down.sql\n000181_bulk_issue_approvals_more\n000178_bulk_issue_approvals_fixture", f.run(f.origin, nil, "show", published+":runner.txt"))
	require.True(t, strings.HasPrefix(f.run(f.origin, nil, "ls-tree", published, "runner.txt"), "100755 "))
	output, err = f.sync(published)
	require.NoError(t, err, output)
	require.Equal(t, published, f.head(), "the next visit takes the published renumbering")
	require.Equal(t, winnerHead, f.run(f.checkout, nil, "rev-parse", "origin/main"), "private-repo agents can inspect the refreshed base without a token")
	output, err = f.push(published, "")
	require.NoError(t, err, output)
	require.Equal(t, published, f.originTip(), "a retry keeps the published numbering and commit")
}

func TestPublicationHandsOffAnIncompleteMigrationPair(t *testing.T) {
	f := newScriptFixture(t)
	require.NoError(t, os.MkdirAll(filepath.Join(f.checkout, "BE/migrations"), 0o755))
	head := f.commitFile(f.checkout, "BE/migrations/000178_bulk_issue_approvals.up.sql", kuayleIdentity)
	winner := filepath.Join(f.root, "winner")
	f.run(f.root, nil, "clone", "-q", f.origin, winner)
	addMigrationPair(f, winner, "000178_saml_sso")
	f.run(winner, nil, "push", "-q", "origin", "main")

	output, err := f.push(head, "")
	require.NoError(t, err, output)
	_, err = pushedCommit(output, head)
	var blocked *checkoutSyncBlockedError
	require.ErrorAs(t, err, &blocked)
	require.Contains(t, blocked.detail, "BE/migrations/000178_bulk_issue_approvals.up.sql")
	require.Contains(t, blocked.detail, "BE/migrations/000178_saml_sso.up.sql")
	require.Contains(t, blocked.detail, "pair")
	require.True(t, f.fails(f.origin, "show-ref", "--verify", "refs/heads/kuayle/rec-1"))
	require.Equal(t, head, f.head())
}

func TestPublicationHandsOffAVersionOnlyMigrationReference(t *testing.T) {
	f := newScriptFixture(t)
	addMigrationPair(f, f.checkout, "000178_bulk_issue_approvals")
	head := f.writeAndCommit(f.checkout, "runner.go", "m.Migrate(178)\n", 0o644, kuayleIdentity)
	winner := filepath.Join(f.root, "winner")
	f.run(f.root, nil, "clone", "-q", f.origin, winner)
	addMigrationPair(f, winner, "000178_saml_sso")
	f.run(winner, nil, "push", "-q", "origin", "main")

	output, err := f.push(head, "")
	require.NoError(t, err, output)
	_, err = pushedCommit(output, head)
	require.ErrorContains(t, err, `"runner.go" refers to a migration by version alone`)
	require.ErrorContains(t, err, "000178_bulk_issue_approvals.up.sql")
	require.ErrorContains(t, err, "000178_saml_sso.up.sql")
	require.True(t, f.fails(f.origin, "show-ref", "--verify", "refs/heads/kuayle/rec-1"))
}
