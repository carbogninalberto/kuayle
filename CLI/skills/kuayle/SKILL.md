---
name: kuayle
description: Use the `kuayle` CLI to work with a kuayle self-hosted issue tracker (issues, comments, labels, projects, cycles). Trigger whenever the user mentions kuayle, references issue identifiers like KUA-123/ENG-42, asks to file/triage/update tickets, or wants an agent to read or write tasks in kuayle.
---

We track our tickets and projects in kuayle, a self-hosted issue tracker.
We use the `kuayle` CLI to communicate with it. Use your Bash tool to call the
`kuayle` executable. Run `kuayle usage` to see the full command reference
(global flags, name resolution, exit codes). Prefer `--format json` and branch
on exit codes: 2=re-auth needed, 3=not found, 4=invalid input, 5=forbidden,
6=rate limited, 7=network/server error.
