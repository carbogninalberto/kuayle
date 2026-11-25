package config

import (
	"fmt"
	"net"
	"net/mail"
	"net/url"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Settings -> Instance configuration also edits the variables the API reads
// once, at startup (KUA-311). A sysadmin proposes values, a dry run checks
// them, and "Apply and restart" stores them as overrides that the API merges
// over its environment on its next start (WithOverrides). The variables that
// may be edited this way are the ones carrying a RestartRule in registry.go.

// RestartFormat says how a string value is checked.
type RestartFormat string

const (
	// FormatText is a short single-line text.
	FormatText RestartFormat = "text"
	// FormatURL is an absolute URL of one of the rule's schemes, without
	// credentials, a query or a fragment.
	FormatURL RestartFormat = "url"
	// FormatHost is a host name or IP address, without a port.
	FormatHost RestartFormat = "host"
	// FormatEmail is an email address, optionally with a display name.
	FormatEmail RestartFormat = "email"
)

// defaultRestartMaxLen bounds a string value without its own MaxLen. It never
// exceeds the column holding an override (maxRuntimeValue).
const defaultRestartMaxLen = 128

// RestartRule makes a variable editable with an apply-and-restart and says
// which values it accepts.
type RestartRule struct {
	Kind VariableKind
	// Format refines a string; the zero value is a choice from Choices when
	// there are any, FormatText otherwise.
	Format RestartFormat
	// Min and Max bound a duration, IntMin and IntMax an integer; inclusive.
	Min, Max       time.Duration
	IntMin, IntMax int64
	// Choices lists the accepted values of an enumeration (lower case).
	Choices []string
	// Schemes lists the accepted URL schemes.
	Schemes []string
	// AllowEmpty accepts an empty string, which turns the feature off.
	AllowEmpty bool
	// MaxLen bounds a string; defaultRestartMaxLen when zero.
	MaxLen int
}

// restartForbidden lists what must never carry a RestartRule, however
// convenient: credentials and keys (also anything Secret), the database and
// Redis addresses, the session signing key, the instance administrators,
// private-network switches and the allow or deny lists that guard outgoing
// requests, internal service addresses, and every variable that another
// service also reads (those would diverge until that service restarts with
// the same value). registry_test.go enforces it on the registry, and
// RestartEditable on every use.
var restartForbidden = regexp.MustCompile(`(?:SECRET|PASSWORD|PASSPHRASE|TOKEN|PRIVATE_KEY|ENCRYPTION|CREDENTIAL|USERNAME|` +
	`ACCESS_KEY|_KEY$|_KEY_|KEY_FILE|DATABASE_URL|REDIS_URL|^JWT_|^SYSADMINS$|^ENVIRONMENT$|^PORT$|^FRONTEND_URL$|^PUBLIC_DEMO_MODE$|` +
	`ALLOW_PRIVATE_NETWORK|ALLOWLIST|DENYLIST|ALLOWED_HOSTS?|SEARXNG_URL|^VOICE_|UPDATER|INGEST_URL|^DEV_MACHINE|^BACKUP_|^STORAGE_|WEBHOOK_URL)`)

// ForbiddenForRestart reports whether a variable name is of a kind that can
// never be edited from Settings.
func ForbiddenForRestart(name string) bool { return restartForbidden.MatchString(name) }

// RestartEditable reports whether the variable can be changed from Settings
// with an apply-and-restart.
func (v Variable) RestartEditable() bool {
	return v.Restart != nil && !v.Secret && !ForbiddenForRestart(v.Name)
}

// NormalizeRestart validates raw as a value of a restart-editable variable
// and returns it in canonical form. It checks the value alone; the whole
// configuration is checked by Config.WithOverrides.
func (v Variable) NormalizeRestart(raw string) (string, error) {
	if !v.RestartEditable() {
		return "", fmt.Errorf("%s cannot be changed from Settings", v.Name)
	}
	rule := v.Restart
	raw = strings.TrimSpace(raw)
	maxLen := rule.MaxLen
	if maxLen <= 0 {
		maxLen = defaultRestartMaxLen
	}
	if rule.Format == FormatURL || rule.Format == FormatEmail {
		maxLen = maxRuntimeValue
	}
	if len(raw) > maxLen {
		return "", fmt.Errorf("%s must be at most %d characters", v.Name, maxLen)
	}
	if strings.ContainsFunc(raw, unicode.IsControl) {
		return "", fmt.Errorf("%s must not contain control characters", v.Name)
	}
	switch rule.Kind {
	case KindBool:
		value, err := strconv.ParseBool(raw)
		if err != nil {
			return "", fmt.Errorf("%s must be true or false", v.Name)
		}
		return strconv.FormatBool(value), nil
	case KindInt:
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return "", fmt.Errorf("%s must be a whole number", v.Name)
		}
		if value < rule.IntMin || value > rule.IntMax {
			return "", fmt.Errorf("%s must be between %d and %d", v.Name, rule.IntMin, rule.IntMax)
		}
		return strconv.FormatInt(value, 10), nil
	case KindDuration:
		value, err := time.ParseDuration(raw)
		if err != nil {
			return "", fmt.Errorf("%s must be a duration such as 90m or 720h", v.Name)
		}
		if value < rule.Min || value > rule.Max {
			return "", fmt.Errorf("%s must be between %s and %s", v.Name, FormatDuration(rule.Min), FormatDuration(rule.Max))
		}
		return FormatDuration(value), nil
	case KindString:
		return v.normalizeString(raw)
	default:
		return "", fmt.Errorf("%s cannot be changed from Settings", v.Name)
	}
}

func (v Variable) normalizeString(raw string) (string, error) {
	rule := v.Restart
	if raw == "" {
		if rule.AllowEmpty {
			return "", nil
		}
		return "", fmt.Errorf("%s must not be empty", v.Name)
	}
	if len(rule.Choices) > 0 {
		lower := strings.ToLower(raw)
		for _, choice := range rule.Choices {
			if lower == choice {
				return choice, nil
			}
		}
		return "", fmt.Errorf("%s must be one of %s", v.Name, strings.Join(rule.Choices, ", "))
	}
	switch rule.Format {
	case FormatURL:
		return normalizeURL(v.Name, raw, rule.Schemes)
	case FormatHost:
		return normalizeHost(v.Name, raw)
	case FormatEmail:
		address, err := mail.ParseAddress(raw)
		if err != nil {
			return "", fmt.Errorf("%s must be an email address such as kuayle@example.com", v.Name)
		}
		return address.String(), nil
	default:
		return raw, nil
	}
}

func normalizeURL(name, raw string, schemes []string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.Hostname() == "" {
		return "", fmt.Errorf("%s must be an absolute URL such as https://example.com", name)
	}
	accepted := false
	for _, scheme := range schemes {
		accepted = accepted || strings.EqualFold(parsed.Scheme, scheme)
	}
	if !accepted {
		return "", fmt.Errorf("%s must start with %s://", name, strings.Join(schemes, ":// or "))
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || strings.Contains(raw, "?") || strings.Contains(raw, "#") {
		return "", fmt.Errorf("%s must not contain credentials, a query or a fragment", name)
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	return parsed.String(), nil
}

var hostLabel = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

func normalizeHost(name, raw string) (string, error) {
	host := strings.ToLower(raw)
	if net.ParseIP(host) != nil {
		return host, nil
	}
	if len(host) > 253 {
		return "", fmt.Errorf("%s is not a valid host name", name)
	}
	for _, label := range strings.Split(strings.TrimSuffix(host, "."), ".") {
		if !hostLabel.MatchString(label) {
			return "", fmt.Errorf("%s must be a host name or IP address without a scheme or port", name)
		}
	}
	return host, nil
}

// RestartEditableNames lists the names Settings may edit, sorted.
func RestartEditableNames() []string {
	names := []string{}
	for _, variable := range registry {
		if variable.RestartEditable() {
			names = append(names, variable.Name)
		}
	}
	sort.Strings(names)
	return names
}

// WithOverrides returns a copy of the configuration with the given variable
// values laid over it: each value must satisfy its variable's rule, and the
// whole is checked as Load checks it. Nothing is read from or written to the
// process environment, so it is safe to call in a running server for a dry
// run.
func (c *Config) WithOverrides(values map[string]string) (*Config, error) {
	next := *c
	fields := map[string]fieldValue{}
	collectFields(reflect.ValueOf(&next).Elem(), fields)
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		variable, ok := Lookup(name)
		if !ok || !variable.RestartEditable() {
			return nil, fmt.Errorf("%s cannot be changed from Settings", name)
		}
		field, ok := fields[name]
		if !ok {
			return nil, fmt.Errorf("%s is not a setting of the API", name)
		}
		// A stored row is checked again: it could have been edited by hand.
		normalized, err := variable.NormalizeRestart(values[name])
		if err != nil {
			return nil, err
		}
		if err := setField(field.value, normalized); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
	}
	if err := next.finalize(); err != nil {
		return nil, err
	}
	return &next, nil
}

func setField(field reflect.Value, raw string) error {
	if field.Type() == durationType {
		value, err := time.ParseDuration(raw)
		if err != nil {
			return err
		}
		field.SetInt(int64(value))
		return nil
	}
	switch field.Kind() {
	case reflect.Bool:
		value, err := strconv.ParseBool(raw)
		if err != nil {
			return err
		}
		field.SetBool(value)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return err
		}
		field.SetInt(value)
	case reflect.Float32, reflect.Float64:
		value, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return err
		}
		field.SetFloat(value)
	case reflect.String:
		field.SetString(raw)
	default:
		return fmt.Errorf("unsupported type %s", field.Type())
	}
	return nil
}

// CarriesCredentials reports whether a value, or an item of a
// comma-separated list, is a URL with a user, a password, a query or a
// fragment. Such a value is never logged or returned.
func CarriesCredentials(value string) bool { return carriesCredentials(value) }
