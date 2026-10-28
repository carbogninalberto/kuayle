FROM node:22.23.3-bookworm-slim@sha256:43ac6c60b8f89723f746e8a92ce91abd5017e627ce1ddfe4238355d3a30b772c AS node-stage

FROM golang:1.27.3-bookworm AS crush-build
# Release binaries embed an older Go standard library with known CVEs; build with patched Go.
ARG CRUSH_VERSION=0.98.2
RUN CGO_ENABLED=0 GOBIN=/out go install -trimpath -ldflags="-s -w" "github.com/charmbracelet/crush@v${CRUSH_VERSION}"

FROM ubuntu:26.04@sha256:f144425ff09be612d6d9ad965196e9cdc23dae1f42110a8a11a3e9a8198759f7

ARG TARGETARCH
ARG DEBIAN_FRONTEND=noninteractive
ARG CODE_SERVER_VERSION=4.143.1
ARG CODE_SERVER_AMD64_SHA256=e4b771c98fafbe2af3990ecd71d93ba22515588db550007076768a014ae34c02
ARG CODE_SERVER_ARM64_SHA256=79ca8df2db219b24de75e2a830c5302808124d8c2bf132f4bc6105cea834bb1c
ARG TTYD_VERSION=1.7.7
ARG TTYD_AMD64_SHA256=8a217c968aba172e0dbf3f34447218dc015bc4d5e59bf51db2f2cd12b7be4f55
ARG TTYD_ARM64_SHA256=b38acadd89d1d396a0f5649aa52c539edbad07f4bc7348b27b4f4b7219dd4165
ARG NPM_VERSION=12.2.0

COPY --from=node-stage /usr/local /usr/local
COPY --from=crush-build /out/crush /usr/local/bin/crush
COPY common/install-agent-clis.sh common/install-docker-cli.sh common/kuayle-ports common/kuayle-service-entrypoint /usr/local/bin/
COPY common/start-common.sh common/codex-model-catalog.cjs common/patch-npm-security.cjs common/test-http-cache-semantics.cjs common/kuayle-qa.md common/kuayle-agents.md /usr/local/lib/kuayle/
COPY ide/patch-security.cjs ide/test-zod-security.cjs ide/test-body-parser-security.cjs /usr/local/lib/kuayle/
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
    && apt-get install -y --no-install-recommends ca-certificates curl git ncurses-term python3 ripgrep sudo tmux xz-utils \
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
    && sh /usr/local/bin/install-agent-clis.sh "$TARGETARCH" \
    && sh /usr/local/bin/install-docker-cli.sh "$TARGETARCH" \
    && node /usr/local/lib/kuayle/patch-npm-security.cjs /usr/lib/code-server \
    && npm install --prefix /tmp/code-server-patches --ignore-scripts --package-lock=false \
         "basic-ftp@6.2.2" "js-yaml@5.4.2" "shell-quote@1.12.0" "undici@7.30.1" "ip-address@10.7.3" \
         "compression@1.8.2" "destroy@1.2.0" "follow-redirects@1.16.1" \
    && rm -rf /usr/lib/code-server/node_modules/js-yaml \
         /usr/lib/code-server/node_modules/basic-ftp \
         /usr/lib/code-server/node_modules/compression \
         /usr/lib/code-server/node_modules/destroy \
         /usr/lib/code-server/node_modules/follow-redirects \
         /usr/lib/code-server/lib/vscode/node_modules/shell-quote \
         /usr/lib/code-server/lib/vscode/node_modules/undici \
         /usr/lib/code-server/lib/vscode/node_modules/ip-address \
         /usr/lib/code-server/node_modules/ip-address \
    && cp -a /tmp/code-server-patches/node_modules/basic-ftp /usr/lib/code-server/node_modules/ \
    && cp -a /tmp/code-server-patches/node_modules/compression /usr/lib/code-server/node_modules/ \
    && cp -a /tmp/code-server-patches/node_modules/destroy /usr/lib/code-server/node_modules/ \
    && cp -a /tmp/code-server-patches/node_modules/follow-redirects /usr/lib/code-server/node_modules/ \
    && cp -a /tmp/code-server-patches/node_modules/js-yaml /usr/lib/code-server/node_modules/ \
    && cp -a /tmp/code-server-patches/node_modules/shell-quote /usr/lib/code-server/lib/vscode/node_modules/ \
    && cp -a /tmp/code-server-patches/node_modules/undici /usr/lib/code-server/lib/vscode/node_modules/ \
    && cp -a /tmp/code-server-patches/node_modules/ip-address /usr/lib/code-server/lib/vscode/node_modules/ \
    && cp -a /tmp/code-server-patches/node_modules/ip-address /usr/lib/code-server/node_modules/ \
    && node /usr/local/lib/kuayle/patch-security.cjs /usr/lib/code-server \
    && rm -rf /root/.npm /tmp/code-server-patches /var/lib/apt/lists/* \
    && rm -f /tmp/code-server.deb /usr/bin/pebble \
         /etc/ssl/private/ssl-cert-snakeoil.key /etc/ssl/certs/ssl-cert-snakeoil.pem \
    && mkdir -p /home/kuayle \
    && chown 1000:1000 /home/kuayle \
    && chmod 0755 /usr/local/bin/kuayle-ports /usr/local/bin/kuayle-service-entrypoint /usr/local/bin/install-agent-clis.sh /usr/local/bin/kuayle-ide-start /usr/local/bin/kuayle-shell /usr/local/bin/kuayle-checkout /usr/local/bin/kuayle-terminal-session \
    && code-server --version \
    && node --version \
    && npm --version \
    && opencode --version \
    && claude --version \
    && codex --version \
    && qwen --version \
    && crush --version \
    && goose --version \
    && kimi --version \
    && ttyd --version \
    && rm -rf /root/.config/code-server \
    && user_name=$(getent passwd 1000 | cut -d: -f1) \
    && printf '%s ALL=(ALL) NOPASSWD:ALL\n' "$user_name" > /etc/sudoers.d/kuayle-builder \
    && chmod 0440 /etc/sudoers.d/kuayle-builder

USER 1000:1000
ENV HOME=/home/kuayle \
    LANG=C.UTF-8
EXPOSE 8080 7681
CMD ["/usr/local/bin/kuayle-ide-start"]
