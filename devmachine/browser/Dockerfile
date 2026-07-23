FROM golang:1.25.12-alpine AS cdp-proxy-build
WORKDIR /src
COPY browser/cdp-proxy/main.go .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /cdp-proxy main.go

FROM ubuntu:26.04

ARG TARGETARCH
ARG DEBIAN_FRONTEND=noninteractive
ARG KASMVNC_VERSION=1.4.0
ARG CHROME_VERSION=150.0.7871.181-1
ARG CHROME_AMD64_SHA256=fec50905f7b1235a440977a833476e0162874f5ca79e506cdf40b71af64d92f4
ARG CHROME_ARM64_SHA256=bf6efc4587ecce26ef579dfdc99364da5f3da4a45f2a6b368f075b6bedc92f7d

RUN rm -f /usr/bin/pebble \
    && apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl openbox x11-utils \
    && case "$TARGETARCH" in \
         amd64) kasm_arch=amd64; kasm_sha=12bac6014149c5fdee75f0d403785aaa3e5dd4ea222de73253a5d4181bc9567e; chrome_arch=amd64; chrome_sha="$CHROME_AMD64_SHA256" ;; \
         arm64) kasm_arch=arm64; kasm_sha=120d9462cb5e917cad91a23f6cb0b780c06f701def40e900b29f996979200638; chrome_arch=arm64; chrome_sha="$CHROME_ARM64_SHA256" ;; \
         *) echo "Unsupported architecture: $TARGETARCH" >&2; exit 1 ;; \
       esac \
    && curl -fsSL -o /tmp/kasmvnc.deb "https://github.com/kasmtech/KasmVNC/releases/download/v${KASMVNC_VERSION}/kasmvncserver_noble_${KASMVNC_VERSION}_${kasm_arch}.deb" \
    && echo "${kasm_sha}  /tmp/kasmvnc.deb" | sha256sum -c - \
    && apt-get install -y --no-install-recommends /tmp/kasmvnc.deb \
    && curl -fsSL -o /tmp/chrome.deb "https://dl.google.com/linux/chrome/deb/pool/main/g/google-chrome-stable/google-chrome-stable_${CHROME_VERSION}_${chrome_arch}.deb" \
    && echo "${chrome_sha}  /tmp/chrome.deb" | sha256sum -c - \
    && apt-get install -y --no-install-recommends /tmp/chrome.deb \
    && rm -f /tmp/kasmvnc.deb /tmp/chrome.deb /usr/bin/pebble \
        /etc/ssl/private/ssl-cert-snakeoil.key /etc/ssl/certs/ssl-cert-snakeoil.pem \
    && rm -rf /var/lib/apt/lists/* \
    && mkdir -p /home/kuayle \
    && chown 1000:1000 /home/kuayle
COPY common/kuayle-service-entrypoint /usr/local/bin/kuayle-service-entrypoint
COPY --from=cdp-proxy-build /cdp-proxy /usr/local/bin/kuayle-browser-cdp-proxy
COPY browser/start.sh /usr/local/bin/kuayle-browser-start
COPY browser/healthcheck.sh /usr/local/bin/kuayle-browser-healthcheck
RUN chmod 0755 /usr/local/bin/kuayle-service-entrypoint /usr/local/bin/kuayle-browser-cdp-proxy /usr/local/bin/kuayle-browser-start /usr/local/bin/kuayle-browser-healthcheck

USER 1000:1000
ENV HOME=/home/kuayle DISPLAY=:1
EXPOSE 3000 9222
CMD ["/usr/local/bin/kuayle-browser-start"]
