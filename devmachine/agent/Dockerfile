FROM golang:1.25.12-bookworm AS go-stage

FROM node:22.17.0-bookworm-slim

ARG PROVIDER
ARG CLAUDE_CODE_VERSION=2.1.14
ARG OPENCODE_VERSION=1.1.25
ARG CODEX_VERSION=0.80.0

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates git curl make openssh-client \
    && rm -rf /var/lib/apt/lists/* \
    && case "$PROVIDER" in \
         claude-code) npm install -g "@anthropic-ai/claude-code@${CLAUDE_CODE_VERSION}" ;; \
         opencode) npm install -g "opencode-ai@${OPENCODE_VERSION}" ;; \
         codex) npm install -g "@openai/codex@${CODEX_VERSION}" ;; \
         *) echo "unsupported provider: $PROVIDER" >&2; exit 1 ;; \
       esac \
    && case "$PROVIDER" in \
         claude-code) claude --version ;; \
         opencode) opencode --version ;; \
         codex) codex --version ;; \
       esac

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
