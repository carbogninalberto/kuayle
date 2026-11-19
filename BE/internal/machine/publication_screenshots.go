package machine

import (
	"net/url"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	log "github.com/sirupsen/logrus"
	"golang.org/x/net/html"
)

const (
	screenshotsRecoveredNote   = "Kuayle restored screenshots missing from the previous description."
	screenshotsUnsupportedNote = "Earlier screenshots with unsupported links were omitted."
	screenshotsCheckFailedNote = "Kuayle could not check the screenshot branch for earlier evidence."
	screenshotsOverflowNote    = "Some screenshots could not fit in the pull request description."
)

var (
	markdownScreenshot = regexp.MustCompile(`^!\[([^\]]*)\]\(<?([^<>()\s]+)>?(?:[ \t]+"[^"]*")?\)`)
	repoScreenshot     = regexp.MustCompile(`^/([^/]+)/([^/]+)/blob/(?:[0-9a-f]{40}|[0-9a-f]{64})/(?:[A-Za-z0-9._%+-]+/)*[A-Za-z0-9._%+-]+\.(?i:png|jpe?g|webp|gif)$`)
	githubAttachment   = regexp.MustCompile(`^/user-attachments/assets/[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$`)
)

// priorPullRequestScreenshots retains durable references from the current
// body and recovers the last stored snapshot if any of its links are missing.
func priorPullRequestScreenshots(github githubAPI, token, owner, repo string, taskID uuid.UUID, body string) pullRequestContent {
	content := pullRequestContent{}
	for _, notice := range []string{screenshotsRecoveredNote, screenshotsUnsupportedNote, screenshotsOverflowNote} {
		if strings.Contains(body, notice) {
			content.notices = append(content.notices, notice)
		}
	}
	seen := map[string]bool{}
	for _, shot := range previousScreenshotImages(body) {
		if !durableScreenshotURL(shot.URL, owner, repo) {
			content.addScreenshotNotice(screenshotsUnsupportedNote)
			continue
		}
		if !seen[shot.URL] {
			seen[shot.URL] = true
			content.screenshots = append(content.screenshots, shot)
		}
	}
	store, ok := github.(screenshotPublisher)
	if !ok {
		return content
	}
	commit, entries, err := store.ListBranchSnapshot(token, owner, repo, screenshotBranch(taskID))
	if err != nil {
		log.WithField("task_id", taskID).WithField("error", strings.ReplaceAll(safeError(err), token, "[redacted]")).Warn("agent screenshot branch could not be read")
		content.addScreenshotNotice(screenshotsCheckFailedNote)
		return content
	}
	for _, entry := range entries {
		if entry.Type != "blob" || !domain.ValidAgentScreenshotName(entry.Path) {
			continue
		}
		link := screenshotURL(owner, repo, commit, entry.Path)
		if !seen[link] {
			seen[link] = true
			content.screenshots = append(content.screenshots, publishedScreenshot{URL: link, Caption: entry.Path})
			content.addScreenshotNotice(screenshotsRecoveredNote)
		}
	}
	return content
}

// previousScreenshotImages reads Markdown images and standalone image tags.
// Only the destination and alt text are reused.
func previousScreenshotImages(body string) []publishedScreenshot {
	var shots []publishedScreenshot
	var fence string
	for _, line := range strings.Split(htmlComment.ReplaceAllString(body, ""), "\n") {
		trimmed := strings.TrimSpace(line)
		if fence != "" {
			if strings.Trim(trimmed, fence[:1]) == "" && len(trimmed) >= len(fence) {
				fence = ""
			}
			continue
		}
		if strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "\t") {
			continue
		}
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			fence = trimmed[:len(trimmed)-len(strings.TrimLeft(trimmed, trimmed[:1]))]
			continue
		}
		if strings.HasPrefix(strings.ToLower(trimmed), "<img ") {
			tokenizer := html.NewTokenizer(strings.NewReader(trimmed))
			tokenizer.Next()
			var shot publishedScreenshot
			for _, attr := range tokenizer.Token().Attr {
				switch attr.Key {
				case "src":
					shot.URL = attr.Val
				case "alt":
					shot.Caption = attr.Val
				}
			}
			// The bot signature may begin with an avatar followed by its name.
			if shot.URL != "" && tokenizer.Next() == html.ErrorToken {
				shots = append(shots, shot)
			}
			continue
		}
		shots = appendMarkdownScreenshots(shots, line)
	}
	return shots
}

func appendMarkdownScreenshots(shots []publishedScreenshot, line string) []publishedScreenshot {
	for i := 0; i < len(line); {
		switch line[i] {
		case '\\':
			i += 2
			continue
		case '`':
			run := len(line[i:]) - len(strings.TrimLeft(line[i:], "`"))
			if end := closingBackticks(line, i+run, run); end >= 0 {
				i = end + run
			} else {
				i += run
			}
			continue
		case '!':
			if match := markdownScreenshot.FindStringSubmatch(line[i:]); match != nil {
				shots = append(shots, publishedScreenshot{URL: match[2], Caption: match[1]})
				i += len(match[0])
				continue
			}
		}
		i++
	}
	return shots
}

func durableScreenshotURL(value, owner, repo string) bool {
	if len(value) > 2048 || !strings.HasPrefix(value, "https://github.com/") || strings.ContainsAny(value, " \t\r\n<>\\()#") {
		return false
	}
	u, err := url.Parse(value)
	if err != nil || path.Clean(u.Path) != u.Path || strings.ContainsAny(u.Path, "\\\x00\r\n\t") {
		return false
	}
	if u.RawQuery == "" && !u.ForceQuery && githubAttachment.MatchString(u.EscapedPath()) {
		return true
	}
	match := repoScreenshot.FindStringSubmatch(u.EscapedPath())
	return u.RawQuery == "raw=true" && match != nil && strings.EqualFold(match[1], owner) && strings.EqualFold(match[2], repo)
}

func mergeScreenshots(current, previous []publishedScreenshot) []publishedScreenshot {
	seen := make(map[string]bool, len(current))
	for _, shot := range current {
		seen[shot.URL] = true
	}
	for _, shot := range previous {
		if !seen[shot.URL] {
			seen[shot.URL] = true
			current = append(current, shot)
		}
	}
	return current
}

func (content *pullRequestContent) addScreenshotNotice(notice string) {
	if !slices.Contains(content.notices, notice) {
		content.notices = append(content.notices, notice)
	}
}
