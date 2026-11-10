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

var wikiMentionChipPattern = regexp.MustCompile(`<span data-type="mention" data-kind="wiki" data-id="([0-9a-f-]{36})"[^>]*></span>`)

// StripHTMLKeepingWikiReferences is StripHTML for readers that cannot resolve
// a chip (the assistant, coding agents): a wiki page reference, which is an
// empty element, becomes the token @[wiki page](wiki:<page id>) instead of
// vanishing. The token carries no title; the reader looks the page up with
// its own access.
func StripHTMLKeepingWikiReferences(sanitized string) string {
	return StripHTML(wikiMentionChipPattern.ReplaceAllString(sanitized, "@[wiki page](wiki:$1)"))
}

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
// mention without a valid page ID is removed, and the mark is dropped from
// any element that is not a span. Only a mention closed by its
// own </span> loses its content: the sanitizer does not balance tags, so an
// unclosed one becomes the chip alone and whatever follows it is kept.
func canonicalWikiMentions(sanitized string) string {
	if !strings.Contains(sanitized, wikiMentionKind) {
		return sanitized
	}
	type piece struct {
		kind  html.TokenType
		token html.Token
		raw   string
	}
	pieces := []piece{}
	tokens := html.NewTokenizer(strings.NewReader(sanitized))
	for {
		kind := tokens.Next()
		if kind == html.ErrorToken {
			break
		}
		raw := string(tokens.Raw())
		pieces = append(pieces, piece{kind: kind, token: tokens.Token(), raw: raw})
	}
	// mentionEnd returns the index of the </span> closing the mention opened
	// at start, or -1 when an enclosing element or the content ends first.
	mentionEnd := func(start int) int {
		open := 0 // elements opened inside the mention
		for i := start + 1; i < len(pieces); i++ {
			switch pieces[i].kind {
			case html.StartTagToken:
				if !voidElements[pieces[i].token.Data] {
					open++
				}
			case html.EndTagToken:
				if open == 0 {
					if pieces[i].token.Data == "span" {
						return i
					}
					return -1
				}
				open--
			}
		}
		return -1
	}
	var out strings.Builder
	for i := 0; i < len(pieces); i++ {
		p := pieces[i]
		opens := p.kind == html.StartTagToken || p.kind == html.SelfClosingTagToken
		if !opens || tokenAttr(p.token, "data-kind") != "wiki" {
			out.WriteString(p.raw)
			continue
		}
		if p.token.Data != "span" {
			// Only a span is a reference: any other element keeps its content
			// and loses the mark, so nothing reading the stored HTML takes it
			// for one.
			kept := p.token
			kept.Attr = nil
			for _, attr := range p.token.Attr {
				if attr.Key != "data-kind" {
					kept.Attr = append(kept.Attr, attr)
				}
			}
			out.WriteString(kept.String())
			continue
		}
		if id, err := uuid.Parse(tokenAttr(p.token, "data-id")); err == nil && id != uuid.Nil {
			out.WriteString(WikiMentionHTML(id))
		}
		if p.kind == html.StartTagToken {
			if end := mentionEnd(i); end >= 0 {
				i = end
			}
		}
	}
	return out.String()
}

// voidElements never have an end tag, so they open nothing.
var voidElements = map[string]bool{"area": true, "base": true, "br": true, "col": true, "embed": true, "hr": true, "img": true, "input": true, "link": true, "meta": true, "source": true, "track": true, "wbr": true}

func tokenAttr(token html.Token, name string) string {
	for _, attr := range token.Attr {
		if attr.Key == name {
			return attr.Val
		}
	}
	return ""
}
