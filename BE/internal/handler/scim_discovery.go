package handler

import (
	"context"
	"database/sql"
	"net/http"
	"strings"

	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/labstack/echo/v5"
)

const scimCoreSchema = "urn:ietf:params:scim:schemas:core:2.0:"

type scimAttribute struct {
	Name          string          `json:"name"`
	Type          string          `json:"type"`
	MultiValued   bool            `json:"multiValued"`
	Required      bool            `json:"required"`
	CaseExact     bool            `json:"caseExact"`
	Mutability    string          `json:"mutability"`
	Returned      string          `json:"returned"`
	Uniqueness    string          `json:"uniqueness"`
	SubAttributes []scimAttribute `json:"subAttributes,omitempty"`
}

func scimOptional(name, kind string) scimAttribute {
	return scimAttribute{Name: name, Type: kind, Mutability: "readWrite", Returned: "default", Uniqueness: "none"}
}

var scimUserAttributes = []scimAttribute{
	{Name: "userName", Type: "string", Required: true, Mutability: "readWrite", Returned: "default", Uniqueness: "server"},
	scimOptional("displayName", "string"),
	scimOptional("active", "boolean"),
	{Name: "name", Type: "complex", Mutability: "readWrite", Returned: "default", Uniqueness: "none", SubAttributes: []scimAttribute{
		scimOptional("formatted", "string"), scimOptional("givenName", "string"), scimOptional("familyName", "string"), scimOptional("middleName", "string"),
	}},
	{Name: "emails", Type: "complex", MultiValued: true, Mutability: "readWrite", Returned: "default", Uniqueness: "none", SubAttributes: []scimAttribute{
		scimOptional("value", "string"), scimOptional("type", "string"), scimOptional("primary", "boolean"),
	}},
}

var scimGroupAttributes = []scimAttribute{
	{Name: "displayName", Type: "string", Required: true, Mutability: "readWrite", Returned: "default", Uniqueness: "server"},
	{Name: "members", Type: "complex", MultiValued: true, Mutability: "readWrite", Returned: "default", Uniqueness: "none", SubAttributes: []scimAttribute{
		{Name: "value", Type: "string", CaseExact: true, Mutability: "immutable", Returned: "default", Uniqueness: "none"},
	}},
}

func (h *SCIMHandler) Discovery(c *echo.Context) error {
	return h.withSCIM(c, func(_ context.Context, _ *repository.SCIMTransaction) (int, any, error) {
		path := strings.TrimPrefix(c.Path(), SCIMPath+"/")
		if path == "ServiceProviderConfig" {
			config := map[string]any{
				"schemas": []string{scimCoreSchema + path},
				"patch":   map[string]bool{"supported": true},
				"filter":  map[string]any{"supported": true, "maxResults": service.SCIMMaxPage},
				"bulk":    map[string]any{"supported": false, "maxOperations": 0, "maxPayloadSize": 0},
				"authenticationSchemes": []map[string]any{{
					"type": "oauthbearertoken", "name": "Bearer token", "description": "Workspace SCIM bearer token", "primary": true,
				}},
				"meta": map[string]string{"resourceType": path, "location": h.resourceURL(c, path)},
			}
			for _, feature := range []string{"changePassword", "sort", "etag"} {
				config[feature] = map[string]bool{"supported": false}
			}
			return http.StatusOK, config, nil
		}
		var resources []map[string]any
		for _, resource := range []struct {
			name       string
			schema     string
			attributes []scimAttribute
		}{{"User", service.SCIMUserSchema, scimUserAttributes}, {"Group", service.SCIMGroupSchema, scimGroupAttributes}} {
			doc := map[string]any{"name": resource.name}
			switch {
			case strings.HasPrefix(path, "Schemas"):
				doc["schemas"] = []string{scimCoreSchema + "Schema"}
				doc["id"] = resource.schema
				doc["attributes"] = resource.attributes
				doc["meta"] = map[string]string{"resourceType": "Schema", "location": h.resourceURL(c, "Schemas/"+resource.schema)}
			default:
				doc["schemas"] = []string{scimCoreSchema + "ResourceType"}
				doc["id"] = resource.name
				doc["endpoint"] = "/" + resource.name + "s"
				doc["schema"] = resource.schema
				doc["meta"] = map[string]string{"resourceType": "ResourceType", "location": h.resourceURL(c, "ResourceTypes/"+resource.name)}
			}
			if id := c.Param("id"); id != "" && id == doc["id"] {
				return http.StatusOK, doc, nil
			}
			resources = append(resources, doc)
		}
		if c.Param("id") != "" {
			return 0, nil, sql.ErrNoRows
		}
		return http.StatusOK, scimList[map[string]any]{[]string{service.SCIMMessage + "ListResponse"}, len(resources), resources, 1, len(resources)}, nil
	})
}
