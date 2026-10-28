#!/bin/sh
set -eu

target_arch=${1:-${TARGETARCH:-}}
case "$target_arch" in
  amd64)
    goose_arch=x86_64
    goose_sha256=10eb96cb6a159b6e1d38eeedbef6ab64cd28168aad91948f1fa5c34eefdf98e3
    kimi_arch=x86_64
    kimi_sha256=a676204f7804ed5dae87652a9d66f578e0718d5026c9cb1bfa32b9a5eace4208
    ;;
  arm64)
    goose_arch=aarch64
    goose_sha256=c32605acfd560aeb2adbf17ec359cc0217e28022a7e647b35a6af5dcfca91749
    kimi_arch=aarch64
    kimi_sha256=9e06eea96273600aed485b1f0bedda95b7084db4f22cf28614fe20138933507a
    ;;
  *)
    echo "unsupported architecture: ${target_arch:-<empty>}" >&2
    exit 1
    ;;
esac

CLAUDE_CODE_VERSION=2.1.312
OPENCODE_VERSION=1.19.5
CODEX_VERSION=0.165.0
QWEN_VERSION=0.26.3
GOOSE_VERSION=1.57.0
KIMI_VERSION=1.52.0

# Crush is built from source in the image (see the crush-build stage) and only verified here.
npm install --global --allow-scripts=@anthropic-ai/claude-code "@anthropic-ai/claude-code@${CLAUDE_CODE_VERSION}"
npm install --global --allow-scripts=opencode-ai "opencode-ai@${OPENCODE_VERSION}"
npm install --global "@openai/codex@${CODEX_VERSION}"
npm install --global --allow-scripts=@qwen-code/qwen-code "@qwen-code/qwen-code@${QWEN_VERSION}"
# qwen-code 0.26.1 bundles sharp 0.35.4 (GHSA-wq5f-xc86-pv6w); 0.35.5 is the patch release.
(cd "$(npm root --global)/@qwen-code/qwen-code" && npm install --no-save --no-audit --no-fund --ignore-scripts "sharp@0.35.5")

download_dir=$(mktemp -d "${TMPDIR:-/tmp}/kuayle-agent-clis.XXXXXX")
cleanup() {
  rm -rf "$download_dir"
}
trap cleanup EXIT INT TERM

download_and_verify() {
  name=$1
  url=$2
  expected_sha256=$3
  archive="$download_dir/${name}.tar.gz"

  if ! curl -fsSL "$url" -o "$archive"; then
    echo "failed to download ${name} from ${url}" >&2
    exit 1
  fi
  actual_sha256=$(sha256sum "$archive" | awk '{print $1}')
  if [ "$actual_sha256" != "$expected_sha256" ]; then
    echo "sha256 mismatch for ${name}: expected ${expected_sha256}, got ${actual_sha256}" >&2
    exit 1
  fi
}

extract_and_install() {
  name=$1
  binary_name=${2:-$1}
  extract_dir="$download_dir/extract-${name}"
  mkdir -p "$extract_dir"
  if ! tar -xzf "$download_dir/${name}.tar.gz" -C "$extract_dir"; then
    echo "failed to extract ${name}" >&2
    exit 1
  fi

  binary_path=$(find "$extract_dir" -type f -name "$binary_name" -print)
  binary_count=$(printf '%s\n' "$binary_path" | awk 'NF { count++ } END { print count + 0 }')
  if [ "$binary_count" -ne 1 ]; then
    echo "expected one ${binary_name} binary in ${name}, found ${binary_count}" >&2
    exit 1
  fi
  install -m 0755 "$binary_path" "/usr/local/bin/${binary_name}"
}

download_and_verify goose \
  "https://github.com/block/goose/releases/download/v${GOOSE_VERSION}/goose-${goose_arch}-unknown-linux-gnu.tar.gz" \
  "$goose_sha256"
download_and_verify kimi \
  "https://github.com/MoonshotAI/kimi-cli/releases/download/${KIMI_VERSION}/kimi-${KIMI_VERSION}-${kimi_arch}-unknown-linux-gnu.tar.gz" \
  "$kimi_sha256"

extract_and_install goose
extract_and_install kimi

for cli in claude opencode codex qwen crush goose kimi; do
  "$cli" --version
done
