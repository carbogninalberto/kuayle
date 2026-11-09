---
name: kuayle
description: Use the official `kuayle` CLI to work with a Kuayle self-hosted issue tracker (issues, comments, labels, projects, cycles, team and project wikis, daily registry). Trigger whenever the user mentions Kuayle, references issue identifiers like KUA-123/ENG-42, asks to file/triage/update tickets, read or edit wiki pages, log to the registry, or wants an agent to read or write tasks in Kuayle.
---

We track our tickets, projects and wikis in Kuayle, a self-hosted issue tracker.
Use your Bash tool to call the `kuayle` executable. Run `kuayle usage` for the
full command reference (global flags, name resolution, exit codes). Prefer
`--format json` and branch on exit codes: 2=re-auth needed, 3=not found,
4=invalid input, 5=forbidden (the token lacks a scope), 6=rate limited,
7=network/server error.

- Issues are addressed by identifier (`ENG-42`); teams, projects, labels and
  people by name.
- Wiki and registry commands need `--team <name|key|id>` or `--project <name|id>`.
  Read a page before editing it; `kuayle wiki update` sends the page version and
  fails with `WIKI_VERSION_CONFLICT` if someone else saved first — re-read, merge,
  and retry rather than overwriting.
- Log notable progress with `kuayle registry append --team <team> "<text>"`.
- Never print or paste the access token. If a command exits 5, report the missing
  scope instead of trying another route.

Install: `curl -fsSL https://raw.githubusercontent.com/carbogninalberto/kuayle/main/CLI/install.sh | sh`
(verifies SHA-256 checksums). Guide: https://github.com/carbogninalberto/kuayle/blob/main/docs/cli.md
