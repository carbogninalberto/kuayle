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
	editorHTMLTokenPattern = regexp.MustCompile("(?i)`[^`\r\n]*`|" +
		`</?(?:p|br|strong|b|em|i|s|del|code|pre|a|h[1-6]|ul|ol|li|blockquote|hr|span|div|label|img|input|table|thead|tbody|tfoot|tr|th|td)(?:\s[^>]*)?/?>`)
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
		"table", "thead", "tbody", "tfoot", "tr", "th", "td", "colgroup", "col",
	)
	policy.AllowAttrs("colspan", "rowspan").Matching(regexp.MustCompile(`^[1-9][0-9]{0,2}$`)).OnElements("th", "td")
	policy.AllowAttrs("align").Matching(regexp.MustCompile(`^(left|center|right)$`)).OnElements("th", "td")

	// Column widths the editor's resizable tables persist: a cell's widths in
	// colwidth, and nothing but pixel width/min-width styles on colgroup
	// columns and the table itself. Any other style declaration is dropped.
	// The width attribute must be a bare integer (px optional): the editor
	// reads it with parseInt, so a value like 25% would load as 25px. A 0
	// colwidth entry is the editor's placeholder for the columns a merged
	// cell spans, so it stays too.
	policy.AllowAttrs("colwidth").Matching(regexp.MustCompile(`^[0-9]{1,4}(,[0-9]{1,4})*$`)).OnElements("th", "td")
	policy.AllowAttrs("width").Matching(regexp.MustCompile(`^[1-9][0-9]{0,3}(px)?$`)).OnElements("col")
	policy.AllowStyles("width", "min-width").Matching(regexp.MustCompile(`^[0-9]{1,4}(\.[0-9]+)?px$`)).OnElements("table", "col")

	// Class values are narrowed to the editor's own names by restrictClasses.
	policy.AllowAttrs("class").Globally()
	policy.AllowAttrs("data-type").Globally()
	policy.AllowAttrs("data-checked").Globally()
	policy.AllowAttrs("data-id").Globally()
	policy.AllowAttrs("data-label").Globally()
	policy.AllowAttrs("data-kind").Globally()
	policy.AllowAttrs("data-identifier").Globally()
	policy.AllowAttrs("data-filename").Globally()
	policy.AllowAttrs("data-size").Globally()

	policy.AllowAttrs("href", "target", "rel", "download").OnElements("a")
	policy.AllowStandardURLs()
	policy.AllowRelativeURLs(true)
	policy.AddTargetBlankToFullyQualifiedLinks(true)
	policy.RequireNoFollowOnLinks(true)
	policy.RequireNoReferrerOnLinks(true)

	policy.AllowAttrs("type", "checked", "disabled").OnElements("input")

	// Allow images with safe src
	policy.AllowImages()
	policy.AllowAttrs("alt", "width", "height").OnElements("img")
}

// SanitizeHTML sanitizes HTML input, stripping dangerous tags and attributes
// and every class name the editor does not emit.
func SanitizeHTML(input string) string {
	return canonicalWikiMentions(restrictClasses(policy.Sanitize(input)))
}

// SanitizeEditorContent accepts either the editor's HTML output or markdown
// from API clients and returns safe HTML that the rich editor can render.
func SanitizeEditorContent(input string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return ""
	}
	return SanitizeHTML(renderEditorContent(input, true))
}

// EditorMarkdown converts Markdown to safe editor HTML without the HTML
// detection SanitizeEditorContent does: a literal tag in the input stays
// visible text. Importers use it for text written in another tracker.
func EditorMarkdown(input string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return ""
	}
	return SanitizeHTML(renderEditorContent(input, false))
}

func containsEditorHTML(input string) bool {
	for match := editorHTMLTokenPattern.FindStringIndex(input); match != nil; match = editorHTMLTokenPattern.FindStringIndex(input) {
		if input[match[0]] == '<' {
			return true
		}
		input = input[match[1]:]
	}
	return false
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

func renderEditorContent(input string, acceptHTML bool) string {
	if acceptHTML && !strings.ContainsAny(input, "|`") && containsEditorHTML(input) {
		return input
	}
	lines := strings.Split(strings.ReplaceAll(strings.ReplaceAll(input, "\r\n", "\n"), "\r", "\n"), "\n")
	// HTML inside a recognized Markdown table or code fence does not determine
	// the format of the whole body. Check the ranges outside those blocks.
	htmlStartLine := 0
	containsHTMLBefore := func(end int) bool {
		return acceptHTML && containsEditorHTML(strings.Join(lines[htmlStartLine:end], "\n"))
	}
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

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		if inCodeFence {
			if strings.HasPrefix(trimmed, "```") {
				out.WriteString("<pre><code>")
				out.WriteString(html.EscapeString(strings.Join(codeFence, "\n")))
				out.WriteString("</code></pre>")
				codeFence = nil
				inCodeFence = false
				htmlStartLine = i + 1
				continue
			}
			codeFence = append(codeFence, line)
			continue
		}

		if strings.HasPrefix(trimmed, "```") {
			if containsHTMLBefore(i) {
				return input
			}
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

		if i+1 < len(lines) {
			if table, consumed := renderMarkdownTable(lines[i:], acceptHTML); consumed > 0 {
				if containsHTMLBefore(i) {
					return input
				}
				flushBlocks()
				out.WriteString(table)
				htmlStartLine = i + consumed
				i += consumed - 1
				continue
			}
		}

		flushQuote()
		flushList()
		paragraph = append(paragraph, trimmed)
	}

	if inCodeFence {
		out.WriteString("<pre><code>")
		out.WriteString(html.EscapeString(strings.Join(codeFence, "\n")))
		out.WriteString("</code></pre>")
	} else if containsHTMLBefore(len(lines)) {
		return input
	}
	flushBlocks()

	return out.String()
}

func renderInlineMarkdown(input string) string {
	return renderInlineMarkdownWithHTML(input, false)
}

func renderInlineMarkdownWithHTML(input string, acceptHTML bool) string {
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

		if acceptHTML && input[i] == '<' {
			if tag := tableCellHTMLPattern.FindString(input[i:]); tag != "" {
				out.WriteString(tag)
				i += len(tag)
				continue
			}
		}

		if htmlLink, next, ok := renderMarkdownLink(input, i, acceptHTML); ok {
			out.WriteString(htmlLink)
			i = next
			continue
		}

		if htmlText, next, ok := renderDelimitedInline(input, i, "**", "strong", acceptHTML); ok {
			out.WriteString(htmlText)
			i = next
			continue
		}
		if htmlText, next, ok := renderDelimitedInline(input, i, "__", "strong", acceptHTML); ok {
			out.WriteString(htmlText)
			i = next
			continue
		}
		if htmlText, next, ok := renderDelimitedInline(input, i, "~~", "s", acceptHTML); ok {
			out.WriteString(htmlText)
			i = next
			continue
		}
		if htmlText, next, ok := renderDelimitedInline(input, i, "*", "em", acceptHTML); ok {
			out.WriteString(htmlText)
			i = next
			continue
		}
		if htmlText, next, ok := renderDelimitedInline(input, i, "_", "em", acceptHTML); ok {
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

func renderMarkdownLink(input string, offset int, acceptHTML bool) (string, int, bool) {
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
	return `<a href="` + html.EscapeString(href) + `">` + renderInlineMarkdownWithHTML(input[offset+1:labelEnd], acceptHTML) + `</a>`, hrefEnd + 1, true
}

func renderDelimitedInline(input string, offset int, delimiter, tag string, acceptHTML bool) (string, int, bool) {
	if !strings.HasPrefix(input[offset:], delimiter) {
		return "", offset, false
	}
	contentStart := offset + len(delimiter)
	contentEndRel := strings.Index(input[contentStart:], delimiter)
	if contentEndRel <= 0 {
		return "", offset, false
	}
	contentEnd := contentStart + contentEndRel
	return "<" + tag + ">" + renderInlineMarkdownWithHTML(input[contentStart:contentEnd], acceptHTML) + "</" + tag + ">", contentEnd + len(delimiter), true
}

func isSafeURL(value string) bool {
	lower := strings.ToLower(value)
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "mailto:")
}
