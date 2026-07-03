package sanitize

import (
	"html"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/microcosm-cc/bluemonday"
)

var policy *bluemonday.Policy

var (
	allowedHTMLTagPattern = regexp.MustCompile(`(?i)</?(?:p|br|strong|b|em|i|s|del|code|pre|a|h[1-6]|ul|ol|li|blockquote|hr|span|div|label|img|input)(?:\s[^>]*)?/?>`)
	headingPattern        = regexp.MustCompile(`^(#{1,6})\s+(.+)$`)
	taskItemPattern       = regexp.MustCompile(`^\s*[-*+]\s+\[([ xX])\]\s+(.+)$`)
	bulletItemPattern     = regexp.MustCompile(`^\s*[-*+]\s+(.+)$`)
	orderedItemPattern    = regexp.MustCompile(`^\s*\d+[.)]\s+(.+)$`)
	horizontalRulePattern = regexp.MustCompile(`^\s*(?:---+|\*\*\*+|___+)\s*$`)
)

func init() {
	policy = bluemonday.NewPolicy()

	policy.AllowElements(
		"p", "br", "strong", "b", "em", "i", "s", "del", "code", "pre",
		"a",
		"h1", "h2", "h3", "h4", "h5", "h6",
		"ul", "ol", "li", "blockquote", "hr",
		"span", "div", "label",
	)

	policy.AllowAttrs("class").Globally()
	policy.AllowAttrs("data-type").Globally()
	policy.AllowAttrs("data-checked").Globally()
	policy.AllowAttrs("data-id").Globally()
	policy.AllowAttrs("data-label").Globally()
	policy.AllowAttrs("data-kind").Globally()

	policy.AllowAttrs("href", "target", "rel").OnElements("a")
	policy.AllowStandardURLs()
	policy.AddTargetBlankToFullyQualifiedLinks(true)
	policy.RequireNoFollowOnLinks(true)
	policy.RequireNoReferrerOnLinks(true)

	policy.AllowAttrs("type", "checked", "disabled").OnElements("input")

	// Allow images with safe src
	policy.AllowImages()
	policy.AllowAttrs("alt", "width", "height").OnElements("img")
}

// SanitizeHTML sanitizes HTML input, stripping dangerous tags and attributes.
func SanitizeHTML(input string) string {
	return policy.Sanitize(input)
}

// SanitizeEditorContent accepts either the editor's HTML output or markdown
// from API clients and returns safe HTML that the rich editor can render.
func SanitizeEditorContent(input string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return ""
	}
	if allowedHTMLTagPattern.MatchString(input) {
		return SanitizeHTML(input)
	}
	return SanitizeHTML(markdownToEditorHTML(input))
}

// PlainText normalizes text-only fields while preserving literal special
// characters such as <, >, &, quotes, slashes, and colons.
func PlainText(input string) string {
	input = html.UnescapeString(input)

	var b strings.Builder
	lastWasSpace := false
	for _, r := range input {
		if r == '\n' || r == '\r' || r == '\t' {
			if !lastWasSpace {
				b.WriteByte(' ')
				lastWasSpace = true
			}
			continue
		}
		if r < 0x20 || r == 0x7f {
			continue
		}
		b.WriteRune(r)
		lastWasSpace = unicode.IsSpace(r)
	}
	return strings.TrimSpace(b.String())
}

// StripHTML removes all HTML tags, returning plain text only.
func StripHTML(input string) string {
	return bluemonday.StrictPolicy().Sanitize(input)
}

func markdownToEditorHTML(input string) string {
	lines := strings.Split(strings.ReplaceAll(strings.ReplaceAll(input, "\r\n", "\n"), "\r", "\n"), "\n")
	var out strings.Builder
	var paragraph []string
	var quote []string
	var listKind string
	var listItems []string
	var codeFence []string
	inCodeFence := false

	flushParagraph := func() {
		if len(paragraph) == 0 {
			return
		}
		out.WriteString("<p>")
		out.WriteString(renderInlineMarkdown(strings.Join(paragraph, " ")))
		out.WriteString("</p>")
		paragraph = nil
	}

	flushQuote := func() {
		if len(quote) == 0 {
			return
		}
		out.WriteString("<blockquote><p>")
		out.WriteString(renderInlineMarkdown(strings.Join(quote, " ")))
		out.WriteString("</p></blockquote>")
		quote = nil
	}

	flushList := func() {
		if listKind == "" {
			return
		}
		out.WriteString("<")
		out.WriteString(listKind)
		if listKind == "ul" && strings.HasPrefix(strings.Join(listItems, ""), `<li data-type="taskItem"`) {
			out.WriteString(` data-type="taskList"`)
		}
		out.WriteString(">")
		out.WriteString(strings.Join(listItems, ""))
		out.WriteString("</")
		out.WriteString(listKind)
		out.WriteString(">")
		listKind = ""
		listItems = nil
	}

	flushBlocks := func() {
		flushParagraph()
		flushQuote()
		flushList()
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if inCodeFence {
			if strings.HasPrefix(trimmed, "```") {
				out.WriteString("<pre><code>")
				out.WriteString(html.EscapeString(strings.Join(codeFence, "\n")))
				out.WriteString("</code></pre>")
				codeFence = nil
				inCodeFence = false
				continue
			}
			codeFence = append(codeFence, line)
			continue
		}

		if strings.HasPrefix(trimmed, "```") {
			flushBlocks()
			inCodeFence = true
			codeFence = nil
			continue
		}

		if trimmed == "" {
			flushBlocks()
			continue
		}

		if matches := headingPattern.FindStringSubmatch(trimmed); matches != nil {
			flushBlocks()
			level := len(matches[1])
			out.WriteString("<h")
			out.WriteByte(byte('0' + level))
			out.WriteString(">")
			out.WriteString(renderInlineMarkdown(matches[2]))
			out.WriteString("</h")
			out.WriteByte(byte('0' + level))
			out.WriteString(">")
			continue
		}

		if horizontalRulePattern.MatchString(trimmed) {
			flushBlocks()
			out.WriteString("<hr>")
			continue
		}

		if matches := taskItemPattern.FindStringSubmatch(line); matches != nil {
			flushParagraph()
			flushQuote()
			if listKind != "ul" || (len(listItems) > 0 && !strings.HasPrefix(listItems[0], `<li data-type="taskItem"`)) {
				flushList()
				listKind = "ul"
			}
			checked := strings.EqualFold(matches[1], "x")
			dataChecked := "false"
			checkedAttr := ""
			if checked {
				dataChecked = "true"
				checkedAttr = ` checked="checked"`
			}
			listItems = append(listItems, `<li data-type="taskItem" data-checked="`+dataChecked+`"><label><input type="checkbox" disabled="disabled"`+checkedAttr+`><span>`+renderInlineMarkdown(matches[2])+`</span></label></li>`)
			continue
		}

		if matches := bulletItemPattern.FindStringSubmatch(line); matches != nil {
			flushParagraph()
			flushQuote()
			if listKind != "ul" || (len(listItems) > 0 && strings.HasPrefix(listItems[0], `<li data-type="taskItem"`)) {
				flushList()
				listKind = "ul"
			}
			listItems = append(listItems, "<li>"+renderInlineMarkdown(matches[1])+"</li>")
			continue
		}

		if matches := orderedItemPattern.FindStringSubmatch(line); matches != nil {
			flushParagraph()
			flushQuote()
			if listKind != "ol" {
				flushList()
				listKind = "ol"
			}
			listItems = append(listItems, "<li>"+renderInlineMarkdown(matches[1])+"</li>")
			continue
		}

		if strings.HasPrefix(trimmed, ">") {
			flushParagraph()
			flushList()
			quote = append(quote, strings.TrimSpace(strings.TrimPrefix(trimmed, ">")))
			continue
		}

		flushQuote()
		flushList()
		paragraph = append(paragraph, trimmed)
	}

	if inCodeFence {
		out.WriteString("<pre><code>")
		out.WriteString(html.EscapeString(strings.Join(codeFence, "\n")))
		out.WriteString("</code></pre>")
	}
	flushBlocks()

	return out.String()
}

func renderInlineMarkdown(input string) string {
	var out strings.Builder
	for i := 0; i < len(input); {
		if input[i] == '\\' && i+1 < len(input) {
			r, size := utf8.DecodeRuneInString(input[i+1:])
			out.WriteString(html.EscapeString(string(r)))
			i += 1 + size
			continue
		}

		if strings.HasPrefix(input[i:], "`") {
			if end := strings.Index(input[i+1:], "`"); end >= 0 {
				contentEnd := i + 1 + end
				out.WriteString("<code>")
				out.WriteString(html.EscapeString(input[i+1 : contentEnd]))
				out.WriteString("</code>")
				i = contentEnd + 1
				continue
			}
		}

		if htmlLink, next, ok := renderMarkdownLink(input, i); ok {
			out.WriteString(htmlLink)
			i = next
			continue
		}

		if htmlText, next, ok := renderDelimitedInline(input, i, "**", "strong"); ok {
			out.WriteString(htmlText)
			i = next
			continue
		}
		if htmlText, next, ok := renderDelimitedInline(input, i, "__", "strong"); ok {
			out.WriteString(htmlText)
			i = next
			continue
		}
		if htmlText, next, ok := renderDelimitedInline(input, i, "~~", "s"); ok {
			out.WriteString(htmlText)
			i = next
			continue
		}
		if htmlText, next, ok := renderDelimitedInline(input, i, "*", "em"); ok {
			out.WriteString(htmlText)
			i = next
			continue
		}
		if htmlText, next, ok := renderDelimitedInline(input, i, "_", "em"); ok {
			out.WriteString(htmlText)
			i = next
			continue
		}

		r, size := utf8.DecodeRuneInString(input[i:])
		out.WriteString(html.EscapeString(string(r)))
		i += size
	}
	return out.String()
}

func renderMarkdownLink(input string, offset int) (string, int, bool) {
	if input[offset] != '[' {
		return "", offset, false
	}
	labelEndRel := strings.Index(input[offset+1:], "](")
	if labelEndRel < 0 {
		return "", offset, false
	}
	labelEnd := offset + 1 + labelEndRel
	hrefStart := labelEnd + 2
	hrefEndRel := strings.Index(input[hrefStart:], ")")
	if hrefEndRel < 0 {
		return "", offset, false
	}
	hrefEnd := hrefStart + hrefEndRel
	href := strings.TrimSpace(input[hrefStart:hrefEnd])
	if !isSafeURL(href) {
		return "", offset, false
	}
	return `<a href="` + html.EscapeString(href) + `">` + renderInlineMarkdown(input[offset+1:labelEnd]) + `</a>`, hrefEnd + 1, true
}

func renderDelimitedInline(input string, offset int, delimiter, tag string) (string, int, bool) {
	if !strings.HasPrefix(input[offset:], delimiter) {
		return "", offset, false
	}
	contentStart := offset + len(delimiter)
	contentEndRel := strings.Index(input[contentStart:], delimiter)
	if contentEndRel <= 0 {
		return "", offset, false
	}
	contentEnd := contentStart + contentEndRel
	return "<" + tag + ">" + renderInlineMarkdown(input[contentStart:contentEnd]) + "</" + tag + ">", contentEnd + len(delimiter), true
}

func isSafeURL(value string) bool {
	lower := strings.ToLower(value)
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "mailto:")
}
