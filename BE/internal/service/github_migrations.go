package service

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	gh "github.com/kuayle/kuayle-backend/pkg/github"
)

const migrationDirectory = "BE/migrations/"
const migrationCheckUnavailableReason = "Kuayle could not check the pull request for migration number collisions"

var migrationFilename = regexp.MustCompile(`^BE/migrations/([0-9]+)_([^/\r\n]+)\.(up|down)\.sql$`)

// pullRequestMigrationRepair looks up the base's migration directory only
// when the branch changes a migration or GitHub capped the comparison.
func pullRequestMigrationRepair(listTree func(string) ([]gh.TreeEntry, error), base, head, common, branch string, files []gh.CompareFile) (string, error) {
	var taskPaths []string
	if len(files) >= maxVerifiedFiles {
		headPaths, err := migrationPaths(listTree, head)
		if err != nil {
			return "", err
		}
		commonPaths, err := migrationPaths(listTree, common)
		if err != nil {
			return "", err
		}
		inherited := make(map[string]bool, len(commonPaths))
		for _, path := range commonPaths {
			inherited[path] = true
		}
		for _, path := range headPaths {
			if !inherited[path] {
				taskPaths = append(taskPaths, path)
			}
		}
	} else {
		for _, file := range files {
			if (file.Status == "added" || file.Status == "renamed" || file.Status == "copied") && migrationFilename.MatchString(file.Filename) {
				taskPaths = append(taskPaths, file.Filename)
			}
		}
	}
	if len(taskPaths) == 0 {
		return "", nil
	}
	if base == "" {
		return "", fmt.Errorf("GitHub's comparison did not include the base commit")
	}
	basePaths, err := migrationPaths(listTree, base)
	if err != nil {
		return "", err
	}
	return migrationRepairInstruction(branch, basePaths, taskPaths), nil
}

func migrationPaths(listTree func(string) ([]gh.TreeEntry, error), ref string) ([]string, error) {
	for _, directory := range []string{"BE", "migrations"} {
		entries, err := listTree(ref)
		if err != nil {
			return nil, err
		}
		ref = ""
		for _, entry := range entries {
			if entry.Type == "tree" && entry.Path == directory {
				ref = entry.SHA
				break
			}
		}
		if ref == "" {
			return nil, nil
		}
	}
	entries, err := listTree(ref)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, entry := range entries {
		if path := migrationDirectory + entry.Path; entry.Type == "blob" && migrationFilename.MatchString(path) {
			paths = append(paths, path)
		}
	}
	return paths, nil
}

func migrationRepairInstruction(base string, basePaths, taskPaths []string) string {
	sort.Strings(basePaths)
	byVersion := make(map[string]string, len(basePaths))
	for _, path := range basePaths {
		match := migrationFilename.FindStringSubmatch(path)
		byVersion[strings.TrimLeft(match[1], "0")] = path
	}
	sort.Strings(taskPaths)
	for _, path := range taskPaths {
		match := migrationFilename.FindStringSubmatch(path)
		other, collides := byVersion[strings.TrimLeft(match[1], "0")]
		if !collides {
			continue
		}
		return fmt.Sprintf("The task's migration %q collides with %q on %s because both use version %s. GitHub's branch update cannot renumber migration files. Fetch and merge origin/%s, move the task's up/down migration pair to the next free number after the base and task migrations, update its in-repo references, and rerun the checks before publishing.",
			path, other, base, match[1], base)
	}
	return ""
}
