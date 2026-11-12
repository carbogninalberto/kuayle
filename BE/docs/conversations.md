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
in as that user can view the same stream. Issue details offer a **Chat with assistant**
button that opens (creating or reusing) the issue's conversation on the assistant page,
which links back to the issue; no chat is embedded in the issue page. Each creator has one reusable chat per issue and original team.
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
four existing Paraglide catalogs.

The page is a three-column shell ([AssistantChat](../../UI/src/lib/features/assistant/AssistantChat.svelte)):
the assistant's own sidebar (places, conversation search, project filter, a
collapsible Recents list with folders, the signed-in person) is fixed from
1024 px and a drawer below; the centre holds the conversation (user turns as
compact right-aligned bubbles, assistant turns as plain Markdown, tool calls as
one-line step chips with the overflow behind "+N more steps", coding tasks as
compact steps where they started) and a floating composer; the right side is a
resizable [panel host](../../UI/src/lib/features/assistant/panels/panel-host.svelte.ts)
(inline from 1280 px, a drawer below) with Details (environment, context
usage of the 50-message model window, changes, background tasks), Agents (each
coding task with the path it took) and Diff panels. New panels, such as a
future workflow editor, register as one more `PanelDefinition`. Unknown tool
names render through the generic step chip; [tool-chips.ts](../../UI/src/lib/features/assistant/tool-chips.ts)
registers per-tool labels, icons or renderers. The one composer sends to the
assistant or, when aimed at a running task, to its agent through the live
channel, and says which. Earlier messages can be folded behind "Show N earlier
messages" (remembered per person). Conversations move between folders by drag
and drop or the row menu. Failed tasks stay listed in the transcript, the
Agents panel and Background tasks; only completed or cancelled ones fold into
the "N finished" row. Each task shows "Iteration N" with why it started (first
run, review changes, CI follow-up, your follow-up, retry after failure),
derived from its recorded transitions and follow-up signals.

**Agent view.** When an issue chat has coding tasks, the top bar offers
"Agent · ENG-1" (a menu with status dots when there are several). It swaps
the centre column to that task's agent thread, named in the URL
(`?conversation=…&agent=<task id or issue identifier>`, an identifier picks the
newest task), with a breadcrumb back to the assistant; the task's step in the
transcript and its Background tasks row open the same view. It is a
transcript and a composer. While it is open the page's composer gives way to
the agent's own
([AgentComposer.svelte](../../UI/src/lib/features/assistant/AgentComposer.svelte)),
under the timeline: it writes straight to that task's agent through the
[live channel](agent-tasks.md#live-channel), with no approval, and is shown
only to the task's creator (nobody else, in any role, writes to another
person's agent) while the task takes messages. Enter sends. One more action,
an icon beside Send, does it now; its shortcut, Ctrl/⌘+Enter, is shown in its
tooltip and announced (`aria-keyshortcuts`). It is "Interrupt and send" while
a Claude Code agent works, with or without a workflow (`mode: "interrupt"`,
no confirmation: it is the point), and "Send and start now" when no agent
works and the task is failed, blocked or waiting (`mode: "start"`). Starting a
task that is waiting on CI, an approval, a merge or a review abandons that
wait and counts a visit and an iteration, so the button and the shortcut ask
once first, naming what the task waits for. Codex and OpenCode are interrupted
by any message, so they have Send alone. A completed or cancelled task has no
composer. The composer has no helper text; a refused send is said above it and
keeps the text. An agent's question has an "Answer the question" button under
it: it moves the cursor to the composer and shows the question above the text
as what the message answers (dismissable); the message is then sent as
`In answer to your question "…":` followed by the answer, so the agent knows
which question it is, and its own reply names that message's number.

The conversation lives in the timeline
([agent-conversation.ts](../../UI/src/lib/features/assistant/agent-conversation.ts),
from `GET /agent-tasks/:taskId/live-messages`, reloaded on each
`task.live_message`): a message to the agent sits in the run that received
it, in the session at the moment it was delivered (before the first thing the
agent did afterwards), right-aligned, with its time and one quiet icon for its
state, named on hover and for assistive technology: waiting, delivered,
replied, or no reply. The agent's reply (the message whose `reply_to` names
it) is the next item; its other messages and questions are placed by time, as
escaped plain text. A message that waits is shown where it will be picked up:
at the end of the running step, or after the last step when none runs. A
message is "no reply" once the attempt that received it ran to its end
without one (or the task ended); an attempt that was interrupted or paused
hands it to the one that continues it. A missing reply never fails the
attempt, so the task goes on; the step's line carries a warning icon, also
when closed, and the attempt's risk notes say which message. There is no
separate "Live messages" thread, in this view or on the task card in the
sidebar.
The header has "Request review"
([AgentReviewRequest.svelte](../../UI/src/lib/features/assistant/AgentReviewRequest.svelte))
while the task awaits review with its pull request open: a menu of the
allowed harnesses that writes the request into the assistant's composer and
returns to the chat, where the assistant asks for approval (see
[requested agent reviews](agent-tasks.md#requested-agent-reviews)). The
requested reviews and their findings are listed under the timeline.
The header also has a Stop button
([TaskStopButton.svelte](../../UI/src/lib/features/assistant/TaskStopButton.svelte))
while the task can still be stopped, shown to the person who started it: one
confirmation, then the existing cancel endpoint, and the task reads as
cancelling until it reports cancelled.

The thread is a timeline of the task's workflow steps, oldest first
([agent-timeline.ts](../../UI/src/lib/features/assistant/agent-timeline.ts),
from `GET /agent-tasks/:taskId/workflow`): one line per step with its name,
the profile and harness that ran it, the model, the visit when it is not the
first, how long it worked (the time a person's pause held it,
`paused_seconds`, is left out; the start time on hover) and a status icon. An agent
step opens to its work; a step that is not an agent's (start, publish, check,
approval, merge, end) is a compact row that opens only when the runner
recorded a detail for it. The step an agent is running is open and streams;
with none running the last agent step is open; the others are closed until
the reader opens them. A finished run shows its final message first, as
Markdown, and the trace that led to it below; a planning step shows its plan
instead. A reviewer step shows its review task's run and links to that task's
own agent view (`?agent=<review task id>`, read by id because the conversation
does not list reviews). A task without a workflow is one step per attempt,
from `/runs`. Each section's element id is stable (`step-<sequence>`, or
`run-<run id>` without a workflow), and a link with that fragment opens the
step, also when the view is already open (the sidebar's path links here). The request and a prompt in the session are clamped with "Expand" in
place; the original output is behind the terminal toggle under each run. The
harness name and the start time are read out with the line and shown in a
tooltip on hover or keyboard focus.

**Notebook, prompt and flags** (KUA-134), for the task's creator, who alone
can read them. The [task notebook](agent-tasks.md#task-notebook) is one quiet
line above the timeline, "Notebook" with the plan's progress ("2 of 5"), so
the transcript stays the view; it opens in place to a short working document
([TaskNotebook.svelte](../../UI/src/lib/features/assistant/TaskNotebook.svelte),
[task-notebook.ts](../../UI/src/lib/features/assistant/task-notebook.ts)):
the plan as a checklist that shows what is done (a step opens to its
acceptance check, commands and files; a plan without steps shows its text),
then what is done (an entry Kuayle recorded from an agent's final message
says so), what remains, how to verify, decisions and findings (open ones
first, each with its source). What people asked is a block of its own,
"From people", apart from what agents wrote, without the request the header
already shows. Who changed which sections at which step, and what Kuayle
noted (pauses, updates accepted in part), is "History" at the end, closed
until asked for. The view has no labels beyond the section names and nothing
to fill in: there is no write route. It is read with the task's shared load
(below), so it follows the task's events and the workflow's fallback poll
without a poll of its own, and again when a message is sent to the agent (an
instruction in it); the same revision is not rendered again. Once the opened
document is laid out the transcript is scrolled to its start
(`scrollToStart`), which also stops the scroller following the live end: a
short transcript is at both ends at once and would otherwise jump below the
notebook as it grows. A review task has
no notebook, and another member's task shows none.

An opened agent step has one small button above its work, "Prompt", that
opens [the prompt its agent received](agent-tasks.md#the-prompt-a-step-received)
in a sheet ([RunPrompt.svelte](../../UI/src/lib/features/assistant/RunPrompt.svelte),
[run-prompt.ts](../../UI/src/lib/features/assistant/run-prompt.ts)); nothing
of it is read before. It is one section per source in the order Kuayle
assembled it, the source's name as the heading with its size, a mark for
parts that went to the instructions file or were cut when recorded, and
"Read-only worktree" for a planning step. Sections are open, show their first
six lines, expand in place and can be closed, so two sources that contradict
each other (a step that says "implement", a profile that says "do not") are
on screen together. A step resumed in several runs offers each by its start
time, newest first; a run from before prompts were recorded says so.

A run that ended without updating the notebook is flagged on its step's
line, also when closed, with a quiet icon named "Did not update the
notebook", beside the "No reply" icon of a run that did not answer a
message. It is read from the notebook's history: Kuayle then records the
agent's final message as done itself, the only time it writes that section,
with the run it belongs to (so the flag covers the notebook's newest 50
changes). `closing_seconds` is not returned by the API and is not shown.
Browser coverage: `UI/tests/e2e/task-notebook.test.ts` (desktop and phone
widths), with the models in `UI/tests/unit/task-notebook.test.mjs` and
`run-prompt.test.mjs`.

What a task did is loaded once per task and shared
([task-run.svelte.ts](../../UI/src/lib/features/assistant/task-run.svelte.ts),
`useTaskRun`): the agent view's timeline, the task card's path and the
workflow chart read the same run, so the workflow is fetched and polled once
however many of them are open. A read that fails is tried once more after two
seconds; if that fails too the card shows one retry button (the chart says so
in words). For a task without a workflow the attempts, the Standard chart and
the state changes are read independently, so one failing takes none of the
others.
The steps reload on the task's events (`task.state_changed`, `task.updated`,
`task.workflow_updated`, `task.review_updated`). As a fallback for missed
events the workflow is polled every 15 s while a step runs and every 2 min
while it waits (approval, CI, a paused or blocked task), and not at all once
it is done or failed; reloads send `summaries_since`. A run's output is read
once when its step is opened; only the run that is going follows
`task.activity`, so finished runs left open do not refetch while another
streams. A run the task was just given and that is not recorded yet (`404`)
is waited for; "records no longer available" is only said for a task that
ended.

The readable session is parsed in the browser from the run's recorded log
chunks (the same `/runs/:runId/logs` pages the task card reads, refreshed on
`task.activity`) by [agent-transcript.ts](../../UI/src/lib/features/assistant/agent-transcript.ts):
Claude Code stream-json, Codex `exec --json` and OpenCode `run --format json`
become agent messages (sanitized Markdown), one-line tool chips paired with
their results by id (expandable, long results behind "show more", failures
marked), "Thought for Ns" markers without signatures and a compact session
header; consecutive calls fold into "Ran N commands". A Claude Code call that
starts a subagent (`Task`/`Agent`) is one collapsed row with what it was asked,
its status and duration: events carrying its id in `parent_tool_use_id` are
kept under it in order, at any depth, and its `tool_result` is the subagent's
answer (`is_error` marks the row failed). The line with that call is often too
large to be recorded, so the row is also made from the `system` `task_started`
event of a `local_agent` task (`tool_use_id`, `description`, `prompt`), or as a
last resort from the first event naming the call. `task_*` events are not
listed: a `task_notification` (`completed`, `stopped` or `failed`) ends a
background subagent (`is_backgrounded`) or a call whose result was not
recorded, and a `local_bash` task stays an ordinary chip. Once the run's
`result` arrives, a call still without a result shows as finished without
one. Codex and OpenCode output names no parent call, so their runs stay flat. OpenCode events carry
their content in `part`: `text` and `reasoning` parts, `tool_use` parts keyed
by `callID` (input, output or error, and the call's own `state.time` for its
duration; `status: "error"` or a non-zero `metadata.exit` is a failure), a
`step_finish` with `reason: "stop"` as the end of the run, and a top-level
`error` whose `error.data.message` is shown, or a pause when it is the
provider's usage limit. Oversized or limit-truncated lines say so,
unknown events and plain output stay visible as escaped text, and "Raw" shows
the original output. The task card's Execution output uses the same view.

**Task card.** In the Agents panel each task is its state, its actions, the
path it took and its execution details. It has no composer, no message thread
and no review action: those are in the agent view. The path
([TaskPath.svelte](../../UI/src/lib/features/assistant/TaskPath.svelte),
[task-path.ts](../../UI/src/lib/features/assistant/task-path.ts), built on the
timeline's steps) is one short vertical line: a step's name, its agent
(profile), how it went as an icon in its colour, the visit when above one,
when it started and how long it worked (as in the agent view, without the
time it was paused). Compact, the default, is each step
once, as it last went, in the order the task last passed them, without the
start; "Full path" opens every step with its loops, and the pauses, resumes
and moves to a newer workflow version where they happened. Only the step the
task is on says more by itself: what it waits for (CI on the pull request, an
approval with its question, the merge, a limit, a person's pause) or, for a
failed task, why it stopped there; the card then does not repeat the task's
reason. A step opens to what the runner recorded for it and to where the task
went next and on which transition. Each step links to its section in the
agent view; on narrow screens following it closes the panel's sheet. The time
on every entry, a state change's included, is absolute for assistive
technology and in a tooltip on hover or keyboard focus; a step's tooltip also
has its whole name, which a narrow row may cut. A task without a workflow
shows its attempts the same way (the last outcome and the current attempt
when compact), each opening to its state changes from `/history`. An attempt
is numbered by its generation and paired with the changes of that generation
(the nth "queued" opens the nth attempt), so one that failed before a run was
recorded shifts nothing. The Workflow panel shows the same path under
the chart.

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
`list_ports` (see [previews and ports from chat](#previews-and-ports-from-chat)),
and the release tools `list_releases`, `get_release`, `draft_release_notes`,
`create_release` and `get_workflow_run` (see [release tools](#release-tools)),
and `ship_pull_request`, which chains merge, tests, release, build and upgrade
behind one approval (see [shipping in one approval](#shipping-in-one-approval)),
and the read-only research tools `web_search`, `repo_list_files`, `repo_glob`,
`repo_grep`, `repo_read_file`, `repo_symbols` and `spawn_research_subagents`
(see [research tools](#research-tools)).
`link_repository`, and the live channel tools `message_task` and
`get_task_messages` (see [agent messages in chat](#agent-messages-in-chat)).
The wiki tools address a team wiki by `team_key` or a project wiki by
`project` name or ID, resolved among the teams and projects the user can see,
and call the same `WikiService` methods as the HTTP routes, so every call
applies the repository's wiki visibility predicates as the conversation
creator. Reads (`list_wiki_pages`, `get_wiki_page` with its headings,
`list_wiki_page_versions`) need `wiki:read` and no approval. Writes need
`wiki:write` and count toward the change limit: `create_wiki_page`,
`update_wiki_page` (rename, and content by `mode`: `replace` with the page's
version, `append`, `replace_section` or `insert_after_heading` by heading text),
`append_wiki_registry` and `manage_wiki_registry` (`status`, `enable`,
`set_overview_schedule`, `generate_overview`). A stale version always fails as
a conflict that tells the model to read the page again; nothing is
overwritten. The destructive actions `delete_wiki_page`,
`restore_wiki_page_version` (the state before or after a recorded change,
bound to the page version shown on the card) and `disable_wiki_registry` are
approval tools (migration 128): nothing happens until the user approves, and
approving re-checks `wiki:write`, the wiki's visibility and, for a restore, the
page version. Pages are returned as text, never as stored HTML. A new wiki
action is one catalog entry in `assistant_wiki_tools.go`, a case in
`runWikiTool` (or, for an approval tool, in `prepareWikiApproval`, the
`requestApproval` switch, `AssistantApprovals.wikiAction`, the policy table and
the approvals' action constraint).
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
enabled, updated by, updated at; no row means enabled, except for the tools in
`repository.DefaultOffAssistantTools`, currently `spawn_research_subagents`,
which are off until an owner or admin turns them on), so members, guests,
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

and admins and a read-only state for members; its Skills tab is a placeholder
until its settings page lands (the API is described under [Skills](#skills)),
and its Custom tools tab is described under [Custom tools](#custom-tools).

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

**Settings UI.** Settings → Assistant → Custom tools
(`/[workspaceSlug]/settings/assistant?tab=custom-tools`, `&tool=<id>` opens
one tool, which `propose_tool` cards link to) lists each tool with its
`custom_<name>` call name, description, status, pending draft, run mode,
read-only mark, interpreter, version, author and an enabled Switch. The editor
has a monospace Textarea for the script and a parameter builder (name, type
string/number/integer/boolean/enum, description, required, length or numeric
bounds, enum values) that produces the canonical schema above and refuses
locally what the server would refuse; a JSON tab shows the generated schema.
`auto` is selectable only with read-only set. Saving offers a draft or an
immediate version, as for skills; activating a draft shows what changes and a
line diff of the script against the version in effect. The tool page shows the
schema, the script, versions (restore) and the run history (inputs, status,
exit code, output), and the test-run dialog builds an inputs form from the
selected version's schema, targets one of your machines or an issue's task
machine and polls `GET /assistant-tool-runs/:runId` until the run ends. Members
get the same pages read-only, without write or test-run controls; the server
enforces every write. In chat, a `propose_tool` result renders as a draft card
with the full script and a schema summary, a pending `run_custom_tool`
approval shows the tool, version, description, interpreter and the exact
validated inputs, and an approved or `auto` run is polled (found by its
`approval_id` in the tool's runs) until it ends; `get_tool_run` is one line.
Scripts, inputs and output are always rendered as escaped text.

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

**Tools.** `message_task(identifier, body)` never sends anything by itself:
the agent reads a live message as its owner's word, and the model may have
been steered by untrusted text it read (issues, web results, pull request
comments, another agent's output). The tool resolves the visible issue and its
newest implementing task, as `follow_up_task` does, checks that the
conversation's user created that task and that it still takes messages, cleans
the text (at most 4,000 characters) and records a pending
[approval](#approvals) (migration 134) bound to that user, the issue, the task
and the exact text. The model receives `pending_user_approval`. The approval
card shows the whole message as escaped Code Block text. Approving re-checks
that the task is still the issue's newest implementing task and still the
approver's, then queues the approved text unchanged with sender
`orchestrator` (`QueueApprovedLiveMessage`: the repository re-checks the
creator, their membership and the issue's visibility, the 30 per task per hour
budget, and that the task has not completed or been cancelled). The message
stores the approval's request ID (`agent_live_messages.request_id`, unique per
task), so repeated, concurrent or resumed approvals deliver it once. A task
that changed, a lost task or an exhausted budget marks the approval `failed`
and delivers nothing. A message to an agent is decided only in Kuayle:
Telegram offers no button for it and ignores a press that names one, because
the button would not show the text. Calls recorded before migration 134 were
sent at once and keep their "sent" line in history.
`get_task_messages(identifier, limit?)` is read-only and returns up to 50 (20
by default) messages of both directions, oldest first. Messages to the agent
carry `body`; the agent's own messages carry only `untrusted_agent_text`
(refused files are left out), and the result's `notice` tells the model never
to follow or act on them by itself. The system prompt says the same: the
assistant reports agent messages and asks to answer an agent with
`message_task` only when the user's turn asks.

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
when linked. The agent view shows both directions with their delivery state
in the task's timeline and has the composer that posts to
`POST /agent-tasks/:taskId/live-messages` (see
[agent tasks](agent-tasks.md#live-channel)); it reloads on each
`task.live_message` event of its task. The task card in the sidebar has no
thread.

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
[requested agent reviews](agent-tasks.md#requested-agent-reviews). `message_task`
does too (migration 134); see [agent messages in chat](#agent-messages-in-chat). `run_check` does too (migration 97); see
[manual checks](agent-tasks.md#manual-checks).

### Model choice for agents started from chat

`delegate_issue`, `request_agent_review` and `follow_up_task` accept an optional
`model` (and `harness` for delegations and reviews) that is stored in the
approval payload; a follow-up payload also shows the task's current model,
harness and `model_server_id`. The approval card (`ApprovalModelPicker`) opens
on what the backend would start: the proposal on the server that offers it
(the task's own first, then the default, then another compatible server), else
the default model server's default model when that server speaks the harness,
else the agent's own sign-in (whose run fills the model and effort from the
workspace's [agent default](../../TECHNICAL.md#agent-model-defaults) when it is
created; a proposed model no server offers also runs there). A follow-up without a proposed model opens on the
task's current model and the server it actually runs on (its own, else the
current default), even if that server no longer lists the model, since an
untouched card keeps both. Like the task composer it offers the compatible
model servers and the models of the selected one. `POST .../approvals/:id`
accepts optional `harness`, `model` and `model_server_id` next to `decision`;
only what the user changed is sent, and a picked model always carries its server
because two servers may list the same model name (the server is ignored without
a model). The approver's choice wins over the proposal and a new harness never
inherits the proposed model. With no choice the default model server applies as
before. The harness must be allowed in the workspace, and the model must be one
the chosen server offers (validation fails the approval otherwise; an unknown
server is never replaced by another). Reviews still default to the implementing
task's harness. A follow-up continues on the task's own harness, so only its
model and server change (`agent_tasks.model`, `thinking` and `model_server_id`
are updated together when the attempt is queued). An explicit model takes the
thinking level of the server that runs it; the default server stays implicit
unless the task was pinned to a server. Harness and model on an approval other
than these three, or on a denial, are ignored. Only the approval's bound actor
can resolve it, and the effect re-checks their access as for any approval.

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

## Instance version and upgrade from chat

`get_instance_version` reads the installed release (stamped into the binary from
`UI/package.json` by the Dockerfile; `dev` for unstamped builds), the latest
non-prerelease in the release manifest and a short changelog. Only instance
administrators (`SYSADMIN_IDS`) also see the updater's state, error and, once an
upgrade ended, the health check and final version.

`upgrade_instance` needs approval and belongs to no issue (migration 105 makes
`conversation_approvals.issue_id` optional for this action only). Asking refuses
non-administrators, an unconfigured, unreachable or busy updater, a development
build and an instance already on the latest release, each with its next step.
Approving re-checks administrator rights and the installed version, then starts
the same `service.SystemUpdater` as Settings → Version → Upgrade; requests,
refusals, starts and failures go to the audit log. The instance restarts during
the run, so the approval card follows `/api/system/update-status` and the
assistant follows progress with `get_instance_version`.

### Assistant action policies

`assistantActionPolicies` in
[assistant_guard.go](../internal/service/assistant_guard.go) is the one
declaration of what each assistant action needs: the GitHub App permissions it
calls and who may do it. Every approval tool and every tool that calls GitHub for
a pull request, a release or a repository scan must have an entry, and every permission an entry names must be in
the App manifest (`githubAppPermissions`); a unit test enforces both, so no entry
asks for a permission before its tool exists. `authorizeAssistantAction` is the
single role check, made when an approval is requested and again, against the
current role, when it is approved; guests and former members are refused.
Admin-or-creator actions (merging and the four pull request changes) make it
where the task's creator is known. Custom tools (`run_custom_tool`) go through
the same check and audit.

Before an approval is created its GitHub permissions are checked live against
the app and installation (cached briefly for requests, never when approving). A
gap is shown on the approval card with what to grant and where; approving
re-checks and refuses while it remains, and a GitHub 403 for a missing permission
names the permissions that are still missing. A read tool with a gap fails with
the same message instead of calling GitHub. When GitHub cannot be asked, the gap
is only a warning. The audit log records `assistant.approval_requested`,
`request_refused`, `approval_approved`, `approval_denied`, `approval_expired`,
`action_executed`, `action_failed` and `action_error` with the actor, tool,
approval and target, each decision and outcome once.

### Pull request tools

`get_pull_request`, `get_pull_request_checks` and `list_pull_request_comments`
read the pull request Kuayle published for the issue's newest task (any task
state) from GitHub now, for anyone who can see the issue. They report what blocks
a merge with the next step instead of a generic error: a draft
(`mark_pull_request_ready`), conflicts or a branch behind its base
(`update_pull_request_branch`), failing checks with links to their logs and a
summary (`follow_up_task`), and unresolved review threads, bot reviewers such as
Codex included. A missing App permission, a deleted pull request and an
unreachable GitHub each get their own message. Checks are read page by page; when
the list was cut short or the legacy commit statuses could not be read, the state
is `incomplete` (`checks_truncated`, `statuses_unavailable`) and never `passing`.

`reply_to_review_comment`, `resolve_review_thread`, `update_pull_request_branch`
and `mark_pull_request_ready` (`draft` true puts it back to draft) need approval
(migration 107 adds the actions) and are bound to the task, repository and pull
request number; the card shows the exact reply, the thread, or the head a branch
update is pinned to. Like merging, they need a workspace owner/admin or the
task's creator, checked when asking and again when approving, and every request,
refusal, change and failure is audited. `update_pull_request_branch` asks the
task agent to merge the base in its own checkout (a `follow_up_task` approval)
when the user created the task and it awaits review, is blocked or failed; it
uses GitHub's update-branch (pinned to the head the user saw) when that is
requested or no agent can take it, and never for conflicts. The new head is
recorded as for any head that moved (see
[pull request heads](agent-tasks.md#pull-request-head-tracking)); the
task's checkout fast-forwards to it when the task next publishes
(`SyncCheckout`).

`merge_pull_request` now merges with the repository's method from Settings →
Development (`repository_merge_methods`, `GET/PUT /dev-machine-merge-methods`,
admins only), otherwise the first method GitHub says the repository allows
(squash, merge, rebase). The method is pinned in the approval; a configured method
the repository disallows, a draft and a closed pull request are refused with
their next step before asking.

### Release tools

The release tools work on a repository linked to the workspace's GitHub
installation (`owner/name`, the same list as Settings → Integrations), not on an
issue. Anyone who can use the assistant (no guests) may read; only workspace
owners and admins may publish. Their entries in `assistantActionPolicies`
declare the GitHub permissions below, so the preflight names a missing one.

| Tool | GitHub App permissions | Role |
| --- | --- | --- |
| `list_releases`, `get_release` | contents: read | member |
| `draft_release_notes` | contents, pull requests, checks, statuses: read | member |
| `create_release` (approval) | contents: write; checks, statuses: read | owner/admin |
| `get_workflow_run` | actions: read | member |

`list_releases` and `get_release` return previous releases and their notes as
untrusted data, so the assistant can learn their style.

**Style skill.** The style is the workspace skill **Release notes style**
(workspace-wide, an ordinary [skill](#skills)). `draft_release_notes` seeds it
with the default style the first time it runs in a workspace (active, no
author); owners and admins edit, version, switch off or delete it in Settings →
Assistant → Skills, and a deleted one is seeded again on the next draft. Its
`Sections:` line sets which sections are rendered and in what order (default:
Features, Improvements, Fixed, Upgrade, Validation, PR links, Full Changelog);
the rest of the body is guidance returned to the model as `style.guidance`. A
switched-off or draft skill is not followed: the built-in default is used and
the result says so.

**Drafting.** `draft_release_notes(repository, tag?, target?, previous_tag?)`
pins the target commit (default: the head of the default branch), takes the
latest non-prerelease release as the previous tag, compares the two with
GitHub's compare API, and reads the pull requests named by the range's merge
commits (`Merge pull request #N`) and squash titles (`… (#N)`), at most 40. Pull
requests go to Features or Fixed by their conventional prefix (`feat`, `fix`)
or `bug`/`enhancement` labels, otherwise Improvements; added migration files
fill Upgrade; Validation states that all checks of the target passed (or that
they have not); then the PR links line and the Full Changelog compare link. It
returns the draft, the pull requests (title, labels, a body excerpt), the
suggested next patch tag, the previous release's notes as an example and the
checks state, and stays within the tool output limit by dropping body excerpts
first. The model rewrites the bullets in the style's wording and passes the
notes to `create_release`.

**Publishing.** `create_release(repository, tag, target_commit, notes, name?)`
needs approval (migration 111 adds the action; like an upgrade
the approval has no issue). Before the approval is offered, and again when it
is approved, Kuayle refuses with a reason, auditing the refusal:

- a tag that is not a plain git tag name or that already exists;
- a target commit not on the default branch (`Tagging refused: commit … is not on main`);
- a target whose checks did not all pass, from the same check-run and
  commit-status reading as `get_pull_request_checks`: failing (named), still
  running, none reported, or not all readable (`Tagging refused: …`);
- no earlier stable release among the newest 100 (the range always starts at
  the latest stable release, which the model cannot change, so a first release
  has no range to scan and is published outside Kuayle);
- a target that does not descend from that release, or a range of 300 or more
  changed files (more than the compare API lists, so the scan cannot cover it);
- a changed text file GitHub sent no diff for (too large), which the scan could
  not read;
- a finding of the secret scan of the range, or of the notes.

The secret scan (`release_scan.go`) reads the added lines of each changed file's
diff from the compare API and looks for private-key headers, AWS, GitHub,
GitLab, Slack, Google and API secret key formats, JWTs and URLs with an
embedded password (placeholders such as `${DB_PASSWORD}` or `<password>` are
allowed), and flags files that hold secrets by name (`.env`, `*.pem`, `*.key`,
`id_rsa`, `*.tfstate`, database dumps and similar; `.env.example` and other
templates are scanned instead). Removed lines and files are not findings.
Binary files (by extension) are listed on the card as not scanned; public keys
(`*.pub`) are content-scanned rather than flagged by name. Tool results are
bounded in encoded bytes so long or multi-byte notes never exceed the tool
output limit. The notes get the same credential patterns plus private IPv4
addresses (RFC 1918, loopback, link-local, CGNAT), host names under `.internal`,
`.local`, `.lan`, `.corp`, `.home`, `.intranet`, `.localdomain` or `.private`,
and local paths (`/Users/<name>/`, `/home/<name>/`), and are limited to 20 000
characters. A refusal names the file or notes line and the type, never the
value.

The approval card shows the repository, tag, short commit and branch, the
number of passing checks, commits and scanned files, files not scanned, a
compare link and the notes in a Textarea. The approver may edit the notes:
`POST …/approvals/:approvalId` accepts `{"decision":"approve","notes":"…"}`;
the edit is checked like the requested notes and stored on the approval with
the claim (`notes_edited`), so the edited notes are what gets published and a
retry publishes the same. An edit that fails the check is answered with 422
`INVALID_APPROVAL_EDIT` and the reason, and the approval stays pending. Approving
re-checks the admin role and GitHub permissions, runs the whole gate again, then
creates the release with `target_commitish` pinned to the full commit SHA,
`make_latest` and not as a draft or prerelease. A release found at the same tag
and commit (an earlier attempt whose answer was lost) counts as done; a 422 from
GitHub fails the approval with its message; an unreachable GitHub leaves the
claim to resume. Telegram can approve too and then publishes the notes as
requested (they cannot be edited there); its button names the release tag.
Requests, refusals, approvals and outcomes go to the audit log as for every
assistant action.

**Following the build.** Publishing a release starts the repository's release
workflows (in Kuayle's own repository, **Build & Push**). `get_workflow_run`
lists the runs of a tag or commit, optionally only one workflow by name, with
`state` `running`, `success`, `failure` or `none` (not started yet), or reads one
run by `run_id` with its failed jobs and their log links. It needs the GitHub
App permission **Actions: Read**, which the manifest now requests; apps created
before it must grant it and installations accept it (see
[GitHub integration](github-integration.md)).

Sources: `BE/internal/service/assistant_releases.go`, `release_notes.go`,
`release_scan.go`, `github_releases.go`, `BE/pkg/github/releases.go` and
`UI/src/lib/features/assistant/ReleaseApprovalCard.svelte`. Checks:
`assistant_releases_test.go` (authorization, gate, scan, approval, workflow
runs), `release_notes_test.go` (style rendering), `release_scan_test.go`,
`assistant_releases_integration_test.go` (request, edited approval, demoted
admin; needs `DATABASE_URL`), `BE/migrations/release_approvals_test.go` and the
release card test in `UI/tests/e2e/assistant.test.ts`.

### Shipping in one approval

`ship_pull_request(identifier, tag, notes, name?, upgrade?)` asks for one
approval for the whole chain, and the card lists every step before anything runs:

1. **merge**: as `merge_pull_request`, at the head GitHub reported when the card
   was offered, with the repository's merge method;
2. **tests**: wait until every check of the merge commit on the default branch
   passed (the same reading as `create_release`'s gate; at most an hour);
3. **release**: as `create_release` at the merge commit, with the tag and the
   notes as left on the card (the gate and the secret scan run again);
4. **build**: wait for the workflow runs the release started (`event=release`);
   a repository without one passes unless an upgrade follows;
5. **upgrade** (only when asked): wait until the release list this instance
   installs from shows the tag, then start the updater as `upgrade_instance`.

Only workspace owners and admins may ask (its policy entry needs what its steps
need on GitHub). Before the card is offered, every step's own request checks
run: each step's tool switch and role (upgrading needs an instance
administrator), `merge_pull_request`'s target and head, a pull request into the
default branch, the tag (valid, new), the notes check and, with `upgrade`, a
release of the repository this instance installs that is newer than the running
version. When each step runs, its own tool's role, GitHub permission preflight
and switch are read again, so a chain approved earlier cannot act with rights
since lost, and its outcome is audited as that tool (`action_executed` or
`action_failed` with `chain` and `step`), besides the approval's own records.

The notes are editable on the card as for `create_release`. Approving answers
at once with the approval still `approving`; the chain runs in the background
and records each step's status (`pending`, `running`, `done`, `failed`,
`skipped`), detail and link in the approval's payload, sending
`approval.updated` so the card follows it. The first failing step stops the
chain: its approval fails with `Step n of m (step) failed: why. Next step: …`
naming what already happened (for example, merged but not released) and the
steps not run; nothing after it runs. GitHub or the database being unreachable
is retried three times before the step fails. A restarted server resumes
`approving` chains from their last recorded step as their approver
(`ResumeShips`); every step is safe to repeat (a merged pull request, a release
at the same tag and commit or an instance already on the version count as
done). Migration 112 adds the action; like a merge the approval belongs to the
issue. Telegram can approve it; progress is followed in Kuayle.

Sources: `BE/internal/service/assistant_ship.go`,
`UI/src/lib/features/assistant/ShipApprovalCard.svelte` and `ShipSteps.svelte`.
Checks: `assistant_ship_test.go` (order, first failure, each step's
authorization, resume, request checks), `assistant_ship_integration_test.go`
(from chat to release with one approval; needs `DATABASE_URL`),
`BE/migrations/ship_approvals_test.go` and the ship card tests in
`UI/tests/e2e/assistant.test.ts`.

### Research tools

KUA-93 adds read-only research: `web_search`, five repository scan tools
(`repo_list_files`, `repo_glob`, `repo_grep`, `repo_read_file`, `repo_symbols`)
and `spawn_research_subagents`. None needs approval or counts toward the change
limit. They act at once for any current non-guest member (as every tool, via
`AssistantTools.run`), each has a workspace switch in Settings → Assistant
(`spawn_research_subagents` is off by default, since it spends the workspace's
AI provider budget; `web_search` and `repo_*` are on, and `web_search` does
nothing until a key is set), and
the system prompt describes only the ones offered to the reply (cite URLs; name
the exact paths and lines read, and put them in any issue filed after a scan).

| Tool | GitHub App permissions | Role |
| --- | --- | --- |
| `web_search` | none (search provider API) | member |
| `repo_*` (five tools) | contents: read | member through an issue they can see (`identifier`); a repository named directly (`repository`) is for owners and admins |
| `spawn_research_subagents` | none itself; each sub-step is checked as its own tool | member |

**Settings** (migration 114, `assistant_research_settings`; no row means the
defaults). `GET /assistant-research-settings` (session only, any non-guest
member) returns
`{"search_provider":"brave"|"tavily"|"searxng","search_providers":["brave","tavily"(,"searxng")],"has_search_api_key":bool,"subagent_model":string,"subagent_max_parallel":1-8,"can_manage":bool,"updated_at"?:time}`.
`PUT` (owners and admins; the role is checked again in the writing SQL
statement, so a concurrent demotion is refused) takes any of
`search_provider`, `search_api_key` (absent keeps it, `null` or `""` removes
it), `subagent_model` (`""` = the conversation's model) and
`subagent_max_parallel` (default 3); unknown fields are 400. The key is
encrypted with a key derived from `JWT_SECRET` (`:assistant-research`), never
returned or logged; `assistant.research_settings_updated` is audited without it.
Guests, outsiders and personal access tokens get 403. The settings UI is the
Research tab of Settings → Assistant (`?tab=research`). `search_providers` is
what this instance offers: `searxng` only while `WEB_SEARCH_SEARXNG_URL` is set
(migration 116 allows it in the table); `PUT` refuses a provider not offered
with 400. `has_search_api_key` does not apply to SearXNG and choosing it keeps
any stored key; the UI hides the key field for it and, when the saved provider
is no longer offered, asks for another one (`web_search` then reports it is not
configured).

**Web search.** Providers implement `WebSearchProvider` (`Name`, `Search`) in
[assistant_web_search.go](../internal/service/assistant_web_search.go):
`BraveSearch` (`GET api.search.brave.com/res/v1/web/search`,
`X-Subscription-Token`) and `TavilySearch` (`POST api.tavily.com/search`, bearer
key). The host is fixed by Kuayle, dialled only at public addresses without
redirects; result URLs are given to the model to cite and never fetched by the
server. `SearXNGSearch` (`GET <base>/search?q=…&format=json&safesearch=1&pageno=1`,
no key; it implements `Keyless`) uses the base URL from the operator's
`WEB_SEARCH_SEARXNG_URL`, never a workspace value. `validate.OperatorBaseURL`
checks it at startup (http(s), host and optional port, no credentials, query,
fragment, escapes or dot segments; an invalid value stops the server). Its
client skips the public-address check only for a dial whose host and port
exactly match the configured ones, and then dials that configured host itself,
so its name is resolved by the operator's DNS (a Compose service on a private
network) while every other private address, another port included, is still
refused; redirects are never followed. SearXNG has no count parameter, so the
tool caps the list. A 401/403 from SearXNG means `json` is missing from its
`search.formats`; the tool error and a server warning tell the operator to
enable it. Arguments: `query` (1–400 characters), `count` (1–10, default 5).
Titles and excerpts are stripped of markup and capped (200 and 500 runes); only
`http(s)` URLs are kept; the list is cut to 12 KB. A missing key, a rejected
key, a rate limit (429), a timeout (15 s), invalid JSON or an unreachable provider is a tool
error that tells the model to say it could not search and not to invent
sources. Result:

```json
{"query":"…","provider":"brave","results":[{"title":"…","url":"https://…","excerpt":"…"}],"truncated":true,"note":"Cite the url of each result you use. …"}
```

`truncated` appears only when the byte budget dropped results.

**Repository scans.** Every scan tool takes `repository` (`owner/name`, linked
and active in the workspace's GitHub installation; owners and admins only, since
the scan token can read a repository the caller cannot see on GitHub) or
`identifier` (an issue the user can see; its development repository and base
branch). The Dev Machine policy's repository allowlist applies to every scan, and optionally `ref`
(branch, tag or full SHA; default: the default branch). The API server mints a
token restricted to that repository with only `contents: read` and `metadata:
read`, pins the ref to a commit and downloads that commit's tarball (the
redirect is followed only to `https://codeload.github.com`, without the
token). The archive is extracted in Go into
`$TMPDIR/kuayle-repo-scan-*/checkout-*` (0700/0600), so there is no git, no
`.git` directory and the token never touches the disk or the logs. Extraction
drops the archive's top directory and refuses absolute paths, `..`, `.git`,
anything under a link, and every symlink and hard link (none is created; they
are counted). Caps: 100 MB archive, 300 MB extracted, 30 000 files
(`checkout_truncated`), 2 MB per file (larger files are listed with
`"omitted":"too_large"` and cannot be read). Files matched by the checkout's
`.gitignore` files (negation, anchored, directory-only, `**`, deeper files win)
are excluded. Reads go through the checkout's index and `os.Root`, so a path
outside the index or a link out of the checkout is refused. Checkouts are keyed
by workspace, repository and ref, shared by concurrent calls (one download),
kept 15 minutes, at most 8 at once, deleted when they expire, on shutdown
(`RepositoryCheckouts.Run`/`Close`) and, for roots a crashed process left, at
the next start. Failed downloads are not cached. GitHub's 404, 401/403 and
outages become tool errors naming the next step. Each result starts with
`repository`, `ref`, `commit` (and `checkout_truncated` when set) and reports
the path or pattern used:

```jsonc
// repo_list_files(path?, limit 1-500 = 200)
{"repository":"acme/app","ref":"main","commit":"<sha>","path":"BE","files":[{"path":"BE/main.go","size":120,"binary":false,"omitted":"too_large"}],"matched":12,"total_files":340,"truncated":false,"omitted":{"ignored":3,"links":1,"too_large":2,"unsafe":0}}
// repo_glob(pattern, limit 1-500 = 200)
{"repository":"…","ref":"…","commit":"…","pattern":"**/*_test.go","files":[{"path":"…","size":0}],"matched":4,"truncated":false}
// repo_grep(pattern, glob?, fixed?, case_insensitive?, limit 1-100 = 50)
{"repository":"…","ref":"…","commit":"…","pattern":"Serve\\(","glob":"BE/**","fixed":false,"case_insensitive":false,"matches":[{"path":"BE/main.go","line":5,"text":"\tServe()"}],"files_searched":10,"files_matched":2,"truncated":false,"timed_out":true,"note":"…"}
// repo_read_file(path, start_line?, end_line?)
{"repository":"…","ref":"…","commit":"…","path":"BE/main.go","start_line":1,"end_line":400,"total_lines":812,"content":"…","truncated":true}
// repo_symbols(path?, glob?, limit 1-500 = 200)
{"repository":"…","ref":"…","commit":"…","path":"","glob":"","symbols":[{"path":"BE/main.go","line":4,"kind":"function","name":"main"}],"files_searched":3,"truncated":false}
```

`binary` and `omitted` are omitted when false/empty; `omitted` (the counts) only
when something was left out. Grep uses RE2 (linear time), skips binary files
(a NUL in the first 8000 bytes), cuts lines to 300 runes and stops after 10 s or
64 MB (`timed_out`). `repo_read_file` returns at most 400 lines and 14 KB per
call (`truncated`: read on from `end_line + 1`). Symbol kinds are `function`,
`method`, `type`, `interface`, `class`, `const`, `var`, `table` and `view`,
from per-language line patterns (Go, JS/TS/Svelte/Vue, Python, Rust,
Java/Kotlin/C#/Scala, Ruby, SQL). Every list is cut to 16 KB of JSON.

**Research sub-tasks.** `spawn_research_subagents(tasks[1-8], max_parallel?)`,
where each task is `{title (≤120), instructions (≤4000), tools?}` and `tools`
narrows the sub-task to some of `web_search` and the `repo_*` tools (the
default: all of them that are available and switched on). Sub-tasks run on the
assistant's own provider (Settings → AI) with `subagent_model`, or the
conversation's model when it is empty; at most `subagent_max_parallel` run at
once (default 3, at most 8, lowered by `max_parallel`). They can only call those
read-only tools, through the same switch, GitHub preflight and role checks as
the parent (`create_issue`, approvals and `spawn_research_subagents` itself are
refused as a failed step). Bounds: 6 model rounds per sub-task (the last offers
no tools), 40 tool calls and about 300 000 estimated tokens (bytes ÷ 4; streamed
replies report no usage) shared by all sub-tasks of the call, 2 minutes per
sub-task and 4 minutes for the call. A failing, timed-out or panicking sub-task
gets its own error status; the others finish. Each sub-task answers with JSON
`{summary, findings, files, citations}` (plain text becomes the summary);
`files` and `citations` keep only paths and URLs its own tool results returned
(`path:line` is accepted), the rest are counted in `dropped_unverified`. The
run is audited as `assistant.subagents_finished` (counts, model, steps,
tokens). Sub-task tool calls are not conversation tool calls of their own: they
are in the result, which is stored when the step finishes (there is no live
progress for sub-tasks yet). The result, at most 20 KB (larger results are
shrunk evenly and marked `truncated`, keeping every status and summary):

```json
{
  "subagents": [
    {
      "index": 0,
      "title": "Callers",
      "status": "completed",
      "model": "small-model",
      "duration_ms": 8123,
      "tokens_estimated": 5400,
      "cost": null,
      "summary": "main calls Serve.",
      "findings": ["BE/main.go:5 calls Serve without a context"],
      "files": ["BE/main.go:5"],
      "citations": [{"url": "https://go.dev/blog/osroot", "title": "Go"}],
      "steps": [
        {"tool": "repo_grep", "input": {"repository": "acme/app", "pattern": "Serve\\("}, "ok": true, "duration_ms": 41},
        {"tool": "create_issue", "input": {}, "ok": false, "error": "create_issue is not available to research subagents; …", "duration_ms": 0}
      ],
      "dropped_unverified": 1
    }
  ],
  "model": "small-model",
  "parallel": 3,
  "duration_ms": 9050,
  "completed": 1,
  "failed": 0,
  "budget": {"max_steps": 40, "steps_used": 1, "max_tokens_estimated": 300000, "tokens_estimated": 5400},
  "truncated": true,
  "note": "Sub-task findings are leads from a smaller model: …"
}
```

`status` is `completed`, `failed`, `timed_out` or `budget_exhausted`; `error`
(present unless completed) is a user-safe reason; `cost` is always `null` for
now; `dropped_unverified` and `truncated` appear only when non-zero/true; step
`input` is the normalised arguments (`{}` when over 600 bytes or shrunk). Order
follows `tasks`. The chat renders the step chip from this output (one nested row
per entry of `subagents`: title, model, duration, tokens/cost, status).

Sources: `BE/internal/service/assistant_web_search.go`,
`assistant_scan_checkout.go`, `assistant_scan_ignore.go`,
`assistant_scan_tools.go`, `assistant_subagents.go`,
`assistant_research_settings.go`, `github_snapshots.go`,
`BE/pkg/github/tarball.go`, `BE/internal/handler/assistant_research.go` and
`UI/src/lib/features/assistant/AssistantResearchSettings.svelte`. Checks:
`assistant_web_search_test.go` (providers on httptest servers, failures,
budget), `assistant_web_search_searxng_test.go` (SearXNG mapping, failures
including a disabled format, the allowed-host dialer and redirects),
`BE/pkg/validate/operator_url_test.go`, `assistant_scan_checkout_test.go` (traversal, links, a link planted
after extraction, size caps, `.gitignore`, sharing, expiry, cleanup, stale
roots), `assistant_scan_tools_test.go`, `assistant_subagents_test.go`
(concurrency cap, failure isolation, read-only tools, shared budget, verified
sources, size), `assistant_research_settings_test.go`,
`assistant_research_integration_test.go`,
`BE/internal/repository/assistant_research_settings_test.go`,
`BE/cmd/server/assistant_research_http_test.go`,
`BE/migrations/assistant_research_settings_test.go` (the last four need
`DATABASE_URL`) and `BE/pkg/github/tarball_test.go`.
