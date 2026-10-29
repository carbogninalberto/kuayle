# Persistent assistant conversations

The Global Assistant at `/:workspaceSlug/assistant` provides persistent chat with
the workspace's configured OpenAI-compatible provider, including DeepSeek
Chat Completions endpoints. The workspace owner configures the existing AI
settings (base URL, model and encrypted key). Removing the key disables new
requests; an already running request finishes or can be stopped.

## Current scope

The assistant can list teams and search, read and create issues through
[orchestrator tools](#orchestrator-tools), and request [approval](#approvals) to
delegate an issue to a coding agent. It has no repository, machine or publishing
tools; the system instruction says so. Conversations currently belong to
their creator, who must remain a non-guest workspace member. Two browsers signed
in as that user can view the same stream. Issue details now embed the same conversation UI, with a full-page link back to the
issue. Each creator has one reusable chat per issue and original team.
Conversations are never shared: [projects](#projects) and private folders only
tag and filter the creator's own list.

The native terminal, IDE, issue workflows and existing AI description expansion
remain separate and retain their existing behavior. There is no new container
or orchestration service.

## API and persistence

All routes below use authenticated sessions and current workspace membership;
PATs are deliberately denied until dedicated chat scopes are defined.

| Method | Workspace-relative path | Purpose |
| --- | --- | --- |
| GET | `/assistant/status` | Configured provider/model status, never the key |
| GET / POST | `/conversations?project_id=<id>\|none` | List the latest 100 of the caller's conversations, optionally of one visible project or without one / create with `{title, project_id?}`, or open/reuse with `{issue_id}` |
| PATCH / DELETE | `/conversations/:conversationId` | One field per request: `{title}`, `{folder_id}` (null for the root) or `{project_id}` (null to clear) / delete |
| GET | `/conversations/:conversationId?before=sequence` | Consistent snapshot, latest 50 messages, pagination flag and replay cursor |
| POST | `/conversations/:conversationId/messages` | `{request_id, content}`; idempotent user turn and queued reply |
| POST | `/conversations/:conversationId/messages/:messageId/retry` | `{request_id}`; explicit successor for the latest unsuccessful reply |
| POST | `/conversations/:conversationId/cancel` | Idempotently stop the active reply |
| GET | `/conversations/:conversationId/events?after=cursor` | Up to 200 ordered durable events |
| WS | `/conversations/:conversationId/stream?after=cursor` | Replay followed by genuine incremental output |

Migration 53 introduces `conversations`, `conversation_messages` and
`conversation_events`. It does not modify existing issue or machine state.
Rolling back **deletes conversation history**, while preserving existing issues
and workspace data. Take a backup before rollback.

Each request UUID is unique per conversation and message role. Reusing it with
different text is a conflict; concurrent sends cannot create two active replies.
Assistant attempts have `queued`, `streaming`, `completed`, `failed`,
`interrupted` and `cancelled` states. These are message-generation states, not
the forthcoming AgentTask or AgentSession state machines.

Message updates and events commit together. Transactions lock the conversation
before allocating cursors, preventing a later committed event from hiding an
earlier uncommitted event during replay. Events carry stable UUIDs and increasing
cursors; clients discard cursors already applied. Snapshot and pagination reads
share that lock, so snapshot cursors describe exactly the returned state.

## Issue conversations

Migration 55 associates a conversation with an issue and its original team. The
composite issue/workspace foreign key prevents cross-workspace associations.
Opening is idempotent for the creator, issue and team; multiple users retain
separate private chats. The issue description, comments and chat remain separate.
No issue lifecycle transition occurs merely by opening or chatting.

Every snapshot, event replay, send, retry, cancellation, worker claim and context
read rechecks current issue/team visibility and non-guest membership. A team move
hides the old conversation; opening after a move uses a separate scope. Deleting
an issue or its original team deletes the scoped chat through foreign keys.
Rolling back migration 55 deletes **issue conversations**, preserving general
conversations and issues; it never converts scoped history into general chat.

The snapshot API includes an authorized `issue` summary. The provider receives
that fresh snapshot (identifier, title, status, and at most 8,000 description
characters) as untrusted context. It receives no repository data or execution
authority. Completed turns still use the bounded conversation context window.

Short database transactions acquire the existing privacy graph barrier before row
locks. Issue conversations pin their issue before taking the conversation cursor
lock, matching deletion cascades. Authorization pins the relevant team membership until the transaction
finishes, so a completed revocation or issue move cannot precede a stale write.
Workspace-member removal locks its parent membership before deleting team grants
to preserve this lock order. Telegram output
uses the separate publication barrier, issue row and membership locks around its
bounded send, without holding the graph-write barrier across network I/O.

## Projects

Migration 86 adds a nullable `conversations.project_id` with a composite
`(workspace_id, project_id)` foreign key, so a chat can only be tagged with a
project of its own workspace; deleting the project clears the tag
(`ON DELETE SET NULL (project_id)`). A check constraint keeps it null on issue
chats: their effective project is always their issue's current project, so they
follow an issue moved to another project and cannot be moved themselves
(`PATCH {project_id}` returns 409 `CONVERSATION_CONFLICT`; `POST` with both
`issue_id` and `project_id` is 400). Rolling back removes only the tags.

Tagging does not change the audience: every read still requires the creator.
Assigning a project, at creation or with `PATCH`, requires that the project
belongs to the workspace and is visible to the caller (workspace-wide, or a
team the caller can access); otherwise the request is 404. Every response
(list, create, snapshot and updates) carries `project_id` and
`project: {id, name, team_id} | null` for the effective project, computed for
the caller at read time. A project the caller can no longer see (for example
after leaving its private team) is reported as `null` and never disclosed: the
chat appears under `project_id=none`, not under that project's filter.

The UI's conversation list has a project filter (all, no project, or one
project), remembered per person and workspace in browser storage. Rows show
their project when all projects are listed, the row menu offers **Move to
project** (disabled for issue chats), and a new chat started under a project
filter is created in that project.

## Execution and recovery

The existing API process runs four lightweight generation workers. PostgreSQL
`SKIP LOCKED` claims and a lease token fence every mutation. A browser refresh,
reconnection, duplicate POST, or additional API process cannot claim the same
attempt. Requests use the provider's streaming Chat Completions API. The worker
persists deltas in batches (200 ms or 1 KiB) and checks cancellation/membership
while awaiting provider output. The model only receives the orchestrator tools
below; it has no direct database, Docker or infrastructure access.

A reply may run for up to 10 minutes, across all of its model rounds and tool
calls; each provider request times out after 90 seconds. The claim grants a
two-minute lease, which the live worker renews every 30 seconds
(`RenewAttempt`, fenced by the lease token and refused once the lease has
expired), so recovery never reclaims a running reply but an attempt whose worker
crashed expires within two minutes. A worker that loses its lease cannot append,
record tool calls or complete. Recovery marks expired attempts `interrupted`; it
never automatically repeats an ambiguous paid provider call.
The user can explicitly retry the latest failed/interrupted/cancelled reply.
Already persisted partial text remains visible, but is excluded from subsequent model
context. Uncommitted output buffered since the last batch may be lost on a
crash; clients only receive committed events, so already displayed output is
retained. Model context includes up to the latest 50 completed messages and
64 KiB, starting with a user message.

The WebSocket uses the existing authenticated Echo/nhooyr stack. A separate
conversation endpoint is necessary because the workspace broadcast hub is not
an appropriate audience for private conversation text. It reads persisted events
every 250 ms and rechecks authorization on every pass, so API replicas share the
same source. It requires the exact configured frontend Origin, bounds writes,
and sends heartbeats. Reconnection is read-only and resumes from the last cursor.

## Security and bounds

- Every repository read/write requires an explicit caller access context,
  matching workspace, conversation creator and current non-guest membership.
  Membership locks prevent a concurrent revocation from authorizing a stale
  write. Worker appends apply the same checks as HTTP calls.
- Provider calls reuse the existing dial-time SSRF guard and refuse redirects.
  The operator's existing `AI_ALLOW_PRIVATE_NETWORK` opt-in is honored. Raw
  provider/network errors are not exposed in chat.
- The configured credential is redacted even across provider chunk boundaries.
  Hidden reasoning fields are ignored. User text and provider text remain
  untrusted data; the UI uses existing Markdown and DOMPurify sanitization, with
  Markdown images disabled so provider output cannot trigger automatic external
  requests. Ordinary links remain available for deliberate navigation.
- Request bodies are capped at 32 KiB; user text at 16 KiB; a reply at 64 KiB;
  an SSE frame at 256 KiB; the provider stream at 2 MiB. A successful stream
  must end with the Chat Completions `[DONE]` marker; EOF alone is interrupted. Live deltas are batched,
  and message/event history reads are paginated.
- Generation concurrency is four per API process. Resource-aware global limits,
  retention configuration and richer task artifacts are later phases; images
  are described under [Images](#images).

## UI foundation and verification

The UI copies the official Nova `message`, `bubble` and `message-scroller`
registry components and composes the existing `input-group` primitive, following
[Chat](https://www.shadcn-svelte.com/docs/ai-elements/chat),
[Message](https://www.shadcn-svelte.com/docs/ai-elements/message) and
[Prompt Input](https://www.shadcn-svelte.com/docs/ai-elements/prompt-input).
Only the required `@shadcn-svelte/primitives` dependency is added. Customized
existing primitives are not overwritten. Typed Svelte state consumes the Go
contract directly; there is no second AI orchestration runtime.

The scroller follows only at the live edge, preserves prepended history, and
provides a latest-message button. Sending or retrying returns the reader to the
live edge (re-arming follow mode, even if they had scrolled up); scrolling away
while a reply streams releases it again. Reloads after a send keep the
transcript on screen; only a first load shows the loading state. The composer supports Enter/Shift+Enter,
per-user/conversation drafts, send, stop and explicit retry. All strings use the
four existing Paraglide catalogs. Desktop conversation history collapses into a
mobile disclosure.

Focused checks, in addition to the repository testing skill's area checks:

```sh
# BE/; DATABASE_URL must identify a disposable migrated PostgreSQL database.
go test ./internal/service -run 'TestAssistant|TestAICompletion' -race -count=1
go test ./internal/repository -run 'TestConversations|TestIssueConversations' -race -count=1
go test ./cmd/server -run TestConversationHTTP -race -count=1
go test ./migrations -run TestConversationsUpgrade -count=1
# UI/
npm run test:e2e -- tests/e2e/assistant.test.ts
```

The HTTP test uses actual JWT/PAT middleware, repositories, workers and two
WebSocket clients against a local streaming provider fixture. It verifies live
output before completion, replay without duplicate execution and revoked access.
It is not a paid-provider smoke test. Migration tests exercise upgrade and
rollback with existing issue fixtures. Browser tests cover keyboard, drafts,
streaming, history, denied access, mobile layout and long transcript scrolling.

## Orchestrator tools

The worker offers OpenAI-compatible function tools: `list_teams`,
`search_issues`, `get_issue`, `create_issue`, `update_issue` (title, description,
status or priority; assignees are not changed), `delegate_issue`, `plan_issues` and `link_issues` (see [dependencies and collaborating agents](agent-tasks.md#dependencies-and-collaborating-agents)),
`get_agent_work`, `list_agent_work`, `merge_pull_request`, `accept_agent_work`
(see [accepting without merge](agent-tasks.md#accepting-without-merge)), `request_agent_review`,
`follow_up_task` (see [requested agent reviews](agent-tasks.md#requested-agent-reviews))
and `link_repository`.
`create_issue` accepts a `parent_identifier`. The write tools (`create_issue`,
`update_issue`, `link_issues` and `link_repository`) count toward the per-reply
change limit. The model only proposes calls.
`AssistantTools` executes each in Go as the conversation creator, re-reading the
current workspace role (guests and roles without `issue:create` or
`issue:update` are refused) and
using the same team-visibility-scoped repositories and `IssueService.Create` as
the HTTP routes. Arguments are strict JSON: unknown fields such as a creator ID
are rejected, and the issue is always created with the user's own identity.
Teams are resolved by key among the teams the user can see.

`get_agent_work` reads one visible issue's latest tasks. `list_agent_work` lists
the workspace's 50 most recently updated tasks (optionally one state) on issues
the user can see, newest first: issue identifier and title, state and reason,
agent, attempt, recorded pull request and latest trusted review decision. Like
`get_agent_work`, visibility follows the issue's team rather than the task's
creator (the Web queue, `GET /agent-tasks`, lists only the user's own tasks);
guests and non-members get nothing, and an oversized list drops its oldest
entries and says `truncated`. `link_repository` sets the GitHub repository and
base branch of an issue, or with `scope: "project"` of the issue's project, in
the same `dev_machine_scope_settings` as `PUT /dev-machine-scope-setting`: it
needs `dev_machine:manage`, a project needs a workspace owner or admin, the
repository must be active in the workspace's GitHub installation and allowed by
the Dev Machine policy's repository allowlist, and the scope's other settings
(environment, test command, instructions, preview) are kept.

A provider may return tool calls only when tools were offered and must finish
the turn with `finish_reason: "tool_calls"`. One reply runs at most 400 tool
calls (at most 32 per model round, enough rounds for each call to run in its
own) and 10 issue changes; further calls fail as data without running. Once the
call budget is spent, half of the 64 KiB reply text is used, or less than 90
seconds of the 10-minute reply remain, the next round offers no tools, so the
model must answer. Each result is at most 24 KiB. The context resent each round
stays bounded: the conversation is unchanged, while the reply's newest tool
rounds are kept complete within 128 KiB and older ones keep their call/result
pairing but have their text, arguments and results replaced by a short
"omitted" marker the model can act on by calling the tool again. Results are returned to the model wrapped as
`untrusted_data`, and the instruction says to ignore instructions inside them.
Prompt-injected content can therefore at most cause the bounded, user-permitted
actions above, which remain visible and attributed in the conversation.

Migration 60 adds `conversation_tool_calls`. Each call is inserted as `running`
under the generation lease **before** it executes, then completed or failed
exactly once with bounded output, an optional `issue_id` and a user-safe error
(internal errors are never shown). Losing the lease or conversation access stops
the reply before the next effect. A message that ends while a call is still
running records that call as failed with an explicit unknown outcome; calls are
never re-executed automatically, and a retry creates a new reply. Calls appear
in history (`tools`) and as `tool.started`/`tool.completed` events, ordered by
sequence. The UI renders them with the AI Elements
[Tool](https://www.shadcn-svelte.com/docs/ai-elements/tool) composition (Trace
plus escaped Code Block input and output) and links created issues. Telegram
uses the same worker, so its replies can use the same tools.

Assistant replies list the issues their tools actually returned with the AI
Elements [Sources](https://www.shadcn-svelte.com/docs/ai-elements/sources)
composition (in-app links, identifiers validated). An empty conversation offers
contextual starting prompts with [Suggestion](https://www.shadcn-svelte.com/docs/ai-elements/suggestion)
buttons, which send like typed messages. An approved delegation shows the task it
started with [Subagent](https://www.shadcn-svelte.com/docs/ai-elements/subagent):
its live state, harness and model, reason and a link to the issue chat.

Model context includes completed message text only; earlier turns' tool
results are not replayed, so the assistant relies on its own replies for
continuity.

## Approvals

`delegate_issue` never starts work. It resolves the issue with the user's own
visibility and records, under the generation lease, a pending approval (migration
61) bound to the conversation creator, the `delegate_issue` action, the issue and
the exact instructions, expiring after 24 hours. The model receives
`pending_user_approval` and is told not to claim work has started.

`POST /conversations/:conversationId/approvals/:approvalId` with
`{"decision":"approve"|"deny"}` is session-only. The backend, not the UI, checks
that the caller is the bound actor in an authorized conversation; anyone else
gets 404. Under a row lock, denial is final, an expired approval becomes
`expired` (HTTP 410), and approval claims the decision as `approving`. The effect
then re-opens the user's issue conversation (re-checking issue visibility),
reads the current task options and creates the coding task through
`AgentTaskService.Create` with the composer's defaults (first allowed provider;
the workspace model only for Codex and Claude Code) and the approval's fixed
request ID. Concurrent clients or repeated clicks therefore converge on one task.
Disabled agents, a disallowed provider, a lost issue or a rejected request mark
the approval `failed` with a user-safe reason; a transient error leaves it
`approving`, and approving again resumes it. An approved or failed decision
cannot be denied, and a denial cannot later be approved.

Decisions stream as `approval.requested`, `approval.updated` and
`approval.resolved` events and appear in history (`approvals`). The UI renders a
pending request with the AI Elements
[Confirmation](https://www.shadcn-svelte.com/docs/ai-elements/confirmation)
composition (Item with Deny/Approve; instructions as escaped Code Block text)
and resolved states as Trace lines; stale copies never reopen a decision.
Telegram delivers pending approvals with inline Approve/Deny buttons that use
this same resolution path; see [Telegram approval buttons](telegram.md#approval-buttons).

`request_agent_review` and `follow_up_task` use the same approval path (migration
84 extends the allowed actions); their effects and re-checks are described in
[requested agent reviews](agent-tasks.md#requested-agent-reviews).

## Images

Migration 82 adds `conversation_attachments` (workspace, conversation,
message, uploader, storage key, sniffed type, size, dimensions, position),
`conversation_attachment_issues`, `conversation_attachment_garbage`,
`agent_task_attachments` and `ai_settings.assistant_vision` (default false).
Rolling back deletes image records and task image lists, not stored objects or
issue assets copied from them.

| Method | Workspace-relative path | Purpose |
| --- | --- | --- |
| POST | `/conversations/:conversationId/attachments` | Multipart `file`; stores one pending image |
| GET | `/conversations/:conversationId/attachments/:attachmentId` | Streams an image to a current reader |
| DELETE | `/conversations/:conversationId/attachments/:attachmentId` | Discards a pending (unsent) image |
| POST | `/ai-settings/vision-check` | Owner only; probes the saved model with a test image |

All are session-only like the other conversation routes. Upload authorizes the
conversation before reading the body, reads at most 10 MB plus one byte, and
accepts only PNG, JPEG, WebP or GIF identified by content (never by name or
client type) with each side at most 16384 pixels; nothing is stripped or
re-encoded. Objects are stored through the configured storage backend under a
random key. A user may hold 24 pending images per conversation; pending images
are visible only to their uploader and expire after 24 hours. Images are served
with their sniffed type, `X-Content-Type-Options: nosniff`, `Content-Disposition:
inline` (or `attachment` with `?download=1`), `Content-Security-Policy:
default-src 'none'; sandbox`, `Cross-Origin-Resource-Policy: same-origin` and
`Cache-Control: private, no-store`. Every read rechecks conversation access
(creator, non-guest membership, issue/team scope).

`POST /messages` accepts `attachment_ids` (at most 6, unique). They must be the
caller's pending images in that conversation; they are bound, in order, to the
new user message in the send transaction, and the `message.created` event and
history carry them as `attachments`. A message may consist of images alone. A
resent request ID with the same text and the same images returns the original
reply without binding anything again; the same request with other images is a
conflict. Deleting attachment rows (directly, by expiry or through a cascade
from a conversation, user or workspace) records their storage keys in
`conversation_attachment_garbage`; the API removes the objects every ten
minutes.

Model context describes every image as `[image attached: name, WxH, id=…]`, so
a model can refer to, file or forward an image it cannot see. When the owner
turns on **Model can see images** in AI settings, the newest 4 images in the
context (each at most 4 MB, 10 MB in total) are also sent as OpenAI-compatible
`image_url` content parts with base64 data URLs; older or larger images remain
text notes. Whether a model accepts images is not assumed: **Check** sends a
16×16 red PNG to the saved model and reports whether it answered and whether
the answer names the colour; provider error text is bounded and has the key
redacted. The toggle stays the owner's decision.

`attach_images_to_issue` (a write tool, counted toward the change limit)
requires `issue:update`, resolves the issue with the user's visibility and
copies the conversation's sent images (all or the named IDs, at most 6) into
new issue assets scoped to the issue's team, then posts one comment by the user
embedding them. Issue readers then see them through the existing asset rules,
outsiders of a private team do not, and an image already filed on that issue
is not filed again. Coding tasks receive these and any other images embedded
in the issue description and comments; see
[issue images](agent-tasks.md#issue-images).

The composer follows AI Elements Prompt Input attachments: an attach button,
paste and drop, previews with upload progress, retry and remove, and send
disabled while an upload is unfinished or failed. Uploads use XHR for progress
with the session cookie. Messages show thumbnails that open in a shadcn Dialog
(Escape closes, focus returns). Focused checks:

```sh
# BE/; DATABASE_URL must identify a disposable migrated PostgreSQL database.
go test ./pkg/imageinfo ./internal/service -run 'TestInspect|TestImageStore|TestModelMessages|TestVisionProbe|TestConversationImages|TestTelegramPhotos|TestTask' -count=1
go test ./cmd/server -run TestConversationAttachmentsHTTP -count=1
go test ./migrations -run TestConversationAttachmentsMigration -count=1
# UI/
npm run test:e2e -- tests/e2e/assistant-attachments.test.ts
```

## Next phases

The full platform specification remains the objective: issue creation/delegation,
AgentTask state transitions, automatically prepared worktrees, native resumable
sessions, structured execution traces, persisted approvals and GitHub review,
then task-aware Telegram notifications/approvals and authenticated QA previews.
The implemented Telegram chat channel already enforces an ID allowlist and verified
Kuayle-account links; approval callbacks remain disabled until secure approvals exist.
Resource-aware scheduling and advanced automation follow only after those workflows
are reliable.

Telegram is now an additional private channel for these same records. See the
[Telegram contract](telegram.md) for encrypted settings, numeric user-ID allowlists,
one-time account linking, webhook deduplication and delivery guarantees. Migration
54 and its delivery tables are required by the current conversation writer.
