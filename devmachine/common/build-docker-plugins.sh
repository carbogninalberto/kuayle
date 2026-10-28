#!/bin/sh
# Builds the Docker Compose and Buildx CLI plugins from their pinned release
# commits, with dependencies raised past published HIGH advisories that the
# upstream release binaries still carry (the same approach as the Caddy and
# runc builds). Flags follow the upstream release builds. Respects GOOS/GOARCH,
# so an image stage can cross-compile from its build platform.
# Usage: build-docker-plugins.sh OUTDIR
set -eu

COMPOSE_VERSION=5.6.1
COMPOSE_COMMIT=6cbc19a3fd59e305fb814dca4d854a4fb67d2941
BUILDX_VERSION=0.38.1
BUILDX_COMMIT=0dbe87f36ed472fe36b6796c3f70b4210a731fc5
# Dependency floors. Advisories without any fixed version are listed, with
# their review date, in .trivyignore.
FLOORS="golang.org/x/net=golang.org/x/net@v0.60.0 github.com/moby/go-archive=github.com/moby/go-archive@v0.3.0"

out=${1:?usage: build-docker-plugins.sh OUTDIR}
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT INT TERM
export CGO_ENABLED=0

# checkout DIR REPOSITORY COMMIT: fetches exactly one commit, then raises the
# dependency floors the module uses.
checkout() {
  git init -q "$1"
  git -C "$1" fetch -q --depth 1 "$2" "$3"
  git -C "$1" checkout -q FETCH_HEAD
  for floor in $FLOORS; do
    (cd "$1" && go mod edit -replace "$floor")
  done
  (cd "$1" && go mod tidy && if [ -d vendor ]; then go mod vendor; fi)
}

mode() {
  if [ -d "$1/vendor" ]; then echo vendor; else echo mod; fi
}

checkout "$work/compose" https://github.com/docker/compose "$COMPOSE_COMMIT"
(cd "$work/compose" && go build -trimpath -buildvcs=false -mod="$(mode .)" -tags e2e \
  -ldflags "-s -w -X github.com/docker/compose/v5/internal.Version=v$COMPOSE_VERSION" \
  -o "$out/docker-compose" ./cmd)

checkout "$work/buildx" https://github.com/docker/buildx "$BUILDX_COMMIT"
(cd "$work/buildx" && go build -trimpath -buildvcs=false -mod="$(mode .)" \
  -ldflags "-s -w -X github.com/docker/buildx/version.Version=v$BUILDX_VERSION -X github.com/docker/buildx/version.Revision=$BUILDX_COMMIT -X github.com/docker/buildx/version.Package=github.com/docker/buildx" \
  -o "$out/docker-buildx" ./cmd/buildx)
