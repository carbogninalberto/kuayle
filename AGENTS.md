# Working on Kuayle

Kuayle has three independently built areas: `BE/` (Go/Echo API and PostgreSQL),
`UI/` (SvelteKit application), and `WEB/` (prerendered marketing site), plus the
optional internal `voice/` transcription service (Python).

## Start with the relevant context

- Read [AGENTS/SKILLS.md](AGENTS/SKILLS.md), then only the skill(s) for the task.
  These are repository instructions; open their `SKILL.md` directly if your agent
  does not discover this directory automatically. No global installation is needed.
- Use [AGENTS/TOOLS.md](AGENTS/TOOLS.md) for reusable tools and verified command
  sources. Search narrowly with `rg`; do not load the entire codebase or every skill.
- Inspect the affected implementation and tests, state acceptance criteria, and
  preserve unrelated work. Read [TECHNICAL.md](TECHNICAL.md) by relevant section.

## Required implementation rules

- **Use shadcn-svelte as the UI foundation in both UI and WEB.** Reuse suitable
  project components, then installed primitives; compose custom components on
  those primitives. Do not add handcrafted substitutes for covered controls.
  Semantic HTML for content/layout is appropriate. Follow the
  [frontend skill](AGENTS/SKILLS/kuayle-frontend/SKILL.md) for installation,
  legacy components, accessibility, styling, and documented exceptions.
- For behavior changes and regressions, write a meaningful failing test, implement
  the behavior, then refactor and verify. Use proportionate checks for docs and
  low-impact edits; do not add tests that merely match implementation text.
  Follow the [testing skill](AGENTS/SKILLS/kuayle-testing/SKILL.md), and report
  skipped or blocked checks accurately.
- Preserve backend authorization, workspace/team isolation, validation, and
  transaction boundaries. UI visibility is never an authorization boundary.
- Follow the [worktree and PR skill](AGENTS/SKILLS/kuayle-worktree-pr/SKILL.md):
  isolated worktrees, draft PRs while incomplete, configured git persona,
  conventional commits, and explicit user authorization before merging.
- CI changes must preserve the [change-impact map](scripts/ci/README.md), its
  scenario tests and stable result gates. Verify selected jobs and intentional
  omissions on the complete PR diff before declaring CI green.
- Keep this harness current in the same change whenever paths, architecture,
  commands, UI conventions, or workflows change. Keep each rule in one source;
  update the indexes when adding/removing tools or skills. Validate with
  `node AGENTS/TOOLS/check-harness.mjs` and review the instructions against the
  changed workflow. Never turn task-specific merge permission into a project rule.
