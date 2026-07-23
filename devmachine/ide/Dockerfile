FROM node:22.23.1-bookworm-slim@sha256:6c74791e557ce11fc957704f6d4fe134a7bc8d6f5ca4403205b2966bd488f6b3 AS node-stage

FROM ubuntu:26.04

ARG TARGETARCH
ARG DEBIAN_FRONTEND=noninteractive
ARG CODE_SERVER_VERSION=4.129.0
ARG CODE_SERVER_AMD64_SHA256=66160d99431595b0e9f4245abae8d0f472289fb77c97007ce79f2d64d06507f8
ARG CODE_SERVER_ARM64_SHA256=e504da34067eacc04e03265eccf90e5d2c3fdc3f06c91c2028a5a80f1d05a5b8
ARG TTYD_VERSION=1.7.7
ARG TTYD_AMD64_SHA256=8a217c968aba172e0dbf3f34447218dc015bc4d5e59bf51db2f2cd12b7be4f55
ARG TTYD_ARM64_SHA256=b38acadd89d1d396a0f5649aa52c539edbad07f4bc7348b27b4f4b7219dd4165
ARG NPM_VERSION=12.0.1
ARG CLAUDE_CODE_VERSION=2.1.218
ARG OPENCODE_VERSION=1.18.4
ARG CODEX_VERSION=0.145.0

COPY --from=node-stage /usr/local /usr/local
COPY common/kuayle-service-entrypoint /usr/local/bin/kuayle-service-entrypoint
COPY ide/start.sh /usr/local/bin/kuayle-ide-start
COPY ide/shell.sh /usr/local/bin/kuayle-shell
COPY ide/checkout.sh /usr/local/bin/kuayle-checkout
COPY ide/terminal-session.sh /usr/local/bin/kuayle-terminal-session
COPY ide/command-event.js /usr/local/lib/kuayle-command-event.js
COPY ide/tmux.conf /etc/tmux.conf
# VS Code's npm support extension is not the npm CLI package; keep its manifest identity unambiguous.
RUN rm -f /usr/bin/pebble \
    && apt-get update \
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
    && npm install --prefix /tmp/code-server-patches --ignore-scripts --package-lock=false \
         "js-yaml@4.3.0" "shell-quote@1.9.0" "tar@7.5.19" \
    && rm -rf /usr/lib/code-server/node_modules/js-yaml \
         /usr/lib/code-server/lib/vscode/node_modules/shell-quote \
         /usr/lib/code-server/lib/vscode/node_modules/tar \
    && cp -a /tmp/code-server-patches/node_modules/js-yaml /usr/lib/code-server/node_modules/ \
    && cp -a /tmp/code-server-patches/node_modules/shell-quote /usr/lib/code-server/lib/vscode/node_modules/ \
    && cp -a /tmp/code-server-patches/node_modules/tar /usr/lib/code-server/lib/vscode/node_modules/ \
    && node -e 'const fs=require("fs"),path="/usr/lib/code-server/lib/vscode/extensions/npm/package.json",manifest=JSON.parse(fs.readFileSync(path));manifest.name="npm-support";fs.writeFileSync(path,JSON.stringify(manifest,null,2)+"\n")' \
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
