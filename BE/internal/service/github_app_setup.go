package service

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	gh "github.com/kuayle/kuayle-backend/pkg/github"
	log "github.com/sirupsen/logrus"
)

// githubAppPermissions and githubAppEvents are the single source of what the
// Kuayle GitHub App needs: the manifest requests them and the setup check
// compares a live app and installation against them.
var githubAppPermissions = map[string]string{
	"pull_requests": "write",
	"contents":      "write",
	"metadata":      "read",
	"issues":        "read",
	"checks":        "read",
	"statuses":      "read",
}

var githubAppEvents = []string{
	"pull_request",
	"pull_request_review",
	"check_suite",
	"push",
	"create",
}

// GitHubAppPermissions returns a copy of the permissions Kuayle's app needs.
func GitHubAppPermissions() map[string]string {
	out := make(map[string]string, len(githubAppPermissions))
	for name, level := range githubAppPermissions {
		out[name] = level
	}
	return out
}

// GitHubAppEvents returns a copy of the webhook events Kuayle's app needs.
func GitHubAppEvents() []string {
	return slices.Clone(githubAppEvents)
}

// githubPermissionRank orders GitHub permission levels; a higher level
// includes the lower ones (write satisfies read).
func githubPermissionRank(level string) int {
	switch level {
	case "read":
		return 1
	case "write":
		return 2
	case "admin":
		return 3
	}
	return 0
}

// missingGitHubAppRequirements lists what a grant of events and permissions
// lacks, in a stable order.
func missingGitHubAppRequirements(events []string, permissions map[string]string) ([]string, []dto.GitHubAppPermissionRequirement) {
	missingEvents := []string{}
	for _, event := range githubAppEvents {
		if !slices.Contains(events, event) {
			missingEvents = append(missingEvents, event)
		}
	}
	missingPermissions := []dto.GitHubAppPermissionRequirement{}
	for name, level := range githubAppPermissions {
		if githubPermissionRank(permissions[name]) < githubPermissionRank(level) {
			missingPermissions = append(missingPermissions, dto.GitHubAppPermissionRequirement{Name: name, Level: level})
		}
	}
	sort.Slice(missingPermissions, func(i, j int) bool { return missingPermissions[i].Name < missingPermissions[j].Name })
	return missingEvents, missingPermissions
}

var githubLoginPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,98}[A-Za-z0-9])?$`)

// githubAppSettingsURL is where the app's owner edits its permissions and
// events; GitHub uses different paths for user- and organization-owned apps.
func githubAppSettingsURL(slug, owner, ownerType string) string {
	if !githubLoginPattern.MatchString(slug) {
		return ""
	}
	if ownerType == "Organization" {
		if !githubLoginPattern.MatchString(owner) {
			return ""
		}
		return fmt.Sprintf("https://github.com/organizations/%s/settings/apps/%s/permissions", owner, slug)
	}
	return fmt.Sprintf("https://github.com/settings/apps/%s/permissions", slug)
}

// safeGitHubPageURL keeps a GitHub-supplied link only when it is a plain
// HTTPS github.com page.
func safeGitHubPageURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "github.com" || parsed.User != nil {
		return ""
	}
	return parsed.String()
}

// evaluateGitHubAppSetup compares the app's configuration first: the
// installation cannot accept what its app does not request.
func evaluateGitHubAppSetup(app *gh.App, inst *gh.Installation) *dto.GitHubAppSetupResponse {
	resp := &dto.GitHubAppSetupResponse{
		State:           dto.GitHubAppSetupOK,
		AppSlug:         app.Slug,
		AccountLogin:    inst.Account.Login,
		AppSettingsURL:  githubAppSettingsURL(app.Slug, app.Owner.Login, app.Owner.Type),
		InstallationURL: safeGitHubPageURL(inst.HTMLURL),
	}
	if inst.SuspendedAt != nil {
		resp.State = dto.GitHubAppSetupInstallationSuspended
		resp.MissingEvents, resp.MissingPermissions = []string{}, []dto.GitHubAppPermissionRequirement{}
		return resp
	}
	resp.MissingEvents, resp.MissingPermissions = missingGitHubAppRequirements(app.Events, app.Permissions)
	if len(resp.MissingEvents) > 0 || len(resp.MissingPermissions) > 0 {
		resp.State = dto.GitHubAppSetupAppMissing
		return resp
	}
	resp.MissingEvents, resp.MissingPermissions = missingGitHubAppRequirements(inst.Events, inst.Permissions)
	if len(resp.MissingEvents) > 0 || len(resp.MissingPermissions) > 0 {
		resp.State = dto.GitHubAppSetupInstallationPending
	}
	return resp
}

// GetAppSetup checks whether the workspace's GitHub App and installation
// deliver every event and permission Kuayle needs. GitHub offers no API for
// an app to change its own settings, so the result links to the pages where
// the app's owner and the installation's account fix them. Failures to reach
// GitHub are reported as a state; only database errors are returned.
func (s *GitHubService) GetAppSetup(ctx context.Context, workspaceID uuid.UUID) (*dto.GitHubAppSetupResponse, error) {
	resp := &dto.GitHubAppSetupResponse{
		Managed:            s.globalApp != nil,
		MissingEvents:      []string{},
		MissingPermissions: []dto.GitHubAppPermissionRequirement{},
	}
	if s.globalApp == nil {
		cfg, err := s.ghRepo.GetAppConfigByWorkspace(ctx, workspaceID)
		if err != nil {
			return nil, err
		}
		if cfg == nil {
			resp.State = dto.GitHubAppSetupNotConfigured
			return resp, nil
		}
	}
	inst, err := s.ghRepo.GetInstallationByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	if inst == nil || inst.InstallationID <= 0 {
		resp.State = dto.GitHubAppSetupNotInstalled
		return resp, nil
	}

	unavailable := func(step string, err error) (*dto.GitHubAppSetupResponse, error) {
		log.WithFields(log.Fields{"workspace_id": workspaceID, "step": step}).WithError(err).Warn("GitHub App setup check failed")
		resp.State = dto.GitHubAppSetupUnavailable
		return resp, nil
	}
	client, _, err := s.getClient(ctx, workspaceID)
	if err != nil {
		return unavailable("client", err)
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	app, err := client.GetApp(ctx)
	if err != nil {
		return unavailable("app", err)
	}
	installation, err := client.GetInstallationContext(ctx, inst.InstallationID)
	if errors.Is(err, gh.ErrNotFound) {
		resp.State = dto.GitHubAppSetupInstallationMissing
		resp.AppSlug = app.Slug
		return resp, nil
	}
	if err != nil {
		return unavailable("installation", err)
	}
	result := evaluateGitHubAppSetup(app, installation)
	result.Managed = resp.Managed
	return result, nil
}

// permissionGap lists the needed permissions the installation lacks. A
// permission the app itself does not request is fixed in the app's settings;
// one the app requests but the installation has not accepted is fixed on the
// installation's page.
func permissionGap(needs, appPermissions, installationPermissions map[string]string, appURL, installationURL string) *domain.GitHubAccessGap {
	var missing []domain.GitHubPermissionNeed
	appLacks := false
	for name, level := range needs {
		if githubPermissionRank(installationPermissions[name]) >= githubPermissionRank(level) {
			continue
		}
		missing = append(missing, domain.GitHubPermissionNeed{Name: name, Level: level})
		if githubPermissionRank(appPermissions[name]) < githubPermissionRank(level) {
			appLacks = true
		}
	}
	link := installationURL
	if appLacks || link == "" {
		link = appURL
	}
	return newGitHubAccessGap(missing, link)
}

// PermissionGap checks the live GitHub App and installation against the
// permissions an assistant tool needs. An unreachable GitHub is an error, not a gap.
func (s *GitHubService) PermissionGap(ctx context.Context, workspaceID uuid.UUID, needs map[string]string) (*domain.GitHubAccessGap, error) {
	inst, err := s.ghRepo.GetInstallationByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	if inst == nil || inst.InstallationID <= 0 {
		return &domain.GitHubAccessGap{Message: "The GitHub App is not installed for this workspace. Connect it in Settings → Integrations → GitHub."}, nil
	}
	client, _, err := s.getClient(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	app, err := s.appFor(ctx, workspaceID, client)
	if err != nil {
		return nil, err
	}
	installation, err := client.GetInstallationContext(ctx, inst.InstallationID)
	if err != nil {
		return nil, err
	}
	return permissionGap(needs, app.Permissions, installation.Permissions,
		githubAppSettingsURL(app.Slug, app.Owner.Login, app.Owner.Type), safeGitHubPageURL(installation.HTMLURL)), nil
}

// appCache remembers workspaces' GitHub Apps; copying a service shares it.
type appCache struct {
	mu    sync.Mutex
	items map[uuid.UUID]cachedApp
}

type cachedApp struct {
	app     *gh.App
	expires time.Time
}

// appFor returns the workspace's GitHub App, reading it from GitHub at most
// once per githubAppTTL. Only the link to the settings page and the app's own
// permissions come from it; whether the installation grants a permission is
// always read live.
func (s *GitHubService) appFor(ctx context.Context, ws uuid.UUID, client *gh.Client) (*gh.App, error) {
	cache := s.apps
	if cache == nil {
		return client.GetApp(ctx)
	}
	cache.mu.Lock()
	hit, ok := cache.items[ws]
	cache.mu.Unlock()
	if ok && time.Now().Before(hit.expires) {
		return hit.app, nil
	}
	app, err := client.GetApp(ctx)
	if err != nil {
		return nil, err
	}
	cache.mu.Lock()
	if cache.items == nil {
		cache.items = map[uuid.UUID]cachedApp{}
	}
	cache.items[ws] = cachedApp{app: app, expires: time.Now().Add(githubAppTTL)}
	cache.mu.Unlock()
	return app, nil
}
