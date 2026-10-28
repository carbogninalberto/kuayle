# Agent task execution contract

## Implementation status

The task domain, repository, worker and HTTP controls are wired into the API
process. Issue conversations expose task delegation, retry, cancellation and live
activity through AI Elements Task/Trace and Model Selector compositions. The assistant remains
conversational; structured orchestrator delegation is still pending. Native
success is followed by an independent check (migration 58) and deterministic
publication of a pull request (migration 59), after which the task waits in
`awaiting_review` until GitHub reports that pull request merged. Review feedback
handling and CI tracking are not yet implemented. This is not a completed autonomous development workflow.

Migration 56 adds tasks and transition history, task-specific checkout scopes,
immutable native-attempt ownership, encrypted recovery credentials and log-source
identity. Existing manual checkouts use an empty scope. Ordinary checkout reads
expose only that manual scope; internal task preparation reads its own scope. Manual native
run creation cannot target a task-owned checkout, including a known checkout ID.
Task work remains under task authorization through delegation/retry controls. Task
worktrees use the task UUID, so two tasks for the same issue do not share a branch or directory.
Rollback refuses active tasks, retained task worktrees and native attempts still
using recovery credentials; it does not silently orphan execution.

## Test command configuration

Migration 57 adds a nullable `test_command` override to development defaults and
tasks, plus the effective `validation_command` snapshot on each task attempt.
The Settings, team, project and issue development forms accept a JSON argv array;
task delegation and retry forms can override it. Omitted/null inherits; `[]`
explicitly means no configured check. Commands are bounded to 64 arguments,
8 KiB per argument and 32 KiB total; empty executables and NUL are rejected.

New accepted intents resolve issue → project → team → workspace within the
existing authorized task transaction. The immutable queued receipt retains both
the requested override and effective command. A replay compares the original
override, including the distinction between inherit and `[]`, and returns the
original snapshot even after defaults change. A new retry resolves current
defaults unless it explicitly supplies an override. Existing attempts migrate
with no configured command; later defaults are never retroactively applied.
Rollback refuses configured overrides or nonempty effective snapshots.

The native attempt receives this command in its instructions and existing
`test_command` field. That is advisory only: the outcome used by the task is the
independent check below. Missing commands are not evidence of success.

## Independent validation

Migration 58 adds the `validate_agent` machine operation. In `validating`, the
worker queues one `validation` step (sequence 0) on the succeeded native attempt,
with the attempt's effective command snapshot, inside the fenced task transaction.
It uses the same workspace/machine advisory locks as native run creation and
requires a running machine, enabled policy and no active native run. An empty
snapshot records a `skipped` step and blocks the task; it is never a pass.

The manager runs the argv directly (no shell, harness bootstrap or provider
parsing) in a deterministic `-validation-<run>` container from the machine's
developer image, on the task checkout, under the hardened agent host config,
egress proxy and `CI=true`. No model, GitHub or runtime credentials are copied
into it; the attempt's encrypted snapshot is decoded only to redact output.
Output is stored as log chunks whose `service_id` is the step ID. Every step and
output write re-verifies the task (still `validating`, same run, creator, machine
generation, enabled policy, succeeded attempt) and the operation's live lease.
The deadline starts at the step's first start, so restarts cannot extend it.
Recovery replays an already exited container's result instead of rerunning it,
after checking image, argv, workdir and ownership labels.

The process exit code alone decides the step: 0 succeeds, anything else fails.
Moving to `publishing` re-checks a succeeded step with exit code 0 and a completed
operation in the same transaction. A failed or cancelled check goes to cleanup
and `failed`; an exhausted infrastructure operation marks the step failed rather
than leaving it queued. Cancellation marks open steps cancelled. Every step change
emits a `task.tests_updated` conversation event with `{task_id, run_id, step}`.
Machines with a task in `validating` or `publishing` count as active for
pause/stop and new runs. Rollback refuses retained validation operations or steps.

Migration 67 keeps QA evidence. After the check exits, and before it completes
under the same lease, the manager copies regular files from the check
container's `playwright-report/` and `test-results/` into
`agent_validation_artifacts`. Only png, jpeg, zip, html, json, xml, txt and webm
files are kept, with at most 5 MB each, 20 MB in total and 100 files. Symlinks,
traversal paths and anything outside those directories are dropped. Collection
is best-effort and never changes the check's result. Downloads are session-only
and task-authorized. They are served as attachments (png/jpeg inline) with
`nosniff`, a sandbox CSP and `no-store`, and the UI always saves them as files,
so a report's HTML never runs on the app origin.

### Compose services for checks

Migration 68 adds an inherited `compose_file` setting: a repository-relative
`.yml`/`.yaml` path without dot segments. It is snapshotted onto the task and
its check step. The agent can edit the file in the checkout, so it is treated as
untrusted. The manager reads it as a regular file of at most 64 KiB through the
created, not yet started, check container, and never runs `docker compose`.
Only this subset is accepted:

- at most 8 services with `image` (no `build`), `environment`, `command`,
  `healthcheck`, `depends_on` (`service_started`/`service_healthy`), `user` and
  `working_dir`;
- `ports`, `expose` and `restart` are accepted but ignored, so nothing is ever
  published;
- anything else, such as volumes, privileges, capabilities, devices,
  `network_mode`, `env_file`, `include`, anchors or required variables, is rejected;
- variables interpolate with an empty environment (`${A:-x}` becomes `x`), so no
  host or manager value can leak in;
- images must match `DEV_MACHINE_COMPOSE_IMAGES` (default `docker.io/library`),
  because pulls run on the host daemon outside the egress proxy.

Services start in dependency order on a per-check internal network, with the
service name as their DNS alias. Their containers have no mounts, published
ports or privileges, drop all capabilities except CHOWN, DAC_OVERRIDE, FOWNER,
SETGID and SETUID (needed by database entrypoints), run with no-new-privileges,
and share 30% of the machine's CPU, memory and PIDs. Every health-checked
service must become healthy within 3 minutes. The check container then joins
that network and runs.

Afterwards the manager removes the services and network. Cancellation and
teardown remove them by label too. A retry before the check starts reuses the
same deterministic containers, and a check that already ran is only replayed.
A rejected or unhealthy stack fails the check without retries, and its reason is
written to the check output and the step summary.


## Publication

Task attempts never receive a GitHub credential (`push_branch` is false), so the
harness cannot push or open pull requests itself. In `publishing`, the worker
queues one `publication` step (sequence 1) and a `publish_agent` operation in the
fenced task transaction, using the same machine locks as validation. The manager
re-verifies the same phase authority (task still `publishing`, current run,
creator, machine generation, enabled policy, live operation lease) on every read
and write, checks that the checkout is the task's own ready checkout, then:

1. Commits pending changes in the developer container **without any credential**,
   as `DEV_MACHINE_AGENT_GIT_NAME <DEV_MACHINE_AGENT_GIT_EMAIL>` (default
   `Kuayle Agent <agent@kuayle.invalid>`), with repository hooks, fsmonitor and
   signing programs disabled and system/global configuration ignored. The message
   is `<ISSUE-ID>: <issue title>`. A branch with no commits ahead of the base is
   recorded as `skipped` and blocks the task; it is never reviewable.
2. Fetches exactly that branch tip into a new temporary repository outside the
   workspace volume, verifies it is still the recorded commit, and only then reads
   a fresh repository-scoped GitHub App token from stdin. It pushes the commit to
   the fixed `https://github.com/<checkout repository>` URL without force. The
   agent-writable checkout's configuration (hooks, URL rewrites, credential
   helpers) is never loaded while the token exists, and the token is never written
   to the workspace volume.
3. Reuses an open pull request from the task branch into the base branch, or opens
   one with the GitHub App. The title carries the issue identifier so the existing
   `pr_opened` GitHub automation moves the issue to In Review; publication does not
   change the issue status itself. The agent's summary is included only as fenced,
   explicitly unverified text. GitHub's URL must belong to the checkout repository.

The branch, commit and pull request git refs, the run's `branch`/`pull_request_url`
and the succeeded step commit together with a `task.publication_updated` event.
Moving to `awaiting_review` re-checks a succeeded publication step and a completed
operation in the same transaction, so an agent-reported URL never reaches review.
Retries after a lost result recommit nothing, push the same commit and reuse the
open pull request. Push or GitHub errors are retried up to the operation limit with
the token redacted; exhausted or terminal failures mark the step failed and block
the task with its validated work intact. Cancellation marks open steps cancelled.
Agent-controlled git configuration can still run programs during the
credential-free commit step (for example clean filters); that runs with the same
privileges the agent already had and no publication credential.

## Agent sandboxing

Autonomous agents cannot answer permission prompts, and their own sandboxes
conflict with the hardened container. The container is the boundary: all
capabilities dropped, no-new-privileges, the egress proxy, and only the task's
checkout and credentials.

- **Codex** runs with `sandbox_mode="danger-full-access"` and
  `approval_policy="never"`. Its workspace-write sandbox uses bubblewrap, which
  needs user namespaces that the container forbids; in a real run every command
  failed.
- **Claude Code** runs with `--permission-mode bypassPermissions`. In its
  default mode every edit and write was denied in a real run, and the attempt
  ended without changes.

## Native session resume and handoff

Codex and Claude Code task attempts keep their native session **transcripts** in
`/workspace/.kuayle/agent-sessions/<task-id>/` on the machine's workspace volume,
outside every checkout, so publication never commits them. The attempt's argv is
wrapped so that, after the image's runner has loaded secrets and configuration,
only `~/.codex/sessions` and `~/.claude/projects` are linked to that directory.
`config.toml`, auth files and all credentials stay on the container's tmpfs home
and are never persisted. Transcripts can contain whatever the agent saw or
printed; they share the machine volume's trust boundary (the same as the task's
worktree) and are wiped as described below.

Claude Code runs receive a Kuayle-assigned `--session-id`; Codex reports its own
`thread_id` in its `thread.started` event, which the manager records once.
Migration 64 stores the ID on the attempt (`native_session_id`, restricted to
`[A-Za-z0-9-]` and never starting with `-`, so it cannot be read as a flag). A
retry or follow-up **resumes natively** (`codex exec resume <id>` with the same
settings as a new session, or `claude --resume <id>`) only when
the previous attempt succeeded on the same machine, where its transcripts live.
Otherwise it starts a new session, so a failed resume is never repeated. Either
way the prompt keeps the task instructions and appends a bounded handoff naming
the previous attempt, its native outcome, its independent check result and exit
code, a recorded pull request, up to 50 changed files and the previous agent's
summary fenced as unverified text, because the harness never saw Kuayle's
independent check or publication. An older handoff is replaced, not accumulated,
and the prompt stays within the attempt limit. Other harnesses always start new
sessions with the handoff.

When a task completes or is cancelled, a `purge_task_session` operation removes
its directory with a short-lived helper container that has no network, a
read-only root, no capabilities and only `rm -rf -- <that directory>`; it works
while the machine is paused or stopped, and still runs after the machine's
generation changes. Failed tasks keep their transcripts so an explicit retry can
use them. Machine teardown removes the whole volume.

## CI and review feedback

Migration 62 adds `agent_task_signals`. Completed `check_suite` events and
submitted `pull_request_review` events for a linked, active repository are
recorded for each task whose **current** attempt recorded that pull request; CI
also has to target the attempt's published commit, so results for older commits
are ignored. The repository name comes from Kuayle's link, never the payload.
Each external event is recorded once (redelivery updates the stored outcome) and
emits `task.signal_recorded`. Outcomes are normalized identifiers; review text is
bounded, untrusted data. `GET /agent-tasks/:taskId/signals` lists them under
current task permissions, and the task card shows them in its execution details.

While the task is `awaiting_review`, a CI `failure`/`timed_out` or a
`changes_requested` review queues a follow-up attempt on the same branch through
the existing retry path, acting as the task creator under their current
permissions. Only reviewers with an `OWNER`, `MEMBER` or `COLLABORATOR`
association can trigger one; other reviews are recorded as `untrusted`. Budgets
are two automatic CI fixes and five review follow-ups per task; further signals
are recorded as `budget_exhausted`. The follow-up keeps the original
instructions, replaces any earlier follow-up section, and explains the failing
suite or the fenced review text. Its request ID is derived from the task,
attempt and event, so redelivery and recovery replay the same attempt. A
permanent refusal records `follow_up_failed`. Because GitHub does not redeliver
failed webhooks automatically, a background loop resumes follow-ups left
unqueued by transient errors after 30 seconds. CI logs are not fetched: the
agent has no GitHub credential and must reproduce failures with the configured
test command. The new attempt then revalidates and republishes to the same pull
request.

### Kuayle issue comments

Migration 80 adds the `comment` signal kind. A comment posted on an issue in a
signed-in session (not with a personal access token, and not by the assistant
or an agent acting for a user) is feedback when the issue's latest task is
`awaiting_review`. Under the task lock the repository re-checks that the author
is a current non-guest member who can see the issue; other comments, comments on
tasks in any other state and empty comments are not recorded. The comment is
stored as bounded plain text (2,000 characters) and queues a follow-up attempt
through the same retry path, acting as the task creator, with the comment fenced
as untrusted feedback in the prompt. The budget is five comment follow-ups per
task (`budget_exhausted` after that); the request ID is derived from the task,
attempt and comment, and unqueued follow-ups are resumed by the same background
loop. Because a follow-up moves the task out of `awaiting_review`, further
comments wait for the next published attempt. Comment creation itself never
fails because of this hook.

## Review completion

GitHub remains the authority for merges, and merging requires the repository's
own human review rules. When a `pull_request` webhook for a linked, active
repository reports `merged`, each task in `awaiting_review` whose **current**
attempt recorded that repository and pull request number moves to `completed`
with no user actor and the reason `Pull request #N was merged on GitHub`. The
repository name comes from Kuayle's repository link, never from the webhook body.
Each task is re-checked under its own lock, so a concurrent follow-up attempt or a
redelivered webhook completes nothing. Closing without merging changes nothing;
the task stays reviewable and can be retried. The issue's own status follows the
existing `pr_merged` GitHub automation.

GitHub does not redeliver failed webhooks, and a relay or outage can lose
them. Every two minutes the API asks GitHub, with the workspace's
repository-scoped installation token, about the recorded pull requests of tasks
still in `awaiting_review`. A merged one goes through the same completion path
as the webhook, so both paths converge and neither completes a task twice. A
pull request that cannot be checked is retried on the next pass without
blocking the others.

## Authorization and side effects

Tasks belong to their creator's private issue conversation and original team.
Reads, requests, transitions, machine reservations, checkout creation and native
attempt dispatch require the current conversation, issue/team visibility and
non-guest workspace membership. A moved or deleted issue does not convert its
old task into a general conversation or unrestricted manual run.

Transactions acquire the privacy graph barrier, issue, conversation, membership
and task locks in the existing conversation order. Workers claim with
`SKIP LOCKED`; a generation, lease token and expiry fence their writes. Reserved
machine, checkout and run IDs are allocated before side effects. Duplicate
request IDs must have identical input. Creation and retry replay return the
immutable queued snapshot stored by their accepted transition, even after a later
attempt or configuration change. Replay still checks current conversation access;
it does not authorize new execution. These transition events are request receipts
and must be retained while the task exists. Missing receipt data fails closed.
State changes, audit records and conversation events commit together.

Native attempts retain `task_id` even after a task switches attempts. The manager
checks that the attempt remains the task's current run before launch and before
persisting output. Missing, revoked, cancelling and superseded tasks deny effects.
Cleanup can still lock and cancel after issue moves, membership removal or chat
deletion. Cancellation is not complete merely because a run's status changed:
the task requires the durable cancel operation to finish and pending preparation
operations to settle. Runtime cancellation removes every container associated
with the run after verifying its workspace and machine ownership. Already removed
containers are harmless; failures on one container do not prevent cleanup of the
others, and any remaining cleanup error prevents the operation from succeeding.

## Failure, cancellation and explicit retries

Unexpected worker errors back off and are counted durably. Five consecutive
failures move the task to `cancelling`; capacity waits and stale leases are not
failures, and a successful step resets the counter. A native failure also enters
cleanup first. Only confirmed cleanup produces the terminal `failed` state.
Exhausted cleanup stays visibly pending rather than claiming the process stopped.
Configuration failures after reserving an environment also enter cleanup: a
preparing task may already have started native execution between worker polls.
An authorized repeat cancellation request can retry failed cleanup operations
once their underlying cause has been repaired.

After native/preparation operations settle, cancellation pauses an automatically
allocated running machine, or stops an incomplete one, preserving its volume and
worktrees. It does not pause a reused manual machine or one serving another
active task. A later manual start superseding an already completed cleanup is
respected. Completion waits for the queued machine cleanup operation.

Explicit retries of blocked/failed tasks and review follow-ups require the
expected task generation and a request UUID. Duplicate requests return the same
attempt; different payloads conflict. Retries allocate a new native run ID and
invalidate old worker leases while retaining the task's checkout and branch.
Retried task-owned machines resume through existing lifecycle policy checks and
task fencing. Reservation reuses the creator's own running, then paused, then
stopped machine of the same environment, whatever repository it served before;
preparation starts a reused paused or stopped machine and waits for a start
already requested. Each start is keyed by task, task generation and machine
generation, so a machine paused again is started again while a start that
failed for good is not retried in a loop. An exhausted checkout is requeued at most once per explicit task
generation, so polling cannot silently produce unlimited setup retries.

## Issue lifecycle

Delegation captures the issue status and edit timestamp; it does not change the
issue. The worker records actual native execution in a transaction that verifies
the current attempt and lease, then moves an unchanged backlog/unstarted issue
to its team's `in_progress` status in the started category, or the first configured
started status by position. Missing started statuses leave the issue unchanged.
Any intervening issue edit, including a status change away and back, preserves
the human/GitHub decision. Explicit retry captures a fresh delegation snapshot.

The issue write lock precedes conversation/task locks. Issue history, the task
transition and conversation event commit together; realtime issue updates are
published afterward. Concurrent tasks cannot duplicate the issue change.
A process that started and failed before the next poll still records its start
before failure cleanup. Native success never moves an issue to Done.

Issue activity attributes agent actions (migration 66): the history entry for
the started status names the task's harness (for example `codex`), and changes
made through the assistant's issue tools name `assistant`, alongside the user on
whose behalf they acted. Field changes record the agent through the request
context; title and description changes pass it to the content-history trigger
through a transaction-local setting, like the actor. The issue views show
"via Codex" or "via Assistant". Commit authorship is configured separately
(`DEV_MACHINE_AGENT_GIT_NAME`/`_EMAIL`), and pull requests are opened by the
GitHub App.

## Native execution and recovery

Preparation records `starting` without setting `started_at`. After potentially
slow image/container preparation, the Docker runtime rechecks
execution authority before start/resume and before handing off credentials.
Cancellation after a successful check still uses durable physical cleanup. The
Docker runtime reports a confirmed container start before output; only that
observer records `running` and the service's container ID. Failed persistence stops execution.
Recovery preserves the original start time and an existing waiting-input state.
An error does not falsely report that the service has stopped; reconciliation
and durable cancellation retain responsibility for physical cleanup.

The manager encrypts a per-attempt snapshot of the authorized runtime secrets
before starting execution. Concurrent managers reuse the first committed
snapshot. Versioned snapshots distinguish credentials from public model/runtime
configuration, so values such as numeric flags do not corrupt native JSON output.
Legacy snapshots retain their original redaction policy for deterministic replay.
Recovery injects and redacts against that same snapshot, including if workspace
credentials have since rotated. The encrypted field is excluded from
JSON responses. Collector event/log ingestion also redacts current workspace
model credentials and retained native snapshots, including credentials rotated
since an attempt started. Unreadable snapshots reject ingestion. Native and
collector output share escaped/multiline and longest-first secret redaction. An
active legacy attempt without a usable snapshot is stopped; recovery never guesses
which credentials its container received.

Docker logs are followed while the native process runs. Waiting for process exit
cannot block handling an output-storage failure. Cancellation or a failed output
observer stops the native process, cancels the Docker HTTP requests, and waits
for output goroutines to exit. The full provider result is parsed from sanitized
captured output, and never establishes independent test success.

The manager buffers logical lines, redacts known secrets before persistence,
including multiline fragments and JSON-escaped forms, and sanitizes invalid UTF-8
and NUL. Each stream is capped at 4 MiB and 4,096 logical records, plus a limit
marker. Lines over 16 KiB are suppressed whole rather than exposing a partial
secret or interpreting an incomplete structured event. Full terminal text stays
in the paginated native log store.

Docker supplies an RFC3339Nano timestamp for each log entry; see the
[Docker log timestamp contract](https://docs.docker.com/reference/cli/docker/container/logs/#retrieve-logs-with-timestamps--t---timestamps).
The first timestamp identifies the retained stream prefix. Replayed logical line
numbers must match both that identity and their stored content. A rotated prefix
fails recovery instead of discarding new output as duplicate old text. Timestamp
prefixes are removed before provider parsing and display.

Each log record, its actual parsed provider activities and a `task.activity`
conversation event commit in one transaction. Replaying a record cannot duplicate
its activities. The conversation payload contains `task_id`, `run_id`, `log_id`,
`stream`, `truncated`, and an array of `{type, payload}` structured events. It does
not duplicate plain terminal output. Conversation replay uses the existing
ordered cursor and authorization checks. Provider payloads remain untrusted;
they cannot authorize actions, approve operations or assert independent test
results.

## Automations

Migration 69 adds event-driven and scheduled delegation. Workspace
administrators manage `agent_automations` rules from the coding-agent settings
page. A rule belongs to one team. It matches open issues that have its label,
its status, or both, optionally only during one UTC `run_hour`, and
`max_active` (1–5) caps its pending, starting and active runs.

Rules match current issue state instead of history events, which are recorded
best-effort. A worker in the API process claims matches every 15 seconds. Each
rule is locked `FOR UPDATE SKIP LOCKED`, and `agent_automation_runs` is unique
per (rule, issue), so a crash or several API instances never miss or duplicate
a match. An issue that already has active agent work is skipped. Nothing is
claimed while coding agents are disabled for the workspace.

By default a run is `pending_approval`, and the rule owner gets a
notification. The owner or a workspace administrator can approve or dismiss
it. Approval starts the task as the approver.

A rule marked unattended starts as its owner without approval, but only if the
workspace policy enables `allow_unattended_automations`. Otherwise its runs
also wait for approval.

Starting reuses the chat delegation path: it opens the issue conversation and
calls `AgentTaskService.Create` with the composer defaults, under the actor's
current permissions. The run's fixed request ID replays the same task after a
crash. A transient failure stays `starting` for the worker to retry. A
permanent one, such as lost access or a disabled provider, becomes `failed`
with the reason. A rule grants nothing by itself, and removing its owner's
access stops its unattended runs.

- `GET/POST /agent-automations`, `PATCH /agent-automations/:id` (`{enabled}`)
  and `DELETE /agent-automations/:id` are administrator-only.
- `GET /agent-automation-runs` lists recent runs (administrators).
- `POST /agent-automation-runs/:id/approve|dismiss` is open to the rule owner
  or an administrator.

## Automatic QA previews

Migration 71 adds an inherited preview setting: an argv command and a port
(1024–65535), set together or not at all. It is snapshotted onto each task,
like the test command.

When an attempt reaches `awaiting_review`, the transition queues one
`start_preview` operation, keyed by attempt. The manager then:

- checks that the task is still in review and the machine is running;
- refuses a port used by one of the machine's own services;
- runs the command in the task worktree on the developer container, as UID
  1000 with `PORT` and `HOST=0.0.0.0` set;
- stops the machine's previous preview (its whole process group) first;
- points the machine's authenticated preview host at the port, so "Open
  preview" appears on the task card.

The process keeps running until a later attempt replaces it, or until the
machine is paused or stopped. Previews reuse the gateway's host-bound tickets;
the application never sees Kuayle credentials.

## Dependencies and collaborating agents

Blocked-by relations order agent work. A relation can be stored from either
side (`blocked_by`, or the inverse `blocking` row). The database refuses
relations that cross a private-team boundary.

- **Waiting tasks:** a queued task whose issue has an unfinished blocker
  (status category not completed or cancelled) is not claimed by the worker.
  Its reason names the blockers from its own team and only counts blockers in
  other teams. It starts on its own once every blocker is finished.
- **Automations:** blocked issues are not matched until they are unblocked.

The assistant can plan and split work with the user's permissions:

- `create_issue` takes an optional `parent_identifier` to create sub-issues.
- `link_issues` records "blocked by" and refuses direct or transitive cycles.
- `get_issue` returns blockers and sub-issues.
- `plan_issues` groups up to 20 issues into waves that can run in parallel,
  reports cycles, and lists blockers outside the plan. Blockers the user
  cannot see are never named.

Because waiting is enforced by the worker, a whole plan can be delegated at
once. Each issue gets its own agent, worktree and branch.

Each native prompt starts with the task's related work in the same team: its
parent, sub-issues, siblings and blockers. Each entry shows the status and the
newest agent task state, branch and pull request. Agents working in parallel
know what the others own and build on their merged changes.

## Remaining workflow work

The worker progresses through preparation, native execution (with native
session resume), independent validation and publication to `awaiting_review`,
and completes when the recorded pull request merges. CI failures and trusted
change requests queue bounded follow-up attempts. Explicit in-app acceptance is
not implemented. Agents coordinate through issues, dependencies and the
related-work context, not through direct messages or shared branches. A
successful process exit must never mark an issue Done.

## HTTP controls and replay

All task routes require an interactive session and the existing Dev Machine demo
guard. PAT scopes do not grant task access. Current creator/conversation/issue
permissions apply to every page, including older attempts. Disabling execution
policy blocks new work and sends running attempts through cleanup; historical reads
and cancellation remain available to authorized users.

- `GET /conversations/:conversationId/task-options` returns policy-allowed native
  autonomous providers and the existing workspace model configuration. Custom
  CLI allocation requires saved command configuration and is not offered yet.
- `GET|POST /conversations/:conversationId/tasks` lists or delegates tasks.
- `GET /agent-tasks` lists the session user's 50 most recent tasks across their
  issue conversations, re-checking membership and each issue's current team
  scope (a moved or hidden issue drops out). The Global Assistant renders it with
  the AI Elements [Queue](https://www.shadcn-svelte.com/docs/ai-elements/queue)
  composition (in progress or waiting, then finished), linking each task to its
  issue chat, and refreshes it every 20 seconds while visible.
- `GET /agent-tasks/:taskId` reads the durable task.
- `POST /agent-tasks/:taskId/cancel` requests cleanup, including an explicit retry
  of exhausted cleanup operations.
- `POST /agent-tasks/:taskId/retry` requires request UUID, expected generation and
  new instructions; it preserves the task worktree.
- `GET /agent-tasks/:taskId/history` pages transition records with `after`.
- `GET /agent-tasks/:taskId/runs` pages native attempts with `before`.
- `GET /agent-tasks/:taskId/runs/:runId/logs` pages that attempt's logs with `after`.
- `GET /agent-tasks/:taskId/runs/:runId/validation` returns that attempt's
  independent check step, or `null` when none was recorded.
- `GET /agent-tasks/:taskId/runs/:runId/artifacts` lists that check's kept
  artifacts (`id, path, content_type, size`); `.../artifacts/:artifactId`
  downloads one.
- `GET /agent-tasks/:taskId/runs/:runId/publication` returns `{step, branch,
  pull_request_url}`; `pull_request_url` is present only for a succeeded step.

Paths are workspace-relative. Task/attempt pages contain 25 entries and an
optional `next_before` UUID; history/log pages contain 100 entries and an optional
`next_after` integer. Cursors must belong to the requested scope. Native log IDs
also arrive through the existing authenticated conversation event stream.
Task attempts, native events and logs are excluded from the older machine read
endpoints (including PAT reads); their content is available through the task
endpoints under current conversation permissions. Ordinary manual attempts stay
on the existing machine routes.
Encrypted recovery credentials never appear in API output. Mutating task bodies
reject unknown properties; callers cannot supply runtime IDs or lease authority.
Provider model/effort validation is shared with direct native execution.


## Web task controls

Issue chats and focused issue conversations share the same task panel. It loads
bounded task pages, applies persisted `task.state_changed`/`task.updated` events,
and uses `task.activity` log cursors to refresh an expanded execution view. Refreshes
are throttled from the first event so continuous output remains live. A
browser refresh or reconnect only reads task state; it never delegates again.
Task and retry forms retain their request UUID after a transport failure. Native
output is rendered as text through Code Block; structured event labels and
transition history use Trace. Long histories, attempts and logs page explicitly.
No provider text or successful exit is displayed as independent validation proof.
The independent check renders through the AI Elements
[Test Results](https://www.shadcn-svelte.com/docs/ai-elements/test-results)
composition (Trace + Code Block) as one check with its real status, exit code and
timestamps; `task.tests_updated` events refresh it. Its output is split from the
native output by the step's `service_id`. The recorded pull request opens from
the task card footer (loaded even while details are collapsed) and the
publication status appears in the execution details; both refresh on
`task.publication_updated`.

The execution details also render the harness's own plan with the AI Elements
[Plan](https://www.shadcn-svelte.com/docs/ai-elements/plan) composition (the
latest Codex `todo_list` item or Claude `TodoWrite` call), provider-supplied
reasoning summaries with [Reasoning](https://www.shadcn-svelte.com/docs/ai-elements/reasoning)
(Codex `reasoning` items, Claude `thinking` blocks; collapsed, never synthesized),
and the attempt's recorded changed files with
[File Tree](https://www.shadcn-svelte.com/docs/ai-elements/file-tree). They are
parsed client-side from the attempt's stored native output and git state, so they
survive reloads. File links point to the recorded pull request's repository and
branch, never to agent-reported URLs. Agent text is rendered as escaped text.

The AI Elements [Checkpoint](https://www.shadcn-svelte.com/docs/ai-elements/checkpoint)
pattern ("restorable milestones where supported") is intentionally not offered:
restoring an attempt would rewrite the task's published branch, which Kuayle does
not do without human review. Attempts, their commits and the recorded pull
request remain inspectable, and a new attempt continues from the branch head.

The UI follows the official [Task](https://www.shadcn-svelte.com/docs/ai-elements/task),
[Model Selector](https://www.shadcn-svelte.com/docs/ai-elements/model-selector),
[Trace](https://www.shadcn-svelte.com/docs/components/trace) and
[Code Block](https://www.shadcn-svelte.com/docs/components/code-block) compositions.
Trace and Code Block were added from the Nova registry without replacing the
existing Button or Collapsible. The unused optional syntax-highlighting helper
and Shiki dependency were omitted; native logs render escaped plain text.
