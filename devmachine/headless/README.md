# Headless Dev Machine Image

`kuayle/dev-machine-headless` supports two roles from one glibc-based image.

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
`KUAYLE_SECRET_NAMES`, add the same optional repository and collector environment
used by the IDE developer container, then start the container. The wrapper
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
containers the runtime creates without `no-new-privileges`, which it does for
the developer terminal of machines created while the workspace allows sudo
(`allow_sudo`); autonomous agent runs stay locked. `/etc/npmrc`
(`NPM_CONFIG_GLOBALCONFIG`) sends global npm installs to
`/workspace/.kuayle/npm-global`, first on `PATH` (agent runs drop it from
theirs), so `npm i -g @anthropic-ai/claude-code` works without sudo. See
`TECHNICAL.md` "Sudo in machines".
