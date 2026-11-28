# Machine Image

`kuayle/dev-machine-headless` is the single Dev Machine image. It is a regular
(unminimized) Ubuntu with manual pages, common tools, the `en_US.UTF-8` locale,
`Etc/UTC`, a fixed font set (Ubuntu, Noto incl. CJK and Color Emoji, Liberation,
DejaVu), Node.js, Go, Python, every built-in agent CLI, and Playwright with its
Chromium in `/opt/ms-playwright` (used by `kuayle-screenshot`; a project's own
Playwright installs its browsers as usual). It runs in two
roles: the developer terminal and every autonomous agent run, check and preview.
Compilers are not preinstalled (their kernel headers fail the image scan);
agents install them with `sudo apt-get` when a project needs them.

## Screenshots

`kuayle-screenshot URL FILE` renders a page with the image's Playwright and
Chromium at a fixed viewport, scale, locale, time zone, colour scheme and font
rendering, so the same page gives the same picture on every machine. Run
`kuayle-screenshot --help` for its options. The smoke test renders a page as
UID 1000 and checks the PNG's size.

## Developer terminal

Use the image CMD and the same service wrapper as other developer containers:

```text
Service:    terminal
Entrypoint: /usr/local/bin/kuayle-service-entrypoint
Cmd:        /usr/local/bin/kuayle-headless-start
WorkingDir: /workspace
User:       1000:1000
Health:     HTTP GET http://127.0.0.1:7681/
```

The runtime must prepare and mount `/run/kuayle-secrets`, set
`KUAYLE_SECRET_NAMES` and the optional repository environment, then start the
container. The wrapper
exports the scoped secrets and executes the CMD. The CMD initializes the shared
workspace, starts ttyd on port 7681, and writes `/workspace/.kuayle-ready` after
ttyd answers.
Equivalent direct Docker execution is:

```sh
docker run --entrypoint /usr/local/bin/kuayle-service-entrypoint IMAGE \
  /usr/local/bin/kuayle-headless-start
```

## Autonomous agent run

The runtime must explicitly select the agent entrypoint and pass the provider
argv as the CMD:

```text
Entrypoint: /usr/local/bin/kuayle-agent-runner
Cmd:        provider argv array
WorkingDir: /workspace or the selected checkout path
User:       1000:1000
```

The runner loads scoped secrets, writes the model server Codex config only when
the file is absent, installs the Git credential helper, and executes the argv. This
matches `DockerRuntime.RunAgent` and keeps the image CMD out of autonomous runs.

## Sudo and global npm installs

The image ships `sudo` with a passwordless rule for UID 1000. It works only in
containers the runtime creates without `no-new-privileges`: always for agent
attempts, checks, previews, tools and commands, so agents can install the
system packages a project needs (they last for that container), and for the
developer terminal of machines created while the workspace allows sudo
(`allow_sudo`). `/etc/npmrc` (`NPM_CONFIG_GLOBALCONFIG`) sends global npm
installs to `/workspace/.kuayle/npm-global`, first on `PATH` (agent runs drop it
from theirs), so `npm i -g @anthropic-ai/claude-code` works without sudo.
Claude Code's managed settings (`/etc/claude-code/managed-settings.json`)
install Kuayle's live inbox hook. See `TECHNICAL.md` "Sudo in machines".
