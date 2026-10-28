#!/bin/sh
# Installs the Docker CLI with the Compose and Buildx plugins, which talk to the
# machine's own Docker daemon (see devmachine/docker). This is the single pin
# list; keep the CLI on the daemon image's Docker version. Every download is
# checked against its SHA-256 before it is unpacked or installed.
set -eu

DOCKER_VERSION=29.9.1
DOCKER_X86_64_SHA256=c158ecc55adb3afb01bbd3539ed3b997d20c26dbdc418080506cc1bf5243ff57
DOCKER_AARCH64_SHA256=3e93cc6d1a686456a3b18eefdde26467a837a7a2b257af22e27cba1dbc1b2018
COMPOSE_VERSION=5.6.0
COMPOSE_X86_64_SHA256=40343e21ca777173e69cff5dbafeb37c6f81f3b0d57d9e597f036e95eb63e76a
COMPOSE_AARCH64_SHA256=733ec76717ceb59052a9609b9dadfb523b2df8eab57a54212872d10a58078ea2
BUILDX_VERSION=0.38.0
BUILDX_AMD64_SHA256=4fe4cc38adf48169132749b6ca22a990928db0118e3407584ee553723115d287
BUILDX_ARM64_SHA256=38e890e1a162bfdbf32fbe91404991dc4fc28e55b0de901b34374a061bab4d2c

arch=${1:-}
case "$arch" in
  amd64)
    docker_arch=x86_64; docker_sha=$DOCKER_X86_64_SHA256
    compose_arch=x86_64; compose_sha=$COMPOSE_X86_64_SHA256
    buildx_arch=amd64; buildx_sha=$BUILDX_AMD64_SHA256 ;;
  arm64)
    docker_arch=aarch64; docker_sha=$DOCKER_AARCH64_SHA256
    compose_arch=aarch64; compose_sha=$COMPOSE_AARCH64_SHA256
    buildx_arch=arm64; buildx_sha=$BUILDX_ARM64_SHA256 ;;
  *) echo "unsupported architecture: ${arch}" >&2; exit 1 ;;
esac

prefix=${KUAYLE_DOCKER_CLI_PREFIX:-/usr/local}
plugins="$prefix/lib/docker/cli-plugins"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT INT TERM

download() {
  curl -fsSL --proto '=https' --tlsv1.2 "$1" -o "$2"
  echo "$3  $2" | sha256sum -c -
}

download "https://download.docker.com/linux/static/stable/${docker_arch}/docker-${DOCKER_VERSION}.tgz" "$work/docker.tgz" "$docker_sha"
tar -xzf "$work/docker.tgz" -C "$work" docker/docker
download "https://github.com/docker/compose/releases/download/v${COMPOSE_VERSION}/docker-compose-linux-${compose_arch}" "$work/docker-compose" "$compose_sha"
download "https://github.com/docker/buildx/releases/download/v${BUILDX_VERSION}/buildx-v${BUILDX_VERSION}.linux-${buildx_arch}" "$work/docker-buildx" "$buildx_sha"

mkdir -p "$prefix/bin" "$plugins"
install -m 0755 "$work/docker/docker" "$prefix/bin/docker"
install -m 0755 "$work/docker-compose" "$plugins/docker-compose"
install -m 0755 "$work/docker-buildx" "$plugins/docker-buildx"

"$prefix/bin/docker" --version
"$prefix/bin/docker" compose version
"$prefix/bin/docker" buildx version
