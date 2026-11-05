package service

import (
	"fmt"
	"net"
	"path"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	gh "github.com/kuayle/kuayle-backend/pkg/github"
)

// The release secret scan runs on the server before a release is tagged: on
// the lines the release range adds (the diff between the previous tag and the
// target commit, from GitHub's compare API) and on the release notes. It is a
// proportionate scan for well-known credential formats, private-key headers and
// files that hold secrets, not a general entropy search; a finding names the
// file, line and type, never the value.

// secretFinding is one match. Line is 0 for a finding about the whole file.
type secretFinding struct {
	File string
	Line int
	Kind string
}

type leakRule struct {
	kind    string
	pattern *regexp.Regexp
}

var leakRules = []leakRule{
	{"private key", regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY( BLOCK)?-----`)},
	{"AWS access key ID", regexp.MustCompile(`\b(AKIA|ASIA)[0-9A-Z]{16}\b`)},
	{"GitHub token", regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{30,}|\bgithub_pat_[A-Za-z0-9_]{30,}`)},
	{"GitLab token", regexp.MustCompile(`\bglpat-[A-Za-z0-9_-]{20,}`)},
	{"Slack token", regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{10,}`)},
	{"API secret key", regexp.MustCompile(`\bsk-(ant-|proj-)?[A-Za-z0-9_-]{32,}|\b[sr]k_live_[A-Za-z0-9]{20,}`)},
	{"Google API key", regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}`)},
	{"JWT", regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`)},
}

// embeddedLogin finds user:password@ in a URL; placeholderSecret tells a
// documented placeholder from a value.
var embeddedLogin = regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9+.-]*://[^/\s:@"'` + "`" + `]+:([^/\s@"'` + "`" + `]+)@`)

func placeholderSecret(value string) bool {
	lower := strings.ToLower(value)
	if strings.ContainsAny(value, "${}<>*%") || strings.Trim(lower, "x.") == "" {
		return true
	}
	switch lower {
	case "password", "pass", "passwd", "secret", "changeme", "example", "token", "pw", "pwd":
		return true
	}
	return false
}

// scanLine returns the kinds of secrets on one line of text.
func scanLine(line string) []string {
	var kinds []string
	for _, rule := range leakRules {
		if rule.pattern.MatchString(line) {
			kinds = append(kinds, rule.kind)
		}
	}
	for _, match := range embeddedLogin.FindAllStringSubmatch(line, -1) {
		if !placeholderSecret(match[1]) {
			kinds = append(kinds, "URL with embedded credentials")
			break
		}
	}
	return kinds
}

// sensitiveFile names files that hold secrets or data whatever their content.
// Templates such as .env.example are content-scanned instead.
func sensitiveFile(name string) bool {
	base := strings.ToLower(path.Base(name))
	for _, suffix := range []string{".example", ".sample", ".template", ".dist"} {
		if strings.HasSuffix(base, suffix) {
			return false
		}
	}
	switch base {
	case ".env", ".npmrc", ".pypirc", ".netrc", ".pgpass", ".htpasswd", "auth.json", "credentials", "credentials.json", "kubeconfig":
		return true
	}
	for _, prefix := range []string{".env.", "id_rsa", "id_dsa", "id_ecdsa", "id_ed25519", "service-account"} {
		if strings.HasPrefix(base, prefix) {
			return true
		}
	}
	for _, suffix := range []string{".env", ".pem", ".key", ".p12", ".pfx", ".jks", ".keystore", ".kdbx", ".tfstate", ".tfvars", ".kubeconfig", ".sqlite", ".sqlite3", ".db", ".dump", ".sql.gz", ".bak"} {
		if strings.HasSuffix(base, suffix) {
			return true
		}
	}
	return strings.Contains(base, ".tfstate.") || (strings.Contains(base, "credentials") && strings.HasSuffix(base, ".json"))
}

var hunkHeader = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

// scanReleaseFiles scans what the range adds. Removed files and removed lines
// are not findings (removing a secret is the fix). unscanned lists changed
// files GitHub sent no diff for (binary or too large), which the scan could not
// read.
func scanReleaseFiles(files []gh.CompareFile) (findings []secretFinding, unscanned []string) {
	for _, file := range files {
		if file.Status == "removed" {
			continue
		}
		if sensitiveFile(file.Filename) {
			findings = append(findings, secretFinding{File: file.Filename, Kind: "sensitive file (" + path.Base(file.Filename) + ")"})
			continue
		}
		if file.Patch == "" {
			if file.Status != "renamed" {
				unscanned = append(unscanned, file.Filename)
			}
			continue
		}
		line := 0
		for _, text := range strings.Split(file.Patch, "\n") {
			if match := hunkHeader.FindStringSubmatch(text); match != nil {
				line, _ = strconv.Atoi(match[1])
				continue
			}
			switch {
			case strings.HasPrefix(text, "+"):
				for _, kind := range scanLine(text[1:]) {
					findings = append(findings, secretFinding{File: file.Filename, Line: line, Kind: kind})
				}
				line++
			case strings.HasPrefix(text, "-"), strings.HasPrefix(text, `\`):
			default:
				line++
			}
		}
	}
	return findings, unscanned
}

// secretRefusal words the findings for the user: where and what, never the value.
func secretRefusal(where string, findings []secretFinding) string {
	return findingsMessage("The secret scan of "+where, "possible secret(s)", findings) + " Remove them (and rotate any real credential) before releasing."
}

func findingsMessage(subject, what string, findings []secretFinding) string {
	const shown = 10
	parts := make([]string, 0, shown)
	for i, f := range findings {
		if i == shown {
			parts = append(parts, fmt.Sprintf("and %d more", len(findings)-shown))
			break
		}
		location := f.File
		if f.Line > 0 {
			location = fmt.Sprintf("%s line %d", f.File, f.Line)
		}
		parts = append(parts, fmt.Sprintf("%s (%s)", location, f.Kind))
	}
	return fmt.Sprintf("%s found %d %s: %s.", subject, len(findings), what, strings.Join(parts, "; "))
}

// maxReleaseNotesRunes bounds the notes; GitHub allows far more, but release
// notes this long are a mistake, and the tool arguments are bounded too.
const maxReleaseNotesRunes = 20000

var (
	ipv4Candidate = regexp.MustCompile(`\b\d{1,3}(?:\.\d{1,3}){3}\b`)
	privateHost   = regexp.MustCompile(`(?i)\b[a-z0-9](?:[a-z0-9-]*[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]*[a-z0-9])?)*\.(?:internal|local|lan|corp|home|intranet|localdomain|private)\b`)
	localPath     = regexp.MustCompile(`(?:/Users/|/home/)[A-Za-z0-9._-]+/|(?i)\b[a-z]:\\Users\\`)
	cgnat         = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}
)

func privateAddress(candidate string) bool {
	ip := net.ParseIP(candidate)
	return ip != nil && (ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || cgnat.Contains(ip))
}

// checkReleaseNotes validates notes before they are offered for approval and
// again before they are published: their size, and that they hold no secret,
// private address or host name, or local path.
func checkReleaseNotes(notes string) error {
	if strings.TrimSpace(notes) == "" {
		return toolFailure("the release notes are empty")
	}
	if n := utf8.RuneCountInString(notes); n > maxReleaseNotesRunes {
		return toolFailure("the release notes are %d characters; they can be at most %d", n, maxReleaseNotesRunes)
	}
	var findings []secretFinding
	for i, line := range strings.Split(notes, "\n") {
		kinds := scanLine(line)
		for _, candidate := range ipv4Candidate.FindAllString(line, -1) {
			if privateAddress(candidate) {
				kinds = append(kinds, "private address")
				break
			}
		}
		if privateHost.MatchString(line) {
			kinds = append(kinds, "private host name")
		}
		if localPath.MatchString(line) {
			kinds = append(kinds, "local path")
		}
		for _, kind := range kinds {
			findings = append(findings, secretFinding{File: "notes", Line: i + 1, Kind: kind})
		}
	}
	if len(findings) > 0 {
		return toolFailure("%s Remove them from the release notes; release notes are public.", findingsMessage("The check of the release notes", "secret(s), private address(es), host name(s) or local path(s)", findings))
	}
	return nil
}
