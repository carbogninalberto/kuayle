package sanitize

import (
	"encoding/json"
	"os"
	"sort"
	"testing"
)

// editorHTMLContract is the fixture shared with the UI sanitizer's unit and
// browser tests, so the two allowlists cannot drift apart unnoticed.
type editorHTMLContract struct {
	AllowedClasses       []string `json:"allowed_classes"`
	AllowedClassPrefixes []string `json:"allowed_class_prefixes"`
	ClassCases           []struct {
		Input  string `json:"input"`
		Output string `json:"output"`
	} `json:"class_cases"`
	Documents []struct {
		Name string `json:"name"`
		HTML string `json:"html"`
	} `json:"documents"`
	Stripped []struct {
		Name string `json:"name"`
		HTML string `json:"html"`
		Want string `json:"want"`
	} `json:"stripped"`
}

func loadEditorHTMLContract(t *testing.T) editorHTMLContract {
	t.Helper()
	raw, err := os.ReadFile("testdata/editor_html_contract.json")
	if err != nil {
		t.Fatalf("read contract: %v", err)
	}
	var contract editorHTMLContract
	if err := json.Unmarshal(raw, &contract); err != nil {
		t.Fatalf("parse contract: %v", err)
	}
	if len(contract.AllowedClasses) == 0 || len(contract.ClassCases) == 0 || len(contract.Documents) == 0 || len(contract.Stripped) == 0 {
		t.Fatal("contract is missing a section")
	}
	return contract
}

func TestEditorClassAllowlistMatchesSharedContract(t *testing.T) {
	contract := loadEditorHTMLContract(t)

	names := EditorClassNames()
	sort.Strings(names)
	want := append([]string(nil), contract.AllowedClasses...)
	sort.Strings(want)
	if !equalStrings(names, want) {
		t.Fatalf("allowed classes = %q, contract says %q", names, want)
	}
	if prefixes := EditorClassPrefixes(); !equalStrings(prefixes, contract.AllowedClassPrefixes) {
		t.Fatalf("allowed class prefixes = %q, contract says %q", prefixes, contract.AllowedClassPrefixes)
	}
}

func TestFilterEditorClassesFollowsSharedContract(t *testing.T) {
	for _, c := range loadEditorHTMLContract(t).ClassCases {
		if got := FilterEditorClasses(c.Input); got != c.Output {
			t.Errorf("FilterEditorClasses(%q) = %q, want %q", c.Input, got, c.Output)
		}
	}
}

func TestSanitizersKeepStoredEditorDocumentsUnchanged(t *testing.T) {
	for _, doc := range loadEditorHTMLContract(t).Documents {
		if got := SanitizeHTML(doc.HTML); got != doc.HTML {
			t.Errorf("%s: SanitizeHTML changed the document\n got %s\nwant %s", doc.Name, got, doc.HTML)
		}
		if got := SanitizeEditorContent(doc.HTML); got != doc.HTML {
			t.Errorf("%s: SanitizeEditorContent changed the document\n got %s\nwant %s", doc.Name, got, doc.HTML)
		}
	}
}

func TestSanitizersStripClassesTheEditorNeverEmits(t *testing.T) {
	for _, doc := range loadEditorHTMLContract(t).Stripped {
		if got := SanitizeHTML(doc.HTML); got != doc.Want {
			t.Errorf("%s: SanitizeHTML\n got %s\nwant %s", doc.Name, got, doc.Want)
		}
		if got := SanitizeEditorContent(doc.HTML); got != doc.Want {
			t.Errorf("%s: SanitizeEditorContent\n got %s\nwant %s", doc.Name, got, doc.Want)
		}
	}
}

func TestSanitizeHTMLLeavesTextThatLooksLikeAClassAttribute(t *testing.T) {
	input := `<p>write class=&#34;fixed inset-0&#34; and &lt;a class=&#34;fixed&#34;&gt; in prose</p><pre><code class="language-html">&lt;a class=&#34;fixed&#34;&gt;</code></pre>`

	if got := SanitizeHTML(input); got != input {
		t.Fatalf("SanitizeHTML changed text content\n got %s\nwant %s", got, input)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
