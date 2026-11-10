#!/bin/sh
# Write checksums.txt (sha256sum format, bare file names) for every release
# archive in a directory, and fail if there are none.
#
#   checksums.sh <dir>
set -eu
cd "$1"
set -- kuayle-*.tar.gz kuayle-*.zip
files=""
for f in "$@"; do [ -f "$f" ] && files="$files $f"; done
[ -n "$files" ] || { echo "no release archives in $(pwd)" >&2; exit 1; }
if command -v sha256sum >/dev/null 2>&1; then
    # shellcheck disable=SC2086
    sha256sum $files > checksums.txt
else
    # shellcheck disable=SC2086
    shasum -a 256 $files > checksums.txt
fi
cat checksums.txt
