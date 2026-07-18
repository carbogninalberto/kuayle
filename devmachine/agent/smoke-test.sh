#!/bin/sh
set -eu

echo "=== OpenCode Agent Image Smoke Test ==="
echo ""

# 1. Verify opencode is available
echo "[1/4] Checking opencode binary..."
if command -v opencode >/dev/null 2>&1; then
    echo "  PASS: opencode found at $(command -v opencode)"
    opencode --version 2>/dev/null || echo "  WARN: opencode --version failed (expected in some environments)"
else
    echo "  FAIL: opencode not found in PATH"
    exit 1
fi

# 2. Verify the repository toolchain is available
echo "[2/4] Checking Go and Make..."
if command -v go >/dev/null 2>&1; then
    echo "  PASS: go found at $(command -v go)"
    go version
else
    echo "  FAIL: go not found in PATH"
    exit 1
fi
command -v make >/dev/null 2>&1 || { echo "  FAIL: make not found in PATH"; exit 1; }

# 3. Verify Go can compile a tiny offline program
echo "[3/4] Compiling offline Go program..."
WORKDIR=$(mktemp -d /tmp/kuayle-agent-smoke.XXXXXX)
trap 'rm -rf "$WORKDIR"' EXIT

cat > "$WORKDIR/main.go" <<'GOEOF'
package main

import "fmt"

func main() {
	fmt.Println("kuayle-smoke-test-passed")
}
GOEOF

cd "$WORKDIR"
if go build -o smoke-binary main.go 2>&1; then
    echo "  PASS: go build succeeded"
else
    echo "  FAIL: go build failed"
    exit 1
fi

# 4. Verify the built binary runs
echo "[4/4] Running compiled binary..."
OUTPUT=$(./smoke-binary 2>&1) || true
if [ "$OUTPUT" = "kuayle-smoke-test-passed" ]; then
    echo "  PASS: binary output correct: $OUTPUT"
else
    echo "  FAIL: unexpected output: $OUTPUT"
    exit 1
fi

echo ""
echo "=== All smoke tests passed ==="
