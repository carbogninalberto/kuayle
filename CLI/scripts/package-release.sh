#!/bin/sh
# Package one release binary as the archive install.sh, install.ps1 and
# `kuayle self-update` expect: kuayle-<target>.tar.gz holding `kuayle`, or
# kuayle-<target>.zip holding `kuayle.exe` for Windows.
#
#   package-release.sh <target> <path/to/kuayle[.exe]> <out-dir>
set -eu
target=$1 binary=$2 out=$3
mkdir -p "$out"
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT
case "$target" in
    *windows*)
        cp "$binary" "$stage/kuayle.exe"
        (cd "$stage" && 7z a -tzip "kuayle-$target.zip" kuayle.exe >/dev/null)
        mv "$stage/kuayle-$target.zip" "$out/" ;;
    *)
        cp "$binary" "$stage/kuayle"
        chmod 755 "$stage/kuayle"
        tar -C "$stage" -czf "$out/kuayle-$target.tar.gz" kuayle ;;
esac
echo "$out/$(ls "$out" | grep "kuayle-$target\.")"
