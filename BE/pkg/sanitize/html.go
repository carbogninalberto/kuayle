package sanitize

import (
	"github.com/microcosm-cc/bluemonday"
)

var policy *bluemonday.Policy

func init() {
	policy = bluemonday.NewPolicy()

	policy.AllowElements(
		"p", "br", "strong", "b", "em", "i", "s", "del", "code", "pre",
		"h1", "h2", "h3", "h4", "h5", "h6",
		"ul", "ol", "li", "blockquote", "hr",
		"span", "div", "label",
	)

	policy.AllowAttrs("class").Globally()
	policy.AllowAttrs("data-type").Globally()
	policy.AllowAttrs("data-checked").Globally()

	policy.AllowAttrs("href", "target", "rel").OnElements("a")
	policy.AllowStandardURLs()
	policy.AddTargetBlankToFullyQualifiedLinks(true)
	policy.RequireNoFollowOnLinks(true)
	policy.RequireNoReferrerOnLinks(true)

	policy.AllowAttrs("type", "checked", "disabled").OnElements("input")
}

// SanitizeHTML sanitizes HTML input, stripping dangerous tags and attributes.
func SanitizeHTML(input string) string {
	return policy.Sanitize(input)
}
