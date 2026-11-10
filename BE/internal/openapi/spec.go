// Package openapi builds the public OpenAPI 3.1 description of the HTTP API.
//
// The document is generated from the operation registry in operations.go
// and, by reflection, from the request and response DTOs. The generated file
// is checked in (openapi.json here and a copy for the marketing site), and
// tests fail when either the DTOs or the registered routes drift from it.
// Regenerate with: go run ./cmd/openapi (from BE).
package openapi

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/kuayle/kuayle-backend/internal/dto"
)

// PlaceholderVersion is info.version in the checked-in file; Document
// replaces it with the release the binary was built from.
const PlaceholderVersion = "0.0.0-dev"

//go:embed openapi.json
var checkedIn []byte

// CheckedIn returns the generated document as committed to the repository.
func CheckedIn() []byte { return checkedIn }

// Document returns the checked-in document stamped with the given release.
func Document(version string) ([]byte, error) {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(checkedIn, &doc); err != nil {
		return nil, err
	}
	var info map[string]json.RawMessage
	if err := json.Unmarshal(doc["info"], &info); err != nil {
		return nil, err
	}
	stamped, err := json.Marshal(strings.TrimPrefix(version, "v"))
	if err != nil {
		return nil, err
	}
	info["version"] = stamped
	if doc["info"], err = json.Marshal(info); err != nil {
		return nil, err
	}
	return json.Marshal(doc)
}

var pathParameters = map[string]object{
	"slug":       {"description": "Workspace slug, as in the app's URLs.", "schema": object{"type": "string"}},
	"teamId":     {"description": "Team ID.", "schema": object{"type": "string", "format": "uuid"}},
	"userId":     {"description": "User ID.", "schema": object{"type": "string", "format": "uuid"}},
	"statusId":   {"description": "Status ID.", "schema": object{"type": "string", "format": "uuid"}},
	"cycleId":    {"description": "Cycle ID.", "schema": object{"type": "string", "format": "uuid"}},
	"identifier": {"description": "Issue identifier: the team key and issue number, for example ENG-42.", "schema": object{"type": "string"}},
	"commentId":  {"description": "Comment ID.", "schema": object{"type": "string", "format": "uuid"}},
	"relationId": {"description": "Relation ID.", "schema": object{"type": "string", "format": "uuid"}},
	"pageId":     {"description": "Wiki page ID.", "schema": object{"type": "string", "format": "uuid"}},
	"id":         {"description": "Resource ID.", "schema": object{"type": "string", "format": "uuid"}},
}

var tagDescriptions = [][2]string{
	{"Account", "The user a token belongs to."},
	{"Workspaces", "Workspaces the user belongs to. Most paths start with a workspace slug."},
	{"Members", "People in a workspace and their roles: owner, admin, member or guest."},
	{"Teams", "Teams own issues, statuses and cycles. Private teams are visible to their members, owners and admins."},
	{"Statuses", "A team's workflow statuses, each in one category: backlog, unstarted, started, completed or cancelled."},
	{"Cycles", "Date-bound iterations of a team. Tokens read cycles; they are changed in the app."},
	{"Issues", "Issues are addressed by identifier, for example ENG-42."},
	{"Comments", "Threaded comments on an issue."},
	{"Issue relations", "Links between issues: related, blocked_by, blocking or duplicate."},
	{"Labels", "Workspace and project labels."},
	{"Projects", "Projects group issues across teams."},
	{"Views", "Saved issue filters. Tokens read views; they are changed in the app."},
	{"Wiki", "Pages of a team's or a project's wiki, with version history, a tree, a dated registry and publication."},
	{"Webhooks", "Signed HTTP callbacks for a workspace. See Webhooks and signature verification."},
}

var routeParam = regexp.MustCompile(`:([A-Za-z]+)`)

// Build generates the document from the registry and the DTOs.
func Build() ([]byte, error) {
	ops := Operations()
	guide := Guide(ops)
	schemas := newSchemas()
	errorRef := schemas.of(reflect.TypeFor[dto.ErrorResponse](), response)

	errorResponse := func(description string) object {
		return object{"description": description, "content": object{"application/json": object{"schema": errorRef}}}
	}
	responses := object{
		"BadRequest":      errorResponse("The request is malformed or breaks a validation rule (BAD_REQUEST or VALIDATION_ERROR)."),
		"Unauthorized":    errorResponse("The token is missing, unknown, expired or revoked (UNAUTHORIZED)."),
		"Forbidden":       errorResponse("The token's scopes or workspace limit, or the user's role, do not allow the request (FORBIDDEN)."),
		"NotFound":        errorResponse("The resource does not exist or the user cannot see it (NOT_FOUND)."),
		"TooManyRequests": errorResponse("Too many requests (RATE_LIMITED). Retry after the number of seconds in the Retry-After header."),
	}
	responseRef := func(name string) object { return object{"$ref": "#/components/responses/" + name} }

	paths := object{}
	seenIDs := map[string]bool{}
	for _, op := range ops {
		if op.ID == "" || seenIDs[op.ID] {
			return nil, fmt.Errorf("openapi: operation %s %s needs a unique ID (got %q)", op.Method, op.Path, op.ID)
		}
		seenIDs[op.ID] = true

		var parameters []object
		for _, match := range routeParam.FindAllStringSubmatch(op.Path, -1) {
			known, ok := pathParameters[match[1]]
			if !ok {
				return nil, fmt.Errorf("openapi: path parameter %q of %s is not described", match[1], op.Path)
			}
			parameters = append(parameters, object{"name": match[1], "in": "path", "required": true, "description": known["description"], "schema": known["schema"]})
		}
		if op.Query != nil {
			parameters = append(parameters, queryParameters(reflect.TypeOf(op.Query), issueFilterDescriptions)...)
		}
		for _, param := range op.Params {
			schema := object{"type": param.Type}
			if param.Format != "" {
				schema["format"] = param.Format
			}
			parameters = append(parameters, object{"name": param.Name, "in": "query", "description": param.Description, "schema": schema})
		}

		scopes := append([]string{op.Scope}, op.AlsoRequires...)
		description := op.Description
		if description != "" {
			description += "\n\n"
		}
		description += "Required scope: `" + strings.Join(scopes, "` and `") + "`."

		statuses := object{
			strconv.Itoa(op.Status): object{
				"description": "Success.",
				"content":     object{"application/json": object{"schema": schemas.of(reflect.TypeOf(op.Response), response)}},
			},
			"401": responseRef("Unauthorized"),
			"403": responseRef("Forbidden"),
			"429": responseRef("TooManyRequests"),
		}
		if len(parameters) > 0 || op.Body != nil {
			statuses["400"] = responseRef("BadRequest")
		}
		if strings.Contains(op.Path, ":") {
			statuses["404"] = responseRef("NotFound")
		}
		if op.Conflict != "" {
			statuses["409"] = errorResponse(op.Conflict)
		}

		operation := object{
			"operationId":        op.ID,
			"tags":               []string{op.Tag},
			"summary":            op.Summary,
			"description":        description,
			"security":           []object{{"personalAccessToken": scopes}},
			"responses":          statuses,
			"x-kuayle-stability": "stable",
			"x-kuayle-scopes":    scopes,
		}
		if len(parameters) > 0 {
			operation["parameters"] = parameters
		}
		if op.Body != nil {
			operation["requestBody"] = object{
				"required": true,
				"content":  object{"application/json": object{"schema": schemas.of(reflect.TypeOf(op.Body), request)}},
			}
		}

		path := routeParam.ReplaceAllString(op.Path, "{$1}")
		item, _ := paths[path].(object)
		if item == nil {
			item = object{}
			paths[path] = item
		}
		item[strings.ToLower(op.Method)] = operation
	}

	tags := make([]object, 0, len(tagDescriptions))
	for _, tag := range tagDescriptions {
		tags = append(tags, object{"name": tag[0], "description": tag[1]})
	}

	doc := object{
		"openapi": "3.1.0",
		"info": object{
			"title":       "Kuayle API",
			"version":     PlaceholderVersion,
			"summary":     "Issues, comments, teams, projects, cycles, labels, views, wikis and webhooks over HTTP.",
			"description": markdown(guide),
			"license":     object{"name": "Apache 2.0", "identifier": "Apache-2.0"},
		},
		"servers": []object{{
			"url":         "{instance}",
			"description": "Your Kuayle instance. Paths already include /api.",
			"variables":   object{"instance": object{"default": "http://localhost:8080", "description": "Origin of the instance, without a trailing slash."}},
		}},
		"security": []object{{"personalAccessToken": []string{}}},
		"tags":     tags,
		"paths":    paths,
		"webhooks": object{
			"delivery": object{"post": object{
				"operationId": "receiveWebhookDelivery",
				"summary":     "A webhook delivery sent to your URL",
				"description": "Preview: the format and signature are implemented, but this release does not emit issue or comment events yet. See Webhooks and signature verification.",
				"security":    []object{},
				"parameters": []object{
					{"name": SignatureHeader, "in": "header", "required": true, "schema": object{"type": "string", "pattern": "^sha256=[0-9a-f]{64}$"},
						"description": "\"sha256=\" followed by the hex HMAC-SHA256 of the raw body, keyed with the webhook's secret."},
					{"name": EventHeader, "in": "header", "required": true, "schema": object{"type": "string"}, "description": "The event name, as in the body."},
				},
				"requestBody":        object{"required": true, "content": object{"application/json": object{"schema": schemas.of(reflect.TypeFor[dto.WebhookDelivery](), response)}}},
				"responses":          object{"2XX": object{"description": "Any response ends the delivery; it is not retried."}},
				"x-kuayle-stability": "preview",
			}},
		},
		"components": object{
			"securitySchemes": object{"personalAccessToken": object{
				"type": "http", "scheme": "bearer", "bearerFormat": "kuayle_pat_<40 characters>",
				"description": "A personal access token created under Settings, API Tokens. The names listed with an operation are the token scopes it needs.",
			}},
			"responses": responses,
			"schemas":   schemas.components,
		},
		"x-kuayle-guide": guide,
	}

	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "\t")
	if err := encoder.Encode(doc); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
