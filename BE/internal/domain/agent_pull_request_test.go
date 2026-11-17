package domain

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultPullRequestTemplateHasTheFiveSections(t *testing.T) {
	require.Equal(t, "## Description\n{{description}}\n\n## Changes\n{{changes}}\n\n## Verification\n{{verification}}\n\n## Screenshots\n{{screenshots}}\n\n## Notes\n{{notes}}\n", DefaultPullRequestTemplate)
	require.NoError(t, ValidatePullRequestTemplate(DefaultPullRequestTemplate))
}

func TestValidatePullRequestTemplate(t *testing.T) {
	for _, template := range []string{
		"{{issue}}\n\n{{description}}",
		"## What\n{{description}}\n\n## Tests\n{{verification}}\n\nThanks {not a placeholder} {{ also not }}",
		strings.Repeat("界", PullRequestTemplateMaxRunes),
	} {
		require.NoError(t, ValidatePullRequestTemplate(template))
	}
	for name, template := range map[string]string{
		"too long":            strings.Repeat("x", PullRequestTemplateMaxRunes+1),
		"unknown placeholder": "## Summary\n{{summary}}",
		"NUL":                 "{{description}}\x00",
		"invalid UTF-8":       "{{description}}" + string([]byte{0xff}),
		"blank":               "  \n ",
	} {
		require.Error(t, ValidatePullRequestTemplate(template), name)
	}
}

func TestParseAgentPullRequestFile(t *testing.T) {
	parsed := ParseAgentPullRequestFile([]byte(`{
		"description": "  The toggle ignored the system theme. It now follows prefers-color-scheme.  ",
		"changes": ["Read the system theme on load", "  ", "Update live when it changes"],
		"verification": "- npm run test:unit: passed\n* npm run check: passed\n\n",
		"notes": "",
		"screenshots": [{"file": "settings-dark.png", "caption": "Settings, dark"}, "settings-light.webp", {"file": "settings-dark.png"}],
		"title": "ignored"
	}`), false)
	require.Empty(t, parsed.Error)
	require.Equal(t, "The toggle ignored the system theme. It now follows prefers-color-scheme.", parsed.Description)
	require.Equal(t, []string{"Read the system theme on load", "Update live when it changes"}, parsed.Changes)
	require.Equal(t, []string{"npm run test:unit: passed", "npm run check: passed"}, parsed.Verification, "a string is read as one item per line")
	require.Empty(t, parsed.Notes)
	require.Equal(t, []AgentScreenshotRef{{File: "settings-dark.png", Caption: "Settings, dark"}, {File: "settings-light.webp"}}, parsed.Screenshots, "a file is listed once")

	// Long values are cut, not rejected: a description that runs over still
	// describes the change.
	long := ParseAgentPullRequestFile([]byte(`{"description":"`+strings.Repeat("a", 3000)+`","changes":[`+strings.TrimSuffix(strings.Repeat(`"x",`, 40), ",")+`]}`), false)
	require.Empty(t, long.Error)
	require.LessOrEqual(t, len([]rune(long.Description)), AgentPullRequestDescriptionMaxRunes)
	require.True(t, strings.HasSuffix(long.Description, "…"))
	require.Len(t, long.Changes, AgentPullRequestMaxItems)

	for name, data := range map[string]string{
		"not JSON":           `description: fixed`,
		"array root":         `[]`,
		"number description": `{"description": 3}`,
		"object changes":     `{"changes": {"a": "b"}}`,
		"number in list":     `{"verification": ["ok", 3]}`,
		"bad screenshot":     `{"screenshots": [{"file": "../../etc/passwd.png"}]}`,
		"nested screenshot":  `{"screenshots": ["shots/a.png"]}`,
		"svg screenshot":     `{"screenshots": ["diagram.svg"]}`,
		"trailing data":      `{"description": "a"} {"description": "b"}`,
		"NUL":                `{"notes": "a\u0000b"}`,
	} {
		rejected := ParseAgentPullRequestFile([]byte(data), false)
		require.NotEmpty(t, rejected.Error, name)
		require.Empty(t, rejected.Description, name)
	}
	require.NotEmpty(t, ParseAgentPullRequestFile([]byte(`{}`), true).Error, "an oversize file is rejected")
	require.True(t, ParseAgentPullRequestFile([]byte(`{"description":" "}`), false).Empty())
}

func TestScreenshotContentType(t *testing.T) {
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 32)...)
	jpeg := append([]byte{0xff, 0xd8, 0xff, 0xe0}, make([]byte, 32)...)
	webp := append([]byte("RIFF\x00\x00\x00\x00WEBPVP8 "), make([]byte, 32)...)
	for name, data := range map[string][]byte{"a.png": png, "b.jpg": jpeg, "c.JPEG": jpeg, "d.webp": webp} {
		_, err := ScreenshotContentType(name, data)
		require.NoError(t, err, name)
	}
	contentType, _ := ScreenshotContentType("a.png", png)
	require.Equal(t, "image/png", contentType)
	for name, data := range map[string][]byte{
		"html.png":   []byte("<html><script>alert(1)</script></html>"),
		"png.jpg":    png,
		"empty.png":  {},
		"huge.png":   append(append([]byte{}, png...), make([]byte, AgentScreenshotMaxBytes)...),
		"riff.webp":  []byte("RIFF\x00\x00\x00\x00AVI LIST"),
		"gif.gif":    []byte("GIF89a......"),
		"short.webp": []byte("RIFF"),
	} {
		_, err := ScreenshotContentType(name, data)
		require.Error(t, err, name)
	}
}
