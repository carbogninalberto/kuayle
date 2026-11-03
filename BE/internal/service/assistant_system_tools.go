package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/pkg/audit"
)

// The instance tools read the installed and latest release and start the same
// updater as Settings → Version → Upgrade. Upgrading is limited to instance
// administrators (the SYSADMIN_IDS users), checked when the request is made and
// again when it is approved.

const instanceReleasesURL = "https://raw.githubusercontent.com/carbogninalberto/kuayle/main/UI/static/releases.json"

type assistantSystemUpdater interface {
	Configured() bool
	Status(ctx context.Context) (dto.SystemUpdateStatusResponse, int, error)
	Start(ctx context.Context) (dto.SystemUpdateStartResponse, int, error)
}

// instanceRelease is one published release of the release manifest.
type instanceRelease struct {
	Tag        string `json:"tag_name"`
	URL        string `json:"html_url"`
	Body       string `json:"body"`
	Prerelease bool   `json:"prerelease"`
}

// AssistantSystem is what the instance tools and their approvals share.
type AssistantSystem struct {
	updater assistantSystemUpdater
	isAdmin func(uuid.UUID) bool
	// version is the installed release; "dev" for unstamped builds.
	version  string
	releases func(ctx context.Context) ([]instanceRelease, error)
	// announce tells connected clients that an update started.
	announce func()
}

func NewAssistantSystem(updater assistantSystemUpdater, isAdmin func(uuid.UUID) bool, version string) *AssistantSystem {
	if isAdmin == nil {
		isAdmin = func(uuid.UUID) bool { return false }
	}
	return &AssistantSystem{updater: updater, isAdmin: isAdmin, version: version, releases: fetchInstanceReleases}
}

// OnUpdateStarted registers a notification sent after the updater accepts a run.
func (s *AssistantSystem) OnUpdateStarted(announce func()) { s.announce = announce }

// SetSystem enables get_instance_version and upgrade_instance.
func (t *AssistantTools) SetSystem(system *AssistantSystem) { t.system = system }

// SetSystem enables approving upgrade_instance requests.
func (s *AssistantApprovals) SetSystem(system *AssistantSystem) { s.system = system }

var assistantSystemToolCatalog = []assistantTool{
	{name: "get_instance_version", system: true, description: "Read the installed Kuayle version, the latest available release, a short changelog of what the upgrade brings and, for instance administrators, the state of the upgrade. Call it to follow an upgrade after it was approved: the instance restarts during the upgrade, so retry after a short wait if the call fails.", parameters: `{"type":"object","properties":{},"additionalProperties":false}`},
	{name: "upgrade_instance", system: true, approval: true, description: "Ask the user to approve upgrading this Kuayle instance to the latest release with the updater behind Settings → Version → Upgrade. Only instance administrators can upgrade. Nothing happens until the user approves in Kuayle; never claim the upgrade started. Only call this when the user asked to upgrade. Afterwards, follow progress with get_instance_version.", parameters: `{"type":"object","properties":{},"additionalProperties":false}`},
}

func fetchInstanceReleases(ctx context.Context) ([]instanceRelease, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, instanceReleasesURL, nil)
	if err != nil {
		return nil, err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("release list returned %d", res.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	var list []instanceRelease
	if json.Unmarshal(data, &list) == nil {
		return list, nil
	}
	var wrapped struct {
		Releases []instanceRelease `json:"releases"`
	}
	if err = json.Unmarshal(data, &wrapped); err != nil {
		return nil, err
	}
	return wrapped.Releases, nil
}

// compareVersions orders dotted numeric versions; a prerelease or build suffix is ignored.
func compareVersions(left, right string) int {
	parse := func(v string) []int {
		v = strings.TrimPrefix(strings.TrimSpace(v), "v")
		if i := strings.IndexAny(v, "-+"); i >= 0 {
			v = v[:i]
		}
		var parts []int
		for _, p := range strings.Split(v, ".") {
			n, _ := strconv.Atoi(p)
			parts = append(parts, n)
		}
		return parts
	}
	a, b := parse(left), parse(right)
	for i := 0; i < len(a) || i < len(b); i++ {
		var x, y int
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

// instanceVersion is the release picture: installed, latest and what is new.
type instanceVersion struct {
	current, latest, latestURL, changelog string
	known, available                      bool
	err                                   error
}

func (s *AssistantSystem) versionInfo(ctx context.Context) instanceVersion {
	info := instanceVersion{current: strings.TrimPrefix(s.version, "v")}
	releases, err := s.releases(ctx)
	if err != nil {
		info.err = err
		return info
	}
	var newer []instanceRelease
	for _, release := range releases {
		if release.Prerelease {
			continue
		}
		if info.latest == "" || compareVersions(release.Tag, info.latest) > 0 {
			info.latest, info.latestURL = strings.TrimPrefix(release.Tag, "v"), release.URL
		}
	}
	info.known = info.latest != ""
	info.available = info.known && info.current != "dev" && compareVersions(info.latest, info.current) > 0
	if info.available {
		for _, release := range releases {
			if !release.Prerelease && compareVersions(release.Tag, info.current) > 0 {
				newer = append(newer, release)
			}
		}
		var summary strings.Builder
		for i, release := range newer {
			if i == 5 {
				break
			}
			fmt.Fprintf(&summary, "%s: %s\n", release.Tag, truncateRunes(strings.TrimSpace(release.Body), 400))
		}
		info.changelog = truncateRunes(strings.TrimSpace(summary.String()), 1500)
	}
	return info
}

// instanceVersion answers get_instance_version. The updater's state, which
// shows who upgraded and why it failed, goes only to instance administrators.
func (t *AssistantTools) instanceVersion(ctx context.Context, user uuid.UUID, arguments string) (any, error) {
	if err := decodeToolInput(arguments, &struct{}{}); err != nil {
		return nil, err
	}
	info := t.system.versionInfo(ctx)
	admin := t.system.isAdmin(user)
	result := map[string]any{"current_version": info.current, "can_upgrade": admin}
	switch {
	case info.err != nil:
		result["latest_error"] = "The release list could not be reached, so the latest version is unknown. Check that this server can reach raw.githubusercontent.com, or open Settings → Version."
	case info.known:
		result["latest_version"], result["update_available"], result["release_url"] = info.latest, info.available, info.latestURL
		if info.changelog != "" {
			result["changelog"] = info.changelog
		}
	}
	if info.current == "dev" {
		result["note"] = "This is a development build without a release version, so it cannot be compared with releases."
	}
	if !admin {
		if info.available {
			result["next_step"] = "Only an instance administrator can upgrade; ask one to upgrade."
		}
		return result, nil
	}
	result["upgrade"] = t.system.upgradeState(ctx, info)
	return result, nil
}

// upgradeState reports the updater's run with the health check once it ended.
func (s *AssistantSystem) upgradeState(ctx context.Context, info instanceVersion) map[string]any {
	if !s.updater.Configured() {
		return map[string]any{"state": "unavailable", "next_step": "The system updater is not configured. Set UPDATER_URL and UPDATER_TOKEN for the backend and run the updater service (see the self-hosting guide), or upgrade from the server with selfhosting/update.sh."}
	}
	status, code, err := s.updater.Status(ctx)
	if err != nil || code != http.StatusOK {
		return map[string]any{"state": "unavailable", "next_step": "The system updater did not answer. If an upgrade is running the services restart, so retry in a minute; otherwise check that the updater container is running."}
	}
	state := status.State
	if state == "" {
		state = LegacyUpdateState(status)
	}
	result := map[string]any{"state": state, "message": status.Message}
	if status.StartedAt != nil {
		result["started_at"] = status.StartedAt
	}
	if status.FinishedAt != nil {
		result["finished_at"] = status.FinishedAt
	}
	switch state {
	case "running":
		result["next_step"] = "The upgrade is running and the instance restarts during it. Check again in a minute."
	case "succeeded":
		// This backend answered and reached the updater, so the new release is serving requests.
		result["health_check"] = "passed"
		result["final_version"] = info.current
		if info.available {
			result["warning"] = fmt.Sprintf("The updater finished but the instance still runs %s while %s is available. Check Settings → Version.", info.current, info.latest)
		}
	case "failed":
		result["health_check"] = "not run"
		result["error"] = truncateRunes(status.Error, 500)
		result["next_step"] = "The upgrade failed. Read the updater output in Settings → Version, fix the cause, then ask for the upgrade again. The instance keeps running its previous release unless the output says migrations were applied."
	}
	return result
}

// LegacyUpdateState derives the run state from sidecars that predate the state
// field; update scripts do not recreate the running updater.
func LegacyUpdateState(status dto.SystemUpdateStatusResponse) string {
	switch {
	case status.Running:
		return "running"
	case status.Message == "Last update failed":
		return "failed"
	case status.Message == "Last update completed successfully":
		return "succeeded"
	}
	return "idle"
}

// prepareUpgrade refuses non-administrators and an unavailable or busy
// updater before asking, so the user is never asked for something that cannot run.
func (t *AssistantTools) prepareUpgrade(ctx context.Context, user uuid.UUID, arguments string) (uuid.UUID, domain.UpgradePayload, error) {
	if t.system == nil {
		return uuid.Nil, domain.UpgradePayload{}, toolFailure("unknown tool")
	}
	if err := decodeToolInput(arguments, &struct{}{}); err != nil {
		return uuid.Nil, domain.UpgradePayload{}, err
	}
	if !t.system.isAdmin(user) {
		audit.Log("system.update_refused", user, map[string]interface{}{"reason": "not an instance administrator"})
		return uuid.Nil, domain.UpgradePayload{}, toolFailure("only an instance administrator can upgrade this instance; ask one to upgrade it")
	}
	if !t.system.updater.Configured() {
		return uuid.Nil, domain.UpgradePayload{}, toolFailure("the system updater is not configured; set UPDATER_URL and UPDATER_TOKEN for the backend and run the updater service, or upgrade from the server with selfhosting/update.sh")
	}
	status, code, err := t.system.updater.Status(ctx)
	if err != nil || code != http.StatusOK {
		return uuid.Nil, domain.UpgradePayload{}, toolFailure("the system updater did not answer; check that the updater container is running, then try again")
	}
	if status.Running {
		return uuid.Nil, domain.UpgradePayload{}, toolFailure("an upgrade is already running; follow it with get_instance_version")
	}
	info := t.system.versionInfo(ctx)
	switch {
	case info.err != nil:
		return uuid.Nil, domain.UpgradePayload{}, toolFailure("the release list could not be reached, so the latest version is unknown; try again, or upgrade from Settings → Version")
	case info.current == "dev":
		return uuid.Nil, domain.UpgradePayload{}, toolFailure("this is a development build without a release version; upgrade it from the server")
	case !info.available:
		return uuid.Nil, domain.UpgradePayload{}, toolFailure("the instance already runs the latest release (%s)", info.current)
	}
	audit.Log("system.update_requested", user, map[string]interface{}{"from": info.current, "to": info.latest})
	return uuid.Nil, domain.UpgradePayload{CurrentVersion: info.current, LatestVersion: info.latest, Changelog: info.changelog}, nil
}

// upgradeInstance starts an approved upgrade as the approver. The instance
// restarts during the run, so progress is read afterwards with get_instance_version.
func (s *AssistantApprovals) upgradeInstance(ctx context.Context, ws, conversation uuid.UUID, approval *domain.ConversationApproval) (*domain.ConversationApproval, error) {
	var payload domain.UpgradePayload
	if s.system == nil || json.Unmarshal(approval.Payload, &payload) != nil {
		return s.conversations.FinishApproval(ctx, ws, conversation, approval.ID, nil, "This approval cannot be executed")
	}
	return s.conversations.FinishApproval(ctx, ws, conversation, approval.ID, nil, s.system.startUpgrade(ctx, approval.ActorID, payload))
}

// startUpgrade re-checks admin rights and the installed version, then asks the
// updater to run. It returns the reason the upgrade did not start, or "".
func (s *AssistantSystem) startUpgrade(ctx context.Context, actor uuid.UUID, payload domain.UpgradePayload) string {
	fail := func(message string) string {
		audit.Log("system.update_failed", actor, map[string]interface{}{"reason": message})
		return message
	}
	if !s.isAdmin(actor) {
		return fail("Only an instance administrator can upgrade this instance")
	}
	if strings.TrimPrefix(s.version, "v") != payload.CurrentVersion {
		return fail("The instance version changed after approval was requested; ask again")
	}
	if !s.updater.Configured() {
		return fail("The system updater is not configured. Set UPDATER_URL and UPDATER_TOKEN for the backend and run the updater service, then ask again")
	}
	result, code, err := s.updater.Start(ctx)
	switch {
	case err != nil:
		return fail("The system updater did not answer. Check that the updater container is running, then ask again")
	case code == http.StatusAccepted || code == http.StatusOK:
		audit.Log("system.update_started", actor, map[string]interface{}{"from": payload.CurrentVersion, "to": payload.LatestVersion})
		if s.announce != nil {
			s.announce()
		}
		return ""
	case code == http.StatusConflict:
		return fail("An upgrade is already running; follow it in Settings → Version")
	case code == http.StatusUnauthorized || code == http.StatusForbidden:
		return fail("The system updater rejected the backend's token; check that UPDATER_TOKEN is the same for the backend and the updater")
	}
	if result.Message != "" {
		return fail("The system updater failed to start the upgrade: " + truncateRunes(result.Message, 200))
	}
	return fail("The system updater failed to start the upgrade")
}
