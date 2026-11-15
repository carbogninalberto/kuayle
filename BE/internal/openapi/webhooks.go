package openapi

import (
	"reflect"

	"github.com/kuayle/kuayle-backend/internal/dto"
)

// IssueEventDelivery documents the body of an issue.* delivery. Kuayle sends
// dto.WebhookDelivery; this type only gives the payload its schema.
type IssueEventDelivery struct {
	Event     string                  `json:"event" validate:"oneof=issue.created issue.updated issue.deleted issue.triaged"`
	Payload   dto.WebhookIssuePayload `json:"payload"`
	Timestamp string                  `json:"timestamp"`
}

// CommentEventDelivery documents the body of a comment.created delivery.
type CommentEventDelivery struct {
	Event     string                    `json:"event" validate:"oneof=comment.created"`
	Payload   dto.WebhookCommentPayload `json:"payload"`
	Timestamp string                    `json:"timestamp"`
}

// WebhookEvent describes one delivered event in the document.
type WebhookEvent struct {
	Name        string
	Description string
	body        reflect.Type
}

// WebhookEvents lists the delivered events in display order. A test keeps the
// names equal to the service's list.
var WebhookEvents = []WebhookEvent{
	{"issue.created", "An issue was created, including through the API, imports, templates and automation.", reflect.TypeFor[IssueEventDelivery]()},
	{"issue.updated", "An issue changed: a field, its status, assignees, labels, project, parent or cycle, including bulk edits and automation. A bulk operation sends one delivery per issue.", reflect.TypeFor[IssueEventDelivery]()},
	{"issue.deleted", "An issue was deleted. The payload holds the issue as it was just before deletion. A bulk delete sends one delivery per issue.", reflect.TypeFor[IssueEventDelivery]()},
	{"issue.triaged", "An issue was accepted from triage.", reflect.TypeFor[IssueEventDelivery]()},
	{"comment.created", "A comment was posted on an issue. The payload holds the comment and the issue's id, identifier and team_id.", reflect.TypeFor[CommentEventDelivery]()},
}

func webhookEventNames() []string {
	names := make([]string, len(WebhookEvents))
	for i, event := range WebhookEvents {
		names[i] = event.Name
	}
	return names
}

func webhookDocs(schemas *schemas) object {
	out := object{}
	for _, event := range WebhookEvents {
		out[event.Name] = object{"post": object{
			"operationId": "receive" + operationSuffix(event.Name),
			"summary":     "The " + event.Name + " delivery sent to your URL",
			"description": event.Description + " See Webhooks and signature verification.",
			"security":    []object{},
			"parameters": []object{
				{"name": SignatureHeader, "in": "header", "required": true, "schema": object{"type": "string", "pattern": "^sha256=[0-9a-f]{64}$"},
					"description": "\"sha256=\" followed by the hex HMAC-SHA256 of the raw body, keyed with the webhook's secret."},
				{"name": EventHeader, "in": "header", "required": true, "schema": object{"type": "string", "enum": []string{event.Name}}, "description": "The event name, as in the body."},
			},
			"requestBody": object{"required": true, "content": object{"application/json": object{"schema": schemas.of(event.body, response)}}},
			"responses":   object{"2XX": object{"description": "Any response ends the delivery; it is not retried."}},
		}}
	}
	return out
}

// operationSuffix turns "issue.created" into "IssueCreated".
func operationSuffix(name string) string {
	out := make([]byte, 0, len(name))
	upper := true
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c == '.' || c == '_' {
			upper = true
			continue
		}
		if upper && c >= 'a' && c <= 'z' {
			c -= 'a' - 'A'
		}
		upper = false
		out = append(out, c)
	}
	return string(out)
}
