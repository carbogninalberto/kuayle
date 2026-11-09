package service

import (
	"regexp"
	"strings"

	"github.com/kuayle/kuayle-backend/pkg/sanitize"
)

var wikiHeading = regexp.MustCompile(`(?is)<h([1-6])(?:\s[^>]*)?>(.*?)</h[1-6]>`)

// wikiHeadings lists the page's headings as text, in order.
func wikiHeadings(content string) []string {
	headings := []string{}
	for _, match := range wikiHeading.FindAllStringSubmatch(content, -1) {
		if text := strings.TrimSpace(sanitize.StripHTML(match[2])); text != "" {
			headings = append(headings, text)
		}
	}
	return headings
}

// wikiSection finds the first heading whose text matches heading (ignoring
// case and surrounding space). It returns where the heading ends and where
// its section ends: at the next heading of the same or a higher level, or at
// the end of the page.
func wikiSection(content, heading string) (headingEnd, sectionEnd int, ok bool) {
	want := strings.ToLower(strings.Join(strings.Fields(heading), " "))
	matches := wikiHeading.FindAllStringSubmatchIndex(content, -1)
	for i, match := range matches {
		text := strings.ToLower(strings.Join(strings.Fields(sanitize.StripHTML(content[match[4]:match[5]])), " "))
		if text != want {
			continue
		}
		level := content[match[2]:match[3]]
		end := len(content)
		for _, next := range matches[i+1:] {
			if content[next[2]:next[3]] <= level {
				end = next[0]
				break
			}
		}
		return match[1], end, true
	}
	return 0, 0, false
}

// replaceWikiSection replaces what follows heading, up to its section's end,
// with addition; insertAfterWikiHeading puts addition right after it.
func replaceWikiSection(content, heading, addition string) (string, bool) {
	start, end, ok := wikiSection(content, heading)
	if !ok {
		return "", false
	}
	return content[:start] + addition + content[end:], true
}

func insertAfterWikiHeading(content, heading, addition string) (string, bool) {
	start, _, ok := wikiSection(content, heading)
	if !ok {
		return "", false
	}
	return content[:start] + addition + content[start:], true
}
