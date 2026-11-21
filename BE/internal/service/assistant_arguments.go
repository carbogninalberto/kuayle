package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"sync"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
)

// Compile only application-owned schemas. Workspace tools validate their
// inputs with domain.ToolSchema after checking permission to run the tool.
var assistantArgumentSchemas = sync.OnceValue(func() map[string]*jsonschema.Schema {
	compiler := jsonschema.NewCompiler()
	compiler.AssertFormat()
	schemas := map[string]*jsonschema.Schema{}
	row := assistantTool{name: "bulk-row", parameters: bulkRowParameters}
	for _, tool := range append(assistantTools(), attachImagesTool, customToolEnvelope, row) {
		if tool.name == bulkUpdateAction {
			// Validate each row at approval execution, so one bad row does
			// not prevent the other updates or break a stored approval.
			tool.parameters = bulkUpdateParameters("{}")
		}
		doc, err := jsonschema.UnmarshalJSON(strings.NewReader(tool.parameters))
		if err != nil {
			panic(err)
		}
		url := "urn:kuayle:tool:" + tool.name
		if err := compiler.AddResource(url, doc); err != nil {
			panic(err)
		}
		schemas[tool.name] = compiler.MustCompile(url)
	}
	return schemas
})

func (tool assistantTool) validateArguments(raw string) error {
	return validateAssistantArguments(tool.name, assistantArgumentSchemas()[tool.name], raw)
}

func validateAssistantArguments(name string, schema *jsonschema.Schema, raw string) error {
	if strings.TrimSpace(raw) == "" {
		raw = "{}"
	}
	var input any
	if err := json.Unmarshal([]byte(raw), &input); err != nil {
		return argumentFailure(name, schema, "give a single JSON object")
	}
	err := schema.Validate(input)
	if err == nil {
		return nil
	}
	var issue *jsonschema.ValidationError
	if !errors.As(err, &issue) {
		return err
	}
	for len(issue.Causes) > 0 {
		if _, ok := issue.ErrorKind.(*kind.OneOf); ok {
			break
		}
		// Report unknown fields before missing ones so a misspelling can
		// be corrected in one retry.
		children := issue.Causes
		issue = children[0]
		for _, child := range children {
			if _, ok := child.ErrorKind.(*kind.AdditionalProperties); ok {
				issue = child
				break
			}
		}
	}
	parent := schema
	current := schema
	for _, field := range issue.InstanceLocation {
		if current.Items2020 != nil {
			current = current.Items2020
		} else if next := current.Properties[field]; next != nil {
			parent, current = current, next
		}
	}
	if current.Properties != nil {
		parent = current
	}
	field := strings.Join(issue.InstanceLocation, ".")
	if field == "" {
		field = "arguments"
	}
	reason := ""
	switch k := issue.ErrorKind.(type) {
	case *kind.AdditionalProperties:
		key := domain.SafeToolArgumentName(slices.Min(k.Properties))
		reason = fmt.Sprintf("unknown argument %s", key)
		if name == "list_agent_work" && key == "identifier" {
			reason += "; use get_agent_work with identifier for one issue"
		}
	case *kind.Required:
		reason = fmt.Sprintf("%s is required", strings.Join(k.Missing, ", "))
		if name == "load_skill" {
			reason += "; use a skill name from the system prompt"
		}
	case *kind.OneOf:
		reason = "give exactly one of the alternatives in the schema"
		if name == "repo_glob" {
			reason = "give either path or pattern"
		}
	case *kind.Type:
		reason = field + " must be " + strings.Join(k.Want, " or ")
	case *kind.Enum:
		choices := make([]string, 0, len(k.Want))
		for _, value := range k.Want {
			choices = append(choices, fmt.Sprint(value))
		}
		reason = field + " must be one of " + strings.Join(choices, ", ")
	case *kind.Minimum:
		reason = fmt.Sprintf("%s must be at least %s", field, k.Want.RatString())
	case *kind.Maximum:
		reason = fmt.Sprintf("%s must be at most %s", field, k.Want.RatString())
	case *kind.MinLength:
		reason = fmt.Sprintf("%s needs at least %d characters", field, k.Want)
	case *kind.MaxLength:
		reason = fmt.Sprintf("%s allows at most %d characters", field, k.Want)
	case *kind.MinItems:
		reason = fmt.Sprintf("%s needs at least %d items", field, k.Want)
	case *kind.MaxItems:
		reason = fmt.Sprintf("%s allows at most %d items", field, k.Want)
	case *kind.MinProperties:
		reason = fmt.Sprintf("%s needs at least %d fields", field, k.Want)
	case *kind.UniqueItems:
		reason = field + " must contain distinct items"
	case *kind.Format:
		reason = field + " must be a valid " + k.Want
	default:
		reason = field + " does not match its schema"
	}
	if len(issue.InstanceLocation) > 0 && current.Properties != nil {
		reason = field + ": " + reason
	}
	return argumentFailure(name, parent, reason)
}

// JSON Schema accepts integral decimals and unbounded integers. A Go int
// needs an integer literal in its machine range. Describe that last binding
// error without copying the JSON value from the decoder's error message.
type toolBindingError struct{ mismatch *json.UnmarshalTypeError }

func (e *toolBindingError) Error() string {
	field := e.mismatch.Field
	switch e.mismatch.Type.Kind() {
	case reflect.Int, reflect.Int64:
		return field + " needs an integer literal in the supported range (no decimal point or exponent)"
	default:
		return fmt.Sprintf("%s must be %s", field, e.mismatch.Type)
	}
}

func toolArgumentErrorText(name string, schema *jsonschema.Schema, err error) string {
	var binding *toolBindingError
	if errors.As(err, &binding) {
		return argumentFailure(name, schema, binding.Error()).Error()
	}
	return toolErrorText(err)
}

func argumentFailure(name string, schema *jsonschema.Schema, reason string) error {
	accepted := strings.Join(slices.Sorted(maps.Keys(schema.Properties)), ", ")
	if accepted == "" {
		accepted = "none"
	}
	return toolFailure("invalid arguments for %s: %s. Accepted arguments: %s", name, reason, accepted)
}
