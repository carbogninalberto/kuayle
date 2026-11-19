package machine

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode"

	"github.com/kuayle/kuayle-backend/internal/domain"
)

// pullRequestBodyMaxBytes keeps a rendered description well below GitHub's
// 65,536-character limit, whatever the template repeats.
const pullRequestBodyMaxBytes = 60000

// pullRequestContent is what an attempt left for its pull request: its
// parsed .kuayle/pull_request.json (nil without a usable one), its published
// screenshots and any evidence carried forward from a previous publication.
type pullRequestContent struct {
	file        *domain.AgentPullRequest
	screenshots []publishedScreenshot
	notices     []string
}

type publishedScreenshot struct {
	URL     string
	Caption string
}

// publicationBody renders the pull request description from the configured
// template (or the default one). The agent writes the description, changes,
// verification, notes and screenshots; everything it wrote is sanitized here
// (sanitizeAgentMarkdown). Kuayle adds the issue reference and, when its
// independent check passed, one line saying so. A section whose placeholders
// all render empty is left out, heading included. Without the agent's file
// the description is the issue line and Kuayle's facts.
func publicationBody(publication *domain.AgentPublication, content pullRequestContent, frontendURL string) string {
	template := publication.PullRequestTemplate
	if domain.ValidatePullRequestTemplate(template) != nil {
		template = domain.DefaultPullRequestTemplate
	}
	file := domain.AgentPullRequest{}
	if content.file != nil {
		file = *content.file
	}
	own := publication.IssueIdentifier
	clean := func(text string) string { return sanitizeAgentMarkdown(text, own) }
	issue := issueReference(publication, frontendURL)
	values := map[string]string{"issue": issue}

	description := clean(file.Description)
	if !strings.Contains(template, "{{issue}}") {
		lead := "Addresses " + issue
		if !strings.HasSuffix(lead, ".") && !strings.HasSuffix(lead, "!") && !strings.HasSuffix(lead, "?") {
			lead += "."
		}
		description = joinBlocks(lead, description)
	}
	values["description"] = description
	values["changes"] = bulletList(file.Changes, clean)

	verification := bulletList(file.Verification, clean)
	if verification == "" {
		// The agent's own account of the tests it chose for Kuayle to re-run.
		verification = clean(publication.AgentSummary)
	}
	values["verification"] = joinBlocks(verification, clean(independentCheckLine(publication, verification != "")))

	shots := make([]string, 0, len(content.screenshots))
	for _, shot := range content.screenshots {
		caption := clean(strings.Join(strings.Fields(strings.ReplaceAll(shot.Caption, zeroWidthSpace, "")), " "))
		alt := strings.NewReplacer("[", "", "]", "", "\\", "").Replace(caption)
		image := "![" + alt + "](" + shot.URL + ")"
		if caption != "" {
			image = caption + "\n" + image
		}
		shots = append(shots, image)
	}
	values["screenshots"] = strings.Join(shots, "\n\n")
	values["notes"] = clean(file.Notes)

	template = sanitizeTemplate(template)
	body := renderPullRequestTemplate(template, values)
	notices := strings.Join(content.notices, "\n\n")
	if len(shots) == 0 || strings.Contains(template, "{{screenshots}}") && len(body)+len(notices)+2 <= pullRequestBodyMaxBytes {
		return bodyWithScreenshotEvidence(body, notices)
	}

	// Keep screenshots visible when the template omits them or expands past
	// the body limit. Reserve space for notices and include only complete images.
	values["screenshots"] = ""
	body = renderPullRequestTemplate(template, values)
	evidence := "## Screenshots"
	// Separate the description, images and notes with blank lines and end
	// the body with a newline.
	evidenceLimit := pullRequestBodyMaxBytes - len(notices) - len(screenshotsOverflowNote) - 7
	for _, shot := range shots {
		if len(evidence)+len(shot)+2 > evidenceLimit {
			content.addScreenshotNotice(screenshotsOverflowNote)
			break
		}
		evidence += "\n\n" + shot
	}
	return bodyWithScreenshotEvidence(body, joinBlocks(evidence, strings.Join(content.notices, "\n\n")))
}

func bodyWithScreenshotEvidence(body, evidence string) string {
	if evidence == "" {
		return truncateText(body, pullRequestBodyMaxBytes)
	}
	if len(body)+len(evidence)+2 <= pullRequestBodyMaxBytes {
		return joinBlocks(body, evidence) + "\n"
	}
	// A truncated description can leave an open Markdown block. Put the
	// evidence first so it still renders in that case.
	return joinBlocks(evidence, truncateText(body, pullRequestBodyMaxBytes-len(evidence)-3)) + "\n"
}

// issueReference is "KUA-12: title", the identifier linked to the issue in
// Kuayle when Kuayle's public address and the workspace are known.
func issueReference(publication *domain.AgentPublication, frontendURL string) string {
	identifier := publication.IssueIdentifier
	title := sanitizeAgentMarkdown(strings.Join(strings.Fields(truncateText(publication.IssueTitle, 300)), " "), identifier)
	reference := identifier
	if link := kuayleIssueURL(frontendURL, publication.WorkspaceSlug, identifier); link != "" {
		reference = "[" + identifier + "](" + link + ")"
	}
	if title == "" {
		return reference
	}
	return reference + ": " + title
}

var issueIdentifierFormat = regexp.MustCompile(`^[A-Z][A-Z0-9]+-[0-9]+$`)

// kuayleIssueURL is the issue's page in Kuayle, or "" when the address,
// workspace or identifier cannot form a safe link.
func kuayleIssueURL(frontendURL, slug, identifier string) string {
	base, err := url.Parse(frontendURL)
	if err != nil || base.Host == "" || (base.Scheme != "https" && base.Scheme != "http") || base.User != nil || base.RawQuery != "" || base.Fragment != "" || slug == "" || !issueIdentifierFormat.MatchString(identifier) {
		return ""
	}
	link := strings.TrimRight(base.String(), "/") + "/" + url.PathEscape(slug) + "/issue/" + identifier
	return strings.NewReplacer("(", "%28", ")", "%29", "<", "%3C", ">", "%3E", " ", "%20").Replace(link)
}

// independentCheckLine is Kuayle's own check result, when one passed; also
// says the agent's own verification comes before it.
func independentCheckLine(publication *domain.AgentPublication, also bool) string {
	if !publicationValidated(publication) {
		return ""
	}
	command := func(argv []string) string {
		return "`" + strings.ReplaceAll(truncateText(strings.Join(strings.Fields(strings.Join(argv, " ")), " "), 160), "`", "'") + "`"
	}
	if publication.CheckSource == domain.CheckSourceAgent {
		if len(publication.CheckCommands) > 1 {
			return fmt.Sprintf("Kuayle re-ran the %d commands the agent chose independently in a clean container: all passed.", len(publication.CheckCommands))
		}
		return "Kuayle re-ran " + command(publication.CheckArgv) + " independently in a clean container: passed."
	}
	if also {
		return "Kuayle also ran " + command(publication.CheckArgv) + " independently in a clean container: passed."
	}
	return "Kuayle ran " + command(publication.CheckArgv) + " independently in a clean container: passed."
}

func bulletList(items []string, clean func(string) string) string {
	lines := make([]string, 0, len(items))
	for _, item := range items {
		if item = clean(strings.Join(strings.Fields(item), " ")); item != "" {
			lines = append(lines, "- "+item)
		}
	}
	return strings.Join(lines, "\n")
}

func joinBlocks(blocks ...string) string {
	kept := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if block = strings.TrimSpace(block); block != "" {
			kept = append(kept, block)
		}
	}
	return strings.Join(kept, "\n\n")
}

var (
	templateHeading = regexp.MustCompile(`^ {0,3}#{1,6}(\s|$)`)
	htmlComment     = regexp.MustCompile(`(?s)<!--.*?-->`)
	blankLines      = regexp.MustCompile(`\n{3,}`)
)

// renderPullRequestTemplate replaces each {{name}} once (values are never
// expanded again) and leaves out a section, from its heading to the next,
// when it has placeholders that all render empty and no other text.
func renderPullRequestTemplate(template string, values map[string]string) string {
	var sections [][]string
	for i, line := range strings.Split(template, "\n") {
		if i == 0 || templateHeading.MatchString(line) {
			sections = append(sections, nil)
		}
		sections[len(sections)-1] = append(sections[len(sections)-1], line)
	}
	var out []string
	for _, section := range sections {
		text := strings.Join(section, "\n")
		placeholders := domain.PullRequestPlaceholderPattern.FindAllStringSubmatch(text, -1)
		if len(placeholders) > 0 {
			empty := true
			for _, placeholder := range placeholders {
				empty = empty && strings.TrimSpace(values[placeholder[1]]) == ""
			}
			rest := section
			if templateHeading.MatchString(section[0]) {
				rest = section[1:]
			}
			static := domain.PullRequestPlaceholderPattern.ReplaceAllString(strings.Join(rest, "\n"), "")
			if empty && strings.TrimSpace(static) == "" {
				continue
			}
		}
		out = append(out, domain.PullRequestPlaceholderPattern.ReplaceAllStringFunc(text, func(match string) string {
			return values[match[2:len(match)-2]]
		}))
	}
	lines := strings.Split(strings.Join(out, "\n"), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRightFunc(line, unicode.IsSpace)
	}
	return strings.TrimSpace(blankLines.ReplaceAllString(strings.Join(lines, "\n"), "\n\n")) + "\n"
}

// sanitizeTemplate keeps a configured template's markdown structure but
// drops HTML comments and, like agent text, renders raw HTML as text and
// stops mentions and closing keywords: an issue's template can be set by
// workspace members who cannot write to the repository.
func sanitizeTemplate(template string) string {
	template = strings.ReplaceAll(htmlComment.ReplaceAllString(strings.ReplaceAll(template, "\r\n", "\n"), ""), "\r", "\n")
	return neutralizeInline(stopClosingKeywords(template))
}

const zeroWidthSpace = "\u200b"

var (
	// GitHub closes an issue from a pull request description that says, for
	// example, "Fixes #12", "closes owner/repo#3", "resolved: <issue URL>" or
	// "fixes GH-5".
	closingKeyword = regexp.MustCompile(`(?i)\b(close[sd]?|fix(?:e[sd])?|resolve[sd]?)\b(\s*:?\s*)(#|gh-\d|[a-z0-9_.-]+/[a-z0-9_.-]+#|https?://)`)
	// Kuayle links a pull request to the issue whose identifier its title,
	// branch or description names, and to none when they name several
	// (forge_linking.go uses the same pattern and word boundaries).
	issueIdentifierLike = regexp.MustCompile(`(?i)([A-Z][A-Z0-9]+-\d+)`)
	// Lines that would open a heading, a code fence or a setext heading.
	blockMarker = regexp.MustCompile("^( {0,3})(#|```|~~~|=+[ \\t]*$|-+[ \\t]*$)")
)

// sanitizeAgentMarkdown makes agent text safe to place in a section: valid
// one-paragraph-break markdown without control characters; no headings, code
// fences or setext underlines that could restructure the description; raw
// HTML and images shown as text; no mentions or GitHub closing keywords; and
// no identifier of another Kuayle issue, so the pull request stays linked to
// its own (own). Inline code spans keep their text except closing keywords
// and identifiers, which GitHub and Kuayle read inside code too.
func sanitizeAgentMarkdown(text, own string) string {
	text = strings.ToValidUTF8(strings.ReplaceAll(text, "\r\n", "\n"), "")
	text = strings.Map(func(r rune) rune {
		if r == '\r' {
			return '\n'
		}
		if r != '\n' && r != '\t' && (unicode.IsControl(r) || r == '\u2028' || r == '\u2029') {
			return -1
		}
		return r
	}, text)
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if match := blockMarker.FindStringSubmatchIndex(line); match != nil {
			lines[i] = line[:match[3]] + "\\" + line[match[3]:]
		}
	}
	text = strings.Join(lines, "\n")
	text = stopIssueIdentifiers(stopClosingKeywords(text), own)
	text = neutralizeInline(text)
	return strings.TrimSpace(blankLines.ReplaceAllString(text, "\n\n"))
}

func stopClosingKeywords(text string) string {
	return closingKeyword.ReplaceAllStringFunc(text, func(match string) string {
		return match[:1] + zeroWidthSpace + match[1:]
	})
}

// stopIssueIdentifiers breaks identifiers Kuayle's pull request linking
// would read, except the task's own.
func stopIssueIdentifiers(text, own string) string {
	isWord := func(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' }
	var b strings.Builder
	last := 0
	for _, loc := range issueIdentifierLike.FindAllStringIndex(text, -1) {
		if loc[0] > 0 && isWord(text[loc[0]-1]) || loc[1] < len(text) && isWord(text[loc[1]]) || strings.EqualFold(text[loc[0]:loc[1]], own) {
			continue
		}
		dash := loc[0] + strings.IndexByte(text[loc[0]:loc[1]], '-')
		b.WriteString(text[last:dash])
		b.WriteString(zeroWidthSpace)
		last = dash
	}
	b.WriteString(text[last:])
	return b.String()
}

// neutralizeInline renders raw HTML and images as text and stops mentions
// outside inline code spans; a code span is a run of backticks closed by a
// run of the same length within the paragraph.
func neutralizeInline(text string) string {
	var b strings.Builder
	for i := 0; i < len(text); {
		switch c := text[i]; {
		case c == '`':
			n := 0
			for i+n < len(text) && text[i+n] == '`' {
				n++
			}
			if end := closingBackticks(text, i+n, n); end >= 0 {
				b.WriteString(text[i : end+n])
				i = end + n
				continue
			}
			b.WriteString(text[i : i+n])
			i += n
			continue
		case c == '<':
			b.WriteString("&lt;")
		case c == '@':
			b.WriteString("@" + zeroWidthSpace)
		case c == '!' && i+1 < len(text) && text[i+1] == '[':
			b.WriteString("!\\[")
			i += 2
			continue
		default:
			b.WriteByte(c)
		}
		i++
	}
	return b.String()
}

// closingBackticks finds a run of exactly n backticks from start, before a
// blank line ends the paragraph; -1 without one.
func closingBackticks(text string, start, n int) int {
	for j := start; j < len(text); {
		if text[j] == '\n' && j+1 < len(text) && text[j+1] == '\n' {
			return -1
		}
		if text[j] != '`' {
			j++
			continue
		}
		m := 0
		for j+m < len(text) && text[j+m] == '`' {
			m++
		}
		if m == n {
			return j
		}
		j += m
	}
	return -1
}
