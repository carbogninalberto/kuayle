FROM debian:bookworm-slim

ARG TARGETARCH
ARG KASMVNC_VERSION=1.4.0
ARG CHROME_AMD64_SHA256=83ed59c85878ebb8fa53915ebe7066cafc58d1c04c1c95449486e6f9d99a1efb

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl openbox socat x11-utils \
    && case "$TARGETARCH" in \
         amd64) kasm_arch=amd64; kasm_sha=a059b9db8d93a7d8bb753e9cf2b119b132c8cf0d832b549a1287b81be68e956a ;; \
         arm64) kasm_arch=arm64; kasm_sha=8e2e7abdde000587fc1d07437db2c71339134b351a166bbeaf12aab59f910dae ;; \
         *) echo "Unsupported architecture: $TARGETARCH" >&2; exit 1 ;; \
       esac \
    && curl -fsSL -o /tmp/kasmvnc.deb "https://github.com/kasmtech/KasmVNC/releases/download/v${KASMVNC_VERSION}/kasmvncserver_bookworm_${KASMVNC_VERSION}_${kasm_arch}.deb" \
    && echo "${kasm_sha}  /tmp/kasmvnc.deb" | sha256sum -c - \
    && apt-get install -y --no-install-recommends /tmp/kasmvnc.deb \
    && if [ "$TARGETARCH" = amd64 ]; then \
         curl -fsSL -o /tmp/chrome.deb https://dl.google.com/linux/direct/google-chrome-stable_current_amd64.deb \
         && echo "${CHROME_AMD64_SHA256}  /tmp/chrome.deb" | sha256sum -c - \
         && apt-get install -y --no-install-recommends /tmp/chrome.deb; \
       else \
         apt-get install -y --no-install-recommends chromium; \
       fi \
    && rm -f /tmp/kasmvnc.deb /tmp/chrome.deb \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --uid 1000 --create-home kuayle
COPY common/kuayle-service-entrypoint /usr/local/bin/kuayle-service-entrypoint
COPY browser/start.sh /usr/local/bin/kuayle-browser-start
COPY browser/healthcheck.sh /usr/local/bin/kuayle-browser-healthcheck
RUN chmod 0755 /usr/local/bin/kuayle-service-entrypoint /usr/local/bin/kuayle-browser-start /usr/local/bin/kuayle-browser-healthcheck

USER 1000:1000
ENV HOME=/home/kuayle DISPLAY=:1
EXPOSE 3000 9222
CMD ["/usr/local/bin/kuayle-browser-start"]
