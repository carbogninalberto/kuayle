package openapi

import (
	"slices"
	"strings"
)

// Section is one part of the written guide. The same sections become the
// document's Markdown description and the x-kuayle-guide extension, which
// the reference page on kuayle.com renders.
type Section struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	Paragraphs []string `json:"paragraphs"`
	List       []string `json:"list,omitempty"`
	Code       *Code    `json:"code,omitempty"`
}

// Code is a sample shown with a section.
type Code struct {
	Language string `json:"language"`
	Text     string `json:"text"`
}

// SignatureHeader, EventHeader and DeliveryHeader are sent with every webhook
// delivery.
const (
	SignatureHeader = "X-Webhook-Signature"
	EventHeader     = "X-Webhook-Event"
	DeliveryHeader  = "X-Webhook-Delivery"
)

const signatureSample = `import { createHmac, timingSafeEqual } from 'node:crypto';

// rawBody: the request body bytes exactly as received, before any JSON parsing.
export function isFromKuayle(rawBody, signatureHeader, secret) {
  const expected = 'sha256=' + createHmac('sha256', secret).update(rawBody).digest('hex');
  const received = Buffer.from(signatureHeader ?? '');
  return received.length === expected.length && timingSafeEqual(received, Buffer.from(expected));
}`

// Guide returns the written guide for the given operations.
func Guide(ops []Operation) []Section {
	return []Section{
		{
			ID: "authentication", Title: "Authentication",
			Paragraphs: []string{
				"Every request is authenticated with a personal access token. Create one in the app under Settings, API Tokens: choose its scopes, optionally limit it to some workspaces and give it an expiry date. The token is shown once and stored only as a hash.",
				"Send the token in the Authorization header as \"Bearer kuayle_pat_...\". Use HTTPS; a token grants access until it expires or is revoked.",
				"A token acts as the user who created it and never has more access than that user. Creating, listing and revoking tokens needs a signed-in browser session and is not possible with a token.",
			},
			Code: &Code{Language: "sh", Text: "curl -H \"Authorization: Bearer $KUAYLE_TOKEN\" \\\n  https://kuayle.example.com/api/workspaces"},
		},
		{
			ID: "scopes", Title: "Token scopes",
			Paragraphs: []string{
				"Each operation names the scope it needs. A request passes when the token has that scope and the user's workspace role grants the same permission: scopes are a ceiling on the role, not an addition to it. Guests, for example, only read, whatever scopes their token carries.",
				"A request without the needed scope or permission gets 403 FORBIDDEN. Private teams stay invisible to non-members: their issues, projects, cycles and wiki pages answer 404 or are left out of lists.",
				"Scopes used by this reference:",
			},
			List: usedScopes(ops),
		},
		{
			ID: "pagination", Title: "Pagination",
			Paragraphs: []string{
				"The issue list is paginated with the page and per_page query parameters (per_page defaults to 50 and should not exceed 100). The response wraps the items in data and adds total_count, page, per_page and has_more; request the next page while has_more is true.",
				"The wiki registry and its overviews use a cursor: pass the next_before value of one response as before in the next request, and stop when next_before is null.",
				"Every other list returns all items the user can see in one JSON array.",
			},
		},
		{
			ID: "errors", Title: "Errors",
			Paragraphs: []string{
				"Errors use the HTTP status and one JSON shape: an error object with a machine-readable code, a message for people, and for validation errors a details array naming each field.",
			},
			List: []string{
				"400 BAD_REQUEST: malformed JSON, an invalid ID, or a rule the request breaks; the message says which",
				"400 VALIDATION_ERROR: one or more fields are invalid; see details",
				"401 UNAUTHORIZED: the token is missing, unknown, expired or revoked",
				"403 FORBIDDEN: the token's scopes, workspace limit or the user's role do not allow the request",
				"404 NOT_FOUND: the resource does not exist or the user cannot see it",
				"409 with a specific code such as CONFLICT or WIKI_VERSION_CONFLICT: the request conflicts with the current state",
				"429 RATE_LIMITED: too many requests; wait for the number of seconds in the Retry-After header",
				"500 INTERNAL_ERROR: the server failed; the request can be retried",
			},
			Code: &Code{Language: "json", Text: "{\n  \"error\": {\n    \"code\": \"VALIDATION_ERROR\",\n    \"message\": \"Request validation failed\",\n    \"details\": [{ \"field\": \"Title\", \"message\": \"Title is required\" }]\n  }\n}"},
		},
		{
			ID: "rate-limits", Title: "Rate limits",
			Paragraphs: []string{
				"Kuayle does not apply a request quota to authenticated API calls. Unauthenticated endpoints are limited per IP address: sign-in endpoints refill 5 requests per second with a burst of 10, and this document refills 10 per second with a burst of 20. A limited request gets 429 RATE_LIMITED with a Retry-After header.",
				"The operator of an instance can add limits at the reverse proxy, so treat 429 as retryable everywhere: wait for Retry-After, then retry. Keep bulk jobs sequential or at low concurrency; the instance is shared with the people using the app.",
			},
		},
		{
			ID: "webhooks", Title: "Webhooks and signature verification",
			Paragraphs: []string{
				"A webhook is a URL of yours registered for a workspace with a secret and a list of event names. The URL must be reachable from the instance and must not be a private, loopback or link-local address. The secret is stored encrypted and never returned.",
				"A delivery is an HTTP POST with a JSON body holding event, payload and timestamp (RFC 3339, UTC). The " + EventHeader + " header repeats the event name. The " + SignatureHeader + " header is \"sha256=\" followed by the hex HMAC-SHA256 of the raw request body, keyed with the webhook's secret. The " + DeliveryHeader + " header is the delivery's id, the same in every attempt.",
				"To verify a delivery, compute the same HMAC over the bytes you received, before parsing them, and compare it with the header in constant time. Reject the request when they differ. Deliveries time out after 10 seconds, so answer with a 2xx status quickly and do slow work afterwards.",
				"Deliveries are sent after the change is saved: one per webhook subscribed to the event, and one per issue for bulk operations (bulk edits and deletes, unfinished issues moved to the next cycle). The issue in an issue event is the stored issue: its ids, identifier, title, description, status, priority, dates and timestamps, including started_at, completed_at, cancelled_at and triaged_at. actor_id is the user whose change caused the event, also for a follow-up Kuayle makes for them, such as closing a parent whose sub-issues are all done. It is null only for system automation: status transitions from forge and GitHub activity.",
				"Delivery is at least once. Each change is stored when it is saved, one delivery per webhook, and survives restarts and updates of the instance; a delivery interrupted by a restart is sent again. A timeout, a network error or a status other than 2xx fails the attempt, and the delivery is retried with the same body, signature and " + DeliveryHeader + " after 30 seconds, 2 minutes, 10 minutes, 30 minutes and 2 hours. After the sixth failed attempt, or once it has waited a day, it is given up. Use " + DeliveryHeader + " to ignore a delivery you already handled. Each workspace has one delivery in flight at a time, workspaces take turns, and each workspace may hold at most 1024 pending deliveries and 8 MiB of payloads, and each webhook 256 deliveries, so a busy workspace or a dead receiver does not crowd out others; a delivery beyond these bounds is dropped. The webhook's delivery log (GET .../webhooks/{id}/deliveries) shows recent deliveries with their status, attempts, last status and error, and next attempt.",
				"A webhook whose deliveries fail three times in a row is paused for five minutes: its deliveries wait while the workspace's other webhooks keep receiving theirs. After the pause the earliest due delivery is attempted; a success resumes the webhook and a failure pauses it again. A first attempt goes out in the order the changes were saved, but a retry arrives after later deliveries: do not rely on order. Compare payload.issue.updated_at (the envelope timestamp has one-second resolution), or fetch the issue again for its current state.",
				"Webhooks are workspace-level and have no user, so they never deliver content of private teams: a workspace that has, or ever had, a private team does not deliver webhooks at all, creating or enabling a webhook there is refused with 403 PRIVATE_WORKSPACE_OPERATION_DISABLED, and a stored delivery is checked again when it is sent. Changes the database makes by itself send no event: deleting a parent detaches its sub-issues, deleting a project or cycle clears it from its issues, deleting a team deletes its issues and renaming a team key changes identifiers. A workspace import sends none either. Kuayle delivers these events:",
			},
			List: webhookEventList(),
			Code: &Code{Language: "js", Text: signatureSample},
		},
		{
			ID: "stability", Title: "Stability and versions",
			Paragraphs: []string{
				"This document lists the stable operations. Its version is the Kuayle release it was built from, and each instance serves the document for its own release at /api/openapi.json. Changes to a stable operation ship with a release and are listed in its notes.",
				"Routes of the app that are not listed here are internal: they serve the web app, the dev machines or integration setup, may change in any release, and most reject tokens.",
			},
		},
	}
}

func usedScopes(ops []Operation) []string {
	var scopes []string
	for _, op := range ops {
		for _, scope := range append([]string{op.Scope}, op.AlsoRequires...) {
			if !slices.Contains(scopes, scope) {
				scopes = append(scopes, scope)
			}
		}
	}
	slices.Sort(scopes)
	return scopes
}

// markdown renders the guide for tools that show info.description.
func markdown(sections []Section) string {
	var b strings.Builder
	b.WriteString("The public HTTP API of Kuayle, the open-source issue tracker. It covers issues, comments, teams, statuses, cycles, projects, labels, views, wikis, members and webhooks, for use with personal access tokens.\n")
	for _, section := range sections {
		b.WriteString("\n## " + section.Title + "\n")
		for _, paragraph := range section.Paragraphs {
			b.WriteString("\n" + paragraph + "\n")
		}
		if len(section.List) > 0 {
			b.WriteString("\n")
			for _, item := range section.List {
				b.WriteString("- " + item + "\n")
			}
		}
		if section.Code != nil {
			b.WriteString("\n```" + section.Code.Language + "\n" + section.Code.Text + "\n```\n")
		}
	}
	return b.String()
}

func webhookEventList() []string {
	list := make([]string, len(WebhookEvents))
	for i, event := range WebhookEvents {
		list[i] = event.Name + ": " + event.Description
	}
	return list
}
