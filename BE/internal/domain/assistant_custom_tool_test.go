package domain

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseToolSchemaAcceptsOnlyBoundedScalarObjects(t *testing.T) {
	many := []string{}
	for i := 0; i < CustomToolMaxProperties+1; i++ {
		many = append(many, `"p`+strings.Repeat("a", i+1)+`":{"type":"boolean"}`)
	}
	cases := []struct {
		name, schema, err string
	}{
		{"empty schema", ``, ""},
		{"all scalar types", `{"type":"object","properties":{"spec":{"type":"string","maxLength":200,"description":"Spec path"},"retries":{"type":"integer","minimum":0,"maximum":5},"ratio":{"type":"number","minimum":0.5},"headed":{"type":"boolean"},"browser":{"type":"string","enum":["chromium","firefox"]},"mode":{"enum":["fast","full"]}},"required":["spec"],"additionalProperties":false}`, ""},
		{"not an object type", `{"type":"array"}`, `"type": "object"`},
		{"missing type", `{"properties":{}}`, `"type": "object"`},
		{"additional properties allowed", `{"type":"object","additionalProperties":true}`, "additional properties"},
		{"unknown top-level keyword", `{"type":"object","$ref":"#/x"}`, `does not support "$ref"`},
		{"nested object", `{"type":"object","properties":{"opts":{"type":"object"}}}`, "string, number, integer or boolean"},
		{"array parameter", `{"type":"object","properties":{"list":{"type":"array","items":{"type":"string"}}}}`, `does not support "items"`},
		{"pattern is not supported", `{"type":"object","properties":{"spec":{"type":"string","pattern":".*"}}}`, `does not support "pattern"`},
		{"bounds on the wrong type", `{"type":"object","properties":{"spec":{"type":"string","minimum":1}}}`, `does not support "minimum"`},
		{"string too long", `{"type":"object","properties":{"spec":{"type":"string","maxLength":100000}}}`, "maxLength must be a whole number"},
		{"quoted bound", `{"type":"object","properties":{"spec":{"type":"string","maxLength":"10"}}}`, "maxLength must be a whole number"},
		{"inverted string bounds", `{"type":"object","properties":{"spec":{"type":"string","minLength":5,"maxLength":2}}}`, "at least minLength"},
		{"inverted number bounds", `{"type":"object","properties":{"n":{"type":"number","minimum":5,"maximum":2}}}`, "must not exceed"},
		{"fractional integer bound", `{"type":"object","properties":{"n":{"type":"integer","maximum":2.5}}}`, "finite integer"},
		{"non-string enum", `{"type":"object","properties":{"n":{"type":"integer","enum":[1,2]}}}`, "enum values must be strings"},
		{"empty enum", `{"type":"object","properties":{"n":{"enum":[]}}}`, "1 to 50 enum strings"},
		{"duplicate enum", `{"type":"object","properties":{"n":{"enum":["a","a"]}}}`, "distinct enum strings"},
		{"bad parameter name", `{"type":"object","properties":{"Spec-File":{"type":"string"}}}`, "snake_case"},
		{"duplicate parameter", `{"type":"object","properties":{"spec":{"type":"string"},"spec":{"type":"boolean"}}}`, `repeats "spec"`},
		{"required unknown", `{"type":"object","properties":{"spec":{"type":"string"}},"required":["other"]}`, "not a parameter"},
		{"required twice", `{"type":"object","properties":{"spec":{"type":"string"}},"required":["spec","spec"]}`, `required repeats`},
		{"too many parameters", `{"type":"object","properties":{` + strings.Join(many, ",") + `}}`, "at most 20 parameters"},
		{"oversized schema", `{"type":"object","properties":{"a":{"type":"string","description":"` + strings.Repeat("x", CustomToolParametersBytes) + `"}}}`, "at most"},
		{"trailing data", `{"type":"object"} {}`, "single JSON object"},
		{"not JSON", `nope`, "JSON object"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, canonical, err := ParseToolSchema(json.RawMessage(tc.schema))
			if tc.err != "" {
				require.ErrorContains(t, err, tc.err)
				var safe *ErrToolSchema
				require.ErrorAs(t, err, &safe, "schema errors are safe to show")
				return
			}
			require.NoError(t, err)
			require.True(t, json.Valid(canonical))
			_, again, err := ParseToolSchema(canonical)
			require.NoError(t, err, "the canonical form parses again")
			require.JSONEq(t, string(canonical), string(again))
		})
	}
}

func TestParseToolSchemaMakesEveryBoundExplicit(t *testing.T) {
	_, canonical, err := ParseToolSchema(json.RawMessage(`{"type":"object","properties":{"spec":{"type":"string"},"mode":{"enum":["fast","full"]}},"required":["spec"]}`))
	require.NoError(t, err)
	require.JSONEq(t, `{"type":"object","properties":{"mode":{"type":"string","enum":["fast","full"]},"spec":{"type":"string","maxLength":1024}},"required":["spec"],"additionalProperties":false}`, string(canonical))
}

func TestValidateInputsChecksTypesBoundsAndKeys(t *testing.T) {
	schema, _, err := ParseToolSchema(json.RawMessage(`{"type":"object","properties":{"spec":{"type":"string","minLength":1,"maxLength":20},"retries":{"type":"integer","minimum":0,"maximum":5},"ratio":{"type":"number","maximum":1},"headed":{"type":"boolean"},"browser":{"enum":["chromium","firefox"]}},"required":["spec"]}`))
	require.NoError(t, err)
	cases := []struct {
		name, inputs, err, canonical string
	}{
		{"valid", `{"spec":"login.spec.ts","retries":2,"ratio":0.5,"headed":true,"browser":"firefox"}`, "", `{"browser":"firefox","headed":true,"ratio":0.5,"retries":2,"spec":"login.spec.ts"}`},
		{"integer written as a whole float", `{"spec":"a","retries":3.0}`, "", `{"retries":3,"spec":"a"}`},
		{"missing required", `{"retries":1}`, "spec is required", ""},
		{"unknown input", `{"spec":"a","extra":1}`, `unknown input "extra"`, ""},
		{"null value", `{"spec":null}`, "must not be null", ""},
		{"wrong string type", `{"spec":5}`, "must be a string", ""},
		{"too long", `{"spec":"` + strings.Repeat("é", 21) + `"}`, "at most 20 characters", ""},
		{"too short", `{"spec":""}`, "at least 1 characters", ""},
		{"NUL", `{"spec":"a\u0000b"}`, "without NUL", ""},
		{"not in enum", `{"spec":"a","browser":"webkit"}`, "one of chromium, firefox", ""},
		{"fractional integer", `{"spec":"a","retries":1.5}`, "whole number", ""},
		{"out of range", `{"spec":"a","retries":6}`, "out of range", ""},
		{"quoted number", `{"spec":"a","ratio":"0.5"}`, "must be a number", ""},
		{"string boolean", `{"spec":"a","headed":"true"}`, "true or false", ""},
		{"duplicate key", `{"spec":"a","spec":"b"}`, `repeats "spec"`, ""},
		{"array", `["spec"]`, "JSON object", ""},
		{"oversized", `{"spec":"` + strings.Repeat("a", CustomToolInputBytes) + `"}`, "at most", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, canonical, err := schema.ValidateInputs(json.RawMessage(tc.inputs))
			if tc.err != "" {
				require.ErrorContains(t, err, tc.err)
				return
			}
			require.NoError(t, err)
			require.JSONEq(t, tc.canonical, string(canonical))
		})
	}
}

// Arguments become environment variables verbatim; shell syntax in them is
// only data.
func TestToolArgumentEnvironmentKeepsValuesLiteral(t *testing.T) {
	schema, _, err := ParseToolSchema(json.RawMessage(`{"type":"object","properties":{"spec":{"type":"string"},"count":{"type":"integer"},"ratio":{"type":"number"},"headed":{"type":"boolean"}}}`))
	require.NoError(t, err)
	_, canonical, err := schema.ValidateInputs(json.RawMessage(`{"spec":"; rm -rf / $(id) ` + "`id`" + ` \"quoted\"\nnext","count":9007199254740991,"ratio":0.25,"headed":false}`))
	require.NoError(t, err)
	stored, err := DecodeToolInputs(canonical)
	require.NoError(t, err)
	env, err := ToolArgumentEnvironment(stored)
	require.NoError(t, err)
	require.Equal(t, []string{
		"KUAYLE_ARG_COUNT=9007199254740991",
		"KUAYLE_ARG_HEADED=false",
		"KUAYLE_ARG_RATIO=0.25",
		"KUAYLE_ARG_SPEC=; rm -rf / $(id) `id` \"quoted\"\nnext",
	}, env)
	_, err = ToolArgumentEnvironment(map[string]any{"BAD NAME": "x"})
	require.Error(t, err)
}

func TestValidCustomToolName(t *testing.T) {
	for name, valid := range map[string]bool{"run_e2e": true, "e2e_login2": true, "a": true, "Run": false, "2fast": false, "run-e2e": false, "": false, strings.Repeat("a", 48): true, strings.Repeat("a", 49): false} {
		require.Equal(t, valid, ValidCustomToolName(name), name)
	}
}
