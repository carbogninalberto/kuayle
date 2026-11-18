package domain

import (
	"testing"

	"github.com/gabriel-vasile/mimetype"
)

// Detection reports text with a charset parameter; plain text, CSV and
// Markdown files must still be accepted by their extension.
func TestAllowedUploadTypeAcceptsDetectedText(t *testing.T) {
	detected := mimetype.Detect([]byte("plain notes\n")).String()
	for ext, want := range map[string]string{".txt": "text/plain", ".log": "text/plain", ".csv": "text/csv", ".md": "text/markdown"} {
		got, ok := AllowedUploadType(detected, ext)
		if !ok || got != want {
			t.Errorf("AllowedUploadType(%q, %q) = %q, %v; want %q", detected, ext, got, ok, want)
		}
	}
	if _, ok := AllowedUploadType(detected, ".pdf"); ok {
		t.Error("text content must not pass as a PDF")
	}
	if _, ok := AllowedUploadType("text/html; charset=utf-8", ".html"); ok {
		t.Error("HTML stays rejected")
	}
}
