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
