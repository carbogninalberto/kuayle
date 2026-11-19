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

func TestSanitizeEditorContentConvertsMarkdownTable(t *testing.T) {
	input := `Release checklist

Step | Result
:--- | ---:
**Build** | Pass \| retry
Test | ` + "`<table> \\| go test`" + `

Ready for review.`
	want := `<p>Release checklist</p><table><thead><tr><th align="left">Step</th><th align="right">Result</th></tr></thead>` +
		`<tbody><tr><td align="left"><strong>Build</strong></td><td align="right">Pass | retry</td></tr>` +
		`<tr><td align="left">Test</td><td align="right"><code>&lt;table&gt; | go test</code></td></tr></tbody></table><p>Ready for review.</p>`
	if got := SanitizeEditorContent(input); got != want {
		t.Fatalf("SanitizeEditorContent() = %q, want %q", got, want)
	}
}

func TestSanitizeEditorContentConvertsMarkdownTableWithInlineHTML(t *testing.T) {
	input := "| Step | Result |\n| --- | --- |\n| **Build** | Pass<br>Retry <strong style=\"color: red\">once</strong> |"
	want := `<table><thead><tr><th>Step</th><th>Result</th></tr></thead><tbody>` +
		`<tr><td><strong>Build</strong></td><td>Pass<br>Retry <strong>once</strong></td></tr></tbody></table>`
	if got := SanitizeEditorContent(input); got != want {
		t.Fatalf("SanitizeEditorContent() = %q, want %q", got, want)
	}
	assertContains(t, EditorMarkdown(input), "Pass&lt;br&gt;Retry")
}

func TestSanitizeEditorContentRequiresMarkdownTableDelimiter(t *testing.T) {
	for _, input := range []string{
		"| Step | Result |\n| Build | Pass |",
		"| Step | Result |\n| ---- |\n| Build | Pass |",
		"```md\n| Step | Result |\n| ---- | ------ |\n| Build | Pass |\n```",
	} {
		got := SanitizeEditorContent(input)
		assertNotContains(t, got, "<table>")
		assertContains(t, got, "| Step | Result |")
	}
}

func TestSanitizeEditorContentKeepsHTMLTableAfterProse(t *testing.T) {
	for _, input := range []string{
		"Summary:\n\n<table><tr><td>Build</td><td>Passed</td></tr></table>",
		"Summary:\n\n<table><tr><td>\n| Step | Result |\n| --- | --- |\n| Build | Passed |\n</td></tr></table>",
	} {
		if got := SanitizeEditorContent(input); got != input {
			t.Errorf("SanitizeEditorContent() = %q, want %q", got, input)
		}
	}
}

func TestSanitizeEditorContentKeepsHTMLTableFragments(t *testing.T) {
	for _, input := range []string{
		"<tbody><tr><td>Build</td></tr></tbody>",
		"<tr><th>Step</th><td>Build</td></tr>",
		"<td>Build</td>",
	} {
		if got := SanitizeEditorContent(input); got != input {
			t.Errorf("SanitizeEditorContent() = %q, want %q", got, input)
		}
	}
}

func TestSanitizeEditorContentKeepsTableExamplesAsCode(t *testing.T) {
	const table = "<table><tr><td>Build</td></tr></table>"
	const escaped = "&lt;table&gt;&lt;tr&gt;&lt;td&gt;Build&lt;/td&gt;&lt;/tr&gt;&lt;/table&gt;"
	for input, want := range map[string]string{
		"Example: " + "`" + table + "`": "<p>Example: <code>" + escaped + "</code></p>",
		"```html\n" + table + "\n```":   "<pre><code>" + escaped + "</code></pre>",
	} {
		if got := SanitizeEditorContent(input); got != want {
			t.Errorf("SanitizeEditorContent() = %q, want %q", got, want)
		}
	}
}

func TestSanitizeEditorContentKeepsSafeHTMLTable(t *testing.T) {
	input := `<table class="fixed" style="position:fixed"><thead><tr><th colspan="2" onclick="run()">Release</th></tr></thead>` +
		`<tbody><tr><td rowspan="2">Build</td><td align="right">Passed</td></tr><tr><td>Reviewed</td></tr></tbody>` +
		`<tfoot><tr><td colspan="-1" rowspan="0" align="bad">Total</td><td></td></tr></tfoot></table>`
	want := `<table><thead><tr><th colspan="2">Release</th></tr></thead>` +
		`<tbody><tr><td rowspan="2">Build</td><td align="right">Passed</td></tr><tr><td>Reviewed</td></tr></tbody>` +
		`<tfoot><tr><td>Total</td><td></td></tr></tfoot></table>`
	for _, clean := range []func(string) string{SanitizeHTML, SanitizeEditorContent} {
		if got := clean(input); got != want {
			t.Errorf("sanitized table = %q, want %q", got, want)
		}
		if got := clean(want); got != want {
			t.Errorf("sanitizing a stored table changed it to %q", got)
		}
	}
}

func TestSanitizeHTMLKeepsTiptapTableOnResave(t *testing.T) {
	// Captured from Tiptap's getHTML() after editing the Mobile cell.
	input := `<p>Review summary.</p><table style="min-width: 50px;"><colgroup>` +
		`<col style="min-width: 25px;"><col style="min-width: 25px;"></colgroup><tbody>` +
		`<tr><th colspan="2"><p>Review checklist</p></th></tr>` +
		`<tr><td rowspan="2"><p>UI</p></td><td><p>Desktop</p></td></tr>` +
		`<tr><td align="center" style="text-align: center;"><p>Mobile checked</p></td></tr></tbody></table><p></p>`
	want := `<p>Review summary.</p><table><tbody><tr><th colspan="2"><p>Review checklist</p></th></tr>` +
		`<tr><td rowspan="2"><p>UI</p></td><td><p>Desktop</p></td></tr>` +
		`<tr><td align="center"><p>Mobile checked</p></td></tr></tbody></table><p></p>`
	got := SanitizeHTML(input)
	if got != want {
		t.Fatalf("SanitizeHTML() = %q, want %q", got, want)
	}
	if resaved := SanitizeEditorContent(got); resaved != want {
		t.Fatalf("SanitizeEditorContent() on resave = %q, want %q", resaved, want)
	}
}

func TestSanitizeEditorContentKeepsProtectedAssetImages(t *testing.T) {
	assetID := "123e4567-e89b-12d3-a456-426614174000"
	input := `<p><img src="/api/workspaces/acme/assets/` + assetID + `" alt="Screenshot"></p>`

	got := SanitizeEditorContent(input)

	assertContains(t, got, `src="/api/workspaces/acme/assets/`+assetID+`"`)
	assertContains(t, got, `alt="Screenshot"`)
}

func TestSanitizeEditorContentRejectsUnsafeImageURL(t *testing.T) {
	input := `<p><img src="javascript:alert(1)" alt="Bad"></p>`

	got := SanitizeEditorContent(input)

	assertNotContains(t, got, `javascript:`)
}

func TestSanitizeEditorContentKeepsAttachmentMetadata(t *testing.T) {
	assetID := "123e4567-e89b-12d3-a456-426614174000"
	input := `<p><a class="editor-attachment" data-type="attachment" data-filename="requirements.pdf" data-size="2048" href="/api/workspaces/acme/assets/` + assetID + `?download=1" download="requirements.pdf" target="_blank">requirements.pdf (2 KB)</a></p>`

	got := SanitizeEditorContent(input)

	assertContains(t, got, `data-type="attachment"`)
	assertContains(t, got, `data-filename="requirements.pdf"`)
	assertContains(t, got, `data-size="2048"`)
	assertContains(t, got, `download="requirements.pdf"`)
	assertContains(t, got, `/api/workspaces/acme/assets/`+assetID+`?download=1`)
}

// Imported text from another tracker is always Markdown: a literal tag in it
// is text to show, never markup that switches the whole body to HTML.
func TestEditorMarkdownTreatsTagsAsText(t *testing.T) {
	got := EditorMarkdown("Use the <div> wrapper and **bold** text\n\n<script>alert(1)</script>")

	assertContains(t, got, "<strong>bold</strong>")
	assertContains(t, got, "&lt;div&gt;")
	assertContains(t, got, "&lt;script&gt;")
	assertNotContains(t, got, "<script>")
	assertNotContains(t, got, "<div>")
	if EditorMarkdown("   ") != "" {
		t.Fatal("blank input must stay empty")
	}
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

// A wiki page reference keeps only the page ID: whatever title or text a
// client sends with it is dropped, so stored content never carries the title
// of a page its readers may not see.
func TestSanitizeStoresWikiMentionsWithoutTitles(t *testing.T) {
	const id = "0b0e7a52-6a0f-4a56-9d3e-0d2b3f6a9c11"
	canonical := `<span data-type="mention" data-kind="wiki" data-id="` + id + `" class="mention mention-wiki"></span>`

	for name, input := range map[string]string{
		"editor chip":     `<p>See <span class="mention mention-wiki" data-type="mention" data-id="` + id + `" data-label="Secret plan" data-kind="wiki">Secret plan</span> first</p>`,
		"nested markup":   `<p>See <span data-kind="wiki" data-id="` + id + `"><strong>Secret</strong> <span>plan</span></span> first</p>`,
		"uppercase id":    `<p>See <span data-type="mention" data-kind="wiki" data-id="0B0E7A52-6A0F-4A56-9D3E-0D2B3F6A9C11">Secret plan</span> first</p>`,
		"already cleaned": `<p>See ` + canonical + ` first</p>`,
	} {
		got := SanitizeEditorContent(input)
		if got != `<p>See `+canonical+` first</p>` {
			t.Errorf("%s: got %q", name, got)
		}
	}

	comment := SanitizeHTML(`<p><span data-type="mention" data-kind="wiki" data-id="` + id + `" data-label="Secret plan">Secret plan</span></p>`)
	if strings.Contains(comment, "Secret") || !strings.Contains(comment, canonical) {
		t.Errorf("comment body kept the title: %q", comment)
	}

	invalid := SanitizeHTML(`<p>a <span data-type="mention" data-kind="wiki" data-id="not-a-page">Secret plan</span> b</p>`)
	if invalid != `<p>a  b</p>` {
		t.Errorf("a reference without a page ID should be dropped, got %q", invalid)
	}

	// Other mentions are untouched.
	issue := `<p><span data-type="mention" data-id="` + id + `" data-label="KUA-1 Title" data-kind="issue" class="mention mention-issue">KUA-1 Title</span></p>`
	if got := SanitizeHTML(issue); got != issue {
		t.Errorf("issue mention changed: %q", got)
	}
}

func TestWikiMentionIDs(t *testing.T) {
	const first, second = "0b0e7a52-6a0f-4a56-9d3e-0d2b3f6a9c11", "7c2b1d4e-9f3a-4b6c-8d5e-1a2b3c4d5e6f"
	content := SanitizeHTML(`<p><span data-kind="wiki" data-id="` + first + `">A</span> <span data-kind="wiki" data-id="` + second + `">B</span> <span data-kind="wiki" data-id="` + first + `">A</span></p>`)
	got := WikiMentionIDs(content)
	if len(got) != 2 || got[0].String() != first || got[1].String() != second {
		t.Fatalf("WikiMentionIDs() = %v", got)
	}
	if ids := WikiMentionIDs(`<p>none</p>`); len(ids) != 0 {
		t.Fatalf("expected no IDs, got %v", ids)
	}
}

// Only a span is a reference. The mark on any other element would be read as
// one by whatever looks for references in stored HTML, so it is not stored.
func TestSanitizeDropsWikiMentionMarkOnOtherElements(t *testing.T) {
	id := "0b0e7a52-6a0f-4a56-9d3e-0d2b3f6a9c11"
	for _, input := range []string{
		`<p data-kind="wiki" data-id="` + id + `">x</p>`,
		`<p data-label="&lt;span" data-kind="wiki" data-id="` + id + `">x<span>y</span></p>`,
		`<a href="https://example.com" data-kind="wiki" data-id="` + id + `">x</a>`,
	} {
		got := SanitizeHTML(input)
		if strings.Contains(got, wikiMentionKind) {
			t.Errorf("SanitizeHTML(%q) kept the reference mark: %q", input, got)
		}
		if !strings.Contains(got, "x") {
			t.Errorf("SanitizeHTML(%q) lost the content: %q", input, got)
		}
		if len(WikiMentionIDs(got)) != 0 {
			t.Errorf("SanitizeHTML(%q) left a reference: %q", input, got)
		}
	}
}

// The sanitizer does not balance tags, so a mention whose </span> is missing
// must not swallow the rest of the content.
func TestSanitizeKeepsContentAfterUnclosedWikiMention(t *testing.T) {
	id := "0b0e7a52-6a0f-4a56-9d3e-0d2b3f6a9c11"
	chip := `<span data-type="mention" data-kind="wiki" data-id="` + id + `" class="mention mention-wiki"></span>`
	open := `<span data-kind="wiki" data-id="` + id + `">`
	for name, tc := range map[string]struct{ in, want string }{
		"closed by the block":       {`<p>See ` + open + `x</p><p>after</p>`, `<p>See ` + chip + `x</p><p>after</p>`},
		"never closed":              {`<p>See ` + open + `x`, `<p>See ` + chip + `x`},
		"later span is not its end": {`<p>See ` + open + `x</p><p>after <span>y</span></p>`, `<p>See ` + chip + `x</p><p>after <span>y</span></p>`},
		"line break inside":         {`<p>See ` + open + `a<br>b</span> after</p>`, `<p>See ` + chip + ` after</p>`},
	} {
		if got := SanitizeHTML(tc.in); got != tc.want {
			t.Errorf("%s: SanitizeHTML() = %q, want %q", name, got, tc.want)
		}
	}
}

// Readers without chips (the assistant, coding agents) still learn that a
// page is referenced, and which one.
func TestStripHTMLKeepingWikiReferencesNamesThePage(t *testing.T) {
	id := "0b0e7a52-6a0f-4a56-9d3e-0d2b3f6a9c11"
	content := SanitizeHTML(`<p>Follow <span data-kind="wiki" data-id="` + id + `">Secret plan</span> <strong>first</strong></p>`)
	if got, want := StripHTMLKeepingWikiReferences(content), "Follow @[wiki page](wiki:"+id+") first"; got != want {
		t.Fatalf("StripHTMLKeepingWikiReferences() = %q, want %q", got, want)
	}
	if got := StripHTML(content); got != "Follow  first" {
		t.Fatalf("StripHTML() = %q", got)
	}
}
