# Agent task execution contract

## Implementation status

The task domain, repository, worker and HTTP controls are wired into the API
process. Issue conversations expose task delegation, retry, cancellation and live
activity through AI Elements Task/Trace and Model Selector compositions. The
assistant delegates through approval-gated [orchestrator tools](conversations.md#orchestrator-tools).
Native success is followed by publication of a pull request (migration 59), after which the task waits in
`awaiting_review` until GitHub reports that pull request merged, an approved
merge from chat completes it, or its creator or a workspace admin accepts it in
Kuayle without merging ([accepting without merge](#accepting-without-merge)). CI failures, GitHub change requests and the
task creator's own issue comments queue bounded follow-up attempts; reviews by a second agent run only on
request (migration 84). That fixed flow is the built-in Standard workflow;
workspaces can design their own as [configurable workflows](#configurable-workflows)
(migration 118). Experimental [team workflows](#team-workflows-experimental),
off unless the server turns them on, run a task with a team of agent roles
that talk in the task's room, moved by a tick that reads facts. See
[remaining workflow work](#remaining-workflow-work) for what is not
implemented.

Terminology: texts users read (transition reasons such as "Native coding
iteration failed; stopping execution before retry", "New execution iteration
requested" and "Implementation iteration finished; preparing the pull request", check and tool-run errors, and assistant tool descriptions) call one
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

## Testing

Before finishing, the agent runs tests relevant to its change and reports what
it ran, the results, and what it skipped. Kuayle publishes the finished work
ready for review. The workflow Check node waits for GitHub CI on the published
pull request. Saved versions with an old local check follow its success edge (or its fallback) and record the step as skipped. New versions accept only CI checks. Existing database values for retired test commands are retained.

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
becomes the task's app port on the daemon. A manual check uses the subset
above, because the check container cannot join the daemon's Compose networks.


### Manual checks

Migration 97 adds the `run_check` machine operation. The task's creator can run
the check on demand, without an agent iteration and without changing the task's
state, generation or attempt. Send an explicit argv in
`POST /agent-tasks/:taskId/checks` as `{"command":["npm","test"]}`.
The requester needs `dev_machine:manage`. The response is 202 with the step.

The task must be `awaiting_review`, `blocked`, `failed` or `completed`, with its
latest attempt's redaction snapshot and ready checkout on a running machine with
an enabled policy (otherwise 409 `CHECK_UNAVAILABLE`; no command at all is 409
`NO_CHECK_COMMAND`). The request takes native-run creation's workspace and
machine locks and refuses with 409 `MACHINE_BUSY` ("the agent is working; try
again when it pauses") while an attempt or publication, or another manual check uses the machine. In return a queued or
running manual check makes new attempts wait (the worker retries quietly),
makes publication of other tasks on the machine wait, refuses
pause, stop and teardown like an active run, and blocks the task's retry.

The result is a `manual_check` step on the task's latest attempt (sequence 100
and up). The operation's idempotency key `task-check:<step id>` names the step. The manager uses a
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
task card shows it live, labelled "Manual check", in execution details.
`GET /agent-tasks/:taskId/runs/:runId/checks` lists an attempt's manual checks.

The assistant's `run_check(identifier, command)` tool asks for approval (the
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
be running with an enabled policy, and an attempt, publication,
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

### Worktree scans

Migration 157 adds the read-only `scan_worktree` machine operation and
`dev_machine_worktree_scans`. A scan reads one task checkout — grep with rg,
glob, a line range of one file, or git status — or lists the worktrees under
`/workspace/tasks`; it never changes the checkout and needs no credentials. A
task scan is allowed while the agent works: it does not take
`lockMachineForCheck`, and `LeaseOperations` lets it lease beside an agent
attempt, publication, check or tool run while a running scan never
blocks other work. A task target must be the requester's own task with a ready
checkout on its running machine; a machine-only target is only for the list
scan and must be a running machine the requester created.

The scan row and its operation share an ID (idempotency key
`worktree-scan:<id>`, one attempt). The manager loads the checkout's path,
branch and base branch and runs one fixed script per kind with the request only
as positional arguments. Grep and glob run rg in the checkout, which respects
its ignore files; the glob is applied after rg because rg's `-g` override would
otherwise include ignored files. Read resolves the path with realpath, refuses
anything outside the checkout or a non-regular file, and bounds the file to
4 MB. Every git command uses the publication preamble's throwaway git
directory, so the checkout's configuration, hooks, fsmonitor and attribute
programs never run; every checkout scan reports its branch, HEAD commit and
whether it is dirty, and status adds changed paths, ahead/behind the base
branch's remote-tracking ref, and the last commit subject. Output, matches,
files, lines, changed paths and worktrees are bounded (`Truncated`), and the
result is trimmed to the table's 64 KiB limit. A scan that cannot run is a
completed scan with a short error, not an operation retry. The table keeps a
workspace's scans for an hour.

## Publication

The [stopped-task publication approval](conversations.md#publishing-a-stopped-task)
queues the same `publish_agent` operation on the original checkout of a failed,
cancelled or blocked task. The operation checks for uncommitted changes and unpublished local commits before
syncing and publishing. It opens or updates a draft pull request marked
“not validated: published from a failed task”. The task's stopped state remains
in the task record, and the approval reports the publication result.

Task attempts never receive a GitHub credential (`push_branch` is false), so the
harness cannot push or open pull requests itself, and the attempt's container
has no git identity. An implementing attempt's prompt says so ("Publishing:
Kuayle commits and pushes for you…", `publicationLine`): leave the changes
uncommitted, do not report the missing identity or token as a problem, and end
with a final message. A planning step and a review do not get the line. In `publishing`, the worker
queues one `publication` step (sequence 1) and a `publish_agent` operation in the
fenced task transaction under the workspace and machine locks. The manager
re-verifies the same phase authority (task still `publishing`, current run,
creator, machine generation, enabled policy, live operation lease) on every read
and write, and checks that the checkout is the task's own ready checkout. It
fetches the task branch and refreshes the base tracking ref before starting a
new visit on a previously published task checkout, and fetches again at
publication. Reattaching to a running agent leaves its checkout alone. Only a
fresh repository receives the fetch credential. The implementing attempt's prompt and Kuayle's
[backend agent instructions](../../AGENTS/SKILLS/kuayle-backend/SKILL.md) ask
implementers to fetch and inspect the base before numbering a migration under
`BE/migrations` and again before publication.
When the remote branch advances, a clean checkout fast-forwards to its tip.
Pending edits are committed locally first and merged with that tip. A conflict
leaves the saved commit, unmerged files and merge state in the checkout.
The publication detail names the affected files, and the next agent receives
that detail and instructions to resolve and stage them. A configurable workflow
returns to its implementing step within its visit and iteration budgets. The
Standard flow queues one repair attempt. If the next publication is also
blocked, it waits for a person with the latest reason.
When the agent rebased its own branch and GitHub's tip still matches the
recorded publication (`PublishedCommit`), the checkout is left alone and the
push is guarded by `--force-with-lease` against
that tip. The commit step normally rebuilds the agent's changes on the published
commit, so the guarded push is a fast-forward; it really forces only when the
published commit is missing from the checkout, and then the push is refused
unless every commit it drops is Kuayle's own. Any other divergence is refused
with a reason saying someone else pushed to the branch, and nothing is
force-pushed. Then:

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
   re-adds the base's commits. The commit step always hands its tip to the
   push step, which decides whether anything is new (see below).
2. Copies exactly that branch tip into a new temporary repository outside the
   workspace volume, verifies it is still the recorded commit, and only then reads
   a fresh repository-scoped GitHub App token from stdin. It fetches the base and
   task branches from GitHub. A failure to read GitHub's task branch
   (`git ls-remote`) stops the publication instead of counting as no branch. A
   commit with nothing to publish is not pushed:
   merging it into the base as GitHub has it (`git merge-tree --write-tree`)
   leaves the base's tree unchanged. That covers work already merged, also when
   it was squashed and whether or not the base was merged back into the branch,
   which then still has commits ahead of the base. A conflict or any other error
   counts as a change, so the push goes ahead. Git older than 2.38 has no
   `merge-tree --write-tree` (usage error, exit 129); the check then falls back
   to `git diff <base>...<commit>`, which misses squashed work: it is pushed
   again, or, when only the checkout merged the base back, the task blocks as
   below. The developer images (Ubuntu 26.04) ship a newer git. Otherwise it
   refuses a push that would add a commit by
   another author or with a co-author trailer, since the checkout's
   remote-tracking refs the commit step relied on are agent-writable. If the
   task and base use the same number for different SQL migrations under
   `BE/migrations`, the publisher moves the task-owned pairs from the first
   collision onward to numbers above both branches' highest version. It
   updates literal references to those names in tracked text files and adds a
   `chore(migrations)` commit under the publication identity with the task's
   issue prefix. The checkout picks up that commit
   on its next sync. When a changed test or runner targets a version by
   number alone, the intended migration can be ambiguous. If ownership,
   pairing, ordering or references require manual repair, publication stops
   with the conflicting paths and sends the task back through the checkout
   repair flow. The migration check requires Node.js, which the developer
   images ship. If the runtime is unavailable, publication reports what is
   needed to retry. It pushes
   the commit to the fixed `https://github.com/<checkout repository>` URL, without
   force unless the sync returned a lease. The lease makes the push the
   `--force-with-lease` one described above, and a force is refused when it would
   drop a commit on the branch that is not authored and committed by the
   configured identity. The author check still covers every commit the push
   adds, including a rewrite's. The agent-writable checkout's configuration
   (hooks, URL rewrites, credential helpers) is never loaded, and the token is
   never written to the workspace volume. Before pushing, the publisher checks the
   installation grant used for that token. If outgoing commits change
   `.github/workflows/` without **Workflows: Read and write**, publication stops
   with `publication_workflows_permission`. A GitHub workflow-permission rejection
   has the same terminal outcome. The task names the files and explains how to
   [update the app and accept the grant](github-integration.md#github-app-setup-check).
3. Reuses an open pull request from the task branch into the base branch, or opens
   one ready for review with the GitHub App. A reused draft receives this attempt's
   description and is marked ready for review. The title carries the issue identifier so the existing
   `pr_opened` GitHub automation moves the issue to In Review; publication does not
   change the issue status itself. The description is rendered from the agent's
   `.kuayle/pull_request.json` and the configured template, sanitized (see
   [Pull request description](#pull-request-description-kua-210)). GitHub's URL
   must belong to the checkout repository.

When the branch has nothing to publish, nothing is pushed and no pull request is
opened or updated (KUA-176). The step is recorded as `skipped`, never
reviewable, with the summary "The agent made no changes against `<base>`;
nothing to publish. The work may already be merged, or the run only produced a
summary." (the base named from the task's checkout). The Standard flow blocks
the task with that reason, so a person can retry it with new instructions;
in a [configurable workflow](#configurable-workflows) it is the publish step's
`no_changes` outcome, which by default completes the task. A completed task
cannot be retried or given a follow-up (retry accepts only failed, blocked and
awaiting-review tasks): to continue a proposal-only task that ended this way,
delegate the issue again. Either way the run and its agent summary are kept.

The same check then runs on GitHub's task branch, when it exists. If that
branch still has changes against the base, an earlier iteration published work
there that the agent has since removed, and not pushing leaves that work in the
branch and its pull request. The push step prints `remains` instead of `none`,
and the manager names the open pull request it finds for the branch. When no
pull request is open, nothing is in a pull request any more (for example it was
squash-merged and the old branch now conflicts with the base), so the run counts
as nothing to publish like above. The step is still `skipped`. When a pull
request holds the removed work, the task is blocked, in the Standard flow and in
a workflow alike, with the reason "The agent removed all its changes, but the
open pull request `<url>` still contains the earlier work. Close the pull
request and cancel the task, or retry it with new instructions." Blocking also
drops a pause request that had not taken effect. In a workflow the publish step
stays open and the workflow does not move on; a retry continues on the
implementing step. The revert itself is never pushed.

The branch, commit and pull request git refs, the run's `branch`/`pull_request_url`
and the succeeded step commit together with a `task.publication_updated` event.
Moving to `awaiting_review` re-checks a succeeded publication step and a completed
operation in the same transaction, so an agent-reported URL never reaches review.
Retries after a lost result recommit nothing, push the same commit and reuse the
open pull request. Push or GitHub errors are retried up to the operation limit with
the token redacted; exhausted or terminal failures mark the step failed and block
the task with its work intact. Cancellation marks open steps cancelled.

### Pull request description (KUA-210)

The implementing attempt writes the description. The prompt
(`withPullRequestFile`) and the `kuayle-agents` skill ask it to write
`.kuayle/pull_request.json` at the worktree root:

```json
{"description": "the problem and the core idea of the fix",
 "changes": ["one short line per change"],
 "verification": ["what the agent ran and the result"],
 "notes": "only when a reviewer needs it",
 "screenshots": [{"file": "settings-dark.png", "caption": "Settings, dark theme"}]}
```

and, when it changed something people see, up to 6 PNG, JPEG or WebP files of
at most 2 MB each in `.kuayle/screenshots/`. Every key is optional. The file is
untrusted: publication reads it (and each listed screenshot) through the
developer container as the agent's user, as a regular file only and never
through a symlinked `.kuayle` or `screenshots` directory, at most 64 KiB.
`domain.ParseAgentPullRequestFile` rejects the whole file when a value has the
wrong type or a screenshot is not a plain `.png`/`.jpg`/`.jpeg`/`.webp` name in
that directory, and cuts longer texts and lists (description 1,500 characters,
notes 1,000, 15 items of 300). A rejected or missing file only leaves the
description to Kuayle's facts. Nothing under `.kuayle/` is committed, and an
attempt's start removes an earlier attempt's file and screenshots.

`publicationBody` renders the nearest issue, project, team or workspace
`pull_request_template` development setting (migration 172), or the default:

```markdown
## Description
{{description}}

## Changes
{{changes}}

## Verification
{{verification}}

## Screenshots
{{screenshots}}

## Notes
{{notes}}
```

`{{issue}}` is the issue identifier and title, the identifier linked to the
issue in Kuayle when the frontend address is known. Without `{{issue}}` in the
template, the description starts with "Addresses <issue>." Verification lists
the agent's own verification items. A section whose placeholders all render
empty is left out with its heading. A section with other text stays. Each
placeholder is replaced once, so agent text that contains a placeholder is
never expanded. Without the agent's file, the description uses the issue line
and any earlier screenshot evidence. The bot signature starts the description,
which GitHub shows as the bot's first comment. The body is limited to 60,000
bytes.

Sanitizing (`sanitizeAgentMarkdown`) applies to everything the agent wrote and
to the issue title: control characters are dropped; lines that would start a
heading, a code fence or a setext underline are escaped so agent text cannot
restructure the description. Raw HTML (`<`) is shown as text. Markdown images
in the description or notes can render when they link to commit-pinned files
in the same repository or to GitHub user attachments. Local image paths in
these fields are dropped, and other images are shown as text. `@` mentions
get a zero-width space. GitHub closing keywords followed by
an issue reference ("fixes #12", "closes owner/repo#3", "resolved: <URL>",
"fixes GH-5") get a zero-width space inside the keyword; and identifiers of
other Kuayle issues get one before the dash, so the pull request stays linked to
its own issue (forge linking refuses a description that names several). Inline
code keeps its text except closing keywords and identifiers. The template
itself keeps its markdown, loses HTML comments, and gets the same HTML, mention
and closing-keyword treatment, since issue-scope settings can be set by members
without repository access. Templates are validated on save
(`domain.ValidatePullRequestTemplate`): non-blank text of at most 8,000
characters using only `{{description}}`, `{{changes}}`, `{{verification}}`,
`{{screenshots}}`, `{{notes}}` and `{{issue}}`; a stored template that fails
the check renders the default.

Screenshots are never committed to the pull request branch. The manager checks
each listed file's size and magic bytes against its extension (PNG, JPEG or
WebP), then commits the accepted ones with the repository-scoped installation
token through the Git data API (`PublishFilesToBranch`) as the whole tree of a
commit on `kuayle/screenshots/<task id>`. The first commit starts a separate
history. Later commits extend that history, and an identical tree reuses the
branch tip. The description links
`https://github.com/<owner>/<repo>/blob/<commit>/<file>?raw=true`: GitHub serves
it with the viewer's own access, so screenshots of a private repository are
visible only to people who can read it, and the images live and are deleted with
the repository. Their commit has no workflow files, so pushing the branch runs no
Actions. The agent could already commit any file to the pull request branch, so
this adds no new way to publish content; a branch protection rule that refuses
the branch prevents new uploads and is reported in the description. Kuayle keeps
the branches after the pull request merges or closes so its description keeps rendering. An
administrator may delete `kuayle/screenshots/*` branches when the images are no
longer needed.

When an attempt updates an open pull request, Kuayle carries forward Markdown
images and standalone image tags that link to commit-pinned files in the same
repository or to GitHub user attachments. Before uploading new files, it checks
the screenshot branch and restores links from its latest snapshot when they
are missing from the description. A successfully uploaded screenshot replaces
earlier images with the same filename in the description. Other carried
images appear under **Earlier attempts** unless the current description or
notes already display them.
Each publish reports the links it restored or omitted, failures to read or
update the screenshot branch, and images that exceed the description limit.
A template without `{{screenshots}}` gets a Screenshots section whenever there
is evidence to show.

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
- **OpenCode** runs with `--auto` (approve what the configuration does not
  explicitly deny). Without it OpenCode auto-rejected reading the live inbox
  and paths outside the worktree, then ended the session, so the run failed
  without a final message.

Every flag an adapter passes is in the flag contract
(`devmachine/common/check-agent-cli-flags.sh`). An attempt runs the CLI from
the shared agent CLI volume only while the manager found that volume to fit
the installed release (at least the image's versions, passing the contract);
otherwise, also right after a deploy until the manager's start check, it runs
its image's CLI ([Agent CLI updates](../../TECHNICAL.md#sudo-in-machines)).

Agent, check and machine service containers (`secureHostConfig`) run with
Docker's bundled init (`HostConfig.Init`, tini) as PID 1: the agent CLIs do not reap the orphaned children they leave, such
as a `git` child of a tool run, and those zombies count against the container's
PID limit until the run crashes at it.

An autonomous attempt's home and `/tmp` are small tmpfs mounts (512 MiB and
256 MiB), so its temporary files, caches, and XDG data and state
(`XDG_DATA_HOME`, `XDG_STATE_HOME`) go to the attempt's run directory
`/workspace/.kuayle/tmp/agent-<run>` on the workspace volume
(`BE/internal/machine/run_tmp.go`), recreated when the container starts and
removed by the manager once the attempt ends. OpenCode keeps its SQLite
database (its sessions), logs and other state in `opencode/` under the data
and state directories; on the home tmpfs they filled it and runs failed with
`database or disk is full` (KUA-187). A task attempt links those two
`opencode/` directories to the task's session directory (below), on the same
volume, so its sessions outlive the run directory. Autonomous runs also give OpenCode `OPENCODE_CONFIG_CONTENT`
(`{"snapshot":false}`, merged over the project's `opencode.json`), because a
snapshot is a git copy of the worktree in its data directory, an unattended run
never undoes an edit, and those copies spend the workspace volume. Claude Code and Codex keep their state
in `~/.claude` and `~/.codex`, Qwen Code in `~/.qwen` and Kimi in `~/.kimi`,
whose session stores are linked or moved to the task's session directory too;
Goose keeps its SQLite session store under the data directory. Interactive
attempts keep the tmpfs defaults.

## Native session resume and handoff

Codex, Claude Code, OpenCode, Qwen Code, Kimi and Goose task attempts keep their native session **transcripts** in
`/workspace/.kuayle/agent-sessions/<task-id>/` on the machine's workspace volume,
outside every checkout, so publication never commits them. The attempt's argv is
wrapped so that, after the image's runner has loaded secrets and configuration,
only the harness's session store is linked to that directory: `~/.codex/sessions`
and `~/.claude/projects` (`sessionScript`), OpenCode's
`$XDG_DATA_HOME/opencode` and `$XDG_STATE_HOME/opencode` (`~/.local/share` and
`~/.local/state` when the run directory could not be prepared) as `opencode/data`
and `opencode/state` (`openCodeSessionScript`), Qwen Code's `~/.qwen/projects`
as `qwen/` (`qwenSessionScript`), or Goose's `$XDG_DATA_HOME/goose/sessions` as
`goose/` (`gooseSessionScript`, all in `BE/internal/agent/providers.go`). Kimi
finds a session through its index of working directories (`kimi.json`), which
it replaces on every write, so a link to it would not survive: its wrapper
(`kimiSessionScript`) points `KIMI_SHARE_DIR` at `kimi/` instead, which moves
Kimi's whole share directory (index, sessions, logs, telemetry and the default
`config.toml` it writes). An attempt's home starts empty and Kimi takes its
provider from the environment, so nothing secret is there.
`config.toml`, auth files and all credentials stay on the container's tmpfs home
(OpenCode's key arrives as the `OPENCODE_API_KEY` variable, never as a file)
and are never persisted. Transcripts can contain whatever the agent saw or
printed; they share the machine volume's trust boundary (the same as the task's
worktree) and are wiped as described below.

Claude Code runs receive a Kuayle-assigned `--session-id`, Kimi runs
`--session <id>` (which Kimi documents as creating the session when there is
none) and Goose runs `--name <id>` (`agent.KuayleNamesSession`); the ID is
recorded when the run is created. Codex reports its own
`thread_id` in its `thread.started` event, OpenCode its `sessionID`
(`ses_…`) at the top level of every event and Qwen Code its `session_id` in
the `system`/`init` event that starts its stream; the manager records the
first one it sees (`harnessSessionID`, `machine/manager.go`) once. Kimi prints
its ID only as text on standard error and Goose not at all (its own IDs, such
as `20261124_1`, can only be resumed, not chosen), so Kuayle names theirs. OpenCode 2 would also
create a session under an ID Kuayle passes to `--session`, but Kuayle records
the ID OpenCode generated, as for Codex, so it never depends on which IDs
OpenCode accepts. Migration 64 stores the ID on the attempt (`native_session_id`); migration 203
allows OpenCode's underscore there and in `agent_sessions`. The ID is
restricted to `[A-Za-z0-9_-]`, at most 128 characters, and starts with a letter or digit, so it cannot be read as a flag. A
retry or follow-up **resumes natively** (`codex exec resume <id>` with the same
settings as a new session, `claude --resume <id>`,
`opencode run … --session <id> -- <prompt>`, `qwen … --resume <id> <prompt>`,
`kimi … --session <id> --prompt <prompt>` or
`goose run --resume --name <id> …`) only when
the previous attempt succeeded on the same machine, where its transcripts live,
and was run by the same harness: a task whose profile switched harness between
attempts starts a new session, because the previous ID names the other
harness's session (`AgentHandoff.Provider`, the previous run's `provider_id`).
It also resumes a failed attempt whose session the user continued through
**Open in Claude Code** on that machine (`session_continued_at`, migration
103), so the user's turns are kept. Otherwise it starts a new session, so a
failed resume is never repeated. Either
way the prompt keeps the task instructions and appends a bounded handoff naming
the previous attempt, its native outcome, a recorded pull request, up to 50 changed files and the previous agent's
summary fenced as unverified text, because the harness never saw Kuayle's publication. An older handoff is replaced, not accumulated,
and the prompt stays within the attempt limit. Pi, Crush and custom harnesses
always start new sessions with the handoff: Pi runs with `--no-session`;
`crush run` prints only the
agent's text (no session ID) and `--session` continues only a session that
exists, so Kuayle can neither record nor name one. An unknown ID fails a
Claude Code, Qwen Code or Goose run; Kimi and OpenCode start a new session
under it.

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
its directory (every harness's session store included) with a short-lived helper container that has no network, a
read-only root, no capabilities and only `rm -rf -- <that directory>`; it works
while the machine is paused or stopped, and still runs after the machine's
generation changes. Failed tasks keep their transcripts so an explicit retry can
use them. Machine teardown removes the whole volume.

## Issue images

Each attempt receives the raster images (PNG, JPEG, WebP, GIF) embedded in its
issue's description and comments that the task creator can read, including
conversation images filed with `attach_images_to_issue`, whether as a comment
or as the issue's own attachments
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
unqueued by transient errors after 30 seconds.

A failed CI signal is classified before it decides anything (KUA-137):
`test_failure`, `build_failure`, `runner_or_network`, `timeout` or `cancelled`.
The webhook first matches the pull request to an agent task that is still open
to follow-ups, and only then reads GitHub: the suite's conclusion, the failed
checks' names, conclusions and output, each failed check's annotations (one
call), and the failed step and the end of the log of a failed Actions job
(`/actions/jobs/{id}` and `/actions/jobs/{id}/logs`, at most the last 256 KiB,
three job logs and eight seconds per suite). If those reads fail, the
conclusion and whatever was read still classify it, and the webhook never fails
because classification could not reach GitHub; with nothing else, it stays a
`test_failure` as before. Only strong runner or network signatures decide
`runner_or_network` (DNS resolution failures, an unreachable package mirror or
registry, a download reset or timed out, no space left, a runner shutdown or
lost communication, a build killed for memory), and only when every failed
check matches one and no failed step runs the change's tests. A truncated list
of failed checks is never the runner's. Such a failure is stored with its class
and evidence and the action `infrastructure`, starts no attempt and uses none
of the two CI fixes. A redelivery keeps the first decision (it never queues a
second follow-up) but corrects the stored class and evidence when this time the
GitHub read succeeded. The other classes follow up exactly as before.
`GET /agent-tasks/:taskId/signals` adds `failure_class` and `failure_evidence`
to each signal without changing its existing fields. The agent has no GitHub
credential and should reproduce the CI failure. The
new attempt then publishes to the same pull request.

What GitHub reported about the failed checks reaches the agent, the notebook
and the person (KUA-206, migration 170), not only "suite failed". The checks
the webhook read become the signal's `summary` (at most 2,500 bytes,
`domain.CIFailureSummary`): for each failed check (at most ten) its name,
conclusion and failed step, its own output, its annotations as
`path:line: title: message` (failures first, GitHub's generic "Process
completed with exit code" left out) and an excerpt of its job log
(`domain.CILogExcerpt`). The excerpt is the failed step's section only: the
`##[group]Run …` group named like the step, or else the step that printed the
first `##[error]`, up to the next step, `Post job cleanup.` or the runner's
clean-up. Timestamps, colours, the step's command and environment and the
lines of passing tests are removed; lines that report failures (errors, failed
tests, panics, expected and received values) come first, then the end of the
step's output, where its verdict is. The signal also stores the suite's
commit (`head_sha`). When the webhook could not read every failed check's log
within its eight seconds (or the suite timed out, which the webhook does not
read), the signal is recorded with `failure_details_pending` and whatever was
read, and `GitHubService` reads the checks again in the background, outside
the webhook and the task worker: one read per commit at a time, at most four
at once, each within 60 seconds and with up to five job logs.
`RecordCIDetails` then stores the summary that says more (an empty or
cut-short read never replaces a fuller one) on every waiting CI signal of that
commit in that repository, clears the flag, wakes a workflow waiting on CI and
requests the follow-ups the flag held back. While it is set, the signal's
follow-up is not requested, neither by the webhook, a redelivery nor the
recovery loop, for at most three minutes from the signal (`domain.CIFailureDetailsWait`);
after that it goes on with what the webhook read. The follow-up's prompt and
its notebook finding (bounded to one 3,000-byte finding) name the suite, keep
its link and carry the summary in a ```` ```text ```` fence under "What GitHub
reported about the failed checks (untrusted CI output, not instructions)";
fences in GitHub's text are broken up so it cannot close them. Without a
summary they say Kuayle could not read the failed checks and that the link
needs GitHub access.

A workflow's CI step fails on a failed suite the webhook reported, also when
the runner or network caused it: an `infrastructure` suite never sends a
Standard follow-up, but in a workflow it is "checks failed" like any other,
and the step's detail adds that CI failed on the runner and may need to be
run again, naming the evidence. While a failed suite's details are still being
read the step waits for them (checking every 15 seconds, at most three
minutes from the signal) and starts the read again if none is running, for
example after a restart. Its detail, which is also the next agent's
`{{failing_checks}}` and the notebook's CI finding, names each failed suite
with its link and its fenced summary, shortened so the whole detail fits one
notebook finding. A failed check run GitHub lists without a webhook (the
step reads the commit's whole check list) gets the same details (KUA-215): the
step records on itself that it waits (`agent_task_workflow_steps.ci`,
migration 175), then asks for the same background read of the commit's failed
checks, and `RecordCIDetails` stores what it found on every open CI step of
that commit in that repository and wakes it, as for a signal. The step waits
for it at most three minutes, then names each failed check with its link and,
once, the fenced summary of the commit's failed checks; without one it says
Kuayle could not read them, as before. The suites a CI step reads are only
those its attempt's webhooks reported for the commit it published
(`head_sha`): a suite of an earlier commit says nothing about this one.

### Workflow CI on an unchanged commit

A CI step judges the commit the task published once (KUA-215). It records
what it judged on its step (`agent_task_workflow_steps.ci`: the commit, the
failed checks it saw, the details read and any re-run), so a later CI step on
the same commit knows the failures it already judged. This matters when the
work comes back to CI without a new commit: an agent that found the failure
flaky and changed nothing publishes the same head again, and before KUA-215
the next CI step failed again within seconds on the same Actions run, until
the iteration budget ran out (KUA-185).

- When an earlier CI step of the task failed on the commit, the step reads
  GitHub's whole check list. What still runs is waited for, and CI that now
  passes passes (`checks_passed`, saying the commit failed CI before).
- Otherwise the old failures are no new result. The first time, Kuayle asks
  GitHub to run the failed jobs again (`rerun-failed-jobs` for each Actions run
  among the failed checks, as the workspace's GitHub App with a token that only
  has **Actions: write**) and records the request on the step; the step polls
  every minute. A check's identity is its link, and an Actions job run again
  gets a new one, so the jobs that failed before are not taken as the re-run's
  result; GitHub gets ten minutes to start them.
- CI that still fails on that commit pauses the workflow for a person
  (`blocked`, workflow `paused`) with "No changes were made since CI failed on
  `<commit>`, and CI still fails on `<checks>`" instead of sending the same
  failure back. The CI step closes as `checks_failed` with its detail, and the
  transition it would take is recorded without being entered. Continuing takes
  it with the failing checks (not the pause's reason) as `{{failing_checks}}`;
  stopping fails the task. A later unchanged attempt does not run the jobs a
  second time: the workflow pauses again at once. A transition that ends the
  workflow, or a limit, is taken as usual.
- When the jobs cannot run again (no failed check is an Actions job, the
  workspace turned pull request actions off, the app lacks Actions write) the
  workflow pauses at once with that reason; a person can run them on GitHub and
  continue. GitHub that cannot be reached is asked again five minutes later.

A new commit is judged as usual: its failure goes back to the agent.

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

### Merging only up-to-date heads

Kuayle itself merges only what a person approved, through one mechanism
(`mergeGate.merge` in `BE/internal/service/assistant_merge_gate.go`): an approved
`merge_pull_request`, the merge step of `ship_pull_request` (both through
`AssistantApprovals.mergeApproved`), and a workflow's ship approval or the merge
step right after it ([merging from a workflow](#merging-from-a-workflow)). CI
checks the pull request head, not a merge with its base, so a head without the
base's newest commits was never tested with them: two pull requests that each
added migration 162 merged green one after the other and broke `main`
(KUA-194). Before every merge Kuayle reads the pull request from GitHub again:

- The head must still be the approved commit, or a branch update Kuayle made
  and verified (below).
- **Behind its base** (`behind_by` > 0 in GitHub's comparison): Kuayle calls
  GitHub's update-branch pinned to the head it merges (`expected_head_sha`), as
  the approver, audits it (`pull_request.branch_updated_before_merge`) and records
  the new head as for any moved head, then does not merge yet: the approved
  merge **waits** for the checks of the new head and merges it once they pass
  (KUA-207, below). A comparison GitHub does not answer is not merged at once.
- **Conflicts with its base** (`mergeable_state` `dirty`, `mergeable` false, or
  update-branch refused for a conflict): nothing changes or merges. The failure
  says "the branch conflicts with `<base>`; needs a rebase" and names
  `update_pull_request_branch`, which sends the task agent to merge the base
  and resolve the conflicts.
- **Migration number collisions** under `BE/migrations` are checked before
  updating and before merging the resulting head. GitHub's branch update
  preserves the filenames from both branches. A workflow sends the
  task back to the implementing step with the conflicting paths and the
  renumbering instructions. For chat approvals, `update_pull_request_branch`
  can send those instructions to the task agent for approval. The repaired
  commit goes through the usual checks and merge approval. Kuayle reads the
  migration tree at the live base commit returned by GitHub's comparison.
  If GitHub cannot provide the complete tree, an approved merge waits and
  retries the check.
- **Up to date**: the head's checks (check runs and commit statuses, read as
  `get_pull_request_checks` does) must not be failing, still running or only
  partly readable. Checks register a moment after a push or update-branch, so a
  head reporting none is held like running checks while the repository uses CI:
  the base's tip (the comparison's merge base, which the head contains) has a
  check, its checks cannot all be read, or the head was committed less than two
  minutes ago. Only a repository without checks on its base tip merges a head
  without checks, leaving any required checks to branch protection.
- The merge stays pinned to the approved commit. GitHub's 405 "Base branch was
  modified" (the base moved during the merge), or an out-of-date refusal from
  strict branch protection, runs these checks again instead of failing, for at
  most three merges; a branch now behind is updated as above.

The gate decides from one read of the pull request into `TaskFacts`
(`readTaskFacts` in `BE/internal/service/task_facts.go`): state, draft, head,
distance from the base, conflict, migration collision and the head's checks. A
fact GitHub did not answer is `unknown`, never `no`. `unmetChecks` turns the
facts and the checks a step requires into what does not hold yet, each `unmet`
(someone acts), `waiting` (CI is running) or `unknown` (read again), with a
reason a person or an agent can act on.

**The board (KUA-262).** `GET /agent-tasks/board?repository=owner/name` lists
the repository's active tasks (not completed, failed or cancelled) with the
visibility of `GET /agent-tasks`: the caller's own tasks on issues whose team
they can still see (the repository shares `ownTaskRows`/`ownTaskVisible` with
`Queue`). It reads only Kuayle's records, never GitHub, so it can lag:

- pull request and head: the newest pull request Kuayle published for the task;
  the head GitHub last reported (`pull_request_heads`) when observed after that
  publication, otherwise the published commit. How far the branch is behind its
  base is not stored; `TaskFacts.Behind` reads it live.
- changed files: `changed_files` of the newest attempt that recorded any (the
  branch's `git diff --name-only origin/<base>...HEAD`, committed changes only),
  at most 200 listed; `changed_file_count` counts them all.
- `ci`: `failing` when a check suite reported on the head failed, `passing`
  when one passed and none failed, empty otherwise. Suites report only when they
  finish, so `passing` can hide one still running.
- `approved_at`: when a person approved the merge Kuayle is carrying out now (an
  unfailed merge wait on the current workflow step, or an assistant
  `merge_pull_request` waiting for checks).

`arrangeBoard` (`service/task_board.go`) derives the merge order: approved and
`passing` by approval time, then the other approved tasks by approval time, then
the other pull requests by number; a task without a pull request has no
`merge_position`. Each task's `overlap` lists the other tasks sharing changed
files (at most 20 named, `file_count` counts them) and whether each is `ahead`.
A step that read the board passes `boardOverlap` into `TaskFacts.Overlap`; the
`overlap` check is `waiting` while a task ahead changes the same files, with a
reason such as "KUA-123 (pull request #12) also changes BE/migrations/... and is
ahead of you in the merge order", and `unknown` when the board was not read.

**Waiting for the updated branch (KUA-207).** An approved merge is tracked as a
`domain.MergeWait`: the approved commit, the head Kuayle merges, its update
rounds and a deadline. After Kuayle's own update-branch:

- The new head is accepted only when it is exactly that update
  (`mergeGate.verifyUpdate`): its parents are the previous head and one commit
  that is on the base branch, and the files and changed lines the pull request
  adds on top of that commit (GitHub's `B...head` comparison) equal those the
  approved head adds on top of its merge base with it (`B...approved`). Hunk
  positions and context lines may differ, because base commits moved them; a
  file without a patch (binary or large) must keep its blob. So an update merge
  that also reverts a base commit or adds an edit is refused, as is a cut-short
  comparison. While GitHub has not created the new head yet the wait keeps the
  old head and verifies the new one when it appears; any other head ends the
  merge with "the pull request head moved".
- Every pass applies all of the gates again. Running or not yet registered
  checks, an unknown distance from the base and a partly readable check list
  keep it waiting; failing checks, a conflict, a closed pull request, a refused
  merge or a lost permission end it with the reason. A branch behind again is
  updated again, at most three updates counted from the head the person
  approved (`maxMergeUpdates`); the wait ends after two hours
  (`mergeWaitTimeout`) with what it waited for.
- An assistant approval waits as `waiting_checks` (migration 171: unresolved,
  with `next_check_at`); see [conversations](conversations.md#pull-request-tools).
  The ship chain keeps its merge step waiting within its tests timeout and saves
  the wait in its payload, so a resumed chain merges the same head.

Without an update of its own, the ship chain still waits, polling up to its
tests timeout, while the head's checks are running or have not appeared yet;
any other hold fails its merge step and nothing is released. A repository whose
CI runs on pull requests but not on its base branch is held only for the
two-minute window.
Reading the checks adds checks and commit statuses read to `merge_pull_request`'s
GitHub permissions. Pull requests merged on GitHub are outside this check:
GitHub's "Require branches to be up to date before merging" protection enforces
the same rule there, and `BE/migrations/versions_test.go` catches duplicate
migration versions once a head contains both. Kuayle merges on no other forge;
`domain.ForgePublisher` documents the rule and an `UpdateMergeRequestBranch`
capability for a future implementation.

### Merging from a workflow

A workflow merges its task's pull request when a person approves its **ship
approval** (KUA-209): an approval whose approved transition (or its `else`
without one) leads straight to a `done` end or to a `merge` step, or (KUA-216)
leads through agent steps that plan or change code, each finishing into the
next, to a `done` end while a CI check's `checks_passed` transition leads to
the approval (`WorkflowGraph.ShipApproval`). The CI condition tells a go-ahead
to ship from a plan's approval, which also leads to agents. The runner
(`BE/internal/service/workflow_merge.go`) uses the merge mechanism above.

- **Entering the ship approval** Kuayle marks a draft pull request ready for
  review (audited `pull_request.marked_ready_by_workflow`) and brings a branch
  behind its base up to date (`pull_request.branch_updated_by_workflow`); the
  step's reason says so. A branch that conflicts with its base leaves the
  approval at once with `branch_conflict`, an outcome no transition names: the
  work goes back to the first step that changes code (`WorkflowGraph.SendBack`),
  as an iteration counted and limited like any other, with Kuayle's instruction
  to merge the base and resolve the conflicts ahead of the step's prompt
  (publication never force-pushes, so the base is merged rather than rebased).
- **Before each agent review** a branch behind its base is brought up to date
  the same way, and a conflicting one goes back as `review_changes_requested`
  with that instruction; the reviewer's visit limit and the budget stop a loop.
- **While a CI step waits for checks** (KUA-260) it reads the pull request
  first (`readBranchFacts`, without its CI). A conflict or a migration
  collision leaves the step at once with `branch_conflict` and goes back as
  above, instead of the step waiting for checks that never start until it
  times out. A branch behind its base that merges cleanly is brought up to
  date, pinned to the commit the step judges, within the bound below; the
  step then judges CI on the new head once it is verified as merges of the
  base into the published commit (recorded on the step's `ci` as `head`,
  `update_of` and `updates`), and the ship merge accepts that head as Kuayle's
  update. A fact GitHub has not reported changes nothing; CI that already
  passed or failed decides as before.
- **A CI step judges Kuayle's update, not the stale commit** (KUA-264). When
  the pull request's head is a verified update of the published commit (an
  earlier step brought it up to date), the CI step judges that head from its
  first pass, never the published commit's older CI. So CI that failed on the
  update carries over: when the work comes back without a new commit, the
  next CI step judges the same update, runs its failed jobs again once and
  then pauses for a person, as for any unchanged commit.
- **While a step waits** (a running review, a ship approval, a merge step
  waiting for a person; KUA-264) each wake reads the pull request again
  (`followBase`) and does what entering a ship approval does, without marking
  a draft ready again: a branch behind its base is brought up to date, and a
  conflict or a migration collision leaves the step with `branch_conflict`
  and goes back as above (a running review is cancelled). A push to the base
  wakes waiting runs (KUA-257); a step's timeout and the budget wake it too.
  Each pass acts only on what GitHub reports then, so a wake on a branch
  already up to date changes nothing.
- **One bound before the approval, one for the merge** (KUA-264, KUA-277).
  The CI step and entering or waking a step count the updates of the
  published commit from the commits themselves (the chain of verified merges
  of the base on top of it, `verifiedUpdates`): none of them makes a fourth
  (`maxMergeUpdates`), and none updates a head someone else pushed, which the
  merge would refuse. A branch that falls behind once more after that stays
  behind until a person approves. The approved merge then has its own three
  updates, counted from the head the person approved: its first pass pins
  that head (`mergeGate.pin`), taking the published commit plus Kuayle's
  verified updates of it made before the approval, however many (at most
  `maxUpdateChain`, 12, which bounds what one pass reads from GitHub), and
  records them as `pinned_updates` on the merge, outside its bound. So a busy
  base that used every update before the approval no longer strands it: the
  merge brings the branch up to date and merges it once green, with no
  person. A fourth move after the approval ends the merge with "fell behind
  again after Kuayle brought it up to date 3 times", for a person; approving
  again starts another three from the head then. A head someone else pushed,
  before or after the approval, is still refused.
- **The step says why** (KUA-265). What a step does because of what Kuayle
  read about the branch is stored with the step as `ci.branch`
  (`domain.WorkflowBranchRecord`, no migration: any step's `ci` column can
  carry it): the action (`updated`, `update_limit`, `conflict`,
  `migration_collision`, `judged_update`, `merge_refused` with its refusal)
  and the facts it acted on (pull request, head, base, commits behind,
  conflict, migration collision, and CI on the head for a merge). The CI step
  stores it in its CI record; a waiting step stores it with its wake-up
  (`RecordWorkflowBranch`, fenced by the worker lease) or as it closes; an
  approved merge's refusal stores it next to the merge, as do the conflict and
  collision send-backs. The same decision on the same facts is not stored
  again. The step's detail in the agent view shows it as a sentence built
  from the facts. A refusal at a done end, which fails the task, keeps only
  its reason.
- **Approving** records the merge on the step that carries it out
  (`agent_task_workflow_steps.merge`, migration 171) with the approver and the
  commit the task published: on the approval itself when it leads to `done`, or
  on the merge step it leads to. The worker merges as the approver
  (re-checked as the task's creator or an owner/admin who can see it, like a
  decision) once every gate passes. The head it merges is the published commit
  or a chain of Kuayle's verified updates of it (any made before the approval,
  then at most three of the merge's own); anything else pushed to the branch
  was not approved and is refused. A branch Kuayle updates waits for the
  new head's checks, polled every minute, as above. Kuayle's merge completes the
  task, so the workflow reaches done only through it.
- **A merge that cannot happen** (failing checks, a head someone else pushed,
  a refusal by GitHub, the two-hour wait running out) keeps the task on that
  step with the reason as its reason and in the step's merge, and tells the
  creator a person is needed again; approving again retries, denying follows
  the denied transition. A branch that now conflicts with its base, or shares
  a migration version with it, goes back to the step that changes code at once
  with `branch_conflict` instead (KUA-264), as entering the approval does. While a merge runs nobody decides the step
  (`can_decide` is false) and its timeout does not fire.
- **A ship approval before agent steps** (KUA-216; the graph of KUA-171:
  `ci → ship → agent step → done`) is entered the same way. Approving records
  the merge on the approval, marked `deferred` and pinned to the published
  commit and its pull request, and the agent steps run as iterations. Their
  attempts publish no pull request of their own, so the pin keeps the one to
  merge. The `done` end they reach does not complete the task at once: it
  stays open in `awaiting_review`, carrying the merge
  (`AgentTaskRepository.DeferredShipMerge` finds it while only agent steps
  followed the approval; once the work left them, the approval no longer
  covers what ships and the end completes as usual). The worker merges as the
  approver; the merge may take a head the agent steps added commits to, when
  the approved commit is still in its history (GitHub's comparison), and only
  once that head's checks pass. Merging completes the task on the end (a held
  completion: the step closes as `done` with the merge as its detail). Nobody
  decides at an end, so a merge refused there (a head that no longer contains
  the approved commit, failing checks, the wait running out) fails the task
  with the reason; a person retries it or merges on GitHub. A pull request a
  person merges on GitHub while the end waits completes the task there, since
  the merge webhook only completes tasks whose attempt published the pull
  request. Commits someone
  else pushes while the agent steps run are merged with theirs when they build
  on the approved commit and pass CI: the approval was given knowing agent
  steps follow it.
- **Accept at the ship approval** (KUA-216): `POST /agent-tasks/:taskId/accept`
  on a task waiting at its ship approval approves it, exactly as approving
  does, and answers with the task still
  in `awaiting_review` while Kuayle merges; the task is done only after the
  merge. A repeated Accept while it merges returns the task. The task card and
  the approval say so (`ship_merge` on the task's workflow view).

A merge step reached any other way waits for a person to merge (ship from chat
or merge on GitHub), as before; graphs with a merge step after the approval
merge once, at that step.

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
  the ordinary task authorization; it answers 200 with the completed task. On a
  task waiting at its workflow's ship approval it approves instead and Kuayle
  merges (see [merging from a workflow](#merging-from-a-workflow), KUA-216).
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
  completed task. The request is to accept without merging and its card says
  so, so it never merges, also at a ship approval.

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
| `preparing`, `waiting_for_disk` | `starting` |
| `running` | `active` |
| `waiting_input` | `waiting_input` |
| `publishing`, `awaiting_review`, `blocked` | `suspended` (the harness exited; a follow-up can resume it) |
| `completed`, `failed`, `cancelled` | the same, with `ended_at` |
| `cancelling` | unchanged until cleanup settles |

The session records the task's current machine and the harness's native
session ID from the current attempt (Claude Code's, Kimi's and Goose's
Kuayle-assigned ID, Codex's `thread_id`, OpenCode's `sessionID`, Qwen Code's
`session_id`), picked up at the next transition; it is
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

- Its transitions never include `publishing` or
  `awaiting_review`; a succeeded harness goes from `running` to `completed`.
  `EnsurePublication` refuses review tasks, so no
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
and a Markdown list of findings. After the requester's focus it says that CI
is judged separately (by a workflow's CI step, or the people who merge), and
that the reviewer must not request changes because of CI it cannot see, a run
on an earlier commit, or a run on this commit that has not finished (KUA-206).
It then gives what Kuayle knew about CI on the reviewed commit when the review
was queued: a workflow's reviewer step reads every check run and status of
the commit from GitHub (five seconds at most) and lists each as passed,
failed or pending; a requested review, or a step that could not read GitHub,
lists the check suites webhooks recorded for that commit (`head_sha`) and says
the list is not complete. Check names are fenced as untrusted text. Without
any of that the prompt says CI on the commit is unknown and not to block on
it; with checks still running, that they are no reason to request changes;
with a failed check, that it supports a finding only when the reviewer can tie
it to the change. When the attempt succeeds, `CompleteReview`
reads the attempt's recorded summary (the harness's final message, already
capped at 4,000 bytes by the provider parser), normalizes the verdict, stores
findings as bounded valid UTF-8 (16 KiB column limit) and completes the review
in one fenced transaction. An answer without a verdict it can read fails the
review instead, in the same transaction, through the usual runtime cleanup,
with the reason `Agent failure: the reviewer's answer had no verdict`; no
verdict, findings, lesson or pull request comment is recorded for it (KUA-227).
A workflow's reviewer step then leaves on `agent_failed` with that reason,
never as an approval. Every review transition emits `task.review_updated` to the
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

The agent view's "Request review" menu (allowed harnesses, implementer first;
shown while the task awaits review with its pull request open) only writes
"Review ENG-12 with Codex" into the assistant's composer and returns to the
chat; chat remains the only way to request one. The requested reviews are
listed under the agent view's timeline. A review requested by another member
appears there for the task's creator; its full logs belong to the requester's
review task. The task card in the sidebar neither requests nor lists reviews.

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
Every run that does not succeed records a failure class (`agent`, `provider`,
`platform`, `ci_infrastructure`, `policy`, `person` or `unknown`), the rule that
decided and a bounded cause, redacted with the run's secrets. The class is
written where the reason is known: a person's cancel, pause or message
interruption is `person` (with a cause naming which), a policy refusal is
`policy`, a machine stop is `platform`, and the cleanup that follows a failure
keeps that failure's class. Cleanup never replaces a class the run already has.
The creator-only run endpoints show the full cause. A task shows its class in
words and one line of that cause (at most 300 characters), never the raw end of
standard error, because everyone who can see the task reads the reason. A
workflow task carries the run's class and cause into the step's detail and,
when the workflow ends in failure, into the task's reason and
`failure_class`. A paused task (a usage limit, a person's pause) has not
failed and keeps no failure class. A run whose harness output says a write
found no space (`database or disk is full`, `SQLITE_FULL`, `ENOSPC`,
`no space left on device`, `disk quota exceeded`) is `platform` with rule
`disk_full`, not the agent's `harness_error`. A provider account without credit
(rule `provider_credit`) pauses the task and a model the provider does not
offer (rule `model_not_found`) fails it; both are `provider` and neither goes
through a workflow's `agent_failed` edge (see
[Provider credit and unknown models](#provider-credit-and-unknown-models)).

A run classified as `platform` with rule `pid_limit` or `manager_restart`
(see [Machine manager restarts](#machine-manager-restarts)) is retried on the
same step. After runtime cleanup, a new generation and run ID continue the
native session when available. The step keeps its workflow visit and iteration.
The task and notebook record the interruption and retry count. After a PID-limit
crash, the handoff tells the agent to continue from the worktree with fewer
parallel workers. A step gets up to three automatic retries
(`agent_tasks.platform_retries`, migration 000162). The count resets when the
workflow starts another step or a person retries the task. The next eligible
failure after the cap ends the task with class `platform` and the latest cause.
A person's cancellation during cleanup ends the task. Other platform rules,
including `out_of_memory`, `time_limit` and `platform_error`, follow their
existing failure paths.

A native failure caused by a resource limit (an out-of-memory kill of the agent
container, see [resource limits](../../TECHNICAL.md#resource-limits-and-quotas))
fails with that explanation as its reason, kept as the final `failed` reason.
Configuration failures after reserving an environment also enter cleanup: a
preparing task may already have started native execution between worker polls.
An authorized repeat cancellation request can retry failed cleanup operations
once their underlying cause has been repaired.

Cancellation, failure and completion leave the task's machine running with
its workspace volume, so a later task can reuse it. The exception is a
start that has not begun: when a cancelling task has no attempt yet and its own
machine (the one created for its reservation, used by no other task or run) has
only the task's own spawn or start operations (its creation spawn and its
`task-start:` keys) that the manager has not leased and that made no attempt,
typically ones waiting for host capacity or disk space, cleanup cancels them instead of
waiting. A start the user requested on that machine is not withdrawn. A machine that never ran is then permanently deleted through the
ordinary teardown, which cancellation waits for; one that ran before (stopped or
paused, restarting for a retry) keeps its workspace and its desired state
returns to its current state. A reused machine, one another task waits for, and
a start already leased or attempted are left alone, and cancellation keeps
waiting for them as before. Revoked access cancels through the same cleanup. Machines are never
paused, stopped or torn down on a timer either; only their user does that.

The manager queues `cleanup_task` after a 30-minute grace period for completed
and cancelled tasks, including tasks that ended before the operation was
introduced. A failed task is eligible when its last published PR for the
branch is closed or merged, or after a seven-day retry window if it published
a PR. A failed task without a recorded publication keeps its checkout. An
active check, preview or task terminal delays cleanup.

The operation retires the checkout and uses an isolated container with the
machine's workspace mounted at `/workspace` to run `git worktree remove`
and remove that task's per-attempt temp directories. The task's branches are
retained, so a follow-up can prepare its checkout again. Docker-enabled agent
attempts set `COMPOSE_PROJECT_NAME` to the normalized worktree directory name
(including the task UUID), so Compose labels volumes with the same project
even when started from a subdirectory. The machine's Docker daemon removes
detached volumes with that project label or `com.kuayle.task-id=<task UUID>`.
Volumes still referenced by a container are logged and skipped. Volumes from
older or explicitly named Compose projects, including issue-only names like
`kua177-gocache`, are cleaned only when either ownership label matches.
A paused or stopped machine with a Docker daemon is cleaned when it next runs.

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
and working on fewer other active tasks than the machine's own `max_tasks`, or
the policy's `task_machine_max_tasks` when it has none (default 10, from 1 to
10; the override is migration 156 and can change while the machine runs). An
idle machine comes first (one already
used for the same issue, then the most recently active); otherwise the task
shares the least loaded busy machine. When every suitable machine of the
creator already runs `task_machine_max_tasks` tasks, the task waits for a free
slot on one of them instead of creating a task machine the host cannot start: it
stays `preparing` with "Waiting for a free task slot on <name> (<tasks> of
<limit> in use)..." and does not count a failure. A task whose own task machine
was created but never started moves to a free slot when one appears, and that
unstarted machine is deleted. The limit bounds concurrency on a machine, not
the number of machines, so raising it (up to 10) lets more tasks run there at
once. A machine excluded from selection (running the user's own agent session,
or with a pending stop, pause, teardown, snapshot, check or custom tool
operation) counts as no suitable machine, so a task creates a task machine
while such a machine exists. Paused and stopped machines are not
chosen, since their user paused or stopped them. The full rule is in
[task machine reuse](../../TECHNICAL.md#resource-limits-and-quotas). Each task
has its own checkout, branch and session directory; attempt creation re-checks
the limit under the machine lock, so tasks on a shared machine run their
attempts and publications side by side while checks and tool runs
still wait for a machine without agent work. Agent attempt and check containers
of tasks sharing a machine split the agent memory share between them (at least
1 GB each; CPU and PID limits are not split), see
[Resource Limits and Quotas](../../TECHNICAL.md#resource-limits-and-quotas). Preparation starts the task's own machine when its
user paused or stopped it, and waits for a start already requested. Each start
is keyed by task, task generation and machine generation, so a machine paused
again is started again while a start that failed for good is not retried in a
loop. An exhausted checkout is requeued at most once per explicit task
generation, so polling cannot silently produce unlimited setup retries.
A task whose machine cannot start below the host disk floor moves from
`preparing` to `waiting_for_disk`. Its status shows the free space measured at
the refusal and the configured floor in GB and percent. Workspace owners and
admins receive an inbox notice once per breach observed by the manager. The
manager retries the queued start without consuming an attempt. The task worker
then picks the task up in `preparing` and continues the same iteration. Disk
wait time is excluded from the workflow budget.
A new task machine is a custom-size machine with the workspace policy's task
machine CPU, memory and disk (default 4 vCPU, 8 GB, 20 GB), headless unless an
environment applies; see [Resource Limits and Quotas](../../TECHNICAL.md#resource-limits-and-quotas)
for its limits and how its memory is split between the agent and terminal.

**Merged work asks first (KUA-193).** When the task's issue has a merged pull
request (`github_pull_requests`) or merge request (`forge_merge_requests`)
linked, a retry or a "send and start now" that would start a new iteration is
refused with `409 MERGED_WORK`, `error.pull_request` naming the newest merged one
(`number`, `url`, `repository`), unless the request carries `"confirm_merged":
true`. The check runs in the retry transaction after replay, state and
pending-work checks, scoped to the workspace and the caller's issue visibility;
a refused request records nothing, so the UI asks ("This issue's work was merged
in #N. Retry anyway?") and sends the same request ID again confirmed. Plain
live messages, resuming a paused step, continuing after a sign-in and automatic
CI, review and comment follow-ups never ask. The assistant's `follow_up_task`
fails with that pull request until the model calls it again with
`confirm_merged` after the user agreed; the card then shows the merged pull
request and approving it confirms. A card proposed before the merge fails on
approval instead. A follow-up proposed by `update_pull_request_branch` shows the
merged pull request on its card the same way.

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

### Machine manager restarts

On SIGTERM the manager stops leasing operations and stops following running
agent attempts and manual checks without stopping their containers: they keep working on the host, each
follower hands its operation back with `DeferOperation` and error code
`manager_restart`, and this counts no attempt. A check keeps its compose
services, and its time limit still counts from when it started. A stopping
manager starts no new agent, check or closing-continuation container: a closing
continuation that has not started is handed over before the attempt is marked
as asked, so the next manager asks. Other operations in progress (a closing
continuation already running, a publication, lifecycle work) get a drain
period, `DEV_MACHINE_SHUTDOWN_DRAIN_SECONDS` (default 90 seconds; 0 stops at
once). After the drain, the manager cancels outstanding work and hands it
back. An interrupted [assistant command](conversations.md#commands-and-temporary-machines)
ends as failed so that one approval runs the command only once.
Compose gives the manager a `stop_grace_period` of 150s, which leaves a minute
after the drain for those cleanups.

The next manager leases the operation at once and re-adopts the run or check:
it finds the container by name, replays its log from the start (stored lines
are de-duplicated) and waits for it to exit, then records the result as usual.
A manager killed without a graceful stop leaves the same handover to the
two-minute operation lease. An unobserved exit is not a guess: a run whose
container had already ended with exit 137 or 143 (a Docker stop or a clean
Docker daemon shutdown) while no manager followed it, for example under an
older manager that still stopped runs, is classified `platform` /
`manager_restart`, not `unknown` / `killed`. Any other exit keeps its usual
rule: a crash by another signal (139 SIGSEGV, 134 SIGABRT) is the harness's own
and stays `unknown` / `killed`, and a host crash, whose containers exit with
255, is not a manager restart. `disk_full` and `usage_limit` evidence also wins
over a restart. A started run whose container no longer exists is not
re-created: it is recorded as failed with `platform` / `manager_restart` right
away.

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
for output goroutines to exit; a stopping manager instead detaches from the
running container without stopping it, for the next manager to re-adopt (see
[Machine manager restarts](#machine-manager-restarts)). The full provider result
is parsed from sanitized captured output, and never establishes independent test
success.

The manager buffers logical lines, redacts known secrets before persistence,
including multiline fragments and JSON-escaped forms, and sanitizes invalid UTF-8
and NUL. A line of a multiline secret is redacted on its own only when, trimmed,
it has at least 8 characters and a letter or digit, and every string value of
8 or more characters in a JSON secret is redacted wherever it is printed
(`sanitize.SecretReplacer`). So the punctuation of a pretty-printed JSON
credential, such as the Codex sign-in's `{` and `}` lines, never redacts the
braces of the output, while its tokens stay redacted on their own, inside an
event or escaped (KUA-227). Redaction then only replaces string content, so
event lines stay JSON and the final message is parsed from them. Each stream is capped at 4 MiB and 4,096 logical records, plus a limit
marker. A stored line never exceeds 16 KiB. A longer line that is one JSON
object (a harness event, up to 1 MiB) is redacted whole, its decoded values are
redacted again, and then every string value over a limit is cut to it (the
limit starts at 8 KiB and halves until the line fits, so short values such as
a command stay whole), keeping the start and the end around a
`[N bytes cut by Kuayle]` note: an
OpenCode `tool_use`, a Claude Code `tool_result` or a Codex
`command_execution` with a large output stays a valid event with its tool,
command and status (KUA-136; OpenCode writes a call's input and output on one
line). The record is not flagged `truncated`, so it still yields its event.
Any other line over 16 KiB (not JSON, still too large after cutting, or over
1 MiB) is suppressed whole and replaced by `[Oversized output line omitted]`
rather than exposing a partial secret or an incomplete structured event. Full
terminal text stays in the paginated native log store.

The 16 KiB limit applies to the line without Docker's timestamp.

The run's result is parsed from the redacted lines before they are cut, so a
final message longer than a stored line is still the summary (cut at the 16 KB
summary limit and flagged `summary_truncated`). Kuayle's own markers are not
part of what is parsed, so they are never a summary. After a stream's storage
cap nothing more is stored, but its lines are still read, redacted and given
to the result parser (the newest 1 MiB of them), because a harness reports its
final message last: a long run keeps its closing `result`, `agent_message` or
`text`. When the parser did not get some line (too long to keep, open when
storage stopped, or pushed out of that 1 MiB), a run without a visible final
message keeps its exit status instead of failing, with a risk note.

**Summary and final message.** A run's summary is the agent's final message,
never its events: with no message, JSON event lines are left out and the
summary is what else the harness printed (plain output; standard error only
when it exited nonzero), usually nothing. For Claude Code, Codex, OpenCode and
Pi, whose final-message event Kuayle knows, a run whose output holds events and
that exits 0 without a final message is recorded as `failed`
(`result.no_final_message`), with the error "The agent ended without a final
message, so there is no summary of what it did", followed by the error the
harness reported last, if any (OpenCode's top-level `error` event, for example
a provider's 429, or the `errorMessage` of Pi's last assistant message, which
Pi writes when the provider kept failing although it then exits 0). Text followed by a tool call or by such an error is not a
final message; it stays the summary. The task fails with that reason (kept
through cleanup), or its workflow step leaves on `agent_failed` with the
reason in its detail and, when the workflow ends there, in the task's reason.
Only work clears the final flag: a Codex `command_execution`, `file_change`,
`mcp_tool_call`, `web_search` or `collab_tool_call` item, an OpenCode
`tool_use` and a Pi assistant message with a tool call; a `todo_list`, `step_finish` or reasoning after the message does
not. "command not found" in a shell tool's output, a shell exit code 126 or 127
and a tool error are risk notes: they fail the run only when the agent did not
conclude;
output of other tools is not scanned. The
next attempt of the task keeps the worktree (attempts share the task's
checkout) and its handoff says that the previous attempt ended without a final
message, to check the worktree, finish and end with a summary.
The agent is not asked again in the same session (the
[closing continuation](#closing-continuation) asks only an attempt that
concluded): a workflow that wants a retry already has one (an `agent_failed`
transition back to the step, bounded by its visit limit). Other harnesses (Qwen Code, Kimi, Goose, Crush, custom) keep their
exit status.

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

Workspace administrators manage rules in **Settings → Dev Machines → Automations**.
A rule matches a team's issues by label, status, or both. Each issue matches a
rule once. Open blockers and an active task exclude an issue. The active limit
(1–5) counts pending approvals, starts and unfinished tasks.

A rule inherits the issue's workflow unless it selects a published workflow in
the same workspace. Following latest resolves the version at task creation.
**Pin the current version** keeps the version published when the rule is saved.
The first runnable step must have an active agent profile. Starting a task
checks the actor's current access, workflow and agent policies. Archiving the
workflow prevents new starts, including pinned ones. Remove its rules before
deleting a workflow. Removing a rule also removes its run history.

Choose **Any time**, a UTC hour, or a five-field cron expression and IANA
timezone. For example, `30 9 * * 1-5` in `Europe/Rome` checks at 09:30 local
time on weekdays. A missing DST minute is skipped and a repeated minute is
eligible twice. Settings shows the next scheduled match. On recovery, overdue
cron rules check current issue state once and advance to the next future
occurrence. The original `run_hour` keeps matching throughout that UTC hour.

Matches and approval notifications commit together. The owner or a workspace
administrator can approve or dismiss a pending run. Approval runs the task as
the approver. An unattended run acts as its owner and needs an enabled
workspace policy allowing unattended starts. Revoking that permission before a
retry sends the run back for approval.

### Running the automation worker

Supervise `./server automation-worker` as a separate process using the same
backend binary, database and Dev Machine policy, keys and demo settings as the
API. It starts matching immediately and polls every 15 seconds without opening
an HTTP port. Set `AUTOMATION_WORKER_EXTERNAL=true` on the API replicas when
using dedicated workers. Existing single-process installations keep an embedded
worker by default. Stop all writers, including standalone workers, before
migrating or restoring the database. Restart them on the matching server version.

Workers lock due rules and lease each task start for two minutes. A committed
task is recovered using the original actor and request ID. Transient errors
back off for 30 then 60 seconds. Three unsuccessful attempts, or one permanent
refusal, leave a failed run with a reason. Open a rule's **Run history** to see
its issues, tasks, current workflow versions and steps, attempts and failures.
The original actor can replay a failed start while still authorized to decide
on it. Replay checks the rule's active limit and keeps the same request ID.

- `GET/POST /agent-automations`, `PATCH /agent-automations/:id` (`{enabled}`)
  and `DELETE /agent-automations/:id` require workspace administration.
- Create accepts optional `workflow_id`, `version_policy` (`latest` or `pin`),
  and either `run_hour` or `cron` with an optional `timezone` (default UTC).
- `GET /agent-automation-runs` returns the newest 50 visible runs. Pass
  `automation_id` for one rule and `before` with the last run ID for older rows.
- `POST /agent-automation-runs/:id/approve|dismiss|replay` records a decision.

Migration 193 preserves existing rules. Downgrade pauses workflow and cron
rules and dismisses their outstanding starts and approvals so an older server
cannot start the wrong workflow or cadence. It drops scheduling and retry metadata.

## Agents as issue assignees

Migration 142 makes an agent profile a member that can be assigned issues and
mentioned in comments (KUA-107). It adds no second execution path: assigning or
mentioning an agent creates a task through `AgentTaskService.Create`, in the
acting member's own issue conversation, exactly as chat delegation and
automations do.

**Identity.** An agent identity is an active `agent_profiles` row; its ID is the
profile's. `agent_identities` holds the avatar colour, and
`agent_team_policies` the teams where the agent can be assigned and mentioned.
A team without an `assignable` row does not offer the agent, so agents are
opt-in per team. Only current owners and administrators change either table,
re-checked in the writing transaction. `GET /agents` also reports whether the
workspace's machine policy lets each agent's harness and model run now.

Switching a team off for an agent, or moving an issue to a team where the
agent is not enabled, only refuses new assignments and mentions. Agents already
assigned stay assigned, and their tasks keep running, until someone unassigns
them; the settings page says so.

**Permissions.** An agent holds none. Its task's creator is the member who
assigned or mentioned it, so every step runs under that member's current
workspace role, team visibility, machine policy and credentials, re-checked by
the worker as for any task. A guest cannot assign, unassign or mention an
agent; a member who cannot see the issue's team cannot either. The routes are
session-only: a personal access token never puts an agent to work.

**Assignment.** `issue_agent_assignees` is one row per (issue, agent), beside
`issue_assignees`, which stays people-only. `PUT /issues/:identifier/agents`
with `{agent_ids}` replaces the set in one transaction that locks the issue,
refuses a guest (`AGENT_ASSIGN_FORBIDDEN`) or an agent that is archived, from
another workspace or not enabled for the issue's team (`AGENT_NOT_ASSIGNABLE`),
and records an `agent_assignees` history entry. A refusal changes nothing.

Each added agent then starts a task as the assigner. The task stores
`agent_profile_id` and takes its harness, model and effort from the profile.
The profile's instructions join the inherited project instructions, and its
skills and profile files are given as for a workflow step. It follows the
built-in flow (implement, publish, await review), not the issue's
default workflow, whose first step would otherwise replace the agent. The
assignment's `request_id` makes the start idempotent. A refusal that cannot
succeed as things are (agents disabled, provider not allowed, the assigner lost
access, the team policy switched off) is recorded on the assignment as `error`.
A transient failure is retried by a worker every 20 seconds for 15 minutes
after the assignment; then the assignment records an error and is no longer
retried, so stuck starts cannot crowd out newer ones. Assigning the agent again
retries. If the agent is unassigned while its task is being created, the start
finds its assignment gone and cancels the task it just created.

**Unassignment.** Removing an agent cancels its task when the task is still
working: `queued`, `preparing`, `waiting_for_disk`, `running`, `waiting_input`, `validating`,
`publishing`, `blocked` or `failed`. Cancellation is the normal `cancelling`
transition, recorded with the member who unassigned as its actor, so the
runtime is stopped and cleaned up as for a cancelled task. A task
`awaiting_review` keeps its pull request and stays reviewable, and a finished
task is left as it is. The task lifecycle has no "stop after the current
attempt" state, which is why unassignment cancels instead. As with people,
any non-guest member who can see the issue may unassign an agent, including one
another member assigned: this cancels that member's task but gives the
unassigner nothing of the task's permissions, output or credentials. The web app asks
before unassigning an agent whose task would be cancelled.

**Mentions.** A comment posted in a signed-in session that @mentions an agent
enabled for the issue's team is delivered once the comment is saved:

- no open task of that agent on the issue: a task starts for the commenter
  with the comment as its request, fenced as text; the agent is not assigned;
- an open task the commenter started: the comment is queued on its
  [live channel](#live-channel); a task `awaiting_review` is left to the
  [comment follow-up](#kuayle-issue-comments), which already starts an attempt;
- an open task someone else started: nothing is sent, because that task
  carries its creator's permissions and credentials.

Comments from tokens, the assistant and agents never reach a mentioned agent.

**Status and history.** Issue responses carry `agent_assignees`, each with the
assigner and the task's state and reason, so the agent shows as an assignee to
everyone who can see the issue. A workspace event `issue.agents_updated` with
only the issue ID follows every assignment change, including a start the
worker settles, and every state change of a task that runs as an agent; clients reload the issue's agents under their own
access. History rows written by such a task set `agent_profile_id`, and the
history API returns `agent_name`: the entry reads as the agent acting on behalf
of the row's user.

**Lists.** The `assignee` list filter accepts an agent ID as well as a user ID,
and `none` means no person and no agent. `agent_assigned_by` keeps issues with
an agent the given user assigned. Grouping by assignee orders people first,
then issues assigned only to agents, then unassigned ones.

- `GET /agents` lists agents for non-guest members; `PUT /agents/:agentId`
  (`{avatar_color, team_ids}`) is administrator-only.
- `GET /issues/:identifier/agents` and `PUT /issues/:identifier/agents`.

Not built yet: choosing agents in the create-issue dialog, bulk actions and
issue templates; assigning from tokens, the MCP server or the assistant; an
agent's own page listing its work.

## App ports and QA

Migration 71 adds an inherited preview setting: an argv command and a port,
snapshotted onto each task. Since migration 85 the port
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
(the Kuayle-owned `kuayle-agents` skill, installed for Claude Code, Codex and
OpenCode in the agent, IDE and headless images, describes it; implementing prompts with
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

**Lessons.** A file with `"to":"memory"` and a `lesson` proposes a lesson for
the repository's [workspace memory](#workspace-memory) instead of a message;
only a refused one is recorded here.

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
agent works. The agent view, the chat and the assistant use it as described in
[agent messages in chat](conversations.md#agent-messages-in-chat).

**Sending to the agent.** `AgentTaskService.QueueLiveMessage(ws, task, sender,
body)` (repository `QueueLiveMessage`) records a message from `user` or
`orchestrator`. The agent view's composer sends as `user` directly. The
assistant never calls it: its `message_task` tool only requests an approval,
and the approved text is queued by `QueueApprovedLiveMessage(ws, task,
request, body)` as `orchestrator`, under the same rules, with the approval's
request ID stored in `request_id` (migration 134, unique per task) so one
approval delivers one message. Only the task's creator may send, under the same conversation
and team checks as the other task actions (another member gets not found);
only implementing tasks that are not completed, cancelled or being cancelled
take messages (a task whose agent is only being stopped by a pause or an
interruption still does); the body is cleaned like an outbox body (at most 4,000 characters);
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
`delivered` with `delivered_via=inbox`, the run ID, the workflow step
(`workflow_node`) and one more `deliveries`. A failed mark rewrites
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
agents' messages, as "Messages from the person who owns this task", each with
its number ("Message 3"), oldest first, at most 10 within the prompt room, and
marked `delivered_via=prompt` with the attempt's run ID; recreating the
attempt places the same messages, and messages claimed for an attempt that
was never created are claimed again. Reviews receive none. A message written
to the inbox in the 20 seconds before the harness exited may not have been
read: when the attempt ends it is queued again (`SettleLiveMessages`, measured
against the harness's exit time, not the time Kuayle gets round to settling)
instead of counting as delivered.

**A reply is mandatory, and the work is never lost over a missing one**
(KUA-132, migration `000151_live_message_replies`). Every message delivered to
an agent, through the inbox or the prompt, must be answered. A reply is
exactly this: an outbox file addressed to the orchestrator whose `reply_to` is
the message's sequence number,
`{"to":"orchestrator","reply_to":3,"body":"..."}`, saying what the agent
understood and what it will do differently. It is recorded as a `from_agent`
message with `reply_to`; a `reply_to` that names no message the task received
is refused (`rejected`), `reply_to` on a file for another recipient is
invalid, and the final message or a file without `reply_to` does not count.
The agent's 30-per-hour budget never drops the first reply to a message.
Kuayle asks for the reply, not only the prompt:

- While the attempt runs, the manager keeps `unanswered.json`
  (`{"unanswered":[3,5]}`) in the inbox: the messages this attempt received
  and has not replied to (`UnansweredLiveMessages`), rewritten whenever that
  changes, after the outbox was collected. An attempt that never owed a reply
  has no such file.
- **Claude Code** cannot stop while that list names a message. The `Stop`
  hook first waits up to 12 seconds for the list to empty (a reply written
  just before stopping is collected within a few seconds), then answers
  `{"decision":"block"}` with the message numbers, the reply format and the
  message text. It asks at most 3 times per message in one attempt
  (`AgentLiveStopReminders`; the count is in the hook's state file) and then
  lets Claude stop. `SubagentStop` and tool calls are never held. A list the
  hook cannot read holds nothing.
- **Asked once more when the attempt has ended.** An attempt that ended well
  (exit 0, a final message, not cancelled) while a message it received has no
  reply is asked for the replies in its
  [closing continuation](#closing-continuation), the one extra turn an
  attempt gets for whatever is missing (`domain.ReplyClosingAsk`; every
  built-in harness but Pi and Crush, whose sessions Kuayle does not continue). The outbox is
  collected again afterwards. It happens once; there is no loop.
- **A missing reply never fails the attempt.** If there is still no reply the
  attempt stays as it ended: the work is checked and published as usual, with
  or without a workflow. The result carries `unanswered_messages` and a risk
  note ("The agent did not reply to message 3…"), the agent view shows the
  message as "No reply" and marks its step, and the message is given once more
  to the next attempt of the same workflow step, if there is one, marked "the
  previous attempt received it and ended without replying; reply first".
- **The bound.** A message is given to at most 2 attempts that ran to an end
  (`AgentLiveReplyAttempts`, counted in `deliveries`); after that Kuayle stops
  asking and it stays unanswered. An attempt that was interrupted, paused or
  cancelled does not count: it was not allowed to finish, and the attempt that
  continues it takes its messages over. A message is never carried to another
  step: only the attempt right after the one that received it, on the same
  `workflow_node`, gets it.
- **A reply Kuayle could not collect is not a missing reply.** The attempt's
  messages are settled only when the outbox was read and recorded. When that
  failed (the machine's services could not be listed, the developer container
  is gone, the read or the write failed) and the attempt owed a reply, nothing
  is concluded, the agent is not asked again, and the result's risk notes say
  the problem is on Kuayle's side (`agent_live_messages.collect_failed`). The
  result has `replies_unknown`, the run carries it in the task's timeline, and
  the agent view shows such a message as delivered, not as "No reply". A
  message that reached the inbox just before the harness exited is queued
  again all the same.

Messages delivered before migration 151 have `deliveries = 0` and owe no
reply. Every implementing prompt, the Claude hook's text and the
`kuayle-agents` skill state the reply format.

A live message can include up to six images uploaded to the task's conversation.
Send their IDs in `attachment_ids` on the live-message request. The server
binds the creator's pending images in the same transaction as the message and
its send mode. An image-only message can have an empty body. The manager
copies each image to the run's read-only inbox before delivering the message
or starting the iteration whose prompt references it. During live delivery,
a missing or size-changed image is omitted with a note in the message. Other
images and later messages still arrive. Temporary storage failures are retried.
The chat history uses the original authenticated conversation thumbnails.

**Sending now.** `SendLiveMessage(ws, task, body, mode)` queues the creator's
message as above and, in the same transaction, acts on `mode`:

- `interrupt` ("Interrupt and send") stops the working attempt and continues
  it with the message. It works for every implementing task, with or without
  a workflow (`CanInterruptForLiveMessage`: the task is `preparing`, `running`
  or `waiting_input`, its run has not ended, and it is not paused or failing).
  The attempt goes through the usual runtime cleanup (`cancelling`, with
  `live_resume_generation` naming the attempt it will continue as; the task's
  JSON then has `"interrupting": true`), which keeps the machine, the
  checkout, the worktree and the session transcripts. When the cleanup has
  confirmed the attempt gone (`FinishCancellation`), the task goes straight
  back to `queued` as a new attempt on the same step, visit and iteration
  (nothing is counted), with `resume_native_session` (so `claude --resume`,
  `codex exec resume`, `opencode run --session`, `qwen --resume`,
  `kimi --session` or `goose run --resume --name` continue the session, when
  the same harness ran the interrupted attempt; Pi, Crush and custom harnesses get
  the handoff) and the
  message in its prompt; the handoff says a person interrupted the attempt to
  say something and that the worktree is unchanged. It is not a pause: a
  pending "pause after this step" stays as the person asked for it, and the
  workflow pauses after the continued step. Who interrupted and when is in the
  task's transitions. A cleanup that fails is started again, as for a pause.
  If the attempt cannot be continued the task says so: a workflow is paused on
  its step ("Paused: the agent was stopped to deliver your message but could
  not be started again…", Resume continues it), any other task is blocked
  ("…Send and start now, or retry the task, to continue"); the message is
  delivered then; the interruption is then over (`live_resume_generation` is
  cleared), so that Resume or Retry is an ordinary one. A person's cancel
  always wins: it clears the mark whatever the task's state, so the cleanup
  cancels and never starts the attempt again. When no attempt is working the
  request is refused with 409 and nothing is queued. A second message sent
  while the attempt is being stopped joins the first.
- `start` ("Send and start now") starts an attempt when none runs. A task
  whose agent a pause stopped resumes that step (nothing is counted). A
  `failed`, `blocked` or `awaiting_review` task starts a new iteration on its
  last agent step exactly as Retry does, except that it keeps its prompt and
  continues the previous attempt's native session however it ended, when the
  same profile ran it (`AgentTask.LiveResumed`; another profile's session is
  never continued). In a workflow that is a new visit of that step: it counts
  one visit and one iteration against the workflow's limits (checked when the
  workflow next decides), the working-time budget keeps running from where it
  was, and a step that was waiting (CI, an approval, a merge, a review) is
  closed as retried, so that wait is abandoned. The agent view therefore asks
  once before starting a task that is `awaiting_review`. A working task, one
  in cleanup, or one paused by a usage limit is 409; a completed or cancelled
  task takes no message at all.
- without a mode the message waits for the working attempt's inbox or the
  next prompt. A harness Kuayle cannot hand a message to in the middle of a
  turn (`HarnessReadsLiveInbox`: every harness but Claude Code) is interrupted
  as with `interrupt` whenever its attempt can be, otherwise the message waits
  for the next attempt; the assistant's approved `message_task` messages
  follow the same rule.

**Rollout order.** Deploy the backend (with migration 151) before the agent
images. The new backend works with old images: their hook and skill never
write `reply_to`, so replies are simply not recognised, the continuation asks
for them once, and messages show "No reply"; nothing fails. The reverse order
does not work: an old backend refuses an outbox file with `reply_to` as an
unknown field, so every reply a new image writes would be recorded as
rejected.

**Agent to orchestrator.** While an attempt runs the manager also collects the
machine's outboxes every 5 seconds (and still during the port scan and at the
end of the attempt). An outbox file `{"to":"orchestrator","body":"..."}` may
add `"kind":"question"` (default `message`), `"channel":"internal"` or
`"live"`, and `"reply_to"` (the sequence number of the message it answers);
`kind` and `reply_to` are refused for issue recipients. Such files are recorded in
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
| POST | `/agent-tasks/:taskId/live-messages` | `{body}` and optionally `{"mode":"interrupt"\|"start"}` (unknown fields refused); 201 with the queued message, always sender `user`; 400 invalid text, an unknown mode or a review task, 409 an ended task or a mode the task's state does not allow (nothing is queued), 429 over the hourly budget |

Messages carry `reply_to` and `deliveries`. Sending and reading are the
creator's alone, whatever the mode: workspace admins and the owner have no
access to another person's agent. An admin, the owner, another member, a
guest and a creator who lost access to the team are answered 404.

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

Rolling migration 151 back keeps replies as plain agent messages and forgets
which message each answered; a task whose agent is being stopped for a
message at that moment is then cancelled by that cleanup instead of continued.
Rolling migration 95 back drops the live channel's messages. Rolling migration
134 back removes `message_task` approvals and the `request_id` column; messages
already delivered are kept.

## Workspace memory

Migration 149 adds short lessons per repository (KUA-134): how to run the
checks, conventions, known traps. Agents, reviews and members only
**propose** lessons; a workspace owner or admin approves, edits or drops each
proposal, and only approved lessons, as that person left them, are ever
handed to an agent. Putting them in a task's prompt is one call,
`service.WorkspaceMemoryService.ApprovedLessons(ctx, task)`, which the prompt
assembly makes for every attempt that plans or changes code (see
[Lessons in the prompt](#lessons-in-the-prompt)). The task notebook's
migration is 150, so that it sorts after this one.

**Storage.** `workspace_lessons` holds one lesson per row: the workspace, the
repository (`repository_full_name`, matched case-insensitively through
`repository_key`, the same identity as `dev_machine_checkouts` and
`repository_merge_methods`), the `state` (`proposed`, `approved`, `retired`,
`dropped`), `text` (what agents see once approved) and `proposed_text` (what
was proposed, never changed), the source (`source_event`: `task`, `review` or
`manual`; `proposer`: `agent`, `external` or `person`; `source_task_id`;
`proposed_by`), the source task's `team_id`, who decided and when, the prompt
`position` and a `dedupe_key`. `workspace_lesson_events` records every
proposal and decision (`proposed`, `added`, `approved`, `edited`, `shared`,
`retired`, `dropped`, `reordered`) with the actor and a detail.

**Limits** (`domain/workspace_memory.go`). A lesson is one line of at most 400
characters, without control characters, zero-width characters or
bidirectional overrides (U+200B–U+200F, U+202A–U+202E, U+2066–U+2069,
U+FEFF), so an approver sees everything an agent would read. A repository has
at most 30 approved lessons and 50 waiting proposals; a member has at most 5
hand proposals waiting on a repository, and a task proposes at most 3 lessons
per kind of event. A proposal discarded for a limit is logged with the
repository and the kind of source, never its content. The block placed in a
prompt is at most 4,000 bytes: lessons are taken whole, in order, until the
next one does not fit.

**Proposals.** Nothing calls a language model; each source is text that
already exists, cut to one lesson when longer, for a person to edit:

| Source | Text | `source_event` / `proposer` |
| --- | --- | --- |
| An agent, typically when its task ends or after a person corrected it | `{"to":"memory","lesson":"..."}` in its [outbox](#agent-messaging) | `task` / `agent` |
| A requested agent review that ends with `request_changes` | Its findings | `review` / `agent` |
| A trusted GitHub change request | Its summary. The author has write access on GitHub, which is not workspace membership, so the text is marked as external | `review` / `external` |
| A member, by hand | `POST /memory/lessons` | `manual` / `person` |

A person's messages to a task (live messages, issue comments) and CI results
propose nothing: a message is private to the task's creator and is not a
lesson, and a failed check's name and summary teach nothing. A correction
becomes a lesson when the corrected agent proposes it through its outbox
(the `kuayle-agents` skill asks it to) or when a member writes it by hand.

A task's proposal is recorded in the transaction of the event that caused
it, inside a savepoint: a proposal that fails is logged and never fails the
outbox batch, the review or the signal (a failed outbox lesson is recorded as
a refused message). The repository is the one of the task's checkout; a task
without a checkout proposes nothing. The same text (without regard to case
and spacing) is proposed once per repository and kind of event, whatever
became of the first proposal, so a dropped proposal does not come back. An
outbox lesson that is malformed, too long, over the task's limit, from a
review task or from a task that ended is refused and recorded as a refused
message to `MEMORY` in the [message registry](#agent-messaging), within the
task's hourly message budget; an accepted lesson is a proposal, never a
message.

**Redaction.** Text is redacted before it is stored, so `text`,
`proposed_text`, API responses and audit details never hold a secret the
redactor knows: the secret values of the source task's machine (the values
its run output is redacted with) for a task's lesson and for a person's edit
of it, and well-known credential formats (`sanitize.RedactCredentials`, the
release scan's rules) always. The API and the machine manager call
`repository.ConfigureLessonRedaction` at startup; a process that did not, or
that cannot read the secrets, stores no lesson. Lessons are redacted again
when they are placed in a prompt.

**Trust.** An agent's lesson and review findings are stored as written by an
agent, a GitHub reviewer's as external, and reach nobody's prompt as they
are: approving is a person's act, and what is handed on is the `text` of the
approved row. A lesson is retired (`approved` → `retired`) or dropped
(`proposed` → `dropped`) for good; both states are final.

**Who reads what.** Owners and admins read every lesson. Any other non-guest
member reads the approved lessons, the lessons they wrote by hand, and the
waiting proposals of tasks they created: a task's proposal carries text from
that task, which stays with its creator and the people who decide until it is
approved. `proposed_text`, the text before a person edited it, is returned
only to owners and admins.

**Teams.** A lesson that came from a task keeps that task's team (`team_id`),
and the team's privacy is read when the lesson is: while the team is private,
only people who can see it see the lesson (`kuayle_team_access`) and only its
tasks receive it; while it is public the lesson reaches every task on the
repository. A team made private later therefore takes its lessons with it. A
lesson written by hand belongs to the workspace, and an owner or admin can
share a team's lesson with the workspace (`workspace_wide`), which is
audited. Today a workspace with private teams cannot have development
machines, so such lessons cannot yet arise from tasks; the rule is enforced
in the queries all the same.

**API** (session only, under `/api/workspaces/:slug`). Non-guest members read
and propose by hand; only current owners and admins decide, re-checked inside
each writing transaction.

| Route | Who | Effect |
| --- | --- | --- |
| `GET /memory/lessons?repository=&state=` | members | Lessons the caller may read, with `can_manage` and the limits; approved ones in prompt order |
| `POST /memory/lessons` `{repository, text}` | members | A lesson written by hand for a connected repository: approved at once for an owner or admin, a proposal otherwise |
| `POST /memory/lessons/:lessonId/approve` `{text?}` | owners, admins | `proposed` → `approved`, optionally with edited text, placed last |
| `PUT /memory/lessons/:lessonId` `{text?, workspace_wide?}` | owners, admins | Edit a proposed or approved lesson; share a team's lesson |
| `POST /memory/lessons/:lessonId/retire` | owners, admins | `approved` → `retired` |
| `POST /memory/lessons/:lessonId/drop` | owners, admins | `proposed` → `dropped` |
| `PUT /memory/lessons/order` `{repository, ids}` | owners, admins | Prompt order; `ids` names every approved lesson once |
| `GET /memory/lessons/:lessonId/audit` | owners, admins | The lesson's audit trail, newest first |

Refusals: `403` for guests and non-members (and members on decisions), `404`
for a lesson of another workspace or of a team the caller cannot see, `400`
`INVALID_LESSON`, `LESSON_REPOSITORY`, `LESSON_ORDER`, and `409`
`LESSON_STATE`, `LESSON_LIMIT`, `LESSON_PENDING_LIMIT`, `LESSON_DUPLICATE`.

**In a prompt.** `ApprovedLessons(ctx, task)` resolves the repository of the
task's checkout and returns a heading and one `- ` line per approved lesson,
or `""` when there is none or no checkout yet.
`ApprovedLessonsForRepository(ctx, workspaceID, teamID, repository, machineID)`
does the same for a known repository. Both redact the machine's secret values
and well-known credential formats again, and fail rather than return
unredacted text when the secrets cannot be read.

**In Settings.** Settings → Lessons
([+page.svelte](../../UI/src/routes/%5BworkspaceSlug%5D/settings/lessons/+page.svelte),
[lessons.ts](../../UI/src/lib/features/memory/lessons.ts)) is the whole
interface, with no explanatory text: "Proposals" at the top, one dashed list
for every repository, each proposal with its repository, where it came from
("Written by an agent", "From a GitHub review", "Proposed by <name>"; a link
to the task's agent view when the reader can open that task) and, for owners
and admins, "Edited" opening the text as it was proposed. Its three actions
are approve, edit (the dialog's button approves with the edited text) and
drop (asked once, as it is final). Below, each repository has its numbered
list of approved lessons, in the order agents read them; a lesson's one
control is a menu with edit, move up, move down and retire (asked once). A
proposal is in no repository's list, which is how the page says that nothing
reaches an agent before approval. "Add lesson" writes one by hand for a
connected repository: a lesson at once for an owner or admin, a proposal for
any other member, who otherwise sees the approved lessons and their own
proposals with no actions. Retired and dropped lessons are not listed. A
lesson of a private team the reader can see carries a lock icon named
"Private". A refusal is said in the reader's language, chosen by the
server's code (`lessonRefusal`), with a generic line for anything else. A
`409`, a `404` or `LESSON_ORDER` means the page is out of date: the list is
read again, a confirmation is closed, and the dialog is closed when its
lesson is gone or was decided meanwhile; a refusal that leaves the lesson as
it was (a limit, a duplicate) keeps the dialog and its text. Guests are not
offered the page in the settings menu, and its address answers with the
"unavailable or no access" line and nothing to retry. Browser coverage: `UI/tests/e2e/lessons.test.ts` as an admin
and as a member, at desktop and phone widths.

Rolling migration 149 back drops the lessons and their audit trail.

## Configurable workflows

Migration 118 lets a workspace replace the fixed flow above with its own
state machines (KUA-95). Nothing changes until someone picks one: a task with
no pinned version runs the built-in **Standard** workflow, which is the fixed
flow in code, and `StandardWorkflowGraph` only draws it.

- **Agent profiles** (`agent_profiles`): name, role, behaviour, harness, model,
  effort and instructions. The **behaviour** is what the engine acts on:
  `plans`, `changes_code` or `reviews` (see [Behaviours](#behaviours)). The
  **role** is a label the workspace chooses and no rule reads: optional, at
  most 40 printable characters on one line, stored trimmed. Migration 143
  replaced the fixed role list: `reviewer` and `security_reviewer` profiles
  became `reviews`, the others `changes_code`, each keeping its old role as
  the label (`Docs writer`), so existing workflows run as before; rolling
  back restores the five roles (a planning profile becomes an implementer and
  other labels are lost). Each workspace gets built-in Implementer and
  Reviewer profiles. Profiles carry no permissions: when a task is created, and again
  each time an agent step starts, its harness and model go through the same
  workspace policy check as a delegated task (allowed providers, model
  servers), and every iteration runs on the task's own machine under the
  workspace's repository, sudo and Docker policy. A step without its own
  model server uses the workspace's default one when it speaks the harness's
  protocol, as the composer and requested reviews do. The behaviour decides how a
  version runs (a reviewing step reviews and leaves on its verdict; the others
  start iterations), so it cannot change while any saved version (archived
  workflows' included) uses the profile (`409 PROFILE_BEHAVIOUR_IN_USE`); the
  profile's row lock serializes this with saves. The role can always change. Saving a profile applies the
  model, effort and model server rules of a task start (not the allowed
  providers, which can change later) and answers `400` with the reason, for
  example a harness that takes no model or effort; `GET /agent-profiles`
  lists the harnesses that take them in `model_harnesses`
  (`harnessAcceptsModel` is the one rule: Codex, Claude Code, OpenCode and Pi)
  and, in `model_formats`, the form and an example of a harness's model
  names when they are not bare names, which the profile form shows.
  OpenCode's model is `provider/model` (`opencode-go/deepseek-v4.1-flash`)
  and its effort is passed on as the model's variant
  (`provider/model#level`; without a model the effort is not passed); a
  task start accepts any of Kuayle's levels, and OpenCode fails a run whose
  model lacks that variant. A profile save also checks a model of OpenCode's
  Go plan (`opencode-go/…`) and its effort against the
  [model catalog](../../TECHNICAL.md#agent-model-catalog) when that list is
  in memory (`validateCatalogModel`): an unknown model or an effort the
  model does not allow is a `400` with the reason. The check runs only on a
  save; a task start, retry or workflow step never reads the catalog or the
  network, so a stored profile keeps running if the list drops its model.
  Pi's model is also `provider/model`; its effort is passed as `--thinking`
  (Pi has no catalog check). OpenCode and Pi never run on a model server
  (`harnessRunsOnModelServer`: Codex and Claude Code), so a step, task,
  retry or review on them gets no server and no server's default model or
  thinking level; with no model each runs the one it is configured with. Profiles stored before that check
  still load and are checked when they are saved again.
- **Stage assistants** (KUA-233, migration 184). In the node inspector, an
  owner or admin can assign a profile with the “changes code” behaviour to a
  start, check, publish, approval, merge or end. Each visit runs the assistant
  in the task worktree before the built-in action, using the profile's harness,
  model, instructions and skills. The run counts against the workflow budget.
  The assistant receives the stage settings, task context and the pinned
  version's outgoing transitions, with each edge ID and destination.
  It can work on the stage, update the notebook, and end its final message with
  `KUAYLE_STAGE_ROUTE <edge ID>` to choose an **Assistant choice** transition.
  Existing check-failure, publication-failure and denial branches back to an
  agent can also be selected for rework. The step records which edge the
  assistant chose. Only name edges marked `[selectable]`. Use
  `KUAYLE_STAGE_CONTINUE` or a plain handoff to let Kuayle evaluate the
  built-in conditions. If the assistant names a `[built-in]` or unknown edge,
  or ends with an invalid `KUAYLE_STAGE_*` directive, Kuayle treats it as
  `KUAYLE_STAGE_CONTINUE`. The task timeline records the directive and the
  selectable edge IDs. At a terminal stage this finishes the configured outcome.
  When HEAD and all non-ignored files (including untracked files) match
  the start of the stage visit, the built-in action reuses the input attempt's
  validation and published pull request. This comparison spans retries. A
  build or test can create ignored output without invalidating that evidence.
  CI, approval, merge and end require this match to continue. An assistant
  that edits code there can explicitly select a configured rework branch.
  If it tries to continue instead, the stage fails and the task shows the
  reason and recorded changed filenames when available. A failed assistant
  cannot select a fallback or authorize the built-in action. Checks,
  publications, human approvals and merges retain their own evidence and
  authorization.
- **Workflows** (`workflows`, `workflow_versions`): a save inserts the next
  immutable version (an update trigger refuses changes) with the graph as
  JSONB; `expected_version` refuses a stale save. Archiving hides a workflow
  from choices; running tasks keep their pinned version.
- **Graph**: nodes `start`, `agent` (profile and iteration prompt template),
  `check` (`ci`: the pull request's CI),
  `approval` (an approval leading to `done` or a merge step, or after CI passed
  through agent steps to `done`, is the ship approval, which merges the pull
  request when approved: see
  [merging from a workflow](#merging-from-a-workflow)), `publish`, `merge` (after
  an approved ship approval it merges; otherwise it waits until the pull request
  merges, through the [ship](conversations.md) chain or GitHub; the merge
  completes the task, so its `merged` or `else` transition must lead to a `done`
  end) and `end`
  (`done` or `failed`). A `ci` check fails at once on a failed check suite
  the webhook reports, with the failed checks' summary (after waiting up to
  three minutes while Kuayle still reads them; see
  [CI and review feedback](#ci-and-review-feedback)), and passes only when GitHub lists at least one check
  run or status for the published commit and all of them completed
  successfully with the whole list read (webhooks only report completed
  suites, so a fast suite passing says nothing about a slow one); it polls
  every minute while checks run and every five minutes while none started. It
  reads only results for the commit its attempt published, and a commit an
  earlier CI step failed on is not failed again on the same results: see
  [workflow CI on an unchanged commit](#workflow-ci-on-an-unchanged-commit).
  Edges carry a condition the source node can produce (`agent_finished`,
  `agent_failed`, `checks_passed`, `checks_failed`, `review_approved`,
  `review_changes_requested`, `approval_approved`, `approval_denied`,
  `published`, `publish_failed`, `no_changes`, `merged`, `timeout`, `else`, `assistant`);
  an agent step whose profile reviews runs on the published pull request as a
  [review task](#requested-agent-reviews) and leaves on its verdict.
  A publish step whose branch has [nothing to publish](#publication) ends
  with `no_changes` ("no changes against the base"), not `publish_failed`.
  A transition naming `no_changes` follows it. An `else` transition catches it
  only when it leads to an approval; an `else` leading to an agent step, a
  check, a publish step, a merge or an end does not. A merge only waits for a
  pull request, and with nothing published none comes; in graphs saved before
  the outcome existed `else` means "publishing failed": try again, or fail.
  Without a matching transition the task completes on the publish step
  (workflow status `done`, no transition recorded) with the reason
  "Workflow · `<step>`: The agent made no changes against `<base>`; …",
  starting no iteration and using no budget, so stored graphs need no
  migration. When the task's open pull request still holds work the agent
  removed, the task blocks instead (see [publication](#publication)).
  `ValidateWorkflowGraph` requires one start and at least one end. An
  unassisted start leads to an agent that plans or changes code. A finished
  planning step leads to another such agent or to an approval (`plan_target`).
  Every node must be reachable, every nonterminal node must have an exit,
  every cycle needs a visit limit, and all assigned profiles must be active
  in the workspace. The repository validates again inside the save transaction, so
  an invalid graph is never stored. Validating, creating or saving a
  workflow, and choosing one as a default, also reports each agent step whose
  profile cannot start a task as an `unusable_profile` issue on that step,
  naming the step and the profile (`422 INVALID_WORKFLOW`). A task start
  refused for that reason says so: `WorkflowStepError` names the workflow,
  the step, the profile and what is wrong, as the `400`/`403` message of the
  task composer and as the error on the assistant's approval card.
- **Budget**: `max_iterations`, `max_minutes` and `on_exhausted` (`fail` or
  `ask`). An **iteration** is one round of work in the worktree
  (`WorkflowGraph.StartsIteration`, KUA-215): it starts when the work enters an
  agent step that plans or changes code from anything else (the start, a
  check, a publication, a review, an approval) or after an agent step failed.
  An agent step entered because the agent step before it finished continues
  the round: an orchestrator's plan and the implementer it hands off to are
  one iteration, as is a chain of implementers. So `max_iterations: 8` on
  `start → orchestrator → implementer → publish → review → …` is eight rounds
  of review (it was four), the templates count as before, and a retry after a
  failed agent still counts. The iteration budget is checked where a round starts, never
  between the steps of a round. A visit limit or the budget either fails the task or pauses the
  workflow for a person to continue or stop. Limits are checked at every
  transition, and the time budget also wakes an automated waiting step (a
  reviewer or CI) when it runs out: the task then stays on that open step
  (`Hold`) and fails or pauses. The time budget counts **working time only**:
  its clock starts when the task first leaves `queued` for real work
  (`preparing`), so a task blocked behind a blocker never runs it, and it is
  stopped during an approval or merge wait, `waiting_input`, or
  `waiting_for_disk`. The hold is recorded in `workflow_held_since`. When the
  wait ends, its duration is added to the budget start (`workflow_budget_from`); while it is held there
  is no deadline and nothing wakes the task for the budget. An iteration that
  is running is not interrupted; its run's own time limit bounds it and the
  budget applies when it ends. Continuing passes a visit limit once; past a
  budget it grants another iteration budget of the same size and restarts the
  time budget at the decision (`workflow_budget_extensions`,
  `workflow_budget_from`), so later transitions do not pause again at once;
  a held wait waits again. Continuing past the time budget at an approval or
  merge step keeps the clock held from the decision until the person acts, so
  the next budget counts only the working time after it. The step's saved
  detail (failing checks, review comments) is passed on as it would have been.
  Cost budgets are a follow-up: runs record the cost their harness reports
  ([token usage and cost](#token-usage-and-cost)), but no budget reads it.
- **Defaults** (`workflow_defaults`): per workspace, team and project; the
  most specific wins. A task may choose any active workflow (`workflow_id` on
  task creation, `"standard"` for Standard); the assistant's `delegate_issue`
  accepts an optional `workflow` (name, ID or `standard`) resolved when the
  approval is requested. [Automations](#automations) can inherit that default, follow a selected workflow, or pin its published version at rule creation. All start through
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
failed, CI, publication) to `WorkflowRunner.Outcome`, which
plans the next moves and applies them, with the step rows, in the same
transaction as the task's state change. Agent steps reuse the iteration
machinery (each runs as a new attempt whose reason names the node and
transition; which of them start a budget iteration is described above),
check nodes wait for GitHub CI, publish nodes open or update the pull request, reviewer steps reuse
review tasks, and waiting nodes (review, CI, approval, merge, timeout) park the
task in `awaiting_review` with `workflow_wake_at`. A waiting node is
evaluated as soon as it is entered (CI may have finished during the review),
then wakes at its timeout, the time budget's end or a CI poll; a finished
review or a CI result wakes it sooner, and the claim query picks the task up.
A move of the base branch wakes it too (KUA-257): a push to a branch of an
active linked repository, or a pull request merged into it, wakes every
waiting workflow task of that workspace whose published pull request in that
repository targets the branch (`WakeBaseBranchWaits`; the base is the task
checkout's, as the review target reads it). A wake already due earlier is kept.
A reviewer step left before its verdict (a timeout, a budget, a person's stop,
or the task ending outside the runner) asks its review task to stop, as a
cancellation request does; a review whose row is locked by its own transition
at that moment is skipped (to keep the review-then-parent lock order) and its
result is ignored. Everything is in
the database, so a restarted API resumes where the task was. Prompt variables
(`{{previous_summary}}`, `{{review_comments}}`, `{{failing_checks}}`,
`{{diff}}`) are passed as fenced, untrusted text, each cut at 12000 bytes;
`{{plan}}` is the exception described under [Behaviours](#behaviours). CI
results and comments on a workflow task are recorded but never queue Standard
follow-ups.

### Behaviours

What an agent step does comes from its profile's behaviour
(`AgentProfile.Plans`, `Reviews`); the workspace policy is checked for every
step the same way whatever it is.

- **`changes_code`**: an iteration in the task's worktree with the
  implementer's contract in its prompt (manual QA, running and reporting relevant tests). It leaves on `agent_finished` or
  `agent_failed`.
- **`reviews`**: a review task on the published pull request; without one the
  step fails at once. It leaves on `review_approved`,
  `review_changes_requested` or `agent_failed`, and can time out.
- **`plans`**: an iteration in the same worktree, framed as "plan this for the
  next agent" (`planningLine` ahead of the task) and without the implementation instructions (`withContract`). It may start a workflow. When its
  run succeeds, `WorkflowRunner.AgentFinished` accepts it only if
  - the worktree is as the step found it. The machine manager fingerprints
    the worktree (`HEAD`, a tree written from a throwaway index holding
    everything git does not ignore, and a hash of the list of ignored paths,
    with a wholly ignored directory as one entry; Kuayle's `.kuayle/` is
    skipped by path) before the agent starts and after it ends, and records `worktree_changed` and
    `worktree_changed_files` on the run (`worktree_before` keeps the first
    fingerprint across a manager restart). An edit, a new file, a staged
    change and a commit all count, and so does a new file the agent hides
    from git (a pattern added to `info/exclude`, a directory that ignores
    itself) or any other newly ignored path, such as a first `node_modules/`;
    work inside a directory that was already ignored does not. This is Kuayle's own comparison, the same
    for Claude Code, Codex and OpenCode; the agent is never asked. A run
    whose worktree could not be compared fails the step too;
  - the harness reported a final message (`RunResult.FinalMessage`: Claude
    Code's `result` event, Codex's last `agent_message` item, OpenCode's last
    `text` part, the last two only when no tool call follows them; text
    before a tool call and raw output are not a plan) that is not blank and was not cut
    at the 16 KB summary limit (`summary_truncated`). A plan cut at that
    limit is not failed at once: the step is asked once, in its
    [closing continuation](#closing-continuation), to rewrite it under
    `domain.PlanMaxCharacters` (12000) characters, and a rewritten plan that
    fits is the one accepted (with `domain.PlanShortenedNote` in the run's
    risk notes).

  Otherwise the step leaves on `agent_failed` with the reason in its detail,
  and a workflow that ends there carries it in the task's reason. Kuayle does
  not revert what a failed planning step changed, so a planning step that
  failed for any reason (also a crash) may only lead to a planning step
  (itself again, or another), an approval or an end; a transition to anything
  else, an `else` one included, fails the workflow with that reason. It also
  stores an empty plan, so an earlier visit's plan is not handed over. An accepted plan is stored
  whole in `agent_task_workflow_steps.plan` (returned with the task's
  workflow steps) and is not subject to the 16000-byte step detail or the
  4000-character handoff summary.

**`{{plan}}`** is the newest stored plan of the task. Unlike the other
variables it is trusted: the workflow's owners placed the planning step to
instruct the agents after it, so it is written into the next iterations'
prompts unfenced and whole, under "Plan to follow", where the step's template
names it, or appended when it does not, on every later visit too (also after
an approval between the plan and the build). The untrusted variables are
shortened and then left out, and then the original task text is shortened,
before the plan would be; the attempt's handoff does not repeat the plan as
unverified text and gives up its summary first. A review step gets the plan
only when its prompt names `{{plan}}`, within the review focus limit of 4000
bytes. `{{previous_summary}}` is unchanged: fenced, untrusted and cut. The
template is filled in one pass, so text that came from a variable (a plan
that mentions `{{diff}}`) is never substituted again, and the handoff and
follow-up markers inside a plan are rewritten so it is not cut there.

A step run by another profile than the previous attempt's step starts a fresh
native session (`AgentHandoff.OtherProfile`): a builder does not continue its
planner's conversation.

A planning step also **cannot write its worktree**: its agent container
mounts the task's worktree read-only over the writable workspace volume, and
the worktree's `.kuayle/` directory (the outbox) writable on top of it
(`readOnlyWorktreeMounts`, two volume-subpath mounts). The comparison above
stays as the second guard, because git keeps a worktree's index and refs in
the repository's common directory, outside that mount, so a commit or a
staged change is still possible. What stays writable for the harness: its
session directory (`/workspace/.kuayle/agent-sessions/<task>`), the run
directory for temporary files and caches (`/workspace/.kuayle/tmp/agent-<run>`),
the home and `/tmp` tmpfs, and `.kuayle/` in the worktree. Per harness
(`domain.ReadOnlyWorktreeSupported`):

| Harness | Worktree of a planning step | Why |
| --- | --- | --- |
| Claude Code, Codex, OpenCode | read-only | Sessions, caches and temporary files are outside the worktree. Not exercised against the real CLIs in tests; the mount spec and the request are. |
| Crush | writable, checked after the run | Keeps its state in `.crush/` inside the working directory. |
| Pi, Qwen Code, Kimi, Goose, custom | writable, checked after the run | Not verified to start with a read-only working directory. |

The mount needs Docker API 1.45 (Engine 26); on an older daemon the step runs
with a writable worktree and a warning is logged. The same happens when the
worktree's `.kuayle/outbox` could not be prepared (the nested mount would
keep the container from starting): the run's `worktree_read_only` is cleared
and the comparison after the run is the guard.

`POST /workflows/validate` also returns `warnings` beside `issues`
(`domain.WorkflowWarnings`). A warning never refuses a graph. Today there is
one, `planning_step_implements`: an agent step runs a planning profile while
its name is that of an implementing role (it contains "implement", "build",
"coder", "developer" or "engineer") or a sentence or clause of its prompt
starts with an order to change code (implement, write the code, make the
changes, fix, commit, push, open a pull request). "Plan how to implement" and
"do not implement" start with something else and pass. The editor shows it
on the step it concerns: a warning icon on the step's card and one line at
the top of the step's inspector ("This profile only plans, but the step tells
it to implement", once even when the name and the prompt both earn it). It is
not listed among the problems to fix and does not disable Save
(`UI/tests/e2e/workflow-warning.test.ts`).

A profile used as an [agent assignee](#agents-as-issue-assignees) runs the built-in flow
whatever its behaviour; planning applies to workflow steps.

Routes (workspace, session only): `GET/POST /agent-profiles`,
`PUT/DELETE /agent-profiles/:profileId`, `GET/POST /workflows`,
`POST /workflows/validate`, `GET/PUT /workflows/:workflowId`,
`POST /workflows/:workflowId/duplicate|archive`, `GET /workflows/:workflowId/audit`,
`PUT /workflow-defaults`; under dev machines `GET /agent-tasks/:taskId/workflow`
(graph, path, visit counts and `runs`) and `POST /agent-tasks/:taskId/workflow/decision`
(`approve`/`deny` an approval node, `continue`/`stop` at a limit; only the
task's creator or an owner or admin who can see the issue, for the step
`sequence` they saw). The UI edits workflows in Settings → Workflows and the
assistant's Workflow side panel (Svelte Flow), profiles in Settings → Agents,
and shows a task's run in its Agents panel.

`runs` is what the agent view's timeline is built from: the task's newest 200
agent runs, oldest first, as `id`, `task_id`, `provider`, `model`, `status`,
`created_at`, `started_at`, `completed_at` and `step`, the sequence of the
workflow step that started the run. Steps do not store their run: a run
belongs to the latest step that started an iteration (`generation` set) and
was entered before the run was created, so a step resumed after a usage limit
has several. The runs of the review task a reviewer step created
(`review_task_id`) are listed on that step with the review's `task_id`; a
review requested outside the workflow is not. `summary` is returned only when
the harness reported a final message (`result.final_message`), never a run's
raw output, prompt or secrets. A finished run's summary never changes, so a
view that reloads passes `?summaries_since=<RFC 3339 time>`, the newest
`completed_at` it has, and gets no summary for runs completed at or before it
(`400` for an unreadable time). A task without a workflow gets its runs with no
`step`. The read has the task's own authorization (its creator, still a member
who can see the issue's team); a review task has the same creator, issue and
conversation as the task it reviews.

Not implemented: per-profile skills and tool sets, sharing across workspaces,
parallel branches and workflows for non-coding automations.

### Pause and resume

The creator of a task can pause its running workflow and resume it later from
the same step (KUA-129, migration `000145_workflow_pause`). It is the creator's
control, like cancel: nobody else, in any role, pauses or resumes a task.

`POST /agent-tasks/:taskId/pause` takes `{"mode": "now" | "after_step"}` and
answers 202 with the task. What it does depends on what the task is doing:

| The task is | `now` | `after_step` |
| --- | --- | --- |
| waiting (CI, an approval, a merge, a reviewer) or not started yet | pauses at once, where it is | the same |
| running its agent | stops the agent and pauses | pauses when the step has an outcome |
| running a check or a publication | pauses when it has a result | the same |

A paused task has `workflow_paused_at`; a pause that has not taken effect yet
is `workflow_pause_request`. While paused the worker does not claim the task
and every workflow move is refused, so a waiting step does not advance when CI
finishes or its review ends, nobody decides an approval, and no step starts.
A reviewer's own review task keeps running; its verdict is read after the
resume. A pull request merged on GitHub still completes the task. A task that
blocks (for example because removed work is still in its pull request) drops a
pause request that had not taken effect, since it already waits for a person.

"Pause now" on a working agent reuses the usage-limit path: the attempt goes
through the usual runtime cleanup (`cancelling`), which keeps the machine, the
checkout, the worktree and the session transcripts, and ends in `blocked` with
the reason "Paused". The task keeps its machine and checkout; a machine that
was stopped meanwhile is started again when the task resumes. A cleanup that
fails for good (the machine manager is down) is started again by the worker
until it succeeds, and the task then waits paused; cancelling it meanwhile
cancels it. A paused task does not count as load on its machine, however it
was paused, so other tasks can share or reuse the machine; only the cleanup
of a stopped attempt still does.

`POST /agent-tasks/:taskId/resume` resumes (202 with the task). An interrupted
step continues as a new attempt on the same step, visit and iteration, with
`resume_native_session` set, so a harness that recorded a session continues
it (`claude --resume`) and any other gets the handoff, both told that a person
paused the attempt and to continue where it stopped.
["Interrupt and send"](#live-channel) stops and continues an attempt in the
same way but is not a pause: it holds nothing and keeps a pending pause. A task paused elsewhere is
simply picked up again: a waiting step is evaluated at once, a finished step's
outcome is applied. Resuming a pause that has not taken effect withdraws it.
The same route still resumes a task paused by a [usage limit](#usage-limits);
if a pause was waiting for the step the limit interrupted, that resume ends
the request too and the task runs again (an automatic resume keeps it).

"Pause now" followed by a resume starts a fresh attempt without counting a
visit or an iteration, so repeating it is bounded only by the workflow's
working-time budget.

The time spent paused is added to the start of the workflow's time budget
(`workflow_budget_from`) and to the open step's `paused_seconds`, which its
timeout leaves out. Visit counts and iterations do not change. A pause inside
a person's wait is already excluded by that hold (`workflow_held_since`), so
resuming does not move the budget a second time; the step's timeout still
leaves the paused time out. Cancelling a paused task cancels it; retrying it
ends the pause and starts a new visit.

Every request, pause, resume and withdrawal is a `workflow_audit_events` row
(entity `task_workflow`; actions `pause_requested`, `paused`, `resumed`,
`pause_withdrawn`; the step's `sequence` and the `mode`). The task carries
`workflow_paused_at` and `workflow_pause_request`, and
`GET /agent-tasks/:taskId/workflow` returns `paused_at`, `pause_request`, the
`pauses` list (the newest 500: action, sequence, mode, actor, time, oldest
first) and each
step's `paused_seconds`. The same list carries a task's moves to another
version of its workflow (audit action `version_switched`, with `from_version`
and `to_version`), under the same authorization, so the sidebar's path shows a
move where it happened. The task card shows "Paused" with one Resume button,
and offers Pause in its menu; `WorkflowPauseControl.svelte` is the same
control as a standalone component.

### Team workflows (experimental)

A team workflow (KUA-252) is a workflow version whose JSON has
`"kind": "pipeline"`: ordered phases, each naming what must be true to move
on, worked by a roster of agent roles that talk in the task's room. Kuayle
reads the facts, runs the platform actions (publish, update the branch, rerun
CI, merge) and guards the merge. Absent or `"graph"` is the graph engine
above. `ParseWorkflowGraph` refuses a pipeline, so the graph runner never runs
one, and a task never changes engine: a move between kinds is refused as
`workflow_kind`, and **Continue on another workflow…** does not offer
pipelines. Standard stays a graph.

**The switch.** `WORKFLOW_PIPELINES_ENABLED` (`config.WorkflowPipelines`,
default `false`) is read by the API process; both Compose files pass it
through (`WORKFLOW_PIPELINES_ENABLED` in `.env`, see `.env.example`).
Off, creating, saving or duplicating a pipeline, making one a default and
starting a task on one answer `403 PIPELINES_DISABLED` (a delegation from the
assistant says "Pipeline workflows are turned off on this server"). Saved
pipelines stay readable, and a task already on one keeps running: the tick
does not read the switch. `GET /workflows` returns the switch as
`pipelines_enabled`, and the task composer offers pipelines only while it is
on. On, owners and admins save pipelines like graphs, including the built-in
`template: "team"` of `POST /workflows`, and a task starts on a pipeline
chosen in the task composer or set as a scope's default.
`POST /workflows/validate` checks a pipeline with `ValidatePipeline` either
way.

**The definition** (`domain/pipeline.go`). `ParsePipeline` refuses unknown
fields, and `ValidatePipeline`, separate from the graph's validator, checks it
on save and again when a task starts:

- `phases` (1 to 50, in order): `id`, a one-line `label` (80 characters), an
  `owner` (a role of the roster, `kuayle` or `human`), an optional `prompt`
  (4,000 bytes, given to the writer's turns in that phase), `checks` (at most
  20 per phase; check IDs are unique across the pipeline) and `guarded`,
  allowed only on the last phase when Kuayle owns it: the merge. A guarded
  merge needs a required `approved` check in a phase before it
  (`missing_approval`): the owner merges.
- A check requires one fact and is `required` (the task stays in its phase
  until it holds or the creator waives it for the head) or advisory. The
  facts are a closed list, each with a defined "met":

  | Fact | Met when |
  | --- | --- |
  | `pr_exists` | Kuayle published a pull request for the task |
  | `pr_draft` | the pull request is not a draft |
  | `head_contains_base` | the head contains the base's tip (the branch is not behind) |
  | `conflict` | the pull request does not conflict with its base |
  | `migration_collision` | no migration number of the branch collides with the base's |
  | `ci_green` | CI passed on the head |
  | `approved` | a person approved merging the head |
  | `merged` | the pull request is merged |
  | `closed` | the pull request is not closed without merging |
  | `overlap` | no task ahead in the repository's merge order ([the board](#merging-only-up-to-date-heads)) changes the same files |
  | `verdict` (with `role`) | that role, which is not the writer, approved the head |
  | `script` (with `script`) | the command (at most 4,000 bytes), run in the task's worktree, exits 0 |

  Values such as the head, the base and the board are posted to the room,
  never required. Only a `verdict` names a role, and only a `script` carries
  a command, the one field with author content.
- `roster` (1 to 10 roles): `role` (1 to 32 lowercase letters, digits, `-` or
  `_`; not `kuayle`, `human`, `lead` or `all`, which the room uses),
  `profile_id` (a profile of the workspace that is not archived), `writer`
  (exactly one role, whose profile changes code and which is not `on_call`)
  and `on_call`.
- `lead` (a `profile_id`, optional), `autonomy` (`approve`, and `waive` per
  check ID: `human`, or `lead` when there is a lead) and `budget`
  (`max_turns` up to 200, `max_minutes` up to 30 days, zero for no limit;
  `on_exhausted` `ask`, or `fail`, the default).

When a task starts, every role's profile goes through the workspace policy as
a graph step's does, the first turn is the writer's with the writer's profile
and the first phase's prompt, and the task is marked `pipeline` (also in its
JSON, so every view knows). A pipeline's roster and lead keep their profiles
in use, as graph steps do. The `team` template: phases implement
(implementer), pull request (Kuayle; `pr_exists`), review (implementer; the
reviewer's `verdict`), CI checks (implementer; `head_contains_base`,
`ci_green`, `migration_collision`), approval (human; `approved`) and merge
(Kuayle, guarded), every check required; the built-in Implementer as the
writer and Reviewer; no lead; 40 turns and 2,880 minutes, then `ask`.
Settings shows a pipeline read only, with a notice (`PipelineView.svelte`);
there is no editor.

**The room** (KUA-261; `domain/task_room.go`, `repository/task_room.go`,
table `task_messages`, migration 206) is one ordered log per pipeline task,
kept on the root task. Each post has a `seq` allocated under the task's
`workflow-runner` advisory lock; an author (`human`, the creator; `member`,
with its role and member; or `kuayle`); recipients (`to_roles`, empty for
everyone: roles, `human`, or `lead` when there is one); a `kind`; text; a
structured `data` part (phase, verdict and commit, check, request action,
fact name and value); an optional `reply_to`, an earlier post; and a
`dedupe_key`, whose repetition returns the first post. `TaskRoomRefusal` says
who may post what:

| Kind | Creator | Writer | Other roles | Lead | Kuayle |
| --- | --- | --- | --- | --- | --- |
| `say`, `ask` | yes | yes | yes | yes | no |
| `verdict` (`approve`, `request_changes` or `comment`, for a commit) | no | no | for its own role | no | no |
| `phase` | no | no | no | yes | only without a lead |
| `request` (`review`, `merge`, `publish` or `split`) | yes | `review` only | no | yes | no |
| `waiver` (of a check) | yes | no | no | checks granted to it | no |
| `grant` (of a check) | yes | no | no | no | no |
| `fact` | no | no | no | no | yes |
| `summary` | no | no | no | yes | yes |

Text is at most 16 KiB, 4,000 characters from agents, and `say` and `ask`
need some; the structured part is at most 16 KiB. Each sender has an hourly
budget, refusals included: 20 posts per role (the agent messages' limit) and
30 for the creator (the live channel's, `429 ROOM_MESSAGE_LIMIT`); Kuayle has
none, and a role's post over its budget is dropped. A person's refused post
is not stored (`400 ROOM_POST_REFUSED` with the reason). A role's refused post
is stored with its `reject_reason` (200 characters), without recipients or
data, is never delivered and is shown to its sender at its next turn. Every
stored post writes a `task.room_message` conversation event in its
transaction, and a person's post wakes the task's tick.

**Agents post through the outbox** (KUA-267). An outbox file whose `to` is
`@role`, `@human` or `@all` is a room post (`domain.ParseAgentOutboxMessage`:
`kind`, `say` by default; `body`; `reply_to`; `data`). `RecordAgentOutbox`
hands those files to `RecordRoomOutbox` before the agent messages: they are
posted for the role the sending task plays (a member's task as its role, the
root task as the writer), with the run and the agent messages' dedupe key. A
file that does not parse or is too large is stored as a refused post with the
reason. On a task without a pipeline the file goes on to the agent messages,
which refuse it ("the task room is only for tasks on a pipeline workflow").
The images ship `kuayle-say` (`devmachine/common/kuayle-say.cjs`, installed as
`/usr/local/bin/kuayle-say`), which validates and writes the file
atomically; agents learn it from "The task room" in the `kuayle-agents`
skill.

**Members and turns** (KUA-271; `domain/task_member.go`,
`repository/task_member.go`, table `task_members`, migration 207). A member is
a role working on one pipeline task: one per role per task, so each role
keeps one native session for the task, continued across its turns. Its
profile is pinned when it is created. It keeps its room cursor
(`last_read_seq`), its `state` (`idle`, `queued` or `running`, kept in step
with its task by every transition), `turns`, `pinned_commit` (the published
commit its latest turn read) and `verdict_asked`. A member is an agent task,
so its runs, session, handoff, logs and transcripts are the existing ones:

- **The writer's turns are the root task's iterations** (`member_task_id`
  NULL; the member exists from the start). A turn's prompt is the task's
  request, then "the Kuayle pipeline, phase <label>" with the phase's
  `prompt` and the posts the writer has not read (a page of 200, at most
  24,000 bytes of them; the rest waits for the next turn) within the 32 KiB
  prompt, with the writer profile's and the scope's instructions; the turn
  also gets the writer profile's instructions file and skills, as a graph
  step does. Each turn is the root's next attempt, so it continues the
  writer's session as any follow-up does
  ([native session resume](#native-session-resume-and-handoff)). A turn that
  cannot start yet, because the writer's last attempt is still being wound
  down, waits for the next poll.
- **Every other role gets one review task of the root**
  (`insertReviewAgentTask`: kind `review`, never published, no push
  credential, with the root's creator, issue and conversation), created the
  first time `StartMemberTurn` starts the role. Each later turn is the next
  generation of that same task. `Attempt` reads the previous attempt for a
  member's task (every other review still starts cold), so the turn resumes
  the role's session (`claude --resume`, `codex exec resume`,
  `opencode run --session`) when its previous turn succeeded on the same
  machine, and otherwise starts with the handoff. A later turn fetches its
  worktree again first (only a member's task re-queues its checkout, once
  per turn), so it reads the newest head. Between turns the member's task
  rests in `blocked` (no notification, session kept; `completed` would purge
  it). A failed or cancelled turn is posted by the tick as a `turn` fact
  with its reason, and the role takes its next turn as after any other (a
  cancelled review task may be queued again only when it is a member's). A
  role whose last turn is still being wound down is tried again at the next
  poll. A role that cannot take its first turn, because its profile was
  archived, is a `turn` fact, and the task pauses for a person. When the
  root task ends (merged, failed, stopped or cancelled) its members' turns
  still under way are cancelled, as a graph's reviews are.
- **What a turn reads.** `domain.RoomInbox` is what a role reads from posts
  newer than its cursor: posts for everyone or for the role, not its own,
  plus its own refused posts with their reasons (a member reads a page of 200
  within the 32 KiB prompt; the rest waits). `domain.RoomTurnPrompt` lists
  them with number, author and kind, the text fenced as data, and says how
  to answer. A member's prompt (`domain.MemberTurnPrompt`) starts with its
  standing contract: it is the `@role` of the team, its session continues,
  another role writes the code and it changes nothing. A turn asked for a
  verdict is the review of the newest commit Kuayle published from the
  root's attempts (`AgentReviewPrompt`, with the CI Kuayle knows on it);
  another turn gets the profile's instructions and answers what it was asked.
  When the role's previous turn read another commit, the prompt names
  `git diff <old> <new>` so it checks what changed.
- **When a turn starts** (`domain.RoomWakes`): when the role's unread posts
  hold one addressed to it, or a `say`, `ask` or `request` that a person or
  another role wrote to everyone. A fact, phase move, verdict or summary to
  everyone waits for the role's next turn. The tick also starts a role's turn
  to ask it for a verdict, and a stall summary addressed to it starts one
  (below). A member whose turn is queued or running, or whose task waits on
  a usage limit, is not started again. Any member turn's end, failed or
  cancelled too, wakes the root's tick.

**Verdicts** (table `task_verdicts`; `agent_reviews` stays for requested
reviews). A member's turn owes a verdict for its pinned commit when the tick
asked it (`StartMemberTurn(..., askVerdict)`; Kuayle may not post
`request`), when its unread posts hold a `request` for a review, or when the
current phase requires its verdict on the head and has none as the turn
starts. A `verdict` post counts
only from the member task of its role (`author_member_id`), for the commit its
turn reads or another one Kuayle published from the root's attempts
(`dev_machine_git_refs`, source `published`: Kuayle keeps no clone to ask
git). An abbreviated commit is stored whole; an unknown or ambiguous one is
refused with the reason. A turn that owes a verdict and posted none is asked
once more in its own session (`domain.VerdictClosingAsk`, through the
[closing continuation](#closing-continuation): Claude Code, Codex and
OpenCode), unless its final message has a `Verdict:` line. When the turn
ends, that line (`ParseAgentReview`) is posted as the member's verdict.
Without one, Kuayle posts a `verdict` fact with `"verdict": "none"` ("The
@reviewer ended its turn without a verdict for commit …"): the check stays
unmet, and the tick does not ask the role again on that head, so the next
stall sends the check to the phase's owner. A verdict, or the owner's
approval, given on an older commit holds for the head only when the head is
that commit plus merges of the base that Kuayle verified (`verifiedUpdates`).

**Facts.** Each pass reads the newest pull request Kuayle published from the
root's attempts (`PublishedWork`) with `readTaskFacts`, the read the merge
gate uses, plus each role's latest verdict, the owner's approval, the script
checks' runs and the board's overlap. What the pass asks of GitHub (these
reads, a branch update, a CI rerun, the merge) has its own deadline inside
the worker's step (`tickGitHubTimeout`, 12 seconds, ending at least 5
seconds before the step does so the pass can still save): a read that runs
out is a fact that could not be read, not an error, and an action that runs
out is tried at the next pass. Kuayle posts a `fact` for each value that
changed since it last posted it, in this order: `pr_exists`, `head_sha`,
`base_ref`, `pr_draft`, `head_contains_base`, `conflict`,
`migration_collision`, `ci_green` (state, head and failing checks, with the
checks' details when it fails), `approved`, `overlap` (the tasks ahead that
change the same files), `merged`, `closed`. A fact that blocks a check
carries its reason and next step (`unmetChecks`). A value Kuayle could not
read is not posted, and a check on it is `unknown`, never unmet: it is read
again. Kuayle also posts facts of its own: `turn` (a role's turn failed or
cannot start, and why), `publication` (publishing the writer's turn failed
or was skipped), `merge` (the merge gate refused, and why), `script`
(below) and the `verdict` with `none` above.

**Script checks** run the check's `script` as `sh -c` in the root task's
worktree, through the [manual check](#manual-checks) runner
(`RunPipelineCheck`), as the task's creator, who needs no permission to run a
command of their own: the command comes from the pinned version, which only
the people who manage workflows write. The checks of the phases up to the
current one run once a pull request is published, once per published commit
and check, and again on a new head; a run on a commit counts for a head that
only merged the base into it (`verifiedUpdates`), as verdicts do. While a
run is under way the check waits, and its end wakes the tick. Exit 0 meets
the check; another exit leaves it unmet with the end of its output (at most
2,000 bytes, fenced as data) as the reason. A check that cannot run (no
ready worktree on a running machine, the machine busy with an attempt) is
`unknown`, never unmet, and is tried again at the next pass. Each result is
posted once as a `script` fact (check, commit, exit code); a failed advisory
check is posted to its phase's owner and never holds the task up.

**The tick** (KUA-273; `service/team_tick.go`, `repository/pipeline_tick.go`,
migration 208). `AgentTaskService.step` runs a task marked `pipeline` through
the tick instead of the graph runner. The root task is the writer: when its
turn ends, a turn that changed the work (or any, while nothing is published
yet) is published once, a failed one is posted as a `turn` fact, and the
task waits in `awaiting_review`, where a pass runs on every wake. The
tick keeps its position in the workflow columns (`workflow_node` is the
current phase; `workflow_status`, `workflow_wake_at`, `workflow_sequence`;
`workflow_iterations` counts the turns charged; the budget clock), so the
claim query, pause, resume and the existing views keep working, and its
memory between passes in `agent_tasks.pipeline_state`: the facts last posted,
the room read so far, the creator's waivers, which head each role was asked
about, the approval, the last published turn, branch updates, the CI rerun,
the merge, the script checks' runs, since when the phase waits on checks
that change by themselves, and the stall counter. Every save is fenced by
the worker's lease, the task's `workflow-runner` advisory lock and its
sequence, which moves on only when the phase or the status changes or a
person decides. One pass:

1. reads the facts, the verdicts, approval and script runs that hold for the
   head, the overlap and the room since the last pass (a creator's `waiver`
   holds for the head the room last named). A pull request that cannot be
   read, or GitHub that does not answer in time, is waited for: nothing is
   posted or started, and the pass runs again in a minute;
2. posts the facts that changed and each script check's result once;
3. takes the first phase with a required check that does not hold and is not
   waived for the head as the current one (the last phase when every check
   holds), and posts `phase` when it changes;
4. completes the task when the pull request is merged, and publishes a
   finished turn of the writer that was not published yet;
5. starts the script checks of the phases up to the current one that have no
   run for the head (or could not run);
6. acts through the existing code, each action at most once per head or
   turn: posts each member's turn that failed; brings a branch that is behind
   its base and merges cleanly up to date (`updateBranch`, at most
   `maxMergeUpdates`, 3, per published commit); runs the failed Actions jobs
   once more when CI still fails on a head the writer was already sent and
   did not change; asks each role whose verdict the current phase requires
   and that has none for the head, once per head; and waits for the owner's
   approval at a phase with an unmet `approved` check;
7. at the guarded last phase, once every required check holds, merges
   through the merge gate as the person who approved the head (the creator
   when the approval was waived), who must still be allowed to decide. A
   merge that waits on the gate longer than `mergeWaitTimeout` (2 hours) is
   refused. A refused merge is a `merge` fact; when the facts read next send
   the work back to an earlier phase, the merge starts afresh once they hold
   again, and otherwise the task pauses for a person (Continue, or a new
   approval, tries again). A pipeline without a guarded phase completes once
   every check holds;
8. starts a turn for every idle role other than the writer whose unread posts
   call for one, owing a verdict when the phase requires its verdict;
9. when nobody works, nothing is under way and nothing is awaited, posts the
   current phase's unmet checks with their reasons as a `summary` to the
   phase's owner (the writer for a phase Kuayle owns). A role's turn starts
   on it in the same pass; for a phase a person owns, the task waits for the
   person, the budget clock held, and is announced once. What changes by
   itself (CI running, a check run under way, a fact not readable) is
   awaited, and read again every minute, for at most `mergeWaitTimeout` on
   the same head and checks; after that it is sent on like any unmet check,
   so checks that never report stall too. The third such summary pass on the
   same head, phase and unmet checks pauses instead (`blocked`, status
   `paused`) and asks a person;
10. starts the writer's turn when its unread posts call for one (a summary
    addressed to it does);
11. charges every turn it starts, writer's or member's, and the working time
    to the pipeline's budget like a graph workflow's iterations, the clock
    held while a person is awaited: a turn the budget has no room for is not
    started, and a used-up budget pauses for a person (`ask`) or fails the
    task (`fail`);
12. otherwise waits for the next wake, or polls in a minute while something
    changes by itself (CI running, a check run, a fact not readable, a role
    or the writer still winding down, a branch update or a merge under way).

The tick has no branch for a particular failure: a new situation is a fact
with a reason, sent to whoever owns the phase.

**Wakes.** The worker runs a pass when `workflow_wake_at` is due. It is set
by a CI result from GitHub (and its failure details, once read), the end of a
member's turn or of a script check's run, a person's post, decision or
resume, and the tick's own poll or the time budget's end (not while a person
is awaited). A pull request merged on GitHub completes the task
directly. A wake that lands while a pass runs is kept: the pass stores the
earlier of that wake and its own (`PipelineApply.ClaimedWake`), so it is
never lost.

**Decisions.** `POST /agent-tasks/:taskId/workflow/decision`, with the
`sequence` the person saw, from the creator or an owner or admin who can see
the issue, as for a graph workflow (offered in the task's workflow panel):

- `approve`, where the current phase has an `approved` check and the task
  waits, records that person's approval of the head the room last named and
  starts a new merge;
- `continue`, where the tick paused, asks every role again, counts stalls
  and waits anew, tries a refused merge again and grants another budget of
  the same size when it is used up;
- `stop`, where it paused, fails the task.

Anything else, `deny` included, answers `409 WORKFLOW_CONFLICT`. The
decision reads the task as it is locked for it, so a pass that ran meanwhile
is neither lost nor overwritten. Each decision is recorded in
`workflow_audit_events`. The creator's
[pause and resume](#pause-and-resume) apply as to a graph workflow, and a
retried pipeline task starts again with its writer's turn, on another budget
of the same size, asking every role again.

**Routes and events** (session-only and behind the demo guard, like every
task route):

- `GET /agent-tasks/:taskId/room?after=<seq>` (at most 200 posts, oldest
  first, refused ones with their reason; `Cache-Control: private, no-store`)
  and `POST /agent-tasks/:taskId/room` (`kind`, `body`, `to`, `data`,
  `reply_to` and a `request_id` that makes a repeated post return the first)
  are the creator's (`lockAgentTask`). A task not pinned to a pipeline answers
  `409 ROOM_UNAVAILABLE`.
- `GET /agent-tasks/:taskId/workflow` (`StepRuns`) lists the runs of the
  members' tasks with the `role` each played (the root's own runs are the
  writer's); a member run's transcript is read through its `task_id`, the
  member's task, which has the root's creator.
- `task.room_message` carries each stored post to the conversation; the
  task's transitions and workflow events are the existing ones.

**The view** (KUA-268). The agent view of a task marked `pipeline` shows its
room instead of a timeline (`TaskRoom.svelte`, rows from `task-room.ts`): the
creator's posts, Kuayle's facts and phase moves, and each role's turn (its
posts with one `run_id`) as one row that opens the run's transcript; a
refused post is muted with its reason. The room is read once, kept by
`task.room_message` events (`ConversationState.taskRooms`, once per `seq`)
and read on from the last unbroken `seq` when the task's run is read again or
a post arrives after a gap. The composer posts a `say` there (text only, with
a `request_id`). The side panel names the pipeline, draws no graph and offers
approve, continue and stop.

**Limits of this first version.**

- No lead runs. A pipeline's `lead` is validated and keeps its profile in
  use, but nothing starts it; Kuayle names the phase (with a `lead` set it
  still derives the phase but posts no `phase` move). `autonomy` is not read:
  approving is a person's decision and waiving the creator's, and a `grant`
  is stored but grants nothing until there is a lead.
- One writer, the root task. The other roles never publish and have no push
  credential.
- `on_call` is validated but not read: every role other than the writer
  works only when the room calls for it or the tick asks it for a verdict.
- Advisory checks other than `script` ones are shown, not acted on.
- No `deny`: a person approves the head or, where the tick paused, continues
  or stops it.
- The owner's composer posts only `say`; `ask`, `request`, `waiver`, `grant`
  and `reply_to` are API only. A `request` for `merge`, `publish` or `split`
  is stored and can start a role's turn, but Kuayle does not act on it.
- A member has no task card of its own: its turns are rows of the root's
  room and runs of the root's workflow view, with their role.
- The board the tick reads is the creator's (the board's visibility), so
  `overlap` sees only the creator's own active tasks on the repository.

### Moving a task to a newer workflow version

A task is pinned to the workflow version it started on; saving the workflow
only affects later tasks. Its creator can move a running task to the newest
version (KUA-130). It is never automatic.

- `GET /agent-tasks/:taskId/workflow/switch` says what the move would do and
  changes nothing: `from_version`, `to_version`, the step the task continues on
  (`node`, `node_label`), or `refused` with the reason. `POST` on the same path
  does it, and answers `409 WORKFLOW_SWITCH_REFUSED` with the reason when the
  task cannot be moved. Both are for the creator, like pause and cancel.
- The task must be at rest (`domain.WorkflowSwitchBlocked`): paused by a person
  with the interrupted attempt stopped, or `awaiting_review` and waiting on a
  step for CI, an approval or a merge. A task whose agent is working is paused
  first (`not_at_rest`); a task being cancelled is not moved.
- The task continues on the step it is on, and only if the newest version still
  has it doing the same kind of work (`domain.MatchWorkflowNode`): the same node
  ID and type, the same check, and for an agent step the same profile, because
  the attempt or review the task has there was started with that profile's
  harness, model and instructions and is continued with them. Label, prompt,
  limits and the transitions may differ, and so may every other step.
  Otherwise the move is refused (`step_removed`, `step_changed`) and the task
  stays on its version. This mode always continues from the current step.
- The step the task is on keeps the prompt it was entered with. The new
  prompts, profiles, transitions and limits apply from its next step.
- A person's retry goes back to the latest implementing step the task's current
  version still has (`retryWorkflow`), and is refused when it has none.
- Transitions planned on the old version are refused: `WorkflowApply.Version`
  names the version they were planned on, so an approval or a worker step that
  raced the move decides again on the new one. A waiting task is looked at
  again at once, so the new version's timeouts and budget apply immediately.
- Visit counts are kept for the steps the newest version still has; iterations,
  budget extensions and the time already spent are kept. So the new limits and
  the new budget count what the task already did, and a task can be over a
  lowered limit at once.
- The worker's lease is dropped, so what it decided on the old version is
  refused and it decides again on the new one. A paused task stays paused.
- The move is recorded in `workflow_audit_events` (`task_workflow`,
  `version_switched`: who, from and to version, the step, the step sequence).
- `GET /agent-tasks/:taskId/workflow` returns `latest_version` next to
  `version`. The task card asks for the plan whenever the task changes state
  and offers "Move to workflow version N" in its menu; the dialog says where
  the task continues, or why it cannot be moved
  ([workflow-switch.ts](../../UI/src/lib/features/assistant/workflow-switch.ts)).
  The move is listed in the workflow view's `pauses`, and the task card's
  path shows it where it happened ("Moved to workflow version N").

### Continuing on a chosen workflow step

The task creator can pick a destination workflow and step from **Continue on
another workflow…** in the task card menu (KUA-174). The rest rule in
`domain.WorkflowSwitchBlocked` allows a switch when a person has paused the
task and its attempt has stopped, or when the task is waiting in the workflow.
Confirming the switch releases the pause and the worker enters the selected step.

- Preview with `GET /agent-tasks/:taskId/workflow/switch?workflow_id=<id>&node=<step-id>`.
  Confirm with `POST` to the same path and a JSON body containing `workflow_id`
  and `node`. The optional `expected_version_id` pins the preview's
  `continuation.to_version_id` for confirmation. Omit both destination fields
  to use the compatible version upgrade described above.
- Choose a different active workflow in the task's workspace. The destination must pass
  the same policy checks as a new task and is pinned to its latest saved
  version. The `standard` alias selects the workspace's
  saved Standard override. The built-in Standard flow has no saved version.
  Select an executable step from the graph. The start and end nodes mark the
  graph's boundaries.
- A preview changes nothing. It returns `from_version`, `to_version`, `node`
  and `node_label`. Its `continuation` names the source workflow and step, the
  destination workflow and version ID, and the destination step type. The
  `carries` list names the task, machine, worktree, checkout, branch and pull
  request, which stay attached when present. Publication continues on the same
  branch and updates the existing open pull request.
- The plan includes the destination `budget`, the selected step's `max_visits`
  and `timeout_minutes`, and `resets` for visits, iterations, budget extensions
  and the time budget. The first entry counts as visit one, and a new coding or
  planning attempt counts as the first iteration. The audit records
  `time_spent_seconds` against the prior budget, excluding time paused or held
  for a person. The new budget starts fresh.
- The worker uses the destination step's profile, harness, model, effort and
  rendered prompt. An implementing, planning or supervised agent starts a new
  attempt on the kept checkout, with a fresh native session. Reviewer profiles
  start a fresh review of the published pull request. CI, approval and merge
  steps enter as a new visit, with their own timeout and no earlier step
  decision or merge authorization. The task's earlier runs and path remain in
  its history.
- Preview refusals include `not_at_rest`, `finished`, `no_workflow`,
  `workflow_unavailable`, `workflow_unusable`, `workflow_archived`, `step_missing`
  and `step_not_runnable`. Confirmation rechecks the plan, can also refuse
  with `target_version_changed`, and returns `409 WORKFLOW_SWITCH_REFUSED` with
  the reason. Both calls are session-only. Tokens receive `403` with `session_required` for the
  reason. A non-creator or an inaccessible task receives `404` with
  `not_creator` for an explicit switch.
- Switching drops the worker lease and advances the step sequence. Old moves
  also fail the `WorkflowApply.Version` check. The `workflow_switched` event
  in `workflow_audit_events` records the actor and both workflows, versions and
  steps. The workflow view includes it in `pauses` and the task card shows it
  in **Full path**.

Migration 199 records the switch sequence on the task. Before rolling it
back, let workers finish entering any selected steps. Rollback removes that
sequence while retaining the audit events and workflow step history.

## Task notebook

Each step used to start almost from zero. A task now has one notebook
(`agent_task_notebooks`, migration 150; `domain.Notebook`), kept by Kuayle
outside the worktree, with a fixed set of bounded sections:

| Section | Written by | Merge |
| --- | --- | --- |
| `instructions` | Kuayle, from people: the task's request, a retry's new instructions, an approved follow-up, a live message | added; the request always stays; 32 KB together |
| `plan.text` | Kuayle, from a planning step's final message (the same text `{{plan}}` carries) | replaced; dropped when a planning step fails |
| `plan.steps` (title, files, commands, acceptance, done) | agents | replaced; a plan text from another attempt, or a failed planning step, clears them |
| `decisions` (decision, why), `done` | agents | added, newest 40 kept |
| `remaining`, `verify` | agents | replaced (`[]` clears) |
| `findings` | Kuayle (`review`, `ci`, `check`, `comment`) and agents (`agent`), numbered | added; agents mark them `resolved` |
| `events` | Kuayle: a usage-limit pause and resume, a pause at a workflow limit, a person's decision | added |

A comment a workspace member posts on the issue while the pull request awaits
review is a `comment` finding, not an instruction: any member who sees the
issue can post one, so it is feedback in the notebook exactly as it is fenced
in the prompt (`followUpPrompt`, `followUpNote`).

`agent_task_notebook_changes` keeps who changed which sections at which step
and attempt (author `agent`, `kuayle` or `person`, user, run, generation,
step, and what was cut or ignored), newest 200 per task.

**Receiving it.** Every attempt that is not a review gets the notebook as
instructions (`Notebook.RenderWith`). People's instructions come first under
their own heading; every entry is indented so agent text cannot open a
heading. It does not repeat what the prompt already holds: where `{{plan}}`
carries the plan, and where the task text is the request, the notebook points
to them.

- Claude Code, Codex and OpenCode read it whole from their instructions file,
  after the profile's own text (`domain.ProfileFiles`).
- The other harnesses get it ahead of the prompt, in its own share of 12000
  bytes (`notebookPromptBytes`). A larger notebook shrinks to it in a fixed
  order: events, then findings, then what is done, then decisions, oldest
  first, and says what it left out. The plan, what remains, how to verify
  and people's instructions stay.

The project's instructions, and on a harness without an instructions file
the profile's too, are placed outside the task prompt's own 32000-byte
budget (`withWholeInstructions`), on every harness: nothing else in the
prompt makes them give way. They are kept whole up to 32,000 bytes together;
beyond that their end is cut (`workflowInstructions` already bounds a
workflow step's project and profile text to that size). A profile read from
an instructions file has no such limit.

**Updating it.** The agent writes `.kuayle/outbox/notebook.json` (or
`notebook-<n>.json`) in its worktree; see the `kuayle-notebook` section of the
`kuayle-agents` skill. Input is read leniently and stored strictly
(`domain.ParseNotebookUpdate`):

- accepted: any of the agents' sections; an update that only resolves
  findings or clears a list with `[]`; a bare text where a list is expected.
  `null` means "not given": it neither clears a section nor counts;
- cut, keeping the first entries, and noted in the change log: a text over 600
  bytes, a list over 40 entries (a step's files and commands included);
- ignored and noted: unknown fields, blank entries, control characters. What
  was cut or ignored is also said once in the notebook's events ("A notebook
  update was accepted in part: …"), which the next agent reads, so a typo is
  not repeated forever;
- refused whole, with the reason kept on the run (`notebook_problem`): a file
  over 16 KB, something that is not one JSON object, a section of the wrong
  shape, an update that says nothing, and any attempt to write a section
  Kuayle owns (`instructions`, `events`, `plan.text`).

**Collection never loses an update.** The outbox take script reads notebook
files first, outside the limits of ten files per worktree and fifty per scan,
and leaves them in place. The manager redacts their strings with the
machine's credential snapshots, `RecordAgentOutbox` merges them, and only
then are they removed, by content (`outboxAckScript`), so an update the agent
rewrote meanwhile stays. A failed read, record or redaction leaves the file
for the next collection; recording the same update twice changes nothing.

**An update counts for the attempt that wrote it.** When an attempt starts
for the first time the manager collects the outbox (so a valid file an
earlier attempt left is still recorded), removes any `notebook*.json`, and
resets the starting run's count (`clearNotebookLeftovers`): an old file never
satisfies a new attempt. `notebook_updates` counts only updates that changed
the notebook. One that changed nothing sets `notebook_unchanged` instead and
is enough only when the attempt itself left the worktree as it found it
(Kuayle's own comparison); otherwise it is treated like a missing update,
and the agent is told that its update changed nothing.

### Closing continuation

An attempt gets at most **one** extra turn before its step closes, in which
it is asked for whatever is still missing
(`machine/closing_continuation.go`). When a run ends and its outbox was
collected, `Manager.settleClosing` lists what is missing, each as a
`domain.ClosingAsk`:

| Missing | Asked of | Ask |
| --- | --- | --- |
| a reply to a delivered message of the task's owner ([live channel](#live-channel)) | an attempt that ended well | `domain.ReplyClosingAsk`: the message numbers and the reply format |
| a task notebook update that counts ([below](#task-notebook)) | an attempt that concluded | `domain.NotebookClosingAsk`: the validator's message, write the file now |
| a plan short enough to hand on whole ([planning steps](#workflows)) | a planning step whose plan was cut | `domain.PlanClosingAsk`: rewrite it under `domain.PlanMaxCharacters` (12000) characters |

`Manager.askToClose(ctx, attempt, asks)` then continues the attempt's own
session once with one short prompt (`domain.ClosingPrompt`) that lists them,
numbered when there are several, and asks for nothing else. Another reason
to ask adds a `ClosingAsk` to that list, never a second continuation.
Afterwards the outbox is collected again and each thing is looked at again;
what is still missing is noted on the run. The continuation never changes how
the attempt ended.

The plan ask is the only one whose reply Kuayle reads: when a planning step's
plan was longer than the next agent can receive whole and the continuation
answers with a plan that fits, that plan replaces the cut one, and the run
carries `domain.PlanShortenedNote` in its risk notes. A rewrite that is still
too long, or none at all, changes nothing: the step fails as before.

- It is a second container of the same run (`…-agent-<run>-closing`) with
  the command line stored at creation (`closing_continuation`, the same
  invocation with placeholders for the session and the prompt), under the
  run's sandbox and mounts but without the live inbox, with the sign-in the
  attempt may have refreshed.
- `closing_asked_at` is taken before it starts: one continuation per
  attempt, also across a manager restart.
- It has five minutes, and never runs past one minute before the attempt's
  own time limit: the run is still recorded as running meanwhile, and a run
  past its limit is killed and marked `timeout`. The limit is counted from
  when the attempt started, also after a manager restart. With less than 30
  seconds left for it, it is not started (and the attempt is not marked as
  asked).
- Nothing is asked for what Kuayle itself failed to collect: a reply or an
  update that may exist unseen is not the agent's omission. Without the
  developer container the outbox is not read at all, so replies stay unknown,
  also after a continuation that ran for something else.
- Its container is removed by its exact name on every path out. A manager
  that restarts and finds the attempt already asked removes what the crash
  left and does not run it again, also when too little time is left or
  nothing is missing any more. The attempt's own container is never
  removed from this path.
- Its output is not the attempt's: summary and logs stay those of the work.
  What its harness reported it spent is added to the run's
  [token usage](#token-usage-and-cost). That it ran and for how long is on
  the run (`closing_asked_at`, `closing_seconds`) and in the machine's events
  (`agent_run.closing_continuation`).

| Harness | Continuation |
| --- | --- |
| Claude Code | `claude --print … --resume <session> -p <prompt>` in the task's session directory |
| Codex | `codex exec resume … <thread> <prompt>` with the thread ID the run reported |
| OpenCode | `opencode run … --session <session> -- <prompt>` with the session ID the run reported, linked to the task's session directory like the attempt, so it opens the same session store |
| Qwen Code | `qwen … --resume <session> <prompt>` with the session ID the run reported, in the same wrapper as the attempt |
| Kimi | `kimi --print … --session <session> --prompt <prompt>` with the ID Kuayle named, in the same wrapper (`KIMI_SHARE_DIR`) as the attempt |
| Goose | `goose run --resume --name <session> … --text <prompt>` with the name Kuayle gave the session, in the same wrapper as the attempt |
| Pi, Crush, custom | none: Kuayle does not continue their sessions, so what is missing is noted at once |

**When an attempt leaves no valid update** (`machine/notebook.go`). The run
of such an attempt has `notebook_required`. The work is never lost over the
notebook:

1. *Asked once, in the same session.* When the attempt concluded (it would
   succeed) without an update that counts, it is asked for one in its
   [closing continuation](#closing-continuation)
   (`domain.NotebookClosingAsk`: the validator's message, write
   `.kuayle/outbox/notebook.json` now), and the outbox is collected again.

2. *Recorded by Kuayle.* If there is still no valid update, the run stays
   succeeded. Kuayle adds the attempt's final message to `done`, marked
   "Recorded by Kuayle from the agent's final message; the agent did not
   update the notebook" (author `kuayle` in the change log), and notes it on
   the run (`notebook_problem`, `domain.NotebookNotUpdated`). The note is in
   the run's risk notes, in the task's reason when it moves to publication,
   in the workflow step's detail, and in the next attempt's handoff.

A collection that failed for a reason of Kuayle's is not the agent's
omission: the agent is not asked, nothing is recorded in its name, and the
run's risk notes say that Kuayle could not collect the update
(`domain.NotebookNotCollected`). A custom, non-LLM harness run as a task
simply gets step 2.

**API.** `GET /agent-tasks/:taskId/notebook` returns the notebook, its
revision and its newest 50 changes to the task's creator (session only, the
task's usual checks). There is no write route: a person adds an instruction
by messaging the agent, retrying with new instructions or approving a
follow-up.

### Lessons in the prompt

The approved lessons of the task's repository
([workspace memory](#workspace-memory)) travel the same way, as the `memory`
prompt source: `devMachineTaskRuntime.Attempt` asks
`WorkspaceMemoryService.ApprovedLessons` for the task (wired with
`AgentTaskService.SetMemory` in `cmd/server/main.go`) and places the block it
returns, under its own heading "Lessons for this repository (approved by the
workspace's admins)". The memory service decides what a task may see (only
approved lessons, a private team's only for that team's tasks) and bounds the
block to 4000 bytes; without approved lessons there is no block. Lessons that
cannot be loaded are logged and the attempt runs without them.

- Claude Code, Codex and OpenCode: in the instructions file, after the
  profile's text and before the notebook.
- The other harnesses: ahead of the prompt. The notebook and the lessons
  share what a prompt has left under 96000 bytes (`promptShares`): with room
  each has its full share (12000 and 4000 bytes); when room is short the
  notebook's detail gives way first, down to a floor of 2000 bytes, and only
  then the lessons. The profile's and the project's instructions do not give
  way to either (they have their own bound of 32,000 bytes, above).

Review tasks are not given the lessons.

### The prompt a step received

Every attempt records what Kuayle assembled for it, split by source
(`dev_machine_agent_run_prompts`, `domain.PromptSource`): `task`,
`workflow_step`, `plan`, `follow_up`, `profile`, `contract` (Kuayle's own
lines), `project` (configured instructions), `notebook`, `handoff`, `issue`,
`related_work`, `messages`, `attachments`, and `memory` (the workspace's
approved lessons). Each part
names its channel: `prompt` (in order, these parts are the prompt exactly as
sent) or `instructions_file`. Each part is cut at 64 KB and the record at
512 KB, marked `truncated`. It has its own table, so reading or polling a
run never carries it. `GET /agent-tasks/:taskId/runs/:runId/prompt` returns
it to the task's creator with `recorded` (false for older runs) and
`worktree_read_only`, redacted with the machine's current and retained
credentials.

### Downloading a task's trace

`GET /agent-tasks/:taskId/debug` returns one JSON document assembled from the
tables that already hold the task's history, in time order, for the task's
creator or a workspace owner/admin who can see its issue (private teams
included; everyone else gets 404). It is session-only and answers as an
attachment, `kuayle-trace-<identifier>.json`, with `Cache-Control: private,
no-store`. The body is the document itself, not the usual response envelope,
so the file can be handed to an LLM or attached to an issue unchanged.

Asking for a review task's trace returns the trace of the task it reviewed,
under that task's access rules: the reviewed task's trace already contains its
review tasks' attempts and reviews, and the filename uses the reviewed task's
identifier. A review task with no recorded reviewed task is answered with 404.

The document (`format: "kuayle.task-trace/v1"`) has a `task` header (state,
reason, failure class, original `prompt` redacted and bounded, workflow version
and node, creator and timestamps), the configured `limits`, `entries`, and
`cuts`. Every entry has a start, an end
(null while it is still open), an outcome and a reason, and carries its kind,
a stable ID and `run_id` when it belongs to an attempt. Entries come from:

| Kind | What it records |
| --- | --- |
| `transition` | every task state change, its reason and actor |
| `workflow_step` | each workflow node entered, its visit, condition, outcome, detail and plan |
| `run` | each attempt (including its review tasks' attempts): status, failure class/rule/cause, environment, output cuts, branch, pull request and notebook result |
| `prompt` | the prompt each attempt received, one entry per attempt, by source and channel |
| `closing_continuation` | when Kuayle asked an attempt for what was missing and for how long |
| `run_step` | publication and manual checks with their commands and summaries |
| `operation` | machine operations requested for the task or its attempts |
| `retry` | retries a person requested, with the instruction they gave |
| `live_message` | both directions of the task's live channel, with deliveries and replies |
| `agent_message` | messages this task's agent sent or received |
| `review` | requested reviews of the task's published work and their findings |
| `ci_signal` | CI and review signals with their outcome, failure class and evidence |
| `pause`, `version_switch` | pauses, resumes and moves to another workflow version |
| `notebook_change`, `notebook` | each notebook revision's author and sections, and the current document |

Free text is redacted twice: first with the current and retained credential
values of every machine an attempt ran on or a machine operation targeted,
then with the well-known credential formats (private keys, cloud keys,
GitHub/GitLab/Slack tokens, API keys, JWTs and passwords embedded in URLs).
The document is bounded in three ways:

- Each text field is cut at 64 KB with `…[truncated]`.
- Each source query keeps its newest 500 rows (2000 transitions), oldest
  first; older rows are cut and listed as `oldest_rows`.
- The whole document is kept under 4 MiB: the largest prompt-source texts are
  dropped first, then the oldest entries.

Every cut is listed in `cuts` (`kind`, `what`, `kept`, `total`, removed
`bytes`); `cuts` is an empty array when nothing was left out. A run's own
`output_cuts` (KUA-137) travel in its `run` entry unchanged. The trace never
includes machine operation payloads, arguments or environment, workspace env
vars, runtime credentials or tokens, log chunks or diffs, or the closing
continuation's command line (only when it was asked and for how long). There
is no Debug view in the app: the action downloads the file, and the file is
the input for an LLM.

## Remaining workflow work

The worker progresses through preparation, native execution (with native
session resume), publication to `awaiting_review`,
and completes when the recorded pull request merges. CI failures and trusted
change requests queue bounded follow-up attempts. Work can also be accepted in
Kuayle without merging. Agents coordinate through issues, dependencies, the
related-work context and [agent messages](#agent-messaging), never through
shared branches. A successful process exit must never mark an issue Done.
Team workflows are experimental; what their first version does not do is
listed under [team workflows](#team-workflows-experimental).

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
- `GET /agent-tasks/board?repository=owner/name` lists those of the tasks that
  are active on one repository, in merge order with their overlap
  ([the board](#merging-only-up-to-date-heads)); `Cache-Control: private, no-store`.
- `GET /agent-tasks/:taskId` reads the durable task.
- `POST /agent-tasks/:taskId/cancel` requests cleanup, including an explicit retry
  of exhausted cleanup operations.
- `POST /agent-tasks/:taskId/pause` and `POST /agent-tasks/:taskId/resume` pause
  and resume a task's workflow ([Pause and resume](#pause-and-resume)); creator
  only, session only.
- `POST /agent-tasks/:taskId/retry` requires request UUID, expected generation and
  new instructions; it preserves the task worktree.
- `POST /agent-tasks/:taskId/accept` completes a task awaiting review without
  merging, given the reviewed `generation` ([accepting without merge](#accepting-without-merge)).
- `GET /agent-tasks/:taskId/history` pages transition records with `after`.
- `GET /agent-tasks/:taskId/runs` pages native attempts with `before`.
- `GET /agent-tasks/:taskId/runs/:runId/logs` pages that attempt's logs with `after`.
- `GET /agent-tasks/:taskId/runs/:runId/artifacts` lists the attempt's kept
  artifacts (`id, path, content_type, size`); `.../artifacts/:artifactId`
  downloads one.
- `POST /agent-tasks/:taskId/checks` runs the supplied `{"command": [...]}`,
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
Manual checks render through the AI Elements
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
GitHub merge `sha`, and only once it contains its base and its checks passed
([merging only up-to-date heads](#merging-only-up-to-date-heads)); if the head moved after the card was shown the approval fails
with a message asking to ask again, and a push that lands after the fresh read is
refused by GitHub with the same clear message. The merge is refused too when the
task's published commit changed.

Publication first fetches the task branch from GitHub with a credential used
only in a fresh repository, and fast-forwards the checkout to commits others
pushed. A divergence is published only when GitHub's tip is exactly the commit
Kuayle recorded as published, which is the agent's own rewrite: the push is
then guarded by `--force-with-lease` against that tip. That push is a
fast-forward in the common case, since the commit step rebuilds on the published
commit; it forces only when the published commit is missing from the checkout,
and then it is refused unless every commit it drops is Kuayle's own. Any other
divergence fails the publication with a reason saying someone else pushed to the
branch.
An agent review is stale when its commit is no longer the recorded head
or the task has published a newer commit:
`get_agent_work` withholds its findings and tells the assistant to offer
`request_agent_review` for the new head. Re-running a review still pins the
commit Kuayle published. An implementing visit synchronizes its checkout before
it starts and can publish a new commit for review.

## Token usage and cost

Each run records what its harness reported it spent (KUA-255, migration
`000204_agent_run_usage`), in nullable columns of `dev_machine_agent_runs`:
`input_tokens`, `output_tokens`, `cache_read_tokens`, `cache_write_tokens`
(bigint) and `cost_usd` (numeric: the harness's own estimate in US dollars, not
a bill). NULL means the harness did not report the value; so do all runs
recorded before the migration and every harness other than the four below.
`input_tokens` excludes cache reads and writes, and `output_tokens` includes
reasoning. When a run ends, the manager reads them from the run's standard
output (`agent.ParseUsage`, called in `machine/manager.go`) and adds what the
[closing continuation](#closing-continuation) reported.

| Harness | Read from | Notes |
| --- | --- | --- |
| Claude Code | `result` events: the sum of `model_usage_since_last_result` over the run's numbered results (`result_index`), every model included (subagents, compaction) | A result's `usage` covers the main loop only, and `total_cost_usd` and `modelUsage` are the session's running totals, which a resumed session continues. They are read, as a session total, only from a Claude Code that does not number its results. |
| Codex | the last `turn.completed` `usage` | Codex reports the thread's running total, which `exec resume` continues, and one `exec` runs one turn. `CompleteAgentRun` stores the total less what the task's earlier runs in the same thread (`native_session_id`) recorded. Input is `input_tokens` less `cached_input_tokens`; there is no cost. |
| OpenCode | the sum of `step_finish` `part.tokens` and `part.cost` | Output is `output` plus `reasoning`. A reported cost of 0 is stored as 0. |
| Pi | the sum of the assistant `message_end` events' `message.usage` (`input`, `output`, `cacheRead`, `cacheWrite`, `cost.total`) | Each states its own model request (Pi keeps no session); `output` already includes reasoning, and `cost.total` is Pi's estimate from its model catalog (0 for an unpriced model, stored as 0). A failed request with an empty usage adds nothing. |

A run whose output Kuayle could not keep whole may under-report (OpenCode and
Pi sum their steps; Claude Code's and Codex's final events are kept in the result
tail). The run DTO carries the five fields when reported, on
`GET /agent-tasks/:taskId/runs` and in the workflow view's `runs`. A task's
usage is the sum over its own runs (`AgentTaskRepository.Usage`, keyed on the
task so that member tasks can join it): `usage` on every page of the runs
response and on `GET /agent-tasks/:taskId/workflow`. A review task's runs
count on the review task. The agent view shows the task's sum in its details
block and each run's under its transcript. Nothing is enforced on cost yet.

## Usage limits

When the agent's provider refuses work until a quota resets (for example Claude
Code's "You've hit your 5-hour limit · resets 4:10am (UTC)"), the task is paused,
not failed: it becomes `blocked` with `usage_limit_message`,
`usage_limit_resets_at` and `usage_limit_resume_at`, keeps its worktree and
session, and resumes on its own after the reset by continuing the same native
session. The provider's wording is read from its JSON events and from the plain
(non-JSON) lines of its output, so a `tool_result` that mentions
`usage_limit_reached` because the agent is editing code about usage limits is
not a limit. The creator's other queued tasks for that agent wait for the reset.
`POST /agent-tasks/:taskId/resume` resumes now and `DELETE` on the same path
turns the automatic resume off. See TECHNICAL.md, "Usage limits".

## Provider credit and unknown models

Two provider answers are about the account or the configuration, not the work,
so another attempt with the same account and model fails the same way (KUA-218).
In production an OpenCode step answered "APIError: Insufficient balance" six
times, and each failure took the workflow's `agent_failed` self-loop and used a
visit.

**No credit.** When the harness's own error says the account has no credit
(OpenCode's "Insufficient balance", Anthropic's "credit balance is too low",
OpenAI's `insufficient_quota` and "You exceeded your current quota", Codex's
"Quota exceeded. Check your plan and billing details."), the manager stores the
run error `provider_credit_exhausted: <the provider's sentence>` and classifies
the run `provider` / `provider_credit`. Only the harness's error and the error
events of its JSON output are read (an OpenCode or Codex `error` or
`turn.failed`, a Claude Code result with `is_error`), never tool output or what
the agent wrote. Links are dropped from the sentence, since a billing link can
name the provider workspace. The task pauses through the usage-limit path: the
attempt is cleaned up, the task ends `blocked` with the provider's sentence in
`usage_limit_message` and `provider_credit_exhausted=true` (migration
`000176_agent_task_provider_credit`), and the reason "Paused: OpenCode's
provider account has no credit left (…). Top up the provider account, then
resume the task; the work done so far is kept." It is not a failure and keeps
no failure class. There is no reset time and no automatic resume, and no
`agent_usage_limit_holds` row: the creator's other tasks pause themselves if
they meet the same account. The creator is told as for a usage limit, through
the blocked transition's conversation event and Telegram notice.

A workflow task stays on its step: no edge is taken, and no visit or iteration
is used. While it waits, `workflow_held_since` stops its time budget, as for a
wait on a person. `POST /agent-tasks/:taskId/resume` after the top-up starts a
new attempt of the same step, visit and iteration with
`resume_native_session=true`. The held time is added to `workflow_budget_from`
and to the step's `paused_seconds`, and the handoff tells the agent that the
account ran out of credit and to continue where it stopped. A retry instead of
a resume forgets the pause and starts a new visit, as a retry always does.

**Unknown model.** When the provider does not offer the run's model, the run
error is `model_not_found: <model>` and the rule `provider` / `model_not_found`.
The model is read from OpenCode's `ProviderModelNotFoundError` ("Model not
found: <model>"), which OpenCode 1.x reports as "UnknownError: Unexpected
server error" in its event and names only in its `level=ERROR` log line on
standard error, from OpenCode 2's error event ("Model unavailable: <model>",
type `provider.no-route`), from Anthropic's `not_found_error` ("model: <model>"), Claude Code's
"There's an issue with the selected model (<model>)", and OpenAI's "The model
`<model>` does not exist or you do not have access to it" or `model_not_found`.
The task fails at once with class `provider` and a reason naming the model and
where it is set, without going through a workflow's `agent_failed` edge: a
step's agent profile in Settings → Agents, an assigned agent's profile, or the
model the task was started with. This is how other configuration errors that a
run reports are handled (a missing sign-in, a resource limit): the task fails
with how to fix it, because a pause would resume with the same model. A retry
keeps the task's model unless the retry names one (`model`); a workflow step
takes its profile's model when the workflow enters the step.

A reviewer step runs as its own review task. When that task fails with
`model_not_found` (read from its current run's `failure_rule`), the workflow
fails with the review task's reason and class `provider` instead of following
the reviewer's `agent_failed` edge, which would start the same review with the
same model. A review task out of credit pauses like any task, and the reviewer
step waits until it is resumed (`POST /agent-tasks/:taskId/resume` with the
review task's ID).
