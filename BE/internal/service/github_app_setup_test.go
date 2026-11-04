package service

import (
	"testing"
	"time"

	"github.com/kuayle/kuayle-backend/internal/dto"
	gh "github.com/kuayle/kuayle-backend/pkg/github"
	"github.com/stretchr/testify/require"
)

// grantAll returns events and permissions covering everything Kuayle needs.
func grantAll() ([]string, map[string]string) {
	permissions := map[string]string{}
	for name, level := range GitHubAppPermissions() {
		permissions[name] = level
	}
	return GitHubAppEvents(), permissions
}

func setupApp(owner, ownerType string, events []string, permissions map[string]string) *gh.App {
	app := &gh.App{Slug: "kuayle-acme", Events: events, Permissions: permissions}
	app.Owner.Login, app.Owner.Type = owner, ownerType
	return app
}

func setupInstallation(events []string, permissions map[string]string) *gh.Installation {
	return &gh.Installation{ID: 77, HTMLURL: "https://github.com/settings/installations/77", Events: events, Permissions: permissions}
}

func without[T comparable](items []T, drop T) []T {
	out := []T{}
	for _, item := range items {
		if item != drop {
			out = append(out, item)
		}
	}
	return out
}

// The manifest is the single source of what Kuayle subscribes to; the setup
// check must compare against exactly that list.
func TestManifestUsesTheSharedGitHubAppRequirements(t *testing.T) {
	manifest, _ := (&GitHubService{frontendURL: "https://kuayle.test"}).GetManifest([16]byte{}, "acme")
	require.Equal(t, GitHubAppEvents(), manifest["default_events"])
	require.Equal(t, GitHubAppPermissions(), manifest["default_permissions"])
	require.Contains(t, GitHubAppEvents(), "pull_request_review")
	require.Contains(t, GitHubAppEvents(), "check_suite")
	require.Equal(t, "read", GitHubAppPermissions()["checks"])

	// Callers get copies, so nobody can change the requirements in place.
	GitHubAppPermissions()["checks"] = "none"
	events := GitHubAppEvents()
	events[0] = "changed"
	require.Equal(t, "read", GitHubAppPermissions()["checks"])
	require.NotEqual(t, "changed", GitHubAppEvents()[0])
}

func TestEvaluateGitHubAppSetup(t *testing.T) {
	events, permissions := grantAll()
	withPermission := func(name, level string) map[string]string {
		out := map[string]string{}
		for k, v := range permissions {
			out[k] = v
		}
		if level == "" {
			delete(out, name)
		} else {
			out[name] = level
		}
		return out
	}

	t.Run("all granted", func(t *testing.T) {
		got := evaluateGitHubAppSetup(setupApp("alice", "User", events, permissions), setupInstallation(events, permissions))
		require.Equal(t, dto.GitHubAppSetupOK, got.State)
		require.Empty(t, got.MissingEvents)
		require.Empty(t, got.MissingPermissions)
	})

	t.Run("app lacks an event", func(t *testing.T) {
		appEvents := without(events, "pull_request_review")
		got := evaluateGitHubAppSetup(setupApp("alice", "User", appEvents, permissions), setupInstallation(appEvents, permissions))
		require.Equal(t, dto.GitHubAppSetupAppMissing, got.State)
		require.Equal(t, []string{"pull_request_review"}, got.MissingEvents)
		require.Empty(t, got.MissingPermissions)
	})

	t.Run("app lacks a permission and an event", func(t *testing.T) {
		appEvents := without(events, "check_suite")
		appPermissions := withPermission("checks", "")
		got := evaluateGitHubAppSetup(setupApp("alice", "User", appEvents, appPermissions), setupInstallation(appEvents, appPermissions))
		require.Equal(t, dto.GitHubAppSetupAppMissing, got.State)
		require.Equal(t, []string{"check_suite"}, got.MissingEvents)
		require.Equal(t, []dto.GitHubAppPermissionRequirement{{Name: "checks", Level: "read"}}, got.MissingPermissions)
	})

	t.Run("read does not satisfy write", func(t *testing.T) {
		appPermissions := withPermission("pull_requests", "read")
		got := evaluateGitHubAppSetup(setupApp("alice", "User", events, appPermissions), setupInstallation(events, appPermissions))
		require.Equal(t, dto.GitHubAppSetupAppMissing, got.State)
		require.Equal(t, []dto.GitHubAppPermissionRequirement{{Name: "pull_requests", Level: "write"}}, got.MissingPermissions)
	})

	t.Run("write satisfies read", func(t *testing.T) {
		appPermissions := withPermission("checks", "write")
		got := evaluateGitHubAppSetup(setupApp("alice", "User", events, appPermissions), setupInstallation(events, appPermissions))
		require.Equal(t, dto.GitHubAppSetupOK, got.State)
	})

	t.Run("installation has not accepted the update", func(t *testing.T) {
		got := evaluateGitHubAppSetup(
			setupApp("alice", "User", events, permissions),
			setupInstallation(without(events, "check_suite"), withPermission("checks", "")),
		)
		require.Equal(t, dto.GitHubAppSetupInstallationPending, got.State)
		require.Equal(t, []string{"check_suite"}, got.MissingEvents)
		require.Equal(t, []dto.GitHubAppPermissionRequirement{{Name: "checks", Level: "read"}}, got.MissingPermissions)
		require.Equal(t, "https://github.com/settings/installations/77", got.InstallationURL)
	})
}

func TestGitHubAppSettingsURLDependsOnOwnerType(t *testing.T) {
	events, permissions := grantAll()
	user := evaluateGitHubAppSetup(setupApp("alice", "User", events, permissions), setupInstallation(events, permissions))
	require.Equal(t, "https://github.com/settings/apps/kuayle-acme/permissions", user.AppSettingsURL)

	org := evaluateGitHubAppSetup(setupApp("acme", "Organization", events, permissions), setupInstallation(events, permissions))
	require.Equal(t, "https://github.com/organizations/acme/settings/apps/kuayle-acme/permissions", org.AppSettingsURL)
}

// Only GitHub web pages may be offered as links; anything else from the API
// response is dropped rather than rendered.
func TestGitHubAppSetupDropsUnsafeLinks(t *testing.T) {
	events, permissions := grantAll()
	app := setupApp("acme/../evil", "Organization", events, permissions)
	app.Slug = "kuayle?x=1"
	inst := setupInstallation(events, permissions)
	inst.HTMLURL = "https://attacker.example/installations/77"
	got := evaluateGitHubAppSetup(app, inst)
	require.Empty(t, got.AppSettingsURL)
	require.Empty(t, got.InstallationURL)
}

// The fix is two steps on GitHub: the app's owner saves the new events and
// permissions (and confirms GitHub's "Update permissions" dialog), then the
// installation's account accepts them. Each re-check reflects where it is.
func TestGitHubAppSetupFollowsTheFixSequence(t *testing.T) {
	events, permissions := grantAll()
	oldEvents := without(without(events, "check_suite"), "pull_request_review")
	oldPermissions := GitHubAppPermissions()
	delete(oldPermissions, "checks")
	inst := setupInstallation(oldEvents, oldPermissions)
	inst.Account.Login = "acme"

	step := evaluateGitHubAppSetup(setupApp("acme", "Organization", oldEvents, oldPermissions), inst)
	require.Equal(t, dto.GitHubAppSetupAppMissing, step.State)
	require.Equal(t, []string{"pull_request_review", "check_suite"}, step.MissingEvents)
	require.Equal(t, []dto.GitHubAppPermissionRequirement{{Name: "checks", Level: "read"}}, step.MissingPermissions)

	// Saved on GitHub: the app requests everything, the installation has not accepted.
	step = evaluateGitHubAppSetup(setupApp("acme", "Organization", events, permissions), inst)
	require.Equal(t, dto.GitHubAppSetupInstallationPending, step.State)
	require.Equal(t, "acme", step.AccountLogin)
	require.Equal(t, "https://github.com/settings/installations/77", step.InstallationURL)

	// Accepted on GitHub.
	step = evaluateGitHubAppSetup(setupApp("acme", "Organization", events, permissions), setupInstallation(events, permissions))
	require.Equal(t, dto.GitHubAppSetupOK, step.State)
	require.Empty(t, step.MissingEvents)
	require.Empty(t, step.MissingPermissions)
}

// Events that need no new permission are applied by GitHub without an
// acceptance: the live installation already lists them, so the check is OK
// rather than pending.
func TestGitHubAppSetupEventsAppliedWithoutAcceptanceAreOK(t *testing.T) {
	events, permissions := grantAll()
	got := evaluateGitHubAppSetup(setupApp("alice", "User", events, permissions), setupInstallation(events, permissions))
	require.Equal(t, dto.GitHubAppSetupOK, got.State)
}

// A suspended installation receives no webhooks, whatever it was granted.
func TestGitHubAppSetupReportsSuspendedInstallation(t *testing.T) {
	events, permissions := grantAll()
	inst := setupInstallation(events, permissions)
	suspended := time.Now()
	inst.SuspendedAt = &suspended
	got := evaluateGitHubAppSetup(setupApp("alice", "User", events, permissions), inst)
	require.Equal(t, dto.GitHubAppSetupInstallationSuspended, got.State)
	require.Equal(t, "https://github.com/settings/installations/77", got.InstallationURL)
}
