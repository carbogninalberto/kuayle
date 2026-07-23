FROM golang:1.25.12-alpine AS cdp-proxy-build
WORKDIR /src
COPY browser/cdp-proxy/main.go .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /cdp-proxy main.go

FROM debian:bookworm-slim

ARG TARGETARCH
ARG KASMVNC_VERSION=1.4.0
ARG CHROME_VERSION=150.0.7871.181-1
ARG CHROME_AMD64_SHA256=fec50905f7b1235a440977a833476e0162874f5ca79e506cdf40b71af64d92f4
ARG CHROME_ARM64_SHA256=bf6efc4587ecce26ef579dfdc99364da5f3da4a45f2a6b368f075b6bedc92f7d

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl openbox x11-utils \
    && case "$TARGETARCH" in \
         amd64) kasm_arch=amd64; kasm_sha=a059b9db8d93a7d8bb753e9cf2b119b132c8cf0d832b549a1287b81be68e956a; chrome_arch=amd64; chrome_sha="$CHROME_AMD64_SHA256" ;; \
         arm64) kasm_arch=arm64; kasm_sha=8e2e7abdde000587fc1d07437db2c71339134b351a166bbeaf12aab59f910dae; chrome_arch=arm64; chrome_sha="$CHROME_ARM64_SHA256" ;; \
         *) echo "Unsupported architecture: $TARGETARCH" >&2; exit 1 ;; \
       esac \
    && curl -fsSL -o /tmp/kasmvnc.deb "https://github.com/kasmtech/KasmVNC/releases/download/v${KASMVNC_VERSION}/kasmvncserver_bookworm_${KASMVNC_VERSION}_${kasm_arch}.deb" \
    && echo "${kasm_sha}  /tmp/kasmvnc.deb" | sha256sum -c - \
    && apt-get install -y --no-install-recommends /tmp/kasmvnc.deb \
    && curl -fsSL -o /tmp/chrome.deb "https://dl.google.com/linux/chrome/deb/pool/main/g/google-chrome-stable/google-chrome-stable_${CHROME_VERSION}_${chrome_arch}.deb" \
    && echo "${chrome_sha}  /tmp/chrome.deb" | sha256sum -c - \
    && apt-get install -y --no-install-recommends /tmp/chrome.deb \
    && rm -f /tmp/kasmvnc.deb /tmp/chrome.deb \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --uid 1000 --create-home kuayle
COPY common/kuayle-service-entrypoint /usr/local/bin/kuayle-service-entrypoint
COPY --from=cdp-proxy-build /cdp-proxy /usr/local/bin/kuayle-browser-cdp-proxy
COPY browser/start.sh /usr/local/bin/kuayle-browser-start
COPY browser/healthcheck.sh /usr/local/bin/kuayle-browser-healthcheck
RUN chmod 0755 /usr/local/bin/kuayle-service-entrypoint /usr/local/bin/kuayle-browser-cdp-proxy /usr/local/bin/kuayle-browser-start /usr/local/bin/kuayle-browser-healthcheck

USER 1000:1000
ENV HOME=/home/kuayle DISPLAY=:1
EXPOSE 3000 9222
CMD ["/usr/local/bin/kuayle-browser-start"]
