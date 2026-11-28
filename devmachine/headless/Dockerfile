FROM golang:1.26.11-alpine AS go-stage

FROM node:22.23.3-bookworm-slim@sha256:43ac6c60b8f89723f746e8a92ce91abd5017e627ce1ddfe4238355d3a30b772c AS node-stage

FROM golang:1.27.4-bookworm AS crush-build
# Release binaries embed an older Go standard library with known CVEs; build with patched Go.
ARG CRUSH_VERSION=0.102.0
# golang.org/x/net is raised past CVE-2026-78661 (GO-2026-6860), which the
# release still carries, by building Crush from a local module that requires it.
ARG CRUSH_XNET_VERSION=v0.62.0
RUN mkdir /src-crush && cd /src-crush && go mod init kuayle.local/crush-build \
    && go get "github.com/charmbracelet/crush@v${CRUSH_VERSION}" "golang.org/x/net@${CRUSH_XNET_VERSION}" \
    && CGO_ENABLED=0 GOBIN=/out go install -trimpath -ldflags="-s -w" github.com/charmbracelet/crush

# Compose and Buildx, built from pinned release commits (see the script).
FROM --platform=$BUILDPLATFORM golang:1.26.11-alpine AS docker-plugins
ARG TARGETARCH
RUN apk add --no-cache git
COPY common/build-docker-plugins.sh /usr/local/bin/build-docker-plugins.sh
RUN GOOS=linux GOARCH=$TARGETARCH sh /usr/local/bin/build-docker-plugins.sh /out

FROM ubuntu:26.04@sha256:f144425ff09be612d6d9ad965196e9cdc23dae1f42110a8a11a3e9a8198759f7

ARG TARGETARCH
ARG DEBIAN_FRONTEND=noninteractive
ARG TTYD_VERSION=1.7.7
ARG TTYD_AMD64_SHA256=8a217c968aba172e0dbf3f34447218dc015bc4d5e59bf51db2f2cd12b7be4f55
ARG TTYD_ARM64_SHA256=b38acadd89d1d396a0f5649aa52c539edbad07f4bc7348b27b4f4b7219dd4165
ARG NPM_VERSION=12.2.0
# Playwright and the Chromium build it pins, for agents' browser QA and
# screenshots (kuayle-screenshot). Keep it at the version WEB/package-lock.json
# locks, so the image renders like the repository's own browser tests.
ARG PLAYWRIGHT_VERSION=1.65.0

COPY --from=node-stage /usr/local /usr/local
COPY --from=crush-build /out/crush /usr/local/bin/crush
COPY --from=go-stage /usr/local/go /usr/local/go
COPY --from=docker-plugins /out/docker-compose /out/docker-buildx /usr/local/lib/docker/cli-plugins/
COPY common/install-agent-clis.sh common/install-docker-cli.sh common/kuayle-ports common/kuayle-service-entrypoint common/kuayle-agent-runner /usr/local/bin/
COPY common/start-common.sh common/codex-model-catalog.cjs common/patch-npm-security.cjs common/test-http-cache-semantics.cjs common/kuayle-qa.md common/kuayle-agents.md common/kuayle-agent-sign-in.sh common/kuayle-claude-config.cjs common/check-agent-cli-flags.sh common/kuayle-live-inbox-hook.cjs common/claude-managed-settings.json /usr/local/lib/kuayle/
COPY headless/kuayle-headless-start /usr/local/bin/kuayle-headless-start
COPY common/kuayle-shell /usr/local/bin/kuayle-shell
COPY common/kuayle-terminal-session /usr/local/bin/kuayle-terminal-session
COPY common/kuayle-task-claude /usr/local/bin/kuayle-task-claude
COPY --chmod=0755 common/kuayle-say.cjs /usr/local/bin/kuayle-say
COPY --chmod=0755 common/kuayle-screenshot.cjs /usr/local/bin/kuayle-screenshot
COPY common/tmux.conf /etc/tmux.conf

# A regular (unminimized) Ubuntu, so the machine behaves like a real one:
# manual pages, common tools, locales and a fixed font set, which makes
# screenshots render the same on every machine.
# Passwordless sudo for the machine user takes effect in agent attempts,
# checks, previews, tools and commands, which run without no-new-privileges
# so agents can install the system packages a project needs; the developer
# terminal stays inert unless the workspace allows sudo (allow_sudo). See
# TECHNICAL.md "Sudo in machines".
ARG SECURITY_REFRESH=initial
RUN rm -f /usr/bin/pebble \
    && apt-get update \
    && apt-get upgrade -y \
    && apt-get install -y --no-install-recommends unminimize \
    && yes | unminimize \
    && apt-get install -y --no-install-recommends \
         ca-certificates curl dnsutils file git gnupg jq less locales lsb-release \
         make man-db nano ncurses-term openssh-client pkg-config procps python3 python3-pip \
         python3-venv ripgrep rsync sudo tmux tzdata unzip wget xz-utils zip \
         fontconfig fonts-dejavu-core fonts-liberation fonts-noto-cjk fonts-noto-color-emoji \
         fonts-noto-core fonts-ubuntu \
         libasound2t64 libatk-bridge2.0-0t64 libatk1.0-0t64 libatspi2.0-0t64 libcairo2 libcups2t64 \
         libdbus-1-3 libdrm2 libgbm1 libglib2.0-0t64 libnspr4 libnss3 libpango-1.0-0 libx11-6 \
         libxcb1 libxcomposite1 libxdamage1 libxext6 libxfixes3 libxkbcommon0 libxrandr2 \
    && sed -i 's/^# *en_US.UTF-8 UTF-8/en_US.UTF-8 UTF-8/' /etc/locale.gen \
    && locale-gen \
    && fc-cache -f \
    && case "${TARGETARCH}" in \
         amd64) ttyd_arch=x86_64; ttyd_sha="$TTYD_AMD64_SHA256" ;; \
         arm64) ttyd_arch=aarch64; ttyd_sha="$TTYD_ARM64_SHA256" ;; \
         *) echo "unsupported architecture: ${TARGETARCH}" >&2; exit 1 ;; \
       esac \
    && curl -fsSL "https://github.com/tsl0922/ttyd/releases/download/${TTYD_VERSION}/ttyd.${ttyd_arch}" -o /usr/local/bin/ttyd \
    && echo "${ttyd_sha}  /usr/local/bin/ttyd" | sha256sum -c - \
    && chmod 0755 /usr/local/bin/ttyd \
    && npm install -g "npm@${NPM_VERSION}" \
    && sh /usr/local/bin/install-agent-clis.sh "$TARGETARCH" \
    && npm install -g "playwright@${PLAYWRIGHT_VERSION}" \
    && PLAYWRIGHT_BROWSERS_PATH=/opt/ms-playwright playwright install chromium \
    && chown -R 1000:1000 /opt/ms-playwright \
    && install -D -m 0644 /usr/local/lib/kuayle/claude-managed-settings.json /etc/claude-code/managed-settings.json \
    && sh /usr/local/lib/kuayle/check-agent-cli-flags.sh \
    && sh /usr/local/bin/install-docker-cli.sh "$TARGETARCH" \
    && node /usr/local/lib/kuayle/patch-npm-security.cjs \
    && rm -rf /root/.npm /var/lib/apt/lists/* \
    && rm -f /usr/bin/pebble /etc/ssl/private/ssl-cert-snakeoil.key /etc/ssl/certs/ssl-cert-snakeoil.pem \
    && mkdir -p /home/kuayle/go /home/kuayle/.cache/go-build \
    && chown -R 1000:1000 /home/kuayle \
    && chmod 0755 \
         /usr/local/bin/kuayle-ports \
         /usr/local/bin/kuayle-service-entrypoint \
         /usr/local/bin/kuayle-agent-runner \
         /usr/local/bin/kuayle-headless-start \
         /usr/local/bin/kuayle-shell \
         /usr/local/bin/kuayle-terminal-session /usr/local/bin/kuayle-task-claude \
         /usr/local/bin/install-agent-clis.sh \
    && ttyd --version \
    && /usr/local/go/bin/go version \
    && user_name=$(getent passwd 1000 | cut -d: -f1) \
    && printf '%s ALL=(ALL) NOPASSWD:ALL\n' "$user_name" > /etc/sudoers.d/kuayle \
    && printf 'Defaults env_keep += "http_proxy https_proxy no_proxy HTTP_PROXY HTTPS_PROXY NO_PROXY"\n' >> /etc/sudoers.d/kuayle \
    && chmod 0440 /etc/sudoers.d/kuayle \
    && visudo -cf /etc/sudoers.d/kuayle \
    && printf 'prefix=/workspace/.kuayle/npm-global\nallow-scripts=@anthropic-ai/claude-code\n' > /etc/npmrc

# Global npm installs (npm i -g, Claude Code's updater) go to the persistent
# workspace volume, ahead of the image's CLIs on PATH, so they work without sudo.
# /etc/npmrc is npm's global config (projects and ~/.npmrc override it); it lets
# Claude Code's install script fetch its native binary, as the image build does.
USER 1000:1000
ENV HOME=/home/kuayle \
    LANG=en_US.UTF-8 \
    LC_ALL=en_US.UTF-8 \
    TZ=Etc/UTC \
    PLAYWRIGHT_BROWSERS_PATH=/opt/ms-playwright \
    SHELL=/usr/local/bin/kuayle-shell \
    COLORTERM=truecolor \
    GOPATH=/home/kuayle/go \
    GOCACHE=/home/kuayle/.cache/go-build \
    GOMODCACHE=/home/kuayle/go/pkg/mod \
    NPM_CONFIG_GLOBALCONFIG=/etc/npmrc \
    PATH="/workspace/.kuayle/npm-global/bin:/usr/local/go/bin:/usr/local/bin:${PATH}"
WORKDIR /workspace
EXPOSE 7681
CMD ["/usr/local/bin/kuayle-headless-start"]
