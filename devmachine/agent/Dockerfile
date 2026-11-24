FROM golang:1.26.11-alpine AS go-stage

# Compose and Buildx, built from pinned release commits (see the script).
FROM --platform=$BUILDPLATFORM golang:1.26.11-alpine AS docker-plugins
ARG TARGETARCH
RUN apk add --no-cache git
COPY common/build-docker-plugins.sh /usr/local/bin/build-docker-plugins.sh
RUN GOOS=linux GOARCH=$TARGETARCH sh /usr/local/bin/build-docker-plugins.sh /out

FROM node:22.23.3-alpine3.23@sha256:baf676f7d0e552f3231945c2f979055ca121bce128c152f7a34e6bd1728b1c5a

ARG PROVIDER
ARG TARGETARCH
ARG NPM_VERSION=12.2.0
ARG CLAUDE_CODE_VERSION=2.1.330
ARG OPENCODE_VERSION=2.3.2
ARG CODEX_VERSION=0.171.0

COPY --from=docker-plugins /out/docker-compose /out/docker-buildx /usr/local/lib/docker/cli-plugins/
COPY common/install-docker-cli.sh /usr/local/lib/kuayle/
COPY common/start-common.sh common/codex-model-catalog.cjs common/patch-npm-security.cjs common/test-http-cache-semantics.cjs common/kuayle-agents.md common/kuayle-live-inbox-hook.cjs common/claude-managed-settings.json common/check-agent-cli-flags.sh /usr/local/lib/kuayle/

ARG SECURITY_REFRESH=initial
RUN apk upgrade --no-cache \
    && apk add --no-cache bash ca-certificates curl git make openssh-client ripgrep \
    && sh /usr/local/lib/kuayle/install-docker-cli.sh "$TARGETARCH" \
    && npm install -g "npm@${NPM_VERSION}" \
    && case "$PROVIDER" in \
         claude-code) npm install -g --allow-scripts=@anthropic-ai/claude-code "@anthropic-ai/claude-code@${CLAUDE_CODE_VERSION}" ;; \
         opencode) npm install -g --allow-scripts=@opencode/cli "@opencode/cli@${OPENCODE_VERSION}" ;; \
         codex) npm install -g "@openai/codex@${CODEX_VERSION}" ;; \
         *) echo "unsupported provider: $PROVIDER" >&2; exit 1 ;; \
       esac \
    && node /usr/local/lib/kuayle/patch-npm-security.cjs \
    && rm -rf /root/.npm \
    && case "$PROVIDER" in \
         claude-code) claude --version \
           && sh /usr/local/lib/kuayle/check-agent-cli-flags.sh claude \
           && install -D -m 0644 /usr/local/lib/kuayle/claude-managed-settings.json /etc/claude-code/managed-settings.json ;; \
         opencode) opencode --version && sh /usr/local/lib/kuayle/check-agent-cli-flags.sh opencode ;; \
         codex) codex --version && sh /usr/local/lib/kuayle/check-agent-cli-flags.sh codex ;; \
       esac \
    && npm --version

COPY --from=go-stage /usr/local/go /usr/local/go
ENV GOPATH=/home/kuayle/go \
    GOCACHE=/home/kuayle/.cache/go-build \
    GOMODCACHE=/home/kuayle/go/pkg/mod \
    PATH="/usr/local/go/bin:${PATH}"

COPY common/kuayle-agent-runner /usr/local/bin/kuayle-agent-runner
COPY --chmod=0755 common/kuayle-say.cjs /usr/local/bin/kuayle-say
RUN chmod 0755 /usr/local/bin/kuayle-agent-runner \
    && mkdir -p /home/kuayle /home/kuayle/go /home/kuayle/.cache/go-build \
    && chown -R 1000:1000 /home/kuayle

USER 1000:1000
ENV HOME=/home/kuayle
WORKDIR /workspace
ENTRYPOINT ["/usr/local/bin/kuayle-agent-runner"]
