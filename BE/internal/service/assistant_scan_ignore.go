package service

import (
	"path"
	"sort"
	"strings"
)

// matchGlob matches a slash-separated path against a glob where "**" spans
// any number of path segments (including none) and every other segment uses
// path.Match syntax (*, ?, [...]), which never crosses a "/".
func matchGlob(pattern, name string) bool {
	return matchSegments(strings.Split(strings.Trim(pattern, "/"), "/"), strings.Split(name, "/"))
}

func matchSegments(pattern, name []string) bool {
	for len(pattern) > 0 {
		if pattern[0] == "**" {
			// Collapse repeated "**" and try every split point.
			for len(pattern) > 0 && pattern[0] == "**" {
				pattern = pattern[1:]
			}
			if len(pattern) == 0 {
				return true
			}
			for i := 0; i <= len(name); i++ {
				if matchSegments(pattern, name[i:]) {
					return true
				}
			}
			return false
		}
		if len(name) == 0 {
			return false
		}
		if ok, err := path.Match(pattern[0], name[0]); err != nil || !ok {
			return false
		}
		pattern, name = pattern[1:], name[1:]
	}
	return len(name) == 0
}

// validGlob reports whether every segment is valid path.Match syntax.
func validGlob(pattern string) bool {
	for _, segment := range strings.Split(strings.Trim(pattern, "/"), "/") {
		if segment == "**" {
			continue
		}
		if _, err := path.Match(segment, ""); err != nil {
			return false
		}
	}
	return true
}

// ignoreRule is one line of a .gitignore file, relative to the directory
// (base, "" for the root) that holds the file.
type ignoreRule struct {
	base     string
	pattern  string
	negate   bool
	dirOnly  bool
	anchored bool
}

// gitignore holds the rules of every .gitignore of a checkout. It implements
// the common subset of gitignore(5): comments, negation, directory-only and
// anchored patterns, "**", and deeper files overriding shallower ones. A file
// inside an ignored directory stays ignored, as in git.
type gitignore struct{ rules []ignoreRule }

func parseGitignore(base, content string) []ignoreRule {
	var rules []ignoreRule
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasSuffix(line, " ") && !strings.HasSuffix(line, `\ `) {
			line = strings.TrimRight(line, " ")
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		rule := ignoreRule{base: base}
		if strings.HasPrefix(line, "!") {
			rule.negate, line = true, line[1:]
		} else if strings.HasPrefix(line, `\`) {
			line = line[1:]
		}
		if strings.HasSuffix(line, "/") {
			rule.dirOnly, line = true, strings.TrimRight(line, "/")
		}
		if strings.Contains(line, "/") {
			rule.anchored, line = true, strings.TrimLeft(line, "/")
		}
		if line == "" || !validGlob(line) {
			continue
		}
		rule.pattern = line
		rules = append(rules, rule)
	}
	return rules
}

// newGitignore orders the rules so deeper .gitignore files come last and win.
func newGitignore(files map[string]string) *gitignore {
	bases := make([]string, 0, len(files))
	for base := range files {
		bases = append(bases, base)
	}
	sort.Slice(bases, func(i, j int) bool {
		di, dj := strings.Count(bases[i], "/"), strings.Count(bases[j], "/")
		if bases[i] == "" || bases[j] == "" {
			return bases[i] == ""
		}
		if di != dj {
			return di < dj
		}
		return bases[i] < bases[j]
	})
	g := &gitignore{}
	for _, base := range bases {
		g.rules = append(g.rules, parseGitignore(base, files[base])...)
	}
	return g
}

// ignored reports whether a file path is ignored, checking its parent
// directories first.
func (g *gitignore) ignored(file string) bool {
	if g == nil || len(g.rules) == 0 {
		return false
	}
	parts := strings.Split(file, "/")
	for i := 1; i < len(parts); i++ {
		if g.match(strings.Join(parts[:i], "/"), true) {
			return true
		}
	}
	return g.match(file, false)
}

func (g *gitignore) match(name string, dir bool) bool {
	ignored := false
	for _, rule := range g.rules {
		rel := name
		if rule.base != "" {
			var ok bool
			if rel, ok = strings.CutPrefix(name, rule.base+"/"); !ok {
				continue
			}
		}
		if rule.dirOnly && !dir {
			continue
		}
		var matched bool
		if rule.anchored {
			matched = matchGlob(rule.pattern, rel)
		} else {
			ok, err := path.Match(rule.pattern, path.Base(rel))
			matched = err == nil && ok
		}
		if matched {
			ignored = !rule.negate
		}
	}
	return ignored
}
