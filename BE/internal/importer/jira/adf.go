package jira

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// ADF is Atlassian Document Format, the JSON structure used for descriptions
// and comments on Jira Cloud. Server/DC returns wiki markup as a plain string.
type adfNode struct {
	Type    string         `json:"type"`
	Text    string         `json:"text"`
	Attrs   map[string]any `json:"attrs"`
	Marks   []adfMark      `json:"marks"`
	Content []adfNode      `json:"content"`
}

type adfMark struct {
	Type  string         `json:"type"`
	Attrs map[string]any `json:"attrs"`
}

// DescriptionToMarkdown converts either an ADF document or a Server/DC wiki
// string to markdown and returns notes about node types it could not map.
func DescriptionToMarkdown(raw json.RawMessage) (string, []string) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return "", nil
	}
	if trimmed[0] == '"' {
		var text string
		if err := json.Unmarshal(trimmed, &text); err == nil {
			return wikiToMarkdown(text), nil
		}
	}
	var node adfNode
	if err := json.Unmarshal(trimmed, &node); err != nil {
		return string(trimmed), []string{"description: unrecognised format"}
	}
	b := &adfBuilder{unmapped: map[string]bool{}}
	markdown := strings.TrimSpace(b.render(node, 0))
	notes := b.notes()
	if len(notes) > 0 {
		markdown += "\n\n_Note: " + strings.Join(notes, "; ") + "_"
	}
	return markdown, notes
}

type adfBuilder struct {
	unmapped map[string]bool
}

func (b *adfBuilder) note(nodeType string) {
	if nodeType != "" {
		b.unmapped[nodeType] = true
	}
}

func (b *adfBuilder) notes() []string {
	out := make([]string, 0, len(b.unmapped))
	for nodeType := range b.unmapped {
		out = append(out, "description: unmapped node "+nodeType)
	}
	return out
}

func attrString(attrs map[string]any, key string) string {
	if value, ok := attrs[key].(string); ok {
		return value
	}
	return ""
}

func (b *adfBuilder) render(node adfNode, depth int) string {
	if depth > 64 {
		return ""
	}
	switch node.Type {
	case "doc":
		return b.blocks(node.Content, depth, "\n\n")
	case "paragraph":
		return b.inline(node.Content, depth)
	case "text":
		return b.marked(node.Text, node.Marks)
	case "heading":
		level := 1
		if value, ok := node.Attrs["level"].(float64); ok && value >= 1 && value <= 6 {
			level = int(value)
		}
		return strings.Repeat("#", level) + " " + b.inline(node.Content, depth)
	case "hardBreak":
		return "  \n"
	case "bulletList", "orderedList":
		ordered := node.Type == "orderedList"
		var out []string
		for i, item := range node.Content {
			marker := "- "
			if ordered {
				marker = fmt.Sprintf("%d. ", i+1)
			}
			out = append(out, b.listItem(item, marker, depth))
		}
		return strings.Join(out, "\n")
	case "listItem":
		return b.blocks(node.Content, depth, "\n\n")
	case "codeBlock":
		language := attrString(node.Attrs, "language")
		return "```" + language + "\n" + b.plain(node.Content) + "\n```"
	case "blockquote":
		body := b.blocks(node.Content, depth, "\n\n")
		lines := strings.Split(body, "\n")
		for i, line := range lines {
			lines[i] = "> " + line
		}
		return strings.Join(lines, "\n")
	case "rule":
		return "---"
	case "mediaSingle", "media", "mediaGroup":
		id := attrString(node.Attrs, "id")
		b.note("media")
		if id == "" {
			return "[attachment]"
		}
		return "[attachment:" + id + "]"
	case "mention":
		text := attrString(node.Attrs, "text")
		if text == "" {
			text = "@" + attrString(node.Attrs, "id")
		}
		return text
	case "emoji":
		if short := attrString(node.Attrs, "shortName"); short != "" {
			return short
		}
		return ""
	case "status":
		return "`" + attrString(node.Attrs, "text") + "`"
	case "table":
		return b.table(node.Content, depth)
	case "tableRow", "tableCell", "tableHeader":
		return b.inline(node.Content, depth)
	case "":
		if node.Text != "" {
			return node.Text
		}
		return ""
	default:
		b.note(node.Type)
		if len(node.Content) > 0 {
			return b.inline(node.Content, depth)
		}
		return node.Text
	}
}

func (b *adfBuilder) blocks(nodes []adfNode, depth int, sep string) string {
	var parts []string
	for _, node := range nodes {
		if text := b.render(node, depth+1); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, sep)
}

func (b *adfBuilder) inline(nodes []adfNode, depth int) string {
	var buf strings.Builder
	for _, node := range nodes {
		buf.WriteString(b.render(node, depth+1))
	}
	return buf.String()
}

func (b *adfBuilder) marked(text string, marks []adfMark) string {
	for _, mark := range marks {
		switch mark.Type {
		case "strong":
			text = "**" + text + "**"
		case "em":
			text = "*" + text + "*"
		case "code":
			text = "`" + text + "`"
		case "strike":
			text = "~~" + text + "~~"
		case "link":
			href := attrString(mark.Attrs, "href")
			if href != "" {
				text = "[" + text + "](" + href + ")"
			}
		case "underline", "subscript", "superscript":
			// Markdown has no direct equivalent; keep the visible text.
		default:
			b.note("mark:" + mark.Type)
		}
	}
	return text
}

func (b *adfBuilder) listItem(item adfNode, marker string, depth int) string {
	body := b.blocks(item.Content, depth+1, "\n\n")
	lines := strings.Split(body, "\n")
	if len(lines) == 0 {
		return strings.TrimRight(marker, " ")
	}
	var out []string
	for i, line := range lines {
		if i == 0 {
			out = append(out, marker+line)
		} else {
			out = append(out, "  "+line)
		}
	}
	return strings.Join(out, "\n")
}

func (b *adfBuilder) plain(nodes []adfNode) string {
	var buf strings.Builder
	for _, node := range nodes {
		if node.Text != "" {
			buf.WriteString(node.Text)
		}
		if len(node.Content) > 0 {
			buf.WriteString(b.plain(node.Content))
		}
		if node.Type == "hardBreak" {
			buf.WriteString("\n")
		}
	}
	return buf.String()
}

func (b *adfBuilder) table(rows []adfNode, depth int) string {
	if len(rows) == 0 {
		return ""
	}
	renderRow := func(row adfNode) []string {
		cells := make([]string, 0, len(row.Content))
		for _, cell := range row.Content {
			cells = append(cells, strings.ReplaceAll(b.render(cell, depth+1), "|", "\\|"))
		}
		return cells
	}
	header := renderRow(rows[0])
	lines := []string{"| " + strings.Join(header, " | ") + " |"}
	separator := make([]string, len(header))
	for i := range separator {
		separator[i] = "---"
	}
	lines = append(lines, "| "+strings.Join(separator, " | ")+" |")
	for _, row := range rows[1:] {
		lines = append(lines, "| "+strings.Join(renderRow(row), " | ")+" |")
	}
	return strings.Join(lines, "\n")
}

// wikiToMarkdown keeps Server/DC wiki markup readable with a few common
// conversions; anything else stays as plain text.
func wikiToMarkdown(text string) string {
	if text == "" {
		return ""
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		for level := 1; level <= 6; level++ {
			prefix := "h" + fmt.Sprint(level) + ". "
			if strings.HasPrefix(line, prefix) {
				lines[i] = strings.Repeat("#", level) + " " + strings.TrimPrefix(line, prefix)
				break
			}
		}
	}
	return strings.Join(lines, "\n")
}
