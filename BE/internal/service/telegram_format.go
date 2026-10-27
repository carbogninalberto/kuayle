package service

import (
	"html"
	"regexp"
	"strings"
)

// Telegram's HTML parse mode accepts only a few tags, and rejects a message
// whose tags are unbalanced. The assistant writes Markdown, so it is converted
// here: every tag is emitted in balanced pairs and all other text is escaped.
// Incomplete syntax, as in a reply still being written, stays literal.

var (
	telegramHeading  = regexp.MustCompile(`^#{1,6}\s+(.*)$`)
	telegramBullet   = regexp.MustCompile(`^\s*[-*+]\s+(.*)$`)
	telegramLinkURL  = regexp.MustCompile(`^https?://[^\s"'<>()]+$`)
	telegramFenceTag = regexp.MustCompile("^```")
)

func telegramHTML(markdown string) string {
	lines := strings.Split(markdown, "\n")
	out := make([]string, 0, len(lines))
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if telegramFenceTag.MatchString(strings.TrimSpace(line)) {
			code := []string{}
			for i++; i < len(lines) && !telegramFenceTag.MatchString(strings.TrimSpace(lines[i])); i++ {
				code = append(code, lines[i])
			}
			out = append(out, "<pre><code>"+html.EscapeString(strings.Join(code, "\n"))+"</code></pre>")
			continue
		}
		if strings.HasPrefix(line, ">") {
			quote := []string{}
			for ; i < len(lines) && strings.HasPrefix(lines[i], ">"); i++ {
				quote = append(quote, telegramInline(strings.TrimSpace(strings.TrimPrefix(lines[i], ">"))))
			}
			i--
			out = append(out, "<blockquote>"+strings.Join(quote, "\n")+"</blockquote>")
			continue
		}
		if match := telegramHeading.FindStringSubmatch(line); match != nil {
			out = append(out, "<b>"+telegramInline(match[1])+"</b>")
			continue
		}
		if match := telegramBullet.FindStringSubmatch(line); match != nil {
			out = append(out, "• "+telegramInline(match[1]))
			continue
		}
		out = append(out, telegramInline(line))
	}
	return strings.Join(out, "\n")
}

// telegramInline converts code spans, bold, italic, strikethrough and http(s)
// links; anything unmatched is escaped text.
func telegramInline(text string) string {
	var b strings.Builder
	for i := 0; i < len(text); {
		rest := text[i:]
		switch {
		case rest[0] == '`':
			if end := strings.IndexByte(rest[1:], '`'); end >= 0 {
				b.WriteString("<code>" + html.EscapeString(rest[1:1+end]) + "</code>")
				i += end + 2
				continue
			}
		case strings.HasPrefix(rest, "**"), strings.HasPrefix(rest, "~~"):
			marker := rest[:2]
			tag := map[string]string{"**": "b", "~~": "s"}[marker]
			if end := strings.Index(rest[2:], marker); end > 0 {
				b.WriteString("<" + tag + ">" + telegramInline(rest[2:2+end]) + "</" + tag + ">")
				i += end + 4
				continue
			}
		case rest[0] == '*' && len(rest) > 1 && rest[1] != ' ':
			if end := strings.IndexByte(rest[1:], '*'); end > 0 && rest[end] != ' ' {
				b.WriteString("<i>" + telegramInline(rest[1:1+end]) + "</i>")
				i += end + 2
				continue
			}
		case rest[0] == '[':
			if label, url, n, ok := telegramLink(rest); ok {
				b.WriteString(`<a href="` + html.EscapeString(url) + `">` + telegramInline(label) + "</a>")
				i += n
				continue
			}
		}
		b.WriteString(html.EscapeString(rest[:1]))
		i++
	}
	return b.String()
}

func telegramLink(text string) (label, url string, n int, ok bool) {
	closeLabel := strings.Index(text, "](")
	if closeLabel <= 1 {
		return "", "", 0, false
	}
	closeURL := strings.IndexByte(text[closeLabel+2:], ')')
	if closeURL < 0 {
		return "", "", 0, false
	}
	label, url = text[1:closeLabel], text[closeLabel+2:closeLabel+2+closeURL]
	if strings.ContainsAny(label, "[]") || !telegramLinkURL.MatchString(url) {
		return "", "", 0, false
	}
	return label, url, closeLabel + 3 + closeURL, true
}
