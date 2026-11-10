#!/bin/sh
# Tests for CLI/install.sh. A fake `curl` on PATH serves a local release, so
# the installer runs unmodified and still asks only for the official URLs.
#
#   sh CLI/scripts/install_test.sh
set -eu

here=$(cd "$(dirname "$0")" && pwd)
installer="$here/../install.sh"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

case "$(uname -s)/$(uname -m)" in
    Linux/x86_64|Linux/amd64) target=x86_64-unknown-linux-musl ;;
    Linux/aarch64|Linux/arm64) target=aarch64-unknown-linux-musl ;;
    Darwin/arm64) target=aarch64-apple-darwin ;;
    *) echo "skip: no release target for this platform"; exit 0 ;;
esac

release="$work/release"
mkdir -p "$release" "$work/bin"
printf '#!/bin/sh\necho kuayle-test-binary\n' > "$work/kuayle"
"$here/package-release.sh" "$target" "$work/kuayle" "$release" >/dev/null
"$here/checksums.sh" "$release" >/dev/null

# Releases API: a newer server release first, then a prerelease, then the CLI.
cat > "$work/releases.json" <<'JSON'
[
  {
    "tag_name": "v9.0.0",
    "prerelease": false
  },
  {
    "tag_name": "cli-v0.3.0",
    "prerelease": true
  },
  {
    "tag_name": "cli-v0.2.0",
    "prerelease": false
  }
]
JSON

# Fake curl: log the URL and serve it from the fixtures, or fail like curl -f.
cat > "$work/bin/curl" <<EOF
#!/bin/sh
out=""
url=""
while [ \$# -gt 0 ]; do
    case "\$1" in
        -o) out=\$2; shift ;;
        -*) ;;
        *) url=\$1 ;;
    esac
    shift
done
echo "\$url" >> "$work/urls.log"
case "\$url" in
    "https://api.github.com/repos/carbogninalberto/kuayle/releases?per_page=100") src="$work/releases.json" ;;
    "https://github.com/carbogninalberto/kuayle/releases/download/cli-v0.2.0/"*) src="$release/\${url##*/}" ;;
    *) exit 22 ;;
esac
[ -f "\$src" ] || exit 22
if [ -n "\$out" ]; then cp "\$src" "\$out"; else cat "\$src" || { echo "curl: (56) Failure writing output to destination" >&2; exit 23; }; fi
EOF
chmod +x "$work/bin/curl"

run() { PATH="$work/bin:$PATH" KUAYLE_INSTALL_DIR="$work/install" sh "$installer"; }
pass=0
ok() { pass=$((pass + 1)); echo "ok - $1"; }
not_ok() { echo "not ok - $1" >&2; exit 1; }

# 1. Latest resolves to the newest stable cli-v* tag and installs a verified binary.
run > "$work/out" 2>&1 || { cat "$work/out"; not_ok "install latest"; }
[ "$("$work/install/kuayle")" = kuayle-test-binary ] || not_ok "installed binary runs"
grep -q 'cli-v0.2.0' "$work/out" && grep -q 'SHA-256 verified' "$work/out" || not_ok "reports version and verification"
if grep -v '^https://\(api\.\)\{0,1\}github\.com/\(repos/\)\{0,1\}carbogninalberto/kuayle/' "$work/urls.log"; then
    not_ok "only official URLs are requested"
fi
ok "installs the newest stable CLI release after verifying it"

# 2. A tampered archive is rejected and the installed binary is kept.
cp "$release/kuayle-$target.tar.gz" "$work/good.tar.gz"
printf 'tampered' >> "$release/kuayle-$target.tar.gz"
if run > "$work/out" 2>&1; then not_ok "mismatch must fail"; fi
grep -q 'checksum mismatch' "$work/out" || not_ok "mismatch message"
[ "$("$work/install/kuayle")" = kuayle-test-binary ] || not_ok "existing binary untouched"
cp "$work/good.tar.gz" "$release/kuayle-$target.tar.gz"
ok "fails closed on a checksum mismatch"

# 3. A checksums.txt without this archive is rejected.
cp "$release/checksums.txt" "$work/sums"
grep -v "kuayle-$target" "$work/sums" > "$release/checksums.txt" || true
if run > "$work/out" 2>&1; then not_ok "missing entry must fail"; fi
grep -q 'no entry' "$work/out" || not_ok "missing entry message"
cp "$work/sums" "$release/checksums.txt"
ok "fails closed when checksums.txt has no entry"

# 4. Only cli-vX.Y.Z tags are accepted.
# Subshells: in POSIX sh an assignment before a function call outlives it.
if (KUAYLE_VERSION='v9.0.0' run) > "$work/out" 2>&1; then not_ok "server tag must be rejected"; fi
if (KUAYLE_VERSION='cli-v1.0.0;rm' run) > "$work/out" 2>&1; then not_ok "bad tag must be rejected"; fi
ok "rejects tags that are not CLI releases"

# 5. A releases list larger than a pipe buffer is read to the end, so curl
#    never reports a broken pipe (curl: (56) Failure writing output).
{ printf '['; i=0; while [ $i -lt 1500 ]; do printf '{"tag_name": "v1.%s.0", "prerelease": false, "body": "%s"},\n' "$i" "padding padding padding padding"; i=$((i + 1)); done
  printf '{"tag_name": "cli-v0.2.0", "prerelease": false}]\n'; } > "$work/big.json"
sed 's/^\[/[{"tag_name": "cli-v0.2.0", "prerelease": false},/' "$work/big.json" > "$work/releases.json"
rm -rf "$work/install"
run > "$work/out" 2>&1 || { cat "$work/out"; not_ok "install with a large releases list"; }
if grep -q 'curl:' "$work/out"; then cat "$work/out"; not_ok "no broken-pipe noise"; fi
ok "reads a large releases list without broken-pipe noise"

# 6. Minified API JSON resolves the same release.
tr -d ' \n' < "$work/releases.json" > "$work/min.json" && mv "$work/min.json" "$work/releases.json"
rm -rf "$work/install"
run > "$work/out" 2>&1 || { cat "$work/out"; not_ok "install from minified JSON"; }
grep -q 'cli-v0.2.0' "$work/out" || not_ok "minified JSON picks cli-v0.2.0"
ok "parses minified releases JSON"

echo "$pass tests passed"
