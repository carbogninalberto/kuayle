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
Kuayle without merging ([accepting without merge](#accepting-without-merge)). CI failures, GitHub change requests and the
task creator's own issue comments queue bounded follow-up attempts; reviews by a second agent run only on
request (migration 84). That fixed flow is the built-in Standard workflow;
workspaces can design their own as [configurable workflows](#configurable-workflows)
(migration 118). See [remaining workflow work](#remaining-workflow-work)
for what is not implemented.

Terminology: texts users read (transition reasons such as "Native coding
iteration failed; stopping execution before retry", "New execution iteration
requested" and "Implementation iteration finished; independent validation is
pending", check and tool-run errors, and assistant tool descriptions) call one
native execution run an **iteration**. Code identifiers, API fields
(`attempt`, `attempt_id`), database columns, enum values and operation keys
keep "attempt", and rows stored before the rename keep their old text.

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

A retry may also switch the task's `model` (internally with the model server an
approver picked). The retry receipt (`agent_task_retries.model` and
`model_server_id`, migration `000110_agent_task_retry_model`) records what was
asked, `''`/NULL meaning keep the task's, so replaying a request ID with another
model or server conflicts like a changed prompt. Existing receipts asked for no
model; rollback drops only the two columns.

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

The manager runs the argv as positional arguments of a fixed `sh` runner that
only prepares the run's temporary directory (no interpolation, harness bootstrap or provider
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
Moving to `publishing` re-checks, in the same transaction, a succeeded step with
exit code 0 and a completed operation, or a step skipped because the snapshot
had no command (an unchecked draft); a skipped step with a command never
qualifies. A failed or cancelled check goes to cleanup
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

### Agent-chosen tests

Migration 102 lets an implement attempt choose the tests Kuayle reruns. The
prompt (`withValidationFile`) and the `kuayle-agents` skill ask the agent to run
fast, preferably database-free tests relevant to its change and then write
`.kuayle/validation.json` at the worktree root:

```json
{"commands": [["sh","-c","cd BE && go test ./internal/service/..."], ["npm","run","check"]],
 "summary": "what was tested and why, and what was not"}
```

The file is untrusted. When the attempt ends, the manager reads it through the
developer container as a regular file only (never through a symlink), at most
64 KiB, and removes it; an attempt's start removes a leftover file, so every
attempt declares its own. `domain.ParseAgentValidationFile` accepts exactly
those two keys: 1 to 8 non-empty argv arrays that pass `ValidateTestCommand`,
and a summary of at most 2,000 characters; anything else rejects the whole file
with a fixed reason. The parsed result, or the reason, is stored on the attempt
(`dev_machine_agent_runs.declared_validation`).

Precedence: a check command configured in the development settings (issue,
project, team or workspace, or requested for the task) is an admin override.
When one exists it alone runs and the declaration is ignored. Otherwise the
declared commands run. Without either, the step is skipped and the change opens
as the not-validated draft described above; a rejected file's reason is in the
skipped step's summary. The step records `command_source` (`configured` or
`agent`), `commands` (agent source only; `command_argv` keeps the first
command), `command_results` (one exit code per command) and `agent_summary`.

The manager runs every command in order, each in its own deterministic check
container (`-validation-<run>` for the first, `-validation-<run>-<n>` after it),
so retries replay what already ran and Docker reports each exit code. All
commands run even after a failure; the first non-zero exit code is the step's.
`CompleteValidation` refuses results that do not match the commands. A
passing check opens the pull request ready for review. Its Verification section
lists each command with pass/fail, exit code and "agent-chosen", and the
agent's summary fenced as unverified text (no mentions, no fence breaking). A
failure fails the task with "Kuayle re-ran the agent's tests and they failed:
<cmd> exit N". The check output stays on the task, and the next attempt's
handoff names the failed command and its exit code. Rollback is refused while an
agent-chosen check is queued or running.

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
and a preview command may be `docker compose up`: the task's preview container
gets the same client access to the daemon as an attempt, and its published port
becomes the task's app port on the daemon. The independent check still uses the subset
above, because the check container cannot join the daemon's Compose networks.


### Manual checks

Migration 97 adds the `run_check` machine operation. The task's creator can run
the check on demand, without an agent iteration and without changing the task's
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

### Custom tool runs

Migration 100 adds the `run_tool` machine operation, which runs one version of
a workspace [custom tool](conversations.md#custom-tools) and records it in
`assistant_tool_runs` (tool and version, validated inputs, requester,
conversation, tool call and approval, machine, issue, task and attempt,
working directory, timeout, status, exit code, output tail, error and times).
Queuing takes the manual check's lock (`lockMachineForCheck`): the machine must
be running with an enabled policy, and an attempt, validation, publication,
manual check or other tool run on it is refused as `MACHINE_BUSY`; in return a
queued or running tool run holds the machine exactly like a manual check. A run
on a task's machine uses its latest attempt's ready checkout as the working
directory and that attempt's snapshot for redaction only.

The manager runs it in a check container of its own,
`kuayle-<routing key>-tool-<run id>`: the developer image, the hardened check
host config (read-only root, all capabilities dropped, only the workspace
volume, the agent share of resources), the machine network with egress only
through the proxy, and no credentials. Its command line is fixed: `sh -c
<bootstrap> kuayle-tool <interpreter>`. The script and the canonical inputs
arrive only in the environment (`KUAYLE_TOOL_SCRIPT`, `KUAYLE_TOOL_INPUT`), and
the bootstrap writes them to private files under a fresh `mktemp` directory,
exports `KUAYLE_TOOL_INPUT_FILE`, removes the two variables and `exec`s the
interpreter on the script file. Each input is also `KUAYLE_ARG_<NAME>` (upper
case; strings verbatim, numbers and booleans as JSON text). Arguments are never
interpolated into a script or command line; a test runs the real bootstrap with
`; rm -rf /`, `$(…)` and backticks and checks they arrive as literal strings.

Every write (`GetToolRun`, start, output, completion) re-checks the operation's
lease, machine generation, running state and policy, the requester's role, the
tool still active and enabled at the run's version (test runs excepted), and
the task still idle on the same attempt or the machine still the requester's.
Output goes through the agent output buffer (redaction, 4 MiB and record caps)
and is stored in batches as a 64 KiB tail; a resumed lease replays it from the
start. The timeout counts from the first start and is capped by the machine's
expiry; a timed-out container is removed and the run fails with "The tool
exceeded its time limit". The exit code decides `succeeded` or `failed`; an
operation that fails for good marks the run `failed` with a safe reason, and a
finished run's container is removed. Runs in a conversation emit
`tool_run.updated` events with the last 4 KiB of output.

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
   `Kuayle Agent <agent@kuayle.invalid>`), without loading the checkout's git
   configuration (see [what publication runs](#what-publication-runs)). The message
   is `<ISSUE-ID>: <issue title>`. Commits since the last published commit (the
   floor Kuayle recorded, or the task branch as fetched from GitHub when that
   descends from it) by any other author or carrying co-author trailers are
   folded into one commit by the configured author; Kuayle's own commits are
   kept, so a retry republishes the same commit. Base-branch commits are never
   judged or folded: when the agent merged the base, or rebased onto a newer one
   so the floor is no longer an ancestor, the folded commit is a merge of the
   floor and that base commit, so the pull request is neither behind its base nor
   re-adds the base's commits. A branch with no commits ahead of the
   base is recorded as `skipped` and blocks the task; it is never reviewable.
2. Copies exactly that branch tip into a new temporary repository outside the
   workspace volume, verifies it is still the recorded commit, and only then reads
   a fresh repository-scoped GitHub App token from stdin. It fetches the base and
   task branches from GitHub and refuses a push that would add a commit by
   another author or with a co-author trailer, since the checkout's
   remote-tracking refs the commit step relied on are agent-writable. It pushes
   the commit to the fixed `https://github.com/<checkout repository>` URL without
   force. The agent-writable checkout's configuration (hooks, URL rewrites,
   credential helpers) is never loaded, and the token is never written to the
   workspace volume.
3. Reuses an open pull request from the task branch into the base branch, or opens
   one with the GitHub App, as a draft when the attempt's check was skipped. A
   reused pull request gets this attempt's description, so a later passing check
   replaces the "Not validated" warning and a draft is marked ready for review
   (GraphQL `markPullRequestReadyForReview`). A ready pull request is never turned
   back into a draft by a later unchecked attempt; its description carries the
   warning instead. The title carries the issue identifier so the existing
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

### What publication runs

The commit, push and sync steps run in the developer container, which holds
secrets and sign-ins an agent run is not given, while agent runs can write the
checkout's git directory. Its `config`, hooks and `info/attributes` can name
programs: filter, diff and merge drivers, hooks (in `core.hooksPath` or defined
in configuration), fsmonitor, credential helpers, ssh and proxy commands,
pagers, editors, signing programs and URL rewrites to the `ext` transport. No
set of command-line overrides covers them all, so git never runs in that
directory. Each step creates a git directory under `/tmp` with its own empty
configuration and no hooks, which borrows only the checkout's objects, refs
and `info/exclude` (so `.kuayle/` stays out), and runs git there against the
checkout's working tree and index, with system and global configuration
ignored and only the `file` and `https` transports allowed
(`publishCheckoutScript`). Nested repositories are recorded at their current
commit without running git inside them, and fetches do not recurse into them.

Consequences: in-tree `.gitattributes` still apply, but the drivers they name
are undefined, so content is committed and checked out as it is in the working
tree. A repository that relies on a filter defined in its local configuration
(for example Git LFS installed with `--local`) gets the raw files in the
published commit. The checkout's `info/attributes`, `core.excludesFile`,
per-worktree configuration and reflogs are not used or written, and a checkout
with reftable refs is refused. These steps still run in the
developer container; running them in a container without its secrets would be
a further hardening.

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
It also resumes a failed attempt whose session the user continued through
**Open in Claude Code** on that machine (`session_continued_at`, migration
103), so the user's turns are kept. Otherwise it starts a new session, so a
failed resume is never repeated. Either
way the prompt keeps the task instructions and appends a bounded handoff naming
the previous attempt, its native outcome, its independent check result and exit
code, a recorded pull request, up to 50 changed files and the previous agent's
summary fenced as unverified text, because the harness never saw Kuayle's
independent check or publication. An older handoff is replaced, not accumulated,
and the prompt stays within the attempt limit. Other harnesses always start new
sessions with the handoff.

**Open in Claude Code.** `POST /agent-tasks/:taskId/terminal` (session-only,
no body) lets the task's creator continue a Claude Code task's own session
interactively. It is allowed only while nothing else uses the session or the
worktree: in `awaiting_review`, `blocked` or `failed` (`409 TASK_BUSY`
otherwise; a non-Claude task answers `409 TERMINAL_UNSUPPORTED`), on the task's
machine, which the creator must own, with its checkout ready and the harness
still allowed by the workspace policy. It returns the usual terminal launch
(`TerminalSessionLaunchResponse`, `resuming`/`pending` while the machine
starts) for a session named `ktask-<8 hex>-<task>[-<native session>]` in the
task's worktree. The native session is the task's latest recorded one, used
only when it was recorded on the task's current machine. Reopening while that
session is still open returns a new ticket for it, so the user attaches to the
same Claude Code instead of starting a second writer on the transcript.
`kuayle-terminal-session` runs `kuayle-task-claude` for these names: it points
`CLAUDE_CONFIG_DIR` at `~/.kuayle/claude-tasks/<task>` in the in-memory home,
whose `projects/` links to the task's transcripts and whose
`.credentials.json` links to the user's own `~/.claude/.credentials.json`;
the owner's saved token comes from the terminal sign-ins (see TECHNICAL.md,
agent sign-in), so no credential reaches the volume. It runs
`claude --resume <id>` when that transcript exists, otherwise a new `claude`
in the worktree with a note, then leaves a shell. Claude Code resumes in place,
so the user's turns are appended to the same transcript. Opening a native
session marks the attempt that recorded it (`session_continued_at`), so the
next attempt resumes it even when that attempt failed. Workspace trust is not
pre-granted: Claude Code asks before it trusts the worktree. The task card shows **Open in Claude Code** for a
stopped Claude Code task (Codex keeps the typed session command). Starting an
attempt while the terminal is still open is not refused: closing the terminal
tab (which ends its tmux session) before the next attempt is the user's
responsibility, and the terminal says so when Claude Code exits.

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
permissions. Only reviewers who can push to the repository can trigger one:
repository or organization owners, and members or collaborators whose `write`,
`maintain` or `admin` permission GitHub confirms
([details](github-integration.md#who-can-steer-an-agent-from-github)); other
reviews are recorded as `untrusted`. Budgets
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
stored as bounded plain text (2,000 characters).

A follow-up runs with the task creator's agent sign-in, GitHub access and
allowed secrets, so **only the creator's own comment queues one**. It goes
through the same retry path, acting as the task creator. The budget is five
comment follow-ups per task (`budget_exhausted` after that); the request ID is
derived from the task, attempt and comment, and unqueued follow-ups are resumed
by the same background loop. Because a follow-up moves the task out of
`awaiting_review`, further comments wait for the next published attempt.

A comment by anyone else, workspace admins and owners included, is stored with
the action `recorded`: it starts no attempt and uses no budget. It stays on the
issue and in the task's signal list, and the creator decides what to do with
it: comment on the issue themselves, approve the assistant's `follow_up_task`
([requested agent reviews](#requested-agent-reviews)) or retry the task. A
comment by another member therefore never starts a run that carries the
creator's credentials.

In the prompt, the name of the commenter (or of the GitHub reviewer) and the
text are both untrusted data inside one `~~~text` block. The name is reduced to
a single line of at most 100 characters without control or formatting
characters, and neither part can close the block. Comment creation itself never
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

A review runs only when the user asks for it in chat, or, when the workspace
turned on the GitHub bot's `mention_reviews` ability, when a trusted commenter
mentions the bot on the task's pull request and asks for one (see [mention-triggered
reviews](github-integration.md#kuayle-bot-identity-and-abilities-kua-96)); nothing else starts one
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
request is idempotent per (task, published commit, harness, model, model
server): it returns the newest such review unless it failed or was cancelled,
in which case a new review starts. Another model or server picked on the
approval card is a review of its own. A review without a chosen server records
the workspace default it runs on (when that server speaks the harness), so a
later default is a different review; an older review without a recorded server
is matched by the server its run used. `attempt` counts every review of the
commit with that harness (unique per task, commit and harness). Replaying the
approval's request ID returns its review.

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

Cancellation, failure and completion leave the task's machine as it is: a
machine Kuayle created for the task and a reused one both stay running, with
their volume and worktrees, so a later task can reuse them. The exception is a
start that has not begun: when a cancelling task has no attempt yet and its own
machine (the one created for its reservation, used by no other task or run) has
only the task's own spawn or start operations (its creation spawn and its
`task-start:` keys) that the manager has not leased and that made no attempt,
typically ones waiting for host capacity, cleanup cancels them instead of
waiting. A start the user requested on that machine is not withdrawn. A machine that never ran is then permanently deleted through the
ordinary teardown, which cancellation waits for; one that ran before (stopped or
paused, restarting for a retry) keeps its workspace and its desired state
returns to its current state. A reused machine, one another task waits for, and
a start already leased or attempted are left alone, and cancellation keeps
waiting for them as before. Revoked access cancels through the same cleanup. Machines are never
paused, stopped or torn down on a timer either; only their user does that.

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
task fencing. A task keeps the machine it was given; when it has none,
reservation uses a running machine of its creator before creating a new task
machine: same workspace and environment (so the same profile), not an
Environment Builder or being deleted, still allowed by the enabled policy (sudo
and Docker machines only while those stay allowed), without the user's own
agent session or a queued lifecycle, snapshot, check or custom tool operation,
and working on fewer other active tasks than the policy's
`task_machine_max_tasks` (default 10, from 1 to 10). An idle machine comes first (one already
used for the same issue, then the most recently active); otherwise the task
shares the least loaded busy machine. Paused and stopped machines are not
chosen, since their user paused or stopped them. The full rule is in
[task machine reuse](../../TECHNICAL.md#resource-limits-and-quotas). Each task
has its own checkout, branch and session directory; attempt creation re-checks
the limit under the machine lock, so tasks on a shared machine run their
attempts, validations and publications side by side while checks and tool runs
still wait for a machine without agent work. Agent attempt and check containers
of tasks sharing a machine split the agent memory share between them (at least
1 GB each; CPU and PID limits are not split), see
[Resource Limits and Quotas](../../TECHNICAL.md#resource-limits-and-quotas). Preparation starts the task's own machine when its
user paused or stopped it, and waits for a start already requested. Each start
is keyed by task, task generation and machine generation, so a machine paused
again is started again while a start that failed for good is not retried in a
loop. An exhausted checkout is requeued at most once per explicit task
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

A preview starts only when the task's creator asks for it: `POST
/agent-tasks/:taskId/preview` (202 with the task; 409 `PREVIEW_UNAVAILABLE`
without a preview command, or without the task's ready checkout on a running
machine) or the assistant's `start_preview` tool, which needs their approval.
Reaching `awaiting_review` queues nothing. The preview command runs files the
agent just wrote, so it never runs in the developer container, which holds the
creator's developer-scoped secrets and synced agent sign-ins and may allow
sudo. It runs in the task's own **preview container**, with the isolation of a
check container (below). Starting it still serves agent-written code to the
creator's browser, so it stays their decision: review the change before
starting its preview. `DELETE /agent-tasks/:taskId/preview` (202)
queues its stop. Preview operations name the task in
`dev_machine_operations.agent_task_id`. The manager then:

- records `preview_state` `starting`, then `running` or `failed` with a bounded
  `preview_error`, and finally `stopped`;
- picks the task's existing port, else the preferred port when free, else the
  first free port in 4100–4999; free means not a service port, not listening
  and not exposed (the number of app ports is not limited);
- exposes it as a `task` app port labelled with the issue identifier, served
  by the preview container (`target` `preview`, migration 136);
- creates and starts the container `kuayle-{routing-key}-preview-{taskId}` and
  runs the command in the task worktree as UID 1000. A restart replaces only
  that task's previous container. A command that exits with an error within a
  second fails the preview with its exit code and last error line.

**Preview container.** It is created like a check container: the developer
image (or the machine's environment image), the workspace volume, the machine
network (egress only through the proxy), a read-only root filesystem, no
capabilities, `no-new-privileges` (also on a machine that allows sudo), the
confinement profiles of agent containers, an empty in-memory home and
`/run/kuayle-secrets`, and bounded Docker logs. Its whole environment is the
image's plus `HOME`, `PORT`, `HOST=0.0.0.0`, the proxy variables and the run
directory variables (`/workspace/.kuayle/tmp/preview-{taskId}`, removed with
the preview). It receives no developer-scoped (`ide`/`terminal`) secret, no
model server key, no GitHub token, no agent sign-in, no live inbox and no agent
CLI mount; nothing is executed in the developer container to start it. On a
machine with its own Docker daemon it additionally gets what an attempt on that
machine gets for the daemon, and nothing else: the `docker-client` certificate,
written to its in-memory `~/.docker`, and `DOCKER_HOST`. Like an attempt it
shares the workspace volume and the machine network with the machine's other
containers; see [security limitations](../../TECHNICAL.md#security-limitations). It is labelled as an agent-class container
(`com.kuayle.service=agent`, `com.kuayle.preview-task-id`,
`com.kuayle.preview-port`), so it takes the agent share of the machine's CPU,
memory and process limits, counts as one more task when that memory share is
split, follows resizes and shared machine memory, and is included in the
machine's usage. The gateway reaches it on the machine network as
`{routing-key}-preview-{port}`; the port scan asks each running preview
container only whether its own port listens, and takes no port requests from
it. A daemon that publishes the task's port (a `docker compose up` preview)
serves it instead, until it stops; the developer container never serves a
task's preview port, whatever listens there.

A stop asks the command to end (SIGTERM, five seconds), removes the task's
preview container, removes its `task` port and revokes the port host's
sessions. It is queued when the task becomes completed, cancelled or failed,
when the user stops it, or when the user removes the task port, and is carried
out in any machine state. Stopping or re-provisioning the machine removes its
preview containers, marks its previews stopped and removes task ports; pausing
keeps them (the containers are paused and resume with the machine); deleting
the machine removes them with its other containers. A preview that an earlier
release started inside the developer container keeps running and keeps its
route until it is stopped or started again; both also end that process.
Task payloads carry `preview: {state, port_id, port, error} | null`; changes
are published as `task.updated` conversation events. The assistant's
`start_preview` (with approval), `stop_preview`, `expose_port` (with approval)
and `list_ports` tools use these same paths; see
[previews and ports from chat](conversations.md#previews-and-ports-from-chat).

Implementing attempts get one prompt line about manual QA: with a preview
command, that the reviewer can start it from the task; without one, that the attempt
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

**Registry entries.** A file with `"to":"registry"` (no channel or kind) is a
line for the wiki registry (migration 126): it is recorded first, so a file
taken twice is logged once and counts toward the same hourly budget, then
appended, acting as the task's creator with their visibility, to today's (UTC)
registry page of the issue's project when that project's registry is on, and
otherwise of the issue's team, prefixed with the issue identifier. The record
becomes `delivered` (to the sender's own issue, and never placed in a prompt)
or stays `rejected` with the reason, such as a registry that is off. Kuayle
also logs "pull request #N published: <url>" there, as the task's creator,
whenever an implementing task publishes, in the Standard flow and in
workflows; a registry that is off or fails never affects the task.

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
`orchestrator`. The task card's composer sends as `user` directly. The
assistant never calls it: its `message_task` tool only requests an approval,
and the approved text is queued by `QueueApprovedLiveMessage(ws, task,
request, body)` as `orchestrator`, under the same rules, with the approval's
request ID stored in `request_id` (migration 134, unique per task) so one
approval delivers one message. Only the task's creator may send, under the same conversation
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

Rolling migration 95 back drops the live channel's messages. Rolling migration
134 back removes `message_task` approvals and the `request_id` column; messages
already delivered are kept.

## Configurable workflows

Migration 118 lets a workspace replace the fixed flow above with its own
state machines (KUA-95). Nothing changes until someone picks one: a task with
no pinned version runs the built-in **Standard** workflow, which is the fixed
flow in code, and `StandardWorkflowGraph` only draws it.

- **Agent profiles** (`agent_profiles`): name, role (`implementer`, `reviewer`,
  `tester`, `docs_writer`, `security_reviewer`), harness, model, effort and
  instructions. Each workspace gets built-in Implementer and Reviewer
  profiles. Profiles carry no permissions: when a task is created, and again
  each time an agent step starts, its harness and model go through the same
  workspace policy check as a delegated task (allowed providers, model
  servers), and every iteration runs on the task's own machine under the
  workspace's repository, sudo and Docker policy. A step without its own
  model server uses the workspace's default one when it speaks the harness's
  protocol, as the composer and requested reviews do. The role decides how a
  version runs (a reviewer step reviews and leaves on its verdict; other roles
  start iterations), so it cannot change while any saved version (archived
  workflows' included) uses the profile (`409 PROFILE_ROLE_IN_USE`); the
  profile's row lock serializes this with saves. Saving a profile applies the
  model, effort and model server rules of a task start (not the allowed
  providers, which can change later) and answers `400` with the reason, for
  example a harness that takes no model or effort; `GET /agent-profiles`
  lists the harnesses that take them in `model_harnesses`
  (`harnessAcceptsModel` is the one rule: Codex, Claude Code and OpenCode)
  and, in `model_formats`, the form and an example of a harness's model
  names when they are not bare names, which the profile form shows.
  OpenCode's model is `provider/model` (`opencode-go/deepseek-v4.1-flash`)
  and its effort is passed on as the model's variant; a task start accepts
  any of Kuayle's levels. A profile save also checks a model of OpenCode's
  Go plan (`opencode-go/…`) and its effort against the
  [model catalog](../../TECHNICAL.md#agent-model-catalog) when that list is
  in memory (`validateCatalogModel`): an unknown model or an effort the
  model does not allow is a `400` with the reason. The check runs only on a
  save; a task start, retry or workflow step never reads the catalog or the
  network, so a stored profile keeps running if the list drops its model.
  OpenCode never runs on a model server (`harnessRunsOnModelServer`: Codex
  and Claude Code), so a step, task, retry or review on it gets no server
  and no server's default model or thinking level; with no model it runs the
  one OpenCode is configured with. Profiles stored before that check
  still load and are checked when they are saved again.
- **Workflows** (`workflows`, `workflow_versions`): a save inserts the next
  immutable version (an update trigger refuses changes) with the graph as
  JSONB; `expected_version` refuses a stale save. Archiving hides a workflow
  from choices; running tasks keep their pinned version.
- **Graph**: nodes `start`, `agent` (profile and iteration prompt template),
  `check` (`tests`: the independent check; `ci`: the pull request's CI),
  `approval`, `publish`, `merge` (waits until the pull request merges, through
  the [ship](conversations.md) chain or GitHub; the merge completes the task,
  so its `merged` or `else` transition must lead to a `done` end) and `end`
  (`done` or `failed`). A `ci` check fails at once on a failed check suite
  the webhook reports, and passes only when GitHub lists at least one check
  run or status for the published commit and all of them completed
  successfully with the whole list read (webhooks only report completed
  suites, so a fast suite passing says nothing about a slow one); it polls
  every minute while checks run and every five minutes while none started.
  Edges carry a condition the source node can produce (`agent_finished`,
  `agent_failed`, `checks_passed`, `checks_failed`, `review_approved`,
  `review_changes_requested`, `approval_approved`, `approval_denied`,
  `published`, `publish_failed`, `merged`, `timeout`, `else`); a reviewer-role
  agent step reviews the published pull request as a [review task](#requested-agent-reviews)
  and leaves on its verdict. `ValidateWorkflowGraph` requires exactly one
  start leading to an implementing agent step, at least one end, every node
  reachable and able to leave, every cycle containing a node with
  `max_visits`, and every agent step naming an active profile in the
  workspace. The repository validates again inside the save transaction, so
  an invalid graph is never stored. Validating, creating or saving a
  workflow, and choosing one as a default, also reports each agent step whose
  profile cannot start a task as an `unusable_profile` issue on that step,
  naming the step and the profile (`422 INVALID_WORKFLOW`). A task start
  refused for that reason says so: `WorkflowStepError` names the workflow,
  the step, the profile and what is wrong, as the `400`/`403` message of the
  task composer and as the error on the assistant's approval card.
- **Budget**: `max_iterations`, `max_minutes` and `on_exhausted` (`fail` or
  `ask`). A visit limit or the budget either fails the task or pauses the
  workflow for a person to continue or stop. Limits are checked at every
  transition, and the time budget also wakes a waiting step (reviewer, CI,
  approval, merge) when it runs out: the task then stays on that open step
  (`Hold`) and fails or pauses. An iteration that is running is not
  interrupted; its run's own time limit bounds it and the budget applies when
  it ends. Continuing passes a visit limit once; past a budget it grants
  another iteration budget of the same size and restarts the time budget at
  the decision (`workflow_budget_extensions`, `workflow_budget_from`), so
  later transitions do not pause again at once; a held wait waits again. The
  step's saved detail (failing checks, review comments) is passed on as it
  would have been. Cost budgets are a follow-up: runs do not record cost.
- **Defaults** (`workflow_defaults`): per workspace, team and project; the
  most specific wins. A task may choose any active workflow (`workflow_id` on
  task creation, `"standard"` for Standard); the assistant's `delegate_issue`
  accepts an optional `workflow` (name, ID or `standard`) resolved when the
  approval is requested, and automations use the defaults, all through
  `AgentTaskService.Create`. A custom workflow's first
  agent step decides the task's harness, model and effort.

Owners and admins create, save, duplicate, archive and set defaults; the
repository re-checks the role inside each write transaction and writes a
`workflow_audit_events` row for every change and every human decision.
Members read and choose; guests get `403` on the profile, workflow and
default routes and, as before, cannot create tasks (issue conversations refuse
guests), so they can neither edit nor run a workflow.

**Runner.** `agent_tasks` records the pinned `workflow_version_id`,
`workflow_node`, `workflow_visits`, `workflow_iterations`, `workflow_status`
and a `workflow_sequence` that fences every move, and
`agent_task_workflow_steps` records each node entered with the edge, condition
and reason that led there. The worker's existing lease is the single runner
per task: the task engine reports each outcome (agent iteration finished or
failed, independent check, publication) to `WorkflowRunner.Outcome`, which
plans the next moves and applies them, with the step rows, in the same
transaction as the task's state change. Agent steps reuse the iteration
machinery (each is an iteration whose reason names the node and transition),
check and publish nodes reuse validation and publication, reviewer steps reuse
review tasks, and waiting nodes (review, CI, approval, merge, timeout) park the
task in `awaiting_review` with `workflow_wake_at`. A waiting node is
evaluated as soon as it is entered (CI may have finished during the review),
then wakes at its timeout, the time budget's end or a CI poll; a finished
review or a CI result wakes it sooner, and the claim query picks the task up.
A reviewer step left before its verdict (a timeout, a budget, a person's stop,
or the task ending outside the runner) asks its review task to stop, as a
cancellation request does; a review whose row is locked by its own transition
at that moment is skipped (to keep the review-then-parent lock order) and its
result is ignored. Everything is in
the database, so a restarted API resumes where the task was. Prompt variables
(`{{previous_summary}}`, `{{review_comments}}`, `{{failing_checks}}`,
`{{diff}}`) are passed as fenced, untrusted text. CI results and comments on a
workflow task are recorded but never queue Standard follow-ups.

Routes (workspace, session only): `GET/POST /agent-profiles`,
`PUT/DELETE /agent-profiles/:profileId`, `GET/POST /workflows`,
`POST /workflows/validate`, `GET/PUT /workflows/:workflowId`,
`POST /workflows/:workflowId/duplicate|archive`, `GET /workflows/:workflowId/audit`,
`PUT /workflow-defaults`; under dev machines `GET /agent-tasks/:taskId/workflow`
(graph, path and visit counts) and `POST /agent-tasks/:taskId/workflow/decision`
(`approve`/`deny` an approval node, `continue`/`stop` at a limit; only the
task's creator or an owner or admin who can see the issue, for the step
`sequence` they saw). The UI edits workflows in Settings → Workflows and the
assistant's Workflow side panel (Svelte Flow), profiles in Settings → Agents,
and shows a task's run in its Agents panel.

Not implemented: per-profile skills and tool sets, sharing across workspaces,
parallel branches and workflows for non-coding automations.

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
guard. The task service applies the same demo rule itself (`service.MachineAccess`),
so paths that skip these routes (assistant approvals, automations, review
follow-ups, GitHub mentions) are refused for users outside `SYSADMINS`, and the
worker fails a task whose creator is excluded; see
[public demo mode](../../TECHNICAL.md#public-demo-mode). PAT scopes do not grant task access. Current creator/conversation/issue
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
- `POST /agent-tasks/:taskId/terminal` opens the creator's stopped Claude Code
  task in Claude Code ([open in Claude Code](#native-session-resume-and-handoff)).
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
A task stores only an explicit model and effort. Each attempt's run fills what
the task left empty from the workspace default of its agent on its own sign-in
when the run is created, so open tasks pick up a changed default on their next
attempt while an existing run keeps its own; see
[agent model defaults](../../TECHNICAL.md#agent-model-defaults).

`GET /agent-model-catalog?harness=<id>` (`dev_machine:read`) lists the models a
profile or task can choose for a coding agent, each with the effort levels it
allows: for OpenCode the public model list of its Go plan, which the API
server fetches from `opencode.ai` and caches in memory for an hour; for Codex
and Claude Code the models the workspace's model servers and agent defaults
already name. It always answers `200`: when the upstream cannot be reached
the last good list is marked `stale`, or the list is empty and `unavailable`,
and every model field stays free text. Sources, caching, the refresh rate
limit and the behaviour with blocked egress are in
[agent model catalog](../../TECHNICAL.md#agent-model-catalog).


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

## Pull request head tracking

Others can push to a task's pull request (a maintainer merging the base branch
into it), so the commit Kuayle published is not always the head. `pull_request`
`opened`, `reopened` and `synchronize` webhooks record GitHub's head in
`pull_request_heads`, only for pull requests Kuayle published. The recorded head
is a hint: `merge_pull_request` reads the head from GitHub again when the card is
requested and again when it is approved, before authorization-dependent effects.

The card carries the head as `commit`, the published commit as
`published_commit`, and `head_change`: the commits added since (authors,
subjects) and whether the tree changed, read from the two commits' trees (an
unknown comparison counts as changed). Kind is `merge` when every added commit
has several parents (the merged branch is not verified, so the tree decides
whether it is harmless), `rewritten` when the published commit is no longer in
the head's history, otherwise `pushed`. Approving merges exactly `commit`, still pinned as the
GitHub merge `sha`; if the head moved after the card was shown the approval fails
with a message asking to ask again, and a push that lands after the fresh read is
refused by GitHub with the same clear message. The merge is refused too when the
task's published commit changed.

Publication first fetches the task branch from GitHub with a credential used
only in a fresh repository, and fast-forwards the checkout to commits others
pushed. It never force-pushes; diverged histories fail the publication with the
reason. An agent review is stale when its commit is no longer the recorded head
or the task has published a newer commit:
`get_agent_work` withholds its findings and tells the assistant to offer
`request_agent_review` for the new head. Re-running a review still pins the
commit Kuayle published; reviewing others' commits needs the checkout updated by
the next publication.

## Usage limits

When the agent's provider refuses work until a quota resets (for example Claude
Code's "You've hit your 5-hour limit · resets 4:10am (UTC)"), the task is paused,
not failed: it becomes `blocked` with `usage_limit_message`,
`usage_limit_resets_at` and `usage_limit_resume_at`, keeps its worktree and
session, and resumes on its own after the reset by continuing the same native
session. The creator's other queued tasks for that agent wait for the reset.
`POST /agent-tasks/:taskId/resume` resumes now and `DELETE` on the same path
turns the automatic resume off. See TECHNICAL.md, "Usage limits".
