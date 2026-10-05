FROM golang:1.25.13-alpine AS go-stage

FROM node:22.23.3-alpine3.23@sha256:baf676f7d0e552f3231945c2f979055ca121bce128c152f7a34e6bd1728b1c5a

ARG PROVIDER
ARG NPM_VERSION=12.2.0
ARG CLAUDE_CODE_VERSION=2.1.289
ARG OPENCODE_VERSION=1.18.34
ARG CODEX_VERSION=0.160.0

COPY common/patch-npm-security.cjs common/test-http-cache-semantics.cjs /usr/local/lib/kuayle/

ARG SECURITY_REFRESH=initial
RUN apk upgrade --no-cache \
    && apk add --no-cache bash ca-certificates curl git make openssh-client \
    && npm install -g "npm@${NPM_VERSION}" \
    && case "$PROVIDER" in \
         claude-code) npm install -g --allow-scripts=@anthropic-ai/claude-code "@anthropic-ai/claude-code@${CLAUDE_CODE_VERSION}" ;; \
         opencode) npm install -g --allow-scripts=opencode-ai "opencode-ai@${OPENCODE_VERSION}" ;; \
         codex) npm install -g "@openai/codex@${CODEX_VERSION}" ;; \
         *) echo "unsupported provider: $PROVIDER" >&2; exit 1 ;; \
       esac \
    && node /usr/local/lib/kuayle/patch-npm-security.cjs \
    && rm -rf /root/.npm \
    && case "$PROVIDER" in \
         claude-code) claude --version ;; \
         opencode) opencode --version ;; \
         codex) codex --version ;; \
       esac \
    && npm --version

COPY --from=go-stage /usr/local/go /usr/local/go
ENV GOPATH=/home/kuayle/go \
    GOCACHE=/home/kuayle/.cache/go-build \
    GOMODCACHE=/home/kuayle/go/pkg/mod \
    PATH="/usr/local/go/bin:${PATH}"

COPY common/kuayle-agent-runner /usr/local/bin/kuayle-agent-runner
RUN chmod 0755 /usr/local/bin/kuayle-agent-runner \
    && mkdir -p /home/kuayle /home/kuayle/go /home/kuayle/.cache/go-build \
    && chown -R 1000:1000 /home/kuayle

USER 1000:1000
ENV HOME=/home/kuayle
WORKDIR /workspace
ENTRYPOINT ["/usr/local/bin/kuayle-agent-runner"]
