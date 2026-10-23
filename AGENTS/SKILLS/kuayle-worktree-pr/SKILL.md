---
name: kuayle-worktree-pr
description: Start isolated Kuayle worktrees, commit and push with the configured persona, maintain draft PRs, review changes and merge only with explicit user authorization.
---

# Worktree to reviewed PR

## Isolate and start a draft

1. Read root instructions and inspect `git status --short --branch`,
   `git worktree list`, remotes, and the remote default branch. Preserve others'
   changes. Fetch the target base, then create a new worktree and dedicated task
   branch, e.g. `git worktree add -b <task-branch> <new-path> origin/main` when
   `main` is the confirmed base. Use an existing task worktree only when continuing
   that same task. Keep service ports and disposable databases isolated too.
2. Confirm the effective author and committer with `git var GIT_AUTHOR_IDENT`
   and `git var GIT_COMMITTER_IDENT`. Honor an identity explicitly configured by
   the user for the agent session, including harness-supplied `GIT_AUTHOR_*` and
   `GIT_COMMITTER_*`; otherwise use the repository's configured identity. If an
   explicitly requested identity is missing or differs, stop before committing
   and report the mismatch. Do not pass `--author` or change persistent Git
   identity settings unless the user requests identity setup or repair. Preserve
   existing authors when replaying commits. Follow the environment's
   `git-commit-as-me` skill when available; these portable requirements also
   apply when that developer-specific skill is unavailable.
3. Commit a coherent, validated first increment, push the dedicated branch with
   `git push -u origin HEAD`, and open a PR with `gh pr create --draft --base main
   --head <task-branch> --title '<type>(<scope>): <change>' --body-file <file>`
   against the confirmed base. Write the body to a temporary file with actual
   newlines. Keep the PR draft while implementation or verification remains.
   Fetch-based worktree creation may initially track `origin/main`; set the
   task branch's upstream on its first push rather than pushing to that base.

## Commit and push completed increments

- Use conventional commit messages such as `docs(agents): document UI composition`
  or `fix(ui): preserve dialog focus`. No `Co-Authored-By:` trailers or generated/AI
  attribution in commits, tags, PR titles or bodies.
- Run applicable [checks](../kuayle-testing/SKILL.md), inspect status/diff, and stage
  explicit task-owned paths. Never stage others' work, secrets, `.env`, keys,
  local databases, `data/`, or generated output. Avoid `git add .`/`git add -A`.
- Commit and push verified work without another confirmation. After each commit,
  verify `git log -1 --format='%an <%ae>%n%cn <%ce>%n%B'`. Subsequent pushes use
  `git push` to the task upstream. Ask before force-pushing, rewriting/deleting published history,
  deleting branches, changing push destination, or committing others' work.

## Review and merge gate

- Review the entire final diff against the issue and acceptance criteria, including
  behavior, permissions, migrations, UI reuse/accessibility, tests and harness
  freshness as applicable. Fix findings and repeat affected checks. Update the
  PR description to explain the final problem/change, evidence, and limitations.
- Inspect `gh pr view <number> --json headRefOid,isDraft,mergeable,reviewDecision,statusCheckRollup`
  and `gh pr checks <number>`. Verify checks belong to the final pushed commit,
  review outstanding feedback, and satisfy required reviews. Do not use admin
  bypass or self-approval as a substitute for a required reviewer.
- Mark ready with `gh pr ready <number>` only after implementation, verification
  and review are complete. **Merge only when the user explicitly authorizes this
  PR's merge.** General commit/push approval is not merge permission; otherwise
  leave the reviewed PR open. If checks or external requirements are blocked,
  report the exact blocker and leave it open.
- When authorized and all gates pass, use a repository-supported merge method
  without deleting the branch, bind the merge to the reviewed head when supported
  (e.g. `gh pr merge <number> --merge --match-head-commit <sha>`), then verify the
  PR is merged and record its merge commit. Do not create a release unless asked.
  Report PR link, commit(s), delivered changes, checks and any material limits.
