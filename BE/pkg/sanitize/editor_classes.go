package sanitize

import (
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

// The class names stored rich text may carry: exactly what the editor and the
// app's own renderers emit. Anything else is removed, because a utility class
// on stored content can restyle the page around it (for example a link
// stretched invisibly over the whole viewport).
//
// The UI sanitizer keeps the same list in UI/src/lib/security/editor-classes.ts.
// testdata/editor_html_contract.json is read by both test suites; change all
// three together.
var editorClassNames = []string{
	"editor-attachment",              // file attachment link
	"mention",                        // @user and issue mentions
	"mention-issue",                  // issue mentions
	"mention-wiki",                   // wiki page references
	"registry-ref",                   // pull request reference in the wiki registry
	"text-[var(--app-accent-light)]", // editor links
	"underline",                      // editor links
}

// Code blocks carry "language-<name>" so they can be highlighted.
var editorClassPrefixes = []string{"language-"}

var editorClassSuffixPattern = regexp.MustCompile(`^[A-Za-z0-9_+.#-]{1,40}$`)

var editorClassSet = func() map[string]struct{} {
	set := make(map[string]struct{}, len(editorClassNames))
	for _, name := range editorClassNames {
		set[name] = struct{}{}
	}
	return set
}()

// EditorClassNames returns the exact class names allowed in stored rich text.
func EditorClassNames() []string {
	return append([]string(nil), editorClassNames...)
}

// EditorClassPrefixes returns the allowed class prefixes; what follows a prefix
// is limited to letters, digits and "_+.#-" (at most 40 characters).
func EditorClassPrefixes() []string {
	return append([]string(nil), editorClassPrefixes...)
}

func isEditorClass(name string) bool {
	if _, ok := editorClassSet[name]; ok {
		return true
	}
	for _, prefix := range editorClassPrefixes {
		if strings.HasPrefix(name, prefix) && editorClassSuffixPattern.MatchString(name[len(prefix):]) {
			return true
		}
	}
	return false
}

func isHTMLSpace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\f' || r == '\r'
}

// FilterEditorClasses keeps the allowed names of a class attribute value, in
// order and separated by single spaces. It returns "" when none is allowed.
func FilterEditorClasses(value string) string {
	names := strings.FieldsFunc(value, isHTMLSpace)
	kept := names[:0]
	for _, name := range names {
		if isEditorClass(name) {
			kept = append(kept, name)
		}
	}
	return strings.Join(kept, " ")
}

// restrictClasses rewrites the class attributes of already sanitized HTML so
// only editor classes remain. Tags without a class attribute, and class
// attributes that are already clean, are copied through byte for byte.
func restrictClasses(sanitized string) string {
	if !strings.Contains(sanitized, "class") {
		return sanitized
	}

	var out strings.Builder
	out.Grow(len(sanitized))
	tokenizer := html.NewTokenizer(strings.NewReader(sanitized))
	for {
		kind := tokenizer.Next()
		if kind == html.ErrorToken {
			// End of input; the tokenizer has already returned everything before it.
			break
		}
		raw := string(tokenizer.Raw())
		if kind != html.StartTagToken && kind != html.SelfClosingTagToken {
			out.WriteString(raw)
			continue
		}

		token := tokenizer.Token()
		changed := false
		attrs := token.Attr[:0]
		for _, attr := range token.Attr {
			if attr.Namespace == "" && attr.Key == "class" {
				kept := FilterEditorClasses(attr.Val)
				if kept != attr.Val {
					changed = true
					if kept == "" {
						continue
					}
					attr.Val = kept
				}
			}
			attrs = append(attrs, attr)
		}
		if !changed {
			out.WriteString(raw)
			continue
		}
		token.Attr = attrs
		out.WriteString(token.String())
	}
	return out.String()
}
