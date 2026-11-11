package sanitize

import (
	"regexp"
	"strings"
)

// CredentialRule is one well-known credential format.
type CredentialRule struct {
	Kind    string
	Pattern *regexp.Regexp
}

// CredentialRules are the well-known credential formats and private-key
// headers. The release secret scan reports them; RedactCredentials removes
// them. They are a proportionate list, not a general entropy search.
var CredentialRules = []CredentialRule{
	{"private key", regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY( BLOCK)?-----`)},
	{"AWS access key ID", regexp.MustCompile(`\b(AKIA|ASIA)[0-9A-Z]{16}\b`)},
	{"GitHub token", regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{30,}|\bgithub_pat_[A-Za-z0-9_]{30,}`)},
	{"GitLab token", regexp.MustCompile(`\bglpat-[A-Za-z0-9_-]{20,}`)},
	{"Slack token", regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{10,}`)},
	{"API secret key", regexp.MustCompile(`\bsk-(ant-|proj-)?[A-Za-z0-9_-]{32,}|\b[sr]k_live_[A-Za-z0-9]{20,}`)},
	{"Google API key", regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}`)},
	{"JWT", regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`)},
}

// EmbeddedLogin finds user:password@ in a URL; its submatch is the password.
var EmbeddedLogin = regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9+.-]*://[^/\s:@"'` + "`" + `]+:([^/\s@"'` + "`" + `]+)@`)

// PlaceholderSecret tells a documented placeholder from a value.
func PlaceholderSecret(value string) bool {
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

// RedactCredentials replaces well-known credential formats and passwords
// embedded in URLs with [REDACTED].
func RedactCredentials(text string) string {
	for _, rule := range CredentialRules {
		text = rule.Pattern.ReplaceAllString(text, "[REDACTED]")
	}
	return EmbeddedLogin.ReplaceAllStringFunc(text, func(match string) string {
		parts := EmbeddedLogin.FindStringSubmatch(match)
		if PlaceholderSecret(parts[1]) {
			return match
		}
		return match[:len(match)-len(parts[1])-1] + "[REDACTED]@"
	})
}
