---
name: kuayle-review
description: Peer-review Kuayle code (a PR, an area or the whole codebase) for security, correctness, architecture flows, maintainability, refactoring, UI, UX, performance, tests and operations, and report verified, ranked findings.
---

# Peer review

A review finds what would hurt a user, an operator or the next developer, proves
it, and says how to fix it. It is not a style pass and not a rewrite. The detailed
checks per area are in [the review lenses](lenses.md); this file is the workflow,
the bar for a finding and the report format.

## Ground rules

- **Read-only.** A review changes no code, data, settings, branches or machines.
  Fix something only when the user asks for fixes; then follow the
  [worktree and PR skill](../kuayle-worktree-pr/SKILL.md) and the
  [testing skill](../kuayle-testing/SKILL.md). Do not post comments on GitHub,
  in Kuayle or anywhere else unless asked.
- **Evidence or it isn't a finding.** Every finding names the file and line,
  the concrete input or state that triggers it and the wrong result. "This could
  be risky" is not a finding. If you cannot build the failure scenario, either
  verify it (test, query, reproduction) or drop it.
- **Review the code as it is.** Read the current implementation and its tests,
  not only the diff, the docs or a summary. Docs that disagree with the code are
  a finding about the docs.
- **Never expose secrets.** Refer to credentials, keys, tokens and `.env` values
  by name and location only; never print, quote or paste their values, even
  partially, in commands, reports, issues or chat. Do not copy production data,
  customer content or deployment-private hosts into reports.
- **Web access goes through headless Playwright**, as [the tool index](../../TOOLS.md)
  says; never a browser extension or the user's signed-in browser.
- **Respect the project's rules.** Findings are measured against
  [AGENTS.md](../../../AGENTS.md), the [backend](../kuayle-backend/SKILL.md),
  [frontend](../kuayle-frontend/SKILL.md), [web](../kuayle-web/SKILL.md) and
  testing skills, [TECHNICAL.md](../../../TECHNICAL.md),
  [SECURITY_AUDIT.md](../../../SECURITY_AUDIT.md) and the contracts under
  [BE/docs](../../../BE/docs). Cite the rule a finding breaks instead of
  restating it, and don't invent stricter house rules.

## Choose the mode

| Mode | When | Scope |
| --- | --- | --- |
| **PR review** | A pull request or a branch | The full diff plus every caller, callee and test it touches. Check the whole flow the change belongs to, not only changed lines. |
| **Area review** | "Review the task engine", "review settings UI" | One feature end to end: UI, API, service, repository, migrations, runtime, tests, docs. |
| **Codebase review** | A periodic general review | Every area, by lens, using the sweep plan below. Deliverable is a report plus issues. |

For a PR review, also read the linked Kuayle issue and its acceptance criteria,
answer and resolve every bot review comment (for example Codex) with a reply that
says what was fixed or why not, and confirm CI ran on the final head commit.
Merging stays behind the user's explicit authorization in the
[worktree and PR skill](../kuayle-worktree-pr/SKILL.md).

## Workflow

1. **Frame.** Write down the scope, the commit reviewed (`git rev-parse HEAD`
   after `git fetch`), what you will not cover, and the questions that matter most
   for this scope (for a task-engine change: can a task get stuck, run twice,
   leak credentials, or lose its result?).
2. **Map the flows.** Before judging code, trace each user-visible flow end to
   end and write the chain down, for example
   `composer → UI/src/lib/api → handler → service → repository/SQL → machine manager → container → GitHub → webhook → task state → UI event`.
   Note every state machine (task states, machine status, review/merge states),
   every place a credential travels, every background loop and every external call.
   Most serious bugs live at the seams between these steps.
3. **Sweep by lens.** Apply each relevant lens in [lenses.md](lenses.md). Use
   `rg` narrowly; read whole functions and their tests rather than grepping
   fragments. Note candidates with file:line as you go.
4. **Verify every candidate.** Confirm it against the actual code path, callers,
   constraints and tests. Prefer proof: a failing test in a scratch worktree, a
   query on a disposable database, a Playwright run against a local build, or a
   precise code trace. Look for the guard that would make it a non-issue
   (middleware, SQL predicate, DB constraint, lease fence, retry loop) before
   reporting it. Mark each finding **Confirmed** (proved) or **Plausible**
   (strong trace, not reproduced) and drop the rest.
5. **Rank.** Assign severity with the rubric below, then order by severity,
   then by how many users or operators it reaches.
6. **Report and file.** Write the report (format below). For a codebase or area
   review, file one Kuayle issue per finding or tightly related group once the
   user agrees (see "Turn findings into issues").

## Delegating a codebase review

Run at most **3 subagents at a time**. Give each one lens group or one area, the
commit, the relevant skill and lens sections, and this file's finding format;
tell it to stay read-only and to return candidates with evidence, not
conclusions. Suggested split: (1) security and authorization, (2) backend
correctness, architecture flows and the machine runtime, (3) UI, UX and
accessibility. Run a second wave for maintainability and refactoring,
performance, tests and CI, and operations. The parent verifies every returned
candidate itself before it enters the report: a subagent's confidence is not
evidence. Report launch failures instead of silently substituting another model.

## Severity

| Severity | Meaning | Examples |
| --- | --- | --- |
| **Critical** | Exploitable security hole, data loss or corruption, or an outage with no workaround. Fix before anything else. | Cross-workspace read through a missing SQL predicate; secret printed to logs or a PR; migration that drops data. |
| **High** | Wrong behavior that users hit in normal use, or a security weakness that needs one more condition. | Task stuck forever after a restart; follow-up loses review findings; guest can trigger an owner action through the API. |
| **Medium** | Wrong in edge cases, degraded UX, missing recovery, misleading UI, a real maintainability cost. | Stale settings until a container is rebuilt; error swallowed and shown as success; duplicated logic that has already drifted. |
| **Low** | Minor polish, small inefficiency, unclear naming, a test gap around correct code. | Inconsistent wording; extra query per page; missing empty state. |
| **Nit** | Optional taste. Report only a handful, grouped at the end. | Comment wording, ordering. |

Raise severity when the issue is silent (no error, wrong result), affects
security or money (API billing, credits), or breaks self-hosted upgrades. Lower it
when a guard elsewhere already limits the blast radius, and say which guard.

## Finding format

Each finding, in this order and in plain language:

- **Title**: the defect, not the fix ("Follow-ups drop review findings", not
  "Improve follow-ups").
- **Severity**, **Lens**, **Confirmed/Plausible**.
- **Where**: `path/to/file.go:123` (and the other ends of the flow involved).
- **What happens**: the concrete trigger and the wrong outcome, as a short
  scenario ("A Reviewer profile changed to Implementer starts editing the
  worktree of every running task that uses it").
- **Why**: the root cause in one or two sentences, with the rule or contract it
  breaks, linked.
- **Fix**: the smallest change that removes the cause, plus the test that would
  have caught it. Name an existing helper or pattern to reuse when there is one.
- **Evidence**: the test, query, trace or reproduction that proved it, without
  secret values.

## Report

Lead with the answer: how healthy the scope is and the few things that matter.
Then:

1. **Summary table**, most severe first: `# | Severity | Lens | Finding | Where | Status`.
2. **Findings** in the format above, grouped by severity.
3. **Flows checked**: the chains you traced, so the reader knows what was covered.
4. **Not checked**: areas, lenses or environments you skipped and why (no
   database, no device, out of scope). Never imply coverage you didn't do.
5. **Strengths worth keeping**: two to five patterns that are done well and
   should be copied, so refactors don't remove them.
6. **Suggested order of work**: what to fix first and what can be one PR.

Write for someone who has not read the code: short sentences, no shorthand, no
unexplained internal names, file:line links for every claim. For a codebase or
area review, publish the report as a page and give the link; for a PR review,
answer in chat unless the user asks for a page.

## Turn findings into issues

Once the user agrees, file one Kuayle issue per finding (or per group that one
PR would fix), most severe first, in the team the code belongs to. Use this
shape, which coding agents can pick up directly:

- **Title**: `<area>: <defect in plain words>`.
- **Problem**: what happens, who it affects, file:line, and how it was confirmed.
- **Change**: the intended fix and the boundaries it must keep (authorization,
  workspace isolation, transactions, existing components).
- **Acceptance criteria**: observable outcomes, including the regression test
  that must fail before the fix.
- Priority from the severity (Critical/High → urgent/high).

Do not file issues for nits, and do not duplicate an open issue: search first and
add evidence to the existing one instead.
