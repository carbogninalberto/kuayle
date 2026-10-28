#!/bin/sh
# Installs the Docker CLI, which talks to the machine's own Docker daemon (see
# devmachine/docker); keep it on the daemon image's Docker version. The
# download is checked against its SHA-256 before it is unpacked or installed.
# The Compose and Buildx plugins are built from source by
# build-docker-plugins.sh in an image stage and copied into
# $prefix/lib/docker/cli-plugins before this runs.
set -eu

DOCKER_VERSION=29.9.2
DOCKER_X86_64_SHA256=9ac225999fd5e863f7694c728ef23d480616b1e5639328fa309befd4243d69eb
DOCKER_AARCH64_SHA256=873b3e0207857c3b2f28392fb922f3ea6efe37620c16d4d4aaf2fef409c90b73

arch=${1:-}
case "$arch" in
  amd64) docker_arch=x86_64; docker_sha=$DOCKER_X86_64_SHA256 ;;
  arm64) docker_arch=aarch64; docker_sha=$DOCKER_AARCH64_SHA256 ;;
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

mkdir -p "$prefix/bin"
install -m 0755 "$work/docker/docker" "$prefix/bin/docker"
chmod 0755 "$plugins/docker-compose" "$plugins/docker-buildx"

"$prefix/bin/docker" --version
"$prefix/bin/docker" compose version
"$prefix/bin/docker" buildx version
