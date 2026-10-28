# The per-machine Docker daemon: rootless dockerd from the official image,
# pinned by digest, with Kuayle's start script and health check. It runs as
# UID 1000 without privileged mode; see TECHNICAL.md "Machine Docker daemon"
# for the confinement it needs. The Docker CLI pins in
# common/install-docker-cli.sh follow this image's Docker version.
# runc and the containerd runc shim are rebuilt from the same release commits
# the official image ships, with golang.org/x/net raised to a version without
# the HIGH advisories the upstream binaries still carry (same approach as the
# Caddy builds). Build flags follow the upstream release builds.
FROM golang:1.26.10-alpine AS runtime-builder
RUN apk add --no-cache git gcc musl-dev libseccomp-dev libseccomp-static linux-headers pkgconf
ARG RUNC_VERSION=1.5.2
ARG RUNC_COMMIT=29dd3dc2b13b4123162e5fe132504bb4b15569f1
ARG CONTAINERD_VERSION=2.4.2
ARG CONTAINERD_COMMIT=b7a25261e029d68978bbf290df0a62fc819b3c51
ARG XNET_VERSION=v0.60.0
WORKDIR /src
RUN git init -q runc && cd runc \
    && git fetch -q --depth 1 https://github.com/opencontainers/runc "$RUNC_COMMIT" && git checkout -q FETCH_HEAD \
    && go mod edit -replace "golang.org/x/net=golang.org/x/net@$XNET_VERSION" && go mod tidy && go mod vendor \
    && CGO_ENABLED=1 go build -trimpath -buildvcs=false -mod=vendor -tags "seccomp urfave_cli_no_docs netgo osusergo" \
       -ldflags "-X main.version=$RUNC_VERSION -X main.gitCommit=$RUNC_COMMIT -extldflags -static -s -w" -o /out/runc .
RUN git init -q containerd && cd containerd \
    && git fetch -q --depth 1 https://github.com/containerd/containerd "$CONTAINERD_COMMIT" && git checkout -q FETCH_HEAD \
    && go mod edit -replace "golang.org/x/net=golang.org/x/net@$XNET_VERSION" && go mod tidy && go mod vendor \
    && CGO_ENABLED=0 go build -trimpath -buildvcs=false -mod=vendor \
       -ldflags "-X github.com/containerd/containerd/v2/version.Version=v$CONTAINERD_VERSION -X github.com/containerd/containerd/v2/version.Revision=$CONTAINERD_COMMIT -X github.com/containerd/containerd/v2/version.Package=github.com/containerd/containerd/v2 -s -w" \
       -o /out/containerd-shim-runc-v2 ./cmd/containerd-shim-runc-v2

FROM docker:29.9.2-dind-rootless@sha256:ae083cc7de51e7a8e5a597f8fb774ca7914c408a95fc0f1477826a913479518a

USER 0:0
COPY docker/kuayle-docker-start docker/kuayle-docker-healthcheck /usr/local/bin/
COPY --from=runtime-builder /out/runc /out/containerd-shim-runc-v2 /usr/local/bin/
# Clients (CLI, Compose, Buildx) run in the developer container, so the
# daemon image drops the bundled client plugins and ctr, and takes Alpine
# security updates.
RUN apk upgrade --no-cache \
    && rm -f /usr/local/bin/ctr /usr/local/libexec/docker/cli-plugins/docker-buildx /usr/local/libexec/docker/cli-plugins/docker-compose \
    && chmod 0755 /usr/local/bin/kuayle-docker-start /usr/local/bin/kuayle-docker-healthcheck \
    && dockerd --version \
    && runc --version \
    && containerd-shim-runc-v2 -v \
    && rootlesskit --version
USER 1000:1000
ENV DOCKER_TLS_CERTDIR=/certs
ENTRYPOINT ["/usr/local/bin/kuayle-docker-start"]
