package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// AssistantCustomTool is a workspace-authored script the assistant can run on
// a dev machine. Its content (description, parameters, script, interpreter,
// run mode) is the version in effect (CurrentVersion); a draft version above
// it awaits activation by an owner or admin.
type AssistantCustomTool struct {
	ID          uuid.UUID       `json:"id" db:"id"`
	WorkspaceID uuid.UUID       `json:"workspace_id" db:"workspace_id"`
	Name        string          `json:"name" db:"name"`
	Description string          `json:"description" db:"description"`
	Parameters  json.RawMessage `json:"parameters" db:"parameters"`
	// Script is left out of lists.
	Script          string     `json:"script,omitempty" db:"script"`
	Interpreter     string     `json:"interpreter" db:"interpreter"`
	RunMode         string     `json:"run_mode" db:"run_mode"`
	ReadOnly        bool       `json:"read_only" db:"read_only"`
	TimeoutSeconds  int        `json:"timeout_seconds" db:"timeout_seconds"`
	Enabled         bool       `json:"enabled" db:"enabled"`
	Status          string     `json:"status" db:"status"`
	CurrentVersion  int        `json:"current_version" db:"current_version"`
	LatestVersion   int        `json:"latest_version" db:"latest_version"`
	PendingVersion  *int       `json:"pending_version" db:"pending_version"`
	CreatedByUserID *uuid.UUID `json:"created_by_user_id" db:"created_by_user_id"`
	AuthorKind      string     `json:"author_kind" db:"author_kind"`
	CreatedAt       time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at" db:"updated_at"`
}

// AssistantCustomToolVersion is one saved edit of a custom tool.
type AssistantCustomToolVersion struct {
	ToolID         uuid.UUID       `json:"tool_id" db:"tool_id"`
	Version        int             `json:"version" db:"version"`
	Name           string          `json:"name" db:"name"`
	Description    string          `json:"description" db:"description"`
	Parameters     json.RawMessage `json:"parameters" db:"parameters"`
	Script         string          `json:"script" db:"script"`
	Interpreter    string          `json:"interpreter" db:"interpreter"`
	RunMode        string          `json:"run_mode" db:"run_mode"`
	ReadOnly       bool            `json:"read_only" db:"read_only"`
	TimeoutSeconds int             `json:"timeout_seconds" db:"timeout_seconds"`
	Status         string          `json:"status" db:"status"`
	AuthorKind     string          `json:"author_kind" db:"author_kind"`
	AuthorUserID   *uuid.UUID      `json:"author_user_id" db:"author_user_id"`
	CreatedAt      time.Time       `json:"created_at" db:"created_at"`
}

// AssistantCustomToolInput is validated tool content. Status is "draft" or
// "active"; empty keeps the tool's current status on updates. Parameters is
// the canonical schema from ParseToolSchema.
type AssistantCustomToolInput struct {
	Name           string
	Description    string
	Parameters     json.RawMessage
	Script         string
	Interpreter    string
	RunMode        string
	ReadOnly       bool
	TimeoutSeconds int
	Status         string
	// AuthorKind is "user" for the API and "assistant" for propose_tool.
	AuthorKind string
}

// AssistantToolRun is one execution of a custom tool version on a machine.
type AssistantToolRun struct {
	ID                uuid.UUID       `json:"id" db:"id"`
	WorkspaceID       uuid.UUID       `json:"workspace_id" db:"workspace_id"`
	ToolID            uuid.UUID       `json:"tool_id" db:"tool_id"`
	ToolVersion       int             `json:"tool_version" db:"tool_version"`
	ToolName          string          `json:"tool_name" db:"tool_name"`
	Inputs            json.RawMessage `json:"inputs" db:"inputs"`
	RequestedByUserID *uuid.UUID      `json:"requested_by_user_id" db:"requested_by_user_id"`
	ConversationID    *uuid.UUID      `json:"conversation_id" db:"conversation_id"`
	ToolCallID        *uuid.UUID      `json:"tool_call_id" db:"tool_call_id"`
	ApprovalID        *uuid.UUID      `json:"approval_id" db:"approval_id"`
	MachineID         *uuid.UUID      `json:"machine_id" db:"machine_id"`
	IssueID           *uuid.UUID      `json:"issue_id" db:"issue_id"`
	TaskID            *uuid.UUID      `json:"task_id" db:"task_id"`
	AgentRunID        *uuid.UUID      `json:"-" db:"agent_run_id"`
	WorkingDirectory  string          `json:"working_directory" db:"working_directory"`
	TimeoutSeconds    int             `json:"timeout_seconds" db:"timeout_seconds"`
	// Test runs are an admin's test-run from Settings; they may run a draft.
	Test            bool       `json:"test" db:"test"`
	Status          string     `json:"status" db:"status"`
	ExitCode        *int       `json:"exit_code" db:"exit_code"`
	Output          string     `json:"output" db:"output"`
	OutputTruncated bool       `json:"output_truncated" db:"output_truncated"`
	Error           *string    `json:"error" db:"error"`
	CreatedAt       time.Time  `json:"created_at" db:"created_at"`
	StartedAt       *time.Time `json:"started_at" db:"started_at"`
	FinishedAt      *time.Time `json:"finished_at" db:"finished_at"`
	DurationMS      *int64     `json:"duration_ms" db:"duration_ms"`
}

// AssistantToolRunExecution is what the machine manager needs to run a tool:
// the run, its version's script and interpreter, and the attempt's redaction
// snapshot when the run uses a task's checkout.
type AssistantToolRunExecution struct {
	Run                    AssistantToolRun
	Script                 string
	Interpreter            string
	EncryptedOutputSecrets *string
}

// CustomToolRunPayload binds a run_custom_tool approval to the tool version,
// the validated inputs and the machine shown to the user.
type CustomToolRunPayload struct {
	Identifier  string          `json:"identifier"`
	Title       string          `json:"title"`
	ToolID      uuid.UUID       `json:"tool_id"`
	ToolName    string          `json:"tool_name"`
	Version     int             `json:"version"`
	Description string          `json:"description"`
	ReadOnly    bool            `json:"read_only"`
	Interpreter string          `json:"interpreter"`
	Inputs      json.RawMessage `json:"inputs"`
	TaskID      *uuid.UUID      `json:"task_id,omitempty"`
	Generation  int64           `json:"generation,omitempty"`
	MachineID   *uuid.UUID      `json:"machine_id,omitempty"`
}

const (
	AssistantToolRunApproval = "approval"
	AssistantToolRunAuto     = "auto"

	// CustomToolPrefix namespaces custom tools for the model, so they can never
	// shadow a built-in tool.
	CustomToolPrefix          = "custom_"
	CustomToolScriptBytes     = 64 << 10
	CustomToolParametersBytes = 16 << 10
	CustomToolInputBytes      = 16 << 10
	CustomToolMaxProperties   = 20
	CustomToolMaxStringLength = 8192
	// CustomToolDefaultStringLength caps a string parameter without maxLength.
	CustomToolDefaultStringLength = 1024
	CustomToolMaxEnumValues       = 50
	CustomToolDefaultTimeout      = 600
	CustomToolMinTimeout          = 10
	CustomToolMaxTimeout          = 1800
	// CustomToolOutputChars is the stored tail of a run's output.
	CustomToolOutputChars = 64 << 10
)

// CustomToolInterpreters are the interpreters every developer image (and so
// every check container) provides.
var CustomToolInterpreters = []string{"sh", "bash", "node", "python3"}

var (
	customToolName     = regexp.MustCompile(`^[a-z][a-z0-9_]{0,47}$`)
	customToolArgument = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)
)

// ValidCustomToolName reports a snake_case tool name of at most 48 characters.
func ValidCustomToolName(name string) bool {
	return customToolName.MatchString(name)
}

// ToolParameter is one validated input of a custom tool.
type ToolParameter struct {
	Name        string
	Type        string
	Description string
	Enum        []string
	MinLength   *int
	MaxLength   *int
	Minimum     *float64
	Maximum     *float64
	Required    bool
}

// ToolSchema is a custom tool's parameter schema: an object of string,
// number, integer, boolean and enum properties, sorted by name.
type ToolSchema struct {
	Properties []ToolParameter
}

// ErrToolSchema is a user-safe schema or input validation failure.
type ErrToolSchema struct{ Message string }

func (e *ErrToolSchema) Error() string { return e.Message }

func schemaError(format string, args ...any) error {
	return &ErrToolSchema{Message: fmt.Sprintf(format, args...)}
}

// strictObject decodes one JSON object, rejecting duplicate keys, keys outside
// allowed and trailing data.
func strictObject(raw []byte, what string, allowed ...string) (map[string]json.RawMessage, []string, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, nil, schemaError("%s must be a JSON object", what)
	}
	fields := map[string]json.RawMessage{}
	order := []string{}
	for decoder.More() {
		token, err = decoder.Token()
		if err != nil {
			return nil, nil, schemaError("%s must be a JSON object", what)
		}
		key, _ := token.(string)
		if _, seen := fields[key]; seen {
			return nil, nil, schemaError("%s repeats %q", what, key)
		}
		if allowed != nil && !slices.Contains(allowed, key) {
			return nil, nil, schemaError("%s does not support %q", what, key)
		}
		var value json.RawMessage
		if err = decoder.Decode(&value); err != nil {
			return nil, nil, schemaError("%s must be a JSON object", what)
		}
		fields[key] = value
		order = append(order, key)
	}
	if _, err = decoder.Token(); err != nil {
		return nil, nil, schemaError("%s must be a JSON object", what)
	}
	if _, err = decoder.Token(); err == nil {
		return nil, nil, schemaError("%s must be a single JSON object", what)
	}
	return fields, order, nil
}

func decodeExact(raw json.RawMessage, target any) bool {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(target) != nil {
		return false
	}
	_, err := decoder.Token()
	return err != nil
}

// jsonNumber reads a JSON number literal; a quoted number is not one.
func jsonNumber(raw json.RawMessage) (json.Number, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || (raw[0] != '-' && (raw[0] < '0' || raw[0] > '9')) {
		return "", false
	}
	var n json.Number
	return n, decodeExact(raw, &n)
}

// ParseToolSchema validates a parameter schema strictly and returns it with
// its canonical JSON, the form stored and offered to the model.
func ParseToolSchema(raw json.RawMessage) (*ToolSchema, json.RawMessage, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = json.RawMessage(`{"type":"object","properties":{}}`)
	}
	if len(raw) > CustomToolParametersBytes {
		return nil, nil, schemaError("parameters must be at most %d bytes", CustomToolParametersBytes)
	}
	top, _, err := strictObject(raw, "parameters", "type", "properties", "required", "additionalProperties")
	if err != nil {
		return nil, nil, err
	}
	var kind string
	if !decodeExact(top["type"], &kind) || kind != "object" {
		return nil, nil, schemaError(`parameters must have "type": "object"`)
	}
	if extra, ok := top["additionalProperties"]; ok {
		var allowed bool
		if !decodeExact(extra, &allowed) || allowed {
			return nil, nil, schemaError(`parameters must not allow additional properties`)
		}
	}
	properties := map[string]json.RawMessage{}
	names := []string{}
	if raw, ok := top["properties"]; ok {
		if properties, names, err = strictObject(raw, "properties"); err != nil {
			return nil, nil, err
		}
	}
	if len(names) > CustomToolMaxProperties {
		return nil, nil, schemaError("a tool takes at most %d parameters", CustomToolMaxProperties)
	}
	required := []string{}
	if raw, ok := top["required"]; ok && !decodeExact(raw, &required) {
		return nil, nil, schemaError("required must be a list of parameter names")
	}
	schema := &ToolSchema{}
	for _, name := range names {
		if !customToolArgument.MatchString(name) {
			return nil, nil, schemaError("parameter %q must be snake_case: lowercase letters, digits and underscores, starting with a letter, at most 40 characters", truncateText(name, 40))
		}
		parameter, err := parseToolParameter(name, properties[name])
		if err != nil {
			return nil, nil, err
		}
		schema.Properties = append(schema.Properties, parameter)
	}
	sort.Slice(schema.Properties, func(i, j int) bool { return schema.Properties[i].Name < schema.Properties[j].Name })
	seen := map[string]bool{}
	for _, name := range required {
		if seen[name] {
			return nil, nil, schemaError("required repeats %q", truncateText(name, 40))
		}
		seen[name] = true
		i := slices.IndexFunc(schema.Properties, func(p ToolParameter) bool { return p.Name == name })
		if i < 0 {
			return nil, nil, schemaError("required names %q, which is not a parameter", truncateText(name, 40))
		}
		schema.Properties[i].Required = true
	}
	canonical, err := schema.JSON()
	if err != nil {
		return nil, nil, err
	}
	if len(canonical) > CustomToolParametersBytes {
		return nil, nil, schemaError("parameters must be at most %d bytes", CustomToolParametersBytes)
	}
	return schema, canonical, nil
}

func parseToolParameter(name string, raw json.RawMessage) (ToolParameter, error) {
	fields, _, err := strictObject(raw, "parameter "+name, "type", "description", "enum", "minLength", "maxLength", "minimum", "maximum")
	if err != nil {
		return ToolParameter{}, err
	}
	p := ToolParameter{Name: name}
	if value, ok := fields["type"]; ok && !decodeExact(value, &p.Type) {
		return p, schemaError("parameter %s has an invalid type", name)
	}
	if value, ok := fields["description"]; ok {
		if !decodeExact(value, &p.Description) || utf8.RuneCountInString(p.Description) > 500 || strings.ContainsRune(p.Description, 0) {
			return p, schemaError("parameter %s needs a description of at most 500 characters", name)
		}
	}
	allow := func(keys ...string) error {
		for key := range fields {
			if key != "type" && key != "description" && !slices.Contains(keys, key) {
				return schemaError("parameter %s of type %s does not support %q", name, p.Type, key)
			}
		}
		return nil
	}
	if value, ok := fields["enum"]; ok {
		if p.Type == "" {
			p.Type = "string"
		}
		if p.Type != "string" {
			return p, schemaError("parameter %s: enum values must be strings", name)
		}
		if err = allow("enum"); err != nil {
			return p, err
		}
		if !decodeExact(value, &p.Enum) || len(p.Enum) == 0 || len(p.Enum) > CustomToolMaxEnumValues {
			return p, schemaError("parameter %s needs 1 to %d enum strings", name, CustomToolMaxEnumValues)
		}
		seen := map[string]bool{}
		for _, item := range p.Enum {
			if seen[item] || item == "" || utf8.RuneCountInString(item) > 200 || strings.ContainsRune(item, 0) {
				return p, schemaError("parameter %s needs distinct enum strings of 1 to 200 characters", name)
			}
			seen[item] = true
		}
		return p, nil
	}
	switch p.Type {
	case "string":
		if err = allow("minLength", "maxLength"); err != nil {
			return p, err
		}
		for key, target := range map[string]**int{"minLength": &p.MinLength, "maxLength": &p.MaxLength} {
			if value, ok := fields[key]; ok {
				n, ok := jsonNumber(value)
				parsed, convErr := strconv.ParseInt(n.String(), 10, 32)
				if !ok || convErr != nil || parsed < 0 || parsed > CustomToolMaxStringLength {
					return p, schemaError("parameter %s: %s must be a whole number from 0 to %d", name, key, CustomToolMaxStringLength)
				}
				v := int(parsed)
				*target = &v
			}
		}
		if p.MaxLength == nil {
			v := CustomToolDefaultStringLength
			p.MaxLength = &v
		}
		if *p.MaxLength < 1 || (p.MinLength != nil && *p.MinLength > *p.MaxLength) {
			return p, schemaError("parameter %s: maxLength must be at least 1 and at least minLength", name)
		}
	case "number", "integer":
		if err = allow("minimum", "maximum"); err != nil {
			return p, err
		}
		for key, target := range map[string]**float64{"minimum": &p.Minimum, "maximum": &p.Maximum} {
			if value, ok := fields[key]; ok {
				n, ok := jsonNumber(value)
				if !ok {
					return p, schemaError("parameter %s: %s must be a number", name, key)
				}
				f, convErr := n.Float64()
				if convErr != nil || math.IsInf(f, 0) || math.IsNaN(f) || (p.Type == "integer" && (f != math.Trunc(f) || math.Abs(f) > maxSafeInteger)) {
					return p, schemaError("parameter %s: %s must be a finite %s", name, key, p.Type)
				}
				*target = &f
			}
		}
		if p.Minimum != nil && p.Maximum != nil && *p.Minimum > *p.Maximum {
			return p, schemaError("parameter %s: minimum must not exceed maximum", name)
		}
	case "boolean":
		if err = allow(); err != nil {
			return p, err
		}
	default:
		return p, schemaError("parameter %s must be of type string, number, integer or boolean, or an enum of strings", name)
	}
	return p, nil
}

// maxSafeInteger is the largest integer every JSON consumer reads exactly.
const maxSafeInteger = 1 << 53

type canonicalToolProperty struct {
	Type        string   `json:"type"`
	Description string   `json:"description,omitempty"`
	Enum        []string `json:"enum,omitempty"`
	MinLength   *int     `json:"minLength,omitempty"`
	MaxLength   *int     `json:"maxLength,omitempty"`
	Minimum     *float64 `json:"minimum,omitempty"`
	Maximum     *float64 `json:"maximum,omitempty"`
}

// JSON is the canonical schema: every bound explicit, additional properties off.
func (s *ToolSchema) JSON() (json.RawMessage, error) {
	properties := map[string]canonicalToolProperty{}
	required := []string{}
	for _, p := range s.Properties {
		properties[p.Name] = canonicalToolProperty{Type: p.Type, Description: p.Description, Enum: p.Enum, MinLength: p.MinLength, MaxLength: p.MaxLength, Minimum: p.Minimum, Maximum: p.Maximum}
		if p.Required {
			required = append(required, p.Name)
		}
	}
	return json.Marshal(struct {
		Type                 string                           `json:"type"`
		Properties           map[string]canonicalToolProperty `json:"properties"`
		Required             []string                         `json:"required"`
		AdditionalProperties bool                             `json:"additionalProperties"`
	}{"object", properties, required, false})
}

// ValidateInputs checks arguments against the schema and returns them typed
// (string, int64, float64 or bool) with their compact canonical JSON.
func (s *ToolSchema) ValidateInputs(raw json.RawMessage) (map[string]any, json.RawMessage, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = json.RawMessage(`{}`)
	}
	if len(raw) > CustomToolInputBytes {
		return nil, nil, schemaError("inputs must be at most %d bytes", CustomToolInputBytes)
	}
	fields, _, err := strictObject(raw, "inputs")
	if err != nil {
		return nil, nil, err
	}
	values := map[string]any{}
	for key := range fields {
		if !slices.ContainsFunc(s.Properties, func(p ToolParameter) bool { return p.Name == key }) {
			return nil, nil, schemaError("unknown input %q", truncateText(key, 40))
		}
	}
	for _, p := range s.Properties {
		value, ok := fields[p.Name]
		if !ok {
			if p.Required {
				return nil, nil, schemaError("input %s is required", p.Name)
			}
			continue
		}
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, nil, schemaError("input %s must not be null", p.Name)
		}
		switch p.Type {
		case "string":
			var text string
			if !decodeExact(value, &text) {
				return nil, nil, schemaError("input %s must be a string", p.Name)
			}
			if strings.ContainsRune(text, 0) || !utf8.ValidString(text) {
				return nil, nil, schemaError("input %s must be text without NUL characters", p.Name)
			}
			if p.Enum != nil && !slices.Contains(p.Enum, text) {
				return nil, nil, schemaError("input %s must be one of %s", p.Name, strings.Join(p.Enum, ", "))
			}
			runes := utf8.RuneCountInString(text)
			if p.MaxLength != nil && runes > *p.MaxLength {
				return nil, nil, schemaError("input %s must be at most %d characters", p.Name, *p.MaxLength)
			}
			if p.MinLength != nil && runes < *p.MinLength {
				return nil, nil, schemaError("input %s must be at least %d characters", p.Name, *p.MinLength)
			}
			values[p.Name] = text
		case "boolean":
			var flag bool
			if !decodeExact(value, &flag) {
				return nil, nil, schemaError("input %s must be true or false", p.Name)
			}
			values[p.Name] = flag
		case "number", "integer":
			n, ok := jsonNumber(value)
			if !ok {
				return nil, nil, schemaError("input %s must be a number", p.Name)
			}
			f, convErr := n.Float64()
			if convErr != nil || math.IsInf(f, 0) || math.IsNaN(f) {
				return nil, nil, schemaError("input %s must be a finite number", p.Name)
			}
			if (p.Minimum != nil && f < *p.Minimum) || (p.Maximum != nil && f > *p.Maximum) {
				return nil, nil, schemaError("input %s is out of range", p.Name)
			}
			if p.Type == "integer" {
				if f != math.Trunc(f) || math.Abs(f) > maxSafeInteger {
					return nil, nil, schemaError("input %s must be a whole number", p.Name)
				}
				values[p.Name] = int64(f)
			} else {
				values[p.Name] = f
			}
		}
	}
	canonical, err := json.Marshal(values)
	if err != nil {
		return nil, nil, err
	}
	if len(canonical) > CustomToolInputBytes {
		return nil, nil, schemaError("inputs must be at most %d bytes", CustomToolInputBytes)
	}
	return values, canonical, nil
}

// ToolArgumentEnvironment renders validated inputs as KUAYLE_ARG_<NAME>
// variables, sorted by name. Values are data: the script reads them, nothing
// interpolates them into a command line.
func ToolArgumentEnvironment(values map[string]any) ([]string, error) {
	names := make([]string, 0, len(values))
	for name := range values {
		if !customToolArgument.MatchString(name) {
			return nil, errors.New("invalid tool argument name")
		}
		names = append(names, name)
	}
	sort.Strings(names)
	env := make([]string, 0, len(names))
	for _, name := range names {
		var text string
		switch value := values[name].(type) {
		case string:
			text = value
		case bool:
			text = strconv.FormatBool(value)
		case int64:
			text = strconv.FormatInt(value, 10)
		case float64:
			text = strconv.FormatFloat(value, 'g', -1, 64)
		case json.Number:
			text = value.String()
		default:
			return nil, errors.New("invalid tool argument value")
		}
		if strings.ContainsRune(text, 0) {
			return nil, errors.New("invalid tool argument value")
		}
		env = append(env, "KUAYLE_ARG_"+strings.ToUpper(name)+"="+text)
	}
	return env, nil
}

// DecodeToolInputs reads stored canonical inputs back for execution. Numbers
// stay json.Number so integers keep their exact text.
func DecodeToolInputs(raw json.RawMessage) (map[string]any, error) {
	values := map[string]any{}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&values); err != nil {
		return nil, err
	}
	for name, value := range values {
		switch value.(type) {
		case string, bool, json.Number:
		default:
			return nil, fmt.Errorf("invalid stored input %q", name)
		}
	}
	return values, nil
}

func truncateText(value string, limit int) string {
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	return string([]rune(value)[:limit])
}
