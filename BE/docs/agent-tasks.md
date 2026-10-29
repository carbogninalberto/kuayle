# Agent task execution contract

## Implementation status

The task domain, repository, worker and HTTP controls are wired into the API
process. Issue conversations expose task delegation, retry, cancellation and live
activity through AI Elements Task/Trace and Model Selector compositions. The
assistant delegates through approval-gated [orchestrator tools](conversations.md#orchestrator-tools).
Native success is followed by an independent check (migration 58) and deterministic
publication of a pull request (migration 59), after which the task waits in
`awaiting_review` until GitHub reports that pull request merged, an approved
merge from chat completes it, or its creator or a workspace admin accepts it in
Kuayle without merging ([accepting without merge](#accepting-without-merge)). CI failures, GitHub change requests and issue
comments queue bounded follow-up attempts; reviews by a second agent run only on
request (migration 84). See [remaining workflow work](#remaining-workflow-work)
for what is not implemented.

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
snapshot records a `skipped` step; it is never a pass. The task still moves to
`publishing`, and the change is opened as a **draft** pull request whose
description starts with a "Not validated" warning; the task then awaits review
with a reason that says no check is configured.

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

On machines with their own Docker daemon (workspace policy `allow_docker`, see
[Machine Docker daemon](../../TECHNICAL.md#machine-docker-daemon)) the agent's
attempt and the developer terminal can run the full `docker compose` against
that daemon (builds, volumes, `env_file`, profiles, any number of services),
and a preview command may be `docker compose up`: its published port becomes
the task's app port on the daemon. The independent check still uses the subset
above, because the check container cannot join the daemon's Compose networks.


### Manual checks

Migration 97 adds the `run_check` machine operation. The task's creator can run
the check on demand, without an agent attempt and without changing the task's
state, generation or attempt: `POST /agent-tasks/:taskId/checks` with an empty
body or `{}` runs the task's check (the command requested for the task, else the
command configured for its issue now, with the configured Compose file), and
`{"command": [argv...]}` runs a one-off argv (the assistant sends
`["sh","-c","<command>"]`). Commands follow `ValidateTestCommand`; a custom
command also needs `dev_machine:manage`, the permission that edits the check
command in the development settings. The answer is 202 with the step.

The task must be `awaiting_review`, `blocked`, `failed` or `completed`, with its
latest attempt's redaction snapshot and ready checkout on a running machine with
an enabled policy (otherwise 409 `CHECK_UNAVAILABLE`; no command at all is 409
`NO_CHECK_COMMAND`). The request takes native-run creation's workspace and
machine locks and refuses with 409 `MACHINE_BUSY` ("the agent is working; try
again when it pauses") while an attempt, an independent validation or
publication, or another manual check uses the machine. In return a queued or
running manual check makes new attempts wait (the worker retries quietly),
makes validation and publication of other tasks on the machine wait, refuses
pause, stop and teardown like an active run, and blocks the task's retry.

The result is a `manual_check` step on the task's latest attempt (sequence 100
and up, so it never takes validation's or publication's place, and it never
counts as validation). The operation's idempotency key `task-check:<step id>`
names the step. The manager reuses the independent check path: the same
credential-free container (developer image, hardened host config, egress proxy,
`CI=true`, the attempt's snapshot used only for redaction), Compose services,
artifacts and log chunks under `service_id` = step ID, but a container of its own,
`kuayle-<routing key>-check-<step id>`, removed once the result is recorded so a
later check never replays an earlier one. Every write re-checks that the
requester still created and may use the task, that the task is still idle on the
same attempt and machine, and the operation's lease, generation and policy. A
retry, cancellation or revoked access stops it. The exit code alone decides
`succeeded` or `failed`; an operation that fails for good (stale machine,
timeout, rejected Compose file) marks the step `failed` with a safe summary, and
cancellation marks it `cancelled`. Every change emits `task.tests_updated`, so the
task card shows it live, labelled "Manual check", beside the independent check.
`GET /agent-tasks/:taskId/runs/:runId/checks` lists an attempt's manual checks.

The assistant's `run_check(identifier, command?)` tool asks for approval (the
card shows the exact argv) and runs the check of the issue's newest task as its
creator. `get_agent_work` then reports the latest manual check: status, exit
code, command and summary, plus the last 1,500 characters of its untrusted
output for the task's creator only.

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
   one with the GitHub App, as a draft when the attempt's check was skipped. A
   reused pull request gets this attempt's description, so a later passing check
   replaces the "Not validated" warning; GitHub's draft flag is left for a person
   to clear. The title carries the issue identifier so the existing
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

## Issue images

Each attempt receives the raster images (PNG, JPEG, WebP, GIF) embedded in its
issue's description and comments that the task creator can read, including
conversation images filed with `attach_images_to_issue`
([conversation images](conversations.md#images)). When the attempt is created,
the newest at most 10 images (10 MB each, 50 MB in total) are fixed in
`agent_task_attachments` (migration 82) under the attempt's run ID, so a
repeated or concurrent attempt creation keeps the same list. Each gets a safe
path `.kuayle/attachments/NN-name.ext`; the native prompt lists the paths,
types and sizes after the task, and drops the list rather than exceed the
prompt limit. Codex and Claude Code open image files by path.

Before starting the agent, the manager reads the listed objects from upload
storage (it needs read access; Compose mounts `uploads` read-only and passes the
storage settings) checking path, size and total, and writes them into the
checkout with the developer container's user: it replaces
`.kuayle/attachments/` and adds `/.kuayle/` once to the repository's
`info/exclude` (`git rev-parse --git-path info/exclude`, shared by worktrees),
never to a tracked `.gitignore`, so the images cannot be committed. No
credential is involved. A failure is logged as `attachments.unavailable` and
does not block the run; success records `attachments.copied`.

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

## Accepting without merge

A task in `awaiting_review` can be accepted in Kuayle when its work is good
enough but should not, or cannot, be merged through Kuayle. Acceptance moves
the task to `completed` with the reason `Accepted in Kuayle by <name>` and the
accepting user as the transition actor. The pull request is left exactly as it
is: Kuayle does not merge, close or comment on it. Like any completion it
queues `stop_preview` for a starting or running task app and purges the native
session. In the same transaction, an issue whose status is not in the
completed or cancelled category moves to its team's completed status
(preferring the `done` slug, then position), with a status history entry by the
accepting user; the `issue.updated` event is published after commit. An issue
already completed or cancelled keeps its status. Sub-issue and parent
auto-close rules are not applied on acceptance.

- `POST /agent-tasks/:taskId/accept` with `{"generation": N}` (the attempt the
  user reviewed; unknown fields are rejected) is for the task's creator, under
  the ordinary task authorization; it answers 200 with the completed task.
  Other users get 404 like every task route, a stale generation or a task not
  awaiting review gets 409 `TASK_CONFLICT`. Repeating an acceptance of the same
  attempt returns the completed task again and changes nothing. A task
  completed by a merge cannot be "accepted" afterwards (409).
- The assistant's `accept_agent_work` tool requests an approval bound to the
  task, its attempt generation and the pull request and commit shown to the
  user (migration 87 allows the action). On approval, the approver must be the
  task's creator or a workspace owner/admin who can still see the issue; the
  repository re-checks role, issue team visibility and the generation under
  the issue and task locks. An admin's acceptance never returns the creator's
  private task payload. A repeated or resumed approval converges on the same
  completed task.

Because merge completion only considers tasks still in `awaiting_review`, a
later merge webhook or the merge reconciler finding the accepted pull request
merged changes nothing on the task. The issue may still follow the separate
`pr_merged` GitHub automation.

## Agent sessions

Migration 84 adds `agent_sessions`. Each task has exactly one session
(`task_id` is unique): `purpose` is `implement` for a coding task and `review`
for a requested review. Follow-up attempts reuse the implement session; native
resume itself stays as described in
[native session resume](#native-session-resume-and-handoff). The session's
state is derived in one place, `syncAgentSession`, called by every task
transition inside the transition's transaction:

| Task state | Session state |
| --- | --- |
| `queued` | `created` (first attempt) or `starting` (follow-up) |
| `preparing` | `starting` |
| `running` | `active` |
| `waiting_input` | `waiting_input` |
| `validating`, `publishing`, `awaiting_review`, `blocked` | `suspended` (the harness exited; a follow-up can resume it) |
| `completed`, `failed`, `cancelled` | the same, with `ended_at` |
| `cancelling` | unchanged until cleanup settles |

The session records the task's current machine and the harness's native
session ID from the current attempt (Claude Code's Kuayle-assigned
`--session-id`, Codex's `thread_id`), picked up at the next transition; it is
the latest recorded ID, not a history. `dev_machine_agent_runs.session_id`
links each attempt, set when the run is created in the fenced run transaction.
Existing tasks were backfilled with a derived implement session and their
attempts linked. Every query filters by workspace.

`GET /agent-tasks/:taskId/sessions` (session-only, task creator) returns
`{sessions: [...]}`: the task's own session and the review sessions of its
published work, newest first, each with `task_state` and, for reviews, the
review record. The task card shows them in its execution details (harness
logo, purpose, state) and shows reviews as review cards.

## Requested agent reviews

A review runs only when the user asks for it in chat; nothing starts one
automatically. The assistant tool `request_agent_review` takes an issue
identifier and/or a task ID, an optional harness (`claude-code`, `codex`,
`opencode`; default the implementing task's harness, and a different one may be
chosen), optional focus instructions (4,000 bytes) and `post_to_pull_request`
(default false). Like other approval tools it only records an approval bound to
the task, its published pull request and commit, the harness and the posting
choice; the user approves in the web chat or Telegram.

Preparing the request requires a non-guest member who can see the issue
(missing, hidden and unpublished tasks give the same answer). Approval
re-checks that the task still awaits review at the same commit, opens the
approver's own issue conversation, requires the harness to be allowed by the
workspace policy and the provider/model checks of any coding task, and calls
`CreateReview` under the conversation transaction. The task must be an
implementing task in `awaiting_review` with a pull request and commit Kuayle
published for its current attempt (the same records as `MergeTarget`). A
request is idempotent per (task, published commit, harness): it returns the
existing review unless that one failed or was cancelled, in which case a new
`attempt` starts. Replaying the approval's request ID returns its review.

A review is an `agent_tasks` row with `kind='review'` and `review_of_task_id`,
created by the approver in their issue conversation, with an `agent_reviews`
record (task, commit, repository, pull request, base branch, focus, posting
choice). It reuses the whole task pipeline (machine reservation and reuse
within the user's caps, its own task-scoped worktree and branch, native
execution, cancellation and transcript purge) with these differences:

- Its transitions never include `validating`, `publishing` or
  `awaiting_review`; a succeeded harness goes from `running` to `completed`.
  `EnsureValidation` and `EnsurePublication` refuse review tasks, so no
  publication operation can be queued for one.
- Its attempt is created without push (like every task attempt) and the run
  repository refuses a review run with `push_branch` or `open_pull_request`.
  The manager adds `GITHUB_TOKEN` only to runs that push, so the reviewer
  never receives a GitHub credential; the checkout's credential helper has
  nothing to read. Any edits it makes stay in its own worktree and are never
  committed or pushed.
- It never moves the issue's status, is not resumed or retried (`Retry`
  refuses it; the user requests a new review), and is excluded from task
  lists, the queue, agent work, merge targets and comment feedback. A queued
  review is not held back by the issue's blockers.

The prompt gives the published commit, branch and base, asks the reviewer to
check out the commit detached (its objects are in the machine's repository
cache after the checkout's fetch), review `git diff origin/<base>...HEAD`, not
change anything, and end with `Verdict: approve | request_changes | comment`
and a Markdown list of findings. When the attempt succeeds, `CompleteReview`
reads the attempt's recorded summary (the harness's final message, already
capped at 4,000 bytes by the provider parser), normalizes the verdict
(`unknown` when missing; never treated as approval), stores findings as
bounded valid UTF-8 (16 KiB column limit) and completes the review in one
fenced transaction. Every review transition emits `task.review_updated` to the
reviewed task's conversation, so its card refreshes.

If the user asked to post, the comment becomes due once the review completes.
The API process claims due comments every 15 seconds with a two-minute lease
(at most five attempts), re-checks that the requester is still a non-guest
member, and posts one issue comment through the GitHub App with a hidden
`kuayle-agent-review` marker; `EnsurePullRequestComment` looks for that marker
first, so a retry never duplicates it. The body quotes the findings with
mentions neutralized and states it is not an approval. Kuayle never submits a
GitHub review (approve or request changes).

To act on findings, the assistant's `follow_up_task` tool requests approval for
a follow-up on the issue's newest implementing task (only its creator, while it
awaits review, is blocked or failed), bound to its attempt generation. Approval
uses the existing retry path with the original instructions plus a
`Follow-up from Kuayle chat` section containing the approved instructions.
`get_agent_work` and `list_agent_work` include the latest review
(`agent_review`: state, harness, verdict, commit, the start of the findings
and the comment state).

The task card's "Request review" menu (allowed harnesses, implementer first)
only writes "Review ENG-12 with Codex" into the chat composer; chat remains the
only way to request one. A review requested by another member appears on the
creator's task card; its full logs belong to the requester's review task.

Limitations: reviewer output parsing and the detached checkout were exercised
with fakes and fixtures, not against a real machine and harness. The review
worktree branch is created from the base branch and the reviewer checks out the
published commit itself. Rollback refuses retained review tasks.

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
A native failure caused by a resource limit (an out-of-memory kill of the agent
container, see [resource limits](../../TECHNICAL.md#resource-limits-and-quotas))
fails with that explanation as its reason, kept as the final `failed` reason;
other native failures use the generic reason and cleanup message.
Configuration failures after reserving an environment also enter cleanup: a
preparing task may already have started native execution between worker polls.
An authorized repeat cancellation request can retry failed cleanup operations
once their underlying cause has been repaired.

After native/preparation operations settle, cancellation pauses an automatically
allocated running machine, or stops an incomplete one, preserving its volume and
worktrees. It does not pause a reused manual machine or one serving another
active task. A later manual start superseding an already completed cleanup is
respected. Completion waits for the queued machine cleanup operation.

Preparation resolves the task's repository from the issue, project, team and
workspace development settings (the most specific configured scope wins), so an
issue needs no repository of its own when its team or project has one. Without
any, a task that has not reserved a machine is blocked with "No development
repository is configured for this issue, its project, team or workspace"; after
setting one, retrying the task resolves it again.

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
A new task machine is a custom-size machine with the workspace policy's task
machine CPU, memory and disk (default 4 vCPU, 8 GB, 20 GB), headless unless an
environment applies; see [Resource Limits and Quotas](../../TECHNICAL.md#resource-limits-and-quotas)
for its limits and how its memory is split between the agent and terminal.

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

## App ports and QA

Migration 71 adds an inherited preview setting: an argv command and a port,
snapshotted onto each task like the test command. Since migration 85 the port
is only preferred and optional (1024–65535 when set); a port without a command
is refused. Each task has its own preview process and app port (see
[App ports](../../TECHNICAL.md#app-ports)), so tasks on one machine never
replace each other's previews.

A preview starts when an attempt reaches `awaiting_review` (one
`start_preview` operation keyed by attempt) or when the task's creator calls
`POST /agent-tasks/:taskId/preview` (202 with the task; 409
`PREVIEW_UNAVAILABLE` without a preview command, or without the task's ready
checkout on a running machine). `DELETE /agent-tasks/:taskId/preview` (202)
queues its stop. Preview operations name the task in
`dev_machine_operations.agent_task_id`. The manager then:

- records `preview_state` `starting`, then `running` or `failed` with a bounded
  `preview_error`, and finally `stopped`;
- picks the task's existing port, else the preferred port when free, else the
  first free port in 4100–4999; free means not a service port, not listening
  and not exposed (the number of app ports is not limited);
- exposes it as a `task` app port labelled with the issue identifier;
- runs the command in the task worktree on the developer container as UID 1000
  with `PORT` and `HOST=0.0.0.0`, in its own session, with pidfile
  `/tmp/kuayle-preview-<taskId>.pid` and a log `/tmp/kuayle-preview-<taskId>.log`
  truncated beyond 8 MiB. A restart stops only that task's previous process
  group.

A stop kills that task's process group, removes its `task` port and revokes
the port host's sessions. It is queued when the task becomes completed,
cancelled or failed, when the user stops it, or when the user removes the task
port. Stopping or re-provisioning the machine marks its previews stopped and
removes task ports; pausing keeps them (the processes are frozen and resume).
Task payloads carry `preview: {state, port_id, port, error} | null`; changes
are published as `task.updated` conversation events. The assistant's
`start_preview` (with approval), `stop_preview`, `expose_port` (with approval)
and `list_ports` tools use these same paths; see
[previews and ports from chat](conversations.md#previews-and-ports-from-chat).

Implementing attempts get one prompt line about manual QA: with a preview
command, that Kuayle starts it for the reviewer; without one, that the attempt
runs in a temporary container and should explain how to start and test the
app, which the reviewer can expose from the machine terminal with the
`kuayle-qa` skill and `kuayle-ports`.

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
Agents of related tasks can also message each other; see
[agent messaging](#agent-messaging).
The task's creator can message its running agent; see
[live channel](#live-channel).

## Agent messaging

Migration 88 adds `agent_messages` (the registry, called the navigator in the
UI), `agent_message_holds` (held conversations), `agent_message_actions` (the
audit of every human action) and `comments.agent_task_id`.

**Sending.** Native attempts run in temporary agent containers that share the
task worktree with the developer container, so an agent sends a message by
writing one JSON file per message to `.kuayle/outbox/<name>.json` at its
worktree root: `{"to":"ENG-12","channel":"internal"|"comment","body":"..."}`
(the Kuayle-owned `kuayle-agents` skill, installed for Claude Code and Codex in
the agent, IDE and headless images, describes it; implementing prompts with
related work get one line about it). Before each attempt the manager creates
the outbox and adds `/.kuayle/` to the repository's `info/exclude`, so outbox
files are never committed or published.

**Collection.** The manager takes outbox files during the 15-second port scan
of each running machine and once more when an attempt ends, before git state
and the next step. A script in the developer container (run as UID 1000, also
under dash) reads `/workspace/tasks/*/.kuayle/outbox/*.json`: worktree,
`.kuayle` and `outbox` must be real directories and each entry a regular file
(never a symlink) whose name uses `[A-Za-z0-9._-]`; at most 10 files per
worktree and 50 per scan, each cut one byte over 16 KiB. Taken and invalid
entries are removed; hidden files stay, so agents write a hidden file and
rename it. A file swapped for a symlink between the check and the read can at
worst make the developer user read its own file, which still has to be a
strict message object to be accepted.

**Validation.** The worktree maps to its task (`TaskForWorktree`). The API
records each file under a per-task advisory lock, in file-name order, within
20 messages per task per hour (refusals included; further files are not
recorded); a file taken twice (same attempt, name and content) is recorded
once. A message is refused, and kept as `rejected` with a reason and a bounded
preview, unless it is one JSON object with only `to`, optional `channel` and
`body` (at most 4,000 characters of valid UTF-8; control characters other
than tabs and newlines are removed), the sender is an implementing task that
has not ended whose creator is still a non-guest member who can see the
issue's team, and `to` names a related issue of the sender's issue in the same
team and workspace: parent, sub-issue, sibling, blocker or blocked issue. The
recipient task is the issue's newest implementing task. Same-team recipients
keep private-team boundaries: a member who can see the sender can see the
recipient unless the issue moved since.

**Holding.** A held conversation (`agent_message_holds`, one row per issue
pair) makes new messages between the pair `held` and holds its queued ones.
Workspace administrators and the creators of the sending or receiving task (for
a conversation: of either issue's implementing tasks) can hold a queued
message, release a held one back to `queued`, drop a queued or held one, hold
a conversation, or release it, which releases its held messages. Delivered,
dropped and refused messages cannot change. Every action records the actor in
`agent_message_actions`; the message also keeps who held, released or dropped
it.

**Delivery.** When an implementing attempt is created, the queued messages
for its issue (oldest first, at most 10, within the prompt room left before
the 32 KiB limit) are marked `delivered` with the attempt's run ID and placed
after the task as "Messages from other agents", each fenced as untrusted text
with the sender's issue, task and channel. Recreating the same attempt places
the same messages; messages claimed for an attempt that was never created
(another run ID became current) are delivered again. Kuayle never starts an
attempt to deliver a message: a recipient waiting in `awaiting_review` keeps
them queued until its next attempt. Reviews receive none.

**Comment channel.** A queued `comment` message is also posted, every 10
seconds by the API process, as a comment on the recipient issue: one
transaction inserts the comment and marks it `posted`, authored by the sending
task's creator (accountable for the task) with `agent_task_id` naming the
sending task, so the issue shows it as an agent message. The body is escaped
plain text under "Message from the agent working on ENG-11". The comment is
`failed` instead when that creator can no longer see the recipient issue.
These comments are not interactive, so they never queue a follow-up attempt;
the recipient agent still receives the message in its prompt. Held comment
messages are posted only once released.

**Reading.** Every non-guest workspace member reads the messages whose sending
issue they can see; the recipient is named only when they can see it too.

- `GET /agent-messages?task_id=&issue_id=&issue=ENG-12&state=&channel=&before=`
  returns `{messages, next_before}` (50 per page, newest first). Each message
  has `from_identifier`, `to_identifier` (or `null`), `to_ref`, `channel`,
  `body`, `state` (`queued`, `held`, `delivered`, `dropped`, `rejected`),
  `reject_reason`, the people and times of hold, release and drop,
  `delivered_run_id`, `comment_state`, `conversation_hold_id` and
  `can_moderate`.
- `GET /agent-messages/:id` returns `{message, actions}`.
- `POST /agent-messages/:id/hold|release|drop` returns the message (403 for
  readers who cannot moderate, 409 when the state does not allow it).
- `GET /agent-message-holds` returns `{holds}`; `POST /agent-message-holds`
  with `{issue_ids:[a,b]}` holds a conversation (201);
  `DELETE /agent-message-holds/:id` releases it (204).

The routes are session-only, like the other task routes. Settings → Agent
messages lists and filters the registry and its held conversations; each task
card shows its issue's messages compactly with the same controls.

Rolling migration 88 back drops the registry, the holds and the audit, and
the agent marker of posted comments (the comments stay as their authors').

## Live channel

Migration 95 adds `agent_live_messages`: one task's two-way channel between
its creator (or the orchestrator acting for them) and its agent, while the
agent works. The task card, the chat and the assistant use it as described in
[agent messages in chat](conversations.md#agent-messages-in-chat).

**Sending to the agent.** `AgentTaskService.QueueLiveMessage(ws, task, sender,
body)` (repository `QueueLiveMessage`) records a message from `user` or
`orchestrator`. Only the task's creator may send, under the same conversation
and team checks as the other task actions (another member gets not found);
only implementing tasks that are not completed, cancelled or cancelling take
messages; the body is cleaned like an outbox body (at most 4,000 characters);
at most 30 messages per task per hour (otherwise
`ErrAgentLiveMessageRateLimited`; an ended task gives `ErrAgentLiveMessageClosed`). Under the task row lock each message gets
the task's next sequence number and state `queued`. It only delivers text.

**Delivery while an attempt runs.** Each unattended attempt gets a Docker
volume `kuayle-<routing-key>-inbox-<run-id>` (labels `com.kuayle.service=agent-inbox`
and the run ID), mounted read-only with `nocopy` at `/run/kuayle/inbox` in the
agent container, outside `/workspace`. The manager writes into it through a
writer container with the volume mounted read-write at `/inbox`, created from
the agent image and never started (no network, read-only root, all
capabilities dropped): Docker copies into a stopped container's volumes. The
agent therefore cannot create, change or remove inbox files. Every 5 seconds
while the attempt runs, the manager writes the queued messages of the task
whose current run it is (at most 10 per pass, oldest first) as root-owned
`<seq>.json` files (mode 0644) holding only
`{"seq","from":"user"|"orchestrator","kind","body","sent_at"}`, then marks them
`delivered` with `delivered_via=inbox` and the run ID. A failed mark rewrites
the same files next time; readers track sequence numbers. Inbox files never
contain credentials, task or user identifiers. If the inbox cannot be
prepared the attempt still runs and messages wait for the next attempt.

**Claude Code.** The Claude agent image installs
`/etc/claude-code/managed-settings.json` (from
`devmachine/common/claude-managed-settings.json`), which the repository's
`.claude` settings cannot replace and the agent cannot edit. It runs
`kuayle-live-inbox-hook.cjs` on `PostToolUse` (every tool) and `Stop`. The
hook reads inbox files with a sequence above the one recorded in
`~/.kuayle/live-inbox.json` (the attempt's in-memory home, mode 0600, outside
the worktree), in order, at most 10 per call; regular files of at most 20 KiB
whose `seq` matches the name. It returns them, fenced and framed as messages
from the task's owner or the orchestrator on their behalf, as
`hookSpecificOutput.additionalContext` after a tool call, or as
`{"decision":"block","reason":...}` when Claude would stop, so Claude continues
with them. A file that cannot be parsed yet is retried on the next call
(later files wait behind it) and skipped after 30 seconds. Without an inbox
the hook prints nothing. Codex and OpenCode have no such hook: the
`kuayle-live-channel` section of the injected `kuayle-agents` skill
(`devmachine/common/kuayle-agents.md`) tells them to list the inbox between
steps and read files above the highest `seq` they read, and every
implementing prompt has one line about the inbox, answering and that skill.

**Next attempt.** Messages still queued when an attempt is created (none ran,
or they arrived after the last pass) are placed in its prompt after other
agents' messages, as "Messages from the person who owns this task", oldest
first, at most 10 within the prompt room, and marked `delivered_via=prompt`
with the attempt's run ID; recreating the attempt places the same messages,
and messages claimed for an attempt that was never created are claimed again.
Reviews receive none. A message written to the inbox in an attempt's last
seconds, after Claude's final tool call and stop check, counts as delivered
without being read.

**Agent to orchestrator.** While an attempt runs the manager also collects the
machine's outboxes every 5 seconds (and still during the port scan and at the
end of the attempt). An outbox file `{"to":"orchestrator","body":"..."}` may
add `"kind":"question"` (default `message`) and `"channel":"internal"` or
`"live"`; `kind` is refused for issue recipients. Such files are recorded in
`agent_live_messages` as `from_agent`, sender `agent`, state `received` with
the sending run ID, or `rejected` with a reason and bounded preview (invalid
files, or a task that ended or whose creator lost access); any task kind may
send them. They have their own budget of 30 per task per hour, are
deduplicated like other outbox files (same attempt, name and content), never
reach another agent, GitHub or an issue, and are untrusted text to show.

**Reading.** `AgentTaskService.LiveMessages(ws, task, limit)` returns the
newest messages of both directions (at most 200), oldest first, for the
task's creator.

**HTTP.** Session-only routes under the Dev Machine demo guard, with the same
authorization as the service (another member, a guest or another workspace
gets 404; PATs get 403):

| Method | Workspace-relative path | Purpose |
| --- | --- | --- |
| GET | `/agent-tasks/:taskId/live-messages` | `{messages}`: the newest 100, both directions, oldest first |
| POST | `/agent-tasks/:taskId/live-messages` | `{body}` only (unknown fields refused); 201 with the queued message, always sender `user`; 400 invalid text or a review task, 409 an ended task, 429 over the hourly budget |

**Conversation events.** Queuing, inbox or prompt delivery and recording an
agent's file each write a `task.live_message` event, with the message and its
issue identifier, to the task's conversation in the same transaction (the
manager's live collection included), and an accepted question queues a
[Telegram notice](telegram.md#task-notifications) for the creator. See
[agent messages in chat](conversations.md#agent-messages-in-chat).

**Cleanup.** When an attempt ends the manager removes the writer and the
volume once unused; the ended agent container keeps it until the run is
cancelled (`CancelAgent` removes the run's inbox volumes after its containers)
or the machine is torn down (`Teardown` removes the machine's inbox volumes).

Rolling migration 95 back drops the live channel's messages.

## Remaining workflow work

The worker progresses through preparation, native execution (with native
session resume), independent validation and publication to `awaiting_review`,
and completes when the recorded pull request merges. CI failures and trusted
change requests queue bounded follow-up attempts. Work can also be accepted in
Kuayle without merging. Agents coordinate through issues, dependencies, the
related-work context and [agent messages](#agent-messaging), never through
shared branches. A successful process exit must never mark an issue Done.

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
- `POST /agent-tasks/:taskId/accept` completes a task awaiting review without
  merging, given the reviewed `generation` ([accepting without merge](#accepting-without-merge)).
- `GET /agent-tasks/:taskId/history` pages transition records with `after`.
- `GET /agent-tasks/:taskId/runs` pages native attempts with `before`.
- `GET /agent-tasks/:taskId/runs/:runId/logs` pages that attempt's logs with `after`.
- `GET /agent-tasks/:taskId/runs/:runId/validation` returns that attempt's
  independent check step, or `null` when none was recorded.
- `GET /agent-tasks/:taskId/runs/:runId/artifacts` lists that check's kept
  artifacts (`id, path, content_type, size`); `.../artifacts/:artifactId`
  downloads one.
- `POST /agent-tasks/:taskId/checks` runs the task's check, or `{"command": [...]}`,
  without an agent attempt; `GET /agent-tasks/:taskId/runs/:runId/checks` lists an
  attempt's manual checks ([manual checks](#manual-checks)).
- `GET /agent-tasks/:taskId/runs/:runId/publication` returns `{step, branch,
  pull_request_url}`; `pull_request_url` is present only for a succeeded step.
- `GET /agent-tasks/:taskId/sessions` returns the task's agent sessions and the
  review sessions of its published work ([agent sessions](#agent-sessions)).

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
While a task awaits review, its card offers **Accept** as the primary action,
next to the pull request. It opens a confirmation dialog stating that the pull
request stays open and unmerged, then calls the accept route with the card's
attempt generation; a 409 shows that the task changed and reloads it.

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
Follow-up: restorable checkpoints may be added later as new attempts that start
from a chosen earlier commit on a fresh branch, leaving the published branch
untouched.

The UI follows the official [Task](https://www.shadcn-svelte.com/docs/ai-elements/task),
[Model Selector](https://www.shadcn-svelte.com/docs/ai-elements/model-selector),
[Trace](https://www.shadcn-svelte.com/docs/components/trace) and
[Code Block](https://www.shadcn-svelte.com/docs/components/code-block) compositions.
Trace and Code Block were added from the Nova registry without replacing the
existing Button or Collapsible. The unused optional syntax-highlighting helper
and Shiki dependency were omitted; native logs render escaped plain text.
