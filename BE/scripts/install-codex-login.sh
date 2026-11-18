#!/bin/sh
# Install only the native executable used for ChatGPT device authorization.
set -eu
version=0.169.0
case "$1" in
    amd64) platform=x64; triple=x86_64; integrity='o05NyAOCDygSPRo1yinzxvkRSdmsZyBD9E7mbpmjRanSfM8lypqWtQgEo4XfNkwhuCR2aFu8mlGOsQGFXVbVww==' ;;
    arm64) platform=arm64; triple=aarch64; integrity='NZ/DaQUpcQmxmqee7Z9uC1rzw3GhSbooawcaTe1LN5Yk+WlinUKGdTXJgClRT/9ogQEEM9TWxYzvxPfj5G/xFg==' ;;
    *) echo "Unsupported Codex architecture: $1" >&2; exit 1 ;;
esac
archive=$(mktemp)
trap 'rm -f "$archive"' EXIT
wget -q -O "$archive" "https://registry.npmjs.org/@openai/codex/-/codex-${version}-linux-${platform}.tgz"
digest=$(openssl dgst -sha512 -binary "$archive" | openssl base64 -A)
[ "$digest" = "$integrity" ]
tar -xzOf "$archive" "package/vendor/${triple}-unknown-linux-musl/bin/codex" > /usr/local/bin/codex
chmod 755 /usr/local/bin/codex
