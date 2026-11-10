package openapi

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const regenerate = "run `go run ./cmd/openapi` in BE and commit both generated files"

// The published document is generated from the DTOs by reflection. Changing a
// public DTO (a field, its JSON name, its validation) or the operation
// registry changes Build's output, so this fails until the checked-in files
// are regenerated and the change to the public contract shows up in review.
func TestCheckedInDocumentMatchesRoutesAndDTOs(t *testing.T) {
	built, err := Build()
	require.NoError(t, err)
	require.True(t, bytes.Equal(built, CheckedIn()), "internal/openapi/openapi.json is out of date: %s", regenerate)

	const websiteCopy = "../../../WEB/src/lib/data/openapi.json"
	site, err := os.ReadFile(websiteCopy)
	if os.IsNotExist(err) {
		t.Skip("WEB is not part of this checkout")
	}
	require.NoError(t, err)
	require.True(t, bytes.Equal(built, site), "%s is out of date: %s", websiteCopy, regenerate)
}

func TestSchemaFollowsStructTags(t *testing.T) {
	type nested struct {
		ID string `json:"id"`
	}
	type embedded struct {
		Page int `json:"page"`
	}
	type payload struct {
		embedded
		Title    string     `json:"title" validate:"required,min=1,max=500"`
		Status   *string    `json:"status" validate:"omitempty,oneof=open closed"`
		Labels   []string   `json:"label_ids" validate:"omitempty,dive,uuid"`
		Parent   *nested    `json:"parent,omitempty"`
		Due      *time.Time `json:"due_date"`
		Internal string     `json:"-"`
	}

	asRequest := newSchemas()
	asRequest.of(reflect.TypeFor[payload](), request)
	got := asRequest.components["payload"].(object)
	require.Equal(t, []string{"title"}, got["required"], "a request requires only fields validated as required")
	properties := got["properties"].(object)
	require.Equal(t, object{"type": "string", "minLength": 1, "maxLength": 500}, properties["title"])
	require.Equal(t, object{"type": []string{"string", "null"}, "enum": []string{"open", "closed"}}, properties["status"])
	require.Equal(t, object{"type": "array", "items": object{"type": "string", "format": "uuid"}}, properties["label_ids"])
	require.Contains(t, properties, "page", "embedded structs share the JSON object")
	require.NotContains(t, properties, "Internal")
	require.NotContains(t, properties, "-")

	asResponse := newSchemas()
	asResponse.of(reflect.TypeFor[payload](), response)
	got = asResponse.components["payload"].(object)
	require.Equal(t, []string{"page", "title", "status", "label_ids", "due_date"}, got["required"], "a response requires every field without omitempty")
	properties = got["properties"].(object)
	require.Equal(t, object{"$ref": "#/components/schemas/nested"}, properties["parent"], "an omitted field is never null")
	require.Equal(t, object{"type": []string{"string", "null"}, "format": "date-time"}, properties["due_date"])
}

func TestDocumentIsStampedWithTheRelease(t *testing.T) {
	stamped, err := Document("v1.4.2")
	require.NoError(t, err)
	var doc struct {
		OpenAPI string `json:"openapi"`
		Info    struct {
			Version string `json:"version"`
			Title   string `json:"title"`
		} `json:"info"`
		Paths map[string]any `json:"paths"`
	}
	require.NoError(t, json.Unmarshal(stamped, &doc))
	require.Equal(t, "3.1.0", doc.OpenAPI)
	require.Equal(t, "1.4.2", doc.Info.Version)
	require.Equal(t, "Kuayle API", doc.Info.Title)
	require.NotEmpty(t, doc.Paths)
	require.Contains(t, string(CheckedIn()), `"version": "`+PlaceholderVersion+`"`)
}

// Generated clients need every reference to resolve, unique operation IDs
// and declared path parameters.
func TestDocumentIsInternallyConsistent(t *testing.T) {
	var doc map[string]any
	require.NoError(t, json.Unmarshal(CheckedIn(), &doc))
	components := doc["components"].(map[string]any)

	var walk func(node any)
	walk = func(node any) {
		switch value := node.(type) {
		case map[string]any:
			if ref, ok := value["$ref"].(string); ok {
				parts := strings.Split(strings.TrimPrefix(ref, "#/components/"), "/")
				require.Len(t, parts, 2, ref)
				require.Contains(t, components[parts[0]], parts[1], "unresolved reference %s", ref)
			}
			for _, child := range value {
				walk(child)
			}
		case []any:
			for _, child := range value {
				walk(child)
			}
		}
	}
	walk(doc)

	placeholder := regexp.MustCompile(`\{([A-Za-z]+)\}`)
	ids := map[string]bool{}
	for path, item := range doc["paths"].(map[string]any) {
		for method, raw := range item.(map[string]any) {
			op := raw.(map[string]any)
			id := op["operationId"].(string)
			require.False(t, ids[id], "duplicate operationId %s", id)
			ids[id] = true
			require.Equal(t, "stable", op["x-kuayle-stability"], "%s %s", method, path)
			require.NotEmpty(t, op["x-kuayle-scopes"], "%s %s names no scope", method, path)

			declared := map[string]bool{}
			parameters, _ := op["parameters"].([]any)
			for _, parameter := range parameters {
				p := parameter.(map[string]any)
				if p["in"] == "path" {
					declared[p["name"].(string)] = true
				}
			}
			for _, match := range placeholder.FindAllStringSubmatch(path, -1) {
				require.True(t, declared[match[1]], "%s %s does not declare {%s}", method, path, match[1])
			}
		}
	}
	require.Len(t, ids, len(Operations()))
}

// The guide is the only place the reference explains auth, pagination,
// errors, rate limits and webhook signatures; the page on kuayle.com renders
// these sections by ID.
func TestGuideCoversTheDocumentedTopics(t *testing.T) {
	var ids []string
	for _, section := range Guide(Operations()) {
		ids = append(ids, section.ID)
		require.NotEmpty(t, section.Paragraphs, section.ID)
	}
	require.Equal(t, []string{"authentication", "scopes", "pagination", "errors", "rate-limits", "webhooks", "stability"}, ids)
}
