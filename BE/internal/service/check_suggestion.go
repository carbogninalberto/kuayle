package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/google/uuid"
)

// CheckSuggestion is a check command Kuayle proposes for a repository from
// its manifests. It is only a proposal: an admin saves it as the check.
type CheckSuggestion struct {
	Command []string `json:"command"`
	// Basis lists the manifest paths the command was derived from.
	Basis []string `json:"basis"`
}

// ErrRepositoryUnreadable means GitHub could not list or read the repository.
var ErrRepositoryUnreadable = errors.New("the repository could not be read from GitHub")

// RepositoryManifestReader reads a repository at ref: the file paths of its
// root and first-level folders, then the requested files.
type RepositoryManifestReader interface {
	RepositoryPaths(ctx context.Context, workspaceID uuid.UUID, fullName, ref string) ([]string, error)
	RepositoryFiles(ctx context.Context, workspaceID uuid.UUID, fullName, ref string, paths []string) (map[string][]byte, error)
}

// SetRepositoryManifests enables check suggestions through reader.
func (s *DevMachineService) SetRepositoryManifests(reader RepositoryManifestReader) {
	s.manifests = reader
}

// SuggestCheck proposes a check command for a linked repository's default
// branch, or returns nil when nothing in it is recognised.
func (s *DevMachineService) SuggestCheck(ctx context.Context, workspaceID, repositoryID uuid.UUID) (*CheckSuggestion, error) {
	if s.manifests == nil {
		return nil, ErrInvalidOperation
	}
	repository, err := s.store.GetLinkedRepository(ctx, workspaceID, repositoryID)
	if err != nil {
		return nil, err
	}
	if repository == nil {
		return nil, ErrRepositoryNotAllowed
	}
	paths, err := s.manifests.RepositoryPaths(ctx, workspaceID, repository.FullName, repository.DefaultBranch)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRepositoryUnreadable, err)
	}
	files, err := s.manifests.RepositoryFiles(ctx, workspaceID, repository.FullName, repository.DefaultBranch, checkManifestsToRead(paths))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRepositoryUnreadable, err)
	}
	return suggestCheckCommand(paths, files), nil
}

const maxCheckProjects = 6

// Folders that hold dependencies, samples or docs rather than a project.
var ignoredCheckFolders = map[string]bool{"vendor": true, "node_modules": true, "third_party": true, "examples": true, "example": true, "docs": true, "testdata": true, "fixtures": true}

// Folder names that are safe to put unquoted in a shell command.
var safeCheckFolder = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// checkProjects groups manifest paths by folder ("" is the root), keeping only
// the root and first-level folders a check should cover.
func checkProjects(paths []string) map[string]map[string]bool {
	projects := map[string]map[string]bool{}
	for _, path := range paths {
		dir, name := "", path
		if i := strings.IndexByte(path, '/'); i >= 0 {
			dir, name = path[:i], path[i+1:]
			if strings.Contains(name, "/") || ignoredCheckFolders[dir] || strings.HasPrefix(dir, ".") || !safeCheckFolder.MatchString(dir) {
				continue
			}
		}
		if projects[dir] == nil {
			projects[dir] = map[string]bool{}
		}
		projects[dir][name] = true
	}
	return projects
}

// checkManifestsToRead lists the files whose contents the suggestion needs.
func checkManifestsToRead(paths []string) []string {
	var read []string
	for dir, names := range checkProjects(paths) {
		for _, name := range []string{"package.json", "Makefile"} {
			if names[name] {
				read = append(read, joinCheckPath(dir, name))
			}
		}
	}
	sort.Strings(read)
	return read
}

func joinCheckPath(dir, name string) string {
	if dir == "" {
		return name
	}
	return dir + "/" + name
}

// suggestCheckCommand derives a cheap check that needs no database or
// services: builds and static checks rather than full test suites where the
// language allows it.
func suggestCheckCommand(paths []string, files map[string][]byte) *CheckSuggestion {
	projects := checkProjects(paths)
	dirs := make([]string, 0, len(projects))
	for dir := range projects {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	rootWorkspaces := false
	if root := projects[""]; root != nil && root["package.json"] {
		var manifest struct {
			Workspaces json.RawMessage `json:"workspaces"`
		}
		rootWorkspaces = json.Unmarshal(files["package.json"], &manifest) == nil && len(manifest.Workspaces) > 0
	}
	var steps, basis []string
	for _, dir := range dirs {
		if len(steps) == maxCheckProjects {
			break
		}
		step, manifest := projectCheck(projects[dir], dir, files, dir != "" && rootWorkspaces)
		if step == "" {
			continue
		}
		if dir != "" {
			step = "(cd " + dir + " && " + step + ")"
		}
		steps = append(steps, step)
		basis = append(basis, joinCheckPath(dir, manifest))
	}
	if len(steps) == 0 {
		return nil
	}
	return &CheckSuggestion{Command: []string{"sh", "-c", strings.Join(steps, " && ")}, Basis: basis}
}

// projectCheck is the check for one folder and the manifest it came from.
func projectCheck(names map[string]bool, dir string, files map[string][]byte, coveredByWorkspaces bool) (string, string) {
	switch {
	case names["go.mod"]:
		return "go build ./... && go vet ./...", "go.mod"
	case names["package.json"] && !coveredByWorkspaces:
		if step := npmCheck(names, files[joinCheckPath(dir, "package.json")]); step != "" {
			return step, "package.json"
		}
	case names["Cargo.toml"]:
		return "cargo build && cargo test", "Cargo.toml"
	}
	if names["Makefile"] {
		if target := makeCheckTarget(files[joinCheckPath(dir, "Makefile")]); target != "" {
			return "make " + target, "Makefile"
		}
	}
	for _, manifest := range []string{"pyproject.toml", "setup.py", "requirements.txt"} {
		if names[manifest] {
			return "python3 -m compileall -q .", manifest
		}
	}
	return "", ""
}

var placeholderNpmTest = regexp.MustCompile(`no test specified`)

func npmCheck(names map[string]bool, manifest []byte) string {
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if json.Unmarshal(manifest, &pkg) != nil {
		return ""
	}
	script := ""
	for _, name := range []string{"check", "typecheck", "lint", "test", "build"} {
		command, ok := pkg.Scripts[name]
		if !ok || strings.TrimSpace(command) == "" {
			continue
		}
		// A watching or placeholder test never finishes or always fails.
		if name == "test" && (strings.Contains(command, "--watch") || placeholderNpmTest.MatchString(command)) {
			continue
		}
		script = name
		break
	}
	if script == "" {
		return ""
	}
	switch {
	case names["pnpm-lock.yaml"]:
		return "pnpm install --frozen-lockfile && pnpm run " + script
	case names["yarn.lock"]:
		return "yarn install --frozen-lockfile && yarn run " + script
	case names["package-lock.json"]:
		return "npm ci && npm run " + script
	}
	return "npm install && npm run " + script
}

var makeTarget = regexp.MustCompile(`(?m)^(check|test)\s*:`)

func makeCheckTarget(makefile []byte) string {
	found := map[string]bool{}
	for _, match := range makeTarget.FindAllSubmatch(makefile, -1) {
		found[string(match[1])] = true
	}
	for _, target := range []string{"check", "test"} {
		if found[target] {
			return target
		}
	}
	return ""
}
