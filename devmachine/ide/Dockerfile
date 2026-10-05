FROM node:22.23.3-bookworm-slim@sha256:43ac6c60b8f89723f746e8a92ce91abd5017e627ce1ddfe4238355d3a30b772c AS node-stage

FROM ubuntu:26.04@sha256:f144425ff09be612d6d9ad965196e9cdc23dae1f42110a8a11a3e9a8198759f7

ARG TARGETARCH
ARG DEBIAN_FRONTEND=noninteractive
ARG CODE_SERVER_VERSION=4.140.0
ARG CODE_SERVER_AMD64_SHA256=281742720604c6654147186d54d1712f833ed7e41a734cc501ca00d4f20301aa
ARG CODE_SERVER_ARM64_SHA256=455dece4da82a6f43f455879992d11769137114dbd340611fdea2299cf8566fd
ARG TTYD_VERSION=1.7.7
ARG TTYD_AMD64_SHA256=8a217c968aba172e0dbf3f34447218dc015bc4d5e59bf51db2f2cd12b7be4f55
ARG TTYD_ARM64_SHA256=b38acadd89d1d396a0f5649aa52c539edbad07f4bc7348b27b4f4b7219dd4165
ARG NPM_VERSION=12.2.0
ARG CLAUDE_CODE_VERSION=2.1.289
ARG OPENCODE_VERSION=1.18.34
ARG CODEX_VERSION=0.160.0

COPY --from=node-stage /usr/local /usr/local
COPY common/kuayle-service-entrypoint /usr/local/bin/kuayle-service-entrypoint
COPY common/patch-npm-security.cjs common/test-http-cache-semantics.cjs /usr/local/lib/kuayle/
COPY ide/start.sh /usr/local/bin/kuayle-ide-start
COPY ide/shell.sh /usr/local/bin/kuayle-shell
COPY ide/checkout.sh /usr/local/bin/kuayle-checkout
COPY ide/terminal-session.sh /usr/local/bin/kuayle-terminal-session
COPY ide/command-event.js /usr/local/lib/kuayle-command-event.js
COPY ide/tmux.conf /etc/tmux.conf
ARG SECURITY_REFRESH=initial
RUN rm -f /usr/bin/pebble \
    && apt-get update \
    && apt-get upgrade -y \
    && apt-get install -y --no-install-recommends ca-certificates curl git ripgrep sudo tmux xz-utils \
    && case "${TARGETARCH}" in \
         amd64) package_arch=amd64; code_server_sha="$CODE_SERVER_AMD64_SHA256"; ttyd_arch=x86_64; ttyd_sha="$TTYD_AMD64_SHA256" ;; \
         arm64) package_arch=arm64; code_server_sha="$CODE_SERVER_ARM64_SHA256"; ttyd_arch=aarch64; ttyd_sha="$TTYD_ARM64_SHA256" ;; \
         *) echo "unsupported architecture: ${TARGETARCH}" >&2; exit 1 ;; \
       esac \
    && curl -fsSL "https://github.com/coder/code-server/releases/download/v${CODE_SERVER_VERSION}/code-server_${CODE_SERVER_VERSION}_${package_arch}.deb" -o /tmp/code-server.deb \
    && echo "${code_server_sha}  /tmp/code-server.deb" | sha256sum -c - \
    && apt-get install -y --no-install-recommends /tmp/code-server.deb \
    && curl -fsSL "https://github.com/tsl0922/ttyd/releases/download/${TTYD_VERSION}/ttyd.${ttyd_arch}" -o /usr/local/bin/ttyd \
    && echo "${ttyd_sha}  /usr/local/bin/ttyd" | sha256sum -c - \
    && chmod 0755 /usr/local/bin/ttyd \
    && npm install -g "npm@${NPM_VERSION}" \
    && npm install -g --allow-scripts=@anthropic-ai/claude-code "@anthropic-ai/claude-code@${CLAUDE_CODE_VERSION}" \
    && npm install -g --allow-scripts=opencode-ai "opencode-ai@${OPENCODE_VERSION}" \
    && npm install -g "@openai/codex@${CODEX_VERSION}" \
    && node /usr/local/lib/kuayle/patch-npm-security.cjs /usr/lib/code-server \
    && npm install --prefix /tmp/code-server-patches --ignore-scripts --package-lock=false \
         "basic-ftp@6.2.2" "js-yaml@5.4.2" "shell-quote@1.12.0" "undici@7.30.0" "ip-address@10.7.3" \
    && rm -rf /usr/lib/code-server/node_modules/js-yaml \
         /usr/lib/code-server/node_modules/basic-ftp \
         /usr/lib/code-server/lib/vscode/node_modules/shell-quote \
         /usr/lib/code-server/lib/vscode/node_modules/undici \
         /usr/lib/code-server/lib/vscode/node_modules/ip-address \
         /usr/lib/code-server/node_modules/ip-address \
    && cp -a /tmp/code-server-patches/node_modules/basic-ftp /usr/lib/code-server/node_modules/ \
    && cp -a /tmp/code-server-patches/node_modules/js-yaml /usr/lib/code-server/node_modules/ \
    && cp -a /tmp/code-server-patches/node_modules/shell-quote /usr/lib/code-server/lib/vscode/node_modules/ \
    && cp -a /tmp/code-server-patches/node_modules/undici /usr/lib/code-server/lib/vscode/node_modules/ \
    && cp -a /tmp/code-server-patches/node_modules/ip-address /usr/lib/code-server/lib/vscode/node_modules/ \
    && cp -a /tmp/code-server-patches/node_modules/ip-address /usr/lib/code-server/node_modules/ \
    && rm -rf /root/.npm /tmp/code-server-patches /var/lib/apt/lists/* \
    && rm -f /tmp/code-server.deb /usr/bin/pebble \
         /etc/ssl/private/ssl-cert-snakeoil.key /etc/ssl/certs/ssl-cert-snakeoil.pem \
    && mkdir -p /home/kuayle \
    && chown 1000:1000 /home/kuayle \
    && chmod 0755 /usr/local/bin/kuayle-service-entrypoint /usr/local/bin/kuayle-ide-start /usr/local/bin/kuayle-shell /usr/local/bin/kuayle-checkout /usr/local/bin/kuayle-terminal-session \
    && code-server --version \
    && node --version \
    && npm --version \
    && opencode --version \
    && claude --version \
    && codex --version \
    && ttyd --version \
    && rm -rf /root/.config/code-server \
    && user_name=$(getent passwd 1000 | cut -d: -f1) \
    && printf '%s ALL=(ALL) NOPASSWD:ALL\n' "$user_name" > /etc/sudoers.d/kuayle-builder \
    && chmod 0440 /etc/sudoers.d/kuayle-builder

USER 1000:1000
ENV HOME=/home/kuayle
EXPOSE 8080 7681
CMD ["/usr/local/bin/kuayle-ide-start"]
