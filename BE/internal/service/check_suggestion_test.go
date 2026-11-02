package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSuggestCheckCommand(t *testing.T) {
	tests := []struct {
		name    string
		paths   []string
		files   map[string]string
		command string
		basis   []string
	}{
		{
			name:    "go module at the root",
			paths:   []string{"go.mod", "main.go"},
			command: "go build ./... && go vet ./...",
			basis:   []string{"go.mod"},
		},
		{
			name:    "monorepo with a Go backend and two npm apps",
			paths:   []string{"README.md", "BE/go.mod", "UI/package.json", "UI/package-lock.json", "WEB/package.json", "WEB/package-lock.json"},
			files:   map[string]string{"UI/package.json": `{"scripts":{"test":"vitest","check":"svelte-check"}}`, "WEB/package.json": `{"scripts":{"build":"vite build","lint":"eslint ."}}`},
			command: "(cd BE && go build ./... && go vet ./...) && (cd UI && npm ci && npm run check) && (cd WEB && npm ci && npm run lint)",
			basis:   []string{"BE/go.mod", "UI/package.json", "WEB/package.json"},
		},
		{
			name:    "npm test is skipped when it watches or is the npm placeholder",
			paths:   []string{"package.json", "a/package.json"},
			files:   map[string]string{"package.json": `{"scripts":{"test":"echo \"Error: no test specified\" && exit 1","build":"tsc"}}`, "a/package.json": `{"scripts":{"test":"jest --watch"}}`},
			command: "npm install && npm run build",
			basis:   []string{"package.json"},
		},
		{
			name:    "pnpm and yarn lockfiles choose their package manager",
			paths:   []string{"web/package.json", "web/pnpm-lock.yaml", "app/package.json", "app/yarn.lock"},
			files:   map[string]string{"web/package.json": `{"scripts":{"typecheck":"tsc --noEmit"}}`, "app/package.json": `{"scripts":{"test":"jest"}}`},
			command: "(cd app && yarn install --frozen-lockfile && yarn run test) && (cd web && pnpm install --frozen-lockfile && pnpm run typecheck)",
			basis:   []string{"app/package.json", "web/package.json"},
		},
		{
			name:    "npm workspaces at the root cover their packages",
			paths:   []string{"package.json", "package-lock.json", "packages/package.json"},
			files:   map[string]string{"package.json": `{"workspaces":["packages/*"],"scripts":{"test":"vitest run"}}`, "packages/package.json": `{"scripts":{"check":"tsc"}}`},
			command: "npm ci && npm run test",
			basis:   []string{"package.json"},
		},
		{
			name:    "cargo, then a Makefile only where nothing else is detected",
			paths:   []string{"Cargo.toml", "tools/Makefile", "docs/Makefile"},
			files:   map[string]string{"tools/Makefile": "build:\n\tcc x.c\ntest: build\n\t./x\n", "docs/Makefile": "html:\n\tsphinx-build\n"},
			command: "cargo build && cargo test && (cd tools && make test)",
			basis:   []string{"Cargo.toml", "tools/Makefile"},
		},
		{
			name:    "a root Makefile is left out when sub-folder projects have their own checks",
			paths:   []string{"Makefile", "BE/go.mod", "UI/package.json", "UI/package-lock.json"},
			files:   map[string]string{"Makefile": "test:\n\tdocker compose up -d db && go test ./...\ncheck: test\n", "UI/package.json": `{"scripts":{"check":"svelte-check"}}`},
			command: "(cd BE && go build ./... && go vet ./...) && (cd UI && npm ci && npm run check)",
			basis:   []string{"BE/go.mod", "UI/package.json"},
		},
		{
			name:    "a root Makefile is kept when it is the only check",
			paths:   []string{"Makefile", "docs/Makefile", "scripts/run.sh"},
			files:   map[string]string{"Makefile": "check:\n\t./scripts/run.sh\n"},
			command: "make check",
			basis:   []string{"Makefile"},
		},
		{
			name:    "a root project manifest still runs beside sub-folder checks",
			paths:   []string{"go.mod", "Makefile", "web/package.json"},
			files:   map[string]string{"Makefile": "test:\n\tgo test ./...\n", "web/package.json": `{"scripts":{"lint":"eslint ."}}`},
			command: "go build ./... && go vet ./... && (cd web && npm install && npm run lint)",
			basis:   []string{"go.mod", "web/package.json"},
		},
		{
			name:    "python projects get a syntax check",
			paths:   []string{"pyproject.toml", "pkg/__init__.py"},
			command: "python3 -m compileall -q .",
			basis:   []string{"pyproject.toml"},
		},
		{
			name:  "vendored and hidden folders are ignored",
			paths: []string{"vendor/go.mod", "node_modules/package.json", ".github/package.json", "examples/go.mod", "README.md"},
		},
		{
			name:  "unsafe folder names are never put in a shell command",
			paths: []string{"a b/go.mod", "x;rm/go.mod"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			files := map[string][]byte{}
			for path, content := range test.files {
				files[path] = []byte(content)
			}
			suggestion := suggestCheckCommand(test.paths, files)
			if test.command == "" {
				require.Nil(t, suggestion)
				return
			}
			require.NotNil(t, suggestion)
			require.Equal(t, []string{"sh", "-c", test.command}, suggestion.Command)
			require.Equal(t, test.basis, suggestion.Basis)
		})
	}
}

func TestCheckManifestsToRead(t *testing.T) {
	paths := []string{"package.json", "UI/package.json", "UI/src/package.json", "BE/go.mod", "tools/Makefile", "vendor/package.json"}
	require.Equal(t, []string{"UI/package.json", "package.json", "tools/Makefile"}, checkManifestsToRead(paths))
}
