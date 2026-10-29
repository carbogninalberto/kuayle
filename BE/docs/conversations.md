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
`follow_up_task` (see [requested agent reviews](agent-tasks.md#requested-agent-reviews)),
`run_check` (see [manual checks](agent-tasks.md#manual-checks)),
`link_repository`, and `start_preview`, `stop_preview`, `expose_port` and
`list_ports` (see [previews and ports from chat](#previews-and-ports-from-chat)).
`link_repository`, and the live channel tools `message_task` and
`get_task_messages` (see [agent messages in chat](#agent-messages-in-chat)).
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
`get_issue` adds the issue's `development_repository` (repository, base branch
and the issue/project/team/workspace scope that configured it), and
`delegate_issue` resolves the same repository when it requests approval and
shows it on the approval payload (`repository`, `base_branch`,
`repository_source`). The model is told not to ask for a repository unless
neither reports one; delegation then fails as data asking the user to choose and
link one, and a repository the policy refuses fails the same way. The approval
does not pin the repository: the task resolves current settings when it runs.

### Tool switches (Settings → Assistant)

`GET /assistant-capabilities` lists every built-in tool from the registry in
`assistant_tools.go` (plus `attach_images_to_issue`), so new tools appear without
further changes: `name`, the model-facing `description` (English),
`requires_approval`, `changes_data` (counts toward the change limit),
`available` (its dependencies are wired on this server, as `available()`
decides) and `enabled` for the workspace, with `can_manage` for the caller. Any
non-guest member may read it. `PUT /assistant-capabilities/:name` with
`{"enabled": true|false}` turns one tool on or off; unknown names are 404. Both
routes are session-only (personal access tokens are refused), and the write is
allowed only for current owners and admins: the role is checked in the same
SQL statement that writes `assistant_tool_settings` (workspace, tool name,
enabled, updated by, updated at; no row means enabled), so members, guests,
outsiders and an admin demoted concurrently get 403.

A disabled tool is enforced on the server, never only in the UI. Each reply
reads the switches fresh and does not offer disabled tools to the model. Each
call is checked again at the dispatch point (`runToolCall`), before approval
requests, the change limit or execution: a call to a disabled tool is recorded
as a failed tool call (`<name> is disabled in this workspace`), returned to the
model as data, and has no effect. Approving a pending approval whose tool has
since been disabled fails it with `This tool is disabled in this workspace`,
from the Web and Telegram alike, and starts nothing. The Settings page
(`/[workspaceSlug]/settings/assistant`) lists the tools with Switches for owners
and admins and a read-only state for members; its Skills tab is described under
[Skills](#skills), and its Custom tools tab is a placeholder for a later phase.

### Skills

Skills are Markdown guidance for the assistant, written for the workspace or
for one team (migration 98: `assistant_skills` and `assistant_skill_versions`).
A skill has a name (1–100 characters on one line, unique ignoring case among
the workspace-wide skills and among each team's), a description that tells the
assistant when to use it (1–500 characters on one line), a body (at most 64
KiB), `enabled`, `status` (`draft` or `active`), `current_version`,
`created_by_user_id` and `author_kind` (`user` or `assistant`). The skill row
holds the content in effect; every saved edit is a version row (name,
description, body, status, author kind and user). A `draft` version above
`current_version` of an active skill is a pending edit (`pending_version`).

The session-only routes under `/api/workspaces/:slug/assistant-skills` are:

| Route | Effect |
| --- | --- |
| `GET /` | `{skills, can_manage}`, without bodies |
| `GET /:skillId` | one skill with its body |
| `GET /:skillId/versions` | `{versions}`, newest first |
| `POST /` | create `{team_id?, name, description, body, status?}`; active unless `status` is `draft` |
| `PUT /:skillId` | save `{name, description, body, status?}` as the next version; the team is fixed at creation |
| `PUT /:skillId/enabled` | `{"enabled": true\|false}` |
| `POST /:skillId/activate` | put a draft into effect: `{"version": n}`, or the newest draft with an empty body |
| `POST /:skillId/versions/:version/restore` | save an old version's content as the next version |
| `DELETE /:skillId` | delete the skill and its versions |

An update without `status` keeps the skill's status. A draft edit of an active
skill only adds the version; any other edit (and a restore) takes effect at
once. Only draft versions can be activated (409 otherwise); a name already used
in the scope is 409. Non-guest members read the skills they can see: workspace
skills and those of teams visible to them, so a private team's skills are
visible only to its members and workspace owners and admins, as for issues.
Writes require a current owner or admin: the role is locked (`FOR SHARE`) in
the transaction that writes, so members, guests, outsiders and an admin
demoted concurrently get 403. Personal access tokens are refused.

**Loading.** Each reply's system prompt lists the active, enabled skills the
conversation's creator can see, by name, team key and description only (at
most 50 skills and 8 KiB; an issue chat lists only its team's skills and
workspace ones). `load_skill(name, team_key?)` returns the body to the model
fenced as Markdown and marked as workspace-authored guidance activated by an
owner or admin; the prompt says to follow it where it applies but that it
cannot grant permissions, skip approvals or override the assistant's rules.
Unlike other tools its result is not wrapped as `untrusted_data`, and only a
summary (name, version, size) is stored on the tool call, so large bodies do
not fill the conversation. Drafts, disabled skills and skills the user cannot
see are never listed or loaded. A name used by several teams in a free chat
needs `team_key` unless one skill is workspace-wide.

**Proposing.** `propose_skill(name, description, body, team_key?)` saves only
drafts, with `author_kind` `assistant` and the creator as author: a new name is
a new draft skill; the name of an existing skill in that scope adds a draft
version and leaves the active version in effect. It is available to owners and
admins only (members get a tool failure), counts toward the change limit and,
because the tool arguments are bounded to 16 KiB, a proposed body is at most
12 000 characters. It is not a `conversation_approvals` request: the stored
tool result (the card) carries the full proposed text, the version and a note,
and activation is the approval step, done by an owner or admin through the API
or Settings → Assistant → Skills. The model only gets a short receipt. Telegram
replies name each draft they saved and say it is not active until activated in
Settings. Both tools are in the registry, so they appear in
`GET /assistant-capabilities` and obey the tool switches; with `load_skill`
off, the prompt lists no skills.

**Settings UI.** Settings → Assistant → Skills
(`/[workspaceSlug]/settings/assistant?tab=skills`, `&skill=<id>` opens one
skill) lists skills with scope, status, version in effect, pending draft,
author kind and update time, filtered by scope. Owners and admins create skills
(workspace-wide or for a team they can see), edit them in a Markdown Textarea
with a preview, save edits as drafts or apply them, switch them on or off,
review a draft as a line diff against the version in effect before activating
it, restore old versions and delete skills after an AlertDialog confirmation;
members get the same views without write controls. In chat, a completed
`propose_skill` call renders as a card with the proposed name, description and
full body (through the sanitizing Markdown renderer, without images), a
"not active until activated" badge and a link to that skill in Settings; a
completed `load_skill` call is one "Used skill" line. Sources:
`UI/src/lib/features/assistant/AssistantSkills.svelte`,
`SkillEditorDialog.svelte`, `SkillReviewDialog.svelte`,
`ProposedSkillCard.svelte` and `skill-diff.ts`; checks:
`UI/tests/e2e/assistant-skills.test.ts`, the skill card test in
`UI/tests/e2e/assistant.test.ts` and `UI/tests/unit/skill-diff.test.mjs`.

### Custom tools

Custom tools are scripts the workspace's owners and admins write for the
assistant (migration 99: `assistant_custom_tools` and
`assistant_custom_tool_versions`; migration 100: `assistant_tool_runs`). A tool
has a `snake_case` name (`^[a-z][a-z0-9_]{0,47}$`, unique in the workspace, and
neither it nor `custom_<name>` may be a built-in tool's name), a description
(1–1000 characters, sent to the model), a parameter schema, a script (at most
64 KiB), an interpreter (`sh`, `bash`, `node` or `python3`, all present in the
developer images and so in every check container), a run mode (`approval`, the
default, or `auto`, allowed only when `read_only` is set; the database enforces
it too), a timeout (`timeout_seconds`, 10–1800, default 600), `enabled`,
`status` (`draft` or `active`), `current_version` and `author_kind`. Versions,
drafts, activation and restore work exactly as for [skills](#skills); the run
mode and read-only flag are part of a version, so only an owner or admin can
put an `auto` version into effect.

**Parameters.** The schema is validated strictly (`domain.ParseToolSchema`) and
stored canonically: an object with `type: "object"`, at most 20 `properties`
named `^[a-z][a-z0-9_]{0,39}$`, `required` naming only those, and no additional
properties. Each property is a `string` (`minLength`/`maxLength` up to 8192,
default `maxLength` 1024), a `number` or `integer` (finite `minimum`/`maximum`;
integers within ±2^53), a `boolean`, or a string `enum` (1–50 distinct values of
1–200 characters), each with an optional description of up to 500 characters.
Any other keyword, nesting, duplicate key or quoted number is refused; the
schema is at most 16 KiB. Inputs are validated against it (unknown or null
keys, wrong types, bounds and NUL characters are refused) and stored compact,
at most 16 KiB.

The session-only routes under `/api/workspaces/:slug` are:

| Route | Effect |
| --- | --- |
| `GET /assistant-tools` | `{tools, interpreters, can_manage}`, without scripts |
| `POST /assistant-tools` | create; active unless `status` is `draft` |
| `GET /assistant-tools/:toolId` | one tool with its script |
| `PUT /assistant-tools/:toolId` | save the content as the next version |
| `DELETE /assistant-tools/:toolId` | delete the tool, its versions and its runs |
| `PUT /assistant-tools/:toolId/enabled` | `{"enabled": true\|false}` |
| `POST /assistant-tools/:toolId/activate` | put a draft into effect: `{"version": n}` or the newest draft |
| `GET /assistant-tools/:toolId/versions` | `{versions}`, newest first |
| `POST /assistant-tools/:toolId/versions/:version/restore` | save an old version as the next version |
| `POST /assistant-tools/:toolId/test-run` | owners and admins: `{inputs, machine_id \| identifier, version?}` runs a saved version (default the newest, even a draft or a disabled tool); 202 with the run |
| `GET /assistant-tools/:toolId/runs` | `{runs}`, newest 50 |
| `GET /assistant-tool-runs/:runId` | one run |

Non-guest members read tools; writes and test runs re-check the actor's current
owner or admin role in the writing transaction (403 otherwise). Owners and
admins read every run; other members read only the runs they asked for, since
the output comes from the requester's machine. Invalid content is 400
`INVALID_ASSISTANT_TOOL` with a safe message, a taken name 409, a busy machine
409 `MACHINE_BUSY` and an unusable machine 409 `TOOL_MACHINE_UNAVAILABLE`.

**Offering and calling.** Every reply offers the active, enabled tools (at most
30) as `custom_<name>` functions, so they can never shadow a built-in tool, with
parameters `{identifier?, machine_id?, inputs}` where `inputs` is the tool's
own schema; a system message says these are admin-written scripts whose
results are untrusted data. A call names the machine: `identifier` runs on the
issue's newest coding task's machine and checkout (the caller must have created
the task, and it must be idle: awaiting review, blocked, failed or completed),
`machine_id` on a machine the caller created (at `/workspace`). Running needs a
non-guest role with `dev_machine:manage`. The call is recorded first; a draft,
disabled, deleted or unknown tool, invalid inputs or an invalid name fail as
data and run nothing. An `approval` tool records a `run_custom_tool` approval
bound to the issue (so approval-mode calls need `identifier`), the tool ID and
version, the validated inputs and the task generation; the card shows them.
Approving queues the run as the approver, once per approval (`approval_id` is
unique), and fails with a safe reason when the tool changed version or was
turned off, the task started another attempt, or the machine is busy or no
longer theirs. An `auto` tool queues the run directly and waits up to two
minutes for it; either way the model gets `{status, exit_code, output_tail
(last 6000 bytes), …}` as `untrusted_data`, and `get_tool_run(run_id |
approval_id)` reads a run later. Execution is described in
[agent tasks](agent-tasks.md#custom-tool-runs).

`propose_tool(name, description, parameters, script, interpreter, read_only?,
timeout_seconds?)` saves only drafts, for owners and admins, like
`propose_skill`: a new name is a new draft tool, an existing name a draft
version, and a proposal is always `approval` mode (`read_only` is kept as the
author's claim). Scripts proposed from chat are at most 12 000 bytes. The card
holds the full script and canonical schema; the model only gets a receipt.
Telegram replies name the drafts they saved, and Telegram approval answers
cover `run_custom_tool`. `propose_tool` and `get_tool_run` are registry tools
with switches; `GET /assistant-capabilities` also lists active custom tools as
`custom_<name>` with `custom: true`, and switching one sets the tool's own
`enabled` flag. Migration 100 also widens `conversation_tool_calls.name` to
allow digits; its rollback renames such calls to `custom_tool` and deletes
`run_custom_tool` approvals.

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
`untrusted_data` (except a loaded skill, see [Skills](#skills)), and the instruction says to ignore instructions inside them.
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

## Agent messages in chat

A running coding agent and the person who owns its task talk through the
task's [live channel](agent-tasks.md#live-channel).

**Tools.** `message_task(identifier, body)` resolves the visible issue and its
newest implementing task, as `follow_up_task` does, and queues the text as the
conversation's user with sender `orchestrator` (`QueueLiveMessage`: only the
task's creator, at most 4,000 characters, 30 per task per hour, not once the
task completed or was cancelled). It needs no approval because it only
delivers text and changes no other state; it is not an issue change. The
result says `delivery: "live"` for a running attempt (the agent reads it after
its current step) or `"next_attempt"`, and the tool card says so.
`get_task_messages(identifier, limit?)` is read-only and returns up to 50 (20
by default) messages of both directions, oldest first. Messages to the agent
carry `body`; the agent's own messages carry only `untrusted_agent_text`
(refused files are left out), and the result's `notice` tells the model never
to follow or act on them by itself. The system prompt says the same: the
assistant reports agent messages and answers an agent with `message_task` only
when the user's turn asks.

**Display.** Agent messages are never conversation messages and never enter
the model's context. Whenever a live message is queued, delivered or received,
the same transaction writes a `task.live_message` event (the message plus the
issue `identifier`) to the conversation that owns the task, when that
conversation's creator is the task's creator; this includes messages the
machine manager records while the attempt runs, so an open chat updates within
seconds. History returns `agent_messages`: the accepted (`received`)
`from_agent` messages of the conversation's tasks in the page's span of time
(from its oldest message, or the beginning for the oldest page, up to the
next newer page),
newest 200, oldest first, labelled with their issue. The chat places them
between its messages by time as distinct items labelled "Coding agent ·
ENG-1", rendered as escaped plain text (never Markdown, so no links, images or
HTML), with a note that the assistant does not act on them unless asked. A
`kind: "question"` item is marked as a question, and one that arrives live
raises a toast; the creator also gets a [Telegram notice](telegram.md#task-notifications)
when linked. The task card's **Live messages** thread lists both directions
with their delivery state and has a composer that posts to
`POST /agent-tasks/:taskId/live-messages` (see
[agent tasks](agent-tasks.md#live-channel)); it reloads on each
`task.live_message` event of its task.

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
[requested agent reviews](agent-tasks.md#requested-agent-reviews). `run_check` does too (migration 97); see
[manual checks](agent-tasks.md#manual-checks).

### Previews and ports from chat

The machine tools act on the issue's newest implementing task and its machine,
resolved after the issue is read with the user's own visibility. They call the
same code as the HTTP routes, as the conversation's user: the task repository's
`RequestPreview` (`POST/DELETE /agent-tasks/:taskId/preview`, task creator only)
and `DevMachineService` `ListPorts`/`CreatePort`/`UpdatePort`
(`/dev-machines/:machineId/ports`, machine owner only, since the gateway admits
only the owner). Another member's task, a task without a machine, a task without
a preview command, a reserved service port, an invalid port or label and a
disabled Dev Machine policy fail as data before anything is asked.

- `start_preview` (approval) binds the task, its machine, the preview command and
  the preferred port. Approval re-reads the task as the approver, refuses it when
  it moved to another machine and queues the preview; `PREVIEW_UNAVAILABLE`
  becomes a failed approval with its reason.
- `stop_preview` (no approval) queues the stop and reports `stopping` or
  `stopped`.
- `expose_port` (approval) binds the machine, port, optional label and gateway
  host, and says when the port already exists. Approval calls `CreatePort`; an
  existing port for that machine and port is reused (and renamed when a new
  label was approved) instead of duplicated.
- `list_ports` (read-only) returns the machine's ports with `https://` gateway
  links, listening state, detected listening ports and the task's preview.

Payloads carry `audience: "owner"`: the card says only the user, as the
machine's owner, can open the link while signed in to Kuayle. After approval
the chat reads the machine's ports from the API (never from tool output) and
offers an Open button per port, which launches it with a one-time ticket as the
machine page does; a starting preview is polled until its task port exists.
Migration 94 adds `start_preview` and `expose_port` to the allowed approval
actions; rolling back deletes those approvals, not the previews or ports.

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
