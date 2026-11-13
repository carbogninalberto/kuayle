#!/usr/bin/env bash
# Resolve the source and architecture set for an automatic release or a manual
# rebuild. Manual runs reuse the release's bump commit and never create a new one.
set -euo pipefail

: "${EVENT_NAME:?EVENT_NAME is required}"
: "${RELEASE_TAG:?RELEASE_TAG is required}"
: "${INCLUDE_ARM64:?INCLUDE_ARM64 is required}"
: "${GITHUB_REPOSITORY:?GITHUB_REPOSITORY is required}"
: "${GITHUB_OUTPUT:?GITHUB_OUTPUT is required}"

case "$INCLUDE_ARM64" in
  true|false) ;;
  *)
    echo "::error::INCLUDE_ARM64 must be true or false" >&2
    exit 1
    ;;
esac

# Docker tags cannot contain '+', so build-metadata tags are not publishable.
if [[ ! "$RELEASE_TAG" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]]; then
  echo "::error::release_tag must be a server release tag such as v0.2.32" >&2
  exit 1
fi

if [ "$EVENT_NAME" = "workflow_dispatch" ]; then
  if [ "${GITHUB_REF:-}" != "refs/heads/main" ]; then
    echo "::error::manual releases must run from refs/heads/main" >&2
    exit 1
  fi

  if ! release=$(gh api "repos/$GITHUB_REPOSITORY/releases/tags/$RELEASE_TAG" 2>/dev/null); then
    echo "::error::release_tag does not identify an existing release" >&2
    exit 1
  fi
  if [ "$(printf '%s' "$release" | jq -r .draft)" != "false" ]; then
    echo "::error::release_tag must identify a published, non-draft release" >&2
    exit 1
  fi

  # The automatic release builds the bump commit, not the tag commit. Require
  # the bot-authored commit so a human commit with a matching subject cannot
  # redirect a manual rebuild.
  source_ref=$(git log HEAD --format='%H%x09%s%x09%an' \
    | awk -F '\t' -v subject="bump(version): $RELEASE_TAG" \
      '$2 == subject && $3 == "github-actions[bot]" && !found { print $1; found = 1 }')
  if [ -z "$source_ref" ]; then
    echo "::error::no 'bump(version): $RELEASE_TAG' commit by github-actions[bot] found on main; publish a new release instead" >&2
    exit 1
  fi

  source_version=$(git show "$source_ref:UI/package.json" 2>/dev/null | jq -r .version || true)
  if [ "$source_version" != "${RELEASE_TAG#v}" ]; then
    echo "::error::the release source $source_ref does not match $RELEASE_TAG" >&2
    exit 1
  fi
else
  if [ -z "${SOURCE_SHA:-}" ]; then
    echo "::error::SOURCE_SHA is required for automatic releases" >&2
    exit 1
  fi
  source_ref="$SOURCE_SHA"
fi

{
  echo "tag=$RELEASE_TAG"
  echo "source_ref=$source_ref"
  echo "include_arm64=$INCLUDE_ARM64"
} >> "$GITHUB_OUTPUT"
