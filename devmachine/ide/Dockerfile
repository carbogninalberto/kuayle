FROM codercom/code-server:4.104.2

ARG TARGETARCH
ARG NODE_VERSION=22.17.0
ARG TTYD_VERSION=1.7.7
ARG CLAUDE_CODE_VERSION=2.1.14
ARG OPENCODE_VERSION=1.1.25
ARG CODEX_VERSION=0.80.0

USER root
COPY common/kuayle-service-entrypoint /usr/local/bin/kuayle-service-entrypoint
COPY ide/start.sh /usr/local/bin/kuayle-ide-start
COPY ide/shell.sh /usr/local/bin/kuayle-shell
COPY ide/checkout.sh /usr/local/bin/kuayle-checkout
COPY ide/terminal-session.sh /usr/local/bin/kuayle-terminal-session
COPY ide/command-event.js /usr/local/lib/kuayle-command-event.js
COPY ide/tmux.conf /etc/tmux.conf
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl git ripgrep sudo tmux xz-utils \
    && rm -rf /var/lib/apt/lists/* \
    && case "${TARGETARCH}" in amd64) node_arch=x64; ttyd_arch=x86_64 ;; arm64) node_arch=arm64; ttyd_arch=aarch64 ;; *) echo "unsupported architecture: ${TARGETARCH}" >&2; exit 1 ;; esac \
    && curl -fsSL "https://nodejs.org/dist/v${NODE_VERSION}/node-v${NODE_VERSION}-linux-${node_arch}.tar.xz" -o /tmp/node.tar.xz \
    && tar -xJf /tmp/node.tar.xz -C /usr/local --strip-components=1 \
    && curl -fsSL "https://github.com/tsl0922/ttyd/releases/download/${TTYD_VERSION}/ttyd.${ttyd_arch}" -o /usr/local/bin/ttyd \
    && chmod 0755 /usr/local/bin/ttyd \
    && npm install -g "@anthropic-ai/claude-code@${CLAUDE_CODE_VERSION}" "opencode-ai@${OPENCODE_VERSION}" "@openai/codex@${CODEX_VERSION}" \
    && rm -f /tmp/node.tar.xz \
    && chmod 0755 /usr/local/bin/kuayle-service-entrypoint /usr/local/bin/kuayle-ide-start /usr/local/bin/kuayle-shell /usr/local/bin/kuayle-checkout /usr/local/bin/kuayle-terminal-session \
    && opencode --version \
    && claude --version \
    && codex --version \
    && ttyd --version \
    && user_name=$(getent passwd 1000 | cut -d: -f1) \
    && printf '%s ALL=(ALL) NOPASSWD:ALL\n' "$user_name" > /etc/sudoers.d/kuayle-builder \
    && chmod 0440 /etc/sudoers.d/kuayle-builder

USER 1000:1000
ENV HOME=/home/kuayle
EXPOSE 8080 7681
CMD ["/usr/local/bin/kuayle-ide-start"]
