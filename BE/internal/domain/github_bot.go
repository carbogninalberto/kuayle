package domain

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

// GitHubBotAbility is one thing the workspace's Kuayle GitHub bot may do on
// GitHub. Each is a per-workspace toggle; a disabled ability is refused where
// the GitHub write happens, whoever asked for it.
type GitHubBotAbility string

const (
	// GitHubBotLinkIssues comments the linked Kuayle issue on new pull requests.
	GitHubBotLinkIssues GitHubBotAbility = "link_issues"
	// GitHubBotTaskFeedback turns CI failures and change requests on a task's
	// pull request into follow-up attempts.
	GitHubBotTaskFeedback GitHubBotAbility = "task_feedback"
	// GitHubBotReviewComments posts agent review findings on pull requests.
	GitHubBotReviewComments GitHubBotAbility = "review_comments"
	// GitHubBotPullRequestActions replies to and resolves review threads,
	// updates branches and marks pull requests ready, after approval.
	GitHubBotPullRequestActions GitHubBotAbility = "pull_request_actions"
	// GitHubBotMergePullRequests merges approved pull requests.
	GitHubBotMergePullRequests GitHubBotAbility = "merge_pull_requests"
	// GitHubBotReleases publishes approved GitHub releases.
	GitHubBotReleases GitHubBotAbility = "releases"
	// GitHubBotMentionReviews starts an agent review when a trusted commenter
	// mentions the bot on a pull request and asks for a review.
	GitHubBotMentionReviews GitHubBotAbility = "mention_reviews"
)

// GitHubBotAbilities lists every ability in display order.
var GitHubBotAbilities = []GitHubBotAbility{
	GitHubBotLinkIssues, GitHubBotTaskFeedback, GitHubBotReviewComments, GitHubBotPullRequestActions,
	GitHubBotMergePullRequests, GitHubBotReleases, GitHubBotMentionReviews,
}

// DefaultGitHubBotName is the bot's name until a workspace chooses another.
const DefaultGitHubBotName = "Kuayle"

// GitHubBotNameMaxRunes bounds the display name.
const GitHubBotNameMaxRunes = 64

// GitHubBotAvatarMaxBytes bounds an uploaded or linked profile picture.
const GitHubBotAvatarMaxBytes = 2 * 1024 * 1024

// AvatarImageTypes maps the raster formats accepted for profile pictures to
// their file extension. SVG is excluded because it can carry script.
var AvatarImageTypes = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/webp": ".webp",
	"image/gif":  ".gif",
}

// GitHubBotAvatarFilePattern matches a stored bot avatar file name.
var GitHubBotAvatarFilePattern = regexp.MustCompile(`^[0-9a-f-]{36}\.(png|jpg|webp|gif)$`)

// GitHubBotAbilityEnabledByDefault keeps everything the bot already did on
// for existing workspaces; the mention trigger, which starts work without an
// approval card, is opt-in.
func GitHubBotAbilityEnabledByDefault(ability GitHubBotAbility) bool {
	return ability != GitHubBotMentionReviews
}

// GitHubBotSettings is a workspace's bot identity and abilities. At most one
// of AvatarURL (an external https image) and AvatarFile (an uploaded image)
// is set. Abilities holds only explicit choices; a missing key is the default.
type GitHubBotSettings struct {
	WorkspaceID    uuid.UUID       `db:"workspace_id"`
	DisplayName    *string         `db:"display_name"`
	AvatarURL      *string         `db:"avatar_url"`
	AvatarFile     *string         `db:"avatar_file"`
	Abilities      map[string]bool `db:"-"`
	MentionHarness string          `db:"mention_harness"`
	MentionModel   string          `db:"mention_model"`
	UpdatedAt      *time.Time      `db:"updated_at"`
}

// Name is the configured display name, or the default.
func (s *GitHubBotSettings) Name() string {
	if s == nil || s.DisplayName == nil || *s.DisplayName == "" {
		return DefaultGitHubBotName
	}
	return *s.DisplayName
}

// Enabled reports whether the workspace allows the ability.
func (s *GitHubBotSettings) Enabled(ability GitHubBotAbility) bool {
	if s != nil {
		if enabled, ok := s.Abilities[string(ability)]; ok {
			return enabled
		}
	}
	return GitHubBotAbilityEnabledByDefault(ability)
}

// IsGitHubBotAbility reports whether name is a known ability.
func IsGitHubBotAbility(name string) bool {
	for _, ability := range GitHubBotAbilities {
		if string(ability) == name {
			return true
		}
	}
	return false
}

// GitHubBotAvatarPath is where Kuayle serves an uploaded bot picture. It is
// public: GitHub must load it to show it in the bot's comments.
func GitHubBotAvatarPath(ws uuid.UUID, file string) string {
	return "/api/github-bot/" + ws.String() + "/avatar/" + file
}

// ValidGitHubBotAvatarURL accepts an absolute https URL without credentials
// or characters that could escape the HTML attribute it is rendered in.
func ValidGitHubBotAvatarURL(raw string) bool {
	if len(raw) > 2048 || strings.ContainsAny(raw, "\"'<>\\` \t\r\n") {
		return false
	}
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && u.Opaque == ""
}

var githubBotNameEscaper = strings.NewReplacer("@", "@\u200b", "<", "", ">", "", "`", "", "[", "", "]", "", "\\", "", "*", "", "_", "", "~", "", "|", "", "#", "", "\n", " ", "\r", " ")

// Signature is the line Kuayle starts its GitHub comments and pull request
// descriptions with: the configured picture and name. GitHub itself always
// shows the GitHub App as the author, so this is how the workspace's identity
// appears there. It is empty until something is configured. An uploaded
// picture is linked only when Kuayle has a public https address.
func (s *GitHubBotSettings) Signature(frontendURL string) string {
	if s == nil || (s.DisplayName == nil && s.AvatarURL == nil && s.AvatarFile == nil) {
		return ""
	}
	image := ""
	switch {
	case s.AvatarURL != nil:
		image = *s.AvatarURL
	case s.AvatarFile != nil:
		if base, err := url.Parse(frontendURL); err == nil && base.Scheme == "https" && base.Host != "" {
			image = strings.TrimRight(base.String(), "/") + GitHubBotAvatarPath(s.WorkspaceID, *s.AvatarFile)
		}
	}
	// Names are validated on save; escaping again keeps older or edited rows inert.
	name := githubBotNameEscaper.Replace(s.Name())
	if image != "" && ValidGitHubBotAvatarURL(image) {
		return fmt.Sprintf("<img src=\"%s\" alt=\"\" width=\"20\" height=\"20\" align=\"top\"> **%s**\n\n", image, name)
	}
	return "**" + name + "**\n\n"
}
