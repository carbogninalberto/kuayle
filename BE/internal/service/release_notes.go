package service

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The release notes style is a workspace skill an owner or admin can edit in
// Settings → Assistant → Skills. Its body is guidance for the assistant; its
// "Sections:" line also decides which sections draft_release_notes renders and
// in what order.

const (
	releaseNotesSkillName        = "Release notes style"
	releaseNotesSkillDescription = "How this workspace writes release notes: sections, order and wording. Use it with draft_release_notes and create_release."
)

const defaultReleaseNotesStyle = `Use this style for every release of the workspace's repositories (draft_release_notes, create_release).

Sections: Features, Improvements, Fixed, Upgrade, Validation, PR links, Full Changelog

- Start each section with "### <name>" and leave out a section with nothing in it.
- One bullet per change: a bold one-line summary with the issue key, such as **Releases from chat (KUA-88).**, then plain sentences on what changed for users and administrators. Put details in nested bullets.
- Features are new capabilities; Improvements change existing behaviour, performance or CI; Fixed names the bug and what was wrong before.
- Upgrade lists each migration by number with what it does, settings or GitHub App permissions administrators must change, and "Back up the database first" when there are migrations.
- Validation lists the automated tests added and the checks that passed on the main branch.
- PR links is one line: "PRs: <url>, <url>". Full Changelog is "**Full Changelog**: <compare url>".
- Never include secrets, tokens, private host names or addresses, local paths or customer data.
`

var defaultReleaseSections = []string{"Features", "Improvements", "Fixed", "Upgrade", "Validation", "PR links", "Full Changelog"}

var sectionsLine = regexp.MustCompile(`(?im)^\s*sections\s*:\s*(.+)$`)

// parseReleaseSections reads the style's "Sections:" line; without one the
// default order applies.
func parseReleaseSections(style string) []string {
	match := sectionsLine.FindStringSubmatch(style)
	if match == nil {
		return append([]string(nil), defaultReleaseSections...)
	}
	var sections []string
	for _, part := range strings.Split(match[1], ",") {
		if name := strings.TrimSpace(strings.Trim(strings.TrimSpace(part), "*_`.")); name != "" && len(sections) < 20 {
			sections = append(sections, truncateRunes(name, 60))
		}
	}
	if len(sections) == 0 {
		return append([]string(nil), defaultReleaseSections...)
	}
	return sections
}

// sectionKind maps a section name to what the draft fills it with, or "".
func sectionKind(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.Contains(lower, "feature"):
		return "features"
	case strings.Contains(lower, "improve"):
		return "improvements"
	case strings.Contains(lower, "fix") || strings.Contains(lower, "bug"):
		return "fixed"
	case strings.Contains(lower, "upgrade") || strings.Contains(lower, "migration"):
		return "upgrade"
	case strings.Contains(lower, "validat") || strings.Contains(lower, "test"):
		return "validation"
	case strings.Contains(lower, "changelog"):
		return "changelog"
	case strings.Contains(lower, "pull request") || strings.Contains(lower, "pr link") || lower == "prs" || lower == "pr":
		return "prs"
	}
	return ""
}

// unrenderedSections are the style's sections the draft cannot fill; the
// assistant writes them from the pull requests.
func unrenderedSections(sections []string) []string {
	var out []string
	for _, s := range sections {
		if sectionKind(s) == "" {
			out = append(out, s)
		}
	}
	return out
}

type releasePull struct {
	Number int      `json:"number"`
	Title  string   `json:"title"`
	URL    string   `json:"url"`
	Author string   `json:"author,omitempty"`
	Labels []string `json:"labels,omitempty"`
	Body   string   `json:"body,omitempty"`
}

type releaseNotesInput struct {
	Repository, PreviousTag, Tag, TargetCommit, Branch string
	Pulls                                              []releasePull
	// Migrations are the migration files the range adds.
	Migrations   []string
	ChecksPassed int
}

var conventionalPrefix = regexp.MustCompile(`^([a-zA-Z]+)(\([^)]*\))?!?:\s*`)

// pullCategory sorts a pull request by its conventional-commit prefix, then by
// its labels; anything else is an improvement for the assistant to refine.
func pullCategory(pull releasePull) string {
	if match := conventionalPrefix.FindStringSubmatch(pull.Title); match != nil {
		switch strings.ToLower(match[1]) {
		case "feat", "feature":
			return "features"
		case "fix", "bugfix", "hotfix":
			return "fixed"
		}
	}
	for _, label := range pull.Labels {
		switch strings.ToLower(label) {
		case "bug", "fix", "bugfix":
			return "fixed"
		case "feature", "enhancement", "feat":
			return "features"
		}
	}
	return "improvements"
}

func pullSummary(title string) string {
	if match := conventionalPrefix.FindStringSubmatch(title); match != nil && isConventionalType(match[1]) {
		title = title[len(match[0]):]
	}
	title = strings.TrimSpace(strings.TrimRight(strings.TrimSpace(title), "."))
	if r, size := utf8.DecodeRuneInString(title); size > 0 {
		title = string(unicode.ToUpper(r)) + title[size:]
	}
	return title
}

func isConventionalType(kind string) bool {
	switch strings.ToLower(kind) {
	case "feat", "feature", "fix", "bugfix", "hotfix", "perf", "refactor", "chore", "ci", "docs", "build", "test", "style", "revert", "bump", "deps":
		return true
	}
	return false
}

// renderReleaseNotes drafts the notes in the style's sections and order. The
// assistant then rewrites the bullets from the pull requests; the structure,
// links and validation line come from here.
func renderReleaseNotes(sections []string, in releaseNotesInput) string {
	var b strings.Builder
	byCategory := map[string][]releasePull{}
	for _, pull := range in.Pulls {
		category := pullCategory(pull)
		byCategory[category] = append(byCategory[category], pull)
	}
	block := func(name string, lines []string) {
		if len(lines) == 0 {
			return
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		if name != "" {
			b.WriteString("### " + name + "\n")
		}
		for _, line := range lines {
			b.WriteString(line + "\n")
		}
	}
	for _, name := range sections {
		var lines []string
		switch kind := sectionKind(name); kind {
		case "features", "improvements", "fixed":
			for _, pull := range byCategory[kind] {
				lines = append(lines, fmt.Sprintf("- **%s** (#%d)", pullSummary(pull.Title), pull.Number))
			}
			block(name, lines)
		case "upgrade":
			if len(in.Migrations) > 0 {
				names := make([]string, len(in.Migrations))
				for i, file := range in.Migrations {
					names[i] = "`" + file[strings.LastIndex(file, "/")+1:] + "`"
				}
				lines = append(lines, "- **Migrations:** "+strings.Join(names, ", ")+". Back up the database first.")
			}
			block(name, lines)
		case "validation":
			line := fmt.Sprintf("- All %d checks passed on %s at %s.", in.ChecksPassed, in.Branch, shortSHA(in.TargetCommit))
			if in.ChecksPassed == 0 {
				line = fmt.Sprintf("- The checks of %s at %s have not passed yet; create_release refuses until they do.", in.Branch, shortSHA(in.TargetCommit))
			}
			block(name, []string{line})
		case "prs":
			if len(in.Pulls) > 0 {
				urls := make([]string, len(in.Pulls))
				for i, pull := range in.Pulls {
					urls[i] = pull.URL
				}
				block("", []string{"PRs: " + strings.Join(urls, ", ")})
			}
		case "changelog":
			link := fmt.Sprintf("https://github.com/%s/compare/%s...%s", in.Repository, in.PreviousTag, in.Tag)
			if in.PreviousTag == "" {
				link = fmt.Sprintf("https://github.com/%s/commits/%s", in.Repository, in.Tag)
			}
			block("", []string{"**Full Changelog**: " + link})
		}
	}
	return b.String()
}

var (
	mergeCommitPull  = regexp.MustCompile(`^Merge pull request #(\d+) `)
	squashCommitPull = regexp.MustCompile(`\(#(\d+)\)\s*$`)
)

// pullNumbersFromMessages finds the pull requests a range merged from its
// commit messages: GitHub's merge commits and squash titles ending in (#N).
func pullNumbersFromMessages(messages []string) []int {
	var numbers []int
	seen := map[int]bool{}
	for _, message := range messages {
		first, _, _ := strings.Cut(message, "\n")
		match := mergeCommitPull.FindStringSubmatch(first)
		if match == nil {
			match = squashCommitPull.FindStringSubmatch(first)
		}
		if match == nil {
			continue
		}
		if n, err := strconv.Atoi(match[1]); err == nil && !seen[n] {
			seen[n] = true
			numbers = append(numbers, n)
		}
	}
	return numbers
}

var semverTag = regexp.MustCompile(`^(v?)(\d+)\.(\d+)\.(\d+)$`)

// nextReleaseTag suggests the next patch version, or "" when the previous tag
// is not a plain version.
func nextReleaseTag(previous string) string {
	match := semverTag.FindStringSubmatch(previous)
	if match == nil {
		return ""
	}
	patch, _ := strconv.Atoi(match[4])
	return fmt.Sprintf("%s%s.%s.%d", match[1], match[2], match[3], patch+1)
}
