package sanitize

import (
	"strings"
	"testing"
)

func TestPlainTextPreservesIssueTitleSpecialCharacters(t *testing.T) {
	input := `fix/issues: use <Select> & "quoted" names`

	got := PlainText(input)

	if got != input {
		t.Fatalf("PlainText() = %q, want %q", got, input)
	}
}

func TestPlainTextDecodesEntitiesInIssueTitle(t *testing.T) {
	input := `fix/issues: use &lt;Select&gt; &amp; "quoted" names`
	want := `fix/issues: use <Select> & "quoted" names`

	got := PlainText(input)

	if got != want {
		t.Fatalf("PlainText() = %q, want %q", got, want)
	}
}

func TestSanitizeEditorContentConvertsMarkdown(t *testing.T) {
	input := strings.Join([]string{
		`# Use <Select> & friends`,
		``,
		`- [x] Preserve **bold** text`,
		`- [ ] Render ` + "`code <tag>`" + ` safely`,
		``,
		`See [docs](https://example.com/docs?q=one&ok=true).`,
	}, "\n")

	got := SanitizeEditorContent(input)

	assertContains(t, got, `<h1>Use &lt;Select&gt; &amp; friends</h1>`)
	assertContains(t, got, `data-type="taskList"`)
	assertContains(t, got, `data-checked="true"`)
	assertContains(t, got, `<strong>bold</strong>`)
	assertContains(t, got, `<code>code &lt;tag&gt;</code>`)
	assertContains(t, got, `href="https://example.com/docs?q=one&amp;ok=true"`)
}

func TestSanitizeEditorContentKeepsSafeEditorHTML(t *testing.T) {
	input := `<p>See <a href="https://example.com?a=1&b=2">docs</a> <script>alert(1)</script></p>`

	got := SanitizeEditorContent(input)

	assertContains(t, got, `<a href="https://example.com?a=1&amp;b=2"`)
	assertNotContains(t, got, `<script`)
	assertNotContains(t, got, `alert(1)`)
}

func assertContains(t *testing.T, value, substring string) {
	t.Helper()
	if !strings.Contains(value, substring) {
		t.Fatalf("expected %q to contain %q", value, substring)
	}
}

func assertNotContains(t *testing.T, value, substring string) {
	t.Helper()
	if strings.Contains(value, substring) {
		t.Fatalf("expected %q not to contain %q", value, substring)
	}
}
