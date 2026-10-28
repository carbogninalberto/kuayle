FROM golang:1.26.10-alpine AS go-stage

FROM node:22.23.3-bookworm-slim@sha256:43ac6c60b8f89723f746e8a92ce91abd5017e627ce1ddfe4238355d3a30b772c AS node-stage

FROM golang:1.27.3-bookworm AS crush-build
# Release binaries embed an older Go standard library with known CVEs; build with patched Go.
ARG CRUSH_VERSION=0.98.2
RUN CGO_ENABLED=0 GOBIN=/out go install -trimpath -ldflags="-s -w" "github.com/charmbracelet/crush@v${CRUSH_VERSION}"

FROM ubuntu:26.04@sha256:f144425ff09be612d6d9ad965196e9cdc23dae1f42110a8a11a3e9a8198759f7

ARG TARGETARCH
ARG DEBIAN_FRONTEND=noninteractive
ARG TTYD_VERSION=1.7.7
ARG TTYD_AMD64_SHA256=8a217c968aba172e0dbf3f34447218dc015bc4d5e59bf51db2f2cd12b7be4f55
ARG TTYD_ARM64_SHA256=b38acadd89d1d396a0f5649aa52c539edbad07f4bc7348b27b4f4b7219dd4165
ARG NPM_VERSION=12.2.0

COPY --from=node-stage /usr/local /usr/local
COPY --from=crush-build /out/crush /usr/local/bin/crush
COPY --from=go-stage /usr/local/go /usr/local/go
COPY common/install-agent-clis.sh common/install-docker-cli.sh common/kuayle-ports common/kuayle-service-entrypoint common/kuayle-agent-runner /usr/local/bin/
COPY common/start-common.sh common/codex-model-catalog.cjs common/patch-npm-security.cjs common/test-http-cache-semantics.cjs common/kuayle-qa.md common/kuayle-agents.md /usr/local/lib/kuayle/
COPY headless/kuayle-headless-start /usr/local/bin/kuayle-headless-start
COPY ide/shell.sh /usr/local/bin/kuayle-shell
COPY ide/checkout.sh /usr/local/bin/kuayle-checkout
COPY ide/terminal-session.sh /usr/local/bin/kuayle-terminal-session
COPY ide/command-event.js /usr/local/lib/kuayle-command-event.js
COPY ide/tmux.conf /etc/tmux.conf

ARG SECURITY_REFRESH=initial
RUN rm -f /usr/bin/pebble \
    && apt-get update \
    && apt-get upgrade -y \
    && apt-get install -y --no-install-recommends \
         ca-certificates curl git make ncurses-term python3 ripgrep tmux xz-utils \
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
         /usr/local/bin/kuayle-checkout \
         /usr/local/bin/kuayle-terminal-session \
         /usr/local/bin/install-agent-clis.sh \
    && ttyd --version \
    && /usr/local/go/bin/go version

USER 1000:1000
ENV HOME=/home/kuayle \
    LANG=C.UTF-8 \
    SHELL=/usr/local/bin/kuayle-shell \
    COLORTERM=truecolor \
    GOPATH=/home/kuayle/go \
    GOCACHE=/home/kuayle/.cache/go-build \
    GOMODCACHE=/home/kuayle/go/pkg/mod \
    PATH="/usr/local/go/bin:/usr/local/bin:${PATH}"
WORKDIR /workspace
EXPOSE 7681
CMD ["/usr/local/bin/kuayle-headless-start"]
