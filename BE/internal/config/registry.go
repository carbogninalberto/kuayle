package config

import (
	"fmt"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// The registry describes every environment variable the API process reads,
// for Settings -> Instance configuration (sysadmins only). registry_test.go
// fails when a Config field has no entry here, and when a variable whose name
// looks like a credential is not marked Secret, so a new variable cannot be
// left out or shown by mistake.

// VariableKind is how a value is parsed and shown.
type VariableKind string

const (
	KindString   VariableKind = "string"
	KindBool     VariableKind = "bool"
	KindInt      VariableKind = "int"
	KindFloat    VariableKind = "float"
	KindDuration VariableKind = "duration"
)

// Source says where an effective value comes from.
type Source string

const (
	SourceEnvironment Source = "environment"
	SourceDefault     Source = "default"
	// SourceOverride is a sysadmin's value stored in the database; only the
	// instance configuration service reports it.
	SourceOverride Source = "override"
)

// Variable groups, in the order the page lists them. The UI translates them.
const (
	GroupServer       = "server"
	GroupData         = "data"
	GroupSecurity     = "security"
	GroupSignIn       = "sign_in"
	GroupWorkflows    = "workflows"
	GroupStorage      = "storage"
	GroupEmail        = "email"
	GroupPush         = "push"
	GroupBackups      = "backups"
	GroupNetwork      = "network"
	GroupIntegrations = "integrations"
	GroupAssistant    = "assistant"
	GroupUpdates      = "updates"
	GroupDevMachines  = "dev_machines"
)

// Groups lists the variable groups in display order.
func Groups() []string {
	return []string{GroupServer, GroupData, GroupSecurity, GroupSignIn, GroupWorkflows, GroupStorage, GroupEmail,
		GroupPush, GroupBackups, GroupNetwork, GroupIntegrations, GroupAssistant, GroupUpdates, GroupDevMachines}
}

// The variables a sysadmin may change at runtime. Every reader of each one
// reads it per use through the instance configuration service, so a change
// applies without a restart.
const (
	VarWorkflowPipelines   = "WORKFLOW_PIPELINES_ENABLED"
	VarDisableRegistration = "DISABLE_REGISTRATION"
	VarBackupTimeout       = "BACKUP_TIMEOUT"
)

// maxRuntimeValue bounds a stored override (instance_config_overrides.value).
const maxRuntimeValue = 256

// RuntimeRule makes a variable editable without a restart and says which
// values it accepts. Min and Max bound a duration, inclusive.
type RuntimeRule struct {
	Kind     VariableKind
	Min, Max time.Duration
}

// Variable is one environment variable the API reads.
type Variable struct {
	Name        string
	Group       string
	Description string
	// Secret values are never returned, not even in part: only whether they
	// are set.
	Secret bool
	// Runtime, when set, lets a sysadmin override the value from Settings
	// without a restart. Nil means the variable is set in .env and needs a
	// restart.
	Runtime *RuntimeRule
	// Standalone variables are read outside Config, by a command of the
	// server binary.
	Standalone bool
}

var registry = []Variable{
	// Server
	{Name: "PORT", Group: GroupServer, Description: "Port the API listens on inside its container."},
	{Name: "ENVIRONMENT", Group: GroupServer, Description: "development, or anything else for production rules: example secrets are refused outside development."},
	{Name: "FRONTEND_URL", Group: GroupServer, Description: "Public origin of the web app; links, CORS, cookies and webhook URLs derive from it."},
	{Name: "AUTOMATION_WORKER_EXTERNAL", Group: GroupServer, Description: "Runs automations in a separate automation-worker process instead of inside the API."},

	// Data
	{Name: "DATABASE_URL", Group: GroupData, Description: "PostgreSQL connection URL, including its credentials.", Secret: true},
	{Name: "REDIS_URL", Group: GroupData, Description: "Redis connection URL.", Secret: true},
	{Name: "ISSUE_DELETE_GRACE_PERIOD", Group: GroupData, Description: "How long deleted issues, comments and wiki pages stay restorable before the hourly purge removes them."},

	// Security
	{Name: "JWT_SECRET", Group: GroupSecurity, Description: "Signs sessions and derives the keys that encrypt stored credentials; keep it on every replica and backup.", Secret: true},
	{Name: "SYSADMINS", Group: GroupSecurity, Description: "Comma-separated user IDs of the instance administrators."},
	{Name: "PUBLIC_DEMO_MODE", Group: GroupSecurity, Description: "Public demo: no password sign-up, and Dev Machines only for sysadmins."},

	// Sign-in
	{Name: VarDisableRegistration, Group: GroupSignIn, Description: "Allows sign-up only with an invite link.",
		Runtime: &RuntimeRule{Kind: KindBool}},
	{Name: "PASSKEYS_ENABLED", Group: GroupSignIn, Description: "Offers passkey sign-in and registration."},
	{Name: "PASSKEY_RP_ID", Group: GroupSignIn, Description: "Passkey relying party ID; defaults to the host of FRONTEND_URL."},
	{Name: "PASSKEY_RP_ORIGINS", Group: GroupSignIn, Description: "Comma-separated origins allowed in passkey ceremonies; defaults to FRONTEND_URL."},
	{Name: "PASSKEY_RP_DISPLAY_NAME", Group: GroupSignIn, Description: "Name authenticators show for this instance."},
	{Name: "SSO_OIDC_WORKSPACE_SLUG", Group: GroupSignIn, Description: "Workspace whose OIDC sign-in this instance configures; its settings page becomes read-only."},
	{Name: "SSO_OIDC_ISSUER", Group: GroupSignIn, Description: "Issuer URL of the instance-level OIDC provider."},
	{Name: "SSO_OIDC_CLIENT_ID", Group: GroupSignIn, Description: "Client ID of the instance-level OIDC application."},
	{Name: "SSO_OIDC_CLIENT_SECRET", Group: GroupSignIn, Description: "Client secret of the instance-level OIDC application.", Secret: true},
	{Name: "SSO_OIDC_ALLOWED_DOMAINS", Group: GroupSignIn, Description: "Comma-separated email domains allowed to sign in through the instance OIDC provider."},
	{Name: "SSO_OIDC_DEFAULT_ROLE", Group: GroupSignIn, Description: "Role of a person who joins through instance OIDC: member or guest."},
	{Name: "SSO_OIDC_REQUIRE_SSO", Group: GroupSignIn, Description: "Requires members of the SSO workspace to sign in with OIDC."},
	{Name: "SAML_SP_CERT_FILE", Group: GroupSignIn, Description: "Path to the certificate Kuayle signs SAML requests with."},
	{Name: "SAML_SP_KEY_FILE", Group: GroupSignIn, Description: "Path to the private key Kuayle signs SAML requests with.", Secret: true},

	// Workflows
	{Name: VarWorkflowPipelines, Group: GroupWorkflows, Description: "Allows saving pipeline (team) workflows and starting tasks on them; tasks already on one keep running either way.",
		Runtime: &RuntimeRule{Kind: KindBool}},

	// Storage
	{Name: "STORAGE_TYPE", Group: GroupStorage, Description: "Where uploads live: local or s3."},
	{Name: "STORAGE_LOCAL_DIR", Group: GroupStorage, Description: "Directory of local uploads."},
	{Name: "STORAGE_LOCAL_URL", Group: GroupStorage, Description: "URL path local uploads are served under."},
	{Name: "S3_ENDPOINT", Group: GroupStorage, Description: "S3-compatible endpoint; empty uses AWS."},
	{Name: "S3_BUCKET", Group: GroupStorage, Description: "Bucket for uploads."},
	{Name: "S3_REGION", Group: GroupStorage, Description: "Region of the upload bucket."},
	{Name: "S3_ACCESS_KEY", Group: GroupStorage, Description: "Access key ID for the upload bucket.", Secret: true},
	{Name: "S3_SECRET_KEY", Group: GroupStorage, Description: "Secret access key for the upload bucket.", Secret: true},
	{Name: "S3_PUBLIC", Group: GroupStorage, Description: "Serves uploads from public bucket URLs instead of signed ones."},
	{Name: "S3_CDN_BASE_URL", Group: GroupStorage, Description: "Base URL of a CDN in front of the bucket."},

	// Email
	{Name: "SMTP_HOST", Group: GroupEmail, Description: "SMTP server for password reset and notification email; empty turns email off."},
	{Name: "SMTP_PORT", Group: GroupEmail, Description: "SMTP server port."},
	{Name: "SMTP_USERNAME", Group: GroupEmail, Description: "SMTP user name.", Secret: true},
	{Name: "SMTP_PASSWORD", Group: GroupEmail, Description: "SMTP password.", Secret: true},
	{Name: "SMTP_FROM", Group: GroupEmail, Description: "Sender address of outgoing email."},

	// Web Push
	{Name: "VAPID_PUBLIC_KEY", Group: GroupPush, Description: "Web Push public key; when set, the environment manages push instead of Settings."},
	{Name: "VAPID_PRIVATE_KEY", Group: GroupPush, Description: "Web Push private key.", Secret: true},
	{Name: "VAPID_SUBJECT", Group: GroupPush, Description: "Contact (mailto or HTTPS) push services may use."},

	// Backups
	{Name: "BACKUP_DIR", Group: GroupBackups, Description: "Directory backup archives are written to."},
	{Name: VarBackupTimeout, Group: GroupBackups, Description: "Longest a backup run or a Verify may take.",
		Runtime: &RuntimeRule{Kind: KindDuration, Min: 5 * time.Minute, Max: 72 * time.Hour}},
	{Name: "KUAYLE_BACKUP_PASSPHRASE", Group: GroupBackups, Description: "Passphrase the backup command of the server binary uses for encrypted archives.", Secret: true, Standalone: true},

	// Private network access
	{Name: "AI_ALLOW_PRIVATE_NETWORK", Group: GroupNetwork, Description: "Lets workspace AI providers use private or loopback addresses; trusts every workspace owner with internal access."},
	{Name: "FORGE_ALLOW_PRIVATE_NETWORK", Group: GroupNetwork, Description: "Lets GitLab, Gitea and Forgejo connections use private or loopback addresses."},
	{Name: "IMPORT_ALLOW_PRIVATE_NETWORK", Group: GroupNetwork, Description: "Lets Jira imports use private or loopback addresses."},
	{Name: "OIDC_ALLOW_PRIVATE_NETWORK", Group: GroupNetwork, Description: "Lets OIDC issuers use private or loopback addresses and plain HTTP."},
	{Name: "BACKUP_S3_ALLOW_PRIVATE_NETWORK", Group: GroupNetwork, Description: "Lets the backup S3 endpoint use private or loopback addresses, such as a MinIO on the same host."},

	// Integrations
	{Name: "GITHUB_APP_ID", Group: GroupIntegrations, Description: "ID of the instance-wide GitHub App shared by every workspace."},
	{Name: "GITHUB_APP_PRIVATE_KEY", Group: GroupIntegrations, Description: "Private key of the instance GitHub App (base64 PEM).", Secret: true},
	{Name: "GITHUB_APP_CLIENT_ID", Group: GroupIntegrations, Description: "OAuth client ID of the instance GitHub App."},
	{Name: "GITHUB_APP_CLIENT_SECRET", Group: GroupIntegrations, Description: "OAuth client secret of the instance GitHub App.", Secret: true},
	{Name: "GITHUB_APP_WEBHOOK_SECRET", Group: GroupIntegrations, Description: "Secret that signs the instance GitHub App's webhooks.", Secret: true},
	{Name: "GITHUB_APP_SLUG", Group: GroupIntegrations, Description: "URL name of the instance GitHub App."},
	{Name: "GITHUB_WEBHOOK_URL", Group: GroupIntegrations, Description: "Relay URL for GitHub webhooks in development; a relay URL lets anyone read the events, so it is not shown.", Secret: true},
	{Name: "SLACK_CLIENT_ID", Group: GroupIntegrations, Description: "Client ID of the Slack app."},
	{Name: "SLACK_CLIENT_SECRET", Group: GroupIntegrations, Description: "Client secret of the Slack app.", Secret: true},
	{Name: "SLACK_SIGNING_SECRET", Group: GroupIntegrations, Description: "Secret that signs Slack requests.", Secret: true},
	{Name: "TELEGRAM_WEBHOOK_URL", Group: GroupIntegrations, Description: "Relay URL for Telegram webhooks in development; a relay URL lets anyone read the events, so it is not shown.", Secret: true},
	{Name: "FORGE_WEBHOOK_BASE_URL", Group: GroupIntegrations, Description: "Origin of the forge webhook URL shown to workspace admins; defaults to FRONTEND_URL."},

	// Assistant
	{Name: "VOICE_URL", Group: GroupAssistant, Description: "Internal address of the voice transcription service."},
	{Name: "VOICE_INTERNAL_TOKEN", Group: GroupAssistant, Description: "Shared secret the voice service requires; voice stays off unless it and VOICE_URL are set.", Secret: true},
	{Name: "WEB_SEARCH_SEARXNG_URL", Group: GroupAssistant, Description: "Base URL of a self-hosted SearXNG for the assistant's web search."},

	// Updates
	{Name: "SYSTEM_UPDATER_URL", Group: GroupUpdates, Description: "Address of the updater service that installs releases from Settings -> Version."},
	{Name: "SYSTEM_UPDATER_TOKEN", Group: GroupUpdates, Description: "Shared secret the updater service requires.", Secret: true},

	// Dev Machines: the API and the machine manager both read these.
	{Name: "DEV_MACHINES_ENABLED", Group: GroupDevMachines, Description: "Turns Dev Machines and agent tasks on."},
	{Name: "DEV_MACHINE_DOMAIN", Group: GroupDevMachines, Description: "Hostname suffix of machine URLs; a separate registrable domain from FRONTEND_URL."},
	{Name: "DEV_MACHINE_ALLOW_SAME_SITE", Group: GroupDevMachines, Description: "Allows machine hostnames on the same site as FRONTEND_URL (personal instances over HTTPS)."},
	{Name: "DEV_MACHINE_ENCRYPTION_KEY", Group: GroupDevMachines, Description: "Encrypts machine credentials and tokens at rest.", Secret: true},
	{Name: "DEV_MACHINE_ACCESS_TICKET_TTL_SECONDS", Group: GroupDevMachines, Description: "Lifetime of a one-time machine access ticket, 15 to 300 seconds."},
	{Name: "DEV_MACHINE_SESSION_TTL_MINUTES", Group: GroupDevMachines, Description: "Lifetime of a machine browser session, 5 to 1440 minutes."},
	{Name: "DEV_MACHINE_GATEWAY_CONTAINER", Group: GroupDevMachines, Description: "Container name of the machine gateway."},
	{Name: "DEV_MACHINE_INGEST_URL", Group: GroupDevMachines, Description: "HTTPS URL machines report events and logs to."},
	{Name: "DEV_MACHINE_EGRESS_ALLOWLIST", Group: GroupDevMachines, Description: "Hosts machines may always reach."},
	{Name: "DEV_MACHINE_EGRESS_DENYLIST", Group: GroupDevMachines, Description: "Hosts machines may never reach."},
	{Name: "DEV_MACHINE_DOCKER_HOST", Group: GroupDevMachines, Description: "Docker endpoint the machine manager drives."},
	{Name: "DEV_MACHINE_SECCOMP_PROFILE", Group: GroupDevMachines, Description: "Seccomp profile of machine containers: default, unconfined or inline JSON."},
	{Name: "DEV_MACHINE_APPARMOR_PROFILE", Group: GroupDevMachines, Description: "AppArmor profile of machine containers."},
	{Name: "DEV_MACHINE_IDE_IMAGE", Group: GroupDevMachines, Description: "Image of IDE machines."},
	{Name: "DEV_MACHINE_HEADLESS_IMAGE", Group: GroupDevMachines, Description: "Image of headless machines and of the harnesses without their own image."},
	{Name: "DEV_MACHINE_BROWSER_IMAGE", Group: GroupDevMachines, Description: "Image of the browser sidecar."},
	{Name: "DEV_MACHINE_COLLECTOR_IMAGE", Group: GroupDevMachines, Description: "Image of the log and event collector sidecar."},
	{Name: "DEV_MACHINE_EGRESS_IMAGE", Group: GroupDevMachines, Description: "Image of the egress proxy sidecar."},
	{Name: "DEV_MACHINE_CLAUDE_IMAGE", Group: GroupDevMachines, Description: "Image of the Claude Code agent."},
	{Name: "DEV_MACHINE_OPENCODE_IMAGE", Group: GroupDevMachines, Description: "Image of the OpenCode agent."},
	{Name: "DEV_MACHINE_CODEX_IMAGE", Group: GroupDevMachines, Description: "Image of the Codex agent."},
	{Name: "DEV_MACHINE_CUSTOM_IMAGE", Group: GroupDevMachines, Description: "Image of the custom CLI agent."},
	{Name: "DEV_MACHINE_DOCKER_IMAGE", Group: GroupDevMachines, Description: "Image of the per-machine Docker daemon."},
	{Name: "DEV_MACHINE_DOCKER_RUNTIME", Group: GroupDevMachines, Description: "Optional OCI runtime for the machine Docker daemon, such as sysbox-runc."},
	{Name: "DEV_MACHINE_DOCKER_SECCOMP_PROFILE", Group: GroupDevMachines, Description: "Seccomp profile of the machine Docker daemon without a runtime."},
	{Name: "DEV_MACHINE_DOCKER_APPARMOR_PROFILE", Group: GroupDevMachines, Description: "AppArmor profile of the machine Docker daemon without a runtime."},
	{Name: "DEV_MACHINE_DISK_QUOTA_MODE", Group: GroupDevMachines, Description: "How disk limits are enforced: hard (XFS quotas) or monitored."},
	{Name: "DEV_MACHINE_HOST_DISK_MIN_FREE_PERCENT", Group: GroupDevMachines, Description: "Host free-space floor in monitored mode, in percent."},
	{Name: "DEV_MACHINE_HOST_DISK_MIN_FREE_GB", Group: GroupDevMachines, Description: "Host free-space floor in monitored mode, in GB."},
	{Name: "DEV_MACHINE_HOST_DISK_PATH", Group: GroupDevMachines, Description: "Path on Docker's filesystem used to measure host free space."},
	{Name: "DEV_MACHINE_AGENT_GIT_NAME", Group: GroupDevMachines, Description: "Commit author name of agent task publications."},
	{Name: "DEV_MACHINE_AGENT_GIT_EMAIL", Group: GroupDevMachines, Description: "Commit author email of agent task publications."},
	{Name: "DEV_MACHINE_HOST_RESERVED_CPU_MILLIS", Group: GroupDevMachines, Description: "CPU kept for the host and control plane, in millicores."},
	{Name: "DEV_MACHINE_HOST_RESERVED_MEMORY_MB", Group: GroupDevMachines, Description: "Memory kept for the host and control plane, in MB."},
	{Name: "DEV_MACHINE_CPU_OVERCOMMIT", Group: GroupDevMachines, Description: "Allowed CPU overcommit, 1 to 10; memory is never overcommitted."},
	{Name: "DEV_MACHINE_SLICES", Group: GroupDevMachines, Description: "Groups a machine's containers in one systemd slice: auto or off."},
	{Name: "DEV_MACHINE_SWAP_PERCENT", Group: GroupDevMachines, Description: "Swap ceiling of a machine slice, in percent of its memory."},
	{Name: "DEV_MACHINE_AGENT_CLI_REFRESH_HOURS", Group: GroupDevMachines, Description: "How often the latest agent CLIs are installed for all machines; 0 turns it off."},
	{Name: "DEV_MACHINE_AGENT_CLI_MIN_AGE_HOURS", Group: GroupDevMachines, Description: "How long an agent CLI version must have been published before machines use it."},
}

var registryByName = func() map[string]Variable {
	byName := make(map[string]Variable, len(registry))
	for _, variable := range registry {
		byName[variable.Name] = variable
	}
	return byName
}()

// Variables lists every registered variable in display order within groups.
func Variables() []Variable {
	return append([]Variable(nil), registry...)
}

// Lookup returns the registered variable called name.
func Lookup(name string) (Variable, bool) {
	variable, ok := registryByName[name]
	return variable, ok
}

// Normalize validates raw as a runtime value of the variable and returns it
// in canonical form. A variable without a runtime rule accepts nothing.
func (v Variable) Normalize(raw string) (string, error) {
	if v.Runtime == nil {
		return "", fmt.Errorf("%s is set in .env and needs a restart", v.Name)
	}
	raw = strings.TrimSpace(raw)
	if len(raw) > maxRuntimeValue {
		return "", fmt.Errorf("%s is too long", v.Name)
	}
	switch v.Runtime.Kind {
	case KindBool:
		value, err := strconv.ParseBool(raw)
		if err != nil {
			return "", fmt.Errorf("%s must be true or false", v.Name)
		}
		return strconv.FormatBool(value), nil
	case KindDuration:
		value, err := time.ParseDuration(raw)
		if err != nil {
			return "", fmt.Errorf("%s must be a duration such as 90m or 2h", v.Name)
		}
		if value < v.Runtime.Min || value > v.Runtime.Max {
			return "", fmt.Errorf("%s must be between %s and %s", v.Name, FormatDuration(v.Runtime.Min), FormatDuration(v.Runtime.Max))
		}
		return FormatDuration(value), nil
	default:
		return "", fmt.Errorf("%s cannot be changed at runtime", v.Name)
	}
}

// FormatDuration writes d without zero trailing units: 720h, 1h30m, 5m.
func FormatDuration(d time.Duration) string {
	text := d.String()
	if strings.HasSuffix(text, "m0s") {
		text = strings.TrimSuffix(text, "0s")
	}
	if strings.HasSuffix(text, "h0m") {
		text = strings.TrimSuffix(text, "0m")
	}
	return text
}

// VariableState is a variable's effective value in the API process.
type VariableState struct {
	Variable
	Kind VariableKind
	// Value is the effective value. It is always empty for a secret and for
	// a withheld value.
	Value string
	// Set reports whether the variable has a non-empty value.
	Set bool
	// Withheld marks a plain setting whose value carries credentials, such
	// as a URL with a user, a password or a query string.
	Withheld bool
	// Default is the value used when the environment does not set it; empty
	// for a secret.
	Default string
	Source  Source
}

// Describe lists every registered variable with its effective value in cfg.
// lookup reads the process environment, which tells an explicit value from a
// default.
func Describe(cfg *Config, lookup func(string) (string, bool)) []VariableState {
	fields := map[string]fieldValue{}
	collectFields(reflect.ValueOf(cfg).Elem(), fields)
	states := make([]VariableState, 0, len(registry))
	for _, variable := range registry {
		state := VariableState{Variable: variable, Kind: KindString, Source: SourceDefault}
		if raw, ok := lookup(variable.Name); ok && strings.TrimSpace(raw) != "" {
			state.Source = SourceEnvironment
		}
		field, ok := fields[variable.Name]
		switch {
		case !ok:
			// A standalone variable is read from the environment as is.
			raw, _ := lookup(variable.Name)
			state.Set = strings.TrimSpace(raw) != ""
			if !variable.Secret {
				state.Value = raw
			}
		case variable.Secret:
			_, state.Kind = formatValue(field.value)
			state.Set = !field.value.IsZero() && (field.value.Kind() != reflect.String || strings.TrimSpace(field.value.String()) != "")
		default:
			state.Value, state.Kind = formatValue(field.value)
			state.Default = field.info.Tag.Get("default")
			state.Set = state.Value != ""
		}
		if !variable.Secret && carriesCredentials(state.Value) {
			state.Value, state.Withheld = "", true
		}
		states = append(states, state)
	}
	return states
}

type fieldValue struct {
	value reflect.Value
	info  reflect.StructField
}

func collectFields(v reflect.Value, out map[string]fieldValue) {
	for i := 0; i < v.NumField(); i++ {
		info := v.Type().Field(i)
		if name := info.Tag.Get("envconfig"); name != "" {
			out[name] = fieldValue{value: v.Field(i), info: info}
			continue
		}
		if info.IsExported() && info.Type.Kind() == reflect.Struct {
			collectFields(v.Field(i), out)
		}
	}
}

var durationType = reflect.TypeOf(time.Duration(0))

func formatValue(v reflect.Value) (string, VariableKind) {
	if v.Type() == durationType {
		return FormatDuration(time.Duration(v.Int())), KindDuration
	}
	switch v.Kind() {
	case reflect.Bool:
		return strconv.FormatBool(v.Bool()), KindBool
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(v.Int(), 10), KindInt
	case reflect.Float32, reflect.Float64:
		return strconv.FormatFloat(v.Float(), 'g', -1, 64), KindFloat
	default:
		return v.String(), KindString
	}
}

// carriesCredentials reports whether a value, or an item of a comma-separated
// list, is a URL with a user, a password, a query or a fragment.
func carriesCredentials(value string) bool {
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if !strings.Contains(part, "://") {
			continue
		}
		parsed, err := url.Parse(part)
		if err != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
			return true
		}
	}
	return false
}
