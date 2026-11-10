package sanitize

import (
	"regexp"
	"strings"

	"github.com/google/uuid"
	"golang.org/x/net/html"
)

const wikiMentionKind = `data-kind="wiki"`

// WikiMentionHTML is the only form a wiki page reference is stored in: the
// page ID and nothing else. The title is resolved when the chip is rendered,
// through the reader's own access, so stored issue, comment and wiki content
// can never leak the title of a page.
func WikiMentionHTML(pageID uuid.UUID) string {
	return `<span data-type="mention" data-kind="wiki" data-id="` + pageID.String() + `" class="mention mention-wiki"></span>`
}

var wikiMentionPattern = regexp.MustCompile(`<span data-type="mention" data-kind="wiki" data-id="([0-9a-f-]{36})"`)

// WikiMentionIDs lists the pages sanitized content references, each once, in
// document order.
func WikiMentionIDs(sanitized string) []uuid.UUID {
	ids := []uuid.UUID{}
	seen := map[uuid.UUID]bool{}
	for _, match := range wikiMentionPattern.FindAllStringSubmatch(sanitized, -1) {
		if id, err := uuid.Parse(match[1]); err == nil && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids
}

// canonicalWikiMentions rewrites every wiki mention of already sanitized HTML
// to WikiMentionHTML, dropping its label, text and any nested markup. A
// mention without a valid page ID is removed.
func canonicalWikiMentions(sanitized string) string {
	if !strings.Contains(sanitized, wikiMentionKind) {
		return sanitized
	}
	tokens := html.NewTokenizer(strings.NewReader(sanitized))
	var out strings.Builder
	depth := 0 // open spans inside the mention being dropped
	for {
		kind := tokens.Next()
		if kind == html.ErrorToken {
			return out.String()
		}
		raw := string(tokens.Raw())
		token := tokens.Token()
		span := token.Data == "span"
		if depth > 0 {
			if span && kind == html.StartTagToken {
				depth++
			} else if span && kind == html.EndTagToken {
				depth--
			}
			continue
		}
		if span && (kind == html.StartTagToken || kind == html.SelfClosingTagToken) && tokenAttr(token, "data-kind") == "wiki" {
			if id, err := uuid.Parse(tokenAttr(token, "data-id")); err == nil && id != uuid.Nil {
				out.WriteString(WikiMentionHTML(id))
			}
			if kind == html.StartTagToken {
				depth = 1
			}
			continue
		}
		out.WriteString(raw)
	}
}

func tokenAttr(token html.Token, name string) string {
	for _, attr := range token.Attr {
		if attr.Key == name {
			return attr.Val
		}
	}
	return ""
}
