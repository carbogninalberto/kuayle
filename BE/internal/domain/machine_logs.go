package domain

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/kuayle/kuayle-backend/pkg/sanitize"
)

const (
	MachineLogMaxLines = 100
	// Even six-byte JSON escapes leave room in the 24 KiB tool response.
	MachineLogBytes = 3 << 10
)

var (
	containerName    = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)
	logAuthorization = regexp.MustCompile(`(?i)\b((?:bearer|authorization:[ \t]*basic)[ \t]+)[A-Za-z0-9._~+/=-]+`)
	logPrivateKey    = regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY(?: BLOCK)?-----.*?(?:-----END [A-Z ]*PRIVATE KEY(?: BLOCK)?-----|$)`)
	logKeyEnd        = regexp.MustCompile(`-----END [A-Z ]*PRIVATE KEY(?: BLOCK)?-----`)
)

func (req WorktreeScanRequest) ValidDockerRead() bool {
	if req.Limit < 1 || req.Limit > MachineLogMaxLines {
		return false
	}
	switch req.Kind {
	case MachineScanContainers:
		return req.Container == ""
	case MachineScanContainerLogs:
		return containerName.MatchString(req.Container)
	}
	return false
}

type MachineLogResult struct {
	Content   string `json:"content"`
	Truncated bool   `json:"truncated"`
}

// MachineLogOutput applies credential redaction before either tail boundary
// can split a secret. truncated also reports loss at the source.
func MachineLogOutput(raw string, lines int, secrets []string, truncated bool) MachineLogResult {
	text := sanitize.SecretReplacer(secrets).Replace(raw)
	text = logPrivateKey.ReplaceAllString(text, "[REDACTED]")
	if end := logKeyEnd.FindStringIndex(text); end != nil {
		// Docker's requested tail can start inside a PEM block.
		text = "[REDACTED]" + text[end[1]:]
	}
	text = logAuthorization.ReplaceAllString(text, "${1}[REDACTED]")
	text = sanitize.RedactCredentials(sanitize.RedactDSNs(text))
	text = sanitize.PostgresText(RedactSecrets(text))
	text = strings.TrimSuffix(text, "\n")
	end := len(text)
	for i := 1; i <= lines; i++ {
		end = strings.LastIndexByte(text[:end], '\n')
		if end < 0 {
			break
		}
		if i == lines {
			text, truncated = text[end+1:], true
		}
	}
	if len(text) > MachineLogBytes {
		text, truncated = text[len(text)-MachineLogBytes:], true
		for !utf8.RuneStart(text[0]) {
			text = text[1:]
		}
	}
	return MachineLogResult{Content: text, Truncated: truncated}
}
