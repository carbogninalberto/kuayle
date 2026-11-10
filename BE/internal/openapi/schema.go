package openapi

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// object is one JSON object of the document. encoding/json writes map keys
// in sorted order, which keeps the generated file stable.
type object = map[string]any

// direction decides which fields a schema requires: a request requires the
// fields validated as "required", a response requires every field the server
// always writes (those without omitempty).
type direction int

const (
	request direction = iota
	response
)

var (
	timeType = reflect.TypeFor[time.Time]()
	uuidType = reflect.TypeFor[uuid.UUID]()
	rawType  = reflect.TypeFor[json.RawMessage]()
)

// schemas turns Go types into JSON Schema (2020-12, as OpenAPI 3.1 uses) by
// reflection, so the published schema of a DTO changes whenever its fields,
// JSON names or validation tags change.
type schemas struct {
	components object
	owners     map[string]reflect.Type
	names      map[use]string
}

type use struct {
	t   reflect.Type
	dir direction
}

func newSchemas() *schemas {
	return &schemas{components: object{}, owners: map[string]reflect.Type{}, names: map[use]string{}}
}

// of returns the schema for t: a $ref for structs, inline otherwise.
func (s *schemas) of(t reflect.Type, dir direction) object {
	switch {
	case t == timeType:
		return object{"type": "string", "format": "date-time"}
	case t == uuidType:
		return object{"type": "string", "format": "uuid"}
	case t == rawType:
		return object{"description": "Any JSON value."}
	}
	switch t.Kind() {
	case reflect.Pointer:
		return s.of(t.Elem(), dir)
	case reflect.String:
		return object{"type": "string"}
	case reflect.Bool:
		return object{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return object{"type": "integer"}
	case reflect.Float32, reflect.Float64:
		return object{"type": "number"}
	case reflect.Slice, reflect.Array:
		return object{"type": "array", "items": s.of(t.Elem(), dir)}
	case reflect.Map:
		return object{"type": "object", "additionalProperties": s.of(t.Elem(), dir)}
	case reflect.Interface:
		return object{"description": "Any JSON value."}
	case reflect.Struct:
		return object{"$ref": "#/components/schemas/" + s.component(t, dir)}
	}
	panic(fmt.Sprintf("openapi: unsupported type %s", t))
}

// component registers the struct t and returns its component name. A struct
// used both as a request and as a response with different required fields
// gets a second component named <Name>Input for the request.
func (s *schemas) component(t reflect.Type, dir direction) string {
	key := use{t, dir}
	if name, done := s.names[key]; done {
		return name
	}
	name := componentName(t)
	// Reserve the name first so a type that contains itself (a comment's
	// replies) refers back to it instead of recursing forever.
	s.names[key] = name
	schema := s.structSchema(t, dir)
	if existing, taken := s.components[name]; taken {
		if s.owners[name] != t {
			panic(fmt.Sprintf("openapi: %s and %s both map to component %q", s.owners[name], t, name))
		}
		if reflect.DeepEqual(existing, schema) {
			return name
		}
		if dir != request {
			panic(fmt.Sprintf("openapi: use %s as a request after its response, or give the request its own type", t))
		}
		name += "Input"
		s.names[key] = name
	}
	s.owners[name] = t
	s.components[name] = schema
	return name
}

// componentName drops the package and the "Response" suffix (IssueResponse
// becomes Issue) and names the paginated envelope after its item (IssueList).
func componentName(t reflect.Type) string {
	name := t.Name()
	if inner, ok := strings.CutPrefix(name, "ListResponse["); ok {
		inner = strings.TrimSuffix(inner, "]")
		inner = inner[strings.LastIndex(inner, ".")+1:]
		return strings.TrimSuffix(inner, "Response") + "List"
	}
	if strings.ContainsAny(name, "[]") {
		panic(fmt.Sprintf("openapi: no component name for generic type %s", name))
	}
	if name == "ErrorResponse" {
		return "Error"
	}
	return strings.TrimSuffix(name, "Response")
}

func (s *schemas) structSchema(t reflect.Type, dir direction) object {
	properties := object{}
	var required []string
	s.fields(t, dir, properties, &required)
	schema := object{"type": "object", "properties": properties}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func (s *schemas) fields(t reflect.Type, dir direction, properties object, required *[]string) {
	for i := range t.NumField() {
		field := t.Field(i)
		tag := field.Tag.Get("json")
		if field.Anonymous && tag == "" {
			s.fields(field.Type, dir, properties, required) // embedded struct: same JSON object
			continue
		}
		if !field.IsExported() || tag == "-" {
			continue
		}
		name, options, _ := strings.Cut(tag, ",")
		if name == "" {
			name = field.Name
		}
		omitEmpty := strings.Contains(options, "omitempty")
		rules := strings.Split(field.Tag.Get("validate"), ",")

		schema := s.of(field.Type, dir)
		applyRules(schema, field.Type, rules)
		// A pointer the server always writes may be null; so may an optional
		// request field. An omitted response field is never null.
		if field.Type.Kind() == reflect.Pointer && !(dir == response && omitEmpty) {
			schema = nullable(schema)
		}
		properties[name] = schema

		switch dir {
		case request:
			if rules[0] == "required" {
				*required = append(*required, name)
			}
		case response:
			if !omitEmpty {
				*required = append(*required, name)
			}
		}
	}
}

func nullable(schema object) object {
	if kind, ok := schema["type"].(string); ok {
		schema["type"] = []string{kind, "null"}
		return schema
	}
	if _, isRef := schema["$ref"]; isRef {
		return object{"anyOf": []object{schema, {"type": "null"}}}
	}
	return schema // "any JSON value" already admits null
}

// applyRules maps go-playground/validator rules to schema keywords. Rules
// after "dive" describe the items of an array.
func applyRules(schema object, t reflect.Type, rules []string) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	for i, rule := range rules {
		key, param, _ := strings.Cut(rule, "=")
		switch key {
		case "dive":
			if items, ok := schema["items"].(object); ok && t.Kind() == reflect.Slice {
				applyRules(items, t.Elem(), rules[i+1:])
			}
			return
		case "min", "max":
			number, err := strconv.Atoi(param)
			if err != nil {
				continue
			}
			schema[boundKeyword(key, t.Kind())] = number
		case "oneof":
			schema["enum"] = strings.Fields(param)
		case "uuid":
			schema["format"] = "uuid"
		case "email":
			schema["format"] = "email"
		case "url":
			schema["format"] = "uri"
		case "hexcolor":
			schema["pattern"] = "^#(?:[0-9a-fA-F]{3,4}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$"
		case "slug":
			schema["pattern"] = "^[a-z0-9]+(?:-[a-z0-9]+)*$"
		}
	}
}

func boundKeyword(bound string, kind reflect.Kind) string {
	switch kind {
	case reflect.String:
		return bound + "Length"
	case reflect.Slice, reflect.Array:
		return bound + "Items"
	}
	return bound + "imum"
}

// queryParameters lists the `query`-tagged fields of a filter struct.
func queryParameters(t reflect.Type, describe map[string]string) []object {
	var out []object
	for i := range t.NumField() {
		field := t.Field(i)
		if field.Anonymous {
			out = append(out, queryParameters(field.Type, describe)...)
			continue
		}
		name := field.Tag.Get("query")
		if name == "" {
			continue
		}
		schema := object{"type": "string"}
		if field.Type.Kind() == reflect.Int {
			schema["type"] = "integer"
		}
		applyRules(schema, field.Type, strings.Split(field.Tag.Get("validate"), ","))
		parameter := object{"name": name, "in": "query", "schema": schema}
		if text := describe[name]; text != "" {
			parameter["description"] = text
		}
		out = append(out, parameter)
	}
	return out
}
