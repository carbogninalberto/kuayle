package sanitize

import (
	"regexp"
	"strings"
)

var markdownTableDelimiter = regexp.MustCompile(`^:?-+:?$`)

func renderMarkdownTable(lines []string) (string, int) {
	header := splitMarkdownTableRow(lines[0])
	if len(header) == 0 {
		return "", 0
	}
	delimiters := splitMarkdownTableRow(lines[1])
	if len(delimiters) != len(header) {
		return "", 0
	}
	alignment := make([]string, len(header))
	for i, delimiter := range delimiters {
		if !markdownTableDelimiter.MatchString(delimiter) {
			return "", 0
		}
		left := strings.HasPrefix(delimiter, ":")
		right := strings.HasSuffix(delimiter, ":")
		switch {
		case left && right:
			alignment[i] = ` align="center"`
		case left:
			alignment[i] = ` align="left"`
		case right:
			alignment[i] = ` align="right"`
		}
	}

	var out strings.Builder
	writeRow := func(tag string, cells []string) {
		out.WriteString("<tr>")
		for i, align := range alignment {
			out.WriteString("<" + tag + align + ">")
			if i < len(cells) {
				// An escaped pipe is literal even inside an inline code span.
				out.WriteString(renderInlineMarkdown(strings.ReplaceAll(cells[i], `\|`, `|`)))
			}
			out.WriteString("</" + tag + ">")
		}
		out.WriteString("</tr>")
	}
	out.WriteString("<table><thead>")
	writeRow("th", header)
	out.WriteString("</thead>")
	// Bound the number of cells synthesized for short rows, as in GFM parsers.
	const maxMissingCells = 65_536
	missingCells := 0
	i := 2
	for ; i < len(lines); i++ {
		cells := splitMarkdownTableRow(lines[i])
		if len(cells) == 0 {
			break
		}
		missingCells += max(0, len(header)-len(cells))
		if missingCells > maxMissingCells {
			break
		}
		if i == 2 {
			out.WriteString("<tbody>")
		}
		writeRow("td", cells)
	}
	if i > 2 {
		out.WriteString("</tbody>")
	}
	out.WriteString("</table>")
	return out.String(), i
}

// A table row has at least one unescaped pipe. The outer pipes are optional.
func splitMarkdownTableRow(line string) []string {
	line = strings.TrimSpace(line)
	var cells []string
	start := 0
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '\\':
			i++
		case '|':
			if i > 0 {
				cells = append(cells, strings.TrimSpace(line[start:i]))
			}
			start = i + 1
		}
	}
	if start == 0 {
		return nil
	}
	if start < len(line) {
		cells = append(cells, strings.TrimSpace(line[start:]))
	}
	return cells
}
