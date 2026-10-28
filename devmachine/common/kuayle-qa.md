---
name: kuayle-qa
description: Use when the reviewer should try your change by hand on this Kuayle Dev Machine; start the app, expose its port with kuayle-ports and report the URL and what to test.
---

# Manual QA on a Kuayle Dev Machine

Kuayle publishes up to ten ports of this machine, each on its own
authenticated host that only the machine's owner can open. Use it so the
reviewer can try your change in a browser.

1. Start the app from the task worktree, bound to `0.0.0.0` (not `localhost`)
   on a free port from 1024 to 65535. Run `kuayle-ports` first to see which
   ports already listen. Keep it running after you finish, detached from your
   shell, for example:
   `setsid nohup npm run dev -- --host 0.0.0.0 --port 3000 >/tmp/app-3000.log 2>&1 &`
2. Check that it answers: `curl -fsS http://127.0.0.1:3000/ >/dev/null`.
3. Expose it with a short label: `kuayle-ports expose 3000 --label web`.
   It prints the port's URL, or why Kuayle refused it.
4. In your final summary, list each exposed port (label and URL) and what to
   test there: pages, flows, accounts or data to use, and the expected result.
   The reviewer opens it from the machine page or the task's app menu in
   Kuayle, which signs them in to that host.

Rules:

- Never expose the machine's own services (code-server on 8080, the terminal
  on 7681); Kuayle refuses them.
- Expose only what the reviewer needs, and remove ports you no longer use with
  `kuayle-ports unexpose PORT`.
- Do not put secrets in labels, logs or the summary. The app receives no Kuayle
  credentials and must not depend on them.
- When the project defines a preview command, Kuayle starts it for each task
  in review on its own port; you do not need to expose that one.
