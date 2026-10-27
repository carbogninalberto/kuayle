package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTelegramHTMLRendersMarkdownSafely(t *testing.T) {
	for name, test := range map[string]struct{ markdown, html string }{
		"escapes html":             {`<script>alert("x")</script> & co`, `&lt;script&gt;alert(&#34;x&#34;)&lt;/script&gt; &amp; co`},
		"bold and italic":          {"**Done** and *mostly* ~~gone~~", "<b>Done</b> and <i>mostly</i> <s>gone</s>"},
		"inline code":              {"Run `npm test` <now>", "Run <code>npm test</code> &lt;now&gt;"},
		"code is literal":          {"`**not bold**`", "<code>**not bold**</code>"},
		"link":                     {"See [PR #12](https://github.com/acme/app/pull/12)", `See <a href="https://github.com/acme/app/pull/12">PR #12</a>`},
		"unsafe link":              {"[x](javascript:alert(1))", "[x](javascript:alert(1))"},
		"heading":                  {"## Plan", "<b>Plan</b>"},
		"list":                     {"- one\n* two\n1. three", "• one\n• two\n1. three"},
		"quote":                    {"> careful\n> really", "<blockquote>careful\nreally</blockquote>"},
		"fence":                    {"```go\nfmt.Println(\"<hi>\")\n```", "<pre><code>fmt.Println(&#34;&lt;hi&gt;&#34;)</code></pre>"},
		"unclosed while streaming": {"**Bold so far and `code", "**Bold so far and `code"},
		"unclosed fence":           {"```\npartial", "<pre><code>partial</code></pre>"},
		"snake_case stays":         {"use snake_case_names", "use snake_case_names"},
		"spaced asterisks":         {"2 * 3 * 4", "2 * 3 * 4"},
	} {
		require.Equal(t, test.html, telegramHTML(test.markdown), name)
	}
}
