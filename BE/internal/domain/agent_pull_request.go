package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"strings"
	"unicode/utf8"
)

// An implement attempt describes its change for the pull request by writing
// AgentPullRequestFile at its worktree root, and may leave screenshots of
// changed screens in AgentScreenshotsDir. Kuayle reads both when it publishes
// (nothing under .kuayle/ is committed) and renders the description from the
// workspace's pull request template. Both are agent output: parsed strictly,
// bounded, and sanitized again when rendered.
const (
	AgentPullRequestFile         = ".kuayle/pull_request.json"
	AgentScreenshotsDir          = ".kuayle/screenshots"
	AgentPullRequestFileMaxBytes = 65536

	AgentPullRequestDescriptionMaxRunes = 1500
	AgentPullRequestNotesMaxRunes       = 1000
	AgentPullRequestItemMaxRunes        = 300
	AgentPullRequestMaxItems            = 15
	AgentScreenshotCaptionMaxRunes      = 120
	AgentScreenshotMaxCount             = 6
	AgentScreenshotMaxBytes             = 2 << 20
)

// PullRequestTemplateMaxRunes bounds a configured pull request template.
const PullRequestTemplateMaxRunes = 8000

// DefaultPullRequestTemplate is used when no development setting configures
// a template. A section whose placeholders all render empty is left out,
// heading included.
const DefaultPullRequestTemplate = "## Description\n{{description}}\n\n## Changes\n{{changes}}\n\n## Verification\n{{verification}}\n\n## Screenshots\n{{screenshots}}\n\n## Notes\n{{notes}}\n"

// PullRequestTemplatePlaceholders are the names a template may use as
// {{name}}.
var PullRequestTemplatePlaceholders = []string{"description", "changes", "verification", "screenshots", "notes", "issue"}

// PullRequestPlaceholderPattern matches a {{name}} placeholder.
var PullRequestPlaceholderPattern = regexp.MustCompile(`\{\{([a-z_]+)\}\}`)

// ValidatePullRequestTemplate accepts non-blank text of at most
// PullRequestTemplateMaxRunes characters whose placeholders are all known.
func ValidatePullRequestTemplate(template string) error {
	if !utf8.ValidString(template) || strings.ContainsRune(template, 0) || strings.TrimSpace(template) == "" || utf8.RuneCountInString(template) > PullRequestTemplateMaxRunes {
		return fmt.Errorf("the pull request template must be text of at most %d characters", PullRequestTemplateMaxRunes)
	}
	for _, match := range PullRequestPlaceholderPattern.FindAllStringSubmatch(template, -1) {
		known := false
		for _, name := range PullRequestTemplatePlaceholders {
			known = known || match[1] == name
		}
		if !known {
			return fmt.Errorf("the pull request template uses the unknown placeholder {{%s}}", match[1])
		}
	}
	return nil
}

// AgentScreenshotRef names one screenshot in AgentScreenshotsDir.
type AgentScreenshotRef struct {
	File    string `json:"file"`
	Caption string `json:"caption,omitempty"`
}

// AgentPullRequest is what an attempt's pull request file declared, or the
// Error that made Kuayle ignore it. Texts are trimmed and cut to their limits.
type AgentPullRequest struct {
	Description  string               `json:"description,omitempty"`
	Changes      []string             `json:"changes,omitempty"`
	Verification []string             `json:"verification,omitempty"`
	Notes        string               `json:"notes,omitempty"`
	Screenshots  []AgentScreenshotRef `json:"screenshots,omitempty"`
	Error        string               `json:"error,omitempty"`
}

// Empty reports whether the file described nothing.
func (p AgentPullRequest) Empty() bool {
	return p.Description == "" && len(p.Changes) == 0 && len(p.Verification) == 0 && p.Notes == "" && len(p.Screenshots) == 0
}

// agentScreenshotName is a file directly in AgentScreenshotsDir.
var agentScreenshotName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}\.(?i:png|jpe?g|webp)$`)

// ValidAgentScreenshotName reports whether name is a screenshot file name
// Kuayle reads: a plain PNG, JPEG or WebP name, no directories.
func ValidAgentScreenshotName(name string) bool {
	return agentScreenshotName.MatchString(name) && !strings.Contains(name, "..")
}

// ParseAgentPullRequestFile reads {"description": "...", "changes": [...],
// "verification": [...], "notes": "...", "screenshots": [...]}. Every key is
// optional and unknown keys are ignored; changes and verification are lists
// of strings or one string with an item per line; a screenshot is a file name
// or {"file": "...", "caption": "..."}. A value of the wrong type rejects the
// whole file with a fixed reason that never echoes it. Texts longer than
// their limit are cut and lists keep their first items.
func ParseAgentPullRequestFile(data []byte, oversize bool) AgentPullRequest {
	reject := func(reason string) AgentPullRequest { return AgentPullRequest{Error: reason} }
	if oversize || len(data) > AgentPullRequestFileMaxBytes {
		return reject("the file is larger than 64 KiB")
	}
	if !utf8.Valid(data) || bytes.ContainsRune(data, 0) {
		return reject("the file is not UTF-8 text")
	}
	var file struct {
		Description *string           `json:"description"`
		Changes     json.RawMessage   `json:"changes"`
		Verify      json.RawMessage   `json:"verification"`
		Notes       *string           `json:"notes"`
		Screenshots []json.RawMessage `json:"screenshots"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&file); err != nil {
		return reject("the file is not a JSON object with description, changes, verification, notes and screenshots")
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return reject("the file has content after its JSON object")
	}
	var result AgentPullRequest
	var ok bool
	if file.Description != nil {
		if result.Description, ok = agentText(*file.Description, AgentPullRequestDescriptionMaxRunes); !ok {
			return reject("\"description\" must be text")
		}
	}
	if file.Notes != nil {
		if result.Notes, ok = agentText(*file.Notes, AgentPullRequestNotesMaxRunes); !ok {
			return reject("\"notes\" must be text")
		}
	}
	if result.Changes, ok = agentItems(file.Changes); !ok {
		return reject("\"changes\" must be a list of strings or one string")
	}
	if result.Verification, ok = agentItems(file.Verify); !ok {
		return reject("\"verification\" must be a list of strings or one string")
	}
	seen := map[string]bool{}
	for _, raw := range file.Screenshots {
		var shot AgentScreenshotRef
		var name string
		if json.Unmarshal(raw, &name) == nil {
			shot.File = name
		} else if err := json.Unmarshal(raw, &shot); err != nil {
			return reject("every screenshot must be a file name or {\"file\", \"caption\"}")
		}
		if !ValidAgentScreenshotName(shot.File) {
			return reject("every screenshot must name a .png, .jpg, .jpeg or .webp file directly in " + AgentScreenshotsDir)
		}
		caption, valid := agentText(shot.Caption, AgentScreenshotCaptionMaxRunes)
		if !valid {
			return reject("a screenshot caption must be text")
		}
		if seen[shot.File] || len(result.Screenshots) == AgentScreenshotMaxCount {
			continue
		}
		seen[shot.File] = true
		result.Screenshots = append(result.Screenshots, AgentScreenshotRef{File: shot.File, Caption: strings.Join(strings.Fields(caption), " ")})
	}
	return result
}

// agentText trims a text and cuts it to limit characters, marking the cut.
func agentText(value string, limit int) (string, bool) {
	if !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
		return "", false
	}
	value = strings.TrimSpace(strings.ReplaceAll(value, "\r\n", "\n"))
	if utf8.RuneCountInString(value) > limit {
		value = strings.TrimSpace(string([]rune(value)[:limit-1])) + "…"
	}
	return value, true
}

// agentItems reads a list of strings, or one string as an item per line
// without its list marker, dropping blank items.
func agentItems(raw json.RawMessage) ([]string, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, true
	}
	var items []string
	var text string
	if json.Unmarshal(raw, &text) == nil {
		for _, line := range strings.Split(text, "\n") {
			line = strings.TrimSpace(line)
			for _, marker := range []string{"- ", "* ", "+ "} {
				line = strings.TrimSpace(strings.TrimPrefix(line, marker))
			}
			items = append(items, line)
		}
	} else if json.Unmarshal(raw, &items) != nil {
		return nil, false
	}
	result := []string{}
	for _, item := range items {
		item, ok := agentText(strings.Join(strings.Fields(item), " "), AgentPullRequestItemMaxRunes)
		if !ok {
			return nil, false
		}
		if item != "" && len(result) < AgentPullRequestMaxItems {
			result = append(result, item)
		}
	}
	if len(result) == 0 {
		return nil, true
	}
	return result, true
}

// ScreenshotContentType checks a screenshot's bytes against its name: a
// non-empty file of at most AgentScreenshotMaxBytes whose magic bytes are
// PNG, JPEG or WebP and match its extension.
func ScreenshotContentType(name string, data []byte) (string, error) {
	if len(data) == 0 {
		return "", errors.New("the screenshot is empty")
	}
	if len(data) > AgentScreenshotMaxBytes {
		return "", errors.New("the screenshot is larger than 2 MB")
	}
	var contentType string
	switch {
	case bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")):
		contentType = "image/png"
	case bytes.HasPrefix(data, []byte{0xff, 0xd8, 0xff}):
		contentType = "image/jpeg"
	case len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		contentType = "image/webp"
	default:
		return "", errors.New("the screenshot is not a PNG, JPEG or WebP image")
	}
	expected := map[string]string{".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".webp": "image/webp"}[strings.ToLower(path.Ext(name))]
	if expected != contentType {
		return "", errors.New("the screenshot's content does not match its file extension")
	}
	return contentType, nil
}
