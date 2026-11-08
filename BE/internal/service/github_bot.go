package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gabriel-vasile/mimetype"
	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/pkg/storage"
	"github.com/kuayle/kuayle-backend/pkg/validate"
	log "github.com/sirupsen/logrus"
)

// GitHubBotValidationError is a rejected bot setting, with a message for the user.
type GitHubBotValidationError struct {
	Code    string
	Message string
}

func (e *GitHubBotValidationError) Error() string { return e.Message }

func botInvalid(code, format string, args ...any) error {
	return &GitHubBotValidationError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// ErrGitHubBotAvatarStorageUnavailable means no file storage is configured.
var ErrGitHubBotAvatarStorageUnavailable = errors.New("bot picture uploads are not available")

// GitHubBotAbilityDisabledError refuses a GitHub action the workspace turned off.
type GitHubBotAbilityDisabledError struct {
	Ability domain.GitHubBotAbility
}

func (e *GitHubBotAbilityDisabledError) Error() string {
	return fmt.Sprintf("The GitHub bot ability %q is turned off in this workspace's GitHub settings", githubBotAbilityLabels[e.Ability])
}

var githubBotAbilityLabels = map[domain.GitHubBotAbility]string{
	domain.GitHubBotLinkIssues:         "Link issues on pull requests",
	domain.GitHubBotTaskFeedback:       "Send CI and review feedback to tasks",
	domain.GitHubBotReviewComments:     "Post agent review findings",
	domain.GitHubBotPullRequestActions: "Act on pull requests",
	domain.GitHubBotMergePullRequests:  "Merge pull requests",
	domain.GitHubBotReleases:           "Publish releases",
	domain.GitHubBotMentionReviews:     "Review when mentioned",
}

type githubBotStore interface {
	Get(ctx context.Context, ws uuid.UUID) (*domain.GitHubBotSettings, error)
	Update(ctx context.Context, ws, user uuid.UUID, change func(*domain.GitHubBotSettings) error) (*domain.GitHubBotSettings, *string, error)
}

// GitHubBotService manages a workspace's GitHub bot identity and abilities.
type GitHubBotService struct {
	store       githubBotStore
	avatars     storage.Backend
	frontendURL string
	// appSlug names the GitHub App, so settings can show the mention handle.
	appSlug func(ctx context.Context, ws uuid.UUID) string
	// imageClient fetches linked pictures to validate them; replaced in tests.
	imageClient *http.Client
}

func NewGitHubBotService(store githubBotStore, avatars storage.Backend, frontendURL string) *GitHubBotService {
	return &GitHubBotService{store: store, avatars: avatars, frontendURL: frontendURL, imageClient: newBotImageClient()}
}

// SetAppSlug lets settings show the handle that mentions the bot on GitHub.
func (s *GitHubBotService) SetAppSlug(slug func(ctx context.Context, ws uuid.UUID) string) {
	s.appSlug = slug
}

// newBotImageClient dials only public addresses and never follows redirects,
// which could otherwise reach internal hosts.
func newBotImageClient() *http.Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second, Control: validate.PublicAddressControl}
	return &http.Client{
		Timeout:       10 * time.Second,
		Transport:     &http.Transport{Proxy: nil, DialContext: dialer.DialContext, TLSHandshakeTimeout: 5 * time.Second},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func botAvatarKey(ws uuid.UUID, file string) string {
	return "github-bot-avatar-" + ws.String() + "-" + file
}

// BotAvatarPath is where Kuayle serves an uploaded bot picture. It is public:
// GitHub must load it to show it in the bot's comments.
func BotAvatarPath(ws uuid.UUID, file string) string {
	return "/api/github-bot/" + ws.String() + "/avatar/" + file
}

// Settings returns the workspace's bot settings as the API presents them.
func (s *GitHubBotService) Settings(ctx context.Context, ws uuid.UUID) (*dto.GitHubBotSettingsResponse, error) {
	settings, err := s.store.Get(ctx, ws)
	if err != nil {
		return nil, err
	}
	return s.response(ctx, settings), nil
}

func (s *GitHubBotService) response(ctx context.Context, settings *domain.GitHubBotSettings) *dto.GitHubBotSettingsResponse {
	out := &dto.GitHubBotSettingsResponse{
		DisplayName:    settings.Name(),
		DefaultName:    domain.DefaultGitHubBotName,
		AvatarSource:   "none",
		Abilities:      make([]dto.GitHubBotAbilityResponse, 0, len(domain.GitHubBotAbilities)),
		MentionHarness: settings.MentionHarness,
		MentionModel:   settings.MentionModel,
		Harnesses:      slices.Clone(ReviewHarnesses),
	}
	switch {
	case settings.AvatarFile != nil:
		out.AvatarSource, out.AvatarURL = "upload", BotAvatarPath(settings.WorkspaceID, *settings.AvatarFile)
	case settings.AvatarURL != nil:
		out.AvatarSource, out.AvatarURL = "url", *settings.AvatarURL
	}
	for _, ability := range domain.GitHubBotAbilities {
		out.Abilities = append(out.Abilities, dto.GitHubBotAbilityResponse{
			Key: string(ability), Enabled: settings.Enabled(ability), EnabledByDefault: domain.GitHubBotAbilityEnabledByDefault(ability),
		})
	}
	if s.appSlug != nil {
		if slug := s.appSlug(ctx, settings.WorkspaceID); slug != "" {
			out.MentionHandle = "@" + slug
		}
	}
	return out
}

// Update applies the fields the request sets. An empty display name restores
// the default; an empty avatar URL removes the picture. A linked picture is
// downloaded and checked to be a PNG, JPEG, WebP or GIF image of at most 2 MB
// before it is saved.
func (s *GitHubBotService) Update(ctx context.Context, ws, user uuid.UUID, req dto.UpdateGitHubBotSettingsRequest) (*dto.GitHubBotSettingsResponse, error) {
	var name *string
	if req.DisplayName != nil {
		value, err := normalizeBotName(*req.DisplayName)
		if err != nil {
			return nil, err
		}
		name = &value
	}
	var avatarURL *string
	if req.AvatarURL != nil {
		value := strings.TrimSpace(*req.AvatarURL)
		if value != "" {
			if err := validBotAvatarURL(value); err != nil {
				return nil, err
			}
			if err := s.checkLinkedImage(ctx, value); err != nil {
				return nil, err
			}
		}
		avatarURL = &value
	}
	for key := range req.Abilities {
		if !domain.IsGitHubBotAbility(key) {
			return nil, botInvalid("INVALID_ABILITY", "Unknown ability %q", key)
		}
	}
	if req.MentionHarness != nil && *req.MentionHarness != "" && !slices.Contains(ReviewHarnesses, *req.MentionHarness) {
		return nil, botInvalid("INVALID_HARNESS", "Choose one of the review harnesses or the default")
	}
	if req.MentionModel != nil {
		trimmed := strings.TrimSpace(*req.MentionModel)
		if !validModelName(trimmed) || len(trimmed) > 128 {
			return nil, botInvalid("INVALID_MODEL", "Invalid model name")
		}
		req.MentionModel = &trimmed
	}
	saved, replaced, err := s.store.Update(ctx, ws, user, func(current *domain.GitHubBotSettings) error {
		if name != nil {
			if *name == "" || *name == domain.DefaultGitHubBotName {
				current.DisplayName = nil
			} else {
				current.DisplayName = name
			}
		}
		if avatarURL != nil {
			current.AvatarFile = nil
			current.AvatarURL = nil
			if *avatarURL != "" {
				current.AvatarURL = avatarURL
			}
		}
		for key, enabled := range req.Abilities {
			current.Abilities[key] = enabled
		}
		if req.MentionHarness != nil {
			current.MentionHarness = *req.MentionHarness
		}
		if req.MentionModel != nil {
			current.MentionModel = *req.MentionModel
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.deleteAvatar(ctx, ws, replaced)
	return s.response(ctx, saved), nil
}

// UploadAvatar validates and stores an uploaded picture as the bot's avatar.
func (s *GitHubBotService) UploadAvatar(ctx context.Context, ws, user uuid.UUID, src io.Reader) (*dto.GitHubBotSettingsResponse, error) {
	if s.avatars == nil {
		return nil, ErrGitHubBotAvatarStorageUnavailable
	}
	data, contentType, ext, err := readBotImage(src)
	if err != nil {
		return nil, err
	}
	file := uuid.NewString() + ext
	if _, err = s.avatars.Put(ctx, botAvatarKey(ws, file), bytes.NewReader(data), contentType); err != nil {
		return nil, err
	}
	saved, replaced, err := s.store.Update(ctx, ws, user, func(current *domain.GitHubBotSettings) error {
		current.AvatarURL = nil
		current.AvatarFile = &file
		return nil
	})
	if err != nil {
		s.deleteAvatar(ctx, ws, &file)
		return nil, err
	}
	s.deleteAvatar(ctx, ws, replaced)
	return s.response(ctx, saved), nil
}

// Avatar opens the workspace's current uploaded picture. Only the file the
// settings reference is served, so a replaced picture stops being public.
func (s *GitHubBotService) Avatar(ctx context.Context, ws uuid.UUID, file string) (io.ReadCloser, string, error) {
	if s.avatars == nil || !domain.GitHubBotAvatarFilePattern.MatchString(file) {
		return nil, "", os.ErrNotExist
	}
	settings, err := s.store.Get(ctx, ws)
	if err != nil {
		return nil, "", err
	}
	if settings.AvatarFile == nil || *settings.AvatarFile != file {
		return nil, "", os.ErrNotExist
	}
	contentType := "image/png"
	for kind, ext := range domain.AvatarImageTypes {
		if strings.HasSuffix(file, ext) {
			contentType = kind
		}
	}
	rc, err := s.avatars.Get(ctx, botAvatarKey(ws, file))
	return rc, contentType, err
}

func (s *GitHubBotService) deleteAvatar(ctx context.Context, ws uuid.UUID, file *string) {
	if file == nil || s.avatars == nil {
		return
	}
	if err := s.avatars.Delete(ctx, botAvatarKey(ws, *file)); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.WithError(err).WithField("workspace_id", ws).Warn("Could not delete a replaced GitHub bot picture")
	}
}

// normalizeBotName trims the name and rejects characters that could break
// out of the plain text Kuayle renders it as on GitHub or mention anyone.
func normalizeBotName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", nil
	}
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) > domain.GitHubBotNameMaxRunes {
		return "", botInvalid("INVALID_NAME", "Use a name of at most %d characters", domain.GitHubBotNameMaxRunes)
	}
	for _, r := range name {
		if unicode.IsControl(r) || strings.ContainsRune("<>@`[]\\*_~|#", r) {
			return "", botInvalid("INVALID_NAME", "The name cannot contain line breaks or any of < > @ ` [ ] \\ * _ ~ | #")
		}
	}
	return name, nil
}

// validBotAvatarURL accepts an absolute https URL without credentials or
// characters that could escape the HTML attribute it is rendered in.
func validBotAvatarURL(raw string) error {
	invalid := botInvalid("INVALID_AVATAR_URL", "Use an https:// image address")
	if len(raw) > 2048 || strings.ContainsAny(raw, "\"'<>\\` \t\r\n") {
		return invalid
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Opaque != "" {
		return invalid
	}
	return nil
}

// checkLinkedImage downloads at most the size limit from a public address and
// checks that the content is an accepted raster image.
func (s *GitHubBotService) checkLinkedImage(ctx context.Context, raw string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return botInvalid("INVALID_AVATAR_URL", "Use an https:// image address")
	}
	request.Header.Set("Accept", "image/png,image/jpeg,image/webp,image/gif")
	response, err := s.imageClient.Do(request)
	if err != nil {
		return botInvalid("AVATAR_UNREACHABLE", "The image could not be downloaded from that address")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return botInvalid("AVATAR_UNREACHABLE", "The image address answered HTTP %d; link the image itself", response.StatusCode)
	}
	_, _, _, err = readBotImage(response.Body)
	return err
}

// readBotImage reads at most the size limit and detects the image type from
// the content, never from a name or header.
func readBotImage(src io.Reader) ([]byte, string, string, error) {
	data, err := io.ReadAll(io.LimitReader(src, domain.GitHubBotAvatarMaxBytes+1))
	if err != nil {
		return nil, "", "", botInvalid("AVATAR_UNREADABLE", "The image could not be read")
	}
	if len(data) > domain.GitHubBotAvatarMaxBytes {
		return nil, "", "", botInvalid("AVATAR_TOO_LARGE", "Image too large (max 2MB)")
	}
	detected := mimetype.Detect(data).String()
	ext, ok := domain.AvatarImageTypes[detected]
	if !ok || len(data) == 0 {
		return nil, "", "", botInvalid("UNSUPPORTED_IMAGE", "Use a PNG, JPEG, WebP or GIF image")
	}
	return data, detected, ext, nil
}

// botSignature is the line Kuayle starts its GitHub comments with: the
// configured picture and name. GitHub itself always shows the GitHub App as
// the author, so this is how the workspace's identity appears there. An
// uploaded picture is linked only when Kuayle has a public https address.
func botSignature(settings *domain.GitHubBotSettings, frontendURL string) string {
	name := settings.Name()
	if settings.DisplayName == nil && settings.AvatarURL == nil && settings.AvatarFile == nil {
		return ""
	}
	image := ""
	switch {
	case settings.AvatarURL != nil:
		image = *settings.AvatarURL
	case settings.AvatarFile != nil:
		if base, err := url.Parse(frontendURL); err == nil && base.Scheme == "https" && base.Host != "" {
			image = strings.TrimRight(base.String(), "/") + BotAvatarPath(settings.WorkspaceID, *settings.AvatarFile)
		}
	}
	// Names are validated on save; escaping again keeps older or edited rows inert.
	name = strings.NewReplacer("@", "@\u200b", "<", "", ">", "", "`", "", "[", "", "]", "", "\\", "", "*", "", "_", "", "~", "", "|", "", "#", "", "\n", " ", "\r", " ").Replace(name)
	if image != "" && validBotAvatarURL(image) == nil {
		return fmt.Sprintf("<img src=\"%s\" alt=\"\" width=\"20\" height=\"20\" align=\"top\"> **%s**\n\n", image, name)
	}
	return "**" + name + "**\n\n"
}
