#!/bin/sh
# Install the official Kuayle CLI from a GitHub release.
#
#   curl -fsSL https://raw.githubusercontent.com/carbogninalberto/kuayle/main/CLI/install.sh | sh
#
# KUAYLE_VERSION       a CLI release tag such as cli-v0.2.0 (default: newest)
# KUAYLE_INSTALL_DIR   where to put the binary (default: ~/.local/bin)
#
# Downloads come only from github.com/carbogninalberto/kuayle. The archive is
# checked against the release's checksums.txt (SHA-256) before anything is
# extracted; a missing entry or a mismatch aborts without installing.
set -eu

REPO="carbogninalberto/kuayle"
INSTALL_DIR="${KUAYLE_INSTALL_DIR:-$HOME/.local/bin}"
VERSION="${KUAYLE_VERSION:-latest}"

fail() { echo "kuayle install: $*" >&2; exit 1; }

OS=$(uname -s)
ARCH=$(uname -m)
case "$OS/$ARCH" in
    Linux/x86_64|Linux/amd64) TARGET="x86_64-unknown-linux-musl" ;;
    Linux/aarch64|Linux/arm64) TARGET="aarch64-unknown-linux-musl" ;;
    Darwin/arm64|Darwin/aarch64) TARGET="aarch64-apple-darwin" ;;
    Darwin/x86_64) fail "Intel macOS has no prebuilt binary; use: cargo install --git https://github.com/$REPO kuayle" ;;
    *) fail "unsupported platform $OS/$ARCH" ;;
esac

if [ "$VERSION" = "latest" ]; then
    # Newest stable release whose tag starts with cli-v (server releases use v*).
    # One JSON field per line, whether the API pretty-prints or not.
    VERSION=$(curl -fsSL "https://api.github.com/repos/$REPO/releases?per_page=100" | tr ',{' '\n\n' | awk '
        /"tag_name":/ { tag = $0; sub(/.*"tag_name": *"/, "", tag); sub(/".*/, "", tag) }
        /"prerelease": *false/ && tag ~ /^cli-v/ && !found { print tag; found = 1 }
    ') || fail "could not look up the latest release"
    # awk reads to the end: stopping early would make curl report a broken pipe.
fi
echo "$VERSION" | grep -Eq '^cli-v[0-9]+\.[0-9]+\.[0-9]+$' || fail "invalid or missing CLI release tag: '$VERSION'"

ARCHIVE="kuayle-${TARGET}.tar.gz"
BASE="https://github.com/${REPO}/releases/download/${VERSION}"

if command -v sha256sum >/dev/null 2>&1; then
    sha256() { sha256sum "$1" | awk '{print $1}'; }
elif command -v shasum >/dev/null 2>&1; then
    sha256() { shasum -a 256 "$1" | awk '{print $1}'; }
else
    fail "sha256sum or shasum is required to verify the download"
fi

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT INT TERM

echo "Downloading kuayle ${VERSION} for ${TARGET}..."
curl -fsSL "$BASE/$ARCHIVE" -o "$TMP/$ARCHIVE" || fail "download failed: $BASE/$ARCHIVE"
curl -fsSL "$BASE/checksums.txt" -o "$TMP/checksums.txt" || fail "download failed: $BASE/checksums.txt"

EXPECTED=$(awk -v name="$ARCHIVE" '{ file = $2; sub(/^\*/, "", file); sub(/.*\//, "", file) } file == name { print tolower($1); exit }' "$TMP/checksums.txt")
echo "$EXPECTED" | grep -Eq '^[0-9a-f]{64}$' || fail "checksums.txt has no entry for $ARCHIVE; refusing to install"
ACTUAL=$(sha256 "$TMP/$ARCHIVE")
[ "$ACTUAL" = "$EXPECTED" ] || fail "checksum mismatch for $ARCHIVE (expected $EXPECTED, got $ACTUAL); refusing to install"
echo "✓ SHA-256 verified"

tar xzf "$TMP/$ARCHIVE" -C "$TMP" kuayle || fail "archive does not contain the kuayle binary"
mkdir -p "$INSTALL_DIR"
# Stage beside the target, then rename, so a running kuayle is never half-written.
cp "$TMP/kuayle" "$INSTALL_DIR/.kuayle.new"
chmod 755 "$INSTALL_DIR/.kuayle.new"
mv -f "$INSTALL_DIR/.kuayle.new" "$INSTALL_DIR/kuayle"

echo "✓ kuayle $VERSION installed to $INSTALL_DIR/kuayle"
case ":$PATH:" in
    *":$INSTALL_DIR:"*) ;;
    *) echo "Add $INSTALL_DIR to your PATH to run kuayle." ;;
esac
