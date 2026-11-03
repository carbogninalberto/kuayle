package service

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/stretchr/testify/require"
)

type updaterFake struct {
	configured bool
	status     dto.SystemUpdateStatusResponse
	statusCode int
	statusErr  error
	startCode  int
	startErr   error
	started    int
}

func (u *updaterFake) Configured() bool { return u.configured }
func (u *updaterFake) Status(context.Context) (dto.SystemUpdateStatusResponse, int, error) {
	code := u.statusCode
	if code == 0 {
		code = http.StatusOK
	}
	return u.status, code, u.statusErr
}
func (u *updaterFake) Start(context.Context) (dto.SystemUpdateStartResponse, int, error) {
	u.started++
	code := u.startCode
	if code == 0 {
		code = http.StatusAccepted
	}
	return dto.SystemUpdateStartResponse{}, code, u.startErr
}

var (
	adminUser = uuid.New()
	plainUser = uuid.New()
)

func newSystemTools(updater *updaterFake, version string, releases ...instanceRelease) (*AssistantTools, *AssistantSystem) {
	system := NewAssistantSystem(updater, func(id uuid.UUID) bool { return id == adminUser }, version)
	system.releases = func(context.Context) ([]instanceRelease, error) { return releases, nil }
	return &AssistantTools{system: system}, system
}

var releaseList = []instanceRelease{
	{Tag: "v0.3.0", Body: "Faster boards", URL: "https://github.com/x/releases/tag/v0.3.0"},
	{Tag: "v0.2.9", Body: "Fixes"},
	{Tag: "v0.4.0-rc1", Body: "Preview", Prerelease: true},
	{Tag: "v0.2.8", Body: "Old"},
}

func TestUpgradeToolsAreOfferedOnlyOnceWired(t *testing.T) {
	var upgrade, version assistantTool
	for _, tool := range assistantSystemToolCatalog {
		switch tool.name {
		case "upgrade_instance":
			upgrade = tool
		case "get_instance_version":
			version = tool
		}
	}
	require.True(t, upgrade.approval, "an upgrade always needs approval in Kuayle")
	require.False(t, version.approval)
	require.False(t, (&AssistantTools{}).available(upgrade))
	tools, _ := newSystemTools(&updaterFake{}, "0.2.8")
	require.True(t, tools.available(upgrade))
}

func TestUpgradeIsRefusedForNonAdministratorsBeforeAnythingIsAsked(t *testing.T) {
	updater := &updaterFake{configured: true}
	tools, _ := newSystemTools(updater, "0.2.8", releaseList...)
	_, _, err := tools.prepareUpgrade(context.Background(), plainUser, `{}`)
	require.ErrorContains(t, err, "only an instance administrator can upgrade")
	require.Zero(t, updater.started)
}

func TestPrepareUpgradeFailuresNameTheNextStep(t *testing.T) {
	for name, tc := range map[string]struct {
		updater *updaterFake
		version string
		want    string
	}{
		"updater not configured": {&updaterFake{}, "0.2.8", "UPDATER_URL and UPDATER_TOKEN"},
		"updater unreachable":    {&updaterFake{configured: true, statusErr: errors.New("refused")}, "0.2.8", "updater container is running"},
		"upgrade running":        {&updaterFake{configured: true, status: dto.SystemUpdateStatusResponse{Running: true}}, "0.2.8", "already running"},
		"already latest":         {&updaterFake{configured: true}, "0.3.0", "already runs the latest release (0.3.0)"},
		"development build":      {&updaterFake{configured: true}, "dev", "development build"},
	} {
		t.Run(name, func(t *testing.T) {
			tools, _ := newSystemTools(tc.updater, tc.version, releaseList...)
			_, _, err := tools.prepareUpgrade(context.Background(), adminUser, `{}`)
			require.ErrorContains(t, err, tc.want)
			require.Zero(t, tc.updater.started)
		})
	}
}

func TestPrepareUpgradeBindsTheVersionsAndStartsNothing(t *testing.T) {
	updater := &updaterFake{configured: true}
	tools, _ := newSystemTools(updater, "v0.2.8", releaseList...)
	issue, payload, err := tools.prepareUpgrade(context.Background(), adminUser, `{}`)
	require.NoError(t, err)
	require.Equal(t, uuid.Nil, issue, "an upgrade belongs to no issue")
	require.Equal(t, "0.2.8", payload.CurrentVersion)
	require.Equal(t, "0.3.0", payload.LatestVersion, "a prerelease is never offered")
	require.Contains(t, payload.Changelog, "Faster boards")
	require.NotContains(t, payload.Changelog, "Preview")
	require.NotContains(t, payload.Changelog, "Old")
	require.Zero(t, updater.started)
	_, _, err = tools.prepareUpgrade(context.Background(), adminUser, `{"force":true}`)
	require.ErrorContains(t, err, "invalid arguments")
}

func TestStartUpgradeRechecksAuthorityAndReportsEachFailure(t *testing.T) {
	payload := domain.UpgradePayload{CurrentVersion: "0.2.8", LatestVersion: "0.3.0"}
	for name, tc := range map[string]struct {
		actor   uuid.UUID
		updater *updaterFake
		version string
		want    string
	}{
		"demoted since asking":  {plainUser, &updaterFake{configured: true}, "0.2.8", "Only an instance administrator"},
		"version moved":         {adminUser, &updaterFake{configured: true}, "0.2.9", "version changed"},
		"updater removed":       {adminUser, &updaterFake{}, "0.2.8", "UPDATER_URL and UPDATER_TOKEN"},
		"updater unreachable":   {adminUser, &updaterFake{configured: true, startErr: errors.New("refused")}, "0.2.8", "updater container is running"},
		"already running":       {adminUser, &updaterFake{configured: true, startCode: http.StatusConflict}, "0.2.8", "already running"},
		"updater rejects token": {adminUser, &updaterFake{configured: true, startCode: http.StatusUnauthorized}, "0.2.8", "UPDATER_TOKEN"},
		"updater fails":         {adminUser, &updaterFake{configured: true, startCode: http.StatusInternalServerError}, "0.2.8", "failed to start the upgrade"},
	} {
		t.Run(name, func(t *testing.T) {
			_, system := newSystemTools(tc.updater, tc.version)
			announced := false
			system.OnUpdateStarted(func() { announced = true })
			require.Contains(t, system.startUpgrade(context.Background(), tc.actor, payload), tc.want)
			require.False(t, announced)
			if tc.actor == plainUser || tc.version != "0.2.8" || !tc.updater.configured {
				require.Zero(t, tc.updater.started, "the updater is not called")
			}
		})
	}
}

func TestStartUpgradeRunsTheUpdaterOnceAndAnnouncesIt(t *testing.T) {
	updater := &updaterFake{configured: true}
	_, system := newSystemTools(updater, "0.2.8")
	announced := false
	system.OnUpdateStarted(func() { announced = true })
	require.Empty(t, system.startUpgrade(context.Background(), adminUser, domain.UpgradePayload{CurrentVersion: "0.2.8", LatestVersion: "0.3.0"}))
	require.Equal(t, 1, updater.started)
	require.True(t, announced)
}

func TestInstanceVersionShowsUpgradeStateOnlyToAdministrators(t *testing.T) {
	finished := time.Now()
	updater := &updaterFake{configured: true, status: dto.SystemUpdateStatusResponse{State: "failed", Error: "migration 105 failed", FinishedAt: &finished}}
	tools, _ := newSystemTools(updater, "0.2.8", releaseList...)

	result, err := tools.instanceVersion(context.Background(), plainUser, `{}`)
	require.NoError(t, err)
	info := result.(map[string]any)
	require.Equal(t, "0.2.8", info["current_version"])
	require.Equal(t, "0.3.0", info["latest_version"])
	require.Equal(t, true, info["update_available"])
	require.Equal(t, false, info["can_upgrade"])
	require.NotContains(t, info, "upgrade", "updater output is for administrators")
	require.Contains(t, info["next_step"], "instance administrator")

	result, err = tools.instanceVersion(context.Background(), adminUser, `{}`)
	require.NoError(t, err)
	upgrade := result.(map[string]any)["upgrade"].(map[string]any)
	require.Equal(t, "failed", upgrade["state"])
	require.Equal(t, "migration 105 failed", upgrade["error"])
	require.Contains(t, upgrade["next_step"], "Settings → Version")
}

func TestInstanceVersionSurvivesAnUnavailableUpdaterAndReleaseList(t *testing.T) {
	tools, system := newSystemTools(&updaterFake{}, "0.2.8")
	system.releases = func(context.Context) ([]instanceRelease, error) { return nil, errors.New("offline") }
	result, err := tools.instanceVersion(context.Background(), adminUser, `{}`)
	require.NoError(t, err)
	info := result.(map[string]any)
	require.Contains(t, info["latest_error"], "release list could not be reached")
	require.Contains(t, info["upgrade"].(map[string]any)["next_step"], "UPDATER_URL")

	tools, _ = newSystemTools(&updaterFake{configured: true, statusErr: errors.New("restarting")}, "0.2.8", releaseList...)
	result, err = tools.instanceVersion(context.Background(), adminUser, `{}`)
	require.NoError(t, err)
	require.Equal(t, "unavailable", result.(map[string]any)["upgrade"].(map[string]any)["state"])
}

func TestCompareVersions(t *testing.T) {
	require.Equal(t, -1, compareVersions("0.2.8", "v0.3.0"))
	require.Equal(t, 1, compareVersions("0.10.0", "0.9.9"))
	require.Equal(t, 0, compareVersions("v1.0", "1.0.0-rc1"))
}

func succeededUpgrade(t *testing.T, tools *AssistantTools, system *AssistantSystem) map[string]any {
	t.Helper()
	result, err := tools.instanceVersion(context.Background(), adminUser, `{}`)
	require.NoError(t, err)
	return result.(map[string]any)["upgrade"].(map[string]any)
}

func TestFinalVersionAndHealthAreReportedOnlyWhenVerified(t *testing.T) {
	succeeded := &updaterFake{configured: true, status: dto.SystemUpdateStatusResponse{State: "succeeded"}}

	// The running binary is the latest release and the probe passes.
	tools, system := newSystemTools(succeeded, "0.3.0", releaseList...)
	system.SetHealthCheck(func(context.Context) error { return nil })
	upgrade := succeededUpgrade(t, tools, system)
	require.Equal(t, "passed", upgrade["health_check"])
	require.Equal(t, "0.3.0", upgrade["final_version"])
	require.NotContains(t, upgrade, "warning")

	// No probe: the health check is not claimed.
	tools, _ = newSystemTools(succeeded, "0.3.0", releaseList...)
	upgrade = succeededUpgrade(t, tools, nil)
	require.Contains(t, upgrade["health_check"], "not verified")

	// A failing probe is reported with a next step.
	tools, system = newSystemTools(succeeded, "0.3.0", releaseList...)
	system.SetHealthCheck(func(context.Context) error { return errors.New("db down") })
	require.Contains(t, succeededUpgrade(t, tools, system)["health_check"], "failed: db down")

	// Finished, yet still the old release: no final version is claimed.
	tools, system = newSystemTools(succeeded, "0.2.8", releaseList...)
	system.SetHealthCheck(func(context.Context) error { return nil })
	upgrade = succeededUpgrade(t, tools, system)
	require.Equal(t, "unconfirmed", upgrade["final_version"])
	require.Contains(t, upgrade["warning"], "still runs 0.2.8")

	// The release list is unreachable: the running version is known, the final one is not.
	tools, system = newSystemTools(succeeded, "0.3.0")
	system.releases = func(context.Context) ([]instanceRelease, error) { return nil, errors.New("offline") }
	upgrade = succeededUpgrade(t, tools, system)
	require.Equal(t, "unknown", upgrade["final_version"])
	require.Equal(t, "0.3.0", upgrade["running_version"])
}

// The request path the model uses: an upgrade call reaches prepareUpgrade, so a
// non-administrator is refused with the real reason, never "unknown tool".
func TestRequestApprovalRoutesUpgradeCallsThroughPrepareUpgrade(t *testing.T) {
	updater := &updaterFake{configured: true}
	tools, _ := newSystemTools(updater, "0.2.8", releaseList...)
	service := &ConversationService{tools: tools}
	attempt := domain.ConversationAttempt{Conversation: domain.Conversation{CreatorID: plainUser}}
	_, _, err := service.requestApproval(context.Background(), attempt, uuid.New(), "upgrade_instance", `{}`)
	require.ErrorContains(t, err, "only an instance administrator can upgrade")
	require.Zero(t, updater.started)
}

// An upgrade approval has no issue; the database stores NULL and it scans as the zero ID.
func TestApprovalWithoutIssueScansAsNil(t *testing.T) {
	var approval domain.ConversationApproval
	require.NoError(t, approval.IssueID.Scan(nil))
	require.Equal(t, uuid.Nil, approval.IssueID)
}
