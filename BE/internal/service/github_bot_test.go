package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
	gh "github.com/kuayle/kuayle-backend/pkg/github"
	"github.com/stretchr/testify/require"
)

// A 1x1 PNG.
var botPNG, _ = base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==")

type botStoreFake struct {
	settings map[uuid.UUID]*domain.GitHubBotSettings
}

func newBotStoreFake() *botStoreFake {
	return &botStoreFake{settings: map[uuid.UUID]*domain.GitHubBotSettings{}}
}

func (f *botStoreFake) Get(_ context.Context, ws uuid.UUID) (*domain.GitHubBotSettings, error) {
	if current, ok := f.settings[ws]; ok {
		copied := *current
		copied.Abilities = map[string]bool{}
		for k, v := range current.Abilities {
			copied.Abilities[k] = v
		}
		return &copied, nil
	}
	return &domain.GitHubBotSettings{WorkspaceID: ws, Abilities: map[string]bool{}}, nil
}

func (f *botStoreFake) Update(ctx context.Context, ws, _ uuid.UUID, change func(*domain.GitHubBotSettings) error) (*domain.GitHubBotSettings, *string, error) {
	current, _ := f.Get(ctx, ws)
	previous := current.AvatarFile
	if err := change(current); err != nil {
		return nil, nil, err
	}
	f.settings[ws] = current
	if previous != nil && (current.AvatarFile == nil || *current.AvatarFile != *previous) {
		return current, previous, nil
	}
	return current, nil, nil
}

func requireBotInvalid(t *testing.T, err error, code string) {
	t.Helper()
	var invalid *GitHubBotValidationError
	require.ErrorAs(t, err, &invalid)
	require.Equal(t, code, invalid.Code)
}

// Names, abilities, harnesses and models are validated; unset fields keep
// their value; an empty name restores the default; the mention trigger is off
// until enabled while the existing abilities stay on.
func TestGitHubBotSettingsUpdateValidatesAndKeepsUnsetFields(t *testing.T) {
	ctx, ws, user := context.Background(), uuid.New(), uuid.New()
	svc := NewGitHubBotService(newBotStoreFake(), nil, "https://kuayle.test")
	svc.SetAppSlug(func(context.Context, uuid.UUID) string { return "kuayle-acme" })

	initial, err := svc.Settings(ctx, ws)
	require.NoError(t, err)
	require.Equal(t, "Kuayle", initial.DisplayName)
	require.Equal(t, "none", initial.AvatarSource)
	require.Equal(t, "@kuayle-acme", initial.MentionHandle)
	require.Len(t, initial.Abilities, len(domain.GitHubBotAbilities))
	for _, ability := range initial.Abilities {
		require.Equal(t, ability.Key != string(domain.GitHubBotMentionReviews), ability.Enabled, ability.Key)
	}

	for _, name := range []string{"Bad\nname", "@octocat", "<b>bot</b>", strings.Repeat("a", 65)} {
		_, err = svc.Update(ctx, ws, user, dto.UpdateGitHubBotSettingsRequest{DisplayName: &name})
		requireBotInvalid(t, err, "INVALID_NAME")
	}
	_, err = svc.Update(ctx, ws, user, dto.UpdateGitHubBotSettingsRequest{Abilities: map[string]bool{"delete_repository": true}})
	requireBotInvalid(t, err, "INVALID_ABILITY")
	_, err = svc.Update(ctx, ws, user, dto.UpdateGitHubBotSettingsRequest{MentionHarness: ptr("custom-cli")})
	requireBotInvalid(t, err, "INVALID_HARNESS")
	for _, model := range []string{"model\x00", strings.Repeat("m", 129)} {
		_, err = svc.Update(ctx, ws, user, dto.UpdateGitHubBotSettingsRequest{MentionModel: &model})
		requireBotInvalid(t, err, "INVALID_MODEL")
	}

	got, err := svc.Update(ctx, ws, user, dto.UpdateGitHubBotSettingsRequest{
		DisplayName: ptr("  Acme Reviewer "), Abilities: map[string]bool{"mention_reviews": true, "merge_pull_requests": false},
		MentionHarness: ptr("claude-code"), MentionModel: ptr(" claude-sonnet-4-5 "),
	})
	require.NoError(t, err)
	require.Equal(t, "Acme Reviewer", got.DisplayName)
	require.Equal(t, "claude-code", got.MentionHarness)
	require.Equal(t, "claude-sonnet-4-5", got.MentionModel)
	enabled := map[string]bool{}
	for _, ability := range got.Abilities {
		enabled[ability.Key] = ability.Enabled
	}
	require.True(t, enabled["mention_reviews"])
	require.False(t, enabled["merge_pull_requests"])
	require.True(t, enabled["releases"])

	got, err = svc.Update(ctx, ws, user, dto.UpdateGitHubBotSettingsRequest{Abilities: map[string]bool{"releases": false}})
	require.NoError(t, err)
	require.Equal(t, "Acme Reviewer", got.DisplayName, "unset fields are kept")
	require.Equal(t, "claude-code", got.MentionHarness)
	got, err = svc.Update(ctx, ws, user, dto.UpdateGitHubBotSettingsRequest{DisplayName: ptr(""), MentionHarness: ptr("")})
	require.NoError(t, err)
	require.Equal(t, "Kuayle", got.DisplayName)
	require.Empty(t, got.MentionHarness, "empty means chat's default harness")
}

// Uploaded pictures must be PNG, JPEG, WebP or GIF content of at most 2 MB,
// whatever their name says. A new picture replaces and deletes the previous
// upload, and only the current one is served.
func TestGitHubBotAvatarUploadValidatesTheImage(t *testing.T) {
	ctx, ws, user := context.Background(), uuid.New(), uuid.New()
	files := newMemoryStorage()
	svc := NewGitHubBotService(newBotStoreFake(), files, "https://kuayle.test")

	_, err := svc.UploadAvatar(ctx, ws, user, strings.NewReader(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`))
	requireBotInvalid(t, err, "UNSUPPORTED_IMAGE")
	_, err = svc.UploadAvatar(ctx, ws, user, strings.NewReader("plain text"))
	requireBotInvalid(t, err, "UNSUPPORTED_IMAGE")
	_, err = svc.UploadAvatar(ctx, ws, user, io.MultiReader(bytes.NewReader(botPNG), bytes.NewReader(make([]byte, domain.GitHubBotAvatarMaxBytes))))
	requireBotInvalid(t, err, "AVATAR_TOO_LARGE")
	require.Empty(t, files.objects, "rejected images are never stored")

	first, err := svc.UploadAvatar(ctx, ws, user, bytes.NewReader(botPNG))
	require.NoError(t, err)
	require.Equal(t, "upload", first.AvatarSource)
	require.Regexp(t, `^/api/github-bot/`+ws.String()+`/avatar/[0-9a-f-]{36}\.png$`, first.AvatarURL)
	firstFile := first.AvatarURL[strings.LastIndex(first.AvatarURL, "/")+1:]
	rc, contentType, err := svc.Avatar(ctx, ws, firstFile)
	require.NoError(t, err)
	rc.Close()
	require.Equal(t, "image/png", contentType)

	second, err := svc.UploadAvatar(ctx, ws, user, bytes.NewReader(botPNG))
	require.NoError(t, err)
	require.NotEqual(t, first.AvatarURL, second.AvatarURL)
	require.Len(t, files.objects, 1, "the replaced picture is deleted")
	_, _, err = svc.Avatar(ctx, ws, firstFile)
	require.Error(t, err, "a replaced picture is no longer served")
	_, _, err = svc.Avatar(ctx, uuid.New(), second.AvatarURL[strings.LastIndex(second.AvatarURL, "/")+1:])
	require.Error(t, err, "another workspace cannot serve this picture")

	removed, err := svc.Update(ctx, ws, user, dto.UpdateGitHubBotSettingsRequest{AvatarURL: ptr("")})
	require.NoError(t, err)
	require.Equal(t, "none", removed.AvatarSource)
	require.Empty(t, files.objects)

	_, err = NewGitHubBotService(newBotStoreFake(), nil, "").UploadAvatar(ctx, ws, user, bytes.NewReader(botPNG))
	require.ErrorIs(t, err, ErrGitHubBotAvatarStorageUnavailable)
}

// A linked picture must be an https address whose content is an accepted
// image within the size limit; it is downloaded once to check before saving.
func TestGitHubBotAvatarURLMustServeAnImage(t *testing.T) {
	ctx, ws, user := context.Background(), uuid.New(), uuid.New()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/bot.png":
			_, _ = w.Write(botPNG)
		case "/page.html":
			_, _ = w.Write([]byte("<html><body>not an image</body></html>"))
		case "/huge.png":
			_, _ = w.Write(botPNG)
			_, _ = w.Write(make([]byte, domain.GitHubBotAvatarMaxBytes))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	store := newBotStoreFake()
	svc := NewGitHubBotService(store, nil, "https://kuayle.test")
	svc.imageClient = server.Client()

	for _, address := range []string{"http://example.com/a.png", "javascript:alert(1)", "https://user:pw@example.com/a.png", `https://example.com/a.png" onerror="x`, "/relative.png"} {
		_, err := svc.Update(ctx, ws, user, dto.UpdateGitHubBotSettingsRequest{AvatarURL: &address})
		requireBotInvalid(t, err, "INVALID_AVATAR_URL")
	}
	_, err := svc.Update(ctx, ws, user, dto.UpdateGitHubBotSettingsRequest{AvatarURL: ptr(server.URL + "/page.html")})
	requireBotInvalid(t, err, "UNSUPPORTED_IMAGE")
	_, err = svc.Update(ctx, ws, user, dto.UpdateGitHubBotSettingsRequest{AvatarURL: ptr(server.URL + "/huge.png")})
	requireBotInvalid(t, err, "AVATAR_TOO_LARGE")
	_, err = svc.Update(ctx, ws, user, dto.UpdateGitHubBotSettingsRequest{AvatarURL: ptr(server.URL + "/missing.png")})
	requireBotInvalid(t, err, "AVATAR_UNREACHABLE")
	require.Empty(t, store.settings, "nothing is saved for a rejected picture")

	got, err := svc.Update(ctx, ws, user, dto.UpdateGitHubBotSettingsRequest{AvatarURL: ptr(server.URL + "/bot.png")})
	require.NoError(t, err)
	require.Equal(t, "url", got.AvatarSource)
	require.Equal(t, server.URL+"/bot.png", got.AvatarURL)

	// The production client refuses loopback addresses outright.
	production := NewGitHubBotService(newBotStoreFake(), nil, "https://kuayle.test")
	_, err = production.Update(ctx, ws, user, dto.UpdateGitHubBotSettingsRequest{AvatarURL: ptr(server.URL + "/bot.png")})
	requireBotInvalid(t, err, "AVATAR_UNREACHABLE")
}

// GitHub comments start with the configured name and picture; defaults leave
// existing comments unchanged, and an uploaded picture is linked only from a
// public https address.
func TestBotSignatureUsesTheConfiguredIdentity(t *testing.T) {
	ws := uuid.New()
	require.Empty(t, botSignature(&domain.GitHubBotSettings{WorkspaceID: ws}, "https://kuayle.test"))
	named := &domain.GitHubBotSettings{WorkspaceID: ws, DisplayName: ptr("Acme Reviewer")}
	require.Equal(t, "**Acme Reviewer**\n\n", botSignature(named, "https://kuayle.test"))
	linked := &domain.GitHubBotSettings{WorkspaceID: ws, AvatarURL: ptr("https://img.test/bot.png")}
	require.Equal(t, "<img src=\"https://img.test/bot.png\" alt=\"\" width=\"20\" height=\"20\" align=\"top\"> **Kuayle**\n\n", botSignature(linked, "https://kuayle.test"))
	uploaded := &domain.GitHubBotSettings{WorkspaceID: ws, AvatarFile: ptr(uuid.NewString() + ".png")}
	require.Contains(t, botSignature(uploaded, "https://kuayle.test/"), `<img src="https://kuayle.test/api/github-bot/`+ws.String()+`/avatar/`)
	require.Equal(t, "**Kuayle**\n\n", botSignature(uploaded, "http://localhost:5173"))
	edited := &domain.GitHubBotSettings{WorkspaceID: ws, DisplayName: ptr("@team *ping*")}
	require.Equal(t, "**@​team ping**\n\n", botSignature(edited, ""))
}

// Turned-off abilities refuse the GitHub write itself, whoever asked for it.
func TestDisabledBotAbilitiesRefuseGitHubActions(t *testing.T) {
	ctx, ws := context.Background(), uuid.New()
	store := newBotStoreFake()
	off := func(abilities ...domain.GitHubBotAbility) {
		settings := &domain.GitHubBotSettings{WorkspaceID: ws, Abilities: map[string]bool{}}
		for _, ability := range abilities {
			settings.Abilities[string(ability)] = false
		}
		store.settings[ws] = settings
	}
	svc := NewGitHubService(nil, nil, nil, nil, nil, nil, nil, "https://kuayle.test", "", nil)
	svc.SetBotSettings(store)
	svc.SetTaskReviews(&mergedPullRequestRecorder{})
	merged := 0
	svc.mergePullRequest = func(context.Context, uuid.UUID, string, int, string, string) error {
		merged++
		return nil
	}
	require.NoError(t, svc.MergeTaskPullRequest(ctx, ws, "acme/app", 7, "abc", "squash"))
	require.Equal(t, 1, merged, "abilities are on by default")

	off(domain.GitHubBotMergePullRequests, domain.GitHubBotReleases, domain.GitHubBotPullRequestActions)
	var disabled *GitHubBotAbilityDisabledError
	require.ErrorAs(t, svc.MergeTaskPullRequest(ctx, ws, "acme/app", 7, "abc", "squash"), &disabled)
	require.Equal(t, domain.GitHubBotMergePullRequests, disabled.Ability)
	require.Equal(t, 1, merged)
	_, err := svc.CreateRelease(ctx, ws, "acme/app", gh.NewRelease{TagName: "v1"})
	require.ErrorAs(t, err, &disabled)
	require.Equal(t, domain.GitHubBotReleases, disabled.Ability)
	require.ErrorAs(t, svc.ReplyToReviewComment(ctx, ws, "acme/app", 7, 1, "ok"), &disabled)
	require.ErrorAs(t, svc.ResolveReviewThread(ctx, ws, "acme/app", 7, "thread"), &disabled)
	require.ErrorAs(t, svc.SetPullRequestDraft(ctx, ws, "acme/app", 7, false), &disabled)
	_, err = svc.UpdatePullRequestBranch(ctx, ws, "acme/app", 7, "abc")
	require.ErrorAs(t, err, &disabled)
	message, ok := disabledAbilityMessage(err)
	require.True(t, ok)
	require.Contains(t, message, "Act on pull requests")
}

// Review comments are signed with the bot identity and are not posted while
// the workspace turned that ability off.
func TestReviewCommentsFollowTheBotSettings(t *testing.T) {
	ws := uuid.New()
	verdict, findings := "comment", "- fine"
	review := func() repository.ReviewComment {
		return repository.ReviewComment{IssueIdentifier: "ENG-1", Review: domain.AgentReview{TaskID: uuid.New(), WorkspaceID: ws, Provider: "codex", CommitSHA: "0123456789abcdef", Repository: "acme/app", PullRequestNumber: 7, Verdict: &verdict, Findings: &findings, Focus: mentionReviewFocusPrefix + "octo"}}
	}
	first, second := review(), review()
	store := &reviewCommentStoreFake{due: []repository.ReviewComment{first}, finished: map[uuid.UUID]bool{}}
	bot := newBotStoreFake()
	bot.settings[ws] = &domain.GitHubBotSettings{WorkspaceID: ws, DisplayName: ptr("Acme Reviewer"), Abilities: map[string]bool{}}
	svc := NewGitHubService(nil, nil, nil, nil, nil, nil, nil, "https://kuayle.test", "", nil)
	svc.SetBotSettings(bot)
	svc.SetReviewComments(store)
	var bodies []string
	svc.postReviewComment = func(_ context.Context, _ uuid.UUID, _ string, _ int, _, body string) error {
		bodies = append(bodies, body)
		return nil
	}
	count, err := svc.PostReviewComments(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, count)
	require.True(t, strings.HasPrefix(bodies[0], "**Acme Reviewer**\n\n**Agent review by Codex**"))
	require.Contains(t, bodies[0], "requested by a mention on GitHub")

	bot.settings[ws].Abilities[string(domain.GitHubBotReviewComments)] = false
	store.due = []repository.ReviewComment{second}
	count, err = svc.PostReviewComments(context.Background())
	require.NoError(t, err)
	require.Zero(t, count)
	require.Len(t, bodies, 1, "nothing is posted while the ability is off")
	require.False(t, store.finished[second.Review.TaskID], "the attempt is recorded as not posted")
}

// Only an @-mention of the app's own handle that asks for a review counts;
// the mention is removed from the focus.
func TestReviewMentionParsing(t *testing.T) {
	cases := []struct {
		body, focus string
		ok          bool
	}{
		{"@kuayle-acme review", "review", true},
		{"@Kuayle-Acme[bot] please REVIEW the parser", "please REVIEW the parser", true},
		{"Could you review this, @kuayle-acme?", "Could you review this, ?", true},
		{"@kuayle-acme thanks", "", false},
		{"@kuayle-acmex review", "", false},
		{"email me at a@kuayle-acme.dev for a review", "", false},
		{"review by kuayle-acme", "", false},
		{"@kuayle-acme reviewed already", "", false},
	}
	for _, c := range cases {
		focus, ok := reviewMention(c.body, "kuayle-acme")
		require.Equal(t, c.ok, ok, c.body)
		if ok {
			require.Equal(t, c.focus, focus, c.body)
		}
	}
	_, ok := reviewMention("@ review", "")
	require.False(t, ok, "without a known app slug nothing is a mention")
}
