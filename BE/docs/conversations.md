# Persistent assistant conversations

The Global Assistant at `/:workspaceSlug/assistant` provides persistent chat with
the workspace's configured OpenAI-compatible provider, including DeepSeek
Chat Completions endpoints. The workspace owner configures the existing AI
settings (base URL, model and encrypted key). Removing the key disables new
requests; an already running request finishes or can be stopped.

## Current scope

This first vertical slice is conversational. It does not yet expose issue,
repository, machine, approval or publishing tools to the model. The system
instruction makes that limitation explicit. Conversations currently belong to
their creator, who must remain a non-guest workspace member. Two browsers signed
in as that user can view the same stream. Conversation sharing and issue/project
associations must add authorization predicates before widening this audience.

The native terminal, IDE, issue workflows and existing AI description expansion
remain separate and retain their existing behavior. There is no new container
or orchestration service.

## API and persistence

All routes below use authenticated sessions and current workspace membership;
PATs are deliberately denied until dedicated chat scopes are defined.

| Method | Workspace-relative path | Purpose |
| --- | --- | --- |
| GET | `/assistant/status` | Configured provider/model status, never the key |
| GET / POST | `/conversations` | List the latest 100 conversations / create one |
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

## Execution and recovery

The existing API process runs four lightweight generation workers. PostgreSQL
`SKIP LOCKED` claims and a lease token fence every mutation. A browser refresh,
reconnection, duplicate POST, or additional API process cannot claim the same
attempt. Requests use the provider's streaming Chat Completions API. The worker
persists deltas in batches (200 ms or 1 KiB) and checks cancellation/membership
while awaiting provider output. No tools or direct infrastructure access are
provided to the model.

An attempt has a two-minute lease and a provider timeout of 90 seconds. A worker
that loses its lease cannot append or complete. Recovery marks expired attempts
`interrupted`; it never automatically repeats an ambiguous paid provider call.
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
  retention configuration, uploads and richer task artifacts are later phases;
  this slice does not claim those capabilities.

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
provides a latest-message button. The composer supports Enter/Shift+Enter,
per-user/conversation drafts, send, stop and explicit retry. All strings use the
four existing Paraglide catalogs. Desktop conversation history collapses into a
mobile disclosure.

Focused checks, in addition to the repository testing skill's area checks:

```sh
# BE/; DATABASE_URL must identify a disposable migrated PostgreSQL database.
go test ./internal/service -run 'TestAssistant|TestAICompletion' -race -count=1
go test ./internal/repository -run TestConversations -race -count=1
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

## Next phases

The full platform specification remains the objective: issue creation/delegation,
AgentTask state transitions, automatically prepared worktrees, native resumable
sessions, structured execution traces, persisted approvals and GitHub review,
then synchronized Telegram and authenticated QA previews. Telegram settings must
include an encrypted bot token with configure/replace/remove and safe status
responses, plus an allowlist of numeric Telegram user IDs enforced for messages
and callbacks. Allowlisted identities must still map to authorized Kuayle users.
Resource-aware scheduling and advanced automation follow only after
those workflows are reliable.
